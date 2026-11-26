//go:build db_integration && garage_integration

// The document round trip, through the composition root the daemon uses.
//
// Written because a day of measuring ran entirely through `IngestPath` -- the
// CLI seam -- which extracts, chunks, and drops the file. That path is debug
// only, and reading it without reading its caller produced two wrong diagnoses
// in one evening: first "Telegram destroys the original", then "nothing uses the
// catalog". Both were artefacts of testing the wrong door.
//
// So this uses the real one: buildAssetService, the function `aura serve` builds its
// asset service with, over the live Postgres and the live Garage. It walks presign, direct
// upload and finalize, and then asks the only question that matters for handing a
// spreadsheet to pandas: can the ORIGINAL bytes be read back? Processing runs in
// the ingest sidecar from the queued job, outside this tier.
//
//	go test -tags 'db_integration garage_integration' -run AssetRoundTrip -v ./cmd/aura/
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/testenv"
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
	cfg, err := config.LoadServe()
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	pool, err := db.Open(context.Background(), &cfg.DB)
	if err != nil {
		t.Fatalf("postgres: %v", err)
	}
	t.Cleanup(pool.Close)
	return cfg, pool
}

// syntheticSpreadsheet is 1 MiB of random bytes. Nothing in this tier reads the content:
// Finalize hashes it and queues it for the ingest sidecar, so only the bytes' identity matters.
func syntheticSpreadsheet(t *testing.T) []byte {
	t.Helper()
	payload := make([]byte, 1<<20)
	if _, err := rand.Read(payload); err != nil {
		t.Fatalf("synthetic payload: %v", err)
	}
	return payload
}

// roundTripOwner is a real non-shared identity, as a browser upload's owner is. The legacy
// `local` row is no help: aura serve retires it at boot, after which an asset cannot name it.
// Its Garage bucket and key are minted on the first presign (mintingResolver) and torn down
// by the production provisioning port, before the identity row goes.
func roundTripOwner(t *testing.T, cfg *config.Config, pool *pgxpool.Pool) string {
	t.Helper()
	testenv.Require(t, "AURA_GARAGE_ADMIN_ENDPOINT")
	testenv.Require(t, "AURA_GARAGE_ADMIN_TOKEN")
	objects, _, _ := buildProvisioningPorts(&chatEnv{cfg: cfg, pool: pool})
	if objects == nil {
		t.Fatal("buildProvisioningPorts composed no object-store provisioner")
	}
	owner := objStoreProvSeedIdentity(t, pool)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := objects.DeprovisionObjectStore(ctx, owner); err != nil {
			t.Errorf("deprovision the round trip's bucket: %v", err)
		}
	})
	return owner
}

func TestAssetRoundTripKeepsTheOriginal(t *testing.T) {
	cfg, pool := roundTripEnv(t)
	owner := roundTripOwner(t, cfg, pool)
	ctx := context.Background()

	objectStore, err := buildObjectStore(ctx, cfg)
	if err != nil {
		t.Fatalf("object store: %v", err)
	}
	service := buildAssetService(cfg, pool, objectStore, nil)
	if service == nil {
		t.Fatal("buildAssetService returned nil: the daemon would fall back to the " +
			"path that deletes the file")
	}

	payload := syntheticSpreadsheet(t)
	sum := sha256.Sum256(payload)
	wantHash := hex.EncodeToString(sum[:])

	// 1. presign -- the daemon mints a URL and a pending asset row.
	presigned, err := service.Presign(ctx, assets.PresignRequest{
		IdentityID:        owner,
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

	// 2. upload -- straight to Garage, never through the daemon: a 30 MB spreadsheet has
	//    no business being streamed through the API process.
	if err := putPresigned(ctx, presigned, payload); err != nil {
		t.Fatalf("upload: %v", err)
	}

	// 3. finalize -- hashes, validates limits, and queues the ingestion job. Processing is
	//    the ingest sidecar's (the ingestion reconciliation tier); nothing here consumes it.
	asset, err := service.Finalize(ctx, owner, presigned.Asset.ID)
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if asset.Status != assets.StatusAccepted {
		t.Fatalf("finalize status = %s, want %s (hashed and queued)", asset.Status, assets.StatusAccepted)
	}
	if asset.ContentHash != wantHash {
		t.Fatalf("sha256 stored %q, source %s", asset.ContentHash, wantHash)
	}

	// 4. the question this test exists for: is the ORIGINAL still there, byte for byte?
	reader, _, err := service.OpenForIdentity(ctx, asset.ID, owner)
	if err != nil {
		t.Fatalf("OpenForIdentity: the original is not retrievable: %v", err)
	}
	defer func() { _ = reader.Close() }()
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("the bytes came back different: %d in, %d out", len(payload), len(got))
	}
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
