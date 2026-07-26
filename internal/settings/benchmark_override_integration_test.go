//go:build db_integration

package settings

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestBenchmarkOverrideRestoresMixedRowsExactlyAndUnblocksWriter(t *testing.T) {
	pool := benchmarkSettingsPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	original := seedBenchmarkSettings(t, ctx, pool)

	override, err := BeginQwenPortabilityOverride(ctx, pool, BenchmarkOverrideRequest{
		RunID:         uuid.Must(uuid.NewV7()),
		OwnerID:       benchmarkSettingsOwner(t, ctx, pool),
		LeaseDuration: time.Minute,
	})
	if err != nil {
		t.Fatalf("begin override: %v", err)
	}
	assertBenchmarkValues(t, ctx, pool, QwenPortabilitySettings)
	if err := override.Heartbeat(ctx); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}

	writerDone := make(chan error, 1)
	go func() {
		_, writeErr := NewStore(pool).Upsert(
			ctx, "AURA_LLM_MODEL", "writer-after-restore", "integration-writer",
		)
		writerDone <- writeErr
	}()
	select {
	case err := <-writerDone:
		t.Fatalf("writer completed while session lock was held: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	if err := override.Restore(ctx); err != nil {
		t.Fatalf("restore: %v", err)
	}
	select {
	case err := <-writerDone:
		if err != nil {
			t.Fatalf("waiting writer: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("waiting writer stayed blocked after exact restore")
	}
	assertOriginalExceptWriter(t, ctx, pool, original)
}

func TestBenchmarkOverrideRecoversExpiredJournal(t *testing.T) {
	pool := benchmarkSettingsPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	original := seedBenchmarkSettings(t, ctx, pool)
	override, err := BeginQwenPortabilityOverride(ctx, pool, BenchmarkOverrideRequest{
		RunID:         uuid.Must(uuid.NewV7()),
		OwnerID:       benchmarkSettingsOwner(t, ctx, pool),
		LeaseDuration: time.Minute,
	})
	if err != nil {
		t.Fatalf("begin override: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE aura.benchmark_settings_overrides
		SET lease_expires_at = clock_timestamp() - interval '1 second'
		WHERE run_id = $1`, override.runID); err != nil {
		t.Fatalf("expire journal: %v", err)
	}
	releaseBenchmarkSettingsConn(ctx, override.conn)
	override.conn = nil

	if err := RecoverExpiredBenchmarkOverride(ctx, pool); err != nil {
		t.Fatalf("recover expired override: %v", err)
	}
	assertExactRows(t, ctx, pool, original)
}

func TestBenchmarkOverrideCanceledCleanupRestoresAndReleasesLock(t *testing.T) {
	pool := benchmarkSettingsPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	original := seedBenchmarkSettings(t, ctx, pool)
	override, err := BeginQwenPortabilityOverride(ctx, pool, BenchmarkOverrideRequest{
		RunID:         uuid.Must(uuid.NewV7()),
		OwnerID:       benchmarkSettingsOwner(t, ctx, pool),
		LeaseDuration: time.Minute,
	})
	if err != nil {
		t.Fatalf("begin override: %v", err)
	}
	canceled, cancelCleanup := context.WithCancel(ctx)
	cancelCleanup()
	if err := override.Restore(canceled); err != nil {
		t.Fatalf("restore with canceled caller context: %v", err)
	}
	assertExactRows(t, ctx, pool, original)
	writerCtx, cancelWriter := context.WithTimeout(ctx, time.Second)
	defer cancelWriter()
	if _, err := NewStore(pool).Upsert(
		writerCtx, "AURA_LLM_MODEL", "writer-after-cancel", "integration-writer",
	); err != nil {
		t.Fatalf("writer remained blocked after canceled cleanup: %v", err)
	}
}

func TestBenchmarkOverrideApplyAndRestoreFailuresRemainRecoverable(t *testing.T) {
	pool := benchmarkSettingsPool(t)
	migratePool := benchmarkSettingsMigratePool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	seedBenchmarkSettings(t, ctx, pool)
	owner := benchmarkSettingsOwner(t, ctx, pool)
	installSettingsFailureTrigger(t, ctx, migratePool, "apply")
	_, err := BeginQwenPortabilityOverride(ctx, pool, BenchmarkOverrideRequest{
		RunID: uuid.Must(uuid.NewV7()), OwnerID: owner, LeaseDuration: time.Minute,
	})
	if err == nil {
		t.Fatal("begin override succeeded through injected apply failure")
	}
	removeSettingsFailureTrigger(t, ctx, migratePool)

	override, err := BeginQwenPortabilityOverride(ctx, pool, BenchmarkOverrideRequest{
		RunID: uuid.Must(uuid.NewV7()), OwnerID: owner, LeaseDuration: time.Minute,
	})
	if err != nil {
		t.Fatalf("begin override after apply rollback: %v", err)
	}
	installSettingsFailureTrigger(t, ctx, migratePool, "restore")
	if err := override.Restore(ctx); err == nil {
		t.Fatal("restore succeeded through injected restore failure")
	}
	removeSettingsFailureTrigger(t, ctx, migratePool)
	if err := override.Restore(ctx); err != nil {
		t.Fatalf("retry restore: %v", err)
	}
}

func TestBenchmarkOverrideLifecycleAndSettingsCRUD(t *testing.T) {
	pool := benchmarkSettingsPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	seedBenchmarkSettings(t, ctx, pool)
	override, err := BeginQwenPortabilityOverride(ctx, pool, BenchmarkOverrideRequest{
		RunID: uuid.Must(uuid.NewV7()), OwnerID: benchmarkSettingsOwner(t, ctx, pool),
		LeaseDuration: time.Minute,
	})
	if err != nil {
		t.Fatalf("begin override: %v", err)
	}
	if err := RecoverExpiredBenchmarkOverride(ctx, pool); !errors.Is(err, ErrBenchmarkOverrideActive) {
		t.Fatalf("concurrent recovery error = %v, want ErrBenchmarkOverrideActive", err)
	}
	if err := override.Restore(ctx); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if err := override.Restore(ctx); err != nil {
		t.Fatalf("idempotent restore: %v", err)
	}
	if err := override.Heartbeat(ctx); !errors.Is(err, ErrInvalidBenchmarkOverride) {
		t.Fatalf("post-restore heartbeat error = %v, want ErrInvalidBenchmarkOverride", err)
	}
	store := NewStore(pool)
	rows, err := store.List(ctx)
	if err != nil || len(rows) == 0 {
		t.Fatalf("List = (%d rows, %v), want seeded rows", len(rows), err)
	}
	if err := store.Delete(ctx, "AURA_LLM_BASE_URL"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

func TestBenchmarkOverrideDatabaseFailureBranches(t *testing.T) {
	pool := benchmarkSettingsPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	seedBenchmarkSettings(t, ctx, pool)
	request := BenchmarkOverrideRequest{
		RunID: uuid.Must(uuid.NewV7()), OwnerID: benchmarkSettingsOwner(t, ctx, pool),
		LeaseDuration: time.Minute,
	}
	override, err := BeginQwenPortabilityOverride(ctx, pool, request)
	if err != nil {
		t.Fatalf("begin override: %v", err)
	}
	canceled, cancelHeartbeat := context.WithCancel(ctx)
	cancelHeartbeat()
	if err := override.Heartbeat(canceled); err == nil {
		t.Fatal("heartbeat succeeded with canceled context")
	}
	writerCtx, cancelWriter := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancelWriter()
	if _, err := NewStore(pool).Upsert(
		writerCtx, "AURA_LLM_MODEL", "blocked-writer", "integration-writer",
	); err == nil {
		t.Fatal("writer acquired the held benchmark lock")
	}
	releaseBenchmarkSettingsConn(ctx, override.conn)
	override.conn = nil
	nextRequest := request
	nextRequest.RunID = uuid.Must(uuid.NewV7())
	if _, err := BeginQwenPortabilityOverride(ctx, pool, nextRequest); !errors.Is(
		err, ErrBenchmarkOverrideActive,
	) {
		t.Fatalf("begin through unexpired predecessor error = %v, want active", err)
	}
	if err := RecoverExpiredBenchmarkOverride(ctx, pool); !errors.Is(err, ErrBenchmarkOverrideActive) {
		t.Fatalf("unexpired recovery error = %v, want ErrBenchmarkOverrideActive", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE aura.benchmark_settings_overrides
		SET lease_expires_at = clock_timestamp() - interval '1 second'
		WHERE run_id = $1`, override.runID); err != nil {
		t.Fatalf("expire abandoned override: %v", err)
	}
	if err := RecoverExpiredBenchmarkOverride(ctx, pool); err != nil {
		t.Fatalf("recover abandoned override: %v", err)
	}

	unlockedConn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire unlocked connection: %v", err)
	}
	releaseBenchmarkSettingsConn(ctx, unlockedConn)
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("pool after hijacking unlocked connection: %v", err)
	}

	closedPool, err := pgxpool.New(ctx, os.Getenv("AURA_DB_URL"))
	if err != nil {
		t.Fatalf("open pool for closed-pool branches: %v", err)
	}
	closedPool.Close()
	if _, err := BeginQwenPortabilityOverride(ctx, closedPool, request); err == nil {
		t.Fatal("begin succeeded with closed pool")
	}
	if err := RecoverExpiredBenchmarkOverride(ctx, closedPool); err == nil {
		t.Fatal("recovery succeeded with closed pool")
	}
	if _, err := NewStore(closedPool).Upsert(
		ctx, "AURA_LLM_MODEL", "closed", "integration-writer",
	); err == nil {
		t.Fatal("settings upsert succeeded with closed pool")
	}
	assertTransactionalPanicPaths(t, ctx, pool)
}

type benchmarkOriginalRows map[string]benchmarkRow

type benchmarkRow struct {
	exists    bool
	value     string
	isSecret  bool
	updatedAt time.Time
	updatedBy *string
}

func benchmarkSettingsPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("AURA_DB_URL")
	if url == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("AURA_DB_URL is required under CI")
		}
		t.Skip("AURA_DB_URL is not set")
	}
	migrateURL := os.Getenv("AURA_DB_MIGRATE_URL")
	if migrateURL == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("AURA_DB_MIGRATE_URL is required under CI")
		}
		t.Skip("AURA_DB_MIGRATE_URL is not set")
	}
	if err := validateBenchmarkIntegrationDSNs(url, migrateURL); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Migrate(t.Context(), migrateURL); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := pgxpool.New(t.Context(), url)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func benchmarkSettingsMigratePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("AURA_DB_MIGRATE_URL")
	if url == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("AURA_DB_MIGRATE_URL is required under CI")
		}
		t.Skip("AURA_DB_MIGRATE_URL is not set")
	}
	if err := validateBenchmarkIntegrationDSNs(os.Getenv("AURA_DB_URL"), url); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(t.Context(), url)
	if err != nil {
		t.Fatalf("open migration pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func assertTransactionalPanicPaths(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool,
) {
	t.Helper()
	assertPanics(t, func() {
		_ = NewStore(pool).withWriteLock(ctx, func(*sqlc.Queries) error {
			panic("settings write panic")
		})
	})
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire panic-path connection: %v", err)
	}
	defer conn.Release()
	assertPanics(t, func() {
		_ = withSettingsConnTx(ctx, conn, func(*sqlc.Queries) error {
			panic("benchmark transaction panic")
		})
	})
}

