// config_agui_run.go defines the AURA_AGUI_RUN_* operator surface (fix-plan 1.3
// Tier B, amendment #90 point 9 — docs/audit/sse-resume-design-1.3b-2026-07-23.md
// §7.1): the detached-run flag plus the RunRegistry bounds the agui composition
// root resolves into agui.ServerConfig, exactly as AGUISSEHeartbeatSec flows today.
// Split out of config.go so the root stays under the 600-LOC cap (CLAUDE.md NO GOD
// CLASS), mirroring the config_sandbox.go sub-struct precedent.
//
// Every field is a non-fatal envutil fallback (a malformed ad-hoc tweak falls back
// at load, never boots fatal); the knobs are cataloged KindBool/KindInt in
// config_knobs.go so the per-profile re-parse pass flags a malformed value under a
// strict tier instead of silently defaulting.
package config

import "github.com/chetto1983/aura/internal/envutil"

// AGUIRunConfig is the detached-run knob bundle. Detach is default-ON since the
// 2026-07-23 live E2E campaign passed at 10/10 (amendment #90 point 10's flip
// condition: kill-TCP resume, reload-attach, cancel, lock-release all proven on
// the live stack — the flip is ratified as amendment #90.1). Off ⇒ nil
// RunRegistry ⇒ the pre-detach /agent/run wire, kept as the operator rollback.
// The four ints are caps/bounds resolved agui-side with non-positive→default
// fallbacks (agui.NewRunRegistry) — the bufferCap convention, NOT the
// heartbeat's <=0-disables.
type AGUIRunConfig struct {
	Detach          bool // AURA_AGUI_RUN_DETACH — decouple run lifetime from the HTTP fetch (default true, #90.1)
	BufferEvents    int  // AURA_AGUI_RUN_BUFFER_EVENTS — per-run replay ring capacity in events (default 2048 ≈ 1 MiB/run worst-case)
	LingerSec       int  // AURA_AGUI_RUN_LINGER_SEC — terminal-session replay window before reaper eviction (default 180)
	MaxWallclockSec int  // AURA_AGUI_RUN_MAX_WALLCLOCK_SEC — outer detached-ctx bound over the agent Budget (default 3600)
	MaxLive         int  // AURA_AGUI_RUN_MAX_LIVE — live-run registry cap; Start past it answers 503 (default 16)
}

// loadAGUIRunConfig reads the AURA_AGUI_RUN_* surface with non-fatal fallbacks,
// wired into loadBase() as cfg.AGUIRun.
func loadAGUIRunConfig() AGUIRunConfig {
	return AGUIRunConfig{
		Detach:          envutil.BoolDefault("AURA_AGUI_RUN_DETACH", true),
		BufferEvents:    envutil.IntDefault("AURA_AGUI_RUN_BUFFER_EVENTS", 2048),
		LingerSec:       envutil.IntDefault("AURA_AGUI_RUN_LINGER_SEC", 180),
		MaxWallclockSec: envutil.IntDefault("AURA_AGUI_RUN_MAX_WALLCLOCK_SEC", 3600),
		MaxLive:         envutil.IntDefault("AURA_AGUI_RUN_MAX_LIVE", 16),
	}
}
