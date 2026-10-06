//go:build docker_integration

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/client"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// TestBrowserRelayDrivesTheBoxBrowser is the live-view chain end to end on the real image
// (prd.md §12): the relay adapter opens browser-relay.mjs over a stdin-capable exec in the
// identity's box, frames come back out, and a click plus typed text sent the way the cockpit
// sends them land in the page. The field's value is then read with agent-browser itself.
func TestBrowserRelayDrivesTheBoxBrowser(t *testing.T) {
	ctx, router, h := liveBox(t)
	run := func(cmd string) string {
		t.Helper()
		res, err := router.Exec(ctx, h, usersandbox.ExecRequest{Command: cmd})
		if err != nil || res.ExitCode != 0 {
			t.Fatalf("exec %q: err=%v code=%d out=%s err=%s", cmd, err, res.ExitCode, res.Stdout, res.Stderr)
		}
		return string(res.Stdout)
	}
	run(`agent-browser --session live open "data:text/html,<input id=u autofocus style='position:absolute;left:0;top:0;width:400px;height:40px'>"`)

	view := openLiveView(t, ctx, router, "live")
	view.click(100, 20)
	for _, ch := range "hi.1" {
		s := string(ch)
		view.send(map[string]any{"type": "input_keyboard", "eventType": "keyDown", "key": s, "text": s})
		view.send(map[string]any{"type": "input_keyboard", "eventType": "keyUp", "key": s})
	}
	// A non-input command must be dropped by the relay itself, not only by the HTTP layer.
	view.send(map[string]any{"type": "navigate", "url": "about:blank"})

	deadline := time.Now().Add(10 * time.Second)
	value := ""
	for time.Now().Before(deadline) {
		value = strings.TrimSpace(run(`agent-browser --session live eval "document.getElementById('u')?.value ?? 'NAVIGATED'"`))
		if value == `"hi.1"` {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if value != `"hi.1"` {
		t.Fatalf("field value = %s, want \"hi.1\" typed through the relay (and no navigation)", value)
	}

	view.close()
}

// liveBox routes the box of a fresh identity from the real image and stops it after the test.
func liveBox(t *testing.T) (context.Context, *usersandbox.SandboxRouter, usersandbox.BoxHandle) {
	t.Helper()
	egressITDockerdOrGate(t)
	image := strings.TrimSpace(os.Getenv("AURA_SANDBOX_IMAGE"))
	if image == "" {
		image = "aura-sandbox:latest"
	}
	cfg := &config.Config{
		Profile:       config.ProfileSingleUserHardened,
		AuthulaSecret: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Sandbox:       config.SandboxConfig{Image: image, CPULimit: 1, MemoryLimit: 1 << 30, PidsLimit: 256},
	}
	router := buildSandboxRouter(cfg, nil)
	ctx := identityctx.WithIdentityID(context.Background(), egressITIdentity(t))
	h, err := router.Route(ctx)
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	cli, err := client.New(client.FromEnv)
	if err != nil {
		t.Fatalf("docker client: %v", err)
	}
	t.Cleanup(func() {
		_ = usersandbox.NewDockerBackend(cli, image, usersandbox.Resources{}).Stop(context.Background(), h)
		_ = cli.Close()
	})
	return ctx, router, h
}

// liveView is the cockpit's side of the relay: viewer input in, stream messages out.
type liveView struct {
	t      *testing.T
	handle agui.BrowserRelayHandle
	input  *io.PipeWriter
	output *io.PipeWriter
	frames chan string
}

// openLiveView opens the relay the way the cockpit does and returns once a frame has arrived.
func openLiveView(t *testing.T, ctx context.Context, router *usersandbox.SandboxRouter, session string) *liveView {
	t.Helper()
	pr, pw := io.Pipe()
	outR, outW := io.Pipe()
	handle, err := sandboxBrowserRelay{router: router}.Open(ctx, session, pr, outW)
	if err != nil {
		t.Fatalf("open relay: %v", err)
	}
	v := &liveView{t: t, handle: handle, input: pw, output: outW, frames: make(chan string, 64)}
	go func() {
		defer close(v.frames)
		sc := bufio.NewScanner(outR)
		sc.Buffer(make([]byte, 1<<20), 8<<20)
		for sc.Scan() {
			var msg struct{ Type string }
			if json.Unmarshal(sc.Bytes(), &msg) == nil {
				v.frames <- msg.Type
			}
		}
	}()
	deadline := time.After(20 * time.Second)
	for {
		select {
		case got, ok := <-v.frames:
			if !ok {
				t.Fatal("relay stream ended before a frame")
			}
			if got == "frame" {
				return v
			}
			if got == "relay_error" {
				t.Fatal("relay reported an error")
			}
		case <-deadline:
			t.Fatal("no frame from the relay within 20s")
		}
	}
}

func (v *liveView) send(msg map[string]any) {
	v.t.Helper()
	b, _ := json.Marshal(msg)
	if _, err := v.input.Write(append(b, '\n')); err != nil {
		v.t.Fatalf("write input: %v", err)
	}
}

func (v *liveView) click(x, y int) {
	v.t.Helper()
	for _, ev := range []string{"mousePressed", "mouseReleased"} {
		v.send(map[string]any{"type": "input_mouse", "eventType": ev, "x": x, "y": y, "button": "left", "clickCount": 1})
	}
}

// close ends viewer input and requires the relay to exit cleanly on that EOF.
func (v *liveView) close() {
	v.t.Helper()
	_ = v.input.Close()
	if code, err := v.handle.Wait(); err != nil || code != 0 {
		v.t.Fatalf("relay exit: code=%d err=%v, want a clean exit on input EOF", code, err)
	}
	_ = v.output.Close()
	for range v.frames {
	}
}
