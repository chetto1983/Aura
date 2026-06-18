//go:build db_integration

package assets

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/jackc/pgx/v5/pgxpool"
)

const localIdentityID = "00000000-0000-0000-0000-000000000001"

func assetEnvOrSkip(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("asset store integration requires %s under CI", key)
		}
		t.Skipf("asset store integration requires %s", key)
	}
	return v
}

func migratedAssetPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pwd := assetEnvOrSkip(t, "POSTGRES_PASSWORD")
	migrateURL := assetEnvOrSkip(t, "AURA_DB_MIGRATE_URL")
	appURL := assetEnvOrSkip(t, "AURA_DB_URL")
	host := os.Getenv("PGHOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("PGPORT")
	if port == "" {
		port = "5432"
	}
	bootstrap := fmt.Sprintf("postgres://aura:%s@%s:%s/aura?sslmode=disable", pwd, host, port)
	if err := db.EnsureRoles(ctx, bootstrap, pwd); err != nil {
		t.Fatalf("EnsureRoles: %v", err)
	}
	if _, err := db.Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	pool, err := db.Open(ctx, &db.Config{URL: appURL})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestPostgresAssetStoreRoundTrip(t *testing.T) {
	pool := migratedAssetPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	now := time.Now().UnixNano()
	threadID := fmt.Sprintf("asset-thread-%d", now)
	objectKey := fmt.Sprintf("assets/%d/manual.pdf", now)
	store := NewStore(pool)

	created, err := store.Create(ctx, CreateRequest{
		IdentityID:        localIdentityID,
		SourceKind:        SourceWeb,
		SourceRef:         "https://example.test/manual.pdf",
		ThreadID:          threadID,
		Scope:             ScopeThread,
		Modality:          ModalityDocument,
		FileName:          "manual.pdf",
		MIMEType:          "application/pdf",
		DeclaredSizeBytes: 42,
		ObjectBucket:      "asset-test",
		ObjectKey:         objectKey,
		Metadata:          map[string]any{"origin": "integration", "label": "manual"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.Status != StatusPresigned {
		t.Fatalf("created asset = %#v", created)
	}

	got, err := store.GetForIdentity(ctx, created.ID, localIdentityID)
	if err != nil {
		t.Fatalf("GetForIdentity: %v", err)
	}
	if got.ID != created.ID || got.FileName != "manual.pdf" || got.Modality != ModalityDocument {
		t.Fatalf("GetForIdentity asset = %#v", got)
	}
	if got.Metadata["origin"] != "integration" || got.Metadata["label"] != "manual" {
		t.Fatalf("metadata round trip = %#v", got.Metadata)
	}

	listed, err := store.ListForThread(ctx, localIdentityID, threadID)
	if err != nil {
		t.Fatalf("ListForThread: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("ListForThread returned %d assets, want 1: %#v", len(listed), listed)
	}
	if listed[0].ID != created.ID || listed[0].FileName != "manual.pdf" || listed[0].Modality != ModalityDocument {
		t.Fatalf("listed asset = %#v", listed[0])
	}
	if listed[0].Metadata["origin"] != "integration" || listed[0].Metadata["label"] != "manual" {
		t.Fatalf("listed metadata round trip = %#v", listed[0].Metadata)
	}
}
