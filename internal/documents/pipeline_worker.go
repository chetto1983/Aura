package documents

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/embeddings"
	"github.com/chetto1983/aura/internal/objectstore"
)

// PipelineConverter is the strict asynchronous Docling boundary.
type PipelineConverter interface {
	ChunkFile(context.Context, DoclingFile) (DoclingResult, error)
}

// PipelineProjector writes one immutable candidate generation to ArcadeDB.
type PipelineProjector interface {
	ProjectCandidate(context.Context, CandidateGeneration) (ProjectionCommit, error)
	DiscardCandidate(context.Context, CandidateGeneration) error
}

// ProjectionCommit is the verified ArcadeDB result consumed by Postgres activation.
type ProjectionCommit struct {
	Fingerprint  string
	PassageCount int
	// Deliberately excluded from the cached preview artifact: a replayed run must not
	// inherit creation ownership, because compensation physically deletes the generation.
	Created bool `json:"-"`
}

// PipelineWorkerConfig fixes producer identities and bounded execution policy.
type PipelineWorkerConfig struct {
	ProducerVersion        string
	ConversionProfile      string
	ChunkTokenizer         string
	ChunkMaxTokens         int
	EmbeddingModel         string
	EmbeddingVersion       string
	EmbeddingFingerprint   string
	EmbeddingDimensions    int
	EmbeddingNormalization string
	ProjectionSchema       string
	MaxPassages            int
	MaxAttempts            int
	LeaseDuration          time.Duration
}

// PipelineRunRequest binds one claimed durable job to one immutable version.
type PipelineRunRequest struct {
	Record          DocumentVersionRecord
	AssetID         string
	JobID           string
	WorkerID        string
	LeaseGeneration int64
	Title           string
	FileName        string
	MIMEType        string
	Path            string
	ObjectBucket    string
	Objects         objectstore.Store
}

// PipelineRunResult is emitted only after the visibility transaction commits.
type PipelineRunResult struct {
	DocumentID         string
	SearchDocumentID   string
	VersionID          string
	VersionNumber      int
	PipelineGeneration int64
	PassageCount       int
}

// PipelineWorker executes the immutable document DAG for one already stored original.
type PipelineWorker struct {
	Converter   PipelineConverter
	Embedder    embeddings.Embedder
	Projector   PipelineProjector
	Persistence PipelinePersistence
	Config      PipelineWorkerConfig
	Clock       Clock
	RetryJitter func(time.Duration) time.Duration
}