func assertPanics(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Error("function did not propagate panic")
		}
	}()
	fn()
}

func benchmarkSettingsOwner(t *testing.T, ctx context.Context, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(ctx,
		`INSERT INTO aura.identities (id, name, kind) VALUES ($1, $2, 'user')`,
		id, "settings-benchmark-"+id.String(),
	); err != nil {
		t.Fatalf("insert owner: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM aura.identities WHERE id = $1`, id)
	})
	return id
}

func seedBenchmarkSettings(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool,
) benchmarkOriginalRows {
	t.Helper()
	if err := RecoverExpiredBenchmarkOverride(ctx, pool); err != nil &&
		!errors.Is(err, ErrBenchmarkOverrideActive) {
		t.Fatalf("pre-test recovery: %v", err)
	}
	for _, key := range benchmarkSettingKeys {
		if _, err := pool.Exec(ctx, `DELETE FROM aura.settings WHERE key = $1`, key); err != nil {
			t.Fatalf("clear %s: %v", key, err)
		}
	}
	actor := "original-actor"
	when := time.Date(2026, 7, 20, 8, 9, 10, 123456000, time.UTC)
	if _, err := pool.Exec(ctx, `
		INSERT INTO aura.settings (key, value, is_secret, updated_at, updated_by)
		VALUES
			('AURA_LLM_PROVIDER', 'openrouter', false, $1, $2),
			('AURA_LLM_BASE_URL', 'https://example.invalid/v1', false, $1, NULL)`,
		when, actor,
	); err != nil {
		t.Fatalf("seed settings: %v", err)
	}
	rows := readBenchmarkRows(t, ctx, pool)
	t.Cleanup(func() {
		restoreFixtureRows(context.Background(), pool, rows)
	})
	return rows
}

