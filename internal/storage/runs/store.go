package runs

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aura/aura/internal/identity"
)

const (
	DefaultSchemaVersion     = 1
	RedactionMetadata        = "metadata"
	EventAuthorizationDenied = "authorization_denied"
	RunOriginUser            = "user"
	RunOriginSubagent        = "subagent"
	RunOriginSourceIngest    = "source_ingest"
	RunOriginScheduler       = "scheduler"
)

type Store struct {
	db  *sql.DB
	now func() time.Time
}

type Run struct {
	ID               string
	ParentRunID      string
	ThreadID         string
	PrincipalID      string
	ActorID          string
	Channel          string
	Status           string
	Model            string
	StartedAt        time.Time
	UpdatedAt        time.Time
	CompletedAt      *time.Time
	CancelledAt      *time.Time
	LastError        string
	CurrentSeq       int64
	IdempotencyKey   string
	CorrelationID    string
	TraceID          string
	SpanID           string
	FinalTextPreview string
	StatsJSON        string
	MetadataJSON     string
}

type Event struct {
	ID             string
	RunID          string
	ParentRunID    string
	Seq            int64
	Type           string
	SchemaVersion  int
	ActorID        string
	CausationID    string
	CorrelationID  string
	IdempotencyKey string
	RunOrigin      string
	PayloadJSON    string
	RedactionLevel string
	CreatedAt      time.Time
}

type OutboxItem struct {
	ID             string
	RunID          string
	EventID        string
	Target         string
	IdempotencyKey string
	PayloadJSON    string
	Status         string
	Attempts       int
	NextAttemptAt  *time.Time
	LastError      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type CreateRunParams struct {
	ID             string
	ParentRunID    string
	ThreadID       string
	PrincipalID    string
	ActorID        string
	Channel        string
	Status         string
	Model          string
	IdempotencyKey string
	CorrelationID  string
	TraceID        string
	SpanID         string
	StartedAt      time.Time
	Metadata       map[string]any
}

type AppendEventParams struct {
	ID               string
	RunID            string
	Type             string
	SchemaVersion    int
	ActorID          string
	CausationID      string
	CorrelationID    string
	IdempotencyKey   string
	RunOrigin        string
	Payload          map[string]any
	PayloadJSON      string
	RedactionLevel   string
	CreatedAt        time.Time
	RunStatus        string
	CompletedAt      *time.Time
	CancelledAt      *time.Time
	LastError        string
	FinalTextPreview string
	Stats            map[string]any
	Metadata         map[string]any
}

type EnqueueOutboxParams struct {
	ID             string
	RunID          string
	EventID        string
	Target         string
	IdempotencyKey string
	Payload        map[string]any
	PayloadJSON    string
	Status         string
	NextAttemptAt  *time.Time
	CreatedAt      time.Time
}

func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("runs: nil db")
	}
	return &Store{
		db:  db,
		now: func() time.Time { return time.Now().UTC() },
	}, nil
}