// Run converts, chunks, embeds, projects, and atomically activates one candidate.
func (w *PipelineWorker) Run(ctx context.Context, req PipelineRunRequest) (PipelineRunResult, error) {
	if err := w.validate(req); err != nil {
		return PipelineRunResult{}, err
	}
	version := req.Record.Version
	document := req.Record.Document
	if err := w.Persistence.BindJobCandidate(ctx, JobCandidateBinding{
		IdentityID: document.IdentityID, JobID: req.JobID, AssetID: req.AssetID,
		DocumentID: document.ID, VersionID: version.ID, WorkerID: req.WorkerID,
		LeaseGeneration: req.LeaseGeneration, PipelineGeneration: version.PipelineGeneration,
		Stage: PipelineStageConvert,
	}); err != nil {
		return PipelineRunResult{}, err
	}

	convertFingerprint := pipelineFingerprint(
		"convert.v2", version.SHA256, w.Config.ProducerVersion, req.MIMEType,
		w.Config.ConversionProfile,
	)
	convertStage, convertRecord, convertCached, err := w.beginStage(
		ctx, req, PipelineStageConvert, convertFingerprint,
	)
	if err != nil {
		return PipelineRunResult{}, err
	}
	var converted DoclingResult
	err = w.runStage(ctx, convertStage, func(stageCtx context.Context) error {
		if convertCached {
			return loadPipelineArtifact(stageCtx, req.Objects, req.ObjectBucket, convertRecord, &converted)
		}
		file, openErr := os.Open(req.Path) //nolint:gosec // path is the worker-owned temp original.
		if openErr != nil {
			return openErr
		}
		var convertErr error
		converted, convertErr = w.Converter.ChunkFile(stageCtx, DoclingFile{
			Name: req.FileName, ContentType: req.MIMEType,
			Size: version.SizeBytes, Reader: file,
		})
		closeErr := file.Close()
		if convertErr != nil {
			return convertErr
		}
		if closeErr != nil {
			return closeErr
		}
		return w.completeJSONStage(stageCtx, req, convertStage, "converted_document", converted)
	})
	if err != nil {
		w.failStage(ctx, convertStage, err)
		return PipelineRunResult{}, err
	}

	chunkFingerprint := pipelineFingerprint(
		"chunk.v2", convertFingerprint, w.Config.ChunkTokenizer,
		strconv.Itoa(w.Config.ChunkMaxTokens), strconv.Itoa(w.Config.MaxPassages),
	)
	chunkStage, chunkRecord, chunkCached, err := w.beginStage(ctx, req, PipelineStageChunk, chunkFingerprint)
	if err != nil {
		return PipelineRunResult{}, err
	}
	var passages []Passage
	err = w.runStage(ctx, chunkStage, func(stageCtx context.Context) error {
		if chunkCached {
			return loadPipelineArtifact(stageCtx, req.Objects, req.ObjectBucket, chunkRecord, &passages)
		}
		var buildErr error
		passages, buildErr = BuildPassages(BuildPassagesRequest{
			IdentityID: document.IdentityID, DocumentID: document.ID,
			SearchDocumentID: document.SearchDocumentID, VersionID: version.ID,
			VersionNumber: version.VersionNumber, OriginalSHA256: version.SHA256,
			PipelineGeneration: version.PipelineGeneration,
			MaxPassages:        w.Config.MaxPassages, Result: converted,
		})
		if buildErr != nil {
			return buildErr
		}
		return w.completeJSONStage(stageCtx, req, chunkStage, "chunks", passages)
	})
	if err != nil {
		w.failStage(ctx, chunkStage, err)
		return PipelineRunResult{}, err
	}

	embedFingerprint := pipelineFingerprint(
		"embed.v2", chunkFingerprint, w.Config.EmbeddingModel,
		w.Config.EmbeddingVersion, w.Config.EmbeddingFingerprint,
		strconv.Itoa(w.Config.EmbeddingDimensions), w.Config.EmbeddingNormalization,
	)
	embedStage, embedRecord, embedCached, err := w.beginStage(ctx, req, PipelineStageEmbed, embedFingerprint)
	if err != nil {
		return PipelineRunResult{}, err
	}
	var vectors [][]float64
	err = w.runStage(ctx, embedStage, func(stageCtx context.Context) error {
		if embedCached {
			return loadPipelineArtifact(stageCtx, req.Objects, req.ObjectBucket, embedRecord, &vectors)
		}
		texts := make([]string, len(passages))
		for index := range passages {
			texts[index] = passages[index].ContextualizedText
		}
		var embedErr error
		vectors, embedErr = w.Embedder.Embed(stageCtx, embeddings.RetrievalDocuments(req.Title, texts))
		if embedErr != nil {
			return embedErr
		}
		if len(vectors) != len(passages) {
			return fmt.Errorf("document pipeline: embedding count %d does not match passages %d", len(vectors), len(passages))
		}
		return w.completeJSONStage(stageCtx, req, embedStage, "embedding_manifest", vectors)
	})
	if err != nil {
		w.failStage(ctx, embedStage, err)
		return PipelineRunResult{}, err
	}

	candidate := CandidateGeneration{
		IdentityID: document.IdentityID, DocumentID: document.ID,
		SearchDocumentID: document.SearchDocumentID, VersionID: version.ID,
		VersionNumber: version.VersionNumber, RawSHA256: version.SHA256,
		PipelineGeneration: version.PipelineGeneration, Passages: passages,
		Embeddings: vectors, EmbeddingModel: w.Config.EmbeddingModel,
		EmbeddingVersion:     w.Config.EmbeddingVersion,
		EmbeddingFingerprint: w.Config.EmbeddingFingerprint,
	}
	projectFingerprint := pipelineFingerprint("project.v2", embedFingerprint, w.Config.ProjectionSchema)
	projectStage, projectRecord, projectCached, err := w.beginStage(ctx, req, PipelineStageProject, projectFingerprint)
	if err != nil {
		return PipelineRunResult{}, err
	}
	var projection ProjectionCommit
	err = w.runStage(ctx, projectStage, func(stageCtx context.Context) error {
		if projectCached {
			if cacheErr := loadPipelineArtifact(stageCtx, req.Objects, req.ObjectBucket, projectRecord, &projection); cacheErr != nil {
				return cacheErr
			}
		} else {
			var projectErr error
			projection, projectErr = w.Projector.ProjectCandidate(stageCtx, candidate)
			if projectErr != nil {
				return projectErr
			}
		}
		if projection.PassageCount != len(passages) || strings.TrimSpace(projection.Fingerprint) == "" {
			return fmt.Errorf("document pipeline: projection postcondition failed")
		}
		candidate.ProjectionFingerprint = projection.Fingerprint
		candidate.ProjectionCount = projection.PassageCount
		if saveErr := w.Persistence.SaveCandidate(stageCtx, candidate); saveErr != nil {
			// DiscardCandidate is a physical DELETE VERTEX with no undo, and activation
			// re-checks PostgreSQL only, so a wrongly discarded generation publishes a
			// document whose passages no longer exist. Destroy only a generation this
			// attempt created while it still holds the stage fence: a lost lease means
			// another worker may already have replayed and activated this generation.
			if projectCached || !projection.Created || stageCtx.Err() != nil {
				return errors.Join(saveErr, fmt.Errorf("%w: generation %d",
					ErrPipelineProjectionUnreconciled, candidate.PipelineGeneration))
			}
			return errors.Join(saveErr,
				w.Projector.DiscardCandidate(context.WithoutCancel(stageCtx), candidate))
		}
		if projectCached {
			return nil
		}
		return w.completeJSONStage(stageCtx, req, projectStage, "preview", projection)
	})
	if err != nil {
		w.failStage(ctx, projectStage, err)
		return PipelineRunResult{}, err
	}

	activateFingerprint := pipelineFingerprint(
		"activate.v1", projectFingerprint, projection.Fingerprint,
	)
	activateStage, _, activateCached, err := w.beginStage(ctx, req, PipelineStageActivate, activateFingerprint)
	if err != nil {
		return PipelineRunResult{}, err
	}
	err = w.runStage(ctx, activateStage, func(stageCtx context.Context) error {
		if activateErr := w.Persistence.ActivateCandidate(stageCtx, ActivationRequest{
			IdentityID: document.IdentityID, DocumentID: document.ID, VersionID: version.ID,
			AssetID: req.AssetID, JobID: req.JobID, LockedBy: req.WorkerID,
			LeaseGeneration: req.LeaseGeneration, PipelineGeneration: version.PipelineGeneration,
			ExpectedPassageCount: len(passages), ExpectedEmbeddingCount: len(vectors),
			ExpectedProjectionCount: projection.PassageCount,
			EventMessage:            "document candidate activated",
		}); activateErr != nil {
			return activateErr
		}
		if activateCached {
			return nil
		}
		return w.Persistence.CompleteStage(context.WithoutCancel(stageCtx), StageCompletion{StageFence: activateStage})
	})
	if err != nil {
		w.failStage(ctx, activateStage, err)
		return PipelineRunResult{}, err
	}
	return PipelineRunResult{
		DocumentID: document.ID, SearchDocumentID: document.SearchDocumentID,
		VersionID: version.ID, VersionNumber: version.VersionNumber,
		PipelineGeneration: version.PipelineGeneration, PassageCount: len(passages),
	}, nil
}

