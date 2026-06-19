//go:build db_integration

package assets

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const localIdentityID = "00000000-0000-0000-0000-000000000001"
const otherIdentityID = "00000000-0000-0000-0000-000000000002"

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

	if _, err := store.MarkUploaded(ctx, created.ID, otherIdentityID, 64, "etag-wrong-identity"); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("MarkUploaded wrong identity error = %v, want pgx.ErrNoRows", err)
	}
	uploaded, err := store.MarkUploaded(ctx, created.ID, localIdentityID, 64, "etag-integration")
	if err != nil {
		t.Fatalf("MarkUploaded: %v", err)
	}
	if uploaded.Status != StatusUploaded || uploaded.SizeBytes != 64 || uploaded.ObjectETag != "etag-integration" || uploaded.UploadedAt.IsZero() {
		t.Fatalf("uploaded asset = %#v", uploaded)
	}

	if _, err := store.MarkAccepted(ctx, uploaded.ID, otherIdentityID, 64, "hash-wrong-identity", "application/pdf"); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("MarkAccepted wrong identity error = %v, want pgx.ErrNoRows", err)
	}
	accepted, err := store.MarkAccepted(ctx, uploaded.ID, localIdentityID, 64, "hash-integration", "application/pdf")
	if err != nil {
		t.Fatalf("MarkAccepted: %v", err)
	}
	if accepted.Status != StatusAccepted || accepted.ContentHash != "hash-integration" || accepted.MIMEType != "application/pdf" || accepted.AcceptedAt.IsZero() {
		t.Fatalf("accepted asset = %#v", accepted)
	}

	if _, err := store.SetStatus(ctx, accepted.ID, otherIdentityID, StatusProcessing, "", ""); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("SetStatus wrong identity error = %v, want pgx.ErrNoRows", err)
	}
	processing, err := store.SetStatus(ctx, accepted.ID, localIdentityID, StatusProcessing, "", "")
	if err != nil {
		t.Fatalf("SetStatus processing: %v", err)
	}
	if processing.Status != StatusProcessing {
		t.Fatalf("processing asset = %#v", processing)
	}

	result := Result{
		Status:     StatusSearchable,
		DocumentID: "doc-integration",
		Summary:    "integration summary",
		Metadata:   map[string]any{"origin": "integration", "stage": "result"},
	}
	if _, err := store.SetResult(ctx, processing.ID, otherIdentityID, result); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("SetResult wrong identity error = %v, want pgx.ErrNoRows", err)
	}
	searchable, err := store.SetResult(ctx, processing.ID, localIdentityID, result)
	if err != nil {
		t.Fatalf("SetResult: %v", err)
	}
	if searchable.Status != StatusSearchable || searchable.DocumentID != "doc-integration" || searchable.SearchableAt.IsZero() {
		t.Fatalf("searchable asset = %#v", searchable)
	}
	if searchable.Metadata["stage"] != "result" {
		t.Fatalf("result metadata = %#v", searchable.Metadata)
	}

	deleted, err := store.SetStatus(ctx, searchable.ID, localIdentityID, StatusDeleted, "", "")
	if err != nil {
		t.Fatalf("SetStatus deleted: %v", err)
	}
	if deleted.Status != StatusDeleted || deleted.DeletedAt.IsZero() {
		t.Fatalf("deleted asset = %#v", deleted)
	}

	if _, err := store.MarkUploaded(ctx, deleted.ID, localIdentityID, 65, "etag-deleted"); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("MarkUploaded deleted asset error = %v, want pgx.ErrNoRows", err)
	}
	if _, err := store.MarkAccepted(ctx, deleted.ID, localIdentityID, 65, "hash-deleted", "application/pdf"); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("MarkAccepted deleted asset error = %v, want pgx.ErrNoRows", err)
	}
	if _, err := store.SetStatus(ctx, deleted.ID, localIdentityID, StatusFailed, "deleted", "deleted"); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("SetStatus deleted asset error = %v, want pgx.ErrNoRows", err)
	}
	if _, err := store.SetResult(ctx, deleted.ID, localIdentityID, result); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("SetResult deleted asset error = %v, want pgx.ErrNoRows", err)
	}
}
