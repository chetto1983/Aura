package auth

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "auth.db")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestIssueLookup_RoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	tok, err := s.Issue(ctx, "u1")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if len(tok) < 40 {
		t.Errorf("token too short: %d chars", len(tok))
	}
	got, err := s.Lookup(ctx, tok)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got != "u1" {
		t.Errorf("user = %q, want u1", got)
	}
}

func TestIssue_RejectsEmptyUser(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Issue(context.Background(), ""); err == nil {
		t.Error("expected error for empty user id")
	}
}

func TestLookup_UnknownToken(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Lookup(context.Background(), "made-up-token"); !errors.Is(err, ErrInvalid) {
		t.Errorf("err = %v, want ErrInvalid", err)
	}
}

func TestLookup_EmptyToken(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Lookup(context.Background(), ""); !errors.Is(err, ErrInvalid) {
		t.Errorf("err = %v, want ErrInvalid", err)
	}
}

func TestLookup_RevokedToken(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	tok, err := s.Issue(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Revoke(ctx, tok); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Lookup(ctx, tok); !errors.Is(err, ErrInvalid) {
		t.Errorf("err = %v, want ErrInvalid", err)
	}
}

func TestRevoke_UnknownToken(t *testing.T) {
	s := newTestStore(t)
	if err := s.Revoke(context.Background(), "made-up-token"); !errors.Is(err, ErrInvalid) {
		t.Errorf("err = %v, want ErrInvalid", err)
	}
}

func TestRevoke_DoubleRevoke(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	tok, err := s.Issue(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Revoke(ctx, tok); err != nil {
		t.Fatal(err)
	}
	if err := s.Revoke(ctx, tok); !errors.Is(err, ErrInvalid) {
		t.Errorf("second revoke err = %v, want ErrInvalid", err)
	}
}

func TestIssue_TokensAreUnique(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seen := make(map[string]struct{})
	for i := range 50 {
		tok, err := s.Issue(ctx, "u1")
		if err != nil {
			t.Fatal(err)
		}
		if _, dup := seen[tok]; dup {
			t.Fatalf("duplicate token at iteration %d", i)
		}
		seen[tok] = struct{}{}
	}
}

func TestMultipleUsers_Isolated(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	tokA, _ := s.Issue(ctx, "alice")
	tokB, _ := s.Issue(ctx, "bob")
	if u, _ := s.Lookup(ctx, tokA); u != "alice" {
		t.Errorf("alice's token resolved to %q", u)
	}
	if u, _ := s.Lookup(ctx, tokB); u != "bob" {
		t.Errorf("bob's token resolved to %q", u)
	}
	// Revoking alice doesn't affect bob.
	if err := s.Revoke(ctx, tokA); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Lookup(ctx, tokB); err != nil {
		t.Errorf("bob's token broke after alice revoke: %v", err)
	}
}

func TestBootstrapUser_ClaimsEmptyAllowlist(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	claimed, err := s.BootstrapUser(ctx, "u1")
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if !claimed {
		t.Fatal("claimed = false, want true")
	}
	ok, err := s.IsUserAllowed(ctx, "u1")
	if err != nil {
		t.Fatalf("allowed lookup: %v", err)
	}
	if !ok {
		t.Fatal("u1 not allowed after bootstrap")
	}
	count, err := s.AllowedUserCount(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("allowed user count = %d, want 1", count)
	}
}

func TestBootstrapUser_OnlyFirstUserWins(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if claimed, err := s.BootstrapUser(ctx, "u1"); err != nil || !claimed {
		t.Fatalf("first bootstrap claimed=%v err=%v, want true nil", claimed, err)
	}
	if claimed, err := s.BootstrapUser(ctx, "u2"); err != nil || claimed {
		t.Fatalf("second bootstrap claimed=%v err=%v, want false nil", claimed, err)
	}
	ok, err := s.IsUserAllowed(ctx, "u2")
	if err != nil {
		t.Fatalf("allowed lookup: %v", err)
	}
	if ok {
		t.Fatal("second user should not be allowed")
	}
	if claimed, err := s.BootstrapUser(ctx, "u1"); err != nil || !claimed {
		t.Fatalf("same bootstrap user claimed=%v err=%v, want true nil", claimed, err)
	}
}
