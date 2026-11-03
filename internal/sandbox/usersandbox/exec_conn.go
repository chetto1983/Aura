package usersandbox

import (
	"context"
	"io"
	"net"
	"sync"
)

type execConnProcess interface {
	Kill()
	Wait() (int, error)
}

type execConn struct {
	net.Conn
	remote  net.Conn
	process execConnProcess
	once    sync.Once
}

// DialExec reaches a loopback-only service without granting network ingress to a box.
func (r *SandboxRouter) DialExec(ctx context.Context, h BoxHandle, command string) (net.Conn, error) {
	return dialExecConn(ctx, func(in io.ReadCloser, out io.Writer) (execConnProcess, error) {
		return r.ExecStream(ctx, h, ExecRequest{Command: command, Dir: "/workspace"}, in, out)
	})
}

func dialExecConn(ctx context.Context, start func(io.ReadCloser, io.Writer) (execConnProcess, error)) (net.Conn, error) {
	local, remote := net.Pipe()
	process, err := start(remote, remote)
	if err != nil {
		_ = local.Close()
		_ = remote.Close()
		return nil, err
	}
	c := &execConn{Conn: local, remote: remote, process: process}
	stop := context.AfterFunc(ctx, func() { _ = c.Close() })
	go func() { _, _ = process.Wait(); stop(); _ = remote.Close() }()
	return c, nil
}

func (c *execConn) Close() error {
	c.once.Do(func() { c.process.Kill(); _ = c.Conn.Close(); _ = c.remote.Close() })
	return nil
}
