package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"time"

	"github.com/chetto1983/aura/internal/config"
)

// maxTimeoutSec is the hard runner-side ceiling on a per-call timeout (D-16/D-19):
// the resolved timeout is clamped to this regardless of what the tool/CLI/config
// asked for, and the SAME clamped value is both put on the request ctx and
// marshalled into the wire body so the sidecar's subprocess timeout matches the
// runner's deadline.
const maxTimeoutSec = 600

// connectTimeoutSec bounds the dial only (NOT the whole call — the total timeout
// rides the request ctx via context.WithTimeout). A connect that exceeds this
// triggers the one-shot auto-start path.
const connectTimeoutSec = 5

// DockerRunner is a thin HTTP client against the compose-managed sidecar (D-08).
// It owns no container lifecycle beyond a single best-effort docker-CLI-gated
// auto-start on connect failure (D-09); execution is HTTP-only. The HTTP client
// copies the openai_compat shape: a connect timeout on the dialer, NO
// http.Client.Timeout (that would count body-read and abort a long-but-healthy
// call — the ctx carries the total timeout), and DisableKeepAlives so goleak is
// order-independent (lingering persistConn goroutines otherwise trip short test
// subsets).
type DockerRunner struct {
	httpClient        *http.Client
	url               string
	defaultTimeoutSec int
}

var _ Runner = (*DockerRunner)(nil)

// NewDockerRunner builds a DockerRunner from the resolved config. The per-runner
// default timeout seeds calls that omit one (timeoutSec <= 0).
func NewDockerRunner(cfg *config.Config) *DockerRunner {
	return &DockerRunner{
		httpClient: &http.Client{
			Transport: &http.Transport{
				DialContext: (&net.Dialer{
					Timeout: connectTimeoutSec * time.Second,
				}).DialContext,
				DisableKeepAlives: true,
			},
		},
		url:               cfg.SandboxURL,
		defaultTimeoutSec: cfg.SandboxTimeoutSec,
	}
}

func (r *DockerRunner) RunPython(ctx context.Context, code string, timeoutSec int) (Result, error) {
	return r.exec(ctx, "python", code, timeoutSec)
}

func (r *DockerRunner) RunShell(ctx context.Context, cmd string, timeoutSec int) (Result, error) {
	return r.exec(ctx, "shell", cmd, timeoutSec)
}

// wireRequest is the D-16 request body. The same effective timeout marshalled here
// is the one the call ctx deadline uses, so the sidecar's subprocess timeout and
// the runner's deadline agree.
type wireRequest struct {
	Code       string `json:"code"`
	TimeoutSec int    `json:"timeout_sec"`
}

// wireResponse is the D-16 response decoded 1:1 into Result.
type wireResponse struct {
	Stdout    string `json:"stdout"`
	Stderr    string `json:"stderr"`
	ExitCode  int    `json:"exit_code"`
	ElapsedMs int64  `json:"elapsed_ms"`
	Truncated bool   `json:"truncated"`
	LimitHit  string `json:"limit_hit"`
}

// exec is the shared per-call path. It resolves+clamps the timeout, derives the
// call ctx deadline from it, POSTs {code,timeout_sec}, and on a transport failure
// attempts ONE docker-CLI-gated auto-start then a single retry. A non-2xx or
// undecodable body is ErrSandboxProtocol; an unrecoverable transport failure is
// ErrSandboxUnreachable. A non-zero exit_code is a normal Result, never an error
// (D-18).
func (r *DockerRunner) exec(ctx context.Context, lang, code string, timeoutSec int) (Result, error) {
	effective := r.effectiveTimeout(timeoutSec)
	ctx, cancel := context.WithTimeout(ctx, time.Duration(effective)*time.Second)
	defer cancel()

	body, err := json.Marshal(wireRequest{Code: code, TimeoutSec: effective})
	if err != nil {
		return Result{}, err
	}

	resp, err := r.post(ctx, lang, body)
	if err != nil {
		// Transport failure: one best-effort auto-start, then a single retry.
		r.autoStart(ctx)
		resp, err = r.post(ctx, lang, body)
		if err != nil {
			return Result{}, fmt.Errorf("sandbox POST /exec/%s: %v: %w", lang, err, ErrSandboxUnreachable)
		}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode/100 != 2 {
		return Result{}, fmt.Errorf("sandbox /exec/%s returned status %d: %w", lang, resp.StatusCode, ErrSandboxProtocol)
	}

	var wr wireResponse
	if derr := json.NewDecoder(resp.Body).Decode(&wr); derr != nil {
		return Result{}, fmt.Errorf("sandbox /exec/%s decode: %v: %w", lang, derr, ErrSandboxProtocol)
	}
	return Result{
		Stdout:    wr.Stdout,
		Stderr:    wr.Stderr,
		ExitCode:  wr.ExitCode,
		ElapsedMs: wr.ElapsedMs,
		Truncated: wr.Truncated,
		LimitHit:  wr.LimitHit,
	}, nil
}

// effectiveTimeout substitutes the config default for an omitted (<=0) timeout and
// clamps the result to the 600s ceiling (D-16/D-19).
func (r *DockerRunner) effectiveTimeout(timeoutSec int) int {
	if timeoutSec <= 0 {
		timeoutSec = r.defaultTimeoutSec
	}
	if timeoutSec > maxTimeoutSec {
		return maxTimeoutSec
	}
	if timeoutSec <= 0 {
		return maxTimeoutSec // defensive: a zero default still gets a sane bound
	}
	return timeoutSec
}

func (r *DockerRunner) post(ctx context.Context, lang string, body []byte) (*http.Response, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, r.url+"/exec/"+lang, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	return r.httpClient.Do(httpReq)
}

// autoStart is the D-09 one-shot best-effort sidecar start. It is GATED on the
// docker CLI being on PATH (RESEARCH OQ3) — if docker is absent it returns
// immediately so the caller's retry fails and the error wraps ErrSandboxUnreachable.
// It NEVER references or mounts the docker socket (escape vector #1); it only shells
// `docker compose up -d aura-sandbox` and then health-probes GET {url}/healthz.
func (r *DockerRunner) autoStart(ctx context.Context) {
	if _, err := exec.LookPath("docker"); err != nil {
		return // docker CLI absent — nothing to start; caller wraps ErrSandboxUnreachable
	}
	upCmd := exec.CommandContext(ctx, "docker", "compose", "up", "-d", "aura-sandbox") //nolint:gosec // fixed argv, no socket
	_ = upCmd.Run()
	r.probeHealth(ctx)
}

// probeHealth polls GET {url}/healthz briefly so the retried POST has a chance to
// land against a just-started sidecar. Failures are ignored: the retry POST is the
// real signal, and a still-down sidecar surfaces as ErrSandboxUnreachable there.
func (r *DockerRunner) probeHealth(ctx context.Context) {
	for i := 0; i < 10; i++ {
		if ctx.Err() != nil {
			return
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.url+"/healthz", nil)
		if err != nil {
			return
		}
		resp, err := r.httpClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode/100 == 2 {
				return
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(200 * time.Millisecond):
		}
	}
}
