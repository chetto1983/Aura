package messagedrafts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
)

// Status is the durable disposition of one outbound call. Dispatching is never
// returned to pending after a crash: external delivery may have happened.
type Status string

// The statuses encode the one-way draft lifecycle persisted in aura.message_drafts.
const (
	StatusPending     Status = "pending"
	StatusDispatching Status = "dispatching"
	StatusSent        Status = "sent"
	StatusFailed      Status = "failed"
	StatusDeclined    Status = "declined"
	StatusUncertain   Status = "uncertain"
	StatusExpired     Status = "expired"
)

// ErrUnavailable means the draft is absent, foreign, expired, or no longer
// pending for the requested transition. The API must not reveal which.
var (
	ErrUnavailable = errors.New("message draft unavailable")
	ErrDuplicate   = errors.New("message draft call conflicts with an existing draft")
)

// DraftInput is written only by the bridge-owned, trusted send interception.
// OriginalArgs may contain message content and must never be logged.
type DraftInput struct {
	IdentityID         string
	ConversationID     string
	ToolCallID         string
	Target             Target
	RegisteredToolName string
	OriginalArgs       json.RawMessage
	ExpiresAt          time.Time
}

// Draft is a server-side record. API responses must build a separate owner-only
// review DTO and must never JSON-encode this struct directly.
type Draft struct {
	ID                   string
	IdentityID           string
	ConversationID       string
	ToolCallID           string
	Target               Target
	RegisteredToolName   string
	OriginalArgs         json.RawMessage
	OriginalFingerprint  string
	EffectiveArgs        json.RawMessage
	EffectiveFingerprint string
	Status               Status
	OutcomeCode          string
	ExpiresAt            time.Time
	DispatchStartedAt    time.Time
	ResolvedAt           time.Time
	CreatedAt            time.Time
}

// Store is an owner-scoped PostgreSQL store. Every query runs with the RLS
// identity set in the transaction, in addition to an explicit identity filter.
type Store struct{ pool *pgxpool.Pool }

// NewStore binds the draft store to the aura_app runtime pool.
func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Create inserts one pending draft per owner/conversation/tool call. An exact
// duplicate returns the existing row; a different original call is rejected.
func (s *Store) Create(ctx context.Context, input DraftInput) (Draft, error) {
	if strings.TrimSpace(input.ConversationID) == "" || len(input.ConversationID) > 128 ||
		strings.TrimSpace(input.ToolCallID) == "" || len(input.ToolCallID) > 128 ||
		!input.ExpiresAt.After(time.Now()) || input.ExpiresAt.After(time.Now().Add(24*time.Hour)) ||
		!strings.HasSuffix(input.RegisteredToolName, "__"+input.Target.Tool) ||
		len(input.RegisteredToolName) <= len(input.Target.Tool)+2 || len(input.RegisteredToolName) > 256 {
		return Draft{}, errInvalidDraftArgs
	}
	owner, err := db.ParseUUID("owner id", input.IdentityID)
	if err != nil {
		return Draft{}, errInvalidDraftArgs
	}
	original, err := MergeApprovedArgs(input.Target, input.OriginalArgs, nil)
	if err != nil {
		return Draft{}, err
	}
	fingerprint := argsFingerprint(input.Target, original)
	var result Draft
	err = db.WithIdentityTx(ctx, s.pool, input.IdentityID, func(q *sqlc.Queries) error {
		row, insertErr := q.InsertMessageDraft(ctx, sqlc.InsertMessageDraftParams{
			IdentityID: owner, ConversationID: input.ConversationID, ToolCallID: input.ToolCallID,
			Recipe: input.Target.Recipe, ToolName: input.Target.Tool, RegisteredToolName: input.RegisteredToolName, Action: input.Target.Action,
			OriginalArgs: original, OriginalFingerprint: fingerprint,
			ExpiresAt: pgtype.Timestamptz{Time: input.ExpiresAt, Valid: true},
		})
		if errors.Is(insertErr, pgx.ErrNoRows) {
			row, insertErr = q.GetMessageDraftByCall(ctx, sqlc.GetMessageDraftByCallParams{
				IdentityID: owner, ConversationID: input.ConversationID, ToolCallID: input.ToolCallID,
			})
			if insertErr == nil && (row.OriginalFingerprint != fingerprint || row.Recipe != input.Target.Recipe || row.ToolName != input.Target.Tool || row.RegisteredToolName != input.RegisteredToolName || row.Action != input.Target.Action) {
				return ErrDuplicate
			}
		}
		if insertErr != nil {
			return insertErr
		}
		result, insertErr = draftFromRow(row)
		return insertErr
	})
	return result, err
}

