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

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// TestBrowserRelayDrivesTheBoxBrowser is the live-view chain end to end on the real image
// (prd.md §12): the relay adapter opens browser-relay.mjs over a stdin-capable exec in the
// identity's box, frames come back out, and a click plus typed text sent the way the cockpit
// sends them land in the page. The field's value is then read with agent-browser itself.
func TestBrowserRelayDrivesTheBoxBrowser(t *testing.T) {
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
	id := egressITIdentity(t)
	router := buildSandboxRouter(cfg, nil)
	ctx := identityctx.WithIdentityID(context.Background(), id)
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

	run := func(cmd string) string {
		t.Helper()
		res, err := router.Exec(ctx, h, usersandbox.ExecRequest{Command: cmd})
		if err != nil || res.ExitCode != 0 {
			t.Fatalf("exec %q: err=%v code=%d out=%s err=%s", cmd, err, res.ExitCode, res.Stdout, res.Stderr)
		}
		return string(res.Stdout)
	}
	run(`agent-browser --session live open "data:text/html,<input id=u autofocus style='position:absolute;left:0;top:0;width:400px;height:40px'>"`)

	pr, pw := io.Pipe()
	outR, outW := io.Pipe()
	relay := sandboxBrowserRelay{router: router}
	hnd, err := relay.Open(ctx, "live", pr, outW)
	if err != nil {
		t.Fatalf("open relay: %v", err)
	}
	frames := make(chan string, 64)
	go func() {
		defer close(frames)
		sc := bufio.NewScanner(outR)
		sc.Buffer(make([]byte, 1<<20), 8<<20)
		for sc.Scan() {
			var msg struct{ Type string }
			if json.Unmarshal(sc.Bytes(), &msg) == nil {
				frames <- msg.Type
			}
		}
	}()
	waitFor := func(kind string) {
		t.Helper()
		deadline := time.After(20 * time.Second)
		for {
			select {
			case got, ok := <-frames:
				if !ok {
					t.Fatalf("relay stream ended before a %q message", kind)
				}
				if got == kind {
					return
				}
				if got == "relay_error" {
					t.Fatal("relay reported an error")
				}
			case <-deadline:
				t.Fatalf("no %q message from the relay within 20s", kind)
			}
		}
	}
	waitFor("frame")

	send := func(v map[string]any) {
		b, _ := json.Marshal(v)
		if _, err := pw.Write(append(b, '\n')); err != nil {
			t.Fatalf("write input: %v", err)
		}
	}
	for _, ev := range []string{"mousePressed", "mouseReleased"} {
		send(map[string]any{"type": "input_mouse", "eventType": ev, "x": 100, "y": 20, "button": "left", "clickCount": 1})
	}
	for _, ch := range "hi.1" {
		s := string(ch)
		send(map[string]any{"type": "input_keyboard", "eventType": "keyDown", "key": s, "text": s})
		send(map[string]any{"type": "input_keyboard", "eventType": "keyUp", "key": s})
	}
	// A non-input command must be dropped by the relay itself, not only by the HTTP layer.
	send(map[string]any{"type": "navigate", "url": "about:blank"})

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

	_ = pw.Close()
	if code, err := hnd.Wait(); err != nil || code != 0 {
		t.Fatalf("relay exit: code=%d err=%v, want a clean exit on input EOF", code, err)
	}
	_ = outW.Close()
	for range frames {
	}
}
