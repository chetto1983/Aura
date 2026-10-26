package mcptools

import (
	"testing"
)

func TestBrowserArgumentsAreAnAllowlist(t *testing.T) {
	for _, tc := range []struct {
		name string
		args map[string]any
	}{
		{"open", map[string]any{"url": "https://example.com/"}},
		{"open", map[string]any{"url": "about:blank"}},
		{"open", map[string]any{"url": "http://8.8.8.8/"}},
		{"open", map[string]any{"url": "http://[2606:4700:4700::1111]/"}},
		{"fill", map[string]any{"selector": "@e1", "text": "hello"}},
		{"screenshot", map[string]any{"fullPage": true}},
		{"eval", map[string]any{"script": "document.title"}},
	} {
		if err := validateBrowserArguments("agent_browser_"+tc.name, tc.args); err != nil {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
	for _, args := range []map[string]any{
		{"path": "/tmp/key"}, {"screenshotDir": "/run/aura"}, {"restore": "/tmp/state"},
		{"namespace": "other"}, {"caCert": "/run/aura/agent-browser.key"},
		{"extraArgs": []any{"--cdp", "9222"}}, {"futureField": true},
	} {
		if err := validateBrowserArguments("agent_browser_screenshot", args); err == nil {
			t.Errorf("accepted %v", args)
		}
	}
	for _, url := range []string{"file:///run/aura/agent-browser.key", "javascript:alert(1)", "data:text/html,hi", "http://localhost:9222/", "http://127.0.0.1/", "http://[::1]/", "http://[::ffff:127.0.0.1]/", "http://[::ffff:192.168.1.2]/", "http://192.168.1.2/", "http://2130706433/", "http://0x7f000001/", "http://127.1/", "http://127.0.0.1.nip.io/",
		"http://100.64.0.1/", "http://0.1.2.3/", "http://198.51.100.7/", "http://[fec0::1]/",
		"http://[64:ff9b::a9fe:a9fe]/", "http://[2002:a9fe:a9fe::1]/"} {
		if err := validateBrowserArguments("agent_browser_open", map[string]any{"url": url}); err == nil {
			t.Errorf("accepted URL %s", url)
		}
	}
}

func TestBrowserEvalRequiresDestructiveApproval(t *testing.T) {
	if browserRecipeActions["agent_browser_eval"] != MCPActionDestructive {
		t.Fatal("eval must require destructive approval")
	}
}
