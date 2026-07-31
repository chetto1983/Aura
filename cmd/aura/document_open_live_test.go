//go:build garage_integration

// document_open against the live catalog and the live object store.
//
// The unit tests prove the wiring with fakes. This proves the only thing that
// actually matters at the end of the chain: that the bytes handed to the agent
// are the user's original file, resolved from the one id a retrieval hit
// carries, and that another identity cannot get them.
//
//	go test -tags garage_integration -run DocumentOpenLive -v ./cmd/aura/
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// liveDocument finds a catalogued document that still has its source asset. It
// reads the catalog directly rather than taking a fixture id, so the test tracks
// whatever the deployment actually holds.
func liveDocument(t *testing.T, pool *pgxpool.Pool) (searchID, title, sha string, size int64) {
	t.Helper()
	err := pool.QueryRow(context.Background(), `
SELECT d.metadata->>'search_document_id', d.title, v.sha256, v.size_bytes
FROM aura.documents d
JOIN aura.document_versions v ON v.document_id = d.id
WHERE d.deleted_at IS NULL
  AND d.metadata->>'search_document_id' IS NOT NULL
  AND v.asset_id IS NOT NULL
ORDER BY d.created_at DESC
LIMIT 1`).Scan(&searchID, &title, &sha, &size)
	if err != nil {
		t.Skipf("no catalogued document with a source asset: %v", err)
	}
	return searchID, title, sha, size
}

func TestDocumentOpenLiveReturnsTheOriginalBytes(t *testing.T) {
	cfg, pool := roundTripEnv(t)
	ctx := context.Background()
	searchID, title, wantSHA, wantSize := liveDocument(t, pool)
	t.Logf("document %q  search id %s  %d bytes  sha256 %.16s…", title, searchID, wantSize, wantSHA)

	opener := newRuntimeDocumentOpener(cfg, pool)
	body, meta, err := opener.OpenDocument(ctx, localSeededIdentityID, searchID)
	if err != nil {
		t.Fatalf("OpenDocument(%s): %v", searchID, err)
	}
	defer func() { _ = body.Close() }()

	sum := sha256.New()
	read, err := io.Copy(sum, body)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	got := hex.EncodeToString(sum.Sum(nil))
	t.Logf("streamed %d bytes, sha256 %.16s…, name %q, mime %q", read, got, meta.FileName, meta.MIMEType)

	// The whole point of the tool: what lands in /workspace is the file the user
	// uploaded, not a re-rendering of it.
	if got != wantSHA {
		t.Fatalf("sha256 mismatch: streamed %s, catalog %s", got, wantSHA)
	}
	if read != wantSize {
		t.Fatalf("size mismatch: streamed %d, catalog %d", read, wantSize)
	}
	if meta.FileName == "" || strings.ContainsAny(meta.FileName, `/\`) {
		t.Fatalf("file name %q is not a usable base name", meta.FileName)
	}
	if meta.CatalogID == "" || meta.DocumentID != searchID {
		t.Fatalf("ids = %#v", meta)
	}
}

func TestDocumentOpenLiveRefusesAnotherIdentity(t *testing.T) {
	cfg, pool := roundTripEnv(t)
	searchID, _, _, _ := liveDocument(t, pool)

	stranger := uuid.NewString()
	body, _, err := newRuntimeDocumentOpener(cfg, pool).
		OpenDocument(context.Background(), stranger, searchID)
	if err == nil {
		_ = body.Close()
		t.Fatalf("identity %s received another identity's document", stranger)
	}
	t.Logf("refused, as it must: %v", err)
}

func TestDocumentOpenLiveRejectsUnknownDocument(t *testing.T) {
	cfg, pool := roundTripEnv(t)
	for _, id := range []string{"doc_" + strings.Repeat("f", 32), uuid.NewString()} {
		body, _, err := newRuntimeDocumentOpener(cfg, pool).
			OpenDocument(context.Background(), localSeededIdentityID, id)
		if err == nil {
			_ = body.Close()
			t.Fatalf("unknown document %s was opened", id)
		}
	}
}
