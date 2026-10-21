// docker_backend_exec_test.go unit-proves the box exec env scrub (T-37-04-SECRETENV) without
// a live dockerd: scrubEnv must drop secret-like KEY=VALUE pairs exactly as the host mergeEnv
// path does (secret.IsSecretEnvVar) so no host secret crosses into an untrusted box. It also
// pins the PID-file kill mechanism's pure shell-string builders (51-09 defect E): the wrapper
// that records a job's PID and the kill snippet that signals it, both daemon-free -- the
// docker-gated cancel/kill behaviour itself is proven live (docker top) by the phase's own
// checkpoint, not re-verified here.

package usersandbox

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moby/moby/client"
)

func TestExecStreamWaitBoundsDockerInspectAfterProcessExit(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		cli := newFakeDockerClient(t, func(request *http.Request) (*http.Response, error) {
			deadline, bounded := request.Context().Deadline()
			if !bounded || time.Until(deadline) > 5*time.Second {
				t.Fatal("process cleanup may wait forever for Docker")
			}
			if unavailable {
				return nil, context.DeadlineExceeded
			}
			return dockerJSONResponse(http.StatusOK, `{"ExitCode":17}`), nil
		})
		done := make(chan struct{})
		close(done)
		handle := &ExecStreamHandle{cli: cli, container: "box", pidFile: "/tmp/job.pid", execID: "finished-login", done: done, killReq: make(chan struct{}), stopDone: make(chan struct{})}
		code, err := handle.Wait()
		if unavailable {
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("Docker timeout was hidden")
			}
		} else if err != nil || code != 17 {
			t.Fatal("lost process exit result")
		}
		_ = cli.Close()
	}
}

func TestExecStreamWaitDoesNotReturnWhileDockerReportsRunning(t *testing.T) {
	calls := 0
	cli := newFakeDockerClient(t, func(*http.Request) (*http.Response, error) {
		calls++
		if calls < 3 {
			return dockerJSONResponse(http.StatusOK, `{"Running":true,"ExitCode":0}`), nil
		}
		return dockerJSONResponse(http.StatusOK, `{"Running":false,"ExitCode":17}`), nil
	})
	defer cli.Close()
	done := make(chan struct{})
	close(done)
	handle := &ExecStreamHandle{cli: cli, execID: "still-running", done: done}
	code, err := handle.Wait()
	if err != nil || code != 17 || calls != 3 {
		t.Fatalf("Wait accepted a running execution: code=%d err=%v inspections=%d", code, err, calls)
	}
}

func TestExecStreamKillRemainsNonblockingAndIdempotent(t *testing.T) {
	release := make(chan struct{})
	cli := newFakeDockerClient(t, func(*http.Request) (*http.Response, error) { <-release; return nil, context.DeadlineExceeded })
	handle := &ExecStreamHandle{cli: cli, container: "box", pidFile: "/tmp/job.pid", execID: "blocked", killReq: make(chan struct{}), done: make(chan struct{}), stopDone: make(chan struct{})}
	t.Cleanup(func() { close(release); <-handle.stopDone; _ = cli.Close() })
	returned := make(chan struct{})
	go func() { handle.Kill(); handle.Kill(); close(returned) }()
	select {
	case <-returned:
	case <-time.After(time.Second):
		close(handle.done)
		t.Fatal("Kill waited for exit instead of signaling the pump")
	}
	select {
	case <-handle.killReq:
	default:
		t.Fatal("Kill did not signal termination")
	}
}

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
		wantPrefix := "echo $$ > '/tmp/.aura-exec-deadbeef.pid' || exit 1; "
		if !strings.HasPrefix(got, wantPrefix) {
			t.Fatalf("wrapCommandWithPIDFile(%q) = %q, want it to start with %q", cmd, got, wantPrefix)
		}
		if !strings.HasSuffix(got, cmd) {
			t.Fatalf("wrapCommandWithPIDFile(%q) = %q, want cmd appended verbatim (no `exec` prefix)", cmd, got)
		}
		if !strings.Contains(got, "trap 'rm -f /tmp/.aura-exec-deadbeef.pid' EXIT; ") {
			t.Fatalf("wrapCommandWithPIDFile(%q) = %q, want an EXIT trap removing the PID file so a long-lived box does not accumulate one per call", cmd, got)
		}
	}
}

