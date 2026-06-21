package manager

import (
	"testing"

	"github.com/chetto1983/aura/internal/mcp"
)

// denied_mount_test.go is the no-silent-destructive-mount backstop (SPEC Prohibition #5 /
// T-29-05). When an allowlist is present (some servers are trusted/runnable), a DENIED
// (TrustBlocked) server — the destructive/un-vetted case — must be BOTH (a) absent from the
// runtime registry (RunnableManagedServers never returns it, so it is never launched/mounted)
// AND (b) explicitly surfaced (SnapshotStatus reports it as StartupBlocked, never silently
// dropped). The Phase-16 mount policy is unchanged; this test pins the invariant the phase
// must not regress.

// TestDeniedToolNotMounted proves a denied server is excluded from the runtime registry yet
// explicitly surfaced, alongside an allowlisted (trusted) sibling that IS mounted.
func TestDeniedToolNotMounted(t *testing.T) {
	doc := mcp.ManagedConfig{
		Profiles: map[string]mcp.ManagedProfile{
			"default": {Servers: []string{"trusted-allowed", "denied-destructive"}},
		},
		ActiveProfile: "default",
		MCPServers: map[string]mcp.ManagedServer{
			// The allowlist: an operator-trusted server that IS eligible to mount.
			"trusted-allowed": {
				Command: "uvx",
				Source:  "recipe:calculator",
				Trust:   mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
			},
			// A denied/destructive server (trust=blocked): must NOT mount, but must surface.
			"denied-destructive": {
				Command: "node",
				Source:  "manual",
				Trust:   mcp.ManagedTrust{Class: mcp.TrustBlocked},
			},
		},
	}

	// (a) absent from the runtime registry — never launched/mounted.
	runnable, err := RunnableManagedServers(doc)
	if err != nil {
		t.Fatalf("RunnableManagedServers: %v", err)
	}
	if _, mounted := runnable["denied-destructive"]; mounted {
		t.Fatalf("denied server is in the runtime registry — it would mount silently (Prohibition #5 VIOLATED): %#v", runnable)
	}
	if _, mounted := runnable["trusted-allowed"]; !mounted {
		t.Fatalf("the allowlisted (trusted) sibling must still mount: %#v", runnable)
	}

	// RuntimeServers (the concrete launch-config registry) must also exclude the denied server.
	cfgs, err := RuntimeServers(doc)
	if err != nil {
		t.Fatalf("RuntimeServers: %v", err)
	}
	if _, mounted := cfgs["denied-destructive"]; mounted {
		t.Fatalf("denied server resolved a runtime launch config — it would mount silently: %#v", cfgs)
	}

	// (b) explicitly surfaced — the operator sees it as blocked, never a silent omission.
	var surfaced bool
	for _, snap := range SnapshotStatus(doc) {
		if snap.Name == "denied-destructive" {
			surfaced = true
			if snap.StartupState != StartupBlocked {
				t.Errorf("denied server surfaced with state %q, want %q (must be explicitly flagged blocked)", snap.StartupState, StartupBlocked)
			}
			if snap.Trust != mcp.TrustBlocked {
				t.Errorf("denied server surfaced with trust %q, want %q", snap.Trust, mcp.TrustBlocked)
			}
		}
	}
	if !surfaced {
		t.Fatal("denied server was NOT surfaced in SnapshotStatus — a silently-dropped destructive server is exactly what Prohibition #5 forbids")
	}
}
