package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"syscall"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/procgroup"
)

// commandTransport is the go-sdk's CommandTransport (mcp/cmd.go, v1.8.0) with two changes,
// both measured 2026-09-26 against that transport:
//
//   - the child's stdout is read through protocolLines, as a box server's is: a host server
//     that printed one line of npm output before answering lost its session at initialize
//     ("invalid character 'a' looking for beginning of value");
//   - closing ends the child's whole process group, which procgroup.SetProcessGroup made it
//     lead for exactly this reason (D-10): a grandchild the server had forked was still
//     running after the session closed.
//
// CommandTransport runs StdoutPipe itself and hides the connection it builds, so neither
// change fits around it; the shutdown ladder below is its own.
type commandTransport struct {
	cmd    *exec.Cmd
	name   string
	logger *slog.Logger
}

// commandTerminateWait is how long each step of the shutdown ladder waits for the child,
// CommandTransport's default.
const commandTerminateWait = 5 * time.Second

func (t *commandTransport) Connect(ctx context.Context) (sdkmcp.Connection, error) {
	stdout, err := t.cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stdin, err := t.cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	t.cmd.Cancel = func() error { return procgroup.KillProcessGroup(t.cmd) }
	if err := t.cmd.Start(); err != nil {
		return nil, err
	}
	outR, outW := io.Pipe()
	go func() {
		_, err := io.Copy(&protocolLines{dst: outW, name: t.name, logger: t.logger}, stdout)
		_ = outW.CloseWithError(err)
	}()
	stdinCloser := &commandStdin{WriteCloser: stdin, cmd: t.cmd, terminateWait: commandTerminateWait}
	return (&sdkmcp.IOTransport{Reader: outR, Writer: stdinCloser}).Connect(ctx)
}

// commandStdin shuts the server down the way the MCP spec asks of a stdio client: close its
// stdin, wait, SIGTERM, wait, SIGKILL. The kill takes the whole process group, and so does a
// clean exit, since the server's own children outlive it otherwise.
type commandStdin struct {
	io.WriteCloser
	cmd           *exec.Cmd
	terminateWait time.Duration
}

func (w *commandStdin) Close() error {
	closeErr := w.WriteCloser.Close()
	exited := make(chan error, 1)
	go func() { exited <- w.cmd.Wait() }()
	wait := func() (error, bool) {
		select {
		case err := <-exited:
			return err, true
		case <-time.After(w.terminateWait):
			return nil, false
		}
	}
	err, ok := wait()
	if !ok && w.cmd.Process.Signal(syscall.SIGTERM) == nil {
		err, ok = wait()
	}
	_ = procgroup.KillProcessGroup(w.cmd)
	if !ok {
		if err, ok = wait(); !ok {
			err = fmt.Errorf("mcp %s: unresponsive subprocess", w.cmd.Path)
		}
	}
	return errors.Join(closeErr, err)
}