// TestKillJobTreeCommand_ReachesTheChildrenNotJustTheGroup pins the snippet killBoxProcessGroup
// runs in a separate box exec. The properties are not cosmetic — each one is a measured failure:
//
//   - it must read the SAME pidFile wrapCommandWithPIDFile wrote;
//   - it must walk /proc for DESCENDANTS, because the job's children get their own process group
//     (measured: wrapper pid=106 pgid=106, its `sleep` pid=149 pgid=149) and the group signal
//     alone killed the wrapper while the work ran on;
//   - it must collect the tree BEFORE signalling, since a child reparents the moment its parent
//     dies and a walk done afterwards has already lost the links;
//   - it must still signal the group, which is free and catches the ordinary case;
//   - it must never fail its own exit when the PID file is gone (a job that already exited).
func TestKillJobTreeCommand_ReachesTheChildrenNotJustTheGroup(t *testing.T) {
	got := killJobTreeCommand("/tmp/.aura-exec-deadbeef.pid")
	for _, want := range []string{
		"cat '/tmp/.aura-exec-deadbeef.pid'",
		"/proc/[0-9]*",      // the descendant walk
		"PPid:",             // read from /proc/<pid>/status, not ps (absent in the box image)
		`kill -TERM "$pid"`, // every collected descendant
		`kill -TERM -"$P"`,  // and still the group
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("killJobTreeCommand = %q, want it to contain %q", got, want)
		}
	}
	// Collection must precede the first signal, or the tree is already wrong when it is read.
	if strings.Index(got, "PPid:") > strings.Index(got, "kill -TERM") {
		t.Fatalf("killJobTreeCommand signals before it finishes walking the tree: %q", got)
	}
	if !strings.Contains(got, `[ -n "$P" ] || exit 0`) {
		t.Fatal("an already-finished job must be harmless")
	}
}

func TestWaitExecExitIsBoundedAndPreservesUnconfirmedState(t *testing.T) {
	cli := newFakeDockerClient(t, func(*http.Request) (*http.Response, error) {
		return dockerJSONResponse(http.StatusOK, `{"Running":true,"ExitCode":0}`), nil
	})
	defer cli.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	_, err := waitExecExit(ctx, cli, "running", nil)
	if !errors.Is(err, ErrExecUnconfirmed) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unconfirmed running execution=%v", err)
	}
}

func TestExecAttachFailureAttemptsTerminationAndPreservesUnconfirmedExit(t *testing.T) {
	for _, stream := range []bool{false, true} {
		var creates atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if strings.HasSuffix(r.URL.Path, "/containers/box/exec") {
				id := "job"
				if creates.Add(1) > 1 {
					id = "terminate"
				}
				w.WriteHeader(201)
				_, _ = io.WriteString(w, `{"Id":"`+id+`"}`)
				return
			}
			w.WriteHeader(500)
			_, _ = io.WriteString(w, `{"message":"attach transport failed"}`)
		}))
		cli, err := client.New(client.WithHost("tcp://"+strings.TrimPrefix(server.URL, "http://")), client.WithAPIVersion("1.51"))
		if err != nil {
			t.Fatal(err)
		}
		backend := NewDockerBackend(cli, "unused", Resources{})
		if stream {
			_, err = backend.ExecStream(t.Context(), BoxHandle{ContainerID: "box"}, ExecRequest{Command: "sleep 30"}, nil, io.Discard)
		} else {
			_, err = backend.Exec(t.Context(), BoxHandle{ContainerID: "box"}, ExecRequest{Command: "sleep 30"})
		}
		_ = cli.Close()
		server.Close()
		if !errors.Is(err, ErrExecUnconfirmed) || creates.Load() != 2 {
			t.Fatalf("stream=%v err=%v created=%d", stream, err, creates.Load())
		}
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
