//go:build db_integration

// Integration proofs for the Phase-36 owner-RLS backstop (migration 0032, MUSR-01 / D-07).
// Requires a running Postgres with the migrations applied through 0032:
//
//	make db-up && aura db migrate      # or the WSL equivalent
//	AURA_DB_URL + AURA_DB_MIGRATE_URL + POSTGRES_PASSWORD set in env
//
// Run via:
//
//	go test -tags db_integration -race -run TestRLS ./internal/db -count=1
//
// No-skip-as-green: db_test.go's envOrSkip t.Fatals under $CI when a DSN is unset, so a
// skipped tier can never pass as green. This file reuses that harness (envOrSkip /
// bootstrapURL / Open / Migrate / EnsureRoles) AND the package goleak TestMain in
// db_test.go — it deliberately declares no second TestMain.
package db

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// rlsMigratedPool applies roles + every embedded migration (through 0032 = owner RLS) and
// returns an aura_app pool (the runtime, RLS-subject role). Closed via t.Cleanup.
func rlsMigratedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := EnsureRoles(ctx, bootstrapURL(t), os.Getenv("POSTGRES_PASSWORD")); err != nil {
		t.Fatalf("EnsureRoles: %v", err)
	}
	if _, err := Migrate(ctx, envOrSkip(t, "AURA_DB_MIGRATE_URL")); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	pool, err := Open(ctx, &Config{URL: envOrSkip(t, "AURA_DB_URL")})
	if err != nil {
		t.Fatalf("Open (aura_app): %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// seedRLSIdentity inserts a user identity on the aura_app pool (RLS is not on aura.identities)
// and schedules its deletion (which cascades its conversations + pauses via FK).
func seedRLSIdentity(t *testing.T, pool *pgxpool.Pool, id, name string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		"INSERT INTO aura.identities (id, name, kind) VALUES ($1, $2, 'user') ON CONFLICT DO NOTHING",
		id, name); err != nil {
		t.Fatalf("seed identity %s: %v", name, err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM aura.identities WHERE id = $1", id)
	})
}

// seedRLSConversation inserts a conversation owned by identityID. Since migration 0089 it
// CANNOT run on the bare pool — aura.conversations is fail closed — so the insert carries
// the identity. That it succeeds is half the proof: the owner is not locked out of their own
// table.
func seedRLSConversation(t *testing.T, pool *pgxpool.Pool, identityID string) string {
	t.Helper()
	convID := uuid.Must(uuid.NewV7()).String()
	if err := WithIdentityTxRaw(context.Background(), pool, identityID, func(tx pgx.Tx) error {
		_, e := tx.Exec(context.Background(),
			"INSERT INTO aura.conversations (id, identity_id, model, status) VALUES ($1, $2, 'rls-test', 'active')",
			convID, identityID)
		return e
	}); err != nil {
		t.Fatalf("seed conversation for %s: %v", identityID, err)
	}
	return convID
}

// seedRLSPause inserts a pending pause for convID WITHOUT an explicit identity_id, so the
// 0032 BEFORE INSERT trigger must populate it from the parent conversation's owner. The
// transaction carries ownerID because that trigger's lookup is itself owner-filtered: under
// 0089 an unscoped insert finds no parent, leaves identity_id NULL, and is refused.
func seedRLSPause(t *testing.T, pool *pgxpool.Pool, ownerID, convID string) string {
	t.Helper()
	token := uuid.Must(uuid.NewV7()).String()
	if err := WithIdentityTxRaw(context.Background(), pool, ownerID, func(tx pgx.Tx) error {
		_, e := tx.Exec(context.Background(),
			"INSERT INTO aura.paused_states (token, conversation_id, kind, question, tool_call_id) VALUES ($1, $2, 'approval', 'ok?', 'call-rls')",
			token, convID)
		return e
	}); err != nil {
		t.Fatalf("seed pause for %s: %v", convID, err)
	}
	return token
}

// TestAuraAppLacksRLSBypass asserts the RESEARCH A3 assumption the whole backstop rests on:
// the runtime aura_app role has neither SUPERUSER nor BYPASSRLS. If it did, RLS would
// silently no-op and every owner policy would be decorative.
func TestAuraAppLacksRLSBypass(t *testing.T) {
	pool := rlsMigratedPool(t)
	var isSuper, bypassRLS bool
	if err := pool.QueryRow(context.Background(),
		"SELECT rolsuper, rolbypassrls FROM pg_roles WHERE rolname = 'aura_app'").Scan(&isSuper, &bypassRLS); err != nil {
		t.Fatalf("read aura_app role attributes: %v", err)
	}
	if isSuper {
		t.Error("aura_app has SUPERUSER — RLS is silently bypassed (A3 violated; use FORCE or a non-super role)")
	}
	if bypassRLS {
		t.Error("aura_app has BYPASSRLS — RLS is silently bypassed (A3 violated)")
	}
}

// TestRLSBackstop is the MUSR-01 / D-07 backstop proof: an UNSCOPED query (ListConversations
// has NO identity_id predicate — the "forgot the *ForIdentity WHERE" case) run inside
// WithIdentityTx(identityA) returns ONLY A's rows. The kernel filters the foreign rows the
// app forgot to. It also proves no self-lockout (the owner sees their OWN row) and symmetry
// (under B, only B's row is visible).
func TestRLSBackstop(t *testing.T) {
	pool := rlsMigratedPool(t)
	ctx := context.Background()

	idA := uuid.Must(uuid.NewV7()).String()
	idB := uuid.Must(uuid.NewV7()).String()
	seedRLSIdentity(t, pool, idA, "rls-a-"+idA[:8])
	seedRLSIdentity(t, pool, idB, "rls-b-"+idB[:8])
	convA := seedRLSConversation(t, pool, idA)
	convB := seedRLSConversation(t, pool, idB)

	seenUnder := func(identityID string) map[string]bool {
		t.Helper()
		seen := map[string]bool{}
		if err := WithIdentityTx(ctx, pool, identityID, func(q *sqlc.Queries) error {
			rows, e := q.ListConversations(ctx, true) // include archived; NO owner predicate
			if e != nil {
				return e
			}
			for _, r := range rows {
				seen[uuid.UUID(r.ID.Bytes).String()] = true
			}
			return nil
		}); err != nil {
			t.Fatalf("WithIdentityTx(%s) unscoped list: %v", identityID, err)
		}
		return seen
	}

	underA := seenUnder(idA)
	if !underA[convA] {
		t.Errorf("backstop hid the owner's OWN conversation %s under identity A (self-lockout)", convA)
	}
	if underA[convB] {
		t.Errorf("backstop LEAKED foreign conversation %s to identity A (isolation broken)", convB)
	}

	underB := seenUnder(idB)
	if !underB[convB] {
		t.Errorf("backstop hid the owner's OWN conversation %s under identity B (self-lockout)", convB)
	}
	if underB[convA] {
		t.Errorf("backstop LEAKED foreign conversation %s to identity B (isolation broken)", convA)
	}
}

// seedRLSDocument inserts a document owned by identityID. Unlike seedRLSConversation it
// CANNOT run on the bare pool: aura.documents is fail-closed as of migration 0087, so the
// insert must carry the identity. That it succeeds here is half the proof — the owner is
// not locked out of their own table.
func seedRLSDocument(t *testing.T, pool *pgxpool.Pool, identityID string) string {
	t.Helper()
	docID := uuid.Must(uuid.NewV7()).String()
	if err := WithIdentityTxRaw(context.Background(), pool, identityID, func(tx pgx.Tx) error {
		_, e := tx.Exec(context.Background(),
			"INSERT INTO aura.documents (id, identity_id, scope, title, status) VALUES ($1, $2, 'library', 'rls-doc', 'ready')",
			docID, identityID)
		return e
	}); err != nil {
		t.Fatalf("seed document for %s: %v", identityID, err)
	}
	return docID
}

// TestRLSFailsClosedWithoutIdentity is the migration-0087 regression proof, and it is
// written against the exact failure that was MEASURED on the live deployment on
// 2026-08-02: connected as aura_app with no app.current_identity set, every row of every
// identity-scoped table was readable, because the 0032-era policies said
// "NULLIF(current_setting(...), ”) IS NULL OR identity_id = ..." and an unset variable
// made the first branch true.
//
// It asserts three things on aura.documents, which 0087 brought under RLS:
//
//	no identity  -> zero rows, even though rows exist and the owner can see them
//	no identity  -> INSERT refused (the restrictive policy's WITH CHECK)
//	identity A   -> INSERT of a row owned by B refused, own row still visible
//
// The read half is the one that regresses silently: a policy that fails open returns
// MORE rows, never an error, so nothing in the app ever notices. Assert the count.
func TestRLSFailsClosedWithoutIdentity(t *testing.T) {
	pool := rlsMigratedPool(t)
	ctx := context.Background()

	idA := uuid.Must(uuid.NewV7()).String()
	idB := uuid.Must(uuid.NewV7()).String()
	seedRLSIdentity(t, pool, idA, "rlsfc-a-"+idA[:8])
	seedRLSIdentity(t, pool, idB, "rlsfc-b-"+idB[:8])
	docA := seedRLSDocument(t, pool, idA)
	seedRLSDocument(t, pool, idB)

	// 1. No identity on the session: the table is invisible, not merely filtered.
	var visible int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM aura.documents").Scan(&visible); err != nil {
		t.Fatalf("count documents with no identity: %v", err)
	}
	if visible != 0 {
		t.Errorf("aura.documents returned %d row(s) to a connection with no app.current_identity, want 0 — RLS is failing OPEN", visible)
	}

	// 2. No identity on the session: writes are refused outright.
	_, err := pool.Exec(ctx,
		"INSERT INTO aura.documents (id, identity_id, scope, title, status) VALUES ($1, $2, 'library', 'no-identity', 'ready')",
		uuid.Must(uuid.NewV7()).String(), idA)
	if err == nil {
		t.Error("INSERT into aura.documents succeeded with no app.current_identity set, want a row-level-security refusal")
	}

	// 3. Under identity A: A's own row is visible and B's is not.
	if err := WithIdentityTxRaw(ctx, pool, idA, func(tx pgx.Tx) error {
		var ownVisible, total int
		if e := tx.QueryRow(ctx, "SELECT count(*) FROM aura.documents WHERE id = $1", docA).Scan(&ownVisible); e != nil {
			return e
		}
		if ownVisible != 1 {
			t.Errorf("owner cannot see their own document %s under their own identity (self-lockout)", docA)
		}
		if e := tx.QueryRow(ctx, "SELECT count(*) FROM aura.documents").Scan(&total); e != nil {
			return e
		}
		if total != 1 {
			t.Errorf("identity A sees %d document(s), want exactly its own 1 — B's row leaked", total)
		}
		return nil
	}); err != nil {
		t.Fatalf("WithIdentityTxRaw(A) read assertions: %v", err)
	}

	// 4. A cannot forge a row owned by B. This needs its OWN transaction: the refused
	//    INSERT aborts the one it runs in, so folding it into the reads above would turn
	//    their COMMIT into a rollback and mask what actually happened.
	err = WithIdentityTxRaw(ctx, pool, idA, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			"INSERT INTO aura.documents (id, identity_id, scope, title, status) VALUES ($1, $2, 'library', 'cross-identity', 'ready')",
			uuid.Must(uuid.NewV7()).String(), idB)
		return e
	})
	if err == nil {
		t.Error("identity A inserted a document owned by identity B, want a row-level-security refusal")
	}
}

