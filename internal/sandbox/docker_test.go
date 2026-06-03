//go:build !sandbox_integration

package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/chetto1983/aura/internal/config"
)

// TestMain runs the sandbox package unit tests under goleak: DockerRunner's HTTP
// client (DialContext dialer + DisableKeepAlives) manages connection goroutines,
// and the one-shot auto-start health probe loops; a leaked persistConn or probe
// goroutine would trip here. Mirrors cmd/aura/main_test.go + internal/identity.
//
// The live-sidecar tier lives in docker_integration_test.go behind the
// //go:build sandbox_integration tag (it cannot run without a Docker daemon at
// AURA_SANDBOX_URL): TestRunner_PythonHappy, TestRunner_PtraceBlocked,
// TestRunner_ProcRootDenied, TestRunner_SocketBlocked, TestRunner_UnshareBlocked,
// TestRunner_TimeoutLimitHit, and the D-20 positive control
// TestRunner_BakedPackagesImport. That file carries its own goleak TestMain so the
// integration build is also leak-checked. This (untagged) file covers the typed
// error + timeout-delivery contracts without a sidecar.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func testConfig(url string, defaultTimeout int) *config.Config {
	return &config.Config{SandboxURL: url, SandboxTimeoutSec: defaultTimeout}
}

func TestSeccompProfileUsesSingleArchitectureDialect(t *testing.T) {
	profile := loadSeccompProfile(t)
	hasArchMap := len(profile["archMap"]) != 0
	hasArchitectures := len(profile["architectures"]) != 0
	if hasArchMap == hasArchitectures {
		t.Fatal("Docker-compatible seccomp profile must specify exactly one architecture dialect: archMap or architectures")
	}
}

func TestSeccompProfileAllowsHTTPListenerAndDeniesEscapePrimitives(t *testing.T) {
	raw := loadSeccompProfile(t)
	var profile struct {
		Syscalls []struct {
			Names  []string `json:"names"`
			Action string   `json:"action"`
		} `json:"syscalls"`
	}
	if err := json.Unmarshal(raw["_raw"], &profile); err != nil {
		t.Fatalf("decode seccomp profile: %v", err)
	}

	allowed := map[string]bool{}
	for _, group := range profile.Syscalls {
		if group.Action != "SCMP_ACT_ALLOW" {
			continue
		}
		for _, name := range group.Names {
			allowed[name] = true
		}
	}

	for _, name := range []string{
		"accept", "accept4", "bind", "getpeername", "getsockname",
		"getsockopt", "listen", "recvfrom", "recvmsg", "sendmsg", "sendto",
		"setsockopt", "socket", "socketpair",
	} {
		if !allowed[name] {
			t.Fatalf("seccomp profile must allow %s so the sidecar listener and healthcheck can run", name)
		}
	}
	for _, name := range []string{"bpf", "connect", "kexec_load", "mount", "process_vm_readv", "ptrace", "unshare", "userfaultfd"} {
		if allowed[name] {
			t.Fatalf("seccomp profile must deny escape primitive %s by omission", name)
		}
	}
}

func TestComposeSandboxUsesEgresslessBridgeWithLoopbackPort(t *testing.T) {
	raw, err := os.ReadFile("../../compose.yaml")
	if err != nil {
		t.Fatalf("read compose.yaml: %v", err)
	}
	compose := string(raw)
	if strings.Contains(compose, "\n    network_mode: none") {
		t.Fatal("network_mode:none prevents the host runner from reaching the published sandbox API")
	}
	for _, want := range []string{
		`"127.0.0.1:18901:18901"`,
		"aura-sandbox-egressless:",
		`com.docker.network.bridge.enable_ip_masquerade: "false"`,
		"- aura-sandbox-egressless",
		"/proc/1/cmdline",
	} {
		if !strings.Contains(compose, want) {
			t.Fatalf("compose.yaml must include %q for host-reachable, egress-contained sandbox networking", want)
		}
	}
	if strings.Contains(compose, "urllib.request.urlopen") {
		t.Fatal("sandbox healthcheck must not require connect(2), which seccomp denies for egress containment")
	}
}

func loadSeccompProfile(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	raw, err := os.ReadFile("../../sandbox/seccomp.json")
	if err != nil {
		t.Fatalf("read seccomp profile: %v", err)
	}
	var profile map[string]json.RawMessage
	if err := json.Unmarshal(raw, &profile); err != nil {
		t.Fatalf("seccomp profile must be valid JSON: %v", err)
	}
	profile["_raw"] = raw
	return profile
}

