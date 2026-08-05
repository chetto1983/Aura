package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/embeddings"
	"github.com/jackc/pgx/v5/pgxpool"
)

func buildRuntimeDocumentPipeline(cfg *config.Config, pool *pgxpool.Pool) (*documents.PipelineWorker, error) {
	if cfg == nil || pool == nil {
		return nil, fmt.Errorf("document pipeline requires configuration and PostgreSQL")
	}
	if strings.TrimSpace(cfg.Embed.Revision) == "" || strings.TrimSpace(cfg.Embed.Fingerprint) == "" {
		return nil, fmt.Errorf("document pipeline requires an immutable embedding revision and artifact fingerprint")
	}
	converter, err := documents.NewDoclingClient(cfg.Document)
	if err != nil {
		return nil, err
	}
	_, _, routeModel := cfg.EmbedRoute()
	model := strings.TrimSpace(routeModel)
	if model == "" {
		model = embeddings.DefaultModel
	}
	embedder := embeddingClient(cfg, documentHTTPClient(cfg))
	embedder.Model = model
	embedder.BatchSize = cfg.DocumentPipeline.EmbedBatchSize
	embedder.Timeout = time.Minute
	index, err := newRuntimeDocumentIndex(cfg, embedder, true)
	if err != nil {
		return nil, err
	}
	return &documents.PipelineWorker{
		Converter: converter, Embedder: embedder,
		Projector:   runtimeDocumentProjector{index: index},
		Persistence: documents.NewPostgresPipelineStore(pool),
		Config: documents.PipelineWorkerConfig{
			ProducerVersion:        cfg.Document.DoclingImage,
			ConversionProfile:      documents.DoclingStandardConversionProfile,
			ChunkTokenizer:         cfg.Document.ChunkTokenizer,
			ChunkMaxTokens:         cfg.Document.ChunkMaxTokens,
			EmbeddingModel:         model,
			EmbeddingVersion:       cfg.Embed.Revision,
			EmbeddingFingerprint:   cfg.Embed.Fingerprint,
			EmbeddingDimensions:    cfg.Embed.Dimensions,
			EmbeddingNormalization: documents.EmbeddingNormalizationMRL,
			ProjectionSchema:       arcadedb.DocumentProjectionSchemaVersion,
			MaxPassages:            cfg.DocumentPipeline.MaxPassagesPerVersion,
			MaxAttempts:            cfg.DocumentPipeline.MaxAttempts,
			LeaseDuration:          cfg.DocumentPipeline.LeaseDuration,
		},
	}, nil
}

type runtimeDocumentProjector struct {
	index *arcadedb.DocumentIndex
}

func (p runtimeDocumentProjector) ProjectCandidate(
	ctx context.Context,
	candidate documents.CandidateGeneration,
) (documents.ProjectionCommit, error) {
	if p.index == nil {
		return documents.ProjectionCommit{}, fmt.Errorf("document projector is not configured")
	}
	if len(candidate.Passages) == 0 || len(candidate.Passages) != len(candidate.Embeddings) {
		return documents.ProjectionCommit{}, fmt.Errorf("document projection requires one embedding per passage")
	}
	passages := make([]arcadedb.ProjectedPassage, len(candidate.Passages))
	for index, passage := range candidate.Passages {
		projected, err := projectDocumentPassage(passage, candidate.Embeddings[index])
		if err != nil {
			return documents.ProjectionCommit{}, err
		}
		passages[index] = projected
	}
	result, err := p.index.ProjectGeneration(ctx, arcadedb.ProjectionGeneration{
		IdentityID: candidate.IdentityID, DocumentID: candidate.DocumentID,
		SearchDocumentID: candidate.SearchDocumentID, VersionID: candidate.VersionID,
		VersionNumber: int64(candidate.VersionNumber), RawSHA256: candidate.RawSHA256,
		PipelineGeneration: strconv.FormatInt(candidate.PipelineGeneration, 10),
		Passages:           passages,
	})
	if err != nil {
		return documents.ProjectionCommit{}, err
	}
	if !result.Active {
		return documents.ProjectionCommit{}, fmt.Errorf("document projection is not active")
	}
	return documents.ProjectionCommit{
		Fingerprint:  result.Fingerprint,
		PassageCount: result.PassageCount,
		Created:      result.Created,
	}, nil
}

func (p runtimeDocumentProjector) DiscardCandidate(
	ctx context.Context,
	candidate documents.CandidateGeneration,
) error {
	if p.index == nil {
		return fmt.Errorf("document projector is not configured")
	}
	generation := strconv.FormatInt(candidate.PipelineGeneration, 10)
	if _, err := p.index.TombstoneGeneration(
		ctx, candidate.IdentityID, candidate.DocumentID, generation,
	); err != nil {
		return fmt.Errorf("discard document projection: %w", err)
	}
	if _, err := p.index.DeleteGeneration(
		ctx, candidate.IdentityID, candidate.DocumentID, generation,
	); err != nil {
		return fmt.Errorf("discard document projection: %w", err)
	}
	verification, err := p.index.DeleteGeneration(
		ctx, candidate.IdentityID, candidate.DocumentID, generation,
	)
	if err != nil {
		return fmt.Errorf("verify discarded document projection: %w", err)
	}
	if verification.Found {
		return fmt.Errorf("discarded document projection is still present")
	}
	return nil
}

func projectDocumentPassage(passage documents.Passage, embedding []float64) (arcadedb.ProjectedPassage, error) {
	locator := passage.Locator
	projected := arcadedb.ProjectedPassage{
		PassageID: passage.ID, Ordinal: int64(passage.Ordinal), Text: passage.ContextualizedText,
		NormalizedSHA256: passage.NormalizedTextSHA256, SelfRef: locator.SelfRef,
		HeadingPath: locator.HeadingPath, Captions: locator.Captions,
		PageNumber: int64Pointer(locator.PageNumber), CharacterSpan: characterSpan(locator.CharStart, locator.CharEnd),
		SheetName: locator.SheetName, TableName: locator.TableName,
		RowNumber: int64Pointer(locator.RowNumber), ColumnNumber: int64Pointer(locator.ColumnNumber),
		CellReference: locator.CellRef, Embedding: embedding,
	}
	if len(locator.BoundingBox) != 0 {
		if len(locator.BoundingBox) != 4 {
			return arcadedb.ProjectedPassage{}, fmt.Errorf("document passage %s has an invalid bounding box", passage.ID)
		}
		projected.BoundingBox = &arcadedb.BoundingBox{
			Left: locator.BoundingBox[0], Top: locator.BoundingBox[1],
			Right: locator.BoundingBox[2], Bottom: locator.BoundingBox[3],
		}
	}
	return projected, nil
}

func int64Pointer(value *int) *int64 {
	if value == nil {
		return nil
	}
	converted := int64(*value)
	return &converted
}

func characterSpan(start, end *int) *arcadedb.CharacterSpan {
	if start == nil || end == nil {
		return nil
	}
	return &arcadedb.CharacterSpan{Start: int64(*start), End: int64(*end)}
}
