package secrets_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/aura/aura/internal/db/migrations"
	"github.com/aura/aura/internal/secrets"
	"github.com/aura/aura/internal/testutil"
)

func openMigratedDB(t *testing.T) *secrets.SQLiteStore {
	t.Helper()
	db := testutil.OpenTestDB(t, migrations.Run)
	return secrets.NewSQLiteStore(db)
}

func TestMigrationAppliesOnFreshDB(t *testing.T) {
	db := testutil.OpenTestDB(t, migrations.Run)
	// Verify the secrets table was created by the migration.
	var name string
	err := db.QueryRowContext(context.Background(),
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'secrets'`,
	).Scan(&name)
	if err != nil {
		t.Fatalf("secrets table not found after migration: %v", err)
	}
	if name != "secrets" {
		t.Fatalf("unexpected table name: %q", name)
	}
}

func TestMigrationSchema(t *testing.T) {
	db := testutil.OpenTestDB(t, migrations.Run)
	ctx := context.Background()

	// Verify columns match the spec: key (PK), value, updated_at.
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(secrets)`)
	if err != nil {
		t.Fatalf("PRAGMA table_info: %v", err)
	}
	defer rows.Close()

	cols := map[string]int{} // name → pk flag
	for rows.Next() {
		var cid, notNull, pk int
		var colName, typ string
		var defaultValue interface{}
		if err := rows.Scan(&cid, &colName, &typ, &notNull, &defaultValue, &pk); err != nil {
			t.Fatalf("scan: %v", err)
		}
		cols[colName] = pk
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}

	for _, want := range []string{"key", "value", "updated_at"} {
		if _, ok := cols[want]; !ok {
			t.Errorf("secrets table missing column %q; got %v", want, cols)
		}
	}
	if cols["key"] != 1 {
		t.Errorf("secrets.key is not the primary key; pk=%d", cols["key"])
	}
}

func TestSetGetRoundtrip(t *testing.T) {
	store := openMigratedDB(t)
	ctx := context.Background()

	if err := store.Set(ctx, secrets.KeyTelegramToken, "bot:12345:abcDEF"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, err := store.Get(ctx, secrets.KeyTelegramToken)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "bot:12345:abcDEF" {
		t.Fatalf("Get = %q, want %q", got, "bot:12345:abcDEF")
	}
}

func TestGetMissingKeyReturnsSentinel(t *testing.T) {
	store := openMigratedDB(t)
	ctx := context.Background()

	_, err := store.Get(ctx, "nonexistent_key")
	if !errors.Is(err, secrets.ErrSecretNotFound) {
		t.Fatalf("Get missing key error = %v, want ErrSecretNotFound", err)
	}
}

func TestGetMissingKeyErrorStringDoesNotContainValue(t *testing.T) {
	store := openMigratedDB(t)
	ctx := context.Background()

	_, err := store.Get(ctx, "my_secret_key")
	// The error must never contain a value (there is none here, but the
	// key name in the error is fine — it is not a secret value).
	if err == nil {
		t.Fatal("expected error for missing key")
	}
	// Verify the error does not contain a fabricated value.
	if strings.Contains(err.Error(), "supersecret") {
		t.Fatalf("error message contains secret value: %v", err)
	}
}

func TestDeleteThenGetReturnsSentinel(t *testing.T) {
	store := openMigratedDB(t)
	ctx := context.Background()

	if err := store.Set(ctx, secrets.KeyLLMAPIKey, "sk-test"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := store.Delete(ctx, secrets.KeyLLMAPIKey); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err := store.Get(ctx, secrets.KeyLLMAPIKey)
	if !errors.Is(err, secrets.ErrSecretNotFound) {
		t.Fatalf("Get after Delete error = %v, want ErrSecretNotFound", err)
	}
}

func TestDeleteNonExistentKeyIsNoop(t *testing.T) {
	store := openMigratedDB(t)
	ctx := context.Background()

	if err := store.Delete(ctx, "never_set"); err != nil {
		t.Fatalf("Delete nonexistent key: %v", err)
	}
}

func TestListReturnsKeysOnly(t *testing.T) {
	store := openMigratedDB(t)
	ctx := context.Background()

	keys := []string{secrets.KeyTelegramToken, secrets.KeyLLMAPIKey, secrets.KeyEmbeddingAPIKey}
	for i, k := range keys {
		if err := store.Set(ctx, k, "some_value_that_must_not_appear"); err != nil {
			t.Fatalf("Set[%d]: %v", i, err)
		}
	}

	listed, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed) != len(keys) {
		t.Fatalf("List returned %d keys, want %d", len(listed), len(keys))
	}
	for _, k := range listed {
		if strings.Contains(k, "some_value_that_must_not_appear") {
			t.Fatalf("List returned a value instead of a key: %q", k)
		}
	}
}

func TestListEmptyStoreReturnsNil(t *testing.T) {
	store := openMigratedDB(t)
	ctx := context.Background()

	listed, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List on empty store: %v", err)
	}
	if len(listed) != 0 {
		t.Fatalf("List on empty store = %v, want empty", listed)
	}
}

func TestSetOverwritesPreviousValue(t *testing.T) {
	store := openMigratedDB(t)
	ctx := context.Background()

	if err := store.Set(ctx, secrets.KeyQdrantAPIKey, "first"); err != nil {
		t.Fatalf("Set first: %v", err)
	}
	if err := store.Set(ctx, secrets.KeyQdrantAPIKey, "second"); err != nil {
		t.Fatalf("Set second: %v", err)
	}
	got, err := store.Get(ctx, secrets.KeyQdrantAPIKey)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "second" {
		t.Fatalf("Get after overwrite = %q, want %q", got, "second")
	}
}

func TestConcurrentSetGetIsSafe(t *testing.T) {
	store := openMigratedDB(t)
	ctx := context.Background()

	const workers = 8
	const rounds = 20
	var wg sync.WaitGroup
	errs := make(chan error, workers*rounds*2)

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < rounds; i++ {
				if err := store.Set(ctx, secrets.KeyGarageS3AccessKey, "value"); err != nil {
					errs <- err
				}
				if _, err := store.Get(ctx, secrets.KeyGarageS3AccessKey); err != nil {
					errs <- err
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent error: %v", err)
	}
}

func TestAllCanonicalKeyConstants(t *testing.T) {
	// Verify all 6 canonical key constants are non-empty and distinct.
	keys := []string{
		secrets.KeyTelegramToken,
		secrets.KeyLLMAPIKey,
		secrets.KeyEmbeddingAPIKey,
		secrets.KeyGarageS3AccessKey,
		secrets.KeyGarageS3SecretKey,
		secrets.KeyQdrantAPIKey,
	}
	seen := map[string]struct{}{}
	for _, k := range keys {
		if k == "" {
			t.Error("canonical key constant is empty string")
		}
		if _, dup := seen[k]; dup {
			t.Errorf("duplicate canonical key constant: %q", k)
		}
		seen[k] = struct{}{}
	}
}
