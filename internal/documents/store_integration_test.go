//go:build db_integration

package documents

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/jackc/pgx/v5/pgxpool"
)

func documentEnvOrSkip(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("document store integration requires %s under CI", key)
		}
		t.Skipf("document store integration requires %s", key)
	}
	return v
}

func migratedDocumentPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pwd := documentEnvOrSkip(t, "POSTGRES_PASSWORD")
	migrateURL := documentEnvOrSkip(t, "AURA_DB_MIGRATE_URL")
	appURL := documentEnvOrSkip(t, "AURA_DB_URL")
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

func TestPostgresJobStoreRoundTrip(t *testing.T) {
	pool := migratedDocumentPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	sourceID := fmt.Sprintf("doc-store-test-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM aura.document_ingest_jobs WHERE source_id = $1", sourceID)
	})

	store := NewPostgresJobStore(pool)
	documentID := DocumentID("hash", sourceID)
	created, err := store.Create(ctx, CreateJobParams{
		SourceID:     sourceID,
		SourceKind:   "local",
		DocumentID:   documentID,
		ContentHash:  "hash",
		OriginalPath: "/tmp/manual.pdf",
		FileName:     "manual.pdf",
		MIMEType:     "application/pdf",
		SizeBytes:    42,
		Status:       JobAccepted,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.Status != JobAccepted {
		t.Fatalf("created job = %#v", created)
	}

	got, err := store.Get(ctx, created.ID)
	if err != nil || got.DocumentID != documentID {
		t.Fatalf("Get = (%#v, %v)", got, err)
	}
	byDoc, err := store.GetByDocumentID(ctx, documentID)
	if err != nil || byDoc.ID != created.ID {
		t.Fatalf("GetByDocumentID = (%#v, %v)", byDoc, err)
	}
	extracting, err := store.UpdateStatus(ctx, created.ID, JobExtracting, "")
	if err != nil || extracting.Status != JobExtracting {
		t.Fatalf("UpdateStatus extracting = (%#v, %v)", extracting, err)
	}
	failed, err := store.UpdateStatus(ctx, created.ID, JobFailed, "transient extractor failure")
	if err != nil || failed.Error == "" {
		t.Fatalf("UpdateStatus failed = (%#v, %v)", failed, err)
	}
	searchable, err := store.UpdateProgress(ctx, created.ID, JobSearchable, 3, 0)
	if err != nil || searchable.Status != JobSearchable || searchable.SparseChunks != 3 ||
		searchable.Error != "" || searchable.SearchableAt.IsZero() {
		t.Fatalf("UpdateProgress searchable = (%#v, %v)", searchable, err)
	}
	complete, err := store.UpdateProgress(ctx, created.ID, JobComplete, 3, 3)
	if err != nil || complete.Status != JobComplete || complete.EmbeddedChunks != 3 || complete.CompletedAt.IsZero() {
		t.Fatalf("UpdateProgress complete = (%#v, %v)", complete, err)
	}
	recent, err := store.ListRecent(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, job := range recent {
		if job.ID == created.ID {
			return
		}
	}
	t.Fatalf("created job %s missing from ListRecent", created.ID)
}

func TestPostgresJobStoreRejectsInvalidIDs(t *testing.T) {
	store := &PostgresJobStore{}
	if _, err := store.Get(t.Context(), "bad"); err == nil {
		t.Fatal("Get accepted invalid uuid")
	}
	if _, err := store.UpdateStatus(t.Context(), "bad", JobFailed, "boom"); err == nil {
		t.Fatal("UpdateStatus accepted invalid uuid")
	}
	if _, err := store.UpdateProgress(t.Context(), "bad", JobSearchable, 1, 0); err == nil {
		t.Fatal("UpdateProgress accepted invalid uuid")
	}
}
