//go:build db_integration

package retention

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRetentionStoreClaimsAreBoundedAndDisjoint(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dsn := retentionIntegrationEnv(t, "AURA_DB_URL")
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	migratePool, err := pgxpool.New(ctx, retentionIntegrationEnv(t, "AURA_DB_MIGRATE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(migratePool.Close)

	var database string
	if err := pool.QueryRow(ctx, `SELECT current_database()`).Scan(&database); err != nil {
		t.Fatal(err)
	}
	if database == "aura" || database == "postgres" {
		t.Fatalf("refusing db_integration database %q; use a disposable database", database)
	}
	var migration int
	if err := migratePool.QueryRow(ctx, `SELECT version FROM public.schema_migrations`).Scan(&migration); err != nil {
		t.Fatal(err)
	}
	if migration < 44 {
		t.Fatalf("disposable database migration = %d, want at least 44", migration)
	}

	candidates := make([]Candidate, 4)
	for i := range candidates {
		candidates[i] = Candidate{
			IdentityID: "00000000-0000-0000-0000-000000000001",
			ArtifactID: uuid.NewString(), Version: 1, Action: ActionDeleteArtifact,
			Class: ClassTemporary, Bytes: int64(i + 1),
		}
	}
	plan, err := BuildPlan("integration-"+uuid.NewString(), candidates, len(candidates))
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(pool)
	now := time.Now().UTC()
	operation, err := store.SavePlan(ctx, plan, now)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	claimed := make(chan []Item, 2)
	errs := make(chan error, 2)
	for _, owner := range []string{"worker-a", "worker-b"} {
		wg.Add(1)
		go func(owner string) {
			defer wg.Done()
			items, claimErr := store.Claim(ctx, operation.ID, owner, 2, now, time.Minute)
			if claimErr != nil {
				errs <- claimErr
				return
			}
			claimed <- items
		}(owner)
	}
	wg.Wait()
	close(errs)
	close(claimed)
	for claimErr := range errs {
		t.Error(claimErr)
	}
	seen := map[string]struct{}{}
	for batch := range claimed {
		if len(batch) > 2 {
			t.Fatalf("claim returned %d items, cap 2", len(batch))
		}
		for _, item := range batch {
			if _, exists := seen[item.ID]; exists {
				t.Fatalf("item %s claimed twice", item.ID)
			}
			seen[item.ID] = struct{}{}
		}
	}
	if len(seen) != len(candidates) {
		t.Fatalf("claimed %d items, want %d", len(seen), len(candidates))
	}
}

func retentionIntegrationEnv(t *testing.T, key string) string {
	t.Helper()
	value := os.Getenv(key)
	if value == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("db_integration requires %s under CI", key)
		}
		t.Skipf("db_integration requires %s", key)
	}
	return value
}