// TestVerifyRLSEnforcedAcceptsAuraApp is the live half of the boot gate: the runtime role
// really does pass the check the daemon runs before serving. The refusal half is unit
// tested (TestBootChatCompositionRefusesRLSBypassRole) because provoking it needs a
// superuser pool, which this tier deliberately does not hand to the app.
func TestVerifyRLSEnforcedAcceptsAuraApp(t *testing.T) {
	if err := VerifyRLSEnforced(context.Background(), rlsMigratedPool(t)); err != nil {
		t.Fatalf("VerifyRLSEnforced(aura_app) = %v, want nil", err)
	}
}

// TestVerifyRLSEnforcedRejectsSuperuser proves the gate actually fires: the bootstrap DSN
// is the superuser `aura` role, which carries rolsuper and rolbypassrls and for which
// Postgres skips row security entirely.
func TestVerifyRLSEnforcedRejectsSuperuser(t *testing.T) {
	ctx := context.Background()
	admin, err := Open(ctx, &Config{URL: bootstrapURL(t)})
	if err != nil {
		t.Fatalf("Open (bootstrap superuser): %v", err)
	}
	t.Cleanup(admin.Close)
	if err := VerifyRLSEnforced(ctx, admin); !errors.Is(err, ErrRLSBypass) {
		t.Fatalf("VerifyRLSEnforced(superuser) = %v, want ErrRLSBypass", err)
	}
}

