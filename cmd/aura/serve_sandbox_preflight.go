// serve_sandbox_preflight.go — the boot-time counterpart to the lazy first-tool-call image
// pull (D-03). A strict profile with isolation on routes every shell/file tool through the
// per-identity sandbox; without this preflight, an unbuilt/unpublished box image boots
// healthy (`/healthz`/`/readyz` both green — checkSandboxReadiness is an ImageInspect, never a
// pull, serve_sandbox_readiness.go) and then denies every tool call with no diagnostic at the
// operator's first shell_exec. This preflight makes that failure LOUD, at boot, naming a
// command that actually works — mirroring gateMultiUserRequiresStrictProfile's own "name the
// exact knob and the exact remedy" shape (that gate's own comment records the cost of getting
// this wrong: a fail-closed message that told an operator to set a variable nothing read left
// them stuck at a boot loop following its own instructions).
//
// It CANNOT live inside Config.ValidateProfile: that function's own doc comment
// (config_validate.go) states it "performs no other I/O" beyond two named exceptions, and
// serve_settings.go's live-reload path depends on that staying true. This is therefore a
// separate boot step, run from bootServe after cfg.Validate() succeeds and before any HTTP
// listener opens.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/chetto1983/aura/internal/config"
)

// sandboxImageBootTimeout bounds the boot-time image-ensure ImagePull. Generous enough for a
// cold registry pull of the fat sandbox image over a slow link, bounded so an unreachable
// registry cannot wedge daemon boot forever.
const sandboxImageBootTimeout = 5 * time.Minute

// browserBoxBootTimeout bounds starting the browser boxes at boot: an existing box starts in a
// few seconds, and a stuck daemon must not hold the listeners closed.
const browserBoxBootTimeout = 2 * time.Minute

// legacyHomeSweepTimeout bounds the boot sweep of legacy browser homes: one helper per shell
// volume takes about a second, and a volume left unswept keeps its shell shut until its next call
// sweeps it.
const legacyHomeSweepTimeout = 2 * time.Minute

// sandboxBootEnabled is the deployment whose tools run in boxes: a strict profile with isolation
// on. Boot touches the sandbox only there.
func sandboxBootEnabled(chat *chatEnv) bool {
	return chat != nil && chat.cfg != nil && chat.cfg.Profile.Strict() && chat.cfg.MUSRIsolation
}

// sweepLegacyBrowserHomes removes, at boot, the logged-in profile the shell volumes kept before
// the browser had a box of its own (operator, 2026-10-15). A volume not swept here keeps its
// shell shut until its next call sweeps it.
func sweepLegacyBrowserHomes(ctx context.Context, chat *chatEnv) {
	if !sandboxBootEnabled(chat) {
		return
	}
	sctx, cancel := context.WithTimeout(ctx, legacyHomeSweepTimeout)
	defer cancel()
	swept, err := chat.sandboxRouter.SweepLegacyBrowserHomes(sctx)
	if err != nil {
		slog.Warn("aura serve: a shell volume may still hold a legacy browser home; its shell stays shut until its next call removes it", "swept", swept, "err", err)
		return
	}
	slog.Info("aura serve: legacy browser homes removed from the shell volumes", "volumes", swept)
}

// startBrowserBoxes starts every identity's existing browser box at boot, so the browser MCP
// server answers the first call and the board's probe (operator, 2026-10-12). A box that does
// not start is logged and boot goes on; its next browser call starts it.
func startBrowserBoxes(ctx context.Context, chat *chatEnv) {
	if !sandboxBootEnabled(chat) {
		return
	}
	bctx, cancel := context.WithTimeout(ctx, browserBoxBootTimeout)
	defer cancel()
	started, err := chat.sandboxRouter.StartBrowserBoxes(bctx)
	if err != nil {
		slog.Warn("aura serve: a browser box did not start at boot; its next browser call starts it", "started", started, "err", err)
		return
	}
	slog.Info("aura serve: browser boxes started", "count", started)
}

// sandboxImagePreflight refuses to boot a strict, isolation-on `aura serve` whose box image is
// neither present locally nor pullable (D-03). It is a no-op — nil immediately — unless BOTH
// chat.cfg.Profile.Strict() and chat.cfg.MUSRIsolation hold, so `aura chat` (a different entry
// point that never calls this) and every non-strict or isolation-off deployment are entirely
// unaffected. On failure it names the configured image ref, the repo build command, and a
// registry pull form — never a knob with no effect (the scar tissue
// gateMultiUserRequiresStrictProfile's own comment records).
func sandboxImagePreflight(ctx context.Context, chat *chatEnv) error {
	if !sandboxBootEnabled(chat) {
		return nil
	}
	pctx, cancel := context.WithTimeout(ctx, sandboxImageBootTimeout)
	defer cancel()
	image := chat.cfg.Sandbox.Image
	if err := chat.sandboxRouter.EnsureImage(pctx); err != nil {
		return fmt.Errorf(
			"config: AURA_SANDBOX_IMAGE %q is neither present locally nor pullable (%w) — "+
				"a strict, multi-identity deployment routes every shell/file tool through this "+
				"image, so the daemon refuses to boot into a silently non-functional tool "+
				"surface. Fix one of: build it from this repo with `make sandbox-images`; "+
				"pull it explicitly with `docker pull %s`; or point AURA_SANDBOX_IMAGE at a "+
				"registry ref this host can reach",
			image, err, image,
		)
	}
	slog.Info("aura serve: sandbox box image verified available", "image", image)

	// DISCRETION, not D-03: every box gets the egress floor sidecar when an egress image is
	// configured, so an unreachable egress image produces the identical silent
	// tool-surface failure — but D-03 names only AURA_SANDBOX_IMAGE as Fatal, so this stays a
	// WARN, never promoted to Fatal.
	if err := chat.sandboxRouter.EnsureEgressImage(pctx); err != nil {
		slog.Warn("aura serve: sandbox egress image not verified available — run `make sandbox-images` or point AURA_SANDBOX_EGRESS_IMAGE at a reachable ref",
			"image", chat.cfg.Sandbox.EgressImage, "err", err)
	}
	return nil
}

// logMultiUserAvailability emits exactly one INFO line at boot when a non-strict deployment
// with isolation off starts (D-05), pointing existing operators at the rollout runbook.
// Nothing is emitted in any other profile/isolation combination, and nothing is emitted per
// request — this runs once, at boot, never on a live request path.
func logMultiUserAvailability(cfg *config.Config) {
	if cfg == nil || cfg.Profile.Strict() || cfg.MUSRIsolation {
		return
	}
	slog.Info("aura serve: multi-identity provisioning is available — see docs/runbooks/musr-rollout.md to enable it")
}
