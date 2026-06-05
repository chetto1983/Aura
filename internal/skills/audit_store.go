package skills

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The append-only skill-audit store copies the canonical Store pattern proved in
// internal/identity (D-A4-01) and internal/askuser: Store{pool,q} over the
// generated sqlc surface, SQLSTATE-based error classification via errors.As +
// pgErr.Code (never message matching), sentinel errors, pgtype conversion at the
// boundary. aura.skill_audit is APPEND-ONLY at the database (T-11-04-R1): the role
// grant withholds UPDATE/DELETE/TRUNCATE and two triggers raise — so this Store
// exposes only InsertAudit + read paths; there is deliberately no Update/Delete.

// AuditAction enumerates the mutation that produced an audit row. It mirrors the
// 0010 CHECK on aura.skill_audit.action.
type AuditAction string

// The eight audit actions, mirroring the 0010 action CHECK: the four model-facing
// mutations, activate/archive, and the two system sweep actions.
const (
	AuditCreate         AuditAction = "create"
	AuditUpdate         AuditAction = "update"
	AuditDelete         AuditAction = "delete"
	AuditInstall        AuditAction = "install"
	AuditActivate       AuditAction = "activate"
	AuditArchive        AuditAction = "archive"
	AuditAutoArchive    AuditAction = "auto_archive"
	AuditCleanupPending AuditAction = "cleanup_pending_stale"
)

// ApprovalSource enumerates how a gated mutation was approved. NULL (the empty
// string here) is the pending row that has not yet exercised its gate.
type ApprovalSource string

// The four approval sources, mirroring the D-29 matrix rows (none=pending).
const (
	ApprovalNone    ApprovalSource = ""         // pending — gate not yet exercised
	ApprovalAskUser ApprovalSource = "ask_user" // user approved/rejected via resume
	ApprovalCLI     ApprovalSource = "cli"      // operator CLI approve / --allow-blocklisted
	ApprovalAuto    ApprovalSource = "auto"     // system sweep (auto_archive / cleanup_pending_stale)
)

// Sentinel errors so callers classify failures without string matching. The
// append-only triggers / role grant surface as ErrAuditImmutable (SQLSTATE 42501
// insufficient_privilege, raised by both the role-denied path and the trigger);
// an incoherent D-29 tuple surfaces as ErrAuditIncoherent (SQLSTATE 23514
// check_violation).
var (
	ErrAuditImmutable  = errors.New("skill_audit is append-only")
	ErrAuditIncoherent = errors.New("skill_audit row violates the D-29 coherence constraint")
)

// AuditStore wraps a pgx pool and the generated Queries (canonical shape). It is
// INSERT + SELECT only — the append-only contract is enforced at the DB, and the
// Store mirrors it by exposing no mutation method.
type AuditStore struct {
	pool *pgxpool.Pool
	q    *sqlc.Queries
}

// NewAuditStore builds an AuditStore over an open pool.
func NewAuditStore(pool *pgxpool.Pool) *AuditStore {
	return &AuditStore{pool: pool, q: sqlc.New(pool)}
}

// AuditRow is the domain projection of one aura.skill_audit row — plain Go types
// at the package boundary instead of the sqlc pgtype wrappers. PausedStateToken
// and ApprovalSource are empty strings when the underlying columns are NULL.
type AuditRow struct {
	ID                string
	CreatedAt         time.Time
	ActorID           string
	IdentityID        string
	SkillName         string
	Action            AuditAction
	ContentHash       string
	ApprovalSource    ApprovalSource
	PausedStateToken  string
	GateRecommended   bool
	GateTaken         bool
	BlocklistOverride bool
}

// AuditInsert carries the plain fields for one new audit row. The caller (the
// writer, inside db.WithTx) supplies a D-29-coherent tuple; an incoherent tuple is
// rejected by the DB CHECK (ErrAuditIncoherent). IdentityID defaults to "local"
// when empty (the single-user scaffold).
type AuditInsert struct {
	ActorID           string
	IdentityID        string
	SkillName         string
	Action            AuditAction
	ContentHash       string
	ApprovalSource    ApprovalSource
	PausedStateToken  string // canonical UUID string, or "" for NULL
	GateRecommended   bool
	GateTaken         bool
	BlocklistOverride bool
}

