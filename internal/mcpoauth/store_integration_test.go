//go:build db_integration

// Live-Postgres test for the per-identity MCP OAuth grant store. It proves the four
// claims the package makes and that nothing but a live engine can prove:
//
//  1. the encrypt-at-rest round trip through the real aura.identity_mcp_oauth table,
//     including that the COLUMN does not contain the token;
//  2. a refresh rewrites the same row and does not accumulate credentials;
//  3. one identity cannot read, overwrite or delete another's grant — enforced by the
//     two RLS policies in migration 0100, not by the WHERE clause;
//  4. the RESTRICTIVE floor denies a write with no principal set.
//
// Bring the stack up and run:
//
//	go test -tags db_integration -run TestGrantStore ./internal/mcpoauth/
//
// A skipped tier must never pass as green (t.Fatal under $CI, per CLAUDE.md).
package mcpoauth

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/dbtest"
	"github.com/chetto1983/aura/internal/identityctx"
)

func oauthEnvOrSkip(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("mcpoauth integration requires %s under CI — a skipped tier must not pass as green", key)
		}
		t.Skipf("mcpoauth integration requires %s", key)
	}
	return v
}

func migratedOAuthPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pwd := oauthEnvOrSkip(t, "POSTGRES_PASSWORD")
	migrateURL := dbtest.MigrateURL(t, oauthEnvOrSkip(t, "AURA_DB_MIGRATE_URL"))
	appURL := oauthEnvOrSkip(t, "AURA_DB_URL")
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
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// seedOAuthIdentity inserts an identity to hang grants off, and registers a cleanup that
// cascades them away.
func seedOAuthIdentity(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	id := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO aura.identities (id, name, kind) VALUES ($1, $2, 'user') ON CONFLICT DO NOTHING`,
		id, "mcpoauth-test-"+id[:8],
	); err != nil {
		t.Fatalf("seed identity: %v", err)
	}
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer ccancel()
		_, _ = pool.Exec(cctx, `DELETE FROM aura.identities WHERE id = $1`, id)
	})
	return id
}

func oauthStore(t *testing.T, pool *pgxpool.Pool) *Store {
	t.Helper()
	s, err := NewStore(pool, oauthEnvOrSkip(t, "AURA_AUTHULA_SECRET"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s
}

func TestGrantStoreRoundTripAndColumnHoldsNoPlaintext(t *testing.T) {
	pool := migratedOAuthPool(t)
	store := oauthStore(t, pool)
	id := seedOAuthIdentity(t, pool)
	ctx := identityctx.WithIdentityID(context.Background(), id)

	want := Grant{
		ServerName:   "slack",
		ResourceURL:  "https://mcp.slack.com/mcp",
		AccessToken:  "xoxp-live-access-" + uuid.NewString(),
		RefreshToken: "xoxr-live-refresh-" + uuid.NewString(),
		ClientInfo:   []byte(`{"client_id":"A1","client_secret":"dcr-minted-secret"}`),
		TokenType:    "Bearer",
		Scopes:       []string{"channels:read", "chat:write"},
		ExpiresAt:    time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond),
	}
	if err := store.Save(ctx, want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := store.Load(ctx, "slack")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.AccessToken != want.AccessToken || got.RefreshToken != want.RefreshToken {
		t.Error("tokens did not survive the round trip")
	}
	if string(got.ClientInfo) != string(want.ClientInfo) {
		t.Errorf("client info = %q, want %q", got.ClientInfo, want.ClientInfo)
	}
	if got.ResourceURL != want.ResourceURL || got.TokenType != want.TokenType {
		t.Errorf("metadata drifted: %+v", got)
	}
	if len(got.Scopes) != 2 || got.Scopes[0] != "channels:read" {
		t.Errorf("scopes = %v", got.Scopes)
	}
	if !got.ExpiresAt.Equal(want.ExpiresAt) {
		t.Errorf("expiry = %v, want %v — an absolute instant must survive a round trip", got.ExpiresAt, want.ExpiresAt)
	}

	// The claim that matters: what is IN the column is not the token. Read the raw bytea
	// as the superuser, bypassing both the store and RLS, so this asserts about storage
	// rather than about the accessor.
	var access, refresh, clientInfo []byte
	withPrincipal(t, pool, id, func(ctx context.Context, tx pgx.Tx) {
		if err := tx.QueryRow(ctx,
			`SELECT access_token_enc, refresh_token_enc, client_info_enc
			 FROM aura.identity_mcp_oauth WHERE identity_id = $1 AND server_name = 'slack'`, id,
		).Scan(&access, &refresh, &clientInfo); err != nil {
			t.Fatalf("read raw ciphertext: %v", err)
		}
	})
	for name, raw := range map[string][]byte{
		"access_token_enc":  access,
		"refresh_token_enc": refresh,
		"client_info_enc":   clientInfo,
	} {
		if len(raw) == 0 {
			t.Errorf("%s is empty — nothing was encrypted", name)
		}
	}
	if strings.Contains(string(access), want.AccessToken) {
		t.Error("access_token_enc contains the access token in the clear")
	}
	if strings.Contains(string(refresh), want.RefreshToken) {
		t.Error("refresh_token_enc contains the refresh token in the clear")
	}
	if strings.Contains(string(clientInfo), "dcr-minted-secret") {
		t.Error("client_info_enc contains the DCR-minted client secret in the clear")
	}
}

// A refresh must rewrite the row, not add one, and must leave created_at alone: that column
// records when the identity first authorized the server, which a refresh does not change.
func TestGrantStoreRefreshRewritesTheSameRow(t *testing.T) {
	pool := migratedOAuthPool(t)
	store := oauthStore(t, pool)
	id := seedOAuthIdentity(t, pool)
	ctx := identityctx.WithIdentityID(context.Background(), id)

	first := Grant{ServerName: "notion", ResourceURL: "https://mcp.notion.com/mcp", AccessToken: "first-token"}
	if err := store.Save(ctx, first); err != nil {
		t.Fatalf("Save first: %v", err)
	}
	created := grantCreatedAt(t, pool, id, "notion")

	second := first
	second.AccessToken = "second-token"
	second.ExpiresAt = time.Now().UTC().Add(2 * time.Hour)
	if err := store.Save(ctx, second); err != nil {
		t.Fatalf("Save second: %v", err)
	}

	list, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("List returned %d rows, want 1 — a refresh accumulated credentials", len(list))
	}
	got, err := store.Load(ctx, "notion")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.AccessToken != "second-token" {
		t.Errorf("access token = %q, want the refreshed one", got.AccessToken)
	}
	if again := grantCreatedAt(t, pool, id, "notion"); !again.Equal(created) {
		t.Errorf("created_at moved on refresh (%v -> %v); it records first authorization", created, again)
	}
}

// The isolation claim, on the engine. Storage-enforced, not WHERE-enforced: the reads below
// go through the store, whose queries carry the identity, but the policies are what make a
// forged one useless — proven by the raw cross-identity UPDATE at the end.
func TestGrantStoreDeniesCrossIdentityAccess(t *testing.T) {
	pool := migratedOAuthPool(t)
	store := oauthStore(t, pool)
	alice := seedOAuthIdentity(t, pool)
	bob := seedOAuthIdentity(t, pool)
	aliceCtx := identityctx.WithIdentityID(context.Background(), alice)
	bobCtx := identityctx.WithIdentityID(context.Background(), bob)

	if err := store.Save(aliceCtx, Grant{
		ServerName: "linear", ResourceURL: "https://mcp.linear.app/mcp", AccessToken: "alice-token",
	}); err != nil {
		t.Fatalf("Save as alice: %v", err)
	}

	if _, err := store.Load(bobCtx, "linear"); !errors.Is(err, ErrNoGrant) {
		t.Fatalf("bob read alice's grant: err = %v, want ErrNoGrant", err)
	}
	if list, err := store.List(bobCtx); err != nil || len(list) != 0 {
		t.Fatalf("bob listed %d of alice's grants (err=%v), want 0", len(list), err)
	}
	// Deleting by the same server name must not reach across. A false "revoked" is worse
	// than a failure: the operator would believe a live token is gone.
	if deleted, err := store.Delete(bobCtx, "linear"); err != nil || deleted {
		t.Fatalf("bob's delete reported deleted=%v (err=%v), want false", deleted, err)
	}
	if _, err := store.Load(aliceCtx, "linear"); err != nil {
		t.Fatalf("alice's grant did not survive bob's delete: %v", err)
	}

	// Now the policy itself, not the accessor: a write scoped to bob that names alice's
	// row must touch nothing.
	withPrincipal(t, pool, bob, func(ctx context.Context, tx pgx.Tx) {
		tag, err := tx.Exec(ctx,
			`UPDATE aura.identity_mcp_oauth SET access_token_enc = '\xff' WHERE identity_id = $1`, alice)
		if err != nil {
			t.Fatalf("cross-identity update: %v", err)
		}
		if tag.RowsAffected() != 0 {
			t.Fatalf("a write scoped to bob modified %d of alice's rows", tag.RowsAffected())
		}
	})
}

// The RESTRICTIVE floor from 0087, on this table. Without it the permissive owner policy
// alone would let a caller with no principal see everything.
func TestGrantStoreWriteWithNoPrincipalIsDeniedByRLS(t *testing.T) {
	pool := migratedOAuthPool(t)
	id := seedOAuthIdentity(t, pool)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := pool.Exec(ctx,
		`INSERT INTO aura.identity_mcp_oauth (identity_id, server_name, resource_url, access_token_enc)
		 VALUES ($1, 'floor-probe', 'https://example.invalid/mcp', '\x00')`, id)
	if err == nil {
		t.Fatal("a write with no app.current_identity succeeded — the RESTRICTIVE floor is not in place")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "row-level security") {
		t.Fatalf("write failed for the wrong reason: %v", err)
	}
}

func TestGrantStoreDeleteReportsWhetherARowWent(t *testing.T) {
	pool := migratedOAuthPool(t)
	store := oauthStore(t, pool)
	id := seedOAuthIdentity(t, pool)
	ctx := identityctx.WithIdentityID(context.Background(), id)

	if deleted, err := store.Delete(ctx, "never-authorized"); err != nil || deleted {
		t.Fatalf("delete of an absent grant reported deleted=%v (err=%v), want false", deleted, err)
	}
	if err := store.Save(ctx, Grant{ServerName: "atlassian", AccessToken: "t"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if deleted, err := store.Delete(ctx, "atlassian"); err != nil || !deleted {
		t.Fatalf("delete of a present grant reported deleted=%v (err=%v), want true", deleted, err)
	}
	if _, err := store.Load(ctx, "atlassian"); !errors.Is(err, ErrNoGrant) {
		t.Fatalf("grant survived its own deletion: %v", err)
	}
}

// A server with no refresh token and no DCR result must round-trip as ABSENT, not as an
// empty string: "the server issued none" and "the server issued an empty one" are
// different facts, and only the first is true here.
func TestGrantStoreKeepsAbsentOptionalsAbsent(t *testing.T) {
	pool := migratedOAuthPool(t)
	store := oauthStore(t, pool)
	id := seedOAuthIdentity(t, pool)
	ctx := identityctx.WithIdentityID(context.Background(), id)

	if err := store.Save(ctx, Grant{
		ServerName: "minimal", ResourceURL: "https://mcp.example.invalid/mcp", AccessToken: "only-access",
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := store.Load(ctx, "minimal")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.RefreshToken != "" {
		t.Errorf("refresh token = %q, want empty", got.RefreshToken)
	}
	if got.ClientInfo != nil {
		t.Errorf("client info = %v, want nil", got.ClientInfo)
	}
	if got.TokenType != "Bearer" {
		t.Errorf("token type = %q, want the Bearer default", got.TokenType)
	}
	if !got.ExpiresAt.IsZero() {
		t.Errorf("expiry = %v, want zero when the server issued none", got.ExpiresAt)
	}
}

// withPrincipal runs fn inside a transaction with app.current_identity set, which is the
// only way to reach these rows: SET LOCAL is scoped to a transaction, and pgx prepares
// every statement, so SET and query cannot ride one Exec. Deliberately raw rather than
// through internal/db.WithIdentityTx — these assertions are ABOUT the policies, so they
// must not be written through the helper whose correctness they are checking.
func withPrincipal(t *testing.T, pool *pgxpool.Pool, identityID string, fn func(context.Context, pgx.Tx)) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SELECT set_config('app.current_identity', $1, true)", identityID); err != nil {
		t.Fatalf("set principal: %v", err)
	}
	fn(ctx, tx)
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

func grantCreatedAt(t *testing.T, pool *pgxpool.Pool, identityID, server string) time.Time {
	t.Helper()
	var at time.Time
	withPrincipal(t, pool, identityID, func(ctx context.Context, tx pgx.Tx) {
		if err := tx.QueryRow(ctx,
			`SELECT created_at FROM aura.identity_mcp_oauth WHERE identity_id = $1 AND server_name = $2`,
			identityID, server,
		).Scan(&at); err != nil {
			t.Fatalf("read created_at: %v", err)
		}
	})
	return at
}
