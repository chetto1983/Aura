package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	// ErrSecretEvidence rejects candidate evidence that resembles credentials.
	ErrSecretEvidence = errors.New("candidate evidence contains a secret")
	// ErrEvidenceOvercollection rejects evidence larger than the minimization boundary.
	ErrEvidenceOvercollection = errors.New("candidate evidence exceeds minimization limit")
)

var secretPattern = regexp.MustCompile(`(?i)(api[_-]?key|password|secret|token)\s*[:=]\s*\S+|sk-[a-z0-9]{16,}`)

// SourceRef is a minimized immutable source pointer; it cannot carry source prose.
type SourceRef struct{ Kind, ID, Digest string }

// CandidateInput is typed durable-memory metadata. Evidence is hashed and never persisted.
type CandidateInput struct {
	TenantID, OwnerID, Class, Purpose, ConsentBasis                 string
	SourceManifest                                                  []SourceRef
	Evidence                                                        string
	Confidence                                                      float64
	Authority, Sensitivity, Region, EncryptionClass, RetentionClass string
	ExpiresAt                                                       time.Time
}

// Candidate is the persisted candidate lifecycle projection.
type Candidate struct {
	ID, TenantID, OwnerID, Class, Purpose, ConsentBasis string
	EvidenceDigest, Authority, Sensitivity, Region      string
	ExpiresAt                                           time.Time
	PromotionAllowed                                    bool
}

// Store persists compaction-derived memory candidates separately from conversations.
type Store struct{ pool *pgxpool.Pool }

// NewStore constructs a durable memory store.
func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// EvidenceDigest returns the lowercase SHA-256 digest used by minimized evidence records.
func EvidenceDigest(v string) string {
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:])
}

// CreateCandidate validates, minimizes, and idempotently persists a candidate and sources.
func (s *Store) CreateCandidate(ctx context.Context, in CandidateInput) (Candidate, error) {
	if err := validateCandidate(in); err != nil {
		return Candidate{}, err
	}
	manifest := append([]SourceRef(nil), in.SourceManifest...)
	sort.Slice(manifest, func(i, j int) bool {
		if manifest[i].Kind == manifest[j].Kind {
			return manifest[i].ID < manifest[j].ID
		}
		return manifest[i].Kind < manifest[j].Kind
	})
	raw, err := json.Marshal(manifest)
	if err != nil {
		return Candidate{}, fmt.Errorf("marshal source manifest: %w", err)
	}
	id := uuid.NewString()
	evidenceDigest := EvidenceDigest(in.Evidence)
	manifestDigest := EvidenceDigest(string(raw))
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Candidate{}, fmt.Errorf("begin candidate: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	const insert = `INSERT INTO aura.compaction_memory_candidates
 (id,tenant_id,owner_id,class,purpose,consent_basis,source_manifest_digest,evidence_digest,confidence,authority,sensitivity,region,encryption_class,retention_class,expires_at)
 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
 ON CONFLICT (tenant_id,owner_id,class,purpose,source_manifest_digest,evidence_digest) DO UPDATE SET id=aura.compaction_memory_candidates.id
 RETURNING id`
	if err := tx.QueryRow(ctx, insert, id, in.TenantID, in.OwnerID, in.Class, in.Purpose, in.ConsentBasis, manifestDigest, evidenceDigest, in.Confidence, in.Authority, in.Sensitivity, in.Region, in.EncryptionClass, in.RetentionClass, in.ExpiresAt).Scan(&id); err != nil {
		return Candidate{}, fmt.Errorf("insert candidate: %w", err)
	}
	for _, src := range manifest {
		if _, err := tx.Exec(ctx, `INSERT INTO aura.compaction_memory_sources(candidate_id,source_kind,source_id,source_digest) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, id, src.Kind, src.ID, src.Digest); err != nil {
			return Candidate{}, fmt.Errorf("insert candidate source: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Candidate{}, fmt.Errorf("commit candidate: %w", err)
	}
	return Candidate{ID: id, TenantID: in.TenantID, OwnerID: in.OwnerID, Class: in.Class, Purpose: in.Purpose, ConsentBasis: in.ConsentBasis, EvidenceDigest: evidenceDigest, Authority: in.Authority, Sensitivity: in.Sensitivity, Region: in.Region, ExpiresAt: in.ExpiresAt, PromotionAllowed: false}, nil
}

func validateCandidate(in CandidateInput) error {
	if _, err := uuid.Parse(in.TenantID); err != nil {
		return fmt.Errorf("candidate tenant: %w", err)
	}
	if _, err := uuid.Parse(in.OwnerID); err != nil {
		return fmt.Errorf("candidate owner: %w", err)
	}
	if len(in.Evidence) > 256 {
		return ErrEvidenceOvercollection
	}
	if secretPattern.MatchString(in.Evidence) {
		return ErrSecretEvidence
	}
	if strings.TrimSpace(in.Evidence) == "" || len(in.SourceManifest) == 0 {
		return errors.New("candidate evidence and source manifest are required")
	}
	if in.Confidence < 0 || in.Confidence > 1 || in.ExpiresAt.IsZero() {
		return errors.New("candidate confidence or expiry is invalid")
	}
	if in.Authority != "user" && in.Authority != "tool" {
		return errors.New("candidate authority must be user or tool")
	}
	if in.EncryptionClass == "" || in.Region == "" || in.Purpose == "" || in.ConsentBasis == "" || in.Class == "" {
		return errors.New("candidate policy metadata is incomplete")
	}
	for _, src := range in.SourceManifest {
		if src.Kind != "turn" && src.Kind != "checkpoint" && src.Kind != "artifact" {
			return errors.New("candidate source kind is invalid")
		}
		if _, err := hex.DecodeString(src.Digest); err != nil || len(src.Digest) != 64 {
			return errors.New("candidate source digest is invalid")
		}
	}
	return nil
}
