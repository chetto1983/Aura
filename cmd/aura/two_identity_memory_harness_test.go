//go:build db_integration && garage_integration && authula_integration && musr_e2e && arcadedb_integration

// two_identity_memory_harness_test.go carries the memory-plane helpers Task 1 of
// 01-03-PLAN.md adds to TestTwoIdentityCrossDeny (two_identity_e2e_test.go):
// provisioning each per-run identity's ArcadeDB tenant through the SAME production
// resolver newChatTenantClients / arcadedb.TenantClients.For uses, deriving raw
// credentials through arcadedb.NewTenantCredentials + DatabaseFor/TenantUserFor/
// PasswordFor directly (never mocked -- per RESEARCH.md's own "Don't Hand-Roll" table,
// this IS the production mechanism, and testing a stand-in would prove nothing about
// it), and the three assertion bodies memory_cross_deny_identityctx,
// memory_cross_deny_derived_credential and memory_cross_deny_concurrent call into.
//
// This file is NOT two_identity_e2e_harness_test.go: it carries a FIFTH build tag
// (see line 1) the four-tag file does not, so the four-tag file (and the acceptance
// file's own four-tag consumers before this plan) keep compiling regardless of
// whether this plane's stack is up. Every ArcadeDB env name this file reads goes through
// musrEnvOrSkip (defined in the four-tag harness), which t.Fatal's under $CI --
// a missing var must fail the acceptance job loudly, never pass a skipped tier green
// (CLAUDE.md NO-SKIP-AS-GREEN).
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identityctx"
)

// musrMemoryProbeSubject/Predicate are fixed across every write this file makes.
// Isolation here rests on the DATABASE boundary (one ArcadeDB database per identity),
// not on the subject name, so a fixed subject is correct: what varies per assertion is
// the OBJECT, a fresh token unique to that call.
const (
	musrMemoryProbeSubject   = "musr-memory-cross-deny-probe"
	musrMemoryProbePredicate = "carries_token"
)

// musrMemoryPlaneFixture bundles the production resolver (the daemon's own
// newChatTenantClients(cfg)), a raw admin client + tenant credentials for the
// derived-credential surface (D-11 item 2), and the base URL the raw probe dials.
type musrMemoryPlaneFixture struct {
	tenants     *arcadedb.TenantClients
	admin       *arcadedb.Client
	credentials *arcadedb.TenantCredentials
	baseURL     string
}

// musrArcadeDBAdminPassword reads ARCADEDB_ADMIN_PASSWORD, falling back to
// ARCADEDB_PASSWORD -- the same fallback the arcadedb-mcp live-test helper
// (memory_live_integration_helpers_test.go) already establishes, because the
// job env sets only the latter. Whichever branch fires, the eventual read goes
// through musrEnvOrSkip.
func musrArcadeDBAdminPassword(t *testing.T) string {
	t.Helper()
	if v := strings.TrimSpace(os.Getenv("ARCADEDB_ADMIN_PASSWORD")); v != "" {
		return v
	}
	return musrEnvOrSkip(t, "ARCADEDB_PASSWORD")
}

