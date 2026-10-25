package config

import (
	"path/filepath"
	"testing"
)

// The code default sits under the per-user ~/.aura, beside the other durable roots; compose
// pins the container's to the aura-home volume.
func TestPluginsDirDefaultsUnderAuraHome(t *testing.T) {
	clearPostgresEnv(t)
	t.Setenv("AURA_PLUGINS_DIR", "")
	if got, want := LoadDB().PluginsDir, filepath.Join(auraHomeDir(), "plugins"); got != want {
		t.Errorf("PluginsDir = %q, want %q", got, want)
	}
	t.Setenv("AURA_PLUGINS_DIR", "/var/lib/aura/plugins")
	if got := LoadDB().PluginsDir; got != "/var/lib/aura/plugins" {
		t.Errorf("PluginsDir = %q, want the env value", got)
	}
	found := false
	for _, k := range knobRegistry() {
		if k.Name == "AURA_PLUGINS_DIR" && k.Kind == KindString {
			found = true
		}
	}
	if !found {
		t.Error("AURA_PLUGINS_DIR has no string knob entry")
	}
}