func readBenchmarkRows(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool,
) benchmarkOriginalRows {
	t.Helper()
	result := make(benchmarkOriginalRows, len(benchmarkSettingKeys))
	for _, key := range benchmarkSettingKeys {
		var row benchmarkRow
		var updatedBy *string
		err := pool.QueryRow(ctx, `
			SELECT value, is_secret, updated_at, updated_by
			FROM aura.settings WHERE key = $1`, key,
		).Scan(&row.value, &row.isSecret, &row.updatedAt, &updatedBy)
		if err == nil {
			row.exists = true
			row.updatedBy = updatedBy
		}
		result[key] = row
	}
	return result
}

func assertExactRows(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, want benchmarkOriginalRows,
) {
	t.Helper()
	got := readBenchmarkRows(t, ctx, pool)
	for key, expected := range want {
		actual := got[key]
		if actual.exists != expected.exists || actual.value != expected.value ||
			actual.isSecret != expected.isSecret || !actual.updatedAt.Equal(expected.updatedAt) ||
			!equalStringPointers(actual.updatedBy, expected.updatedBy) {
			t.Errorf("%s = %#v, want %#v", key, actual, expected)
		}
	}
}

func equalStringPointers(a, b *string) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

func assertBenchmarkValues(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, want BenchmarkModelSettings,
) {
	t.Helper()
	got := readBenchmarkRows(t, ctx, pool)
	for key, value := range map[string]string{
		"AURA_LLM_PROVIDER": want.Provider,
		"AURA_LLM_MODEL":    want.Model,
		"AURA_LLM_BASE_URL": want.BaseURL,
	} {
		if !got[key].exists || got[key].value != value || got[key].isSecret {
			t.Errorf("%s = %#v, want non-secret value %q", key, got[key], value)
		}
	}
}

