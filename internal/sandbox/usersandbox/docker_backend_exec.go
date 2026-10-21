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
	"log/slog"
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
		_, stopErr := killBoxProcessGroup(b.cli, h.ContainerID, pidFile, ec.ID)
		return ExecResult{}, errors.Join(fmt.Errorf("box exec attach: %w", err), stopErr)
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
		_, stopErr := killBoxProcessGroup(b.cli, h.ContainerID, pidFile, ec.ID)
		att.Close()
		<-copyDone
		return ExecResult{}, errors.Join(ctx.Err(), stopErr)
	case cerr := <-copyDone:
		if cerr != nil && !errors.Is(cerr, io.EOF) {
			return ExecResult{}, fmt.Errorf("box exec stream: %w", cerr)
		}
	}

	code, err := waitExecExit(ctx, b.cli, ec.ID, ctx.Done())
	if ctx.Err() != nil {
		_, stopErr := killBoxProcessGroup(b.cli, h.ContainerID, pidFile, ec.ID)
		return ExecResult{}, errors.Join(ctx.Err(), stopErr)
	}
	if err != nil {
		return ExecResult{}, err
	}
	return ExecResult{Stdout: outBuf.Bytes(), Stderr: errBuf.Bytes(), ExitCode: code}, nil
}

// ExecStreamHandle is a live streamed/detached box exec backing a BACKGROUND shell job (37-09).
// It streams the job's combined stdout/stderr into the writer supplied at start, blocks for the
// exit code via ExecInspect (Wait), and terminates the job tree with bounded TERM/KILL escalation
// (Kill). It is a CONCRETE DockerBackend capability, NOT a 6th Backend verb — D-02 locks the
// Backend seam at Resolve/Exec/Suspend/Resume/Stop so the DGX agent-sandbox E2B impl drops in
// unmodified; the router surfaces this streaming verb structurally (router_tools.go), never through
// the Backend interface. It owns every goroutine it starts (goleak-clean).
type ExecStreamHandle struct {
	cli       *client.Client
	execID    string
	container string
	pidFile   string
	attach    client.ExecAttachResult
	in        io.ReadCloser

	lifecycleMu sync.Mutex
	killing     bool
	completed   bool
	waitOnce    sync.Once
	waitCode    int
	waitErr     error

	killReq  chan struct{}
	done     chan struct{}
	stopDone chan struct{}
	stopCode int
	stopErr  error
}

