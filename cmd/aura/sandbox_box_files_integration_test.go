//go:build docker_integration

package main

import (
	"context"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"github.com/moby/moby/client"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
	"github.com/chetto1983/aura/internal/secret"
)

// TestComposedBoxRunsAgentBrowserWithItsIdentityKey is the composition-root proof for
// authenticated browsing (prd.md §12): a box routed through buildSandboxRouter carries the key
// derived for ITS identity, and the image's agent-browser entry point drives Chromium with it
// without ever minting a key of its own. It needs the real box image, so unlike the egress tests
// it does not fall back to busybox: it defaults to aura-sandbox:latest, which the CI
// docker_integration job builds before this tier runs (override with AURA_SANDBOX_IMAGE).
func TestComposedBoxRunsAgentBrowserWithItsIdentityKey(t *testing.T) {
	egressITDockerdOrGate(t)
	image := strings.TrimSpace(os.Getenv("AURA_SANDBOX_IMAGE"))
	if image == "" {
		image = "aura-sandbox:latest"
	}
	const authulaSecret = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	cfg := &config.Config{
		Profile:       config.ProfileSingleUserHardened,
		AuthulaSecret: authulaSecret,
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

	res, err := router.Exec(ctx, h, usersandbox.ExecRequest{Command: `
		set -e
		cat ` + browserStateKeyPath + `; echo
		agent-browser open "data:text/html,<button>Run</button>" >/dev/null
		agent-browser snapshot
		agent-browser close >/dev/null
		find / -name .encryption-key -path "*agent-browser*" 2>/dev/null | wc -l`})
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("exec: err=%v code=%d stdout=%s stderr=%s", err, res.ExitCode, res.Stdout, res.Stderr)
	}
	lines := strings.Split(strings.TrimSpace(string(res.Stdout)), "\n")
	want, _ := secret.IdentityKey(authulaSecret, browserStateKeyDomain, id)
	if lines[0] != hex.EncodeToString(want) {
		t.Fatalf("box key %q is not the key derived for identity %s", lines[0], id)
	}
	if !strings.Contains(string(res.Stdout), `button "Run"`) {
		t.Fatalf("agent-browser did not render the page: %s", res.Stdout)
	}
	if minted := lines[len(lines)-1]; minted != "0" {
		t.Fatalf("agent-browser minted %s key file(s) of its own", minted)
	}
}