func (w *PipelineWorker) validate(req PipelineRunRequest) error {
	if w == nil || w.Converter == nil || w.Embedder == nil || w.Projector == nil ||
		w.Persistence == nil || req.Objects == nil {
		return fmt.Errorf("document pipeline is not configured")
	}
	for name, value := range map[string]string{
		"identity": req.Record.Document.IdentityID, "document": req.Record.Document.ID,
		"search document": req.Record.Document.SearchDocumentID,
		"version":         req.Record.Version.ID, "asset": req.AssetID, "job": req.JobID,
		"worker": req.WorkerID, "path": req.Path, "bucket": req.ObjectBucket,
		"producer version":        w.Config.ProducerVersion,
		"conversion profile":      w.Config.ConversionProfile,
		"chunk tokenizer":         w.Config.ChunkTokenizer,
		"embedding model":         w.Config.EmbeddingModel,
		"embedding version":       w.Config.EmbeddingVersion,
		"embedding fingerprint":   w.Config.EmbeddingFingerprint,
		"embedding normalization": w.Config.EmbeddingNormalization,
		"projection schema":       w.Config.ProjectionSchema,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("document pipeline: %s is required", name)
		}
	}
	if req.Record.Version.DocumentID != req.Record.Document.ID ||
		req.Record.Version.IdentityID != req.Record.Document.IdentityID ||
		req.Record.Version.SearchDocumentID != req.Record.Document.SearchDocumentID {
		return fmt.Errorf("document pipeline: version ownership is incoherent")
	}
	if req.Record.Version.VersionNumber <= 0 || req.Record.Version.PipelineGeneration <= 0 ||
		req.LeaseGeneration <= 0 || w.Config.MaxPassages <= 0 ||
		w.Config.ChunkMaxTokens <= 0 || w.Config.EmbeddingDimensions <= 0 ||
		w.Config.MaxAttempts <= 0 || w.Config.LeaseDuration <= 0 {
		return fmt.Errorf("document pipeline: generation and execution bounds must be positive")
	}
	return nil
}

