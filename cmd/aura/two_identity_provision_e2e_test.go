//go:build db_integration && garage_integration && authula_integration && musr_e2e

// The Phase-01 tracer acceptance test (D-09/E2E-02): drives ONE `aura identity create`-
// shaped path — StartSession then Provision, the exact pair the CLI verb calls — through
// the REAL saga against the live stack, and asserts all four E2E-02 resources exist,
// positively, for the returned identity: an ArcadeDB database + derived credential, a
// Garage bucket + scoped key, the per-identity skills root (by name), and a sandbox box.
package main

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/idroot"
	"github.com/chetto1983/aura/internal/objectstore/garageadmin"
)

func TestIdentityCreateProvisionsEveryPlane(t *testing.T) {
	pool := musrMigratedPool(t)
	svc, env := musrBuildProvisionService(t, pool)
	cfg := env.cfg
	ctx := context.Background()

	email := fmt.Sprintf("musr-create-%s@example.test", uuid.NewString()[:8])

	start, err := svc.StartSession(ctx, localSeededIdentityID)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if start.SessionToken == "" {
		t.Fatal("StartSession returned an empty session token")
	}

	resp, err := svc.Provision(ctx, localSeededIdentityID, start.SessionToken, agui.OnboardingProvisionRequest{
		Email:            email,
		Password:         "Musr-Create-Test-Passw0rd!",
		SecurityQuestion: "musr create test question",
		SecurityAnswer:   "musr create test answer",
		Capabilities:     []string{"agent.run"},
		LinkTelegram:     true,
	})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	identityID := resp.IdentityID
	if identityID == "" {
		t.Fatal("Provision returned an empty identity id")
	}
	if resp.DeepLink == "" {
		t.Fatal("Provision returned no Telegram deep link (D-06: Telegram stays required)")
	}

	// De-provision every plane this run caused to be provisioned — registered BEFORE any
	// subtest so it still runs on a t.Fatal inside one. Runs LIFO relative to
	// musrBuildProvisionService's authulaProvider.Close() cleanup, so the Authula core is
	// still open when this fires.
	t.Cleanup(func() {
		cctx := context.Background()
		if env.sandboxRouter != nil {
			_ = env.sandboxRouter.Destroy(cctx, identityID)
		}
		if env.filesystem != nil {
			_ = env.filesystem.DeprovisionIdentityDirs(cctx, identityID)
		}
		if env.objectStore != nil {
			_ = env.objectStore.DeprovisionObjectStore(cctx, identityID)
		}
		if env.memory != nil {
			_ = env.memory.PurgeMemory(cctx, identityID)
		}
		var authulaUserID string
		_ = pool.QueryRow(cctx,
			`SELECT authula_user_id FROM aura.identity_auth_links WHERE identity_id = $1::uuid`,
			identityID).Scan(&authulaUserID)
		if authulaUserID != "" && env.deleteAuthulaUser != nil {
			_ = env.deleteAuthulaUser(cctx, authulaUserID)
		}
		_, _ = pool.Exec(cctx, `DELETE FROM aura.identities WHERE id = $1::uuid`, identityID)
	})

	// Second assertion (Task 1 behavior): the identity row was written by the SAGA — it
	// carries the recovery row and the requested capability grant — not by a raw INSERT.
	t.Run("identity row carries the recovery challenge and the requested grant", func(t *testing.T) {
		var recoveryCount int
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM aura.identity_recovery WHERE identity_id = $1::uuid`,
			identityID).Scan(&recoveryCount); err != nil {
			t.Fatalf("query identity_recovery: %v", err)
		}
		if recoveryCount != 1 {
			t.Fatalf("identity_recovery rows = %d, want 1 (the saga's recovery leg)", recoveryCount)
		}
		// aura.capability_grants is RLS-owner-scoped (migration 0087, fail-closed): a bare
		// SELECT with no app.current_identity bound sees ZERO rows even though the row
		// exists, so the check must run inside a tx scoped to the identity being read —
		// the same kernel RLS backstop pattern two_identity_e2e_harness_test.go's
		// assertRLSCount already establishes.
		var grantCount int
		if err := db.WithIdentityTxRaw(ctx, pool, identityID, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx,
				`SELECT count(*) FROM aura.capability_grants WHERE identity_id = $1::uuid AND capability = 'agent.run'`,
				identityID).Scan(&grantCount)
		}); err != nil {
			t.Fatalf("query capability_grants: %v", err)
		}
		if grantCount != 1 {
			t.Fatalf("agent.run grant rows = %d, want 1 (the saga's aura leg)", grantCount)
		}
	})

	t.Run("ArcadeDB database exists and answers to the derived credential", func(t *testing.T) {
		tenantCreds, err := arcadedb.NewTenantCredentials()
		if err != nil {
			t.Fatalf("NewTenantCredentials: %v", err)
		}
		database, err := arcadedb.DatabaseFor(identityID)
		if err != nil {
			t.Fatalf("DatabaseFor: %v", err)
		}
		client, err := arcadedb.New(arcadedb.Config{
			BaseURL:  cfg.ArcadeDB.BaseURL,
			Database: database,
			User:     arcadedb.TenantUserFor(database),
			Password: tenantCreds.PasswordFor(database),
		})
		if err != nil {
			t.Fatalf("build tenant client: %v", err)
		}
		if _, err := client.Query(ctx, "select 1 as ok", nil); err != nil {
			t.Fatalf("tenant credential could not query its own database %q: %v", database, err)
		}
	})

	t.Run("Garage bucket and scoped key exist for the identity", func(t *testing.T) {
		admin, err := garageadmin.New(cfg.GarageAdminEndpoint, cfg.GarageAdminToken)
		if err != nil {
			t.Fatalf("garageadmin.New: %v", err)
		}
		bucket, err := garageadmin.BucketForIdentity(identityID)
		if err != nil {
			t.Fatalf("BucketForIdentity: %v", err)
		}
		bucketID, err := admin.BucketIDByAlias(ctx, bucket)
		if err != nil {
			t.Fatalf("bucket %q does not exist: %v", bucket, err)
		}
		if bucketID == "" {
			t.Fatalf("bucket %q resolved to an empty id", bucket)
		}
		// aura.identity_object_store is likewise RLS-owner-scoped (migration 0087) — scope
		// the read the same way the capability_grants check above does.
		var storedBucket, accessKey string
		if err := db.WithIdentityTxRaw(ctx, pool, identityID, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx,
				`SELECT bucket, access_key FROM aura.identity_object_store WHERE identity_id = $1::uuid`,
				identityID).Scan(&storedBucket, &accessKey)
		}); err != nil {
			t.Fatalf("query identity_object_store: %v", err)
		}
		if storedBucket != bucket {
			t.Fatalf("stored bucket = %q, want %q", storedBucket, bucket)
		}
		if accessKey == "" {
			t.Fatal("stored access_key is empty — no scoped key persisted")
		}
	})

	// This is the assertion that keeps the filesystem leg honest (Task 1 behavior): a
	// blank AURA_SKILLS_IDENTITY_DIR makes identityDirRoots(cfg) return an EMPTY map,
	// which makes ProvisionIdentityDirs a no-op that returns nil — an assertion phrased
	// only over "the roots" would then pass while nothing was provisioned. Asserting the
	// root SET is non-empty first turns a future config regression into a red test.
	t.Run("skills root is provisioned and named, not vacuously satisfied", func(t *testing.T) {
		roots := identityDirRoots(cfg)
		if len(roots) == 0 {
			t.Fatal("identityDirRoots(cfg) is empty — SkillsIdentityDir is unset, so ProvisionIdentityDirs " +
				"provisioned NOTHING and this test would otherwise pass vacuously")
		}
		skillsBase, ok := roots["skills"]
		if !ok {
			t.Fatalf("identityDirRoots(cfg) = %v, want a %q key", roots, "skills")
		}
		dir, err := idroot.RootIdentityDir(skillsBase, identityID)
		if err != nil {
			t.Fatalf("RootIdentityDir(%q, %q): %v", skillsBase, identityID, err)
		}
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("skills root %q does not exist: %v", dir, err)
		}
		if !info.IsDir() {
			t.Fatalf("skills root %q exists but is not a directory", dir)
		}
	})

	t.Run("sandbox router reports a live box for the identity", func(t *testing.T) {
		if env.sandboxRouter == nil {
			t.Fatal("sandbox router is nil — the eager sandbox leg could not have run")
		}
		handle, err := env.sandboxRouter.EnsureBox(ctx, identityID)
		if err != nil {
			t.Fatalf("EnsureBox: %v", err)
		}
		if handle.ContainerID == "" {
			t.Fatal("EnsureBox returned an empty container id")
		}
		if handle.IdentityID != identityID {
			t.Fatalf("handle.IdentityID = %q, want %q", handle.IdentityID, identityID)
		}
	})
}
