// Package toolapprovals is the durable record of a tool call parked for the operator's approval
// (migration 0160): the exact arguments an accept runs, and the one-way state of that call.
package toolapprovals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
)

// Status is where a parked call stands. Only the moves in transitions exist.
type Status string

// The statuses of aura.tool_approvals.status.
const (
	StatusPending   Status = "pending"
	StatusAccepted  Status = "accepted"
	StatusRunning   Status = "running"
	StatusDone      Status = "done"
	StatusUnknown   Status = "unknown"
	StatusDeclined  Status = "declined"
	StatusExpired   Status = "expired"
	StatusCancelled Status = "cancelled"
	StatusNotRun    Status = "not_run"
)

// Source is what parked the call: the tool's own review, or the gateway's policy.
type Source string

// The sources of aura.tool_approvals.source.
const (
	SourceTool    Source = "tool"
	SourceGateway Source = "gateway"
)

// decisions are the answers DecideTx gives a pending record. Only DecideTx accepts, inside the
// resume transaction; a by-id Transition never does.
var decisions = []Status{StatusAccepted, StatusDeclined, StatusExpired, StatusNotRun, StatusCancelled}

// transitions is the by-id state table; not_run from running is a hook's refusal of an approved call.
var transitions = map[Status][]Status{
	StatusPending:  {StatusDeclined, StatusExpired, StatusNotRun, StatusCancelled},
	StatusAccepted: {StatusRunning, StatusNotRun, StatusCancelled},
	StatusRunning:  {StatusDone, StatusUnknown, StatusNotRun},
}

// CallKey names one call of one assistant turn. A pause token names exactly one, but no history
// message carries a pause token, so consumers join on the turn's seq and the call id.
type CallKey struct {
	AssistantSeq int
	ToolCallID   string
}

// Input is what the park round writes. ExecuteArgs are the arguments the accept runs and may hold
// message content: never log them.
type Input struct {
	IdentityID, ConversationID, PauseToken, ToolCallID string
	// AssistantSeq is the seq CommitPause gives the parked assistant turn.
	AssistantSeq, CallIndex                                      int
	ToolName                                                     string
	Source                                                       Source
	ExecuteArgs                                                  json.RawMessage
	Fingerprint, Binding, RanNote, Brief, GrantTool, GrantAction string
}

// Record is a stored parked call. Scope and OperatorID are empty until a decision sets them.
type Record struct {
	ID string
	Input
	Status            Status
	Scope, OperatorID string
	DecidedAt         time.Time
}

// Decision answers a pending record. To is accepted, declined, expired, not_run or cancelled.
type Decision struct {
	PauseToken        string
	To                Status
	Scope, OperatorID string
}

// ErrUnavailable means the record is absent, foreign, malformed or not in the state a move
// needs. Callers must not reveal which.
var ErrUnavailable = errors.New("tool approval unavailable")

// Store is the owner-scoped record store. The *Tx methods run in the caller's identity
// transaction; the others open one through db.WithIdentityTx.
type Store struct{ pool *pgxpool.Pool }

// NewStore binds the store to the aura_app runtime pool.
func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// InsertTx writes one pending record.
func (s *Store) InsertTx(ctx context.Context, q *sqlc.Queries, in Input) error {
	owner, conv, err := ownerConversation(in.IdentityID, in.ConversationID)
	if err != nil {
		return err
	}
	token, err := parseKey(in.PauseToken)
	if err != nil {
		return err
	}
	if err := q.InsertToolApproval(ctx, sqlc.InsertToolApprovalParams{
		IdentityID: owner, ConversationID: conv, PauseToken: token,
		AssistantSeq: int32(in.AssistantSeq), CallIndex: int32(in.CallIndex),
		ToolCallID: in.ToolCallID, ToolName: in.ToolName, Source: string(in.Source),
		ExecuteArgs: in.ExecuteArgs, ExecuteFingerprint: in.Fingerprint,
		ReviewBinding: in.Binding, RanNote: in.RanNote, Brief: in.Brief,
		GrantTool: in.GrantTool, GrantAction: in.GrantAction,
	}); err != nil {
		return fmt.Errorf("insert tool approval %s: %w", in.ToolCallID, err)
	}
	return nil
}

