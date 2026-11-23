package config

import "testing"

// The cockpit relay and the mail watch append "/admin/..." to this base, so a trailing slash
// sent them to "//admin/...", which the PIM answers 404 (measured 2026-11-23). ci.yml's calendar
// job writes the URL with one.
func TestPIMURLDropsATrailingSlash(t *testing.T) {
	clearPostgresEnv(t)
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
