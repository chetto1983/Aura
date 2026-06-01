//go:build !sandbox_integration

package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

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
