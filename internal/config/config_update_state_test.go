package config

import "testing"

// compose_boot_order_test.go pins DefaultUpdateStateDir to the compose mount; this pins that
// Aura reads it when nothing overrides it.
func TestUpdateStateDirDefaultsToTheComposeMount(t *testing.T) {
	clearPostgresEnv(t)
	t.Setenv("AURA_UPDATE_STATE_DIR", "")
	if got := LoadDB().UpdateStateDir; got != DefaultUpdateStateDir {
		t.Fatalf("UpdateStateDir default = %q, want %q", got, DefaultUpdateStateDir)
	}
	t.Setenv("AURA_UPDATE_STATE_DIR", "/srv/aura/update")
	if got := LoadDB().UpdateStateDir; got != "/srv/aura/update" {
		t.Fatalf("UpdateStateDir = %q, want the override", got)
	}
}
