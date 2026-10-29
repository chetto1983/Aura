//go:build db_integration

// Integration tests for internal/channels/telegram.Store. Requires a running
// Postgres with the migrations applied through 0012 (telegram_accounts +
// telegram_setup_pending):
//
//	make db-up && aura db migrate           # or the WSL equivalent
//	AURA_DB_URL + AURA_DB_MIGRATE_URL + POSTGRES_PASSWORD set in env
//
// Run via:
//
//	go test -tags db_integration -race ./internal/channels/telegram
//
// No-skip-as-green: envOrSkip t.Fatals under $CI when the DSN is unset. The live
// round-trip is the authoritative Wave-1 post-build gate the orchestrator runs;
// this file is the compile-and-assert contract.
package telegram

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/dbtest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// localID is the seeded `local` identity (migration 0004), the FK parent for the
// onboarding tokens + accounts these tests create.
const localID = "00000000-0000-0000-0000-000000000001"

func envOrSkip(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("integration test requires %s, but it is unset under CI — "+
				"a skipped integration test must not pass as green; wire it in ci.yml", key)
		}
		t.Skipf("integration test requires %s; set it and re-run (e.g. via .env + make db-up)", key)
	}
	return v
}

func migratedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(ownerCtx(), 30*time.Second)
	defer cancel()

	pwd := envOrSkip(t, "POSTGRES_PASSWORD")
	migrateURL := dbtest.MigrateURL(t, envOrSkip(t, "AURA_DB_MIGRATE_URL"))
	appURL := envOrSkip(t, "AURA_DB_URL")

	host := os.Getenv("PGHOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("PGPORT")
	if port == "" {
		port = "5432"
	}
	bootstrap := fmt.Sprintf("postgres://aura:%s@%s:%s/aura?sslmode=disable", pwd, host, port)

	if err := db.EnsureRoles(ctx, bootstrap, pwd); err != nil {
		t.Fatalf("EnsureRoles: %v", err)
	}
	if _, err := db.Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	pool, err := db.Open(ctx, &db.Config{URL: appURL})
	if err != nil {
		t.Fatalf("Open (aura_app): %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// freshToken inserts a pending onboarding token with the given TTL offset and
// registers cleanup. Returns the token string.
func freshToken(t *testing.T, s *Store, ttl time.Duration) string {
	t.Helper()
	tok := uuid.Must(uuid.NewV7()).String()
	if err := s.InsertPending(ownerCtx(), InsertPendingParams{
		OnboardingToken: tok,
		IdentityID:      localID,
		GeneratedBy:     "integration-test",
		ExpiresAt:       time.Now().Add(ttl),
	}); err != nil {
		t.Fatalf("InsertPending: %v", err)
	}
	t.Cleanup(func() {
		_, _ = s.pool.Exec(ownerCtx(),
			"DELETE FROM aura.telegram_setup_pending WHERE onboarding_token = $1", tok)
	})
	return tok
}

// cleanupAccount deletes the link under the identity that owns it: under the links' owner
// isolation, a DELETE with no identity bound matches no row and reports no error.
func cleanupAccount(t *testing.T, s *Store, tgUserID int64) {
	t.Helper()
	t.Cleanup(func() {
		acct, err := s.GetAccountByTelegramID(ownerCtx(), tgUserID)
		if err != nil {
			return
		}
		_ = db.WithIdentityTxRaw(ownerCtx(), s.pool, acct.IdentityID, func(tx pgx.Tx) error {
			_, err := tx.Exec(ownerCtx(), "DELETE FROM aura.telegram_accounts WHERE telegram_user_id = $1", tgUserID)
			return err
		})
	})
}

// The activity mark is a write, so it runs under the account's identity, and the mark
// must move: an UPDATE that RLS hides matches nothing and still reports success.
func TestTouchLastSeenMovesTheMark(t *testing.T) {
	pool := migratedPool(t)
	s := New(pool)
	ctx := ownerCtx()
	const tgUserID int64 = 9_000_030_001
	cleanupAccount(t, s, tgUserID)
	if _, err := s.ConsumeOnboarding(ctx, ConsumeParams{OnboardingToken: freshToken(t, s, time.Hour), TelegramUserID: tgUserID}); err != nil {
		t.Fatalf("ConsumeOnboarding: %v", err)
	}
	if acct, err := s.GetAccountByTelegramID(ctx, tgUserID); err != nil || !acct.LastSeenAt.IsZero() {
		t.Fatalf("fresh link = (%+v, %v), want one never seen", acct, err)
	}

	if err := s.TouchLastSeen(ctx, localID, tgUserID); err != nil {
		t.Fatalf("TouchLastSeen: %v", err)
	}
	if acct, err := s.GetAccountByTelegramID(ctx, tgUserID); err != nil || acct.LastSeenAt.IsZero() {
		t.Fatalf("after TouchLastSeen = (%+v, %v), want last_seen_at set", acct, err)
	}
}

// TestOnboardingRoundTrip is the UX-03 contract: InsertPending → ConsumeOnboarding
// persists the account + consumes the token; re-consuming the same token returns
// ErrTokenConsumed (single-use, T-13-01-TokenReplay) and writes no second account.
func TestOnboardingRoundTrip(t *testing.T) {
	pool := migratedPool(t)
	s := New(pool)
	ctx := ownerCtx()

	tok := freshToken(t, s, time.Hour)
	const tgUserID int64 = 9_000_000_001
	cleanupAccount(t, s, tgUserID)

	acct, err := s.ConsumeOnboarding(ctx, ConsumeParams{
		OnboardingToken: tok,
		TelegramUserID:  tgUserID,
		Username:        "tester",
		FirstName:       "Aura",
	})
	if err != nil {
		t.Fatalf("ConsumeOnboarding: %v", err)
	}
	if acct.TelegramUserID != tgUserID || acct.IdentityID != localID || acct.Username != "tester" {
		t.Errorf("consumed account mismatch: %+v", acct)
	}

	// The account row is durably present (fresh Store sees it).
	got, err := New(pool).GetAccountByTelegramID(ctx, tgUserID)
	if err != nil {
		t.Fatalf("GetAccountByTelegramID after consume: %v", err)
	}
	if got.TelegramUserID != tgUserID {
		t.Errorf("account not persisted: %+v", got)
	}

	// The token is consumed.
	consumed, err := s.PendingConsumed(ctx, tok)
	if err != nil {
		t.Fatalf("PendingConsumed: %v", err)
	}
	if !consumed {
		t.Error("token must be marked consumed after ConsumeOnboarding")
	}

	// Re-consume of the same token is rejected single-use, and writes no duplicate.
	_, err = s.ConsumeOnboarding(ctx, ConsumeParams{
		OnboardingToken: tok,
		TelegramUserID:  9_000_000_002,
		Username:        "replay",
	})
	if !errors.Is(err, ErrTokenConsumed) {
		t.Fatalf("re-consume: want ErrTokenConsumed, got %v", err)
	}
	cleanupAccount(t, s, 9_000_000_002)
	if _, err := s.GetAccountByTelegramID(ctx, 9_000_000_002); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("replay consume must not create an account, got err=%v", err)
	}
}

// TestConsumeExpiredToken: an expired (but unconsumed) token is rejected by the
// same single-use chokepoint (expires_at > now() guard), classified ErrTokenConsumed.
func TestConsumeExpiredToken(t *testing.T) {
	pool := migratedPool(t)
	s := New(pool)
	ctx := ownerCtx()

	tok := freshToken(t, s, -time.Minute) // already expired
	_, err := s.ConsumeOnboarding(ctx, ConsumeParams{OnboardingToken: tok, TelegramUserID: 9_000_000_010})
	if !errors.Is(err, ErrTokenConsumed) {
		t.Fatalf("consume expired token: want ErrTokenConsumed, got %v", err)
	}
}

// TestConsumeUnknownToken: an unknown token matches no row → ErrTokenConsumed (the
// consume path collapses unknown/spent/expired; PendingConsumed distinguishes
// unknown via ErrTokenNotFound).
func TestConsumeUnknownToken(t *testing.T) {
	pool := migratedPool(t)
	s := New(pool)
	ctx := ownerCtx()

	unknown := uuid.Must(uuid.NewV7()).String()
	if _, err := s.ConsumeOnboarding(ctx, ConsumeParams{OnboardingToken: unknown, TelegramUserID: 9_000_000_020}); !errors.Is(err, ErrTokenConsumed) {
		t.Errorf("consume unknown token: want ErrTokenConsumed, got %v", err)
	}
	if _, err := s.PendingConsumed(ctx, unknown); !errors.Is(err, ErrTokenNotFound) {
		t.Errorf("PendingConsumed(unknown): want ErrTokenNotFound, got %v", err)
	}
}

// TestDuplicateAccountClassified: consuming a second valid token for an already
// onboarded telegram_user_id hits the PK and classifies ErrAccountExists via
// SQLSTATE 23505 (never message-match); the tx rolls back so the second token
// stays unconsumed.
func TestDuplicateAccountClassified(t *testing.T) {
	pool := migratedPool(t)
	s := New(pool)
	ctx := ownerCtx()

	const tgUserID int64 = 9_000_000_030
	cleanupAccount(t, s, tgUserID)

	tok1 := freshToken(t, s, time.Hour)
	if _, err := s.ConsumeOnboarding(ctx, ConsumeParams{OnboardingToken: tok1, TelegramUserID: tgUserID, Username: "first"}); err != nil {
		t.Fatalf("first ConsumeOnboarding: %v", err)
	}

	tok2 := freshToken(t, s, time.Hour)
	_, err := s.ConsumeOnboarding(ctx, ConsumeParams{OnboardingToken: tok2, TelegramUserID: tgUserID, Username: "dup"})
	if !errors.Is(err, ErrAccountExists) {
		t.Fatalf("duplicate account: want ErrAccountExists, got %v", err)
	}
	// The second token rolled back unconsumed (consume-and-INSERT atomicity).
	consumed, err := s.PendingConsumed(ctx, tok2)
	if err != nil {
		t.Fatalf("PendingConsumed(tok2): %v", err)
	}
	if consumed {
		t.Error("a duplicate-account rollback must leave the second token unconsumed")
	}
}

// TestCleanupExpired removes stale unconsumed tokens and leaves live + consumed
// ones untouched, returning the deleted count.
func TestCleanupExpired(t *testing.T) {
	pool := migratedPool(t)
	s := New(pool)
	ctx := ownerCtx()

	stale := freshToken(t, s, -time.Hour) // expired, unconsumed → swept
	live := freshToken(t, s, time.Hour)   // not expired → kept

	n, err := s.CleanupExpired(ctx)
	if err != nil {
		t.Fatalf("CleanupExpired: %v", err)
	}
	if n < 1 {
		t.Errorf("CleanupExpired should remove at least the stale token, got %d", n)
	}

	// stale is gone; live survives.
	if _, err := s.PendingConsumed(ctx, stale); !errors.Is(err, ErrTokenNotFound) {
		t.Errorf("stale token must be swept, got err=%v", err)
	}
	if _, err := s.PendingConsumed(ctx, live); err != nil {
		t.Errorf("live token must survive cleanup: %v", err)
	}
}

// TestCountAccounts counts every link: the setup footer reads it with no identity bound.
func TestCountAccounts(t *testing.T) {
	pool := migratedPool(t)
	s := New(pool)
	ctx := ownerCtx()

	before, err := s.CountAccounts(ctx)
	if err != nil {
		t.Fatalf("CountAccounts: %v", err)
	}

	const tgUserID int64 = 9_000_000_040
	cleanupAccount(t, s, tgUserID)
	tok := freshToken(t, s, time.Hour)
	if _, err := s.ConsumeOnboarding(ctx, ConsumeParams{OnboardingToken: tok, TelegramUserID: tgUserID, Username: "counted"}); err != nil {
		t.Fatalf("ConsumeOnboarding: %v", err)
	}

	after, err := s.CountAccounts(ctx)
	if err != nil {
		t.Fatalf("CountAccounts after: %v", err)
	}
	if after != before+1 {
		t.Errorf("CountAccounts: want %d, got %d", before+1, after)
	}
}

// The owner's list and unlink run under the owner's identity and also filter on it: the WHERE
// clause is the application filter, and row security the backstop for a forgotten one.
func TestLinkedAccountsAndUnlinkStayWithTheirOwner(t *testing.T) {
	pool := migratedPool(t)
	s := New(pool)
	ctx := ownerCtx()
	idA, idB := userIdentity(t, pool, "links-a"), userIdentity(t, pool, "links-b")
	const linkA1, linkA2, linkB int64 = 9_000_070_001, 9_000_070_002, 9_000_070_003
	linkThroughOnboarding(t, s, idA, linkA2)
	linkThroughOnboarding(t, s, idA, linkA1)
	linkThroughOnboarding(t, s, idB, linkB)

	got, err := s.LinkedAccounts(ctx, idA)
	if err != nil {
		t.Fatalf("LinkedAccounts(A): %v", err)
	}
	if len(got) != 2 || got[0].TelegramUserID != linkA2 || got[1].TelegramUserID != linkA1 || got[0].IdentityID != idA {
		t.Fatalf("LinkedAccounts(A) = %+v, want %d then %d, both A's, oldest first", got, linkA2, linkA1)
	}
	if none, err := s.LinkedAccounts(ctx, "local"); err != nil || len(none) != 0 {
		t.Fatalf("LinkedAccounts(local) = %+v, %v; want none: a non-UUID identity owns no link", none, err)
	}

	if ok, err := s.Unlink(ctx, idA, linkB); err != nil || ok {
		t.Fatalf("Unlink(A, B's link) = %v, %v; want false", ok, err)
	}
	if acct, err := s.GetAccountByIdentity(ctx, idB); err != nil || acct.TelegramUserID != linkB {
		t.Fatalf("B's link after A's unlink = %+v, %v; want it still there", acct, err)
	}
	if ok, err := s.Unlink(ctx, idA, linkA1); err != nil || !ok {
		t.Fatalf("Unlink(A, its own link) = %v, %v; want true", ok, err)
	}
	if _, err := s.GetAccountByTelegramID(ctx, linkA1); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("A's unlinked account still resolves: %v", err)
	}
}

// userIdentity seeds a user identity; deleting it in cleanup takes its links with it.
func userIdentity(t *testing.T, pool *pgxpool.Pool, name string) string {
	t.Helper()
	id := uuid.NewString()
	if _, err := pool.Exec(ownerCtx(), "INSERT INTO aura.identities (id, name, kind) VALUES ($1, $2, 'user')", id, name+"-"+id); err != nil {
		t.Fatalf("seed identity %s: %v", name, err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(ownerCtx(), "DELETE FROM aura.identities WHERE id = $1", id); err != nil {
			t.Errorf("delete identity %s: %v", name, err)
		}
	})
	return id
}

// linkThroughOnboarding links a chat to identityID the way /start does: a minted token, consumed.
func linkThroughOnboarding(t *testing.T, s *Store, identityID string, chat int64) {
	t.Helper()
	token := uuid.NewString()
	if err := s.InsertPending(ownerCtx(), InsertPendingParams{
		OnboardingToken: token, IdentityID: identityID, ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("mint a token for %d: %v", chat, err)
	}
	if _, err := s.ConsumeOnboarding(ownerCtx(), ConsumeParams{OnboardingToken: token, TelegramUserID: chat}); err != nil {
		t.Fatalf("link %d: %v", chat, err)
	}
}