func (w *PipelineWorker) beginStage(
	ctx context.Context,
	req PipelineRunRequest,
	stage PipelineStage,
	fingerprint string,
) (StageFence, PipelineStageRecord, bool, error) {
	record, err := w.Persistence.UpsertStage(ctx, StageUpsert{
		IdentityID: req.Record.Document.IdentityID, DocumentID: req.Record.Document.ID,
		VersionID: req.Record.Version.ID, Stage: stage, InputFingerprint: fingerprint,
		ProducerVersion:    w.Config.ProducerVersion,
		PipelineGeneration: req.Record.Version.PipelineGeneration,
		MaxAttempts:        w.Config.MaxAttempts, NextAttemptAt: w.now(),
	})
	if err != nil {
		return StageFence{}, PipelineStageRecord{}, false, err
	}
	if record.Status == "succeeded" {
		return StageFence{}, record, true, nil
	}
	claimed, err := w.Persistence.ClaimStage(ctx, StageClaim{
		IdentityID: req.Record.Document.IdentityID, StageID: record.ID,
		Stage: stage, WorkerID: req.WorkerID, LeaseDuration: w.Config.LeaseDuration,
	})
	if err != nil {
		return StageFence{}, PipelineStageRecord{}, false, err
	}
	return StageFence{
		IdentityID: claimed.IdentityID, StageID: claimed.ID, WorkerID: req.WorkerID,
		AttemptCount: claimed.AttemptCount, LeaseGeneration: claimed.LeaseGeneration,
		LeaseDuration: w.Config.LeaseDuration,
	}, claimed, false, nil
}

