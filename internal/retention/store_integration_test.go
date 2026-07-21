//go:build db_integration

package retention

import (
	"context"
	"errors"
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

func TestRetentionStorePersistsTwoPhaseLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, retentionIntegrationEnv(t, "AURA_DB_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	candidates := []Candidate{
		{IdentityID: "00000000-0000-0000-0000-000000000001", ArtifactID: uuid.NewString(), Version: 1, Action: ActionDeleteArtifact, Class: ClassTemporary, Bytes: 4},
		{IdentityID: "00000000-0000-0000-0000-000000000001", ArtifactID: uuid.NewString(), Version: 2, Action: ActionDeleteArtifact, Class: ClassCrashArtifact, Bytes: 5},
		{IdentityID: "00000000-0000-0000-0000-000000000001", ArtifactID: uuid.NewString(), Version: 3, Action: ActionDeleteArtifact, Class: ClassFullTrace, Bytes: 6},
	}
	plan, err := BuildPlan("lifecycle-"+uuid.NewString(), candidates, len(candidates))
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(pool)
	now := time.Now().UTC()
	operation, err := store.SavePlan(ctx, plan, now)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := store.GetByToken(ctx, plan.Token)
	if err != nil || loaded.ID != operation.ID || loaded.CandidateCount != 3 {
		t.Fatalf("GetByToken() = %+v, %v", loaded, err)
	}
	items, err := store.Claim(ctx, operation.ID, "worker-a", 3, now, time.Minute)
	if err != nil || len(items) != 3 {
		t.Fatalf("Claim() = %d, %v", len(items), err)
	}

	if err := store.RecordArtifact(ctx, items[0].ID, "wrong-worker", ArtifactRemoved, 4, "", now); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("wrong-owner RecordArtifact error = %v", err)
	}
	if err := store.RecordArtifact(ctx, items[0].ID, "worker-a", ArtifactRemoved, 4, "", now); err != nil {
		t.Fatal(err)
	}
	if err := store.FinalizeItem(ctx, items[0].ID, "worker-a", now); err != nil {
		t.Fatal(err)
	}
	if err := store.FinalizeItem(ctx, items[0].ID, "worker-a", now); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("duplicate FinalizeItem error = %v", err)
	}

	if err := store.RetryItem(ctx, items[1].ID, "worker-a", "external_unavailable", now); err != nil {
		t.Fatal(err)
	}
	retried, err := store.Claim(ctx, operation.ID, "worker-b", 1, now.Add(time.Second), time.Minute)
	if err != nil || len(retried) != 1 || retried[0].ID != items[1].ID || retried[0].AttemptCount != 2 {
		t.Fatalf("retry Claim() = %+v, %v", retried, err)
	}
	if err := store.RecordArtifact(ctx, retried[0].ID, "worker-b", ArtifactAbsent, 0, "", now); err != nil {
		t.Fatal(err)
	}
	if err := store.FinalizeItem(ctx, retried[0].ID, "worker-b", now); err != nil {
		t.Fatal(err)
	}

	if err := store.FailItem(ctx, items[2].ID, "worker-a", "ownership_mismatch", now); err != nil {
		t.Fatal(err)
	}
	final, err := store.FinalizeOperation(ctx, operation.ID, now)
	if err != nil || final.Status != StatusFailed || final.CompletedCount != 2 || final.CompletedBytes != 4 || final.FailureCount != 1 {
		t.Fatalf("FinalizeOperation() = %+v, %v", final, err)
	}

	if _, err := store.Claim(ctx, "bad", "worker", 1, now, time.Minute); err == nil {
		t.Fatal("invalid operation ID claim succeeded")
	}
	if _, err := store.Claim(ctx, operation.ID, "", 0, now, 0); err == nil {
		t.Fatal("invalid claim bounds succeeded")
	}
	if _, err := store.FinalizeOperation(ctx, "bad", now); err == nil {
		t.Fatal("invalid operation finalization succeeded")
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
