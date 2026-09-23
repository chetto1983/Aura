//go:build db_integration

package pimprovider

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/secret"
)

func appsEnvOrSkip(t *testing.T, key string) string {
	t.Helper()
	value := os.Getenv(key)
	if value == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("pimprovider integration requires %s under CI", key)
		}
		t.Skipf("pimprovider integration requires %s", key)
	}
	return value
}

func liveStore(t *testing.T) (*Store, *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := db.Open(ctx, &db.Config{URL: appsEnvOrSkip(t, "AURA_DB_URL")})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(pool.Close)
	clean := func() { _, _ = pool.Exec(context.Background(), `DELETE FROM aura.pim_provider_app`) }
	clean()
	t.Cleanup(clean)
	store, err := NewStore(pool, appsEnvOrSkip(t, "AURA_AUTHULA_SECRET"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return store, pool
}

func TestStoreSealsTheGoogleSecretAndKeepsItOnAResave(t *testing.T) {
	store, pool := liveStore(t)
	ctx := context.Background()

	if _, err := store.Get(ctx, Google); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("Get before save err = %v, want ErrNotConfigured", err)
	}
	if err := store.Upsert(ctx, App{Provider: Google, ClientID: "cid-1", ClientSecret: "plain-secret"}, "admin-1"); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	var ciphertext []byte
	if err := pool.QueryRow(ctx, `SELECT client_secret_ciphertext FROM aura.pim_provider_app WHERE provider = 'google'`).Scan(&ciphertext); err != nil {
		t.Fatalf("read ciphertext: %v", err)
	}
	if len(ciphertext) == 0 || bytes.Contains(ciphertext, []byte("plain-secret")) {
		t.Fatal("client_secret_ciphertext is empty or holds the plaintext")
	}
	other, err := secret.NewSealer(appsEnvOrSkip(t, "AURA_AUTHULA_SECRET"), "aura-mcp-registry-key-v1")
	if err != nil {
		t.Fatalf("NewSealer: %v", err)
	}
	if _, err := other.Open(ciphertext); err == nil {
		t.Fatal("another store's key opened the PIM provider secret")
	}

	// Same client, empty secret: the keep-secret UPDATE, not the upsert the row CHECK rejects.
	if err := store.Upsert(ctx, App{Provider: Google, ClientID: "cid-1"}, "admin-2"); err != nil {
		t.Fatalf("resave without secret: %v", err)
	}
	got, err := store.Get(ctx, Google)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ClientSecret != "plain-secret" || !got.SecretSet || got.UpdatedBy != "admin-2" {
		t.Fatalf("after resave = %+v, want the stored secret kept and updated_by admin-2", got)
	}
}

func TestStoreKeepSecretOnAMissingRowIsNotConfigured(t *testing.T) {
	store, _ := liveStore(t)
	err := store.Upsert(context.Background(), App{Provider: Google, ClientID: "cid"}, "admin")
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("keep-secret on no row err = %v, want ErrNotConfigured", err)
	}
}

func TestStoreListsWithoutDecrypting(t *testing.T) {
	store, _ := liveStore(t)
	ctx := context.Background()
	if err := store.Upsert(ctx, App{Provider: OutlookCom, ClientID: "ms-cid", TenantID: "consumers"}, "admin"); err != nil {
		t.Fatalf("Upsert outlook: %v", err)
	}
	if err := store.Upsert(ctx, App{Provider: Google, ClientID: "g-cid", ClientSecret: "s"}, "admin"); err != nil {
		t.Fatalf("Upsert google: %v", err)
	}
	apps, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(apps) != 2 || apps[0].Provider != Google || apps[1].Provider != OutlookCom {
		t.Fatalf("List = %+v, want google then outlook.com", apps)
	}
	if !apps[0].SecretSet || apps[0].ClientSecret != "" || apps[1].SecretSet || apps[1].TenantID != "consumers" {
		t.Fatalf("List rows = %+v", apps)
	}
}
