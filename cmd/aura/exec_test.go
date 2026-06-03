package main

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestParseExecArgs_Python(t *testing.T) {
	ea, err := parseExecArgs([]string{"python", "print(2+2)"})
	if err != nil {
		t.Fatalf("parseExecArgs: %v", err)
	}
	if ea.lang != "python" || ea.code != "print(2+2)" || ea.codeStdin || ea.session != "" {
		t.Fatalf("parse mismatch: %+v", ea)
	}
}

func TestParseExecArgs_ShellAndStdin(t *testing.T) {
	ea, err := parseExecArgs([]string{"shell", "-"})
	if err != nil {
		t.Fatalf("parseExecArgs: %v", err)
	}
	if ea.lang != "shell" || !ea.codeStdin || ea.code != "" {
		t.Fatalf("the - form must set codeStdin, got %+v", ea)
	}
}

func TestParseExecArgs_LangEnumRejected(t *testing.T) {
	if _, err := parseExecArgs([]string{"ruby", "puts 1"}); err == nil {
		t.Fatal("lang ruby must be rejected")
	}
}

func TestParseExecArgs_MissingCode(t *testing.T) {
	if _, err := parseExecArgs([]string{"python"}); err == nil {
		t.Fatal("a missing code positional must be an error")
	}
}

func TestParseExecArgs_SessionParsed(t *testing.T) {
	ea, err := parseExecArgs([]string{"--session", "abc", "python", "x"})
	if err != nil {
		t.Fatalf("parseExecArgs: %v", err)
	}
	if ea.session != "abc" || ea.lang != "python" || ea.code != "x" {
		t.Fatalf("--session must be parsed and routed to the session runner by runExec, got %+v", ea)
	}
}

func TestParseExecArgs_SessionMissingValue(t *testing.T) {
	if _, err := parseExecArgs([]string{"--session"}); err == nil {
		t.Fatal("--session with no value must error")
	}
}

// TestRunExec_Exit70 re-execs this test binary so runExec's os.Exit(70) path is
// covered without killing the test process (the agent_test.go re-exec pattern). A
// dead loopback URL with docker absent on PATH skips the auto-start, so the POST
// fails into a wrapped ErrSandboxUnreachable → exit 70 (D-19).
func TestRunExec_Exit70(t *testing.T) {
	if os.Getenv("AURA_TEST_RUNEXEC_EXIT") != "" {
		runExec(strings.Split(os.Getenv("AURA_TEST_RUNEXEC_ARGS"), "|"))
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run", "TestRunExec_Exit70") //nolint:gosec // re-exec self
	cmd.Env = append(os.Environ(),
		"AURA_TEST_RUNEXEC_EXIT=1",
		"AURA_TEST_RUNEXEC_ARGS=python|print(1)",
		"AURA_SANDBOX_URL=http://127.0.0.1:1", // dead loopback port
		"PATH=",                               // exec.LookPath("docker") fails → autoStart is a no-op
	)
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("want a non-zero ExitError, got %v", err)
	}
	if ee.ExitCode() != exitUnreachable {
		t.Fatalf("want exit %d on ErrSandboxUnreachable, got %d", exitUnreachable, ee.ExitCode())
	}
}

// TestRunExec_SessionLive supersedes the 2a "reserved --session" test: in 08-08 the
// inert-reject is gone and --session routes LIVE through the session-bound runner
// (RunPythonSession). With a dead loopback URL and docker absent on PATH, the
// session POST fails into a wrapped ErrSandboxUnreachable → exit 70 — proving the
// flag now drives the session runner (an exit 64 would mean it was still rejected as
// a usage error).
func TestRunExec_SessionLive(t *testing.T) {
	if os.Getenv("AURA_TEST_RUNEXEC_EXIT") != "" {
		runExec(strings.Split(os.Getenv("AURA_TEST_RUNEXEC_ARGS"), "|"))
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run", "TestRunExec_SessionLive") //nolint:gosec // re-exec self
	cmd.Env = append(os.Environ(),
		"AURA_TEST_RUNEXEC_EXIT=1",
		"AURA_TEST_RUNEXEC_ARGS=--session|s1|python|print(1)",
		"AURA_SANDBOX_URL=http://127.0.0.1:1", // dead loopback port
		"PATH=",                               // exec.LookPath("docker") fails → autoStart is a no-op
	)
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("want a non-zero ExitError, got %v", err)
	}
	if ee.ExitCode() != exitUnreachable {
		t.Fatalf("want exit %d (session runner reached a dead sidecar), got %d", exitUnreachable, ee.ExitCode())
	}
}
