package main

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/browserlogin"
	"github.com/chetto1983/aura/internal/identityctx"
)

func TestSignInViewRefusesExpiredLifecycleBeforeExec(t *testing.T) {
	const session = "signin-0123456789abcdef01234567"
	live := true
	b := &sandboxSignInBrowser{owns: func(context.Context, string, string) bool { return live }}
	s := &signInBrowserSession{owner: "alice", session: session, opts: browserlogin.LaunchOptions{CockpitOrigin: "https://aura.test", StreamOrigin: "https://aura.test:8444"}}
	b.sessions = map[string]*signInBrowserSession{session: s}
	ctx := identityctx.WithIdentityID(context.Background(), "alice")
	if !b.Owns(ctx, "alice", session) {
		t.Fatal("live owner denied")
	}
	live = false
	if b.Owns(ctx, "alice", session) {
		t.Fatal("expired core lifecycle still owns desktop")
	}
	if _, err := b.Dial(ctx, session); err == nil {
		t.Fatal("dial should refuse before nil router")
	}
	live = true
	if b.Owns(ctx, "bob", session) {
		t.Fatal("cross-owner view allowed")
	}
	ctx = identityctx.WithIdentityID(ctx, "bob")
	if b.Owns(ctx, "alice", session) {
		t.Fatal("caller context was ignored")
	}
}

func TestSignInViewCancelsOnlyItsExactRoute(t *testing.T) {
	const session = "signin-0123456789abcdef01234567"
	var route string
	b := &sandboxSignInBrowser{owns: func(context.Context, string, string) bool { return true }, cancel: func(_ context.Context, _ string, r string) error { route = r; return nil }, sessions: map[string]*signInBrowserSession{session: {owner: "alice"}}}
	ctx := identityctx.WithIdentityID(context.Background(), "alice")
	if err := b.Cancel(ctx, "alice", session); err != nil || route != "/signing-in/"+session {
		t.Fatalf("route=%q err=%v", route, err)
	}
}
