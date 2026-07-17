// config_share.go defines the AURA_SHARE_* operator config surface (Phase 37F,
// WEBSHARE-02/03): the share-lifecycle kill-switch + expiry cap that plan 37-10's
// share-create handler reads. Split out of config.go so the root stays under the
// 600-LOC cap (CLAUDE.md NO GOD CLASS), mirroring the db.Config / SandboxConfig
// sub-struct composition (config_sandbox.go).
//
// Why PublicEnabled defaults to false: it is the D-02 org kill-switch AND the
// closure for R-08. RequireCapability returns `next` unchanged when
// !SecretConfigured (internal/agui/auth.go:282), so on loopback the `share.public`
// capability gate does not exist. The kill-switch below is re-checked INSIDE the
// share-create handler, where no such bypass applies — two independent gates, one
// survives loopback. A default of `true` would make loopback dev able to mint
// public links with no gate at all.
//
// Why this is an env knob and not an aura.settings row: settings.AllowedKeys is a
// static model-backend-only allowlist that exists specifically so a settings row
// can never reach connection/security env; a share kill-switch is security env.
// settings.OverlayEnv is also boot-time only.
package config

import "github.com/chetto1983/aura/internal/envutil"

// defaultShareMaxExpiryDays is the fail-closed expiry cap (D-04) when the operator
// has not tuned it and when a non-positive override would otherwise make every
// public mint fail or never expire — neither is a sane operator intent.
const defaultShareMaxExpiryDays = 90

// ShareConfig is the AURA_SHARE_* operator surface for Phase 37F conversation/
// artifact sharing (WEBSHARE-02/03).
type ShareConfig struct {
	// PublicEnabled is the D-02 org kill-switch for the public share tier. See the
	// package doc above for why the default is false (R-08 loopback closure).
	PublicEnabled bool // AURA_SHARE_PUBLIC_ENABLED — public-tier kill-switch, default false

	// MaxExpiryDays caps how far in the future a public link's expires_at may be
	// set. A non-positive value is clamped back to the default (D-04): a zero/
	// negative cap would make every public mint fail or never expire.
	MaxExpiryDays int // AURA_SHARE_MAX_EXPIRY_DAYS — public-tier expiry cap in days, default 90
}

// loadShare reads the AURA_SHARE_* surface with non-fatal fallbacks, wired into
// loadBase() as cfg.Share. A malformed value silently absorbs to the fallback
// (never a fatal boot), matching the envutil contract every other per-subsystem
// config leaf follows.
func loadShare() ShareConfig {
	maxExpiryDays := envutil.IntDefault("AURA_SHARE_MAX_EXPIRY_DAYS", defaultShareMaxExpiryDays)
	if maxExpiryDays <= 0 {
		maxExpiryDays = defaultShareMaxExpiryDays
	}
	return ShareConfig{
		PublicEnabled: envutil.BoolDefault("AURA_SHARE_PUBLIC_ENABLED", false),
		MaxExpiryDays: maxExpiryDays,
	}
}
