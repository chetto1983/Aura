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

// sandboxImagePreflight refuses to boot a strict, isolation-on `aura serve` whose box image is
// neither present locally nor pullable (D-03). It is a no-op — nil immediately — unless BOTH
// chat.cfg.Profile.Strict() and chat.cfg.MUSRIsolation hold, so `aura chat` (a different entry
// point that never calls this) and every non-strict or isolation-off deployment are entirely
// unaffected. On failure it names the configured image ref, the repo build command, and a
// registry pull form — never a knob with no effect (the scar tissue
// gateMultiUserRequiresStrictProfile's own comment records).
func sandboxImagePreflight(ctx context.Context, chat *chatEnv) error {
	if chat == nil || chat.cfg == nil || !chat.cfg.Profile.Strict() || !chat.cfg.MUSRIsolation {
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

	// DISCRETION, not D-03: specFor (usersandbox/router.go) sets Egress.Floor:true on every
	// strict-profile box, so an unreachable egress image produces the identical silent
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