func (w *PipelineWorker) completeJSONStage(
	ctx context.Context,
	req PipelineRunRequest,
	fence StageFence,
	kind string,
	value any,
) error {
	body, err := json.Marshal(value)
	if err != nil {
		w.failStage(ctx, fence, err)
		return err
	}
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	key := derivedArtifactKey(req.Record, kind, digest)
	if _, err := req.Objects.Put(ctx, objectstore.ObjectRef{Bucket: req.ObjectBucket, Key: key},
		bytes.NewReader(body), objectstore.PutOptions{MIMEType: "application/json", Size: int64(len(body))}); err != nil {
		w.failStage(ctx, fence, err)
		return err
	}
	storageID, err := w.Persistence.RecordDerivedArtifact(ctx, DerivedArtifactRequest{
		IdentityID: req.Record.Document.IdentityID, DocumentID: req.Record.Document.ID,
		VersionID: req.Record.Version.ID, PipelineGeneration: req.Record.Version.PipelineGeneration,
		Bucket: req.ObjectBucket, ObjectKey: key, Kind: kind, SHA256: digest,
		SizeBytes: int64(len(body)), ContentType: "application/json", RetentionClass: "derived",
	})
	if err != nil {
		// Bounded rather than bare WithoutCancel: the ownership probe must survive the
		// cancelled request that brought us here, but it must not outlive the stage.
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		cleanupErr := discardUnledgeredArtifact(cleanupCtx, w.Persistence, req.Objects,
			ArtifactLedgerQuery{
				IdentityID: req.Record.Document.IdentityID,
				Bucket:     req.ObjectBucket,
				ObjectKey:  key,
			})
		combined := errors.Join(err, cleanupErr)
		w.failStage(ctx, fence, combined)
		return combined
	}
	return w.Persistence.CompleteStage(ctx, StageCompletion{
		StageFence: fence, ArtifactStorageObjectID: storageID, ArtifactObjectKey: key,
		ArtifactSHA256: digest, ArtifactSizeBytes: int64(len(body)),
	})
}

func (w *PipelineWorker) runStage(
	ctx context.Context,
	fence StageFence,
	work func(context.Context) error,
) error {
	if fence.StageID == "" {
		return work(ctx)
	}
	stageCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	heartbeatResult := make(chan error, 1)
	go func() {
		heartbeatResult <- w.maintainStageLease(stageCtx, cancel, fence)
	}()
	workErr := work(stageCtx)
	cancel()
	<-heartbeatResult
	if workErr != nil {
		return workErr
	}
	// A successful stage callback includes its fenced CompleteStage write. It is
	// therefore authoritative even if a concurrent heartbeat observed that the
	// stage had just transitioned out of running.
	return nil
}

func (w *PipelineWorker) maintainStageLease(
	ctx context.Context,
	cancel context.CancelFunc,
	fence StageFence,
) error {
	interval := fence.LeaseDuration / 3
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := w.Persistence.HeartbeatStage(ctx, fence); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				cancel()
				return err
			}
		}
	}
}

func (w *PipelineWorker) failStage(ctx context.Context, fence StageFence, cause error) {
	if fence.StageID == "" || cause == nil {
		return
	}
	retryable := true
	errorClass := "transient"
	var doclingError *DoclingError
	if errors.As(cause, &doclingError) {
		retryable = doclingError.Retryable
		errorClass = string(doclingError.Class)
	}
	retryCap := exponentialRetryCap(time.Minute, fence.AttemptCount)
	retryDelay := fullJitter(retryCap)
	if w.RetryJitter != nil {
		retryDelay = w.RetryJitter(retryCap)
	}
	_ = w.Persistence.FailStage(context.WithoutCancel(ctx), StageFailure{
		StageFence: fence, Retryable: retryable, ErrorClass: errorClass,
		ErrorCode: "stage_failed", RedactedMessage: "document pipeline stage failed",
		NextAttemptAt: w.now().Add(retryDelay),
	})
}

func (w *PipelineWorker) now() time.Time {
	if w.Clock != nil {
		return w.Clock().UTC()
	}
	return time.Now().UTC()
}

func pipelineFingerprint(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(part))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func derivedArtifactKey(record DocumentVersionRecord, kind, digest string) string {
	return "identity/" + record.Document.IdentityID + "/document/" + record.Document.ID +
		"/version/" + record.Version.ID + "/generation/" +
		strconv.FormatInt(record.Version.PipelineGeneration, 10) + "/" + kind + "/" + digest + ".json"
}