// DecideTx is the one-shot answer to a pending record; a second answer gets ErrUnavailable.
func (s *Store) DecideTx(ctx context.Context, q *sqlc.Queries, d Decision) (Record, error) {
	if !slices.Contains(decisions, d.To) {
		return Record{}, ErrUnavailable
	}
	token, err := parseKey(d.PauseToken)
	if err != nil {
		return Record{}, err
	}
	operator := pgtype.UUID{}
	if d.OperatorID != "" {
		if operator, err = parseKey(d.OperatorID); err != nil {
			return Record{}, err
		}
	}
	row, err := q.DecideToolApproval(ctx, sqlc.DecideToolApprovalParams{
		ToStatus: string(d.To), ApprovalScope: db.OptionalText(d.Scope), OperatorID: operator, PauseToken: token,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Record{}, ErrUnavailable
	}
	if err != nil {
		return Record{}, fmt.Errorf("decide tool approval %s: %w", d.PauseToken, err)
	}
	return recordFromRow(row), nil
}

// Open returns the conversation's pending, accepted and running records, in turn and call order.
func (s *Store) Open(ctx context.Context, ownerID, conversationID string) ([]Record, error) {
	return s.list(ctx, ownerID, conversationID, func(q *sqlc.Queries, owner, conv pgtype.UUID) ([]sqlc.AuraToolApprovals, error) {
		return q.ListOpenToolApprovals(ctx, sqlc.ListOpenToolApprovalsParams{IdentityID: owner, ConversationID: conv})
	})
}

// Decided returns every record of one parked turn, in call order.
func (s *Store) Decided(ctx context.Context, ownerID, conversationID string, assistantSeq int) ([]Record, error) {
	return s.list(ctx, ownerID, conversationID, func(q *sqlc.Queries, owner, conv pgtype.UUID) ([]sqlc.AuraToolApprovals, error) {
		return q.ListToolApprovalsOfTurn(ctx, sqlc.ListToolApprovalsOfTurnParams{
			IdentityID: owner, ConversationID: conv, AssistantSeq: int32(assistantSeq),
		})
	})
}

// OpenCalls keys the conversation's accepted and running records by (assistant_seq, call id):
// the calls whose result is still to come.
func (s *Store) OpenCalls(ctx context.Context, ownerID, conversationID string) ([]CallKey, error) {
	owner, conv, err := ownerConversation(ownerID, conversationID)
	if err != nil {
		return nil, err
	}
	var keys []CallKey
	err = db.WithIdentityTx(ctx, s.pool, ownerID, func(q *sqlc.Queries) error {
		rows, err := q.ListOpenToolApprovalCalls(ctx, sqlc.ListOpenToolApprovalCallsParams{IdentityID: owner, ConversationID: conv})
		for _, row := range rows {
			keys = append(keys, CallKey{AssistantSeq: int(row.AssistantSeq), ToolCallID: row.ToolCallID})
		}
		return err
	})
	return keys, err
}

// StaleAccepted returns the owner's accepted records decided before the cutoff, oldest first.
// They are candidates: the caller keeps only turns with no card left to decide.
func (s *Store) StaleAccepted(ctx context.Context, ownerID string, decidedBefore time.Time) ([]Record, error) {
	owner, err := parseKey(ownerID)
	if err != nil {
		return nil, err
	}
	return s.collect(ctx, ownerID, func(q *sqlc.Queries) ([]sqlc.AuraToolApprovals, error) {
		return q.ListStaleAcceptedToolApprovals(ctx, sqlc.ListStaleAcceptedToolApprovalsParams{
			IdentityID: owner, DecidedAt: pgtype.Timestamptz{Time: decidedBefore, Valid: true},
		})
	})
}

// TransitionTx moves one record from one state to the next, if the state table has that move
// and the record is still in from.
func (s *Store) TransitionTx(ctx context.Context, q *sqlc.Queries, id string, from, to Status) error {
	if !allowed(from, to) {
		return ErrUnavailable
	}
	key, err := parseKey(id)
	if err != nil {
		return err
	}
	n, err := q.TransitionToolApproval(ctx, sqlc.TransitionToolApprovalParams{ID: key, FromStatus: string(from), ToStatus: string(to)})
	if err != nil {
		return fmt.Errorf("transition tool approval %s %s -> %s: %w", id, from, to, err)
	}
	if n == 0 {
		return ErrUnavailable
	}
	return nil
}

// Transition is TransitionTx in an identity transaction of its own.
func (s *Store) Transition(ctx context.Context, ownerID, id string, from, to Status) error {
	if !allowed(from, to) {
		return ErrUnavailable
	}
	if _, err := parseKey(ownerID); err != nil {
		return err
	}
	return db.WithIdentityTx(ctx, s.pool, ownerID, func(q *sqlc.Queries) error {
		return s.TransitionTx(ctx, q, id, from, to)
	})
}

func allowed(from, to Status) bool { return slices.Contains(transitions[from], to) }

// ResultCommitted is whether the record's call already has its tool turn after the parked turn.
// An earlier turn with the same call id answered another round's call.
func (s *Store) ResultCommitted(ctx context.Context, ownerID string, r Record) (bool, error) {
	_, conv, err := ownerConversation(ownerID, r.ConversationID)
	if err != nil {
		return false, err
	}
	var committed bool
	err = db.WithIdentityTx(ctx, s.pool, ownerID, func(q *sqlc.Queries) error {
		found, err := q.HasToolResultAfterSeq(ctx, sqlc.HasToolResultAfterSeqParams{
			ConversationID: conv, ToolCallID: r.ToolCallID, AfterSeq: int32(r.AssistantSeq),
		})
		committed = found
		return err
	})
	return committed, err
}

func (s *Store) list(ctx context.Context, ownerID, conversationID string,
	read func(*sqlc.Queries, pgtype.UUID, pgtype.UUID) ([]sqlc.AuraToolApprovals, error)) ([]Record, error) {
	owner, conv, err := ownerConversation(ownerID, conversationID)
	if err != nil {
		return nil, err
	}
	return s.collect(ctx, ownerID, func(q *sqlc.Queries) ([]sqlc.AuraToolApprovals, error) { return read(q, owner, conv) })
}

func (s *Store) collect(ctx context.Context, ownerID string, read func(*sqlc.Queries) ([]sqlc.AuraToolApprovals, error)) ([]Record, error) {
	var records []Record
	err := db.WithIdentityTx(ctx, s.pool, ownerID, func(q *sqlc.Queries) error {
		rows, err := read(q)
		for _, row := range rows {
			records = append(records, recordFromRow(row))
		}
		return err
	})
	return records, err
}

func ownerConversation(ownerID, conversationID string) (pgtype.UUID, pgtype.UUID, error) {
	owner, err := parseKey(ownerID)
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, err
	}
	conv, err := parseKey(conversationID)
	return owner, conv, err
}

func parseKey(value string) (pgtype.UUID, error) {
	parsed, err := uuid.Parse(value)
	if err != nil {
		return pgtype.UUID{}, ErrUnavailable
	}
	return pgtype.UUID{Bytes: parsed, Valid: true}, nil
}

func recordFromRow(row sqlc.AuraToolApprovals) Record {
	return Record{
		ID:         row.ID.String(),
		IdentityID: row.IdentityID.String(), ConversationID: row.ConversationID.String(),
		PauseToken: row.PauseToken.String(), ToolCallID: row.ToolCallID,
		AssistantSeq: int(row.AssistantSeq), CallIndex: int(row.CallIndex),
		ToolName: row.ToolName, Source: Source(row.Source), ExecuteArgs: json.RawMessage(row.ExecuteArgs),
		Fingerprint: row.ExecuteFingerprint, Binding: row.ReviewBinding, RanNote: row.RanNote,
		Brief: row.Brief, GrantTool: row.GrantTool, GrantAction: row.GrantAction,
		Status: Status(row.Status), Scope: row.ApprovalScope.String,
		OperatorID: row.OperatorID.String(), DecidedAt: row.DecidedAt.Time,
	}
}
