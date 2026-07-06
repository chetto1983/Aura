// docker_backend_exec.go implements the Backend.Exec verb: it runs a single command inside
// the box through POSIX /bin/sh (never the host's Windows shell — 37-RESEARCH Pitfall 6),
// demuxes the multiplexed exec stream with stdcopy, and reads the exit code via ExecInspect.
// The box exec env is scrubbed of secret-like vars EXACTLY as the host mergeEnv path does
// (secret.IsSecretEnvVar) so no host secret ever crosses into an untrusted per-identity box.

package usersandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/chetto1983/aura/internal/secret"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"
)

// Ensure DockerBackend satisfies the Backend seam at compile time (completed by Exec here).
var _ Backend = (*DockerBackend)(nil)

// Exec runs req.Command inside the box via /bin/sh -c, returning the demuxed stdout/stderr
// and the process exit code. The env is secret-scrubbed. It honors ctx cancellation: on
// cancel it closes the hijacked stream and waits for the demux goroutine to exit (goleak-
// clean) before returning ctx.Err().
func (b *DockerBackend) Exec(ctx context.Context, h BoxHandle, req ExecRequest) (ExecResult, error) {
	ec, err := b.cli.ExecCreate(ctx, h.ContainerID, client.ExecCreateOptions{
		Cmd:          []string{"/bin/sh", "-c", req.Command},
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
		// Unblock the demux read, then wait for the goroutine to exit (no leak).
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
