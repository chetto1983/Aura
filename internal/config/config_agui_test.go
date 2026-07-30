package config

import "testing"

// config_agui_test.go locks the AG-UI gateway knob loads: the Phase 12 (Slice 8b)
// bind/buffer/heartbeat set (moved out of config_test.go on the Tier B touch
// so that file stays under the 600-LOC cap, refactor-on-touch) plus the fix-plan
// 1.3 Tier B AURA_AGUI_RUN_* detached-run bundle (amendment #90 point 9). Unset →
// documented defaults; set → overrides. The loopback bind default is the
// auth-deferred compensating control (Pitfall 6 / amendment #35); Detach defaults
// TRUE since amendment #90.1 (live E2E 10/10, 2026-07-23) — explicit "false" is
// the operator rollback to the pre-detach wire.
func TestAGUIConfigDefaultsAndOverrides(t *testing.T) {
	clearPostgresEnv(t)

	cfg := LoadDB()
	if cfg.AGUIBind != "127.0.0.1:9080" {
		t.Errorf("AGUIBind default = %q, want 127.0.0.1:9080", cfg.AGUIBind)
	}
	if cfg.AGUIBufferCap != 64 {
		t.Errorf("AGUIBufferCap default = %d, want 64", cfg.AGUIBufferCap)
	}
	if cfg.AGUISSEHeartbeatSec != 15 {
		t.Errorf("AGUISSEHeartbeatSec default = %d, want 15", cfg.AGUISSEHeartbeatSec)
	}
	// Behavior change, amendment #90.1: default flipped to true after the live E2E
	// campaign passed 10/10 — the rollback is an explicit AURA_AGUI_RUN_DETACH=false.
	if !cfg.AGUIRun.Detach {
		t.Error("AGUIRun.Detach default = false, want true (amendment #90.1 flip)")
	}
	if cfg.AGUIRun.BufferEvents != 2048 {
		t.Errorf("AGUIRun.BufferEvents default = %d, want 2048", cfg.AGUIRun.BufferEvents)
	}
	if cfg.AGUIRun.LingerSec != 180 {
		t.Errorf("AGUIRun.LingerSec default = %d, want 180", cfg.AGUIRun.LingerSec)
	}
	if cfg.AGUIRun.MaxWallclockSec != 3600 {
		t.Errorf("AGUIRun.MaxWallclockSec default = %d, want 3600", cfg.AGUIRun.MaxWallclockSec)
	}
	if cfg.AGUIRun.MaxLive != 16 {
		t.Errorf("AGUIRun.MaxLive default = %d, want 16", cfg.AGUIRun.MaxLive)
	}

	t.Setenv("AURA_AGUI_BIND", "127.0.0.1:9999")
	t.Setenv("AURA_AGUI_BUFFER_CAP", "128")
	t.Setenv("AURA_AGUI_SSE_HEARTBEAT_SEC", "5")
	t.Setenv("AURA_AGUI_RUN_DETACH", "false")
	t.Setenv("AURA_AGUI_RUN_BUFFER_EVENTS", "512")
	t.Setenv("AURA_AGUI_RUN_LINGER_SEC", "60")
	t.Setenv("AURA_AGUI_RUN_MAX_WALLCLOCK_SEC", "900")
	t.Setenv("AURA_AGUI_RUN_MAX_LIVE", "4")
	cfg = LoadDB()
	if cfg.AGUIBind != "127.0.0.1:9999" {
		t.Errorf("AGUIBind override = %q, want 127.0.0.1:9999", cfg.AGUIBind)
	}
	if cfg.AGUIBufferCap != 128 {
		t.Errorf("AGUIBufferCap override = %d, want 128", cfg.AGUIBufferCap)
	}
	if cfg.AGUISSEHeartbeatSec != 5 {
		t.Errorf("AGUISSEHeartbeatSec override = %d, want 5", cfg.AGUISSEHeartbeatSec)
	}
	if cfg.AGUIRun.Detach {
		t.Error("AGUIRun.Detach override = true, want false (explicit rollback wins)")
	}
	if cfg.AGUIRun.BufferEvents != 512 {
		t.Errorf("AGUIRun.BufferEvents override = %d, want 512", cfg.AGUIRun.BufferEvents)
	}
	if cfg.AGUIRun.LingerSec != 60 {
		t.Errorf("AGUIRun.LingerSec override = %d, want 60", cfg.AGUIRun.LingerSec)
	}
	if cfg.AGUIRun.MaxWallclockSec != 900 {
		t.Errorf("AGUIRun.MaxWallclockSec override = %d, want 900", cfg.AGUIRun.MaxWallclockSec)
	}
	if cfg.AGUIRun.MaxLive != 4 {
		t.Errorf("AGUIRun.MaxLive override = %d, want 4", cfg.AGUIRun.MaxLive)
	}

	// A malformed value falls back (non-fatal load), never boots fatal.
	t.Setenv("AURA_AGUI_RUN_DETACH", "not-a-bool")
	t.Setenv("AURA_AGUI_RUN_BUFFER_EVENTS", "not-an-int")
	cfg = LoadDB()
	if !cfg.AGUIRun.Detach {
		t.Error("malformed AURA_AGUI_RUN_DETACH must fall back to the default (true, #90.1)")
	}
	if cfg.AGUIRun.BufferEvents != 2048 {
		t.Errorf("malformed AURA_AGUI_RUN_BUFFER_EVENTS fell back to %d, want 2048", cfg.AGUIRun.BufferEvents)
	}
}