func (s *Store) CreateOrGetRun(ctx context.Context, params CreateRunParams) (Run, bool, error) {
	if params.IdempotencyKey != "" {
		if runID, ok, err := s.runIDForIdempotencyKey(ctx, "inbound", params.IdempotencyKey); err != nil {
			return Run{}, false, err
		} else if ok {
			run, err := s.GetRun(ctx, runID)
			return run, false, err
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Run{}, false, fmt.Errorf("runs: begin create run: %w", err)
	}
	defer tx.Rollback()

	run, err := s.insertRun(ctx, tx, params)
	if err != nil {
		return Run{}, false, err
	}
	if params.IdempotencyKey != "" {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO run_idempotency_keys (scope, key, run_id, created_at) VALUES (?, ?, ?, ?)`,
			"inbound",
			params.IdempotencyKey,
			run.ID,
			formatTime(run.StartedAt),
		); err != nil {
			return Run{}, false, fmt.Errorf("runs: record inbound idempotency key: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return Run{}, false, fmt.Errorf("runs: commit create run: %w", err)
	}
	return run, true, nil
}

func (s *Store) AppendEvent(ctx context.Context, params AppendEventParams) (Event, error) {
	if params.RunID == "" {
		return Event{}, errors.New("runs: event run id is required")
	}
	if params.Type == "" {
		return Event{}, errors.New("runs: event type is required")
	}
	if params.IdempotencyKey != "" {
		if eventID, ok, err := s.eventIDForIdempotencyKey(ctx, "event", params.IdempotencyKey); err != nil {
			return Event{}, err
		} else if ok {
			return s.GetEvent(ctx, eventID)
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Event{}, fmt.Errorf("runs: begin append event: %w", err)
	}
	defer tx.Rollback()

	now := params.CreatedAt
	if now.IsZero() {
		now = s.now()
	}
	if _, err := tx.ExecContext(ctx, `UPDATE runs SET current_seq = current_seq + 1, updated_at = ? WHERE id = ?`, formatTime(now), params.RunID); err != nil {
		return Event{}, fmt.Errorf("runs: advance run seq: %w", err)
	}

	var run Run
	if err := tx.QueryRowContext(ctx, `
SELECT id, parent_run_id, thread_id, principal_id, actor_id, channel, status, model, started_at, updated_at,
       completed_at, cancelled_at, last_error, current_seq, idempotency_key, correlation_id,
       trace_id, span_id, final_text_preview, stats_json, metadata_json
FROM runs
WHERE id = ?`, params.RunID).Scan(runScanDest(&run)...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Event{}, fmt.Errorf("runs: append event for missing run %q", params.RunID)
		}
		return Event{}, fmt.Errorf("runs: read run after seq advance: %w", err)
	}

	payloadJSON, err := eventPayloadJSON(params)
	if err != nil {
		return Event{}, err
	}
	schemaVersion := params.SchemaVersion
	if schemaVersion == 0 {
		schemaVersion = DefaultSchemaVersion
	}
	redactionLevel := params.RedactionLevel
	if redactionLevel == "" {
		redactionLevel = RedactionMetadata
	}
	event := Event{
		ID:             firstNonEmpty(params.ID, newID("evt")),
		RunID:          params.RunID,
		ParentRunID:    run.ParentRunID,
		Seq:            run.CurrentSeq,
		Type:           params.Type,
		SchemaVersion:  schemaVersion,
		ActorID:        firstNonEmpty(params.ActorID, run.ActorID),
		CausationID:    params.CausationID,
		CorrelationID:  firstNonEmpty(params.CorrelationID, run.CorrelationID),
		IdempotencyKey: params.IdempotencyKey,
		RunOrigin:      deriveRunOrigin(params.RunOrigin, run),
		PayloadJSON:    payloadJSON,
		RedactionLevel: redactionLevel,
		CreatedAt:      now,
	}

	if _, err := tx.ExecContext(ctx, `
INSERT INTO run_events (
  id, run_id, parent_run_id, seq, type, schema_version, actor_id, causation_id,
  correlation_id, idempotency_key, run_origin, payload_json, redaction_level, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.ID,
		event.RunID,
		event.ParentRunID,
		event.Seq,
		event.Type,
		event.SchemaVersion,
		event.ActorID,
		event.CausationID,
		event.CorrelationID,
		event.IdempotencyKey,
		event.RunOrigin,
		event.PayloadJSON,
		event.RedactionLevel,
		formatTime(event.CreatedAt),
	); err != nil {
		return Event{}, fmt.Errorf("runs: insert run event: %w", err)
	}
	if params.IdempotencyKey != "" {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO run_idempotency_keys (scope, key, run_id, event_id, created_at) VALUES (?, ?, ?, ?, ?)`,
			"event",
			params.IdempotencyKey,
			params.RunID,
			event.ID,
			formatTime(now),
		); err != nil {
			return Event{}, fmt.Errorf("runs: record event idempotency key: %w", err)
		}
	}
	if err := s.applySnapshotUpdate(ctx, tx, params, now); err != nil {
		return Event{}, err
	}
	if err := tx.Commit(); err != nil {
		return Event{}, fmt.Errorf("runs: commit append event: %w", err)
	}
	return event, nil
}

func (s *Store) GetRun(ctx context.Context, runID string) (Run, error) {
	var run Run
	if err := s.db.QueryRowContext(ctx, `
SELECT id, parent_run_id, thread_id, principal_id, actor_id, channel, status, model, started_at, updated_at,
       completed_at, cancelled_at, last_error, current_seq, idempotency_key, correlation_id,
       trace_id, span_id, final_text_preview, stats_json, metadata_json
FROM runs
WHERE id = ?`, runID).Scan(runScanDest(&run)...); err != nil {
		return Run{}, fmt.Errorf("runs: get run %q: %w", runID, err)
	}
	return run, nil
}

func (s *Store) GetEvent(ctx context.Context, eventID string) (Event, error) {
	var event Event
	if err := s.db.QueryRowContext(ctx, `
SELECT id, run_id, parent_run_id, seq, type, schema_version, actor_id, causation_id,
       correlation_id, idempotency_key, run_origin, payload_json, redaction_level, created_at
FROM run_events
WHERE id = ?`, eventID).Scan(eventScanDest(&event)...); err != nil {
		return Event{}, fmt.Errorf("runs: get event %q: %w", eventID, err)
	}
	return event, nil
}

func (s *Store) Events(ctx context.Context, runID string) ([]Event, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, run_id, parent_run_id, seq, type, schema_version, actor_id, causation_id,
       correlation_id, idempotency_key, run_origin, payload_json, redaction_level, created_at
FROM run_events
WHERE run_id = ?
ORDER BY seq`, runID)
	if err != nil {
		return nil, fmt.Errorf("runs: query events: %w", err)
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var event Event
		if err := rows.Scan(eventScanDest(&event)...); err != nil {
			return nil, fmt.Errorf("runs: scan event: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("runs: iterate events: %w", err)
	}
	return events, nil
}

func (s *Store) EnqueueOutbox(ctx context.Context, params EnqueueOutboxParams) (OutboxItem, error) {
	if params.RunID == "" {
		return OutboxItem{}, errors.New("runs: outbox run id is required")
	}
	if params.Target == "" {
		return OutboxItem{}, errors.New("runs: outbox target is required")
	}
	if params.IdempotencyKey == "" {
		return OutboxItem{}, errors.New("runs: outbox idempotency key is required")
	}
	now := params.CreatedAt
	if now.IsZero() {
		now = s.now()
	}
	payloadJSON, err := outboxPayloadJSON(params)
	if err != nil {
		return OutboxItem{}, err
	}
	item := OutboxItem{
		ID:             firstNonEmpty(params.ID, newID("out")),
		RunID:          params.RunID,
		EventID:        params.EventID,
		Target:         params.Target,
		IdempotencyKey: params.IdempotencyKey,
		PayloadJSON:    payloadJSON,
		Status:         firstNonEmpty(params.Status, "pending"),
		NextAttemptAt:  params.NextAttemptAt,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if _, err := s.db.ExecContext(ctx, `
INSERT INTO run_outbox (
  id, run_id, event_id, target, idempotency_key, payload_json, status, attempts,
  next_attempt_at, last_error, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?, '', ?, ?)`,
		item.ID,
		item.RunID,
		item.EventID,
		item.Target,
		item.IdempotencyKey,
		item.PayloadJSON,
		item.Status,
		formatOptionalTime(item.NextAttemptAt),
		formatTime(item.CreatedAt),
		formatTime(item.UpdatedAt),
	); err != nil {
		return OutboxItem{}, fmt.Errorf("runs: enqueue outbox: %w", err)
	}
	return item, nil
}

func (s *Store) RecordAuthorizationDenial(ctx context.Context, decision identity.AuthorizationDecision) error {
	if decision.Decision != identity.DecisionDeny {
		return nil
	}
	if decision.RunID == "" {
		return errors.New("runs: authorization denial run id is required")
	}
	if decision.ActorID == "" {
		return errors.New("runs: authorization denial actor id is required")
	}
	if decision.Capability == "" {
		return errors.New("runs: authorization denial capability is required")
	}
	payload := authorizationDenialPayload(decision)
	event, err := s.AppendEvent(ctx, AppendEventParams{
		RunID:          decision.RunID,
		Type:           EventAuthorizationDenied,
		ActorID:        decision.ActorID,
		CausationID:    decision.EventID,
		IdempotencyKey: "authorization_denied:" + decision.ID,
		Payload:        payload,
		RedactionLevel: RedactionMetadata,
		CreatedAt:      decision.CreatedAt,
	})
	if err != nil {
		return fmt.Errorf("runs: record authorization denial run event: %w", err)
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("runs: marshal authorization denial audit payload: %w", err)
	}
	createdAt := decision.CreatedAt
	if createdAt.IsZero() {
		createdAt = event.CreatedAt
	}
	if _, err := s.db.ExecContext(ctx, `
INSERT INTO audit_events (
  id, run_id, event_id, type, actor_id, target_type, target_id,
  payload_json, redaction_level, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO NOTHING
`,
		authorizationDenialAuditID(decision.ID),
		decision.RunID,
		event.ID,
		EventAuthorizationDenied,
		decision.ActorID,
		"authorization",
		decision.ID,
		string(payloadJSON),
		RedactionMetadata,
		formatTime(createdAt),
	); err != nil {
		return fmt.Errorf("runs: record authorization denial audit event: %w", err)
	}
	return nil
}

func (s *Store) runIDForIdempotencyKey(ctx context.Context, scope, key string) (string, bool, error) {
	var runID string
	err := s.db.QueryRowContext(ctx, `SELECT run_id FROM run_idempotency_keys WHERE scope = ? AND key = ?`, scope, key).Scan(&runID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("runs: lookup idempotency key: %w", err)
	}
	return runID, true, nil
}

func (s *Store) eventIDForIdempotencyKey(ctx context.Context, scope, key string) (string, bool, error) {
	var eventID string
	err := s.db.QueryRowContext(ctx, `SELECT event_id FROM run_idempotency_keys WHERE scope = ? AND key = ?`, scope, key).Scan(&eventID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("runs: lookup event idempotency key: %w", err)
	}
	return eventID, eventID != "", nil
}

func (s *Store) insertRun(ctx context.Context, tx *sql.Tx, params CreateRunParams) (Run, error) {
	if params.Channel == "" {
		return Run{}, errors.New("runs: channel is required")
	}
	if params.Status == "" {
		return Run{}, errors.New("runs: status is required")
	}
	startedAt := params.StartedAt
	if startedAt.IsZero() {
		startedAt = s.now()
	}
	metadataJSON, err := encodeJSON(params.Metadata, "{}")
	if err != nil {
		return Run{}, fmt.Errorf("runs: encode run metadata: %w", err)
	}
	run := Run{
		ID:             firstNonEmpty(params.ID, newID("run")),
		ParentRunID:    params.ParentRunID,
		ThreadID:       params.ThreadID,
		PrincipalID:    params.PrincipalID,
		ActorID:        params.ActorID,
		Channel:        params.Channel,
		Status:         params.Status,
		Model:          params.Model,
		StartedAt:      startedAt,
		UpdatedAt:      startedAt,
		IdempotencyKey: params.IdempotencyKey,
		CorrelationID:  params.CorrelationID,
		TraceID:        params.TraceID,
		SpanID:         params.SpanID,
		StatsJSON:      "{}",
		MetadataJSON:   metadataJSON,
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO runs (
  id, parent_run_id, thread_id, principal_id, actor_id, channel, status, model, started_at,
  updated_at, current_seq, idempotency_key, correlation_id, trace_id, span_id,
  stats_json, metadata_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?, ?, ?, ?)`,
		run.ID,
		run.ParentRunID,
		run.ThreadID,
		run.PrincipalID,
		run.ActorID,
		run.Channel,
		run.Status,
		run.Model,
		formatTime(run.StartedAt),
		formatTime(run.UpdatedAt),
		run.IdempotencyKey,
		run.CorrelationID,
		run.TraceID,
		run.SpanID,
		run.StatsJSON,
		run.MetadataJSON,
	); err != nil {
		return Run{}, fmt.Errorf("runs: insert run: %w", err)
	}
	return run, nil
}

func (s *Store) applySnapshotUpdate(ctx context.Context, tx *sql.Tx, params AppendEventParams, now time.Time) error {
	sets := []string{"updated_at = ?"}
	args := []any{formatTime(now)}
	if params.RunStatus != "" {
		sets = append(sets, "status = ?")
		args = append(args, params.RunStatus)
	}
	if params.CompletedAt != nil {
		sets = append(sets, "completed_at = ?")
		args = append(args, formatTime(*params.CompletedAt))
	}
	if params.CancelledAt != nil {
		sets = append(sets, "cancelled_at = ?")
		args = append(args, formatTime(*params.CancelledAt))
	}
	if params.LastError != "" {
		sets = append(sets, "last_error = ?")
		args = append(args, params.LastError)
	}
	if params.FinalTextPreview != "" {
		sets = append(sets, "final_text_preview = ?")
		args = append(args, params.FinalTextPreview)
	}
	if params.Stats != nil {
		statsJSON, err := encodeJSON(params.Stats, "{}")
		if err != nil {
			return fmt.Errorf("runs: encode stats: %w", err)
		}
		sets = append(sets, "stats_json = ?")
		args = append(args, statsJSON)
	}
	if params.Metadata != nil {
		metadataJSON, err := encodeJSON(params.Metadata, "{}")
		if err != nil {
			return fmt.Errorf("runs: encode metadata: %w", err)
		}
		sets = append(sets, "metadata_json = ?")
		args = append(args, metadataJSON)
	}
	args = append(args, params.RunID)
	query := "UPDATE runs SET " + joinSQL(sets, ", ") + " WHERE id = ?"
	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("runs: update run snapshot: %w", err)
	}
	return nil
}

func runScanDest(run *Run) []any {
	var startedAt, updatedAt string
	var completedAt, cancelledAt sql.NullString
	return []any{
		&run.ID,
		&run.ParentRunID,
		&run.ThreadID,
		&run.PrincipalID,
		&run.ActorID,
		&run.Channel,
		&run.Status,
		&run.Model,
		scanTime(&run.StartedAt, &startedAt),
		scanTime(&run.UpdatedAt, &updatedAt),
		scanOptionalTime(&run.CompletedAt, &completedAt),
		scanOptionalTime(&run.CancelledAt, &cancelledAt),
		&run.LastError,
		&run.CurrentSeq,
		&run.IdempotencyKey,
		&run.CorrelationID,
		&run.TraceID,
		&run.SpanID,
		&run.FinalTextPreview,
		&run.StatsJSON,
		&run.MetadataJSON,
	}
}

func eventScanDest(event *Event) []any {
	var createdAt string
	return []any{
		&event.ID,
		&event.RunID,
		&event.ParentRunID,
		&event.Seq,
		&event.Type,
		&event.SchemaVersion,
		&event.ActorID,
		&event.CausationID,
		&event.CorrelationID,
		&event.IdempotencyKey,
		&event.RunOrigin,
		&event.PayloadJSON,
		&event.RedactionLevel,
		scanTime(&event.CreatedAt, &createdAt),
	}
}

type scannerFunc func(any) error

func (f scannerFunc) Scan(value any) error { return f(value) }

func scanTime(dst *time.Time, raw *string) sql.Scanner {
	return scannerFunc(func(value any) error {
		if err := scanString(raw, value); err != nil {
			return err
		}
		parsed, err := time.Parse(time.RFC3339Nano, *raw)
		if err != nil {
			parsed, err = time.Parse(time.RFC3339, *raw)
		}
		if err != nil {
			return err
		}
		*dst = parsed.UTC()
		return nil
	})
}

func scanOptionalTime(dst **time.Time, raw *sql.NullString) sql.Scanner {
	return scannerFunc(func(value any) error {
		if value == nil {
			*raw = sql.NullString{}
			*dst = nil
			return nil
		}
		if err := raw.Scan(value); err != nil {
			return err
		}
		if !raw.Valid || raw.String == "" {
			*dst = nil
			return nil
		}
		parsed, err := time.Parse(time.RFC3339Nano, raw.String)
		if err != nil {
			parsed, err = time.Parse(time.RFC3339, raw.String)
		}
		if err != nil {
			return err
		}
		parsed = parsed.UTC()
		*dst = &parsed
		return nil
	})
}

func scanString(dst *string, value any) error {
	switch v := value.(type) {
	case string:
		*dst = v
	case []byte:
		*dst = string(v)
	default:
		return fmt.Errorf("unsupported time value %T", value)
	}
	return nil
}

func eventPayloadJSON(params AppendEventParams) (string, error) {
	if params.PayloadJSON != "" {
		return params.PayloadJSON, nil
	}
	return encodeJSON(params.Payload, "{}")
}

func outboxPayloadJSON(params EnqueueOutboxParams) (string, error) {
	if params.PayloadJSON != "" {
		return params.PayloadJSON, nil
	}
	return encodeJSON(params.Payload, "{}")
}

func authorizationDenialPayload(decision identity.AuthorizationDecision) map[string]any {
	payload := map[string]any{
		"decision_id":     decision.ID,
		"actor_id":        decision.ActorID,
		"capability":      string(decision.Capability),
		"resource_type":   decision.Resource.Type,
		"resource_id":     decision.Resource.ID,
		"decision":        string(decision.Decision),
		"reason":          decision.Reason,
		"run_id":          decision.RunID,
		"redaction_level": RedactionMetadata,
	}
	if decision.EventID != "" {
		payload["causation_event_id"] = decision.EventID
	}
	return payload
}

func authorizationDenialAuditID(decisionID string) string {
	if decisionID == "" {
		return newID("audit")
	}
	return "audit_" + decisionID
}

func encodeJSON(value map[string]any, fallback string) (string, error) {
	if value == nil {
		return fallback, nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func formatTime(ts time.Time) string {
	return ts.UTC().Format(time.RFC3339Nano)
}

func formatOptionalTime(ts *time.Time) any {
	if ts == nil {
		return nil
	}
	return formatTime(*ts)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func deriveRunOrigin(explicit string, run Run) string {
	origin := strings.TrimSpace(explicit)
	switch origin {
	case RunOriginUser, RunOriginSubagent, RunOriginSourceIngest, RunOriginScheduler:
		return origin
	}
	if strings.TrimSpace(run.ParentRunID) != "" {
		return RunOriginSubagent
	}
	switch strings.TrimSpace(run.Channel) {
	case "swarm":
		return RunOriginSubagent
	case "cron", "heartbeat", "scheduler":
		return RunOriginScheduler
	case "source_ingest":
		return RunOriginSourceIngest
	default:
		return RunOriginUser
	}
}

func newID(prefix string) string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return prefix + "_0000000000000000"
	}
	return prefix + "_" + hex.EncodeToString(buf[:])
}

func joinSQL(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for _, part := range parts[1:] {
		out += sep + part
	}
	return out
}
