package runs

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
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

// --- scan helpers ---

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

// --- utility helpers ---

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

