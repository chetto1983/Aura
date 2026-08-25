// config_agui_steer.go defines the AURA_AGUI_RUN_STEER* operator surface
// (amendment #132 item 10, corrected by amendment #142 correction 4 / D-12):
// mid-turn steering's enable flag plus its per-conversation queue-depth and
// per-steer byte caps. Split out of config.go so the root stays under the
// 600-LOC cap (CLAUDE.md NO GOD CLASS), mirroring config_agui_run.go's
// sub-struct precedent — this file is its structural twin.
//
// Enabled ships default TRUE. D-12's operator ruling is verbatim "un flag a
// off è = a dark code" (CLAUDE.md's own DARK-CODE-IS-FORBIDDEN rule): a
// capability nobody can reach without editing the environment is not a
// shipped feature. AURA_AGUI_RUN_STEER=false remains the explicit rollback —
// the same shape D-11 already endorsed for AURA_AGUI_RUN_DETACH.
//
// Max=8 and MaxBytes=16384 are the numbers amendment #132 item 10 ratifies —
// and #132's own closing paragraph says plainly they are UNTESTED defaults
// carried from the design study, not measured values. A reader three phases
// from now must not mistake either number for something proven; only
// Enabled's default is a decision (D-12), the two caps are placeholders
// pending real usage.
//
// Every field is a non-fatal envutil fallback: a malformed value falls back
// to the default at load, never boots fatal (T-52-03 — a cap must never
// silently parse to 0/unlimited). All three are cataloged in
// config_knobs.go so the per-profile re-parse pass flags a malformed value
// under a strict tier instead of silently defaulting.
package config

import "github.com/chetto1983/aura/internal/envutil"

// AGUISteerConfig is the mid-turn steering knob bundle consumed by
// internal/steer.Inbox (plan 52-02) and the AG-UI steer route (plan 52-04).
type AGUISteerConfig struct {
	Enabled  bool // AURA_AGUI_RUN_STEER — mid-turn steering on/off (default true, D-12: off is dark code)
	Max      int  // AURA_AGUI_RUN_STEER_MAX — queued-steer cap per conversation (default 8, untested — amendment #132 item 10)
	MaxBytes int  // AURA_AGUI_RUN_STEER_MAX_BYTES — per-steer byte cap (default 16384, untested — amendment #132 item 10)
}

// loadAGUISteerConfig reads the AURA_AGUI_RUN_STEER* surface with non-fatal
// fallbacks, wired into loadBase() as cfg.AGUISteer.
func loadAGUISteerConfig() AGUISteerConfig {
	return AGUISteerConfig{
		Enabled:  envutil.BoolDefault("AURA_AGUI_RUN_STEER", true),
		Max:      envutil.IntDefault("AURA_AGUI_RUN_STEER_MAX", 8),
		MaxBytes: envutil.IntDefault("AURA_AGUI_RUN_STEER_MAX_BYTES", 16384),
	}
}