func assertOriginalExceptWriter(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, original benchmarkOriginalRows,
) {
	t.Helper()
	got := readBenchmarkRows(t, ctx, pool)
	if got["AURA_LLM_MODEL"].value != "writer-after-restore" {
		t.Errorf("model = %q, want waiting writer value", got["AURA_LLM_MODEL"].value)
	}
	delete(got, "AURA_LLM_MODEL")
	delete(original, "AURA_LLM_MODEL")
	for key, expected := range original {
		actual := got[key]
		if actual.exists != expected.exists || actual.value != expected.value ||
			actual.isSecret != expected.isSecret || !actual.updatedAt.Equal(expected.updatedAt) ||
			!equalStringPointers(actual.updatedBy, expected.updatedBy) {
			t.Errorf("%s = %#v, want %#v", key, actual, expected)
		}
	}
}

func installSettingsFailureTrigger(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, phase string,
) {
	t.Helper()
	sql := `
		CREATE OR REPLACE FUNCTION aura.fail_benchmark_settings_test() RETURNS trigger
		LANGUAGE plpgsql AS $$
		BEGIN
			IF ($1 = 'apply' AND NEW.value IN ('llamacpp', 'qwen'))
			   OR ($1 = 'restore' AND OLD.value IN ('llamacpp', 'qwen')) THEN
				RAISE EXCEPTION 'injected settings failure';
			END IF;
			RETURN NEW;
		END; $$;
		CREATE TRIGGER fail_benchmark_settings_test
		BEFORE INSERT OR UPDATE ON aura.settings
		FOR EACH ROW EXECUTE FUNCTION aura.fail_benchmark_settings_test()`
	sql = replaceTriggerPhase(sql, phase)
	if _, err := pool.Exec(ctx, sql); err != nil {
		t.Fatalf("install %s trigger: %v", phase, err)
	}
}

func replaceTriggerPhase(sql, phase string) string {
	if phase == "apply" {
		return stringReplace(sql, "$1", "'apply'")
	}
	return stringReplace(sql, "$1", "'restore'")
}

func stringReplace(value, old, replacement string) string {
	for {
		index := -1
		for i := 0; i+len(old) <= len(value); i++ {
			if value[i:i+len(old)] == old {
				index = i
				break
			}
		}
		if index < 0 {
			return value
		}
		value = value[:index] + replacement + value[index+len(old):]
	}
}

func removeSettingsFailureTrigger(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		DROP TRIGGER IF EXISTS fail_benchmark_settings_test ON aura.settings;
		DROP FUNCTION IF EXISTS aura.fail_benchmark_settings_test()`); err != nil {
		t.Fatalf("remove settings failure trigger: %v", err)
	}
}

func restoreFixtureRows(ctx context.Context, pool *pgxpool.Pool, rows benchmarkOriginalRows) {
	for _, key := range benchmarkSettingKeys {
		row := rows[key]
		if !row.exists {
			_, _ = pool.Exec(ctx, `DELETE FROM aura.settings WHERE key = $1`, key)
			continue
		}
		_, _ = pool.Exec(ctx, `
			INSERT INTO aura.settings (key, value, is_secret, updated_at, updated_by)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (key) DO UPDATE SET
				value = EXCLUDED.value,
				is_secret = EXCLUDED.is_secret,
				updated_at = EXCLUDED.updated_at,
				updated_by = EXCLUDED.updated_by`,
			key, row.value, row.isSecret, row.updatedAt, row.updatedBy,
		)
	}
}
