//go:build sandbox_integration

// Integration tier for the sandbox runner. Requires a live 05-02 sidecar reachable
// at AURA_SANDBOX_URL (gVisor-primary on x86, seccomp floor on arm64). It CANNOT
// run without a Docker daemon, so it is gated behind the sandbox_integration build
// tag AND the no-skip-as-green env discipline: an unset AURA_SANDBOX_URL skips
// locally but t.Fatals under $CI so a skipped tier never reports falsely green
// (CLAUDE.md, mirrors internal/identity/store_test.go envOrSkip). These tests
// assert the isolation boundary errno — a PASS that does not trigger the boundary
// (e.g. ptrace succeeding) is a false green and the assertion catches it.
package sandbox

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/chetto1983/aura/internal/config"
)

// TestMain runs the integration tier under goleak so a leaked HTTP/probe goroutine
// fails the build (mirrors the untagged docker_test.go TestMain + internal/db).
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// sidecarURL returns the configured sidecar URL or skips/fatals per no-skip-as-green.
func sidecarURL(t *testing.T) string {
	t.Helper()
	v := os.Getenv("AURA_SANDBOX_URL")
	if v == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("integration test requires AURA_SANDBOX_URL, but it is unset under CI — " +
				"a skipped integration test must not pass as green; wire it in ci.yml (05-04 DinD)")
		}
		t.Skip("integration test requires AURA_SANDBOX_URL + a live sidecar; set it and run make sandbox-up")
	}
	return v
}

func integrationRunner(t *testing.T) *DockerRunner {
	t.Helper()
	return NewDockerRunner(&config.Config{SandboxURL: sidecarURL(t), SandboxTimeoutSec: 30})
}

func ctxT(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// TestRunner_PythonHappy: print(2+2) → "4", exit 0 (CAP-01 SC#1).
func TestRunner_PythonHappy(t *testing.T) {
	res, err := integrationRunner(t).RunPython(ctxT(t), "print(2+2)", 0)
	if err != nil {
		t.Fatalf("RunPython: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("want exit 0, got %d (stderr=%q)", res.ExitCode, res.Stderr)
	}
	if strings.TrimSpace(res.Stdout) != "4" {
		t.Fatalf("want stdout 4, got %q", res.Stdout)
	}
}

// TestRunner_PtraceBlocked: a ctypes ptrace returns EPERM in stderr / non-zero —
// never a Go error (the boundary holds; D-18). CAP-01 SC#2.
func TestRunner_PtraceBlocked(t *testing.T) {
	code := `import ctypes,os
libc=ctypes.CDLL(None,use_errno=True)
r=libc.ptrace(0,0,0,0)
print("rc",r,"errno",ctypes.get_errno())`
	res, err := integrationRunner(t).RunPython(ctxT(t), code, 0)
	if err != nil {
		t.Fatalf("ptrace probe must be a Result, not a Go error: %v", err)
	}
	if !boundaryHit(res, "1") && !boundaryHit(res, "EPERM") {
		t.Fatalf("ptrace must be denied (EPERM/errno 1), got exit=%d stdout=%q stderr=%q", res.ExitCode, res.Stdout, res.Stderr)
	}
}

// TestRunner_ProcRootDenied: /proc/self/root/etc/shadow → ENOENT/EPERM. CAP-01 SC#2.
func TestRunner_ProcRootDenied(t *testing.T) {
	code := `
try:
    open('/proc/self/root/etc/shadow').read()
    print("OPENED")
except OSError as e:
    print("errno", e.errno)`
	res, err := integrationRunner(t).RunPython(ctxT(t), code, 0)
	if err != nil {
		t.Fatalf("proc-root probe must be a Result, not a Go error: %v", err)
	}
	if strings.Contains(res.Stdout, "OPENED") {
		t.Fatalf("/proc/self/root/etc/shadow must NOT be readable, got stdout=%q", res.Stdout)
	}
}

// TestRunner_SocketBlocked: socket().connect(('1.1.1.1',80)) → EPERM (net-none). CAP-01 SC#3.
func TestRunner_SocketBlocked(t *testing.T) {
	code := `import socket
try:
    s=socket.socket(); s.settimeout(3); s.connect(('1.1.1.1',80)); print("CONNECTED")
except OSError as e:
    print("errno", e.errno)`
	res, err := integrationRunner(t).RunPython(ctxT(t), code, 0)
	if err != nil {
		t.Fatalf("socket probe must be a Result, not a Go error: %v", err)
	}
	if strings.Contains(res.Stdout, "CONNECTED") {
		t.Fatalf("outbound connect must be denied, got stdout=%q", res.Stdout)
	}
}

// TestRunner_UnshareBlocked: unshare(CLONE_NEWNET) → EPERM (seccomp). CAP-01 SC#3.
func TestRunner_UnshareBlocked(t *testing.T) {
	code := `import ctypes
libc=ctypes.CDLL(None,use_errno=True)
CLONE_NEWNET=0x40000000
r=libc.unshare(CLONE_NEWNET)
print("rc",r,"errno",ctypes.get_errno())`
	res, err := integrationRunner(t).RunPython(ctxT(t), code, 0)
	if err != nil {
		t.Fatalf("unshare probe must be a Result, not a Go error: %v", err)
	}
	if !boundaryHit(res, "1") && !boundaryHit(res, "EPERM") {
		t.Fatalf("unshare(CLONE_NEWNET) must be denied (EPERM/errno 1), got exit=%d stdout=%q stderr=%q", res.ExitCode, res.Stdout, res.Stderr)
	}
}

// TestRunner_TimeoutLimitHit: an infinite loop trips limit_hit=="timeout".
func TestRunner_TimeoutLimitHit(t *testing.T) {
	res, err := integrationRunner(t).RunPython(ctxT(t), "while True:\n    pass", 2)
	if err != nil {
		t.Fatalf("timeout probe must be a Result, not a Go error: %v", err)
	}
	if res.LimitHit != "timeout" {
		t.Fatalf("want limit_hit timeout, got %q (exit=%d)", res.LimitHit, res.ExitCode)
	}
}

// TestRunner_BakedPackagesImport: the D-20 positive control — the curated baked set
// imports + runs C-extensions under the hardened seccomp/gVisor runtime, proving the
// allowlist is not too tight for the batteries-included libs.
func TestRunner_BakedPackagesImport(t *testing.T) {
	code := `import numpy,pandas
print(numpy.arange(9).reshape(3,3).sum())`
	res, err := integrationRunner(t).RunPython(ctxT(t), code, 0)
	if err != nil {
		t.Fatalf("baked-package probe must be a Result, not a Go error: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("baked numpy/pandas import must run under the hardened runtime, got exit=%d stderr=%q", res.ExitCode, res.Stderr)
	}
	if strings.TrimSpace(res.Stdout) != "36" {
		t.Fatalf("want numpy.arange(9).reshape(3,3).sum()==36, got %q", res.Stdout)
	}
}

// boundaryHit reports whether a security-probe Result shows the boundary firing: a
// non-zero exit, or the expected errno/EPERM marker in either stream.
func boundaryHit(res Result, marker string) bool {
	if res.ExitCode != 0 {
		return true
	}
	hay := res.Stdout + "\n" + res.Stderr
	// errno 1 == EPERM; the ptrace/unshare probes print "errno 1" on denial.
	return strings.Contains(hay, "errno "+marker) || strings.Contains(hay, marker)
}
