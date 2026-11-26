package sidecarurl

import "testing"

var sidecars = []struct {
	name                     string
	url                      func() string
	portEnv, container, dflt string
}{
	{"PIM", PIM, "AURA_PIM_MCP_PORT", "http://aura-pim-mcp:8080", "8093"},
	{"Memory", Memory, "AURA_ARCADEDB_MCP_PORT", "http://aura-arcadedb-mcp:8096", "8096"},
	{"WhatsAppMCP", WhatsAppMCP, "AURA_WHATSAPP_MCP_PORT", "http://whatsapp:8080", "8092"},
	{"WhatsAppBridge", WhatsAppBridge, "AURA_WHATSAPP_BRIDGE_PORT", "http://whatsapp:8081", "8094"},
}

func TestComposeNameInsideTheAppliance(t *testing.T) {
	t.Setenv("AURA_IN_CONTAINER", "1")
	for _, s := range sidecars {
		t.Setenv(s.portEnv, "9100")
		if got := s.url(); got != s.container {
			t.Errorf("%s() = %q, want the Compose name %q whatever the port", s.name, got, s.container)
		}
	}
}

func TestLoopbackPublishForANativeAura(t *testing.T) {
	t.Setenv("AURA_IN_CONTAINER", "")
	for _, s := range sidecars {
		t.Setenv(s.portEnv, "")
		if got, want := s.url(), "http://127.0.0.1:"+s.dflt; got != want {
			t.Errorf("%s() = %q, want the default publish %q", s.name, got, want)
		}
		t.Setenv(s.portEnv, " 9100 ")
		if got := s.url(); got != "http://127.0.0.1:9100" {
			t.Errorf("%s() with %s=9100 = %q, want the configured port", s.name, s.portEnv, got)
		}
	}
}

func TestAPortThatIsNotOneFallsBackToTheDefault(t *testing.T) {
	t.Setenv("AURA_IN_CONTAINER", "")
	for _, s := range sidecars {
		for _, bad := range []string{s.dflt + "@evil.example", "0", "65536", "-1", "junk", "80 93"} {
			t.Setenv(s.portEnv, bad)
			if got, want := s.url(), "http://127.0.0.1:"+s.dflt; got != want {
				t.Errorf("%s() with %s=%q = %q, want the fallback %q", s.name, s.portEnv, bad, got, want)
			}
		}
	}
}
