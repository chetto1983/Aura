package documents

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresPipelineStore is the PipelinePersistence implementation over the aura.*
// schema. Every method body runs inside a per-identity transaction, so row scoping
// is imposed by the session identity rather than by a WHERE clause a caller can
// forget to write.
type PostgresPipelineStore struct {
	pool *pgxpool.Pool
}

// NewPostgresPipelineStore tolerates a nil pool so wiring can construct the store
// before the pool exists; every method then reports "not configured" instead of
// panicking on first use.
func NewPostgresPipelineStore(pool *pgxpool.Pool) *PostgresPipelineStore {
	return &PostgresPipelineStore{pool: pool}
}

// ReserveCandidateVersion derives the version id from the document id and the raw
// SHA-256, so re-uploading identical bytes resolves to the version already on
// record (reported by CandidateVersion.Replayed) instead of minting a second one.
func (s *PostgresPipelineStore) ReserveCandidateVersion(
	ctx context.Context,
	req CandidateVersionRequest,
) (CandidateVersion, error) {
	params, err := candidateVersionParams(req)
	if err != nil {
		return CandidateVersion{}, err
	}
	var row sqlc.ReservePipelineCandidateVersionRow
	err = s.withIdentity(ctx, req.IdentityID, func(q *sqlc.Queries) error {
		var queryErr error
		row, queryErr = q.ReservePipelineCandidateVersion(ctx, params)
		return queryErr
	})
	if err != nil {
		return CandidateVersion{}, candidateRejectedError("reserve version", err)
	}
	return candidateVersionFromSQL(row), nil
}

// RecordDerivedArtifact returns the storage object id of a derived artifact. The
// (bucket, object key) pair is unique schema-wide, so a colliding insert only
// replays when the existing row is the identical live artifact of the same version
// and generation; any other owner yields ErrPipelineCandidateRejected rather than
// silently rebinding someone else's object.
func (s *PostgresPipelineStore) RecordDerivedArtifact(
	ctx context.Context,
	req DerivedArtifactRequest,
) (string, error) {
	params, err := derivedArtifactParams(req)
	if err != nil {
		return "", err
	}
	var id string
	err = s.withIdentity(ctx, req.IdentityID, func(q *sqlc.Queries) error {
		rowID, queryErr := q.RecordPipelineDerivedArtifact(ctx, params)
		id = uuidString(rowID)
		return queryErr
	})
	if err != nil {
		return "", candidateRejectedError("record derived artifact", err)
	}
	return id, nil
}

// CountArtifactLedgerOwners reports how many live ledger rows in this identity still
// reference an object key. It deliberately takes no row lock and runs in its own
// transaction: it is a cleanup precondition, not part of the write it follows, and
// locking here would stall behind any in-flight delete without proving anything more.
func (s *PostgresPipelineStore) CountArtifactLedgerOwners(
	ctx context.Context,
	query ArtifactLedgerQuery,
) (int, error) {
	identityID, err := pgUUID("pipeline identity id", strings.TrimSpace(query.IdentityID))
	if err != nil {
		return 0, err
	}
	bucket, objectKey := strings.TrimSpace(query.Bucket), strings.TrimSpace(query.ObjectKey)
	if bucket == "" || objectKey == "" {
		return 0, fmt.Errorf("artifact bucket and object key are required")
	}
	var owners int64
	err = s.withIdentity(ctx, query.IdentityID, func(q *sqlc.Queries) error {
		count, queryErr := q.CountPipelineArtifactLedgerOwners(ctx, sqlc.CountPipelineArtifactLedgerOwnersParams{
			IdentityID: identityID, Bucket: bucket, ObjectKey: objectKey,
		})
		owners = count
		return queryErr
	})
	if err != nil {
		// Not candidateRejectedError: an unreadable ledger is an unknown answer, never
		// a proof that the artifact is unowned.
		return 0, fmt.Errorf("count artifact ledger owners: %w", err)
	}
	return int(owners), nil
}