// TestDockerRunner_UnreachableSentinel: a dead URL with docker absent on PATH
// (the auto-start is a no-op) surfaces a wrapped ErrSandboxUnreachable.
func TestDockerRunner_UnreachableSentinel(t *testing.T) {
	t.Setenv("PATH", "") // exec.LookPath("docker") fails → autoStart is a no-op
	r := NewDockerRunner(testConfig("http://127.0.0.1:1", 5))
	_, err := r.RunPython(context.Background(), "print(1)", 0)
	if err == nil {
		t.Fatal("want an error against a dead URL")
	}
	if !errors.Is(err, ErrSandboxUnreachable) {
		t.Fatalf("want errors.Is ErrSandboxUnreachable, got %v", err)
	}
}

// TestRunShell_MalformedProtocol: a 200 with an undecodable body is a wrapped
// ErrSandboxProtocol — the runner never trusts a partial response (D-18).
func TestRunShell_MalformedProtocol(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "this is not json")
	}))
	defer srv.Close()

	r := NewDockerRunner(testConfig(srv.URL, 5))
	_, err := r.RunShell(context.Background(), "echo hi", 0)
	if err == nil {
		t.Fatal("want an error for an undecodable body")
	}
	if !errors.Is(err, ErrSandboxProtocol) {
		t.Fatalf("want errors.Is ErrSandboxProtocol, got %v", err)
	}
}

// TestRunShell_Non2xxProtocol: a non-2xx status is ErrSandboxProtocol too.
func TestRunShell_Non2xxProtocol(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	r := NewDockerRunner(testConfig(srv.URL, 5))
	if _, err := r.RunPython(context.Background(), "x", 0); !errors.Is(err, ErrSandboxProtocol) {
		t.Fatalf("want ErrSandboxProtocol on a 500, got %v", err)
	}
}

// TestRunner_NonZeroExitIsResult: a non-zero exit_code is a normal Result, never a
// Go error (D-18 — code's fault → result).
func TestRunner_NonZeroExitIsResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(wireResponse{Stdout: "", Stderr: "boom", ExitCode: 1})
	}))
	defer srv.Close()

	r := NewDockerRunner(testConfig(srv.URL, 5))
	res, err := r.RunShell(context.Background(), "false", 0)
	if err != nil {
		t.Fatalf("a non-zero exit must NOT be a Go error, got %v", err)
	}
	if res.ExitCode != 1 || res.Stderr != "boom" {
		t.Fatalf("want exit 1 / stderr boom, got %+v", res)
	}
}

// TestRunner_TimeoutClampedAndBodied: the runner marshals the SAME effective
// timeout it puts on the ctx into the {"code","timeout_sec"} body. A value >600 is
// clamped to 600; a 0/omitted value substitutes the config default (D-16/D-19).
func TestRunner_TimeoutClampedAndBodied(t *testing.T) {
	var got wireRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_ = json.NewDecoder(req.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(wireResponse{Stdout: "ok"})
	}))
	defer srv.Close()

	r := NewDockerRunner(testConfig(srv.URL, 42)) // config default = 42

	if _, err := r.RunPython(context.Background(), "code-a", 9000); err != nil {
		t.Fatalf("RunPython clamp call: %v", err)
	}
	if got.TimeoutSec != maxTimeoutSec {
		t.Fatalf("timeout_sec>600 must be clamped to %d on the wire, got %d", maxTimeoutSec, got.TimeoutSec)
	}
	if got.Code != "code-a" {
		t.Fatalf("code must be marshalled verbatim, got %q", got.Code)
	}

	if _, err := r.RunShell(context.Background(), "code-b", 0); err != nil {
		t.Fatalf("RunShell default call: %v", err)
	}
	if got.TimeoutSec != 42 {
		t.Fatalf("omitted timeout must substitute the config default (42) on the wire, got %d", got.TimeoutSec)
	}

	if _, err := r.RunShell(context.Background(), "code-c", 5); err != nil {
		t.Fatalf("RunShell explicit call: %v", err)
	}
	if got.TimeoutSec != 5 {
		t.Fatalf("an in-range timeout must pass through verbatim, got %d", got.TimeoutSec)
	}
}

