package memory

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var (
	// ErrPromotionDisabled means independent review or class policy has not enabled promotion.
	ErrPromotionDisabled = errors.New("memory promotion is disabled")
	// ErrPolicyDenied hides whether a candidate exists across an authorization boundary.
	ErrPolicyDenied = errors.New("memory policy denied")
)

// ClassPolicy is an explicit per-memory-class allowlist.
type ClassPolicy struct{ AllowPromotion, AllowRetrieval bool }

// Policy cannot enable memory behavior until the independent review is approved.
type Policy struct {
	ReviewApproved bool
	Classes        map[string]ClassPolicy
}

// Principal carries every hard retrieval and promotion boundary.
type Principal struct {
	TenantID, OwnerID, Purpose, Region string
	Capabilities                       []string
	MaxSensitivity                     string
}

// Memory is a promoted typed record; it contains no continuation-summary prose.
type Memory struct {
	ID, CandidateID, Class, EvidenceDigest string
	Confidence                             float64
	CreatedAt                              time.Time
}

// Promote creates one idempotent durable memory only after every hard gate.
func (s *Store) Promote(ctx context.Context, policy Policy, p Principal, candidateID string) (Memory, error) {
	if !policy.ReviewApproved {
		return Memory{}, ErrPromotionDisabled
	}
	var class, tenant, owner, purpose, region, sensitivity, consent, digest string
	var confidence float64
	var expiry time.Time
	err := s.pool.QueryRow(ctx, `SELECT class,tenant_id,owner_id,purpose,region,sensitivity,consent_basis,evidence_digest,confidence,expires_at
		FROM aura.compaction_memory_candidates WHERE id=$1 AND revoked_at IS NULL AND superseded_by IS NULL AND expires_at>now()`, candidateID).
		Scan(&class, &tenant, &owner, &purpose, &region, &sensitivity, &consent, &digest, &confidence, &expiry)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Memory{}, ErrPolicyDenied
		}
		return Memory{}, fmt.Errorf("load candidate for promotion: %w", err)
	}
	cp, ok := policy.Classes[class]
	if !ok || !cp.AllowPromotion {
		return Memory{}, ErrPromotionDisabled
	}
	if !principalAllows(p, tenant, owner, purpose, region, sensitivity) {
		return Memory{}, ErrPolicyDenied
	}
	id := newID()
	err = s.pool.QueryRow(ctx, `INSERT INTO aura.compaction_memories(id,candidate_id,tenant_id,owner_id,class,purpose,consent_basis,evidence_digest,sensitivity,region,expires_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT(candidate_id) DO UPDATE SET candidate_id=EXCLUDED.candidate_id
		RETURNING id,created_at`, id, candidateID, tenant, owner, class, purpose, consent, digest, sensitivity, region, expiry).Scan(&id, &expiry)
	if err != nil {
		return Memory{}, fmt.Errorf("promote candidate: %w", err)
	}
	return Memory{ID: id, CandidateID: candidateID, Class: class, EvidenceDigest: digest, Confidence: confidence, CreatedAt: expiry}, nil
}

