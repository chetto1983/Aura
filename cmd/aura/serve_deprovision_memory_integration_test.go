//go:build arcadedb_integration

// The memory purge against a live ArcadeDB.
//
// The unit tests prove the adapter reads the postconditions correctly; only a real
// server proves the postconditions are the ones ArcadeDB actually answers — that
// `drop database` makes /api/v1/exists say false, and that a dropped user is
// refused at /api/v1/ready rather than quietly still accepted.
//
//	go test -tags arcadedb_integration -run ArcadeMemoryPurgeLive -v ./cmd/aura/
package main

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/google/uuid"
)

func liveArcadeMemoryPurger(t *testing.T) (arcadeMemoryPurgeAdapter, *arcadedb.Client) {
	t.Helper()
	password := strings.TrimSpace(os.Getenv("ARCADEDB_PASSWORD"))
	if password == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("ARCADEDB_PASSWORD must be set in CI: a skipped integration tier is a falsely-green job")
		}
		t.Skip("ARCADEDB_PASSWORD not set")
	}
	if strings.TrimSpace(os.Getenv("AURA_ARCADEDB_TENANT_SECRET")) == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("AURA_ARCADEDB_TENANT_SECRET must be set in CI: a skipped integration tier is a falsely-green job")
		}
		t.Skip("AURA_ARCADEDB_TENANT_SECRET not set")
	}
	base := arcadedb.Config{
		BaseURL:  arcadeEnvOrDefault("ARCADEDB_URL", "http://127.0.0.1:2480"),
		Database: arcadeEnvOrDefault("ARCADEDB_DATABASE", "aura_memory"),
	}
	adminCfg := base
	adminCfg.User = arcadeEnvOrDefault("ARCADEDB_ADMIN_USER", "root")
	adminCfg.Password = password
	admin, err := arcadedb.New(adminCfg)
	if err != nil {
		t.Fatalf("admin client: %v", err)
	}
	credentials, err := arcadedb.NewTenantCredentials()
	if err != nil {
		t.Fatalf("tenant credentials: %v", err)
	}
	return arcadeMemoryPurgeAdapter{admin: admin, base: base, credentials: credentials}, admin
}

// arcadeEnvOrDefault is namespaced because cmd/aura's garage_integration tier declares
// its own envOrDefault: the two tiers compile into the same test binary whenever both
// tags are on (scripts/tagged_tier_compile.sh takes the union), and a bare name here
// collided with it.
func arcadeEnvOrDefault(name, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return fallback
}

func TestArcadeMemoryPurgeLiveErasesDatabaseAndCredential(t *testing.T) {
	purger, admin := liveArcadeMemoryPurger(t)
	ctx := context.Background()
	identity := uuid.NewString()
	database, err := arcadedb.DatabaseFor(identity)
	if err != nil {
		t.Fatalf("DatabaseFor: %v", err)
	}
	user := arcadedb.TenantUserFor(database)
	password := purger.credentials.PasswordFor(database)
	t.Cleanup(func() {
		_, _ = admin.DropDatabase(context.Background(), database)
		_ = admin.DropUser(context.Background(), user)
	})

	if _, err := admin.CreateDatabase(ctx, database); err != nil {
		t.Fatalf("create tenant database: %v", err)
	}
	if err := admin.CreateUser(ctx, user, password, map[string][]string{database: {"admin"}}); err != nil {
		t.Fatalf("create tenant user: %v", err)
	}
	// The credential must be live BEFORE the purge, or the negative bind afterwards
	// would prove nothing.
	tenantCfg := purger.base
	tenantCfg.Database = database
	tenantCfg.User = user
	tenantCfg.Password = password
	tenant, err := arcadedb.New(tenantCfg)
	if err != nil {
		t.Fatalf("tenant client: %v", err)
	}
	accepted, err := tenant.CredentialAccepted(ctx)
	if err != nil {
		t.Fatalf("probe the fresh credential: %v", err)
	}
	if !accepted {
		t.Fatal("the freshly created tenant credential was refused — the negative probe below would be vacuous")
	}

	if err := purger.PurgeMemory(ctx, identity); err != nil {
		t.Fatalf("PurgeMemory: %v", err)
	}

	exists, err := admin.DatabaseExists(ctx, database)
	if err != nil {
		t.Fatalf("exists after purge: %v", err)
	}
	if exists {
		t.Fatalf("database %s survived the purge", database)
	}
	accepted, err = tenant.CredentialAccepted(ctx)
	if err != nil {
		t.Fatalf("probe the purged credential: %v", err)
	}
	if accepted {
		t.Fatalf("credential %s still authenticates after the purge", user)
	}
}

// The saga journal re-runs a failed step, so a second purge over the same identity
// must converge rather than fail on drops the server now refuses.
func TestArcadeMemoryPurgeLiveIsIdempotent(t *testing.T) {
	purger, _ := liveArcadeMemoryPurger(t)
	identity := uuid.NewString()
	if err := purger.PurgeMemory(context.Background(), identity); err != nil {
		t.Fatalf("purge of a never-provisioned identity: %v", err)
	}
	if err := purger.PurgeMemory(context.Background(), identity); err != nil {
		t.Fatalf("second purge: %v", err)
	}
}
