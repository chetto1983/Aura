//go:build ignore

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/objectstore"
	"github.com/jackc/pgx/v5"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] == "help" || os.Args[1] == "--help" {
		fmt.Fprintln(os.Stdout, "document_pipeline_e2e_probe: preflight|setup|status|snapshot|arcade|conversation|verify-delete|cleanup")
		return
	}
	timeout := probeTimeout
	if os.Args[1] == "cleanup" {
		timeout = cleanupTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var err error
	switch os.Args[1] {
	case "preflight":
		err = runPreflight(ctx, os.Args[2:])
	case "setup":
		err = runSetup(ctx, os.Args[2:])
	case "status":
		err = runStatus(ctx, os.Args[2:])
	case "snapshot":
		err = runSnapshot(ctx, os.Args[2:])
	case "arcade":
		err = runArcade(ctx, os.Args[2:])
	case "conversation":
		err = runConversation(ctx, os.Args[2:])
	case "verify-delete":
		err = runVerifyDelete(ctx, os.Args[2:])
	case "cleanup":
		err = runCleanup(ctx, os.Args[2:])
	default:
		err = errors.New("unknown operation")
	}
	if err != nil {
		// Never serialize Error(); dependency errors can carry DSNs, signed URLs or tokens.
		fmt.Fprintf(os.Stderr, "document pipeline E2E probe failed: operation=%s class=%T\n", os.Args[1], err)
		os.Exit(1)
	}
}

func runPreflight(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("preflight", flag.ContinueOnError)
	model := fs.String("expected-model", "", "expected production agent model")
	embedModel := fs.String("expected-embed-model", "", "expected embedding model")
	embedVersion := fs.String("expected-embed-version", "", "expected embedding revision")
	docling := fs.String("expected-docling-producer", "", "expected Docling producer revision")
	if err := fs.Parse(args); err != nil {
		return err
	}
	result, err := preflight(ctx, *model, *embedModel, *embedVersion, *docling)
	if err != nil {
		return err
	}
	return outputJSON(result)
}

func runSetup(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	baseURL := fs.String("base-url", "", "production Aura origin")
	runID := fs.String("run-id", "", "disposable production run identifier")
	statePath := fs.String("state", "", "private state path")
	credentialPath := fs.String("credentials", "", "private credential path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *baseURL == "" || *runID == "" || *statePath == "" || *credentialPath == "" {
		return errors.New("setup arguments are incomplete")
	}
	return setup(ctx, *baseURL, *runID, *statePath, *credentialPath)
}

type jobStatus struct {
	AssetID         string `json:"asset_id"`
	Status          string `json:"status"`
	Stage           string `json:"stage"`
	AttemptCount    int    `json:"attempt_count"`
	LeaseGeneration int64  `json:"lease_generation"`
	LeaseRemaining  int64  `json:"lease_remaining_seconds"`
	ErrorCode       string `json:"error_code"`
}

