//go:build db_integration && authula_integration && musr_e2e

// TestTwoRolesTracer is Task 3 of plan 02-01 — the binding proof that Task 1 (the
// wildcard retirement) and Task 2 (the per-identity credential) are one path, not two
// unrelated changes. One provisioned pair, three subtests, on a live disposable
// database (internal/dbtest's guard refuses a `db_integration` DSN named `aura`
// outside CI — this file never weakens it).
//
// Deliberately self-contained under exactly THREE tags (db_integration,
// authula_integration, musr_e2e) — no garage_integration, no arcadedb_integration.
// `make musr-e2e` (scripts/musr_e2e.sh) passes all five tags together, so this file's
// helpers use the tracer* prefix rather than the existing musr* names from
// two_identity_e2e_harness_test.go: that file requires garage_integration too, so
// under THIS file's own narrower tag set (`go vet -tags 'db_integration
// authula_integration musr_e2e' ./cmd/aura/`, per the plan's own <verify>) its helpers
// are not compiled in at all, and reusing their names would collide the moment all
// five tags run together under make musr-e2e.
//
// Run via:
//
//	go test -race -count=1 -p 1 -tags 'db_integration authula_integration musr_e2e' -run TestTwoRolesTracer -v ./cmd/aura/
package main

import (
	"context"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/dbtest"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/identitykey"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/runner"
)

// tracerLocalIdentity is the seeded operator identity (migration 0004) — post-0121 it
// holds the explicit six declared capabilities (Task 1), never the retired wildcard.
const tracerLocalIdentity = "00000000-0000-0000-0000-000000000001"

const tracerIdentityHeader = "X-Tracer-Test-Identity"

// tracerEnvOrSkip mirrors every other db_integration tier in this repo: skip locally,
// t.Fatal under $CI so a missing DSN never reports this tier as falsely green.
func tracerEnvOrSkip(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("musr_e2e tracer requires %s, but it is unset under CI — a skipped "+
				"acceptance tier must not pass as green; wire it in ci.yml", key)
		}
		t.Skipf("musr_e2e tracer requires %s; bring the stack up and set it", key)
	}
	return v
}

// tracerMigratedPool applies roles + migrations against the composed
// AURA_DB_URL/AURA_DB_MIGRATE_URL — NEVER the live `aura` DB (internal/dbtest.MigrateURL
// enforces this) — and returns an aura_app pool ready for the test. Closed via t.Cleanup.
func tracerMigratedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pwd := tracerEnvOrSkip(t, "POSTGRES_PASSWORD")
	migrateURL := dbtest.MigrateURL(t, tracerEnvOrSkip(t, "AURA_DB_MIGRATE_URL"))
	appURL := tracerEnvOrSkip(t, "AURA_DB_URL")
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

// tracerProvisionMember inserts a fresh, non-administrative identity holding EXACTLY
// the four declared user capabilities (identity.UserSet()) — neither
// identity.create nor identity.delete — and returns its UUID. Deleting it at test end
// cascades its grants (FK ON DELETE CASCADE).
func tracerProvisionMember(t *testing.T, pool *pgxpool.Pool, idStore *identity.Store) string {
	t.Helper()
	ctx := context.Background()
	id := uuid.NewString()
	name := "tracer-member-" + id[:8] + "@example.test"
	if _, err := pool.Exec(ctx,
		`INSERT INTO aura.identities (id, name, kind) VALUES ($1::uuid, $2, 'user')`, id, name,
	); err != nil {
		t.Fatalf("provision member identity: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM aura.identities WHERE id = $1::uuid`, id)
	})
	for _, cap := range identity.UserSet() {
		if err := idStore.GrantCapability(ctx, id, cap); err != nil {
			t.Fatalf("grant %q to member: %v", cap, err)
		}
	}
	for _, admin := range identity.Administrative() {
		if ok, err := idStore.HasCapability(ctx, id, admin); err != nil || ok {
			t.Fatalf("member unexpectedly holds administrative capability %q (ok=%v err=%v)", admin, ok, err)
		}
	}
	return id
}

// tracerOnboardingMux builds the SAME shape the composition root mounts for
// POST /api/onboarding/start: agui.RequireAuth wrapping a parent mux whose onboarding
// route is interposed with agui.RequireCapability(identity.CapIdentityCreate) — the
// real gate, not a stand-in. The SessionValidator reads a test header instead of a
// real Authula session cookie, mirroring cmd/aura/two_identity_e2e_test.go's
// http_read_cross_deny subtest (the established pattern for driving RequireAuth +
// RequireCapability in a db_integration test without a live browser login).
func tracerOnboardingMux(t *testing.T, pool *pgxpool.Pool, idStore *identity.Store) *httptest.Server {
	t.Helper()
	server := agui.NewServer(tracerStubRunner{}, conversations.New(pool, conversations.Config{RunDir: t.TempDir(), TurnCapBytes: 65536}), agui.ServerConfig{})
	server.SetOnboardingService(agui.NewOnboardingService(agui.OnboardingDeps{
		Capabilities: idStore,
	}))
	aguiHandler := server.Mux()

	deps := agui.AuthDeps{
		SecretConfigured: true,
		LocalIdentityID:  tracerLocalIdentity,
		Identities:       identityCheckerAdapter{store: idStore},
		SessionValidator: func(r *http.Request) (string, bool) {
			id := r.Header.Get(tracerIdentityHeader)
			return id, id != ""
		},
	}
	parent := http.NewServeMux()
	parent.Handle("POST /api/onboarding/start", agui.RequireCapability(aguiHandler, deps, identity.CapIdentityCreate))
	srv := httptest.NewServer(agui.RequireAuth(parent, deps))
	t.Cleanup(srv.Close)
	return srv
}