// musrNewMemoryPlaneFixture provisions each per-run identity's ArcadeDB tenant through
// the production path (arcadedb.TenantClients.For auto-provisions its database + server
// user on first successful call, the SAME lazy-provision the daemon relies on) and
// registers a t.Cleanup that drops both tenant databases and their server users. It
// builds the resolver through newChatTenantClients(cfg) -- the daemon's OWN composition
// root, cmd/aura/chat_memory_projection.go -- not a parallel one.
func musrNewMemoryPlaneFixture(t *testing.T, idA, idB string) musrMemoryPlaneFixture {
	t.Helper()
	baseURL := musrEnvOrSkip(t, "ARCADEDB_URL")
	database := musrEnvOrSkip(t, "ARCADEDB_DATABASE")
	adminUser := musrEnvOrSkip(t, "ARCADEDB_ADMIN_USER")
	adminPassword := musrArcadeDBAdminPassword(t)
	musrEnvOrSkip(t, "AURA_ARCADEDB_TENANT_SECRET") // read by arcadedb.NewTenantCredentials below and inside newChatTenantClients
	embedBaseURL := musrEnvOrSkip(t, "AURA_EMBED_BASE_URL")

	cfg := &config.Config{
		ArcadeDB: config.ArcadeDBConfig{
			BaseURL:       baseURL,
			Database:      database,
			AdminUser:     adminUser,
			AdminPassword: adminPassword,
		},
		Embed: config.EmbedConfig{BaseURL: embedBaseURL},
	}
	tenants := newChatTenantClients(cfg)
	if tenants == nil {
		t.Fatalf("newChatTenantClients returned nil with ArcadeDB.BaseURL=%q -- expected a configured resolver", baseURL)
	}

	admin, err := arcadedb.New(arcadedb.Config{
		BaseURL: baseURL, Database: database, User: adminUser, Password: adminPassword,
	})
	if err != nil {
		t.Fatalf("build ArcadeDB admin client: %v", err)
	}
	credentials, err := arcadedb.NewTenantCredentials()
	if err != nil {
		t.Fatalf("arcadedb.NewTenantCredentials: %v", err)
	}

	// Auto-provision both tenants NOW (rather than lazily on first write) so the
	// derived-credential surface (which never goes through TenantClients.For) can
	// assume both databases already exist.
	if _, err := tenants.For(context.Background(), idA); err != nil {
		t.Fatalf("provision A's ArcadeDB tenant: %v", err)
	}
	if _, err := tenants.For(context.Background(), idB); err != nil {
		t.Fatalf("provision B's ArcadeDB tenant: %v", err)
	}

	t.Cleanup(func() {
		musrCleanupArcadeDBTenant(t, admin, idA)
		musrCleanupArcadeDBTenant(t, admin, idB)
	})

	return musrMemoryPlaneFixture{tenants: tenants, admin: admin, credentials: credentials, baseURL: baseURL}
}

