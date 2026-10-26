package config

import "testing"

// config_agui_steer_test.go locks the AURA_AGUI_RUN_STEER* knob bundle (amendment
// #142, D-12) using the same three-phase pattern config_agui_test.go already
// proves for AURA_AGUI_RUN_DETACH: default -> explicit override wins -> a
// malformed value falls back to the default without a fatal boot. Enabled
// defaults TRUE (D-12: a flag shipped off is dark code per CLAUDE.md) — the
// explicit rollback is AURA_AGUI_RUN_STEER=false, not the shipping position.
func TestAGUISteerConfigDefaultsAndOverrides(t *testing.T) {
	clearPostgresEnv(t)

	cfg := LoadDB()
	if !cfg.AGUISteer.Enabled {
		t.Error("AGUISteer.Enabled default = false, want true (D-12: off is dark code)")
	}
	if cfg.AGUISteer.Max != 8 {
		t.Errorf("AGUISteer.Max default = %d, want 8 (amendment #132 item 10, untested default)", cfg.AGUISteer.Max)
	}
	if cfg.AGUISteer.MaxBytes != 16384 {
		t.Errorf("AGUISteer.MaxBytes default = %d, want 16384 (amendment #132 item 10, untested default)", cfg.AGUISteer.MaxBytes)
	}

	t.Setenv("AURA_AGUI_RUN_STEER", "false")
	t.Setenv("AURA_AGUI_RUN_STEER_MAX", "3")
	t.Setenv("AURA_AGUI_RUN_STEER_MAX_BYTES", "1024")
	cfg = LoadDB()
	if cfg.AGUISteer.Enabled {
		t.Error("AGUISteer.Enabled override = true, want false (explicit rollback wins, D-12)")
	}
	if cfg.AGUISteer.Max != 3 {
		t.Errorf("AGUISteer.Max override = %d, want 3", cfg.AGUISteer.Max)
	}
	if cfg.AGUISteer.MaxBytes != 1024 {
		t.Errorf("AGUISteer.MaxBytes override = %d, want 1024", cfg.AGUISteer.MaxBytes)
	}

	// A malformed value falls back (non-fatal load), never boots fatal, never
	// falls back to zero (T-52-03: a cap parsed as 0 would mean unlimited).
	t.Setenv("AURA_AGUI_RUN_STEER", "not-a-bool")
	t.Setenv("AURA_AGUI_RUN_STEER_MAX", "banana")
	t.Setenv("AURA_AGUI_RUN_STEER_MAX_BYTES", "not-an-int")
	cfg = LoadDB()
	if !cfg.AGUISteer.Enabled {
		t.Error("malformed AURA_AGUI_RUN_STEER must fall back to the default (true, D-12)")
	}
	if cfg.AGUISteer.Max != 8 {
		t.Errorf("malformed AURA_AGUI_RUN_STEER_MAX fell back to %d, want 8", cfg.AGUISteer.Max)
	}
	if cfg.AGUISteer.MaxBytes != 16384 {
		t.Errorf("malformed AURA_AGUI_RUN_STEER_MAX_BYTES fell back to %d, want 16384", cfg.AGUISteer.MaxBytes)
	}
}

// TestEveryNewKnobIsCatalogued is the D-11 tripwire the plan-check pass asked for: this
// plan's four new knobs and AURA_AGUI_RUN_DETACH must be in the registry, so the re-parse
// pass checks them, and AURA_AGUI_RUN_DETACH's loader default must stay true (D-11). The
// registry no longer carries defaults (2026-10-26), so the loader is the only side asserted.
func TestEveryNewKnobIsCatalogued(t *testing.T) {
	clearPostgresEnv(t)
	cfg := LoadDB()

	byName := make(map[string]KnobSpec)
	for _, k := range knobRegistry() {
		byName[k.Name] = k
	}

	for _, name := range []string{
		"AURA_AGUI_RUN_STEER", "AURA_AGUI_RUN_STEER_MAX", "AURA_AGUI_RUN_STEER_MAX_BYTES",
		"AURA_ASKUSER_PAUSE_TTL_SEC", "AURA_AGUI_RUN_DETACH",
	} {
		if _, ok := byName[name]; !ok {
			t.Errorf("%s is absent from the knob registry (D-11/D-12 tripwire)", name)
		}
	}

	if !cfg.AGUIRun.Detach {
		t.Error("AGUIRun.Detach loader default = false, want true — the D-11 catalogue fix asserts the CODE side, never the other way around")
	}
}
