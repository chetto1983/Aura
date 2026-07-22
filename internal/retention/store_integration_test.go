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
	if protectedRetentionIntegrationDatabase(database) {
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
	operation, created, err := store.SavePlan(ctx, plan, now)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("unique retention plan was reported as a replay")
	}
	if items, claimErr := store.Claim(ctx, operation.ID, "unauthorized", 1, now, time.Minute); claimErr != nil || len(items) != 0 {
		t.Fatalf("claim before first authorization = %+v, %v", items, claimErr)
	}
	authorized, err := store.Authorize(ctx, operation.ID, plan.Token, plan.PolicyVersion, now)
	if err != nil || !authorized {
		t.Fatalf("Authorize() = %v, %v", authorized, err)
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
	assertDisposableRetentionDatabase(t, ctx, pool)

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
	operation, created, err := store.SavePlan(ctx, plan, now)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("unique retention plan was reported as a replay")
	}
	loaded, err := store.GetByToken(ctx, plan.Token)
	if err != nil || loaded.ID != operation.ID || loaded.CandidateCount != 3 {
		t.Fatalf("GetByToken() = %+v, %v", loaded, err)
	}
	if authorized, err := store.Authorize(ctx, operation.ID, plan.Token, plan.PolicyVersion, now); err != nil || !authorized {
		t.Fatalf("Authorize() = %v, %v", authorized, err)
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

func TestRetentionPlanRefreshesExpiredUnchangedSnapshotForImmediateApply(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, retentionIntegrationEnv(t, "AURA_DB_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	assertDisposableRetentionDatabase(t, ctx, pool)

	now := time.Now().UTC().Truncate(time.Microsecond)
	candidate := Candidate{
		IdentityID: "retention-refresh-owner", ArtifactID: uuid.NewString(),
		Version: 1, Action: ActionDeleteArtifact, Class: ClassTemporary, Bytes: 7,
	}
	effects := []string{}
	policy := DefaultPolicy(EnvironmentProduction)
	policy.Version = "refresh-" + uuid.NewString()
	policy.BatchSize = 1
	engine := &Engine{
		Policy: policy, Source: staticSource{candidate}, Store: NewStore(pool),
		Revalidator: &fakeRevalidator{version: 1},
		Remover:     &fakeRemover{effects: &effects, result: RemovalResult{Bytes: 7}},
		Finalizer:   &fakeFinalizer{effects: &effects},
		WorkerID:    "refresh-worker", PlanValidity: time.Minute,
		Now: func() time.Time { return now },
	}
	first, err := engine.Plan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	refreshed, err := engine.Plan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.Token != first.Token {
		t.Fatalf("unchanged snapshot token = %q, want %q", refreshed.Token, first.Token)
	}
	operation, err := engine.Store.GetByToken(ctx, refreshed.Token)
	if err != nil {
		t.Fatal(err)
	}
	if !operation.CreatedAt.Equal(now) {
		t.Fatalf("refreshed authorization time = %s, want %s", operation.CreatedAt, now)
	}
	report, err := engine.Apply(ctx, refreshed.Token)
	if err != nil {
		t.Fatalf("immediate Apply after refreshed Plan: %v", err)
	}
	if report.Completed != 1 || report.Bytes != 7 {
		t.Fatalf("Apply report = %+v", report)
	}
}

func TestRetentionPlanRefreshNeverMutatesNonPlannedOperation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, retentionIntegrationEnv(t, "AURA_DB_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	assertDisposableRetentionDatabase(t, ctx, pool)

	for _, status := range []Status{StatusDeleting, StatusRetryable, StatusCompleted} {
		t.Run(string(status), func(t *testing.T) {
			plan, buildErr := BuildPlan("immutable-"+uuid.NewString(), nil, 1)
			if buildErr != nil {
				t.Fatal(buildErr)
			}
			store := NewStore(pool)
			created := time.Now().UTC().Truncate(time.Microsecond)
			operation, inserted, saveErr := store.SavePlan(ctx, plan, created)
			if saveErr != nil {
				t.Fatal(saveErr)
			}
			if !inserted {
				t.Fatal("unique retention plan was reported as a replay")
			}
			updated := created.Add(10 * time.Second)
			if _, execErr := pool.Exec(ctx, `
				UPDATE aura.retention_operations
				SET status = $1, updated_at = $2
				WHERE id = $3`, status, updated, operation.ID); execErr != nil {
				t.Fatal(execErr)
			}

			replayed, inserted, saveErr := store.SavePlan(ctx, plan, created.Add(2*defaultPlanValidity))
			if saveErr != nil {
				t.Fatal(saveErr)
			}
			if inserted {
				t.Fatal("existing retention plan replay was reported as newly inserted")
			}
			var gotStatus Status
			var gotCreated, gotUpdated time.Time
			if queryErr := pool.QueryRow(ctx, `
				SELECT status, created_at, updated_at
				FROM aura.retention_operations
				WHERE id = $1`, operation.ID).Scan(&gotStatus, &gotCreated, &gotUpdated); queryErr != nil {
				t.Fatal(queryErr)
			}
			if replayed.ID != operation.ID || replayed.Status != status || gotStatus != status {
				t.Fatalf("replayed operation/status = %+v/%s, want id %s status %s", replayed, gotStatus, operation.ID, status)
			}
			if !gotCreated.Equal(created) || !gotUpdated.Equal(updated) {
				t.Fatalf("non-planned timestamps changed: created=%s updated=%s", gotCreated, gotUpdated)
			}
		})
	}
}

func TestRetentionBacklogIsDurableAcrossReplayRestartAndTerminalTransitions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, retentionIntegrationEnv(t, "AURA_DB_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	assertDisposableRetentionDatabase(t, ctx, pool)

	store := NewStore(pool)
	baseline, err := store.PendingCount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	candidates := []Candidate{
		{IdentityID: "retention-backlog-owner", ArtifactID: uuid.NewString(), Version: 1, Action: ActionDeleteArtifact, Class: ClassTemporary, Bytes: 3},
		{IdentityID: "retention-backlog-owner", ArtifactID: uuid.NewString(), Version: 1, Action: ActionDeleteArtifact, Class: ClassCrashArtifact, Bytes: 5},
	}
	plan, err := BuildPlan("backlog-"+uuid.NewString(), candidates, len(candidates))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	operation, created, err := store.SavePlan(ctx, plan, now)
	if err != nil || !created {
		t.Fatalf("first SavePlan operation=%+v created=%t err=%v", operation, created, err)
	}
	assertRetentionBacklogCount(t, ctx, store, baseline+int64(len(candidates)))

	restarted := NewStore(pool)
	assertRetentionBacklogCount(t, ctx, restarted, baseline+int64(len(candidates)))
	replayed, created, err := restarted.SavePlan(ctx, plan, now.Add(time.Minute))
	if err != nil || created || replayed.ID != operation.ID {
		t.Fatalf("replayed SavePlan operation=%+v created=%t err=%v", replayed, created, err)
	}
	assertRetentionBacklogCount(t, ctx, restarted, baseline+int64(len(candidates)))

	if authorized, authorizeErr := restarted.Authorize(ctx, operation.ID, plan.Token, plan.PolicyVersion, now); authorizeErr != nil || !authorized {
		t.Fatalf("Authorize()=%t, %v", authorized, authorizeErr)
	}
	items, err := restarted.Claim(ctx, operation.ID, "backlog-worker", len(candidates), now, time.Minute)
	if err != nil || len(items) != len(candidates) {
		t.Fatalf("Claim()=%d, %v", len(items), err)
	}
	for _, item := range items {
		if err := restarted.FailItem(ctx, item.ID, "backlog-worker", "test_terminal", now); err != nil {
			t.Fatal(err)
		}
	}
	assertRetentionBacklogCount(t, ctx, restarted, baseline)
}

func TestRetentionStoreFailsClosedAfterPoolShutdown(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, retentionIntegrationEnv(t, "AURA_DB_URL"))
	if err != nil {
		t.Fatal(err)
	}
	assertDisposableRetentionDatabase(t, ctx, pool)
	store := NewStore(pool)
	if _, _, err := store.SavePlan(ctx, Plan{}, time.Now()); err == nil {
		pool.Close()
		t.Fatal("invalid plan succeeded before database access")
	}
	pool.Close()

	candidate := Candidate{
		IdentityID: "retention-closed-pool", ArtifactID: uuid.NewString(), Version: 1,
		Action: ActionDeleteArtifact, Class: ClassTemporary, Bytes: 1,
	}
	plan, err := BuildPlan("closed-pool-"+uuid.NewString(), []Candidate{candidate}, 1)
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.NewString()
	checks := map[string]func() error{
		"save": func() error {
			_, _, err := store.SavePlan(ctx, plan, time.Now())
			return err
		},
		"count": func() error { _, err := store.PendingCount(ctx); return err },
		"get":   func() error { _, err := store.GetByToken(ctx, plan.Token); return err },
		"authorize": func() error {
			_, err := store.Authorize(ctx, id, plan.Token, plan.PolicyVersion, time.Now())
			return err
		},
		"claim": func() error {
			_, err := store.Claim(ctx, id, "worker", 1, time.Now(), time.Minute)
			return err
		},
		"record":        func() error { return store.RecordArtifact(ctx, id, "worker", ArtifactRemoved, 1, "", time.Now()) },
		"finalize item": func() error { return store.FinalizeItem(ctx, id, "worker", time.Now()) },
		"retry":         func() error { return store.RetryItem(ctx, id, "worker", "test", time.Now()) },
		"fail":          func() error { return store.FailItem(ctx, id, "worker", "test", time.Now()) },
		"finalize operation": func() error {
			_, err := store.FinalizeOperation(ctx, id, time.Now())
			return err
		},
	}
	for name, check := range checks {
		t.Run(name, func(t *testing.T) {
			if err := check(); err == nil {
				t.Fatal("operation succeeded against a closed pool")
			}
		})
	}
}

func assertDisposableRetentionDatabase(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var database string
	if err := pool.QueryRow(ctx, `SELECT current_database()`).Scan(&database); err != nil {
		t.Fatal(err)
	}
	if protectedRetentionIntegrationDatabase(database) {
		t.Fatalf("refusing db_integration database %q; use a disposable database", database)
	}
}

func protectedRetentionIntegrationDatabase(name string) bool {
	return (name == "aura" || name == "postgres") && os.Getenv("GITHUB_ACTIONS") != "true"
}

func assertRetentionBacklogCount(t *testing.T, ctx context.Context, store *Store, want int64) {
	t.Helper()
	got, err := store.PendingCount(ctx)
	if err != nil || got != want {
		t.Fatalf("PendingCount()=%d, %v; want %d", got, err, want)
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
