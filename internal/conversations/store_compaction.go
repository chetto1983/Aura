package conversations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ErrCompactionSuperseded reports a lost lease, pointer CAS, or governance race.
var ErrCompactionSuperseded = errors.New("conversations: compaction claim superseded")

// CompactionClaimParams contains the durable identity and captured state for a claim.
type CompactionClaimParams struct {
	OperationID, ConversationID, BranchID, IdempotencyKey, OwnerID string
	CapturedWatermark                                              int
	GovernanceWatermark                                            int64
	BaseGeneration                                                 int
	Priority                                                       ClaimPriority
	LeaseUntil                                                     time.Time
}

// CompactionClaim is the existing or newly acquired durable outcome.
type CompactionClaim struct {
	OperationID         string
	State               ClaimState
	OwnerID             string
	LeaseUntil          time.Time
	OutcomeCheckpointID string
}

// ClaimCompaction performs only durable coordination; inference starts after it returns.
func (s *Store) ClaimCompaction(ctx context.Context, p CompactionClaimParams) (CompactionClaim, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return CompactionClaim{}, fmt.Errorf("begin compaction claim: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	conv, err := uuid.Parse(p.ConversationID)
	if err != nil {
		return CompactionClaim{}, fmt.Errorf("parse conversation id: %w", err)
	}
	op, err := uuid.Parse(p.OperationID)
	if err != nil {
		return CompactionClaim{}, fmt.Errorf("parse operation id: %w", err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO aura.compaction_active_pointers(conversation_id,branch_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, conv, p.BranchID)
	if err != nil {
		return CompactionClaim{}, fmt.Errorf("ensure active pointer: %w", err)
	}
	var generation int
	if err = tx.QueryRow(ctx, `SELECT generation FROM aura.compaction_active_pointers WHERE conversation_id=$1 AND branch_id=$2 FOR UPDATE`, conv, p.BranchID).Scan(&generation); err != nil {
		return CompactionClaim{}, fmt.Errorf("lock active pointer: %w", err)
	}
	if generation != p.BaseGeneration {
		return CompactionClaim{}, ErrCompactionSuperseded
	}
	var existing CompactionClaim
	err = tx.QueryRow(ctx, `INSERT INTO aura.compaction_claims(operation_id,conversation_id,branch_id,idempotency_key,captured_watermark_seq,governance_watermark,base_active_generation,priority,state,owner_id,lease_until)
	VALUES($1,$2,$3,$4,$5,$6,$7,$8,'pending',$9,$10)
	ON CONFLICT(conversation_id,branch_id,idempotency_key) DO UPDATE SET updated_at=aura.compaction_claims.updated_at
	RETURNING operation_id::text,state,owner_id,lease_until,COALESCE(outcome_checkpoint_id::text,'')`, op, conv, p.BranchID, p.IdempotencyKey, p.CapturedWatermark, p.GovernanceWatermark, p.BaseGeneration, p.Priority, p.OwnerID, p.LeaseUntil).Scan(&existing.OperationID, &existing.State, &existing.OwnerID, &existing.LeaseUntil, &existing.OutcomeCheckpointID)
	if err != nil {
		return CompactionClaim{}, fmt.Errorf("insert compaction claim: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return CompactionClaim{}, fmt.Errorf("commit compaction claim: %w", err)
	}
	return existing, nil
}

// FinalizeCompactionParams carries validated inference output into the short CAS transaction.
type FinalizeCompactionParams struct {
	OperationID, OwnerID                                                  string
	CurrentGovernanceWatermark                                            int64
	CheckpointID                                                          string
	CapturedWatermarkSeq                                                  int
	SummarizedTurnSeqs, TailTurnSeqs, ProtectedTurnSeqs, ExcludedTurnSeqs []int
	ManifestDigest, CompleteCaptureDigest, DigestAlgorithm                string
	DigestVersion                                                         int
	StructuredSummary                                                     json.RawMessage
	SchemaVersion, PromptVersion, ProjectionVersion                       int
	QualityState, RolloutMode                                             string
	RetentionUntil                                                        time.Time
}

// FinalizeCompaction uses a short serializable transaction for claim validation, insert and pointer CAS.
func (s *Store) FinalizeCompaction(ctx context.Context, p FinalizeCompactionParams) (string, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return "", fmt.Errorf("begin finalize: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	op, err := uuid.Parse(p.OperationID)
	if err != nil {
		return "", fmt.Errorf("parse operation id: %w", err)
	}
	var conv uuid.UUID
	var branch, state, owner string
	var lease time.Time
	var base int
	var capturedGov int64
	var prior *uuid.UUID
	err = tx.QueryRow(ctx, `SELECT conversation_id,branch_id,state,owner_id,lease_until,base_active_generation,governance_watermark,outcome_checkpoint_id FROM aura.compaction_claims WHERE operation_id=$1 FOR UPDATE`, op).Scan(&conv, &branch, &state, &owner, &lease, &base, &capturedGov, &prior)
	if err != nil {
		return "", fmt.Errorf("lock claim: %w", err)
	}
	if prior != nil {
		return prior.String(), nil
	}
	if state != "pending" || owner != p.OwnerID || time.Now().After(lease) || capturedGov != p.CurrentGovernanceWatermark {
		_, _ = tx.Exec(ctx, `UPDATE aura.compaction_claims SET state='superseded',updated_at=now() WHERE operation_id=$1`, op)
		_ = tx.Commit(ctx)
		return "", ErrCompactionSuperseded
	}
	var active int
	var parent *uuid.UUID
	if err = tx.QueryRow(ctx, `SELECT generation,checkpoint_id FROM aura.compaction_active_pointers WHERE conversation_id=$1 AND branch_id=$2 FOR UPDATE`, conv, branch).Scan(&active, &parent); err != nil {
		return "", fmt.Errorf("lock pointer: %w", err)
	}
	if active != base {
		_, _ = tx.Exec(ctx, `UPDATE aura.compaction_claims SET state='superseded',updated_at=now() WHERE operation_id=$1`, op)
		_ = tx.Commit(ctx)
		return "", ErrCompactionSuperseded
	}
	id, err := uuid.Parse(p.CheckpointID)
	if err != nil {
		return "", fmt.Errorf("parse checkpoint id: %w", err)
	}
	sum, _ := json.Marshal(p.SummarizedTurnSeqs)
	tail, _ := json.Marshal(p.TailTurnSeqs)
	prot, _ := json.Marshal(p.ProtectedTurnSeqs)
	excluded, _ := json.Marshal(p.ExcludedTurnSeqs)
	_, err = tx.Exec(ctx, `INSERT INTO aura.compaction_checkpoints(id,conversation_id,branch_id,generation,parent_id,captured_watermark_seq,summarized_turn_seqs,tail_turn_seqs,protected_turn_seqs,excluded_turn_seqs,manifest_digest,complete_capture_digest,digest_algorithm,digest_version,structured_summary,schema_version,prompt_version,projection_version,quality_state,rollout_mode,retention_until) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)`, id, conv, branch, active+1, parent, p.CapturedWatermarkSeq, sum, tail, prot, excluded, p.ManifestDigest, p.CompleteCaptureDigest, p.DigestAlgorithm, p.DigestVersion, p.StructuredSummary, p.SchemaVersion, p.PromptVersion, p.ProjectionVersion, p.QualityState, p.RolloutMode, p.RetentionUntil)
	if err != nil {
		return "", fmt.Errorf("insert checkpoint: %w", err)
	}
	cmd, err := tx.Exec(ctx, `UPDATE aura.compaction_active_pointers SET generation=$3,checkpoint_id=$4,updated_at=now() WHERE conversation_id=$1 AND branch_id=$2 AND generation=$5`, conv, branch, active+1, id, active)
	if err != nil || cmd.RowsAffected() != 1 {
		return "", ErrCompactionSuperseded
	}
	_, err = tx.Exec(ctx, `UPDATE aura.compaction_claims SET state='completed',outcome_checkpoint_id=$2,updated_at=now() WHERE operation_id=$1`, op, id)
	if err != nil {
		return "", fmt.Errorf("complete claim: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit finalize: %w", err)
	}
	return id.String(), nil
}
