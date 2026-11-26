package config

import (
	"testing"

	"github.com/chetto1983/aura/internal/sidecarurl"
)

// The cockpit relay and the mail watch append "/admin/..." to this base, so a trailing slash
// sent them to "//admin/...", which the PIM answers 404 (measured 2026-11-23). ci.yml's calendar
// job writes the URL with one.
func TestPIMURLDropsATrailingSlash(t *testing.T) {
	clearPostgresEnv(t)
	t.Setenv("AURA_IN_CONTAINER", "1")
	cases := map[string]string{
		"":                        "http://aura-pim-mcp:8080",
		"http://127.0.0.1:8093":   "http://127.0.0.1:8093",
		"http://127.0.0.1:8093/":  "http://127.0.0.1:8093",
		"http://127.0.0.1:8093//": "http://127.0.0.1:8093",
	}
	for raw, want := range cases {
		t.Setenv("AURA_PIM_MCP_URL", raw)
		if got := LoadDB().CalendarMCPURL; got != want {
			t.Errorf("AURA_PIM_MCP_URL=%q: CalendarMCPURL = %q, want %q", raw, got, want)
		}
	}
}

// The Settings relays reach the PIM and the WhatsApp bridge where the MCP manager mounts them.
// With a Compose name as the default, a native Aura mounted the calendar on 127.0.0.1:8093 and
// its mail relay answered "calendar sidecar unreachable" (CI run 45864462788).
func TestSidecarURLsDefaultToWhereTheManagerMountsThem(t *testing.T) {
	clearPostgresEnv(t)
	t.Setenv("AURA_PIM_MCP_URL", "")
	t.Setenv("AURA_WHATSAPP_BRIDGE_URL", "")
	t.Setenv("AURA_PIM_MCP_PORT", "9093")
	t.Setenv("AURA_WHATSAPP_BRIDGE_PORT", "9094")
	for _, tc := range []struct{ name, inContainer, pim, bridge string }{
		{"container", "1", "http://aura-pim-mcp:8080", "http://whatsapp:8081"},
		{"native", "", "http://127.0.0.1:9093", "http://127.0.0.1:9094"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("AURA_IN_CONTAINER", tc.inContainer)
			cfg := LoadDB()
			if cfg.CalendarMCPURL != tc.pim || cfg.CalendarMCPURL != sidecarurl.PIM() {
				t.Errorf("CalendarMCPURL = %q, want %q, where the manager mounts it (%q)",
					cfg.CalendarMCPURL, tc.pim, sidecarurl.PIM())
			}
			if cfg.WhatsAppBridgeURL != tc.bridge || cfg.WhatsAppBridgeURL != sidecarurl.WhatsAppBridge() {
				t.Errorf("WhatsAppBridgeURL = %q, want %q, where the manager mounts it (%q)",
					cfg.WhatsAppBridgeURL, tc.bridge, sidecarurl.WhatsAppBridge())
			}
		})
	}
}