func runStatus(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	identityID := fs.String("identity", "", "disposable production identity")
	assetID := fs.String("asset", "", "disposable production asset")
	if err := fs.Parse(args); err != nil {
		return err
	}
	r, err := openRuntime(ctx)
	if err != nil {
		return err
	}
	defer r.close()
	var result jobStatus
	err = db.WithIdentityTxRaw(ctx, r.pool, *identityID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
SELECT asset_id::text, status, stage, attempt_count, lease_generation,
       GREATEST(0, COALESCE(EXTRACT(EPOCH FROM locked_until - clock_timestamp())::bigint, 0)), error_code
FROM aura.ingestion_jobs
WHERE identity_id=$1::uuid AND asset_id=$2::uuid
ORDER BY created_at DESC LIMIT 1`, *identityID, *assetID).Scan(
			&result.AssetID, &result.Status, &result.Stage, &result.AttemptCount,
			&result.LeaseGeneration, &result.LeaseRemaining, &result.ErrorCode)
	})
	if err != nil {
		return err
	}
	return outputJSON(result)
}

func runSnapshot(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("snapshot", flag.ContinueOnError)
	identityID := fs.String("identity", "", "disposable production identity")
	assetID := fs.String("asset", "", "disposable production asset")
	expectedSHA := fs.String("sha256", "", "uploaded fixture SHA-256")
	embedModel := fs.String("expected-embed-model", "", "expected embedding model")
	embedVersion := fs.String("expected-embed-version", "", "expected embedding revision")
	doclingProducer := fs.String("expected-docling-producer", "", "expected Docling producer")
	statePath := fs.String("state", "", "private state path")
	label := fs.String("label", "", "identity label")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *identityID == "" || *assetID == "" || *expectedSHA == "" || *embedModel == "" ||
		*embedVersion == "" || *doclingProducer == "" || *statePath == "" || *label == "" {
		return errors.New("snapshot arguments are incomplete")
	}
	r, err := openRuntime(ctx)
	if err != nil {
		return err
	}
	defer r.close()
	snapshot, err := collectSnapshot(ctx, r, *identityID, *assetID)
	if err != nil {
		return err
	}
	if err := validateSnapshot(snapshot, *expectedSHA, *embedModel, *embedVersion, *doclingProducer, r.cfg.Embed.Dimensions); err != nil {
		return err
	}
	var state e2eState
	if err := readJSON(*statePath, &state); err != nil {
		return err
	}
	if state.Snapshots == nil {
		state.Snapshots = map[string]e2eSnapshot{}
	}
	state.Snapshots[*label] = snapshot
	if err := writePrivateJSON(*statePath, &state); err != nil {
		return err
	}
	return outputJSON(snapshot)
}

func collectSnapshot(ctx context.Context, r *runtimeDeps, identityID, assetID string) (e2eSnapshot, error) {
	result := e2eSnapshot{IdentityID: identityID, AssetID: assetID, Stages: map[string]string{}, StageProducers: map[string]string{}}
	err := db.WithIdentityTxRaw(ctx, r.pool, identityID, func(tx pgx.Tx) error {
		var documentGen, versionGen int64
		var jobError, assetError, documentError, versionError string
		if err := tx.QueryRow(ctx, `
SELECT a.catalog_document_id::text, a.document_version_id::text, a.status, a.pipeline_generation, a.content_hash, a.error_code,
       d.search_document_id, d.status, d.pipeline_generation, d.error_code,
       v.version_number, v.sha256, v.status, v.pipeline_generation, v.error_code,
       j.status, j.stage, j.attempt_count, j.lease_generation, j.error_code
FROM aura.assets a
JOIN aura.documents d ON d.id=a.catalog_document_id AND d.identity_id=a.identity_id
JOIN aura.document_versions v ON v.id=a.document_version_id AND v.identity_id=a.identity_id
JOIN LATERAL (
  SELECT status, stage, attempt_count, lease_generation, error_code
  FROM aura.ingestion_jobs
  WHERE identity_id=a.identity_id AND asset_id=a.id
  ORDER BY created_at DESC LIMIT 1
) j ON true
WHERE a.identity_id=$1::uuid AND a.id=$2::uuid`, identityID, assetID).Scan(
			&result.DocumentID, &result.VersionID, &result.AssetStatus, &result.PipelineGen, &result.AssetSHA256, &assetError,
			&result.SearchDocumentID, &result.DocumentStatus, &documentGen, &documentError,
			&result.VersionNumber, &result.OriginalSHA256, &result.VersionStatus, &versionGen, &versionError,
			&result.JobStatus, &result.JobStage, &result.JobAttempts, &result.JobLeaseGen, &jobError); err != nil {
			return err
		}
		if result.PipelineGen != documentGen || result.PipelineGen != versionGen {
			return errors.New("pipeline generations disagree")
		}
		if assetError != "" || documentError != "" || versionError != "" || jobError != "" {
			return errors.New("pipeline contains an error code")
		}
		rows, err := tx.Query(ctx, `
SELECT stage, status, producer_version, error_code, diagnostic_state::text
FROM aura.document_pipeline_stages
WHERE identity_id=$1::uuid AND document_id=$2::uuid AND version_id=$3::uuid AND pipeline_generation=$4`,
			identityID, result.DocumentID, result.VersionID, result.PipelineGen)
		if err != nil {
			return err
		}
		for rows.Next() {
			var stage, status, producer, errorCode, diagnostic string
			if err := rows.Scan(&stage, &status, &producer, &errorCode, &diagnostic); err != nil {
				rows.Close()
				return err
			}
			if errorCode != "" || forbiddenEvidence(producer+" "+diagnostic) {
				rows.Close()
				return errors.New("stage contains degraded, fallback, mock or skipped evidence")
			}
			result.Stages[stage] = status
			result.StageProducers[stage] = producer
		}
		if err := rows.Err(); err != nil {
			return err
		}
		return collectSnapshotDetails(ctx, tx, &result)
	})
	return result, err
}

func collectSnapshotDetails(ctx context.Context, tx pgx.Tx, result *e2eSnapshot) error {
	rows, err := tx.Query(ctx, `
SELECT ordinal, page_number, self_ref, char_start, char_end, sheet_name, cell_ref
FROM aura.document_chunks
WHERE identity_id=$1::uuid AND version_id=$2::uuid AND pipeline_generation=$3 AND active AND deleted_at IS NULL
ORDER BY ordinal`, result.IdentityID, result.VersionID, result.PipelineGen)
	if err != nil {
		return err
	}
	for rows.Next() {
		var ordinal int
		var page, charStart, charEnd *int
		var selfRef, sheet, cell *string
		if err := rows.Scan(&ordinal, &page, &selfRef, &charStart, &charEnd, &sheet, &cell); err != nil {
			rows.Close()
			return err
		}
		locator := documents.PassageLocator{PageNumber: page, CharStart: charStart, CharEnd: charEnd}
		if selfRef != nil {
			locator.SelfRef = *selfRef
		}
		if sheet != nil {
			locator.SheetName = *sheet
		}
		if cell != nil {
			locator.CellRef = *cell
		}
		token, err := documents.CitationToken(result.SearchDocumentID, result.VersionNumber, locator, ordinal)
		if err != nil {
			rows.Close()
			return err
		}
		result.CitationTokens = append(result.CitationTokens, token)
		result.ChunkCount++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, `
SELECT count(*), count(*) FILTER (WHERE projection_status='active'),
       count(DISTINCT embedding_model), min(embedding_model),
       count(DISTINCT embedding_version), min(embedding_version),
       count(DISTINCT embedding_dim), min(embedding_dim)
FROM aura.document_embeddings
WHERE identity_id=$1::uuid AND version_id=$2::uuid AND pipeline_generation=$3 AND active AND deleted_at IS NULL`,
		result.IdentityID, result.VersionID, result.PipelineGen).Scan(
		&result.EmbeddingCount, &result.ProjectionCount, &result.EmbedModels, &result.EmbedModel,
		&result.EmbedVersions, &result.EmbedVersion, &result.EmbedDimensions, &result.EmbedDimension); err != nil {
		return err
	}
	rows, err = tx.Query(ctx, `
SELECT bucket, object_key FROM aura.storage_objects
WHERE identity_id=$1::uuid AND (document_id=$2::uuid OR version_id=$3::uuid OR asset_id=$4::uuid)
  AND status='live' ORDER BY kind, object_key`, result.IdentityID, result.DocumentID, result.VersionID, result.AssetID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var ref objectRef
		if err := rows.Scan(&ref.Bucket, &ref.Key); err != nil {
			rows.Close()
			return err
		}
		result.Objects = append(result.Objects, ref)
	}
	rows.Close()
	rows, err = tx.Query(ctx, `
SELECT event_type FROM aura.ingestion_events
WHERE identity_id=$1::uuid AND (entity_id=$2::uuid OR entity_id=$3::uuid)
ORDER BY id`, result.IdentityID, result.DocumentID, result.VersionID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var event string
		if err := rows.Scan(&event); err != nil {
			rows.Close()
			return err
		}
		result.Events = append(result.Events, event)
	}
	return rows.Err()
}

func validateSnapshot(s e2eSnapshot, expectedSHA, embedModel, embedVersion, doclingProducer string, dimensions int) error {
	if s.AssetStatus != "searchable" && s.AssetStatus != "complete" {
		return errors.New("asset is not searchable")
	}
	if s.DocumentStatus != "ready" || s.VersionStatus != "ready" || s.JobStatus != "succeeded" {
		return errors.New("catalog lifecycle is not ready and succeeded")
	}
	if !strings.EqualFold(s.AssetSHA256, expectedSHA) || !strings.EqualFold(s.OriginalSHA256, expectedSHA) || s.PipelineGen <= 0 || s.JobLeaseGen <= 0 {
		return errors.New("hash or generation evidence is invalid")
	}
	for _, stage := range []string{"convert", "chunk", "embed", "project", "activate"} {
		if s.Stages[stage] != "succeeded" {
			return fmt.Errorf("required stage did not succeed")
		}
	}
	if s.StageProducers["convert"] != doclingProducer {
		return errors.New("Docling producer does not match pinned production revision")
	}
	if s.ChunkCount <= 0 || s.EmbeddingCount != s.ChunkCount || s.ProjectionCount != s.ChunkCount || len(s.CitationTokens) != s.ChunkCount {
		return errors.New("chunk, embedding, projection and citation cardinalities disagree")
	}
	if s.EmbedModels != 1 || s.EmbedModel != embedModel || s.EmbedVersions != 1 || s.EmbedVersion != embedVersion ||
		s.EmbedDimensions != 1 || s.EmbedDimension != dimensions || forbiddenEvidence(s.EmbedModel+" "+s.EmbedVersion) {
		return errors.New("embedding evidence does not match pinned production model")
	}
	if len(s.Objects) < 3 || !contains(s.Events, "candidate_activated") {
		return errors.New("storage artifacts or activation audit evidence are incomplete")
	}
	return nil
}

func forbiddenEvidence(value string) bool {
	value = strings.ToLower(value)
	for _, marker := range []string{"mock", "fake", "stub", "fallback", "degrad", "skip"} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func contains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func runArcade(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("arcade", flag.ContinueOnError)
	identityID := fs.String("identity", "", "disposable production identity")
	if err := fs.Parse(args); err != nil {
		return err
	}
	r, err := openRuntime(ctx)
	if err != nil {
		return err
	}
	defer r.close()
	database, err := arcadedb.DatabaseFor(*identityID)
	if err != nil {
		return err
	}
	exists, err := r.arcade.DatabaseExists(ctx, database)
	if err != nil || !exists {
		return errors.New("ArcadeDB tenant database is absent")
	}
	user := arcadedb.TenantUserFor(database)
	probe, err := arcadedb.New(arcadedb.Config{BaseURL: r.cfg.ArcadeDB.BaseURL, Database: database,
		User: user, Password: r.tenantPW.PasswordFor(database)})
	if err != nil {
		return err
	}
	accepted, err := probe.CredentialAccepted(ctx)
	if err != nil || !accepted {
		return errors.New("ArcadeDB tenant credential is not accepted")
	}
	return outputJSON(map[string]any{"identity_id": *identityID, "database": database, "exists": true, "credential_accepted": true})
}

func runConversation(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("conversation", flag.ContinueOnError)
	identityID := fs.String("identity", "", "disposable production identity")
	conversationID := fs.String("conversation", "", "disposable production conversation")
	expectedModel := fs.String("expected-model", "", "expected production model")
	if err := fs.Parse(args); err != nil {
		return err
	}
	r, err := openRuntime(ctx)
	if err != nil {
		return err
	}
	defer r.close()
	var model string
	err = db.WithIdentityTxRaw(ctx, r.pool, *identityID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT model FROM aura.conversations WHERE id=$1 AND identity_id=$2::uuid`, *conversationID, *identityID).Scan(&model)
	})
	if err != nil {
		return err
	}
	if model != *expectedModel || forbiddenEvidence(model) {
		return errors.New("conversation did not use the expected production model")
	}
	return outputJSON(map[string]string{"conversation_id": *conversationID, "identity_id": *identityID, "model": model})
}