// tracerStubRunner satisfies agui.Runner minimally — NewServer needs one, and no
// subtest here drives a real turn (only the onboarding start route, which never
// touches the Runner at all).
type tracerStubRunner struct{}

func (tracerStubRunner) NewConversation(context.Context) (string, error) { return "", nil }

func (tracerStubRunner) DeleteConversationLifecycle(context.Context, string, string) (int64, error) {
	return 0, nil
}

func (tracerStubRunner) Turn(context.Context, string, *string) iter.Seq2[*agent.Event, error] {
	return func(func(*agent.Event, error) bool) {}
}

func (tracerStubRunner) TurnBranch(context.Context, string, int) iter.Seq2[*agent.Event, error] {
	return func(func(*agent.Event, error) bool) {}
}

func (tracerStubRunner) SubmitAnswers(context.Context, map[string]runner.ResponseInput) (int, error) {
	return 0, nil
}

func (tracerStubRunner) ValidateResumeAnswers(context.Context, map[string]runner.ResponseInput) error {
	return nil
}

func (tracerStubRunner) SubmitAnswer(context.Context, string, runner.ResponseInput) (runner.ResolveDirective, error) {
	return runner.ResolveDirective{}, nil
}

func TestTwoRolesTracer(t *testing.T) {
	pool := tracerMigratedPool(t)
	idStore := identity.New(pool)
	memberID := tracerProvisionMember(t, pool, idStore)

	t.Run("admin_creates", func(t *testing.T) {
		srv := tracerOnboardingMux(t, pool, idStore)
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/onboarding/start", strings.NewReader("{}"))
		req.Header.Set(tracerIdentityHeader, tracerLocalIdentity)
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatalf("POST onboarding/start as admin: %v", err)
		}
		defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()
		if resp.StatusCode == http.StatusForbidden {
			t.Fatalf("admin (holding the explicit six) was refused onboarding/start with 403, want admitted")
		}
	})

	t.Run("member_refused", func(t *testing.T) {
		srv := tracerOnboardingMux(t, pool, idStore)
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/onboarding/start", strings.NewReader("{}"))
		req.Header.Set(tracerIdentityHeader, memberID)
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatalf("POST onboarding/start as member: %v", err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("member (holding only the four user capabilities) got %d, want 403", resp.StatusCode)
		}
		bodyStr := string(body)
		if strings.Contains(bodyStr, tracerLocalIdentity) {
			t.Errorf("403 response body leaks the admin identity id: %q", bodyStr)
		}
		for _, cap := range identity.All() {
			if strings.Contains(bodyStr, cap) {
				t.Errorf("403 response body leaks a capability name %q: %q", cap, bodyStr)
			}
		}
	})

	t.Run("own_key_on_the_turn", func(t *testing.T) {
		authulaSecret := tracerEnvOrSkip(t, "AURA_AUTHULA_SECRET")
		keyStore, err := identitykey.NewStore(pool, authulaSecret)
		if err != nil {
			t.Fatalf("identitykey.NewStore: %v", err)
		}
		memberCtx := identityctx.WithIdentityID(context.Background(), memberID)
		memberCap := 15.0
		if err := keyStore.Save(memberCtx, identitykey.Record{
			Key: "sk-or-v1-tracer-" + uuid.NewString(), Hash: "hash-tracer-member", Label: "sk-or-v1-tra...er1", LimitUSD: &memberCap, LimitReset: "monthly",
		}); err != nil {
			t.Fatalf("Save member key: %v", err)
		}
		// The admin identity has no stored key on this fresh database.
		if _, err := keyStore.Load(identityctx.WithIdentityID(context.Background(), tracerLocalIdentity)); err == nil {
			t.Fatal("admin identity unexpectedly has a stored OpenRouter key on a fresh database")
		}

		resolver := runner.NewIdentityLLMResolver(keyStore, nil, llm.Config{Provider: "openrouter", BaseURL: ""}, nil, nil)

		snapMember, err := resolver.SnapshotFor(context.Background(), memberID)
		if err != nil {
			t.Fatalf("SnapshotFor(member): %v", err)
		}
		if snapMember.Config.APIKey == "" || !strings.HasPrefix(snapMember.Config.APIKey, "sk-or-v1-tracer-") {
			t.Fatalf("member's resolved snapshot APIKey = %q, want the stored member key", snapMember.Config.APIKey)
		}

		snapAdmin, err := resolver.SnapshotFor(context.Background(), tracerLocalIdentity)
		if err == nil {
			t.Fatalf("SnapshotFor(admin, no stored key): want an error, got a snapshot with client %v", snapAdmin.Client)
		}
		if snapAdmin.Client != nil {
			t.Fatalf("SnapshotFor(admin, no stored key): want a nil client, got %v", snapAdmin.Client)
		}
		if snapAdmin.Client == snapMember.Client {
			t.Fatal("the refused admin snapshot's client identity equals the member's — must be distinct")
		}
	})
}
