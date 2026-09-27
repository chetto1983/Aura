//go:build docker_integration

package usersandbox

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// lockedBuffer is the concurrent-safe sink the pump goroutine writes into.
type lockedBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// TestExecStreamStdin proves the channel the browser live view relays input through: bytes
// written to in reach the box process as they are written, closing in ends the job's input, and
// a Kill with an input pipe nobody ever closes still returns (the handle closes in itself).
// goleak (TestMain) catches a feeder left parked on the read.
func TestExecStreamStdin(t *testing.T) {
	skipUnlessDockerd(t)
	cli := newTestDockerClient(t)
	backend := NewDockerBackend(cli, testBoxImage(), testLimits())
	ctx := context.Background()
	h, err := backend.Resolve(ctx, SandboxSpec{IdentityID: strings.ReplaceAll("stdin-"+time.Now().Format("150405.000000"), ".", "")})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	t.Cleanup(func() { _ = backend.Stop(context.Background(), h) })

	t.Run("lines in, lines out, EOF ends the job", func(t *testing.T) {
		pr, pw := io.Pipe()
		out := &lockedBuffer{}
		hnd, err := backend.ExecStream(ctx, h, ExecRequest{Command: `while read -r l; do echo "got:$l"; done; echo eof`}, pr, out)
		if err != nil {
			t.Fatalf("exec stream: %v", err)
		}
		for _, l := range []string{"a", "b"} {
			if _, err := io.WriteString(pw, l+"\n"); err != nil {
				t.Fatalf("write: %v", err)
			}
		}
		deadline := time.Now().Add(10 * time.Second)
		for !strings.Contains(out.String(), "got:b") && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		if got := out.String(); !strings.Contains(got, "got:a\ngot:b\n") || strings.Contains(got, "eof") {
			t.Fatalf("before EOF out = %q, want both lines echoed and the loop still reading", got)
		}
		_ = pw.Close()
		code, err := hnd.Wait()
		if err != nil || code != 0 || !strings.HasSuffix(out.String(), "eof\n") {
			t.Fatalf("after EOF: code=%d err=%v out=%q, want a clean exit after the loop", code, err, out.String())
		}
	})

	t.Run("kill returns with an input pipe nobody closes", func(t *testing.T) {
		pr, pw := io.Pipe()
		defer pw.Close()
		hnd, err := backend.ExecStream(ctx, h, ExecRequest{Command: "cat >/dev/null"}, pr, io.Discard)
		if err != nil {
			t.Fatalf("exec stream: %v", err)
		}
		hnd.Kill()
		done := make(chan struct{})
		go func() { _, _ = hnd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			t.Fatal("Wait never returned after Kill: the stdin feeder is parked on a pipe nobody writes")
		}
	})
}
