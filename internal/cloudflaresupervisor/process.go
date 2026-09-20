package cloudflaresupervisor

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// ProcessLauncher launches only the packaged binary; overrides support synthetic tests.
type ProcessLauncher struct {
	Binary, TempDir string
	StopTimeout     time.Duration
}

// Start isolates the credential snapshot, environment and diagnostics of each child.
func (l ProcessLauncher) Start(tokenPath string) (Child, error) {
	// Pin each child to an immutable private copy: a later projection must not
	// change which credential an already-starting process reads.
	in, err := os.Open(tokenPath) // #nosec G304 -- supervisor supplies a fixed projection path, never request input.
	if err != nil {
		return nil, errors.New("token unavailable")
	}
	defer func() { _ = in.Close() }()
	token, err := os.CreateTemp(l.TempDir, "aura-connector-*")
	if err != nil {
		return nil, errors.New("token snapshot failed")
	}
	remove := true
	defer func() {
		_ = token.Close()
		if remove {
			_ = os.Remove(token.Name())
		}
	}()
	n, err := io.Copy(token, io.LimitReader(in, 64*1024+1))
	if err != nil || n == 0 || n > 64*1024 {
		return nil, errors.New("token snapshot failed")
	}
	if err := token.Close(); err != nil {
		return nil, errors.New("token snapshot failed")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, errors.New("metrics port unavailable")
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		return nil, errors.New("metrics port unavailable")
	}
	binary := l.Binary
	if binary == "" {
		binary = "/usr/local/bin/cloudflared"
	}
	cmd := exec.Command(binary, "tunnel", "--no-autoupdate", "--protocol", "auto", "--metrics", address, "run", "--token-file", token.Name()) // #nosec G204 -- packaged executable, no shell or user-supplied arguments.
	// Upstream errors and diagnostic output can contain credentials or paths.
	// Fixed status codes are the only diagnostics crossing this boundary.
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	cmd.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=/nonexistent"}
	if err := cmd.Start(); err != nil {
		return nil, errors.New("connector start failed")
	}
	timeout := l.StopTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	child := &processChild{cmd: cmd, done: make(chan struct{}), url: "http://" + address + "/ready", token: token.Name(), timeout: timeout, client: &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}}}
	go func() { _ = cmd.Wait(); _ = os.Remove(child.token); close(child.done) }()
	remove = false
	return child, nil
}

type processChild struct {
	cmd        *exec.Cmd
	done       chan struct{}
	url, token string
	timeout    time.Duration
	client     *http.Client
	once       sync.Once
}

func (c *processChild) Exited() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

func (c *processChild) Ready(ctx context.Context) bool {
	if c.Exited() {
		return false
	}
	return probe(ctx, c.client, c.url)
}

func (c *processChild) Stop() {
	c.once.Do(func() {
		defer c.client.CloseIdleConnections()
		if !c.Exited() {
			_ = c.cmd.Process.Signal(syscall.SIGTERM)
			timer := time.NewTimer(c.timeout)
			defer timer.Stop()
			select {
			case <-c.done:
			case <-timer.C:
				_ = c.cmd.Process.Kill()
				<-c.done
			}
		}
	})
}