func runVerifyDelete(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("verify-delete", flag.ContinueOnError)
	identityID := fs.String("identity", "", "disposable production identity")
	label := fs.String("label", "", "identity label")
	statePath := fs.String("state", "", "private state path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var state e2eState
	if err := readJSON(*statePath, &state); err != nil {
		return err
	}
	snapshot, ok := state.Snapshots[*label]
	if !ok || snapshot.IdentityID != *identityID {
		return errors.New("snapshot not found for delete verification")
	}
	r, err := openRuntime(ctx)
	if err != nil {
		return err
	}
	defer r.close()
	err = db.WithIdentityTxRaw(ctx, r.pool, *identityID, func(tx pgx.Tx) error {
		var status string
		var deleted bool
		if err := tx.QueryRow(ctx, `SELECT status, deleted_at IS NOT NULL FROM aura.documents WHERE id=$1::uuid AND identity_id=$2::uuid`,
			snapshot.DocumentID, *identityID).Scan(&status, &deleted); err != nil {
			return err
		}
		if status != "deleted" || !deleted {
			return errors.New("document tombstone is incomplete")
		}
		var activeChunks, activeEmbeddings int
		if err := tx.QueryRow(ctx, `
SELECT (SELECT count(*) FROM aura.document_chunks WHERE identity_id=$1::uuid AND document_id=$2::uuid AND active AND deleted_at IS NULL),
       (SELECT count(*) FROM aura.document_embeddings WHERE identity_id=$1::uuid AND document_id=$2::uuid AND active AND deleted_at IS NULL)`,
			*identityID, snapshot.DocumentID).Scan(&activeChunks, &activeEmbeddings); err != nil {
			return err
		}
		if activeChunks != 0 || activeEmbeddings != 0 {
			return errors.New("active retrieval rows survived delete")
		}
		for _, ref := range snapshot.Objects {
			var objectStatus string
			var verified bool
			if err := tx.QueryRow(ctx, `SELECT status, deletion_verified_at IS NOT NULL FROM aura.storage_objects
WHERE identity_id=$1::uuid AND bucket=$2 AND object_key=$3`, *identityID, ref.Bucket, ref.Key).Scan(&objectStatus, &verified); err != nil {
				return err
			}
			if objectStatus != "object_deleted" || !verified {
				return errors.New("storage ledger deletion is not verified")
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	creds, err := r.objects.Resolve(identityctx.WithIdentityID(ctx, *identityID))
	if err != nil {
		return err
	}
	store, err := objectstore.NewS3(ctx, objectstore.S3Config{Endpoint: r.cfg.ObjectStoreEndpoint, Region: r.cfg.ObjectStoreRegion,
		AccessKey: creds.AccessKey, SecretKey: creds.SecretKey, PathStyle: r.cfg.ObjectStorePathStyle})
	if err != nil {
		return err
	}
	for _, ref := range snapshot.Objects {
		if _, headErr := store.Head(ctx, objectstore.ObjectRef{Bucket: ref.Bucket, Key: ref.Key}); headErr == nil || !isNotFound(headErr) {
			return errors.New("Garage HEAD did not prove object absence")
		}
	}
	return outputJSON(map[string]any{"identity_id": *identityID, "document_id": snapshot.DocumentID,
		"status": "deleted", "head_absent": len(snapshot.Objects), "active_chunks": 0, "active_embeddings": 0})
}

func isNotFound(err error) bool {
	var responseErr *smithyhttp.ResponseError
	if errors.As(err, &responseErr) && responseErr.HTTPStatusCode() == 404 {
		return true
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		code := strings.ToLower(apiErr.ErrorCode())
		return code == "notfound" || code == "nosuchkey" || code == "nosuchobject"
	}
	return false
}

func runCleanup(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("cleanup", flag.ContinueOnError)
	statePath := fs.String("state", "", "private state path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return cleanup(ctx, *statePath)
}