// TestRLSPausedStatesTriggerAndBackstop proves the paused_states half: the 0032 BEFORE INSERT
// trigger populates identity_id from the parent conversation (the runner write-path is plan
// 05), and an UNSCOPED cross-thread pending list (ListAllPendingPausedStates, no identity
// predicate) inside WithIdentityTx(A) returns only A's pause — the approval-center isolation
// the *ForIdentity handler is layered on.
func TestRLSPausedStatesTriggerAndBackstop(t *testing.T) {
	pool := rlsMigratedPool(t)
	ctx := context.Background()

	idA := uuid.Must(uuid.NewV7()).String()
	idB := uuid.Must(uuid.NewV7()).String()
	seedRLSIdentity(t, pool, idA, "rlsp-a-"+idA[:8])
	seedRLSIdentity(t, pool, idB, "rlsp-b-"+idB[:8])
	convA := seedRLSConversation(t, pool, idA)
	convB := seedRLSConversation(t, pool, idB)
	tokenA := seedRLSPause(t, pool, idA, convA)
	tokenB := seedRLSPause(t, pool, idB, convB)

	// The trigger populated identity_id from each pause's parent conversation. The read is
	// owner-scoped for the same reason the insert is: the table is fail closed.
	assertPauseOwner := func(owner, token, wantOwner string) {
		t.Helper()
		var got string
		if err := WithIdentityTxRaw(ctx, pool, owner, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx,
				"SELECT identity_id::text FROM aura.paused_states WHERE token = $1", token).Scan(&got)
		}); err != nil {
			t.Fatalf("read pause %s owner: %v", token, err)
		}
		if got != wantOwner {
			t.Errorf("pause %s identity_id = %q, want %q (trigger did not populate from parent conversation)", token, got, wantOwner)
		}
	}
	assertPauseOwner(idA, tokenA, idA)
	assertPauseOwner(idB, tokenB, idB)

	// Backstop: an unscoped cross-thread pending list under A returns only A's pause.
	seen := map[string]bool{}
	if err := WithIdentityTx(ctx, pool, idA, func(q *sqlc.Queries) error {
		rows, e := q.ListAllPendingPausedStates(ctx, 100) // NO identity predicate
		if e != nil {
			return e
		}
		for _, r := range rows {
			seen[uuid.UUID(r.Token.Bytes).String()] = true
		}
		return nil
	}); err != nil {
		t.Fatalf("WithIdentityTx(A) unscoped pending list: %v", err)
	}
	if !seen[tokenA] {
		t.Errorf("paused_states RLS hid the owner's own pause %s (self-lockout)", tokenA)
	}
	if seen[tokenB] {
		t.Errorf("paused_states RLS leaked foreign pause %s to identity A (isolation broken)", tokenB)
	}
}

