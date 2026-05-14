package auth

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	auradb "github.com/aura/aura/internal/db"
)

var (
	_ TokenReader         = (*Store)(nil)
	_ TokenIssuer         = (*Store)(nil)
	_ TokenRevoker        = (*Store)(nil)
	_ TokenWriter         = (*Store)(nil)
	_ TokenRepository     = (*Store)(nil)
	_ AccessReader        = (*Store)(nil)
	_ AccessWriter        = (*Store)(nil)
	_ PendingReader       = (*Store)(nil)
	_ DashboardRepository = (*Store)(nil)
	_ Repository          = (*Store)(nil)
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

func TestNewStoreWithDBRejectsNil(t *testing.T) {
	if _, err := NewStoreWithDB(nil); err == nil {
		t.Fatal("NewStoreWithDB(nil) error = nil, want error")
	}
}

func TestNewStoreWithDBDoesNotCreateSchema(t *testing.T) {
	db, err := auradb.Open(filepath.Join(t.TempDir(), "shared.db"))
	if err != nil {
		t.Fatalf("open shared db: %v", err)
	}
	defer db.Close()

	if _, err := NewStoreWithDB(db); err != nil {
		t.Fatalf("NewStoreWithDB: %v", err)
	}

	if tableExists(t, db, "api_tokens") {
		t.Fatal("NewStoreWithDB created api_tokens; migrations should own schema")
	}
}

func tableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var got string
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE name = ?`, name).Scan(&got)
	if err == nil {
		return true
	}
	if err == sql.ErrNoRows {
		return false
	}
	t.Fatalf("query sqlite_master: %v", err)
	return false
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

func TestIssueSetsExpiresAt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 5, 5, 8, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }

	tok, err := s.Issue(ctx, "u1")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	var expiresAt string
	if err := s.db.QueryRowContext(ctx,
		`SELECT expires_at FROM api_tokens WHERE token_hash = ?`,
		hashToken(tok),
	).Scan(&expiresAt); err != nil {
		t.Fatalf("query expires_at: %v", err)
	}
	want := now.Add(30 * 24 * time.Hour).Format(time.RFC3339)
	if expiresAt != want {
		t.Fatalf("expires_at = %q, want %q", expiresAt, want)
	}
}

func TestLookupExpiredToken(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 5, 5, 8, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }

	tok, err := s.Issue(ctx, "u1")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	s.now = func() time.Time { return now.Add(30*24*time.Hour + time.Second) }

	if _, err := s.Lookup(ctx, tok); !errors.Is(err, ErrExpired) {
		t.Fatalf("lookup err = %v, want ErrExpired", err)
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

func TestLookupPrimaryFailureAfterDroppedAPITokens(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	tok, err := s.Issue(ctx, "u1")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, `DROP TABLE api_tokens`); err != nil {
		t.Fatalf("drop api_tokens: %v", err)
	}

	userID, err := s.Lookup(ctx, tok)
	if err == nil {
		t.Fatal("lookup err = nil, want query failure after dropped table")
	}
	if userID != "" {
		t.Fatalf("userID = %q, want empty when primary lookup fails", userID)
	}
}

func TestLookupSurfacesLastUsedAuditFailureWithoutDenyingToken(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	tok, err := s.Issue(ctx, "u1")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	s.updateLastUsed = func(context.Context, string, string) error {
		return errors.New("database locked")
	}

	userID, err := s.Lookup(ctx, tok)
	if userID != "u1" {
		t.Fatalf("userID = %q, want u1", userID)
	}
	var auditErr *AuditUpdateError
	if !errors.As(err, &auditErr) {
		t.Fatalf("err = %v, want AuditUpdateError", err)
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

func TestBootstrapUser_ClosesOwnPendingRequest(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, err := s.RequestAccess(ctx, "owner", "ownername"); err != nil {
		t.Fatalf("request access: %v", err)
	}
	if claimed, err := s.BootstrapUser(ctx, "owner"); err != nil || !claimed {
		t.Fatalf("bootstrap claimed=%v err=%v, want true nil", claimed, err)
	}
	pending, err := s.ListPending(ctx)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending after bootstrap = %+v, want empty", pending)
	}
}

func TestBootstrapE2EUser_DoesNotBlockFirstRealOwner(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if claimed, err := s.BootstrapE2EUser(ctx, "e2e"); err != nil || !claimed {
		t.Fatalf("e2e bootstrap claimed=%v err=%v, want true nil", claimed, err)
	}
	if claimed, err := s.BootstrapUser(ctx, "owner"); err != nil || !claimed {
		t.Fatalf("real bootstrap claimed=%v err=%v, want true nil", claimed, err)
	}
	ok, err := s.IsUserAllowed(ctx, "owner")
	if err != nil {
		t.Fatalf("allowed lookup: %v", err)
	}
	if !ok {
		t.Fatal("real owner should be allowed after e2e bootstrap")
	}
}

func TestBootstrapE2EUser_DoesNotBypassExistingRealOwner(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if claimed, err := s.BootstrapUser(ctx, "owner"); err != nil || !claimed {
		t.Fatalf("real bootstrap claimed=%v err=%v, want true nil", claimed, err)
	}
	if claimed, err := s.BootstrapE2EUser(ctx, "e2e"); err != nil || claimed {
		t.Fatalf("e2e bootstrap claimed=%v err=%v, want false nil", claimed, err)
	}
	ok, err := s.IsUserAllowed(ctx, "e2e")
	if err != nil {
		t.Fatalf("allowed lookup: %v", err)
	}
	if ok {
		t.Fatal("e2e user should not be added after a real owner exists")
	}
}
