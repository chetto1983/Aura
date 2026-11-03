package config

import "testing"

func TestSignInCanDeriveTheConfiguredDesktopPort(t *testing.T) {
	t.Setenv("AURA_MCP_SANDBOX_PORT", "9448")
	if got := loadBase().MCPSandboxPort; got != 9448 {
		t.Fatalf("port=%d", got)
	}
}

func TestSignInImageIsPinnedAndCanBeConfigured(t *testing.T) {
	t.Setenv("AURA_SIGNIN_BROWSER_IMAGE", "")
	const pinned = "lscr.io/linuxserver/chromium@sha256:b147169cc5ff21c62df977f314598537eecf566591c13ad0726b1e5cbd0d01ca"
	if got := loadSandboxConfig().SignInBrowserImage; got != pinned {
		t.Fatalf("default image = %q", got)
	}
	t.Setenv("AURA_SIGNIN_BROWSER_IMAGE", "registry.example/desktop@sha256:abc")
	if got := loadSandboxConfig().SignInBrowserImage; got != "registry.example/desktop@sha256:abc" {
		t.Fatalf("configured image = %q", got)
	}
}