// SaveCandidate re-reads the version inside the writing transaction and abandons
// the whole batch when it has drifted, so passages and their embeddings can never
// land half-written under a generation that moved on beneath them.
func (s *PostgresPipelineStore) SaveCandidate(ctx context.Context, candidate CandidateGeneration) error {
	prepared, err := prepareCandidate(candidate)
	if err != nil {
		return err
	}
	err = s.withIdentity(ctx, candidate.IdentityID, func(q *sqlc.Queries) error {
		version, queryErr := q.GetPipelineCandidateVersion(ctx, prepared.version)
		if queryErr != nil {
			return queryErr
		}
		if !candidateMatchesVersion(candidate, version) {
			return ErrPipelineCandidateRejected
		}
		for _, params := range prepared.chunks {
			if _, queryErr = q.UpsertPipelineCandidateChunk(ctx, params); queryErr != nil {
				return queryErr
			}
		}
		for _, params := range prepared.embeddings {
			if _, queryErr = q.UpsertPipelineCandidateEmbedding(ctx, params); queryErr != nil {
				return queryErr
			}
		}
		return nil
	})
	if err != nil {
		return candidateRejectedError("save candidate", err)
	}
	return nil
}

// ListPassages reads the generation named by the key, deliberately not the
// document's active one: it exists to verify a candidate before activation, when
// none of its passages is active yet.
func (s *PostgresPipelineStore) ListPassages(
	ctx context.Context,
	key CandidateKey,
) ([]Passage, error) {
	params, err := candidateKeyParams(key)
	if err != nil {
		return nil, err
	}
	var rows []sqlc.AuraDocumentChunks
	err = s.withIdentity(ctx, key.IdentityID, func(q *sqlc.Queries) error {
		var queryErr error
		rows, queryErr = q.ListPipelineCandidateChunks(ctx, params)
		return queryErr
	})
	if err != nil {
		return nil, fmt.Errorf("list pipeline passages: %w", err)
	}
	out := make([]Passage, 0, len(rows))
	for _, row := range rows {
		passage, conversionErr := pipelinePassageFromSQL(row)
		if conversionErr != nil {
			return nil, conversionErr
		}
		out = append(out, passage)
	}
	return out, nil
}

// ListEmbeddings returns embedding ledger rows, never vectors: aura.document_embeddings
// stores model, dimension and projection state plus a (namespace, id) pointer into
// the vector index, so the vectors themselves are only reachable through it.
func (s *PostgresPipelineStore) ListEmbeddings(
	ctx context.Context,
	key CandidateKey,
) ([]CandidateEmbedding, error) {
	params, err := candidateEmbeddingKeyParams(key)
	if err != nil {
		return nil, err
	}
	var rows []sqlc.AuraDocumentEmbeddings
	err = s.withIdentity(ctx, key.IdentityID, func(q *sqlc.Queries) error {
		var queryErr error
		rows, queryErr = q.ListPipelineCandidateEmbeddings(ctx, params)
		return queryErr
	})
	if err != nil {
		return nil, fmt.Errorf("list pipeline embeddings: %w", err)
	}
	out := make([]CandidateEmbedding, 0, len(rows))
	for _, row := range rows {
		out = append(out, pipelineEmbeddingFromSQL(row))
	}
	return out, nil
}

// ActivateCandidate hands the expected counts to a single atomic statement instead
// of pre-checking them here: any count read in Go would be stale by the time the
// swap runs. A precondition the statement rejects comes back as no rows, which is
// reported as ErrPipelineActivationRejected.
func (s *PostgresPipelineStore) ActivateCandidate(ctx context.Context, req ActivationRequest) error {
	params, err := activationParams(req)
	if err != nil {
		return err
	}
	err = s.withIdentity(ctx, req.IdentityID, func(q *sqlc.Queries) error {
		_, queryErr := q.ActivatePipelineCandidate(ctx, params)
		return queryErr
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("activate candidate: %w", ErrPipelineActivationRejected)
		}
		return fmt.Errorf("activate candidate: %w", err)
	}
	return nil
}

func (s *PostgresPipelineStore) withIdentity(
	ctx context.Context,
	identityID string,
	fn func(*sqlc.Queries) error,
) error {
	if s == nil || s.pool == nil {
		return fmt.Errorf("document pipeline store is not configured")
	}
	if identityID == "" {
		return fmt.Errorf("pipeline identity id is required")
	}
	return db.WithIdentityTx(ctx, s.pool, identityID, fn)
}

func candidateRejectedError(action string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, ErrPipelineCandidateRejected) {
		return fmt.Errorf("%s: %w", action, ErrPipelineCandidateRejected)
	}
	return fmt.Errorf("%s: %w", action, err)
}
