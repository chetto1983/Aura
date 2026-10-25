package packs

import (
	"strings"
	"testing"
)

// The valid and invalid examples are the specification's own (§5.5).
func TestValidPluginName(t *testing.T) {
	t.Parallel()
	for _, ok := range []string{"my-plugin", "acme.tools", "lint3r", "a", strings.Repeat("a", 64)} {
		if err := ValidPluginName(ok); err != nil {
			t.Errorf("%q refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"My-Plugin", "-start", "end-", "has--double", "too.many..dots", "", strings.Repeat("a", 65), "a b", "a_b", "a/b", ".aura"} {
		if err := ValidPluginName(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

// A connector name becomes a registry key, a tool namespace prefix and a line of the approval
// card, so it carries the plugin-name rule: no underscore at all, hence no "__" separator, and
// no newline.
func TestValidConnectorName(t *testing.T) {
	t.Parallel()
	for _, ok := range []string{"broker-analysis", "slack", "ms365"} {
		if err := ValidConnectorName(ok); err != nil {
			t.Errorf("%q refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"a__b", "Gmail", "x\nsource: evil", "", "a b"} {
		err := ValidConnectorName(bad)
		if err == nil || !strings.Contains(err.Error(), "connector name") {
			t.Errorf("%q: err = %v", bad, err)
		}
	}
}

func TestNormalizeTransport(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"": "stdio", "stdio": "stdio",
		"streamable-http": "streamable_http", "http": "streamable_http", "streamable_http": "streamable_http",
	} {
		if got, err := NormalizeTransport(in); err != nil || got != want {
			t.Errorf("%q -> %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"sse", "ws"} {
		if _, err := NormalizeTransport(bad); err == nil || !strings.Contains(err.Error(), `unsupported transport "`+bad+`"`) {
			t.Errorf("%q: err = %v", bad, err)
		}
	}
}
