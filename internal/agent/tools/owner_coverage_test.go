package tools

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/identityctx"
)

// owner_coverage_test.go unit-covers the daemon-free owner/session + per-turn-id plumbing that
// otherwise only ran under the routed docker_integration paths: the request/tool-call context
// helpers, the local-fallback owner resolution, and the conversation-delete TerminateSession
// leg (MUSR-05) plus its ShellPoll forwarders.

// TestResultContextHelpers covers WithRequestID/RequestIDFromContext + ToolCallIDFromContext.
func TestResultContextHelpers(t *testing.T) {
	t.Parallel()
	base := context.Background()
	if got := RequestIDFromContext(base); got != "" {
		t.Fatalf("RequestIDFromContext(bare) = %q, want empty", got)
	}
	if got := RequestIDFromContext(WithRequestID(base, "req-7")); got != "req-7" {
		t.Fatalf("RequestIDFromContext = %q, want req-7", got)
	}
	if got := ToolCallIDFromContext(base); got != "" {
		t.Fatalf("ToolCallIDFromContext(bare) = %q, want empty", got)
	}
	tc := ctxWithRunDir("sess-oc", "call-9", t.TempDir())
	if got := ToolCallIDFromContext(tc); got != "call-9" {
		t.Fatalf("ToolCallIDFromContext = %q, want call-9", got)
	}
}

// TestOwnerFromContext covers the local-fallback: a bare ctx is `local`, an identity-scoped ctx
// is that principal.
func TestOwnerFromContext(t *testing.T) {
	t.Parallel()
	if got := ownerFromContext(context.Background()); got != localOwnerID {
		t.Fatalf("ownerFromContext(bare) = %q, want local %q", got, localOwnerID)
	}
	ctx := identityctx.WithIdentityID(context.Background(), "principal-1")
	if got := ownerFromContext(ctx); got != "principal-1" {
		t.Fatalf("ownerFromContext(scoped) = %q, want principal-1", got)
	}
}

// TestBackgroundShells_TerminateSession proves the conversation-delete kill leg (MUSR-05 step 4)
// terminates + drops ONLY the (owner, session)-matching running jobs, leaves a same-owner
// different-session job untouched, and is nil-safe + idempotent.
func TestBackgroundShells_TerminateSession(t *testing.T) {
	t.Parallel()
	var killedMatch, killedOther bool
	bg := &BackgroundShells{shells: map[string]*bgShell{
		"jobMatch": {ownerID: "owner-1", sessionID: "sess-1", cancel: func() { killedMatch = true }, bufCap: 1024},
		"jobOther": {ownerID: "owner-1", sessionID: "sess-2", cancel: func() { killedOther = true }, bufCap: 1024},
	}}

	bg.TerminateSession("owner-1", "sess-1")

	if !killedMatch {
		t.Fatal("matching running job was not cancelled")
	}
	if killedOther {
		t.Fatal("a same-owner different-session job was wrongly terminated (session binding breached)")
	}
	if _, ok := bg.get("jobMatch"); ok {
		t.Fatal("terminated job was not dropped from the registry")
	}
	if _, ok := bg.get("jobOther"); !ok {
		t.Fatal("non-matching job was wrongly removed")
	}

	// nil-safe + idempotent: a second call and a nil registry are both no-ops (no panic).
	var nilBG *BackgroundShells
	nilBG.TerminateSession("x", "y")
	bg.TerminateSession("owner-1", "sess-1")
}

// TestShellPoll_RegistryForwarders covers the ShellPoll SessionJobTerminator / SessionEvictor
// forwarders: nil-safe, and delegating to the shared registry.
func TestShellPoll_RegistryForwarders(t *testing.T) {
	t.Parallel()
	// nil-safe on a nil tool and a nil registry.
	var nilPoll *ShellPoll
	nilPoll.TerminateSession("o", "s")
	nilPoll.Evict("s")
	(&ShellPoll{}).TerminateSession("o", "s")
	(&ShellPoll{}).Evict("s")

	// Delegates to the registry.
	var killed bool
	bg := &BackgroundShells{shells: map[string]*bgShell{
		"j": {ownerID: "o", sessionID: "s", cancel: func() { killed = true }, bufCap: 1024},
	}}
	(&ShellPoll{Shells: bg}).TerminateSession("o", "s")
	if !killed {
		t.Fatal("ShellPoll.TerminateSession did not delegate to the registry")
	}
	(&ShellPoll{Shells: bg}).Evict("s") // no-op delegate (registry already drained); must not panic
}
