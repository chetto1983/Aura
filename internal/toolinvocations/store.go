package toolinvocations

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// EventStart and EventEnd label append-only tool invocation lifecycle rows.
const (
	EventStart = "start"
	EventEnd   = "end"
)

// Event is one append-only fact in aura.tool_invocations. Start and end events
// are correlated by ConversationID + RequestID + ToolCallID.
type Event struct {
	ID                string
	ConversationID    string
	RequestID         string
	ToolCallID        string
	ToolName          string
	Event             string
	Seq               int
	Timestamp         time.Time
	StartedAt         time.Time
	EndedAt           time.Time
	DurationMS        int64
	Arguments         string
	ArgsBytes         int
	Status            string
	Error             string
	ResultPreview     string
	PreviewBytes      int
	ResultBytes       int
	ResultTruncated   bool
	ResultSidecarPath string
	ExitCode          *int
	Meta              map[string]any
}

// Store is the append-only tool invocation ledger wrapper over sqlc.
type Store struct {
	q *sqlc.Queries
}

// New builds a tool invocation store backed by the provided Postgres pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{q: sqlc.New(pool)}
}

// Insert appends one tool invocation event to the ledger.
func (s *Store) Insert(ctx context.Context, e Event) error {
	p, err := e.toParams()
	if err != nil {
		return fmt.Errorf("insert tool invocation: %w", err)
	}
	if err := s.q.InsertToolInvocation(ctx, p); err != nil {
		return fmt.Errorf("insert tool invocation %s/%s/%s: %w", e.ConversationID, e.RequestID, e.ToolCallID, err)
	}
	return nil
}

// ListByConversation returns all tool invocation events for one conversation.
func (s *Store) ListByConversation(ctx context.Context, conversationID string) ([]Event, error) {
	u, err := uuidParam("conversation_id", conversationID)
	if err != nil {
		return nil, fmt.Errorf("list tool invocations: %w", err)
	}
	rows, err := s.q.ListToolInvocationsByConversation(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("list tool invocations %s: %w", conversationID, err)
	}
	out := make([]Event, 0, len(rows))
	for _, r := range rows {
		out = append(out, eventFromRow(r))
	}
	return out, nil
}

func (e Event) toParams() (sqlc.InsertToolInvocationParams, error) {
	conv, err := uuidParam("conversation_id", e.ConversationID)
	if err != nil {
		return sqlc.InsertToolInvocationParams{}, err
	}
	req, err := uuidParam("request_id", e.RequestID)
	if err != nil {
		return sqlc.InsertToolInvocationParams{}, err
	}
	ts := e.Timestamp
	if ts.IsZero() {
		ts = e.StartedAt
	}
	if ts.IsZero() {
		ts = e.EndedAt
	}
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	meta, err := jsonBytes(e.Meta)
	if err != nil {
		return sqlc.InsertToolInvocationParams{}, err
	}
	// The event-shape CHECK requires started_at NOT NULL for a start row. Default it to
	// the resolved ts (IN-01) so a future emitter that forgets to set StartedAt on a
	// start event degrades to the row's own timestamp instead of failing the CHECK.
	startedAt := e.StartedAt
	if e.Event == EventStart && startedAt.IsZero() {
		startedAt = ts
	}
	return sqlc.InsertToolInvocationParams{
		ConversationID: conv,
		RequestID:      req,
		ToolCallID:     e.ToolCallID,
		ToolName:       e.ToolName,
		EventKind:      e.Event,
		Seq:            clampInt32(e.Seq),
		Ts:             timestamptz(ts),
		StartedAt:      timestamptz(startedAt),
		EndedAt:        timestamptz(e.EndedAt),
		DurationMs:     int8OrNull(e.DurationMS, e.Event == EventEnd),
		// DELIBERATE full-forensic capture (WR-02): ArgsRaw/ResultPreview persist the
		// VERBATIM tool argument JSON + result preview — including any secret a model
		// placed on a shell_exec/sandbox_exec command line (e.g. an inline
		// `Authorization: Bearer ...`). This is the chosen forensic posture for a
		// single-operator host: capture everything for audit. SECURITY IMPLICATION: the
		// ledger is append-only (migration 0011 triggers reject DELETE) and aura_app has
		// SELECT, so any captured secret is durable and un-deletable, removed only by a
		// conversation FK-cascade. Redaction/capping is a tracked follow-up (WR-02,
		// .planning/STATE.md) — do NOT add a silent capture path without that design.
		ArgsRaw:           textOrNull(e.Arguments, e.Arguments != "" || e.ArgsBytes > 0),
		ArgsBytes:         int4OrNull(e.ArgsBytes, e.Arguments != "" || e.ArgsBytes > 0),
		Status:            textOrNull(e.Status, e.Status != ""),
		Error:             textOrNull(e.Error, e.Error != ""),
		ResultPreview:     textOrNull(e.ResultPreview, e.Event == EventEnd),
		PreviewBytes:      int4OrNull(e.PreviewBytes, e.Event == EventEnd),
		ResultBytes:       int4OrNull(e.ResultBytes, e.Event == EventEnd),
		ResultTruncated:   boolOrNull(e.ResultTruncated, e.Event == EventEnd),
		ResultSidecarPath: textOrNull(e.ResultSidecarPath, e.ResultSidecarPath != ""),
		ExitCode:          exitCodeOrNull(e.ExitCode),
		Meta:              meta,
	}, nil
}