// ExecStream starts req inside the box as a streamed/detached exec and returns a handle. The
// wrapper shell's PID is recorded to a per-job file BEFORE the command runs so Kill can signal the
// whole job process group from a SEPARATE box exec — the Docker exec API has no kill-exec verb, and
// detaching the stream alone leaves the process running (37-RESEARCH A5). Output is demuxed into
// out as the box produces it; the env is secret-scrubbed exactly as Exec.
//
// in, when non-nil, becomes the job's stdin: its bytes reach the box process as they are read,
// and its EOF closes that stdin. The handle closes in when the job ends, so a reader parked on a
// pipe nobody writes to cannot outlive it. The browser live view relays input events this way,
// because the viewport stream is loopback inside the box netns and exec is Aura's only channel
// in (prd.md §12).
func (b *DockerBackend) ExecStream(ctx context.Context, h BoxHandle, req ExecRequest, in io.ReadCloser, out io.Writer) (*ExecStreamHandle, error) {
	token, err := randHexToken()
	if err != nil {
		return nil, err
	}
	pidFile := "/tmp/.aura-bg-" + token + ".pid"
	ec, err := b.cli.ExecCreate(ctx, h.ContainerID, client.ExecCreateOptions{
		Cmd:          []string{"/bin/sh", "-c", wrapCommandWithPIDFile(pidFile, req.Command)},
		WorkingDir:   req.Dir,
		Env:          scrubEnv(req.Env),
		AttachStdin:  in != nil,
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return nil, fmt.Errorf("box bg exec create: %w", err)
	}
	att, err := b.cli.ExecAttach(ctx, ec.ID, client.ExecAttachOptions{})
	if err != nil {
		_, stopErr := killBoxProcessGroup(b.cli, h.ContainerID, pidFile, ec.ID)
		return nil, errors.Join(fmt.Errorf("box bg exec attach: %w", err), stopErr)
	}
	s := &ExecStreamHandle{
		cli:       b.cli,
		execID:    ec.ID,
		container: h.ContainerID,
		pidFile:   pidFile,
		attach:    att,
		in:        in,
		killReq:   make(chan struct{}),
		done:      make(chan struct{}),
		stopDone:  make(chan struct{}),
	}
	go s.pump(out)
	return s, nil
}

// pump joins output/input copying on EOF or after the bounded termination worker settles.
// Wait separately confirms remote execution exit: stream EOF alone is not that proof.
func (s *ExecStreamHandle) pump(out io.Writer) {
	defer close(s.done)
	feedDone := s.feedStdin()
	copyDone := make(chan struct{})
	go func() {
		_, _ = stdcopy.StdCopy(out, out, s.attach.Reader)
		close(copyDone)
	}()
	select {
	case <-copyDone:
		s.attach.Close()
	case <-s.killReq:
		<-s.stopDone
		s.attach.Close() // unblock the StdCopy read so the copy goroutine exits
		<-copyDone
	}
	if s.in != nil {
		_ = s.in.Close() // unblock a feeder parked on a read nobody will satisfy
	}
	<-feedDone
}

// feedStdin copies in to the job's stdin and half-closes it on EOF, so a box process reading
// until EOF sees the end of input. Without in it returns an already-closed channel.
func (s *ExecStreamHandle) feedStdin() <-chan struct{} {
	done := make(chan struct{})
	if s.in == nil {
		close(done)
		return done
	}
	go func() {
		defer close(done)
		_, _ = io.Copy(s.attach.Conn, s.in)
		_ = s.attach.CloseWrite()
	}()
	return done
}

// wrapCommandWithPIDFile prefixes cmd with a shell snippet that records the wrapper shell's own
// PID to pidFile before cmd runs -- the ONE PID-file kill mechanism this package uses, shared by
// ExecStream's background jobs and Exec's synchronous cancel/timeout path (defect E, above). No
// `exec` prefix: the command may be a compound line / builtin / pipeline, so it runs as a child of
// this wrapper sh. $$ is the wrapper's PID and, runc starting each exec as a session leader, its
// process-group id too.
//
// What that does NOT give you is the job: measured in a real box on 2026-09-06, the wrapper sat
// at pid=106 pgid=106 while the command it ran sat at pid=149 pgid=149 — its OWN group. Signalling
// the wrapper's group therefore killed the wrapper and left the work running. The PID recorded
// here is consequently the ROOT OF A TREE, and killJobTreeCommand walks it; it is not a
// process-group handle, and reading it as one is the mistake this comment used to make.
//
// Refuse untracked execution if the PID file cannot be written. On TERM the wrapper waits
// for its foreground child so escalation can kill the child without orphaning it, then
// exits instead of continuing a compound command. EXIT removes the tracking file.
func wrapCommandWithPIDFile(pidFile, cmd string) string {
	return "echo $$ > '" + pidFile + "' || exit 1; trap 'rm -f " + pidFile + "' EXIT; trap 'wait; exit 143' TERM; " + cmd
}

// Kill requests bounded termination once, without waiting under the caller's registry lock.
// Confirmed completion and termination admission share one lock. No Docker I/O or
// worker join happens under it, so callers may still hold their registry lock.
func (s *ExecStreamHandle) Kill() {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if !s.completed && !s.killing {
		s.startKillLocked()
	}
}

func (s *ExecStreamHandle) startKillLocked() {
	s.killing = true
	close(s.killReq)
	go func() {
		defer close(s.stopDone)
		s.stopCode, s.stopErr = killBoxProcessGroup(s.cli, s.container, s.pidFile, s.execID)
		if s.stopErr != nil {
			slog.Warn("sandbox job exit unconfirmed", "container", s.container, "exec", s.execID, "err", s.stopErr)
		}
	}()
}

// Wait joins stream and termination work and returns a confirmed remote exit code.
// ErrExecUnconfirmed is distinct from both a confirmed nonzero exit and cancellation.
func (s *ExecStreamHandle) Wait() (int, error) {
	s.waitOnce.Do(func() { s.waitCode, s.waitErr = s.wait() })
	return s.waitCode, s.waitErr
}

func (s *ExecStreamHandle) wait() (int, error) {
	<-s.done
	// Stream EOF does not prove process exit. Inspection and escalation stay bounded.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	code, err := waitExecExit(ctx, s.cli, s.execID, s.killReq)
	s.lifecycleMu.Lock()
	if err == nil && !s.killing {
		s.completed = true
		s.lifecycleMu.Unlock()
		return code, nil
	}
	// Kill may have won while the final successful ExecInspect was in flight.
	// Its worker remains owned by Wait, including any unconfirmed-exit result.
	if !s.killing {
		s.startKillLocked()
	}
	s.lifecycleMu.Unlock()
	<-s.stopDone
	if s.stopErr != nil {
		return s.stopCode, errors.Join(err, s.stopErr)
	}
	return s.stopCode, s.stopErr
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
