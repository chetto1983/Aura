// The storage half of the verification evidence ledger.
//
// A Go port of the persistence in NousResearch/hermes-agent
// `agent/verification_evidence.py` (MIT, commit 9d4ef04ed), whose store is SQLite in
// the user's home directory. Aura's store is Postgres, so the schema is migration 0094
// and the isolation is that migration's RLS policy rather than a per-user file.
//
// The three public operations are the original's, name for name, and they maintain one
// invariant between them:
//
//	RecordTerminalResult  — a classified command CLEARS last_edit_at
//	MarkWorkspaceEdited   — an edit SETS it
//	VerificationStatus    — "passed" only when an event exists and nothing edited after
//
// Everything reads and writes through db.WithIdentityTx, because both tables carry a
// fail-closed RLS policy: a caller with no app.current_identity sees nothing at all,
// which is the behaviour we want if the identity is ever lost rather than a silent
// cross-tenant read.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// maxEvidenceAge and maxEventsPerSessionRoot are the original's retention bounds.
	maxEvidenceAge          = 30 * 24 * time.Hour
	maxEventsPerSessionRoot = 100

	// verificationReadTimeout bounds the status read the gate makes at a voluntary
	// termination. Short on purpose: that read runs on the TURN's own goroutine, so an
	// unbounded one would hold the turn open for as long as a stalled Postgres or an
	// exhausted pgxpool takes to answer -- which is never. Losing the answer costs one
	// nudge; losing the turn costs the session.
	verificationReadTimeout = 2 * time.Second
)

// Ledger statuses. "not_applicable" is what the caller gets for a directory the project
// detector does not recognise; the policy treats it as nothing to say.
const (
	StatusPassed        = "passed"
	StatusStale         = "stale"
	StatusUnverified    = "unverified"
	StatusNotApplicable = "not_applicable"
)

// EvidenceStore is the Postgres-backed ledger.
type EvidenceStore struct {
	pool *pgxpool.Pool
}

// NewEvidenceStore returns a ledger over pool. A nil pool yields a nil store, and every
// method on a nil store is a no-op that reports "not applicable" -- the Go equivalent of
// the original's ImportError guards, so a deployment without the ledger degrades to no
// nudge rather than to a panic.
func NewEvidenceStore(pool *pgxpool.Pool) *EvidenceStore {
	if pool == nil {
		return nil
	}
	return &EvidenceStore{pool: pool}
}

func identityUUID(identityID string) (pgtype.UUID, error) {
	parsed, err := uuid.Parse(identityID)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("verification ledger: identity %q: %w", identityID, err)
	}
	return pgtype.UUID{Bytes: parsed, Valid: true}, nil
}

// RecordTerminalResult records a finished command when it is verification evidence, and
// reports ok=false when it is not. Recording CLEARS the edit marker: the workspace has
// just been proved, whatever it looked like a moment ago.
func (s *EvidenceStore) RecordTerminalResult(
	ctx context.Context,
	identityID string,
	input ClassifyVerificationCommandInput,
) (VerificationCommandEvidence, bool, error) {
	if s == nil {
		return VerificationCommandEvidence{}, false, nil
	}
	evidence, ok := ClassifyVerificationCommand(input)
	if !ok {
		return VerificationCommandEvidence{}, false, nil
	}
	owner, err := identityUUID(identityID)
	if err != nil {
		return VerificationCommandEvidence{}, false, err
	}

	err = db.WithIdentityTx(ctx, s.pool, identityID, func(q *sqlc.Queries) error {
		inserted, err := q.InsertVerificationEvent(ctx, sqlc.InsertVerificationEventParams{
			IdentityID: owner, SessionID: evidence.SessionID,
			Cwd: evidence.CWD, Root: evidence.Root,
			Command: evidence.Command, CanonicalCommand: evidence.CanonicalCommand,
			Kind: evidence.Kind, Scope: evidence.Scope, Status: evidence.Status,
			ExitCode: int32(evidence.ExitCode), OutputSummary: evidence.OutputSummary,
		})
		if err != nil {
			return fmt.Errorf("insert verification event: %w", err)
		}
		if err := q.UpsertVerificationStateVerified(ctx, sqlc.UpsertVerificationStateVerifiedParams{
			IdentityID: owner, SessionID: evidence.SessionID, Root: evidence.Root,
			LastEventID: pgtype.Int8{Int64: inserted.ID, Valid: true},
		}); err != nil {
			return fmt.Errorf("mark workspace verified: %w", err)
		}
		return s.prune(ctx, q, owner, evidence.SessionID, evidence.Root)
	})
	if err != nil {
		return VerificationCommandEvidence{}, false, err
	}
	return evidence, true, nil
}

// MarkWorkspaceEdited stales this workspace's evidence after a successful file edit.
func (s *EvidenceStore) MarkWorkspaceEdited(
	ctx context.Context,
	identityID, sessionID, root string,
	paths []string,
) error {
	if s == nil {
		return nil
	}
	owner, err := identityUUID(identityID)
	if err != nil {
		return err
	}
	if paths == nil {
		paths = []string{}
	}
	encoded, err := json.Marshal(paths)
	if err != nil {
		return fmt.Errorf("encode changed paths: %w", err)
	}
	return db.WithIdentityTx(ctx, s.pool, identityID, func(q *sqlc.Queries) error {
		return q.UpsertVerificationStateEdited(ctx, sqlc.UpsertVerificationStateEditedParams{
			IdentityID: owner, SessionID: sessionID, Root: root, ChangedPaths: encoded,
		})
	})
}