// TestRLSConversationPlaneFailsClosedWithoutIdentity is the migration-0089 regression proof,
// written against the exact reading MEASURED on the live deployment on 2026-08-02: connected
// as aura_app with no app.current_identity set, all 32 conversations and all 648 turns were
// visible, because the 0032 policies said "NULLIF(current_setting(...), ”) IS NULL OR
// identity_id = ..." and an unset variable made the first branch true.
//
// The read half is the one that regresses silently — a policy that fails open returns MORE
// rows, never an error — so every case below asserts a COUNT, not just an absence of error.
func TestRLSConversationPlaneFailsClosedWithoutIdentity(t *testing.T) {
	pool := rlsMigratedPool(t)
	ctx := context.Background()

	idA := uuid.Must(uuid.NewV7()).String()
	idB := uuid.Must(uuid.NewV7()).String()
	seedRLSIdentity(t, pool, idA, "rlsconv-a-"+idA[:8])
	seedRLSIdentity(t, pool, idB, "rlsconv-b-"+idB[:8])
	convA := seedRLSConversation(t, pool, idA)
	convB := seedRLSConversation(t, pool, idB)
	seedRLSTurn(t, pool, idA, convA, 1)
	seedRLSTurn(t, pool, idB, convB, 1)
	seedRLSPause(t, pool, idA, convA)
	seedRLSPause(t, pool, idB, convB)

	// 1. No identity: the whole conversation plane is invisible, not merely filtered.
	for _, table := range []string{"aura.conversations", "aura.conversation_turns", "aura.paused_states"} {
		var visible int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&visible); err != nil {
			t.Fatalf("count %s with no identity: %v", table, err)
		}
		if visible != 0 {
			t.Errorf("%s returned %d row(s) to a connection with no app.current_identity, want 0 — RLS is failing OPEN", table, visible)
		}
	}

	// 2. No identity: writes are refused outright, not silently dropped.
	if _, err := pool.Exec(ctx,
		"INSERT INTO aura.conversations (id, identity_id, model, status) VALUES ($1, $2, 'rls-test', 'active')",
		uuid.Must(uuid.NewV7()).String(), idA); err == nil {
		t.Error("INSERT into aura.conversations succeeded with no app.current_identity, want a row-level-security refusal")
	}

	// 3. Under identity A: A sees exactly its own rows and none of B's.
	if err := WithIdentityTxRaw(ctx, pool, idA, func(tx pgx.Tx) error {
		for table, want := range map[string]int{
			"aura.conversations":      1,
			"aura.conversation_turns": 1,
			"aura.paused_states":      1,
		} {
			var got int
			if e := tx.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&got); e != nil {
				return e
			}
			if got != want {
				t.Errorf("identity A sees %d row(s) in %s, want exactly its own %d — B's rows leaked, or A is locked out of its own", got, table, want)
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("WithIdentityTxRaw(A) read assertions: %v", err)
	}

	// 4. A cannot forge a conversation owned by B. Its own transaction: the refused INSERT
	//    aborts the one it runs in, so folding it into the reads above would turn their
	//    COMMIT into a rollback and mask what actually happened.
	if err := WithIdentityTxRaw(ctx, pool, idA, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			"INSERT INTO aura.conversations (id, identity_id, model, status) VALUES ($1, $2, 'rls-test', 'active')",
			uuid.Must(uuid.NewV7()).String(), idB)
		return e
	}); err == nil {
		t.Error("identity A inserted a conversation owned by identity B, want a row-level-security refusal")
	}

	// 5. A cannot append a turn to B's conversation either — the child policy names B as the
	//    parent's owner, so it is not merely deferring to a parent lookup that already failed.
	if err := WithIdentityTxRaw(ctx, pool, idA, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			"INSERT INTO aura.conversation_turns (conversation_id, seq, role, content) VALUES ($1, 99, 'user', 'forged')",
			convB)
		return e
	}); err == nil {
		t.Error("identity A appended a turn to identity B's conversation, want a row-level-security refusal")
	}
}