// Get returns a draft only to its owner. Foreign IDs and missing IDs share the
// same unavailable response so the API cannot be used as an existence oracle.
func (s *Store) Get(ctx context.Context, ownerID, draftID string) (Draft, error) {
	owner, id, err := draftKey(ownerID, draftID)
	if err != nil {
		return Draft{}, ErrUnavailable
	}
	var result Draft
	err = db.WithIdentityTx(ctx, s.pool, ownerID, func(q *sqlc.Queries) error {
		row, readErr := q.GetMessageDraftForIdentity(ctx, sqlc.GetMessageDraftForIdentityParams{IdentityID: owner, ID: id})
		if readErr != nil {
			return readErr
		}
		result, readErr = draftFromRow(row)
		return readErr
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Draft{}, ErrUnavailable
	}
	return result, err
}

// ListPending returns only unexpired pending drafts in one owner's conversation.
func (s *Store) ListPending(ctx context.Context, ownerID, conversationID string) ([]Draft, error) {
	owner, err := db.ParseUUID("owner id", ownerID)
	if err != nil || strings.TrimSpace(conversationID) == "" {
		return nil, ErrUnavailable
	}
	var drafts []Draft
	err = db.WithIdentityTx(ctx, s.pool, ownerID, func(q *sqlc.Queries) error {
		rows, readErr := q.ListPendingMessageDrafts(ctx, sqlc.ListPendingMessageDraftsParams{
			IdentityID: owner, ConversationID: conversationID,
		})
		if readErr != nil {
			return readErr
		}
		drafts = make([]Draft, 0, len(rows))
		for _, row := range rows {
			draft, mapErr := draftFromRow(row)
			if mapErr != nil {
				return mapErr
			}
			drafts = append(drafts, draft)
		}
		return nil
	})
	return drafts, err
}

// ClaimSend validates edits against the saved original, then atomically takes
// the only pending -> dispatching transition. A competing caller gets no claim.
func (s *Store) ClaimSend(ctx context.Context, ownerID, draftID string, overrides json.RawMessage) (Draft, error) {
	owner, id, err := draftKey(ownerID, draftID)
	if err != nil {
		return Draft{}, ErrUnavailable
	}
	var claimed Draft
	err = db.WithIdentityTx(ctx, s.pool, ownerID, func(q *sqlc.Queries) error {
		row, readErr := q.GetMessageDraftForIdentity(ctx, sqlc.GetMessageDraftForIdentityParams{IdentityID: owner, ID: id})
		if errors.Is(readErr, pgx.ErrNoRows) {
			return ErrUnavailable
		}
		if readErr != nil {
			return readErr
		}
		if row.Status != string(StatusPending) || !row.ExpiresAt.Valid || !row.ExpiresAt.Time.After(time.Now()) {
			return ErrUnavailable
		}
		target := Target{Recipe: row.Recipe, Tool: row.ToolName, Action: row.Action}
		effective, mergeErr := MergeApprovedArgs(target, row.OriginalArgs, overrides)
		if mergeErr != nil {
			return mergeErr
		}
		fp := argsFingerprint(target, effective)
		row, readErr = q.ClaimMessageDraftSend(ctx, sqlc.ClaimMessageDraftSendParams{
			ID: id, IdentityID: owner, EffectiveArgs: effective,
			EffectiveFingerprint: pgtype.Text{String: fp, Valid: true},
		})
		if errors.Is(readErr, pgx.ErrNoRows) {
			return ErrUnavailable
		}
		if readErr != nil {
			return readErr
		}
		claimed, readErr = draftFromRow(row)
		return readErr
	})
	return claimed, err
}

// Decline consumes one pending draft without entering the transport path.
func (s *Store) Decline(ctx context.Context, ownerID, draftID string) (Draft, error) {
	owner, id, err := draftKey(ownerID, draftID)
	if err != nil {
		return Draft{}, ErrUnavailable
	}
	var result Draft
	err = db.WithIdentityTx(ctx, s.pool, ownerID, func(q *sqlc.Queries) error {
		row, declineErr := q.DeclineMessageDraft(ctx, sqlc.DeclineMessageDraftParams{ID: id, IdentityID: owner})
		if errors.Is(declineErr, pgx.ErrNoRows) {
			return ErrUnavailable
		}
		if declineErr != nil {
			return declineErr
		}
		result, declineErr = draftFromRow(row)
		return declineErr
	})
	return result, err
}

// MarkOutcome records the real transport result. It never starts a dispatch.
func (s *Store) MarkOutcome(ctx context.Context, ownerID, draftID string, status Status, code string) (Draft, error) {
	if status != StatusSent && status != StatusFailed && status != StatusUncertain {
		return Draft{}, errInvalidDraftArgs
	}
	if len(code) > 80 || strings.IndexFunc(code, func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_'
	}) >= 0 {
		return Draft{}, errInvalidDraftArgs
	}
	owner, id, err := draftKey(ownerID, draftID)
	if err != nil {
		return Draft{}, ErrUnavailable
	}
	var result Draft
	err = db.WithIdentityTx(ctx, s.pool, ownerID, func(q *sqlc.Queries) error {
		row, updateErr := q.MarkMessageDraftOutcome(ctx, sqlc.MarkMessageDraftOutcomeParams{
			ID: id, IdentityID: owner, Status: string(status),
			OutcomeCode: pgtype.Text{String: code, Valid: code != ""},
		})
		if errors.Is(updateErr, pgx.ErrNoRows) {
			return ErrUnavailable
		}
		if updateErr != nil {
			return updateErr
		}
		result, updateErr = draftFromRow(row)
		return updateErr
	})
	return result, err
}