// Retrieve applies hard policy filters before relevance, recency, and limit ranking.
func (s *Store) Retrieve(ctx context.Context, policy Policy, p Principal, limit int) ([]Memory, error) {
	if !policy.ReviewApproved || !slices.Contains(p.Capabilities, "memory.read") || limit <= 0 {
		return []Memory{}, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT m.id,m.candidate_id,m.class,m.evidence_digest,c.confidence,m.created_at
		FROM aura.compaction_memories m JOIN aura.compaction_memory_candidates c ON c.id=m.candidate_id
		WHERE m.tenant_id=$1 AND m.owner_id=$2 AND m.purpose=$3 AND m.region=$4
		AND m.revoked_at IS NULL AND m.superseded_by IS NULL AND m.expires_at>now()
		AND c.revoked_at IS NULL AND c.superseded_by IS NULL AND c.expires_at>now()
		AND CASE m.sensitivity WHEN 'public' THEN 0 WHEN 'normal' THEN 1 WHEN 'sensitive' THEN 2 WHEN 'restricted' THEN 3 ELSE 99 END
		 <= CASE $5 WHEN 'public' THEN 0 WHEN 'normal' THEN 1 WHEN 'sensitive' THEN 2 WHEN 'restricted' THEN 3 ELSE -1 END
		ORDER BY c.confidence DESC,m.created_at DESC LIMIT $6`, p.TenantID, p.OwnerID, p.Purpose, p.Region, p.MaxSensitivity, limit)
	if err != nil {
		return nil, fmt.Errorf("retrieve memories: %w", err)
	}
	defer rows.Close()
	out := make([]Memory, 0)
	for rows.Next() {
		var m Memory
		if err := rows.Scan(&m.ID, &m.CandidateID, &m.Class, &m.EvidenceDigest, &m.Confidence, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan memory: %w", err)
		}
		if cp, ok := policy.Classes[m.Class]; !ok || !cp.AllowRetrieval {
			continue
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("retrieve memory rows: %w", err)
	}
	return out, nil
}

// WithdrawConsent transactionally revokes matching candidates and promoted memories.
func (s *Store) WithdrawConsent(ctx context.Context, tenant, owner, purpose string) error {
	return s.revoke(ctx, "consent_withdrawn", `tenant_id=$2 AND owner_id=$3 AND purpose=$4`, tenant, owner, purpose)
}

// DeleteSource transactionally propagates source deletion through candidates and memories.
func (s *Store) DeleteSource(ctx context.Context, kind, id string) error {
	return s.revoke(ctx, "source_deleted", `id IN (SELECT candidate_id FROM aura.compaction_memory_sources WHERE source_kind=$2 AND source_id=$3)`, kind, id)
}

// ForgetMe transactionally revokes all identity-owned candidates and memories.
func (s *Store) ForgetMe(ctx context.Context, tenant, owner string) error {
	return s.revoke(ctx, "forget_me", `tenant_id=$2 AND owner_id=$3`, tenant, owner)
}

// Expire transactionally revokes candidates and memories whose retention expired.
func (s *Store) Expire(ctx context.Context, now time.Time) error {
	return s.revoke(ctx, "expired", `expires_at <= $2`, now)
}

// Supersede links an old candidate and memory to their typed replacements.
func (s *Store) Supersede(ctx context.Context, oldCandidate, newCandidate string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin supersession: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `UPDATE aura.compaction_memory_candidates SET superseded_by=$2 WHERE id=$1 AND superseded_by IS NULL`, oldCandidate, newCandidate); err != nil {
		return fmt.Errorf("supersede candidate: %w", err)
	}
	if _, err = tx.Exec(ctx, `UPDATE aura.compaction_memories SET superseded_by=(SELECT id FROM aura.compaction_memories WHERE candidate_id=$2) WHERE candidate_id=$1 AND superseded_by IS NULL`, oldCandidate, newCandidate); err != nil {
		return fmt.Errorf("supersede memory: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit supersession: %w", err)
	}
	return nil
}

func (s *Store) revoke(ctx context.Context, reason, predicate string, args ...any) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin memory revocation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	query := `UPDATE aura.compaction_memory_candidates SET revoked_at=COALESCE(revoked_at,now()),revocation_reason=COALESCE(revocation_reason,$1) WHERE ` + predicate
	params := append([]any{reason}, args...)
	if _, err = tx.Exec(ctx, query, params...); err != nil {
		return fmt.Errorf("revoke candidates: %w", err)
	}
	if _, err = tx.Exec(ctx, `UPDATE aura.compaction_memories m SET revoked_at=COALESCE(m.revoked_at,now()),revocation_reason=COALESCE(m.revocation_reason,$1) FROM aura.compaction_memory_candidates c WHERE c.id=m.candidate_id AND c.revoked_at IS NOT NULL`, reason); err != nil {
		return fmt.Errorf("revoke memories: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit memory revocation: %w", err)
	}
	return nil
}

func principalAllows(p Principal, tenant, owner, purpose, region, sensitivity string) bool {
	return slices.Contains(p.Capabilities, "memory.read") && p.TenantID == tenant && p.OwnerID == owner && p.Purpose == purpose && p.Region == region && sensitivityAllows(p.MaxSensitivity, sensitivity)
}
func sensitivityAllows(max, actual string) bool {
	rank := map[string]int{"public": 0, "normal": 1, "sensitive": 2, "restricted": 3}
	m, mok := rank[max]
	a, aok := rank[actual]
	return mok && aok && a <= m
}
func newID() string { return uuid.NewString() }