// TestRLSSharedLinksResolveAnonymouslyButWriteOwnerOnly pins the ONE table migration 0089
// deliberately does not fail closed on the read side. A share link is a capability handed to
// someone who is not the owner, so "mine" would not fail closed here — it would break
// sharing silently, resolving a valid link to nothing instead of erroring.
//
// The three documented non-owner readers are all served by the same predicate: ResolveByToken
// (anonymous), ResolveLiveByID (any authenticated bearer, D-10) and DueForExpiry (the sweep,
// which selects rows whose expires_at has ALREADY passed and would find nothing under a
// liveness-based predicate).
func TestRLSSharedLinksResolveAnonymouslyButWriteOwnerOnly(t *testing.T) {
	pool := rlsMigratedPool(t)
	ctx := context.Background()

	idA := uuid.Must(uuid.NewV7()).String()
	idB := uuid.Must(uuid.NewV7()).String()
	seedRLSIdentity(t, pool, idA, "rlsshare-a-"+idA[:8])
	seedRLSIdentity(t, pool, idB, "rlsshare-b-"+idB[:8])
	convA := seedRLSConversation(t, pool, idA)

	liveID := uuid.Must(uuid.NewV7()).String()
	revokedID := uuid.Must(uuid.NewV7()).String()
	insertShare := func(shareID string, revoked bool) {
		t.Helper()
		if err := WithIdentityTxRaw(ctx, pool, idA, func(tx pgx.Tx) error {
			_, e := tx.Exec(ctx, `
				INSERT INTO aura.shared_links (id, owner_identity_id, conversation_id, tier, snapshot_id, snapshot_bucket, revoked_at)
				VALUES ($1, $2, $3, 'internal', $4, 'rls-bucket', CASE WHEN $5 THEN now() ELSE NULL END)`,
				shareID, idA, convA, uuid.Must(uuid.NewV7()).String(), revoked)
			return e
		}); err != nil {
			t.Fatalf("seed share link %s (revoked=%v): %v", shareID, revoked, err)
		}
	}
	insertShare(liveID, false)
	insertShare(revokedID, true)

	// 1. Anonymous resolution still works: the unrevoked link is readable with NO identity.
	//    This is the assertion that catches an over-tightened policy — the failure mode is a
	//    link that resolves to nothing, which no error would ever report.
	var anonVisible int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM aura.shared_links WHERE id = $1", liveID).Scan(&anonVisible); err != nil {
		t.Fatalf("anonymous resolve by id: %v", err)
	}
	if anonVisible != 1 {
		t.Errorf("an unrevoked share link is invisible to an anonymous reader (%d rows) — ResolveByToken and ResolveLiveByID are broken", anonVisible)
	}

	// 2. A REVOKED link is not: the widening is bounded to rows the resolvers may serve and
	//    the expiry sweep must find.
	var revokedVisible int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM aura.shared_links WHERE id = $1", revokedID).Scan(&revokedVisible); err != nil {
		t.Fatalf("anonymous read of a revoked link: %v", err)
	}
	if revokedVisible != 0 {
		t.Errorf("a revoked share link is readable with no identity (%d rows), want 0", revokedVisible)
	}

	// 3. Writes stay owner-only: nobody can plant a link they do not own, with or without an
	//    identity set.
	if _, err := pool.Exec(ctx, `
		INSERT INTO aura.shared_links (id, owner_identity_id, conversation_id, tier, snapshot_id, snapshot_bucket)
		VALUES ($1, $2, $3, 'internal', $4, 'rls-bucket')`,
		uuid.Must(uuid.NewV7()).String(), idA, convA, uuid.Must(uuid.NewV7()).String()); err == nil {
		t.Error("INSERT into aura.shared_links succeeded with no app.current_identity, want a refusal")
	}
	if err := WithIdentityTxRaw(ctx, pool, idB, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `
			INSERT INTO aura.shared_links (id, owner_identity_id, conversation_id, tier, snapshot_id, snapshot_bucket)
			VALUES ($1, $2, $3, 'internal', $4, 'rls-bucket')`,
			uuid.Must(uuid.NewV7()).String(), idA, convA, uuid.Must(uuid.NewV7()).String())
		return e
	}); err == nil {
		t.Error("identity B inserted a share link owned by identity A, want a row-level-security refusal")
	}

	// 4. An AUTHENTICATED non-owner sees nothing and can change nothing. The widening in
	//    point 1 is for a caller with NO identity — the plain-pool resolvers — not for
	//    everyone: identity B running an owner-scoped query must not observe A's link at all,
	//    and must not be able to revoke it (the DELETE/UPDATE hole a single FOR ALL policy
	//    would have left).
	if err := WithIdentityTxRaw(ctx, pool, idB, func(tx pgx.Tx) error {
		var seenByB int
		if e := tx.QueryRow(ctx, "SELECT count(*) FROM aura.shared_links WHERE id = $1", liveID).Scan(&seenByB); e != nil {
			return e
		}
		if seenByB != 0 {
			t.Errorf("identity B sees %d of identity A's share links under its own identity, want 0", seenByB)
		}
		tag, e := tx.Exec(ctx, "UPDATE aura.shared_links SET revoked_at = now() WHERE id = $1", liveID)
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 0 {
			t.Errorf("identity B revoked %d of identity A's share links, want 0", tag.RowsAffected())
		}
		return nil
	}); err != nil {
		t.Fatalf("WithIdentityTxRaw(B) revoke attempt: %v", err)
	}
}

// seedRLSTurn appends one turn to convID as its owner.
func seedRLSTurn(t *testing.T, pool *pgxpool.Pool, ownerID, convID string, seq int) {
	t.Helper()
	if err := WithIdentityTxRaw(context.Background(), pool, ownerID, func(tx pgx.Tx) error {
		_, e := tx.Exec(context.Background(),
			"INSERT INTO aura.conversation_turns (conversation_id, seq, role, content) VALUES ($1, $2, 'user', 'rls turn')",
			convID, seq)
		return e
	}); err != nil {
		t.Fatalf("seed turn %d for %s: %v", seq, convID, err)
	}
}