func draftKey(ownerID, draftID string) (pgtype.UUID, pgtype.UUID, error) {
	owner, err := db.ParseUUID("owner id", ownerID)
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, err
	}
	id, err := db.ParseUUID("draft id", draftID)
	return owner, id, err
}

func argsFingerprint(target Target, raw json.RawMessage) string {
	bound, _ := json.Marshal(struct {
		Recipe string          `json:"recipe"`
		Tool   string          `json:"tool"`
		Action string          `json:"action"`
		Args   json.RawMessage `json:"args"`
	}{Recipe: target.Recipe, Tool: target.Tool, Action: target.Action, Args: raw})
	sum := sha256.Sum256(bound)
	return hex.EncodeToString(sum[:])
}

func draftFromRow(row sqlc.AuraMessageDrafts) (Draft, error) {
	if !row.ID.Valid || !row.IdentityID.Valid || !row.ExpiresAt.Valid || !row.CreatedAt.Valid {
		return Draft{}, errors.New("message draft row is incomplete")
	}
	draft := Draft{
		ID: uuid.UUID(row.ID.Bytes).String(), IdentityID: uuid.UUID(row.IdentityID.Bytes).String(),
		ConversationID: row.ConversationID, ToolCallID: row.ToolCallID,
		Target:             Target{Recipe: row.Recipe, Tool: row.ToolName, Action: row.Action},
		RegisteredToolName: row.RegisteredToolName,
		OriginalArgs:       json.RawMessage(row.OriginalArgs), OriginalFingerprint: row.OriginalFingerprint,
		EffectiveArgs: json.RawMessage(row.EffectiveArgs), Status: Status(row.Status),
		ExpiresAt: row.ExpiresAt.Time, CreatedAt: row.CreatedAt.Time,
	}
	if row.EffectiveFingerprint.Valid {
		draft.EffectiveFingerprint = row.EffectiveFingerprint.String
	}
	if row.OutcomeCode.Valid {
		draft.OutcomeCode = row.OutcomeCode.String
	}
	if row.DispatchStartedAt.Valid {
		draft.DispatchStartedAt = row.DispatchStartedAt.Time
	}
	if row.ResolvedAt.Valid {
		draft.ResolvedAt = row.ResolvedAt.Time
	}
	return draft, nil
}