func uuidParam(name, s string) (pgtype.UUID, error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("%s %q: %w", name, s, err)
	}
	return pgtype.UUID{Bytes: u, Valid: true}, nil
}

func timestamptz(t time.Time) pgtype.Timestamptz {
	if t.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: t.UTC(), Valid: true}
}

func textOrNull(s string, valid bool) pgtype.Text {
	return pgtype.Text{String: s, Valid: valid}
}

func int4OrNull(n int, valid bool) pgtype.Int4 {
	return pgtype.Int4{Int32: clampInt32(n), Valid: valid}
}

// clampInt32 saturates an int to the int32 range so a >2 GiB args/result byte count
// (or a pathological seq) never overflows into a wrong/negative forensic count in the
// ledger. The byte fields are token-bounded in practice, so saturation is the honest
// degradation: the count maxes out rather than wrapping.
func clampInt32(n int) int32 {
	if n > math.MaxInt32 {
		return math.MaxInt32
	}
	if n < math.MinInt32 {
		return math.MinInt32
	}
	return int32(n)
}

func int8OrNull(n int64, valid bool) pgtype.Int8 {
	return pgtype.Int8{Int64: n, Valid: valid}
}

func boolOrNull(b bool, valid bool) pgtype.Bool {
	return pgtype.Bool{Bool: b, Valid: valid}
}

func exitCodeOrNull(exitCode *int) pgtype.Int4 {
	if exitCode == nil {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: int32(*exitCode), Valid: true}
}

func jsonBytes(m map[string]any) ([]byte, error) {
	if len(m) == 0 {
		return nil, nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("meta json: %w", err)
	}
	return b, nil
}

func eventFromRow(r sqlc.AuraToolInvocations) Event {
	out := Event{
		ID:                uuid.UUID(r.ID.Bytes).String(),
		ConversationID:    uuid.UUID(r.ConversationID.Bytes).String(),
		RequestID:         uuid.UUID(r.RequestID.Bytes).String(),
		ToolCallID:        r.ToolCallID,
		ToolName:          r.ToolName,
		Event:             r.EventKind,
		Seq:               int(r.Seq),
		Timestamp:         r.Ts.Time,
		StartedAt:         r.StartedAt.Time,
		EndedAt:           r.EndedAt.Time,
		DurationMS:        r.DurationMs.Int64,
		Arguments:         r.ArgsRaw.String,
		ArgsBytes:         int(r.ArgsBytes.Int32),
		Status:            r.Status.String,
		Error:             r.Error.String,
		ResultPreview:     r.ResultPreview.String,
		PreviewBytes:      int(r.PreviewBytes.Int32),
		ResultBytes:       int(r.ResultBytes.Int32),
		ResultTruncated:   r.ResultTruncated.Bool,
		ResultSidecarPath: r.ResultSidecarPath.String,
	}
	if r.ExitCode.Valid {
		ec := int(r.ExitCode.Int32)
		out.ExitCode = &ec
	}
	if len(r.Meta) > 0 {
		if err := json.Unmarshal(r.Meta, &out.Meta); err != nil {
			// An append-only forensic ledger should not silently drop a corrupt meta
			// jsonb (IN-02): log it. out.Meta stays nil/partial, the rest of the row reads.
			slog.Warn("tool invocation meta unmarshal failed", "id", out.ID, "err", err)
		}
	}
	return out
}