func TestRunner_RequestContextAllowsTimeoutReportGrace(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var got wireRequest
		if err := json.NewDecoder(req.Body).Decode(&got); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if got.TimeoutSec != 1 {
			t.Errorf("wire timeout = %d, want 1", got.TimeoutSec)
		}
		time.Sleep(1100 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(wireResponse{ExitCode: 124, LimitHit: "timeout"})
	}))
	defer srv.Close()

	r := NewDockerRunner(testConfig(srv.URL, 30))
	res, err := r.RunPython(context.Background(), "while True:\n    pass", 1)
	if err != nil {
		t.Fatalf("runner should leave response grace after sidecar timeout: %v", err)
	}
	if res.LimitHit != "timeout" || res.ExitCode != 124 {
		t.Fatalf("want structured timeout result, got %+v", res)
	}
}

// TestRunner_WireMappedOneToOne: every D-16 field decodes 1:1 into Result.
func TestRunner_WireMappedOneToOne(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(wireResponse{
			Stdout: "out", Stderr: "err", ExitCode: 7, ElapsedMs: 123, Truncated: true, LimitHit: "timeout",
		})
	}))
	defer srv.Close()

	r := NewDockerRunner(testConfig(srv.URL, 5))
	res, err := r.RunPython(context.Background(), "x", 0)
	if err != nil {
		t.Fatalf("RunPython: %v", err)
	}
	want := Result{Stdout: "out", Stderr: "err", ExitCode: 7, ElapsedMs: 123, Truncated: true, LimitHit: "timeout"}
	if res != want {
		t.Fatalf("wire→Result mismatch: got %+v want %+v", res, want)
	}
}

// TestRunner_SessionExecPathAndWire: the 2b session-bound calls POST to
// /session/{id}/exec/{lang} (NOT the stateless /exec/{lang}) with the same
// {code,timeout_sec} body, and decode the Result 1:1. No live container — the
// session lifecycle is the SessionManager's (08-05); this asserts the HTTP wire
// contract only. The stateless 2a path is untouched (covered above).
func TestRunner_SessionExecPathAndWire(t *testing.T) {
	var gotPath string
	var gotReq wireRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotPath = req.URL.Path
		_ = json.NewDecoder(req.Body).Decode(&gotReq)
		_ = json.NewEncoder(w).Encode(wireResponse{
			Stdout: "42", Stderr: "", ExitCode: 0, ElapsedMs: 7, Truncated: false, LimitHit: "",
		})
	}))
	defer srv.Close()

	// The 2b session path now resolves the per-conv container URL from a private
	// SessionEndpoint (D-05) instead of r.url; register both convIDs at the test
	// server so the wire-contract assertions still exercise /session/{id}/exec/{lang}.
	ep := NewSessionEndpoint()
	ep.Register("conv-1", srv.URL)
	ep.Register("conv-2", srv.URL)
	r := NewDockerRunnerWithEndpoint(srv.URL, 5, ep)

	res, err := r.RunPythonSession(context.Background(), "conv-1", "print(x)", 12)
	if err != nil {
		t.Fatalf("RunPythonSession: %v", err)
	}
	if gotPath != "/session/conv-1/exec/python" {
		t.Fatalf("python session path: got %q want /session/conv-1/exec/python", gotPath)
	}
	if gotReq.Code != "print(x)" || gotReq.TimeoutSec != 12 {
		t.Fatalf("session wire body: got %+v want code=print(x) timeout=12", gotReq)
	}
	want := Result{Stdout: "42", ElapsedMs: 7}
	if res != want {
		t.Fatalf("session wire→Result mismatch: got %+v want %+v", res, want)
	}

	if _, err := r.RunShellSession(context.Background(), "conv-2", "pwd", 0); err != nil {
		t.Fatalf("RunShellSession: %v", err)
	}
	if gotPath != "/session/conv-2/exec/shell" {
		t.Fatalf("shell session path: got %q want /session/conv-2/exec/shell", gotPath)
	}
	if gotReq.TimeoutSec != 5 { // 0 → config default (5) substituted, same as stateless
		t.Fatalf("omitted session timeout must substitute the config default (5), got %d", gotReq.TimeoutSec)
	}
}

// TestRunner_SessionExecProtocolError: the session path reuses the stateless
// ErrSandboxProtocol taxonomy on a non-2xx / undecodable body.
func TestRunner_SessionExecProtocolError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	ep := NewSessionEndpoint()
	ep.Register("conv-x", srv.URL)
	r := NewDockerRunnerWithEndpoint(srv.URL, 5, ep)
	if _, err := r.RunPythonSession(context.Background(), "conv-x", "x", 0); !errors.Is(err, ErrSandboxProtocol) {
		t.Fatalf("want ErrSandboxProtocol on a 502 from the session path, got %v", err)
	}
}
