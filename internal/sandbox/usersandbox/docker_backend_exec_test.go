// docker_backend_exec_test.go unit-proves the box exec env scrub (T-37-04-SECRETENV) without
// a live dockerd: scrubEnv must drop secret-like KEY=VALUE pairs exactly as the host mergeEnv
// path does (secret.IsSecretEnvVar) so no host secret crosses into an untrusted box. It also
// pins the PID-file kill mechanism's pure shell-string builders (51-09 defect E): the wrapper
// that records a job's PID and the kill snippet that signals it, both daemon-free -- the
// docker-gated cancel/kill behaviour itself is proven live (docker top) by the phase's own
// checkpoint, not re-verified here.

package usersandbox

import (
	"slices"
	"strings"
	"testing"
)

// TestWrapCommandWithPIDFile_RecordsPIDBeforeRunningCmd pins the shape ExecStream's background
// jobs and Exec's synchronous cancel/timeout path both depend on: the wrapper records $$ to
// pidFile BEFORE cmd runs, and cmd itself travels through completely unmodified -- no `exec`
// prefix, so a compound line / builtin / pipeline still runs as a child of the wrapper shell.
func TestWrapCommandWithPIDFile_RecordsPIDBeforeRunningCmd(t *testing.T) {
	for _, cmd := range []string{
		"echo hi",
		"cd /workspace && ls -la | grep foo",
		"sleep 480 && echo STALL-PROBE-DONE",
	} {
		got := wrapCommandWithPIDFile("/tmp/.aura-exec-deadbeef.pid", cmd)
		wantPrefix := "echo $$ > '/tmp/.aura-exec-deadbeef.pid'; "
		if !strings.HasPrefix(got, wantPrefix) {
			t.Fatalf("wrapCommandWithPIDFile(%q) = %q, want it to start with %q", cmd, got, wantPrefix)
		}
		if !strings.HasSuffix(got, cmd) {
			t.Fatalf("wrapCommandWithPIDFile(%q) = %q, want cmd appended verbatim (no `exec` prefix)", cmd, got)
		}
	}
}

// TestKillProcessGroupCommand_SignalsBothGroupAndProcess pins the kill snippet
// killBoxProcessGroup runs in a separate box exec: it must read the SAME pidFile
// wrapCommandWithPIDFile wrote, signal the process GROUP (negative PID) and the process itself,
// and never fail its own exit even when the PID file is gone (a job that already exited).
func TestKillProcessGroupCommand_SignalsBothGroupAndProcess(t *testing.T) {
	got := killProcessGroupCommand("/tmp/.aura-exec-deadbeef.pid")
	for _, want := range []string{
		"cat '/tmp/.aura-exec-deadbeef.pid'",
		`kill -TERM -"$P"`, // the process group
		`kill -TERM "$P"`,  // the process itself
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("killProcessGroupCommand = %q, want it to contain %q", got, want)
		}
	}
	if !strings.HasSuffix(strings.TrimSpace(got), "true") {
		t.Fatalf("killProcessGroupCommand = %q, want it to end clean (`; true`) so a vanished PID file is not a kill-exec failure", got)
	}
}

func TestExec_ScrubsSecretEnv(t *testing.T) {
	in := []string{
		"PATH=/usr/local/bin:/usr/bin",
		"HOME=/root",
		"PUBLIC_URL=https://example.com",
		"OPENROUTER_API_KEY=sk-secret",
		"AWS_SECRET_ACCESS_KEY=abc123",
		"GITHUB_TOKEN=ghp_xxx",
		"DB_PASSWORD=hunter2",
		"SESSION_ID=deadbeef",
		"DATABASE_URL=postgres://user:pass@host:5432/db",
		"MALFORMED_NO_EQUALS",
	}
	got := scrubEnv(in)

	wantKept := []string{
		"PATH=/usr/local/bin:/usr/bin",
		"HOME=/root",
		"PUBLIC_URL=https://example.com",
	}
	for _, kv := range wantKept {
		if !slices.Contains(got, kv) {
			t.Errorf("scrubEnv dropped non-secret %q; got %v", kv, got)
		}
	}

	wantDropped := []string{
		"OPENROUTER_API_KEY=sk-secret",
		"AWS_SECRET_ACCESS_KEY=abc123",
		"GITHUB_TOKEN=ghp_xxx",
		"DB_PASSWORD=hunter2",
		"SESSION_ID=deadbeef",
		"DATABASE_URL=postgres://user:pass@host:5432/db",
		"MALFORMED_NO_EQUALS",
	}
	for _, kv := range wantDropped {
		if slices.Contains(got, kv) {
			t.Errorf("scrubEnv leaked secret/invalid entry %q; got %v", kv, got)
		}
	}
}