// toParams converts an AuditInsert to the generated InsertSkillAuditParams,
// applying the NULL boundary mapping for the optional columns.
func (a AuditInsert) toParams() (sqlc.InsertSkillAuditParams, error) {
	identity := a.IdentityID
	if identity == "" {
		identity = "local"
	}
	var src pgtype.Text
	if a.ApprovalSource != ApprovalNone {
		src = pgtype.Text{String: string(a.ApprovalSource), Valid: true}
	}
	var token pgtype.UUID
	if a.PausedStateToken != "" {
		u, err := uuid.Parse(a.PausedStateToken)
		if err != nil {
			return sqlc.InsertSkillAuditParams{}, fmt.Errorf("invalid paused_state_token %q: %w", a.PausedStateToken, err)
		}
		token = pgtype.UUID{Bytes: u, Valid: true}
	}
	return sqlc.InsertSkillAuditParams{
		ActorID:           a.ActorID,
		IdentityID:        identity,
		SkillName:         a.SkillName,
		Action:            string(a.Action),
		ContentHash:       a.ContentHash,
		ApprovalSource:    src,
		PausedStateToken:  token,
		GateRecommended:   a.GateRecommended,
		GateTaken:         a.GateTaken,
		BlocklistOverride: a.BlocklistOverride,
	}, nil
}

// InsertAudit appends one audit row via the pool (non-tx). The writer's atomic
// path inserts via InsertAuditTx so the audit row and the activation bookkeeping
// commit all-or-nothing.
func (s *AuditStore) InsertAudit(ctx context.Context, in AuditInsert) (AuditRow, error) {
	params, err := in.toParams()
	if err != nil {
		return AuditRow{}, fmt.Errorf("insert skill audit: %w", err)
	}
	row, err := s.q.InsertSkillAudit(ctx, params)
	if err != nil {
		return AuditRow{}, fmt.Errorf("insert skill audit %q: %w", in.SkillName, classifyAuditErr(err))
	}
	return auditFromRow(row), nil
}

// InsertAuditTx appends one audit row using a tx-bound Queries (the writer's
// db.WithTx closure passes it), so the INSERT participates in the surrounding
// atomic write.
func InsertAuditTx(ctx context.Context, q *sqlc.Queries, in AuditInsert) error {
	params, err := in.toParams()
	if err != nil {
		return fmt.Errorf("insert skill audit (tx): %w", err)
	}
	if _, err := q.InsertSkillAudit(ctx, params); err != nil {
		return fmt.Errorf("insert skill audit %q (tx): %w", in.SkillName, classifyAuditErr(err))
	}
	return nil
}

// AuditFilter narrows the audit list for the CLI. Limit <= 0 falls back to 100.
type AuditFilter struct {
	SkillName string    // "" = all skills
	Since     time.Time // zero = no lower bound
	Limit     int
}

// List returns audit rows newest-first, filtered by skill name and/or a since
// cutoff. It is the read half (aura_app holds SELECT).
func (s *AuditStore) List(ctx context.Context, f AuditFilter) ([]AuditRow, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	var name pgtype.Text
	if f.SkillName != "" {
		name = pgtype.Text{String: f.SkillName, Valid: true}
	}
	var since pgtype.Timestamptz
	if !f.Since.IsZero() {
		since = pgtype.Timestamptz{Time: f.Since, Valid: true}
	}
	rows, err := s.q.ListSkillAudit(ctx, sqlc.ListSkillAuditParams{
		Limit:     int32(limit),
		SkillName: name,
		Since:     since,
	})
	if err != nil {
		return nil, fmt.Errorf("list skill audit: %w", err)
	}
	out := make([]AuditRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, auditFromRow(r))
	}
	return out, nil
}

// auditFromRow projects a generated row onto the domain AuditRow.
func auditFromRow(r sqlc.AuraSkillAudit) AuditRow {
	out := AuditRow{
		ID:                uuid.UUID(r.ID.Bytes).String(),
		CreatedAt:         r.CreatedAt.Time,
		ActorID:           r.ActorID,
		IdentityID:        r.IdentityID,
		SkillName:         r.SkillName,
		Action:            AuditAction(r.Action),
		ContentHash:       r.ContentHash,
		GateRecommended:   r.GateRecommended,
		GateTaken:         r.GateTaken,
		BlocklistOverride: r.BlocklistOverride,
	}
	if r.ApprovalSource.Valid {
		out.ApprovalSource = ApprovalSource(r.ApprovalSource.String)
	}
	if r.PausedStateToken.Valid {
		out.PausedStateToken = uuid.UUID(r.PausedStateToken.Bytes).String()
	}
	return out
}

// classifyAuditErr maps a pgx error to a sentinel via SQLSTATE (errors.As +
// pgErr.Code, never message-matching, mirrors identity.isUniqueViolation):
// 42501 insufficient_privilege (role-denied OR the append-only trigger) →
// ErrAuditImmutable; 23514 check_violation (the D-29 CHECK) → ErrAuditIncoherent.
func classifyAuditErr(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	switch pgErr.Code {
	case "42501":
		return fmt.Errorf("%w: %v", ErrAuditImmutable, err)
	case "23514":
		return fmt.Errorf("%w: %v", ErrAuditIncoherent, err)
	default:
		return err
	}
}
