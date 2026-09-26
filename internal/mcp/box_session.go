package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// box_session.go opens a stdio MCP server that runs INSIDE the calling identity's sandbox
// box instead of as Aura's child. An exec is Aura's only channel into a box, so the server's
// stdin and stdout are the exec's, and the SDK's IOTransport speaks MCP over them — the same
// transport CommandTransport uses underneath, minus the *exec.Cmd a box process does not
// have. Measured before this was written (prd.md §12, spikes/agent-browser-auth/mcpbox):
// handshake under 150 ms, and a suspended box ends the session cleanly within 2.3 s, which
// the bridge's redial then answers with a fresh exec in the resumed box.

// ErrNoBox answers a box server opened by a caller that has no sandbox: a CLI probe, or a
// host where the sandbox is off. It is permanent for that caller, so it deliberately does
// not wrap ErrTransport, and a box server is never run on the host instead.
var ErrNoBox = errors.New("mcp: this server runs in the sandbox box, and this caller has no sandbox")

// BoxLauncher starts a stdio server in the box of the identity ctx carries. stdin feeds the
// server's stdin and its EOF closes it; the server's stdout is copied to stdout as it comes.
// The launcher keeps the server's stderr off stdout, where it would corrupt the framing.
type BoxLauncher interface {
	StartStdio(ctx context.Context, name string, argv, env []string, stdin io.ReadCloser, stdout io.Writer) (BoxProcess, error)
}

// BoxProcess is one server process running in a box.
type BoxProcess interface {
	// Wait blocks until the process has exited and its output is drained.
	Wait() (int, error)
	// Kill ends the process. It never blocks and is safe to call more than once.
	Kill()
	// Touch marks the box as in use, so the idle reaper does not suspend it under a
	// session that is working but opens no new exec.
	Touch()
}

// openSDKBox is OpenSDKSessionForConfig's box branch; the caller has already checked the
// launch shape and opened the connect boundary.
func openSDKBox(processCtx, handshakeCtx context.Context, name string, cfg ServerConfig, o SessionOptions) (*sdkmcp.ClientSession, error) {
	if o.Box == nil {
		return nil, fmt.Errorf("mcp %q: %w", name, ErrNoBox)
	}
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	argv := append([]string{cfg.Command}, cfg.Args...)
	proc, err := o.Box.StartStdio(processCtx, name, argv, cfg.Env, inR, outW)
	if err != nil {
		_ = inW.Close()
		_ = outR.Close()
		return nil, TransportErrorf(name, err)
	}
	exited := make(chan struct{})
	go func() {
		_, _ = proc.Wait()
		// The server is gone: EOF on its stdout is what ends the SDK session.
		_ = outW.Close()
		close(exited)
	}()
	go func() {
		select {
		case <-processCtx.Done():
			proc.Kill()
		case <-exited:
		}
	}()

	o.Sending = append(slices.Clip(o.Sending), touchBoxOnCall(proc))
	transport := &sdkmcp.IOTransport{Reader: outR, Writer: &boxStdin{WriteCloser: inW, proc: proc}}
	client := newSDKClient(o)
	session, err := BoundedCall(handshakeCtx,
		func(ctx context.Context) (*sdkmcp.ClientSession, error) { return client.Connect(ctx, transport, nil) },
		closeSession)
	if err != nil {
		proc.Kill()
		return nil, TransportErrorf(name, err)
	}
	logNegotiatedProtocol(resolveLogger(o.Logger), name, transportLabelBox, session)
	return session, nil
}

// boxStdin closes the server's stdin and then ends the process: EOF alone is a request a
// server may ignore, and nothing else would ever reap it.
type boxStdin struct {
	io.WriteCloser
	proc BoxProcess
}

func (w *boxStdin) Close() error {
	err := w.WriteCloser.Close()
	w.proc.Kill()
	return err
}

// touchBoxOnCall keeps the box awake while its tools are being called. The idle reaper reads
// the box's last use from new execs, and a long-lived server session opens none.
func touchBoxOnCall(proc BoxProcess) sdkmcp.Middleware {
	return func(next sdkmcp.MethodHandler) sdkmcp.MethodHandler {
		return func(ctx context.Context, method string, req sdkmcp.Request) (sdkmcp.Result, error) {
			if method == "tools/call" {
				proc.Touch()
			}
			return next(ctx, method, req)
		}
	}
}
