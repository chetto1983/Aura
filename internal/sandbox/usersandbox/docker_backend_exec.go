// docker_backend_exec.go implements the Backend.Exec verb: it runs a single command inside
// the box through POSIX /bin/sh (never the host's Windows shell — 37-RESEARCH Pitfall 6),
// demuxes the multiplexed exec stream with stdcopy, and reads the exit code via ExecInspect.
// The box exec env is scrubbed of secret-like vars EXACTLY as the host mergeEnv path does
// (secret.IsSecretEnvVar) so no host secret ever crosses into an untrusted per-identity box.

package usersandbox

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/chetto1983/aura/internal/secret"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"
)

// Ensure DockerBackend satisfies the Backend seam at compile time (completed by Exec here).
var _ Backend = (*DockerBackend)(nil)

// Exec runs req.Command inside the box via /bin/sh -c, returning the demuxed stdout/stderr
// and the process exit code. The env is secret-scrubbed. It honors ctx cancellation: on
// cancel/timeout it signals the box-side process group (the SAME PID-file mechanism
// ExecStream/Kill uses, below -- one kill implementation, not two) before closing the
// hijacked stream, then waits for the demux goroutine to exit (goleak-clean).
//
// Before this fix, cancel/timeout only closed the stream and returned: the box-side
// process kept running with nothing left to observe it. Measured live 2026-08-29
// (live-check/d03/RESULTS.md defect E): a staleness-reaped `shell_exec sleep 480` was
// still alive in `docker top` almost three minutes after its reap, next to the retried
// attempt's own copy of the same command -- N concurrent copies of one command in one
// box under the shipped delegation retry policy.
func (b *DockerBackend) Exec(ctx context.Context, h BoxHandle, req ExecRequest) (ExecResult, error) {
	token, err := randHexToken()
	if err != nil {
		return ExecResult{}, fmt.Errorf("box exec pid token: %w", err)
	}
	pidFile := "/tmp/.aura-exec-" + token + ".pid"
	ec, err := b.cli.ExecCreate(ctx, h.ContainerID, client.ExecCreateOptions{
		Cmd:          []string{"/bin/sh", "-c", wrapCommandWithPIDFile(pidFile, req.Command)},
		WorkingDir:   req.Dir,
		Env:          scrubEnv(req.Env),
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return ExecResult{}, fmt.Errorf("box exec create: %w", err)
	}
	att, err := b.cli.ExecAttach(ctx, ec.ID, client.ExecAttachOptions{})
	if err != nil {
		return ExecResult{}, fmt.Errorf("box exec attach: %w", err)
	}
	defer att.Close()

	var outBuf, errBuf bytes.Buffer
	copyDone := make(chan error, 1)
	go func() {
		_, cerr := stdcopy.StdCopy(&outBuf, &errBuf, att.Reader)
		copyDone <- cerr
	}()

	select {
	case <-ctx.Done():
		// Signal the box-side process group BEFORE detaching -- detaching alone (the
		// pre-fix behaviour) leaves the process running with no handle left to kill it.
		killBoxProcessGroup(b.cli, h.ContainerID, pidFile)
		att.Close()
		<-copyDone
		return ExecResult{}, ctx.Err()
	case cerr := <-copyDone:
		if cerr != nil && !errors.Is(cerr, io.EOF) {
			return ExecResult{}, fmt.Errorf("box exec stream: %w", cerr)
		}
	}

	ins, err := b.cli.ExecInspect(ctx, ec.ID, client.ExecInspectOptions{})
	if err != nil {
		return ExecResult{}, fmt.Errorf("box exec inspect: %w", err)
	}
	return ExecResult{Stdout: outBuf.Bytes(), Stderr: errBuf.Bytes(), ExitCode: ins.ExitCode}, nil
}

// ExecStreamHandle is a live streamed/detached box exec backing a BACKGROUND shell job (37-09).
// It streams the job's combined stdout/stderr into the writer supplied at start, blocks for the
// exit code via ExecInspect (Wait), and terminates the job with a box-side SIGTERM to its process
// group (Kill). It is a CONCRETE DockerBackend capability, NOT a 6th Backend verb — D-02 locks the
// Backend seam at Resolve/Exec/Suspend/Resume/Stop so the DGX agent-sandbox E2B impl drops in
// unmodified; the router surfaces this streaming verb structurally (router_tools.go), never through
// the Backend interface. It owns every goroutine it starts (goleak-clean).
type ExecStreamHandle struct {
	cli       *client.Client
	execID    string
	container string
	pidFile   string
	attach    client.ExecAttachResult

	killOnce sync.Once
	killReq  chan struct{}
	done     chan struct{}
}

// ExecStream starts req inside the box as a streamed/detached exec and returns a handle. The
// wrapper shell's PID is recorded to a per-job file BEFORE the command runs so Kill can signal the
// whole job process group from a SEPARATE box exec — the Docker exec API has no kill-exec verb, and
// detaching the stream alone leaves the process running (37-RESEARCH A5). Output is demuxed into
// out as the box produces it; the env is secret-scrubbed exactly as Exec.
func (b *DockerBackend) ExecStream(ctx context.Context, h BoxHandle, req ExecRequest, out io.Writer) (*ExecStreamHandle, error) {
	token, err := randHexToken()
	if err != nil {
		return nil, err
	}
	pidFile := "/tmp/.aura-bg-" + token + ".pid"
	ec, err := b.cli.ExecCreate(ctx, h.ContainerID, client.ExecCreateOptions{
		Cmd:          []string{"/bin/sh", "-c", wrapCommandWithPIDFile(pidFile, req.Command)},
		WorkingDir:   req.Dir,
		Env:          scrubEnv(req.Env),
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return nil, fmt.Errorf("box bg exec create: %w", err)
	}
	att, err := b.cli.ExecAttach(ctx, ec.ID, client.ExecAttachOptions{})
	if err != nil {
		return nil, fmt.Errorf("box bg exec attach: %w", err)
	}
	s := &ExecStreamHandle{
		cli:       b.cli,
		execID:    ec.ID,
		container: h.ContainerID,
		pidFile:   pidFile,
		attach:    att,
		killReq:   make(chan struct{}),
		done:      make(chan struct{}),
	}
	go s.pump(out)
	return s, nil
}

// pump demuxes the box exec stream into out until the job exits on its own (stream EOF) or Kill
// requests termination — in which case it signals the box-side process group, detaches to unblock
// the copy, and joins the copy goroutine. It closes done on return (Wait's completion signal).
func (s *ExecStreamHandle) pump(out io.Writer) {
	defer close(s.done)
	copyDone := make(chan struct{})
	go func() {
		_, _ = stdcopy.StdCopy(out, out, s.attach.Reader)
		close(copyDone)
	}()
	select {
	case <-copyDone:
		s.attach.Close()
	case <-s.killReq:
		s.sigterm()
		s.attach.Close() // unblock the StdCopy read so the copy goroutine exits
		<-copyDone
	}
}

// sigterm signals the job's process group from a separate short-lived box exec. Runs in the
// pump goroutine, so Kill itself stays non-blocking.
func (s *ExecStreamHandle) sigterm() {
	killBoxProcessGroup(s.cli, s.container, s.pidFile)
}

// wrapCommandWithPIDFile prefixes cmd with a shell snippet that records the wrapper shell's own
// PID to pidFile before cmd runs -- the ONE PID-file kill mechanism this package uses, shared by
// ExecStream's background jobs and Exec's synchronous cancel/timeout path (defect E, above). No
// `exec` prefix: the command may be a compound line / builtin / pipeline, so it runs as a child of
// this wrapper sh. $$ is the wrapper's PID; runc starts each exec as a session leader, so $$ is
// also the process-group id killBoxProcessGroup signals to terminate the whole job tree.
func wrapCommandWithPIDFile(pidFile, cmd string) string {
	return "echo $$ > '" + pidFile + "'; " + cmd
}

// killProcessGroupCommand is the pure shell snippet killBoxProcessGroup runs in a separate,
// short-lived box exec: read the PID wrapCommandWithPIDFile recorded and signal both the process
// group (the negative PID) and the process itself, best-effort. A job that already exited leaves
// no PID to signal, so the trailing `; true` keeps the kill exec's own exit clean either way.
func killProcessGroupCommand(pidFile string) string {
	return "P=$(cat '" + pidFile + "' 2>/dev/null); [ -n \"$P\" ] && { kill -TERM -\"$P\" 2>/dev/null; kill -TERM \"$P\" 2>/dev/null; }; true"
}

// killBoxProcessGroup signals the process group recorded in pidFile from a separate, short-lived
// box exec -- the Docker exec API has no kill-exec verb, and closing/detaching the original exec's
// hijacked stream alone leaves the box-side process running (37-RESEARCH A5; defect E, above).
// Best-effort and silent on failure: a box that is already gone, or a job that already exited,
// leaves nothing to signal and nothing to report.
func killBoxProcessGroup(cli *client.Client, containerID, pidFile string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ec, err := cli.ExecCreate(ctx, containerID, client.ExecCreateOptions{Cmd: []string{"/bin/sh", "-c", killProcessGroupCommand(pidFile)}})
	if err != nil {
		return
	}
	att, err := cli.ExecAttach(ctx, ec.ID, client.ExecAttachOptions{})
	if err != nil {
		return
	}
	_, _ = io.Copy(io.Discard, att.Reader) // run the kill exec to completion
	att.Close()
}

// Kill requests termination of the job (non-blocking, once-guarded): the actual box-side SIGTERM
// runs in the pump goroutine, so Kill is safe to call while holding the registry lock.
func (s *ExecStreamHandle) Kill() {
	s.killOnce.Do(func() { close(s.killReq) })
}

// Wait blocks until the job's stream ends, then returns the box exit code via ExecInspect. err is
// a non-nil INFRA failure (inspect failed), distinct from a non-zero exit code.
func (s *ExecStreamHandle) Wait() (int, error) {
	<-s.done
	ins, err := s.cli.ExecInspect(context.Background(), s.execID, client.ExecInspectOptions{})
	if err != nil {
		return 0, fmt.Errorf("box bg exec inspect: %w", err)
	}
	return ins.ExitCode, nil
}

// randHexToken mints a short unguessable hex token for the per-job PID file name.
func randHexToken() (string, error) {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("box bg token: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

// scrubEnv drops secret-like KEY=VALUE entries from the box exec env using the SAME predicate
// the host shell_exec mergeEnv path uses (secret.IsSecretEnvVar) — the one canonical denylist
// (envkey.go), so a var redacted on the host path can never leak on the box path (B-09).
func scrubEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || secret.IsSecretEnvVar(k, v) {
			continue
		}
		out = append(out, kv)
	}
	return out
}
