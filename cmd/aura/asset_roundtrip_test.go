//go:build garage_integration

// The document round trip, through the composition root the daemon uses.
//
// Written because a day of measuring ran entirely through `IngestPath` -- the
// CLI seam -- which extracts, chunks, and drops the file. That path is debug
// only, and reading it without reading its caller produced two wrong diagnoses
// in one evening: first "Telegram destroys the original", then "nothing uses the
// catalog". Both were artefacts of testing the wrong door.
//
// So this uses the real one: buildAssetService, the same function serve.go:294
// calls, over the live Postgres and the live Garage. It walks the whole loop --
// presign, upload, finalize, process -- and then asks the only question that
// matters for handing a spreadsheet to pandas: after all that, can the ORIGINAL
// bytes be read back?
//
//	go test -tags garage_integration -run AssetRoundTrip -v ./cmd/aura/
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/jackc/pgx/v5/pgxpool"
)

func roundTripEnv(t *testing.T) (*config.Config, *pgxpool.Pool) {
	t.Helper()
	if os.Getenv("AURA_DB_URL") == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("AURA_DB_URL must be set in CI: a skipped integration tier is a falsely-green job")
		}
		t.Skip("AURA_DB_URL not set")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("config: %v", err)
	}
	pool, err := db.Open(context.Background(), &cfg.DB)
	if err != nil {
		t.Skipf("postgres: %v", err)
	}
	t.Cleanup(pool.Close)
	return cfg, pool
}

func TestAssetRoundTripKeepsTheOriginal(t *testing.T) {
	cfg, pool := roundTripEnv(t)
	ctx := context.Background()

	objectStore, err := buildObjectStore(ctx, cfg)
	if err != nil {
		t.Skipf("object store: %v", err)
	}
	service := buildAssetService(cfg, pool, objectStore)
	if service == nil {
		t.Fatal("buildAssetService returned nil: the daemon would fall back to the " +
			"path that deletes the file")
	}

	path := filepath.Join(envOrDefault("AURA_BASELINE_CORPUS", "/mnt/d/tmp/baseline-corpus"),
		"Clienti.xlsx")
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("fixture %s: %v", path, err)
	}
	sum := sha256.Sum256(payload)
	wantHash := hex.EncodeToString(sum[:])
	t.Logf("source: %s, %d bytes, sha256 %s…", filepath.Base(path), len(payload), wantHash[:16])

	// 1. presign -- the daemon mints a URL and a pending asset row.
	presigned, err := service.Presign(ctx, assets.PresignRequest{
		IdentityID:        localSeededIdentityID,
		SourceKind:        assets.SourceWeb,
		Scope:             assets.ScopeLibrary,
		FileName:          "Clienti.xlsx",
		MIMEType:          "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		DeclaredSizeBytes: int64(len(payload)),
		ModalityHint:      assets.ModalityDocument,
	})
	if err != nil {
		t.Fatalf("presign: %v", err)
	}
	t.Logf("1. presign  -> asset %s, bucket %s, key %s",
		presigned.Asset.ID, presigned.Asset.ObjectBucket, presigned.Asset.ObjectKey)

	// 2. upload -- straight to Garage, never through the daemon. This is the step
	//    that looks convoluted and is not: a 30 MB spreadsheet has no business
	//    being streamed through the API process.
	if err := putPresigned(ctx, presigned, payload); err != nil {
		t.Fatalf("upload: %v", err)
	}
	t.Logf("2. upload   -> %d bytes PUT directly to the object store", len(payload))

	// 3. finalize -- hashes, sniffs, validates limits, and queues processing.
	asset, err := service.Finalize(ctx, localSeededIdentityID, presigned.Asset.ID)
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	t.Logf("3. finalize -> status %s, modality %s, %d bytes, sha256 %.16s…",
		asset.Status, asset.Modality, asset.SizeBytes, asset.ContentHash)
	if asset.ContentHash != "" && asset.ContentHash != wantHash {
		t.Fatalf("sha256 mismatch: stored %s, source %s", asset.ContentHash, wantHash)
	}

	// 4. processing -- DocumentProcessor pulls it back down, indexes it, and
	//    records the document_versions row. It runs on a queue, so it is waited
	//    for rather than assumed.
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		current, err := service.GetForIdentity(ctx, asset.ID, localSeededIdentityID)
		if err != nil {
			t.Fatalf("poll asset: %v", err)
		}
		if current.Status == assets.StatusComplete ||
			current.Status == assets.StatusSearchable || current.Status == assets.StatusFailed {
			asset = current
			break
		}
		time.Sleep(2 * time.Second)
	}
	t.Logf("4. process  -> status %s, document_id %q", asset.Status, asset.DocumentID)

	// 5. the question the whole day was really about: is the ORIGINAL still there?
	reader, reopened, err := service.OpenForIdentity(ctx, asset.ID, localSeededIdentityID)
	if err != nil {
		t.Fatalf("OpenForIdentity: the original is not retrievable: %v", err)
	}
	defer func() { _ = reader.Close() }()
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	back := sha256.Sum256(got)
	t.Logf("5. reopen   -> %d bytes, sha256 %.16s…, file %q",
		len(got), hex.EncodeToString(back[:]), reopened.FileName)
	if !bytes.Equal(got, payload) {
		t.Fatalf("the bytes came back different: %d in, %d out", len(payload), len(got))
	}
	t.Log("the original survives the pipeline byte for byte: a sandbox can be handed the real file")
}

func putPresigned(ctx context.Context, presigned assets.PresignResponse, payload []byte) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPut,
		presigned.Upload.URL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	for key, value := range presigned.Upload.RequiredHeaders {
		request.Header.Set(key, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return &uploadError{status: response.StatusCode, body: strings.TrimSpace(string(body))}
	}
	return nil
}

type uploadError struct {
	status int
	body   string
}

func (e *uploadError) Error() string {
	return "presigned PUT returned " + http.StatusText(e.status) + ": " + e.body
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