// musrCleanupArcadeDBTenant drops identity's tenant database and server user. Errors are
// reported (t.Errorf, not t.Fatalf -- cleanup for the OTHER identity must still run) but
// a "not found"-shaped error is swallowed: a subtest that failed before provisioning
// completed leaves nothing to drop, and that is not itself a cleanup failure.
func musrCleanupArcadeDBTenant(t *testing.T, admin *arcadedb.Client, identity string) {
	t.Helper()
	database, err := arcadedb.DatabaseFor(identity)
	if err != nil {
		t.Errorf("cleanup DatabaseFor(%s): %v", identity, err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := admin.DropDatabase(ctx, database); err != nil && !strings.Contains(strings.ToLower(err.Error()), "not found") {
		t.Errorf("drop tenant database %s: %v", database, err)
	}
	if err := admin.DropUser(ctx, arcadedb.TenantUserFor(database)); err != nil && !strings.Contains(strings.ToLower(err.Error()), "not found") {
		t.Errorf("drop tenant user %s: %v", arcadedb.TenantUserFor(database), err)
	}
}

// musrWriteMemoryToken writes one fact into identity's OWN tenant database, carrying
// token as its object -- a value unique to this call, so a later read can distinguish
// "this fact" from anything else the run wrote.
func musrWriteMemoryToken(t *testing.T, ctx context.Context, tenants *arcadedb.TenantClients, identity, token string) {
	t.Helper()
	client, err := tenants.For(ctx, identity)
	if err != nil {
		t.Fatalf("tenants.For(%s): %v", identity, err)
	}
	if _, err := client.UpsertFact(ctx, arcadedb.Fact{
		Subject: musrMemoryProbeSubject, Predicate: musrMemoryProbePredicate, Object: token,
		Statement: fmt.Sprintf("musr cross-deny probe for %s carries token %s", identity, token),
		Source:    arcadedb.FactSource{RunID: "musr-mem-" + identity, WriterRole: arcadedb.WriterParent},
	}, time.Now()); err != nil {
		t.Fatalf("UpsertFact(%s): %v", identity, err)
	}
}

// musrMemoryProbeFacts reads principal's OWN tenant database for the fixed probe
// subject+predicate, resolving the tenant client through an identityctx stamped for
// principal -- exercising the SAME chain production traffic does: identityctx ->
// TenantClients.For -> DatabaseFor -> the derived credential. It never touches
// *testing.T, so it is safe to call from a goroutine (the concurrent subtest below).
func musrMemoryProbeFacts(ctx context.Context, tenants *arcadedb.TenantClients, principal string) ([]arcadedb.FactHit, error) {
	stamped := identityctx.WithIdentityID(ctx, principal)
	client, err := tenants.For(stamped, identityctx.IdentityID(stamped))
	if err != nil {
		return nil, err
	}
	return client.FactsAbout(ctx, musrMemoryProbeSubject, musrMemoryProbePredicate, 20, time.Time{}, arcadedb.FactsAboutDirect)
}

// musrMemoryHitsContainingToken narrows a FactsAbout result to the hits whose object is
// exactly token -- the fixed probe subject/predicate can carry several tokens across a
// run's several writes, and a cardinality assertion must count only the one this call
// cares about.
func musrMemoryHitsContainingToken(hits []arcadedb.FactHit, token string) []arcadedb.FactHit {
	var out []arcadedb.FactHit
	for _, hit := range hits {
		if hit.Object == token {
			out = append(out, hit)
		}
	}
	return out
}

// musrMemoryCrossDenyIdentityCtx is the memory_cross_deny_identityctx subtest body
// (D-11 item 1). A writes a fact carrying a fresh token; under identityctx stamped for
// B, the resolved client sees ZERO of it; under identityctx stamped for A (the positive
// control, same subtest), it sees at least one.
func musrMemoryCrossDenyIdentityCtx(t *testing.T, fx musrMemoryPlaneFixture, idA, idB string) {
	t.Helper()
	ctx := context.Background()
	token := "musr-mem-idctx-" + uuid.NewString()
	musrWriteMemoryToken(t, ctx, fx.tenants, idA, token)

	bAll, err := musrMemoryProbeFacts(ctx, fx.tenants, idB)
	if err != nil {
		t.Fatalf("B's memory read: %v", err)
	}
	if bHits := musrMemoryHitsContainingToken(bAll, token); len(bHits) != 0 {
		t.Errorf("B's identityctx-resolved client saw %d fact(s) carrying A's token, want 0: %+v", len(bHits), bHits)
	}

	aAll, err := musrMemoryProbeFacts(ctx, fx.tenants, idA)
	if err != nil {
		t.Fatalf("A's memory read: %v", err)
	}
	if aHits := musrMemoryHitsContainingToken(aAll, token); len(aHits) == 0 {
		t.Error("A's identityctx-resolved client saw 0 of its own fact, want at least 1 (positive control)")
	}
}

// musrMemoryCrossDenyDerivedCredential is the memory_cross_deny_derived_credential
// subtest body (D-11 item 2). B's derived credential -- TenantUserFor(DatabaseFor(B))
// with PasswordFor(DatabaseFor(B)) -- aimed directly at A's database over ArcadeDB's
// HTTP API must be refused by the SERVER (a SecurityException naming the user and the
// database), never merely a non-200. The SAME credential aimed at B's own database (the
// positive control, same subtest) must succeed, proving the refusal is about the
// DATABASE and not about the credential itself.
func musrMemoryCrossDenyDerivedCredential(t *testing.T, fx musrMemoryPlaneFixture, idA, idB string) {
	t.Helper()
	databaseA, err := arcadedb.DatabaseFor(idA)
	if err != nil {
		t.Fatalf("DatabaseFor(A): %v", err)
	}
	databaseB, err := arcadedb.DatabaseFor(idB)
	if err != nil {
		t.Fatalf("DatabaseFor(B): %v", err)
	}
	userB := arcadedb.TenantUserFor(databaseB)
	passwordB := fx.credentials.PasswordFor(databaseB)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	bAimedAtA, err := arcadedb.New(arcadedb.Config{
		BaseURL: fx.baseURL, Database: databaseA, User: userB, Password: passwordB,
	})
	if err != nil {
		t.Fatalf("build B-aimed-at-A client: %v", err)
	}
	if _, err := bAimedAtA.Query(ctx, "SELECT 1 AS ok", nil); err == nil {
		t.Fatal("B's derived credential reached A's database, want a server-level SecurityException refusal")
	} else {
		var serverErr *arcadedb.ServerError
		if !errors.As(err, &serverErr) {
			t.Fatalf("B-aimed-at-A error = %v (%T), want *arcadedb.ServerError so the refusal shape can be checked", err, err)
		}
		if !strings.Contains(serverErr.Exception, "SecurityException") {
			t.Errorf("B-aimed-at-A server error exception = %q, want it to name SecurityException", serverErr.Exception)
		}
		if !strings.Contains(serverErr.Detail, userB) || !strings.Contains(serverErr.Detail, databaseA) {
			t.Errorf("B-aimed-at-A server error detail = %q, want it to name the user %q and the database %q",
				serverErr.Detail, userB, databaseA)
		}
	}

	// Positive control: the SAME credential aimed at its OWN database succeeds --
	// proving the refusal above is about the database, not about the credential.
	bAimedAtB, err := arcadedb.New(arcadedb.Config{
		BaseURL: fx.baseURL, Database: databaseB, User: userB, Password: passwordB,
	})
	if err != nil {
		t.Fatalf("build B-aimed-at-B client: %v", err)
	}
	if _, err := bAimedAtB.Query(ctx, "SELECT 1 AS ok", nil); err != nil {
		t.Fatalf("B's derived credential aimed at its OWN database was refused (%v) -- the credential itself must be valid", err)
	}
}

// musrMemoryCrossDenyConcurrent is the memory_cross_deny_concurrent subtest body -- the
// ISO-02 concurrency edge. A's read of its own fact and B's read of A's token are
// issued from two goroutines that overlap in time, joined with a WaitGroup, under
// -race. Both outcomes must hold while both identities are live, not only when they
// take turns.
func musrMemoryCrossDenyConcurrent(t *testing.T, fx musrMemoryPlaneFixture, idA, idB string) {
	t.Helper()
	ctx := context.Background()
	token := "musr-mem-concurrent-" + uuid.NewString()
	musrWriteMemoryToken(t, ctx, fx.tenants, idA, token)

	var wg sync.WaitGroup
	var aAll, bAll []arcadedb.FactHit
	var aErr, bErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		aAll, aErr = musrMemoryProbeFacts(ctx, fx.tenants, idA)
	}()
	go func() {
		defer wg.Done()
		bAll, bErr = musrMemoryProbeFacts(ctx, fx.tenants, idB)
	}()
	wg.Wait()

	if aErr != nil {
		t.Fatalf("A's concurrent memory read: %v", aErr)
	}
	if bErr != nil {
		t.Fatalf("B's concurrent memory read: %v", bErr)
	}
	if aHits := musrMemoryHitsContainingToken(aAll, token); len(aHits) == 0 {
		t.Error("A's concurrent read saw 0 of its own fact, want at least 1")
	}
	if bHits := musrMemoryHitsContainingToken(bAll, token); len(bHits) != 0 {
		t.Errorf("B's concurrent read saw %d of A's fact(s), want 0", len(bHits))
	}
}
