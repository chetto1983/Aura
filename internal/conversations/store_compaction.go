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

// ErrCompactionInProgress reports that a live owner is already running inference.
var ErrCompactionInProgress = errors.New("conversations: compaction inference in progress")

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

// CompactionPreview is the bounded operator projection of the active checkpoint.
type CompactionPreview struct {
	CheckpointID, PriorCheckpointID, Summary string
}

// PreviewCompaction returns only the active checkpoint and its parent for one branch.
func (s *Store) PreviewCompaction(ctx context.Context, conversationID, branchID string) (CompactionPreview, error) {
	conv, err := uuid.Parse(conversationID)
	if err != nil {
		return CompactionPreview{}, err
	}
	var checkpointID, priorID, summary string
	err = s.pool.QueryRow(ctx, `SELECT cp.id::text,COALESCE(cp.parent_id::text,''),cp.structured_summary::text
		FROM aura.compaction_active_pointers ap
		JOIN aura.compaction_checkpoints cp ON cp.id=ap.checkpoint_id
		WHERE ap.conversation_id=$1 AND ap.branch_id=$2`, conv, branchID).Scan(&checkpointID, &priorID, &summary)
	if errors.Is(err, pgx.ErrNoRows) {
		return CompactionPreview{}, nil
	}
	if err != nil {
		return CompactionPreview{}, fmt.Errorf("preview compaction: %w", err)
	}
	return CompactionPreview{CheckpointID: checkpointID, PriorCheckpointID: priorID, Summary: summary}, nil
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
	var pendingOp uuid.UUID
	var pendingOwner, pendingPriority string
	var pendingLease time.Time
	var inferenceStarted bool
	err = tx.QueryRow(ctx, `SELECT operation_id,owner_id,priority,lease_until,inference_started FROM aura.compaction_claims WHERE conversation_id=$1 AND branch_id=$2 AND state='pending' FOR UPDATE`, conv, p.BranchID).Scan(&pendingOp, &pendingOwner, &pendingPriority, &pendingLease, &inferenceStarted)
	if err == nil {
		if pendingOp == op {
			if time.Now().After(pendingLease) {
				_, err = tx.Exec(ctx, `UPDATE aura.compaction_claims SET owner_id=$2,lease_until=$3,updated_at=now() WHERE operation_id=$1`, op, p.OwnerID, p.LeaseUntil)
				if err != nil {
					return CompactionClaim{}, err
				}
			}
		} else if p.Priority == ClaimPriorityManual && pendingPriority == string(ClaimPriorityAutomatic) && !inferenceStarted {
			_, err = tx.Exec(ctx, `UPDATE aura.compaction_claims SET state='superseded',updated_at=now() WHERE operation_id=$1`, pendingOp)
			if err != nil {
				return CompactionClaim{}, err
			}
		} else {
			return CompactionClaim{}, ErrCompactionInProgress
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return CompactionClaim{}, fmt.Errorf("inspect pending claim: %w", err)
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

// MarkCompactionInferenceStarted durably closes the manual-priority preemption window.
func (s *Store) MarkCompactionInferenceStarted(ctx context.Context, operationID, ownerID string) error {
	op, err := uuid.Parse(operationID)
	if err != nil {
		return err
	}
	cmd, err := s.pool.Exec(ctx, `UPDATE aura.compaction_claims SET inference_started=true,updated_at=now() WHERE operation_id=$1 AND owner_id=$2 AND state='pending' AND lease_until>now()`, op, ownerID)
	if err != nil {
		return fmt.Errorf("mark inference started: %w", err)
	}
	if cmd.RowsAffected() != 1 {
		return ErrCompactionSuperseded
	}
	return nil
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
	if state != "pending" || owner != p.OwnerID {
		return "", ErrCompactionSuperseded
	}
	if time.Now().After(lease) || capturedGov != p.CurrentGovernanceWatermark {
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

// RestoreCompaction serializes on the active pointer and appends an immutable audit event.
func (s *Store) RestoreCompaction(ctx context.Context, conversationID, branchID, checkpointID, operationID, actorID string) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	conv, err := uuid.Parse(conversationID)
	if err != nil {
		return err
	}
	target, err := uuid.Parse(checkpointID)
	if err != nil {
		return err
	}
	op, err := uuid.Parse(operationID)
	if err != nil {
		return err
	}
	var old *uuid.UUID
	var generation int
	if err = tx.QueryRow(ctx, `SELECT checkpoint_id,generation FROM aura.compaction_active_pointers WHERE conversation_id=$1 AND branch_id=$2 FOR UPDATE`, conv, branchID).Scan(&old, &generation); err != nil {
		return fmt.Errorf("lock restore pointer: %w", err)
	}
	var targetGeneration int
	if err = tx.QueryRow(ctx, `SELECT generation FROM aura.compaction_checkpoints WHERE id=$1 AND conversation_id=$2 AND branch_id=$3`, target, conv, branchID).Scan(&targetGeneration); err != nil {
		return fmt.Errorf("load restore target: %w", err)
	}
	if _, err = tx.Exec(ctx, `UPDATE aura.compaction_active_pointers SET checkpoint_id=$3,generation=$4,updated_at=now() WHERE conversation_id=$1 AND branch_id=$2`, conv, branchID, target, targetGeneration); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO aura.compaction_restore_events(id,conversation_id,branch_id,old_checkpoint_id,new_checkpoint_id,operation_id,actor_id) VALUES($1,$2,$3,$4,$5,$6,$7)`, uuid.New(), conv, branchID, old, target, op, actorID); err != nil {
		return fmt.Errorf("audit restore: %w", err)
	}
	return tx.Commit(ctx)
}

// QuarantineCompactionCheckpoint records corruption without changing the active pointer.
func (s *Store) QuarantineCompactionCheckpoint(ctx context.Context, checkpointID, kind, reason, observedDigest string) error {
	id, err := uuid.Parse(checkpointID)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO aura.compaction_quarantine(id,checkpoint_id,artifact_kind,reason,observed_digest) VALUES($1,$2,$3,$4,$5)`, uuid.New(), id, kind, reason, observedDigest)
	if err != nil {
		return fmt.Errorf("quarantine checkpoint: %w", err)
	}
	return nil
}