// VerificationStatus returns the best known state for one session and workspace.
//
// The three answers and what each means:
//
//	unverified — nothing has been recorded, or an edit staled it with no prior event
//	stale      — a passing event exists but an edit happened after it
//	passed     — the last event passed and nothing has been edited since
//
// A failing event reports its own status ("failed"), because the ledger records what was
// proved rather than only what succeeded.
func (s *EvidenceStore) VerificationStatus(
	ctx context.Context,
	identityID, sessionID, root string,
) (VerificationStatus, error) {
	if s == nil {
		return VerificationStatus{Status: StatusNotApplicable}, nil
	}
	owner, err := identityUUID(identityID)
	if err != nil {
		return VerificationStatus{Status: StatusNotApplicable}, err
	}

	var row sqlc.GetVerificationStateRow
	err = db.WithIdentityTx(ctx, s.pool, identityID, func(q *sqlc.Queries) error {
		found, qerr := q.GetVerificationState(ctx, sqlc.GetVerificationStateParams{
			IdentityID: owner, SessionID: sessionID, Root: root,
		})
		if qerr != nil {
			return qerr
		}
		row = found
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return VerificationStatus{Status: StatusUnverified}, nil
	}
	if err != nil {
		return VerificationStatus{Status: StatusUnverified}, err
	}
	return statusFromRow(row), nil
}

func statusFromRow(row sqlc.GetVerificationStateRow) VerificationStatus {
	// An edit outranks the event it staled: last_edit_at is set only by an edit and
	// cleared only by a passing record, so its presence alone decides staleness. No
	// timestamp comparison, which is what stops the two clocks disagreeing.
	if row.LastEditAt.Valid {
		if row.Status.Valid {
			return VerificationStatus{Status: StatusStale, Evidence: evidenceFromRow(row)}
		}
		return VerificationStatus{Status: StatusUnverified}
	}
	if !row.Status.Valid {
		return VerificationStatus{Status: StatusUnverified}
	}
	return VerificationStatus{Status: row.Status.String, Evidence: evidenceFromRow(row)}
}

func evidenceFromRow(row sqlc.GetVerificationStateRow) *VerificationEvidence {
	return &VerificationEvidence{
		CanonicalCommand: row.CanonicalCommand.String,
		Command:          row.Command.String,
		OutputSummary:    row.OutputSummary.String,
	}
}

// prune bounds the ledger exactly where the original does: after a successful record,
// and never in a way that can delete the row the state points at.
func (s *EvidenceStore) prune(
	ctx context.Context,
	q *sqlc.Queries,
	owner pgtype.UUID,
	sessionID, root string,
) error {
	if err := q.DeleteSupersededVerificationEvents(ctx, sqlc.DeleteSupersededVerificationEventsParams{
		IdentityID: owner, SessionID: sessionID, Root: root, Limit: maxEventsPerSessionRoot,
	}); err != nil {
		return fmt.Errorf("prune superseded events: %w", err)
	}
	cutoff := pgtype.Timestamptz{Time: time.Now().UTC().Add(-maxEvidenceAge), Valid: true}
	if err := q.DeleteExpiredVerificationState(ctx, cutoff); err != nil {
		return fmt.Errorf("prune expired state: %w", err)
	}
	if err := q.DeleteExpiredVerificationEvents(ctx, cutoff); err != nil {
		return fmt.Errorf("prune expired events: %w", err)
	}
	return nil
}

// LedgerAdapter joins the two halves the policy needs behind one interface: the
// filesystem detector answers "is this a project and how does it verify itself", the
// Postgres store answers "what was proved here". They are separate because they have
// nothing in common but the question the gate asks.
type LedgerAdapter struct {
	Detector   FilesystemProjectDetector
	Store      *EvidenceStore
	IdentityID string
}

// ProjectFactsFor delegates to the detector.
func (a LedgerAdapter) ProjectFactsFor(cwd string) ProjectFacts {
	return a.Detector.ProjectFactsFor(cwd)
}

// VerificationStatusFor resolves cwd to its project root before reading the ledger,
// because the ledger keys on ROOT: a suite run from a subdirectory and one run from the
// top are the same workspace, and looking the status up by cwd would split them.
//
// A read failure reports "not_applicable" rather than propagating: the gate that consumes
// this decides whether to nudge, and a database hiccup must not become a turn error. It
// must not become a NUDGE either -- "unverified" would read as "this workspace needs
// proof", so an outage would demand verification of every edited workspace on every turn,
// twice, about a ledger nobody could actually ask. "not_applicable" is the policy's way
// of saying there is nothing to say. The slog.Warn keeps the outage visible.
func (a LedgerAdapter) VerificationStatusFor(sessionID, cwd string) VerificationStatus {
	facts := a.Detector.ProjectFactsFor(cwd)
	if !facts.Found {
		return VerificationStatus{Status: StatusNotApplicable}
	}
	ctx, cancel := verificationReadContext()
	defer cancel()
	status, err := a.Store.VerificationStatus(ctx, a.IdentityID, sessionID, facts.Root)
	if err != nil {
		slog.Warn("verification ledger: read status", "error", err, "session_id", sessionID, "root", facts.Root)
		return VerificationStatus{Status: StatusNotApplicable}
	}
	return status
}

// verificationReadContext bounds one ledger read (verificationReadTimeout). It is rooted
// in context.Background() on purpose: the read happens AT a voluntary termination, where
// a wallclock trip may already have made the turn ctx Done -- the same hazard the
// completion critic severs its deadline for (llm_agent_completion.go). Detached is not
// the same as unbounded, and this timeout is the whole difference between them.
func verificationReadContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), verificationReadTimeout)
}
