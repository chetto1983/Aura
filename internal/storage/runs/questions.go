package runs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const (
	QuestionStatusWaiting  = "waiting"
	QuestionStatusAnswered = "answered"
)

var (
	ErrQuestionNotFound        = errors.New("runs: question not found")
	ErrQuestionNotWaiting      = errors.New("runs: question is not waiting")
	ErrQuestionChannelMismatch = errors.New("runs: question channel mismatch")
)

type Question struct {
	ID            string
	RunID         string
	EventID       string
	AnswerRunID   string
	AnswerEventID string
	ThreadID      string
	ActorID       string
	Channel       string
	Kind          string
	Status        string
	QuestionText  string
	OptionsJSON   string
	AnswerPreview string
	AnswerJSON    string
	RequestedAt   time.Time
	AnsweredAt    *time.Time
	ExpiresAt     *time.Time
	ProducerJSON  string
	MetadataJSON  string
}

type RecordQuestionRequestedParams struct {
	ID           string
	RunID        string
	EventID      string
	ThreadID     string
	ActorID      string
	Channel      string
	Kind         string
	QuestionText string
	Options      []string
	RequestedAt  time.Time
	ExpiresAt    *time.Time
	Producer     map[string]any
	Metadata     map[string]any
}

type RecordQuestionAnsweredParams struct {
	ID                string
	AnswerRunID       string
	AnswerEventID     string
	ThreadID          string
	Channel           string
	ActorID           string
	SelectedOptionIDs []string
	FreeText          string
	AnsweredMessageID string
	AnsweredAt        time.Time
	Metadata          map[string]any
}

func (s *Store) RecordQuestionRequested(ctx context.Context, params RecordQuestionRequestedParams) (Question, error) {
	if params.RunID == "" {
		return Question{}, errors.New("runs: question run id is required")
	}
	if params.EventID == "" {
		return Question{}, errors.New("runs: question event id is required")
	}
	if params.ThreadID == "" {
		return Question{}, errors.New("runs: question thread id is required")
	}
	if params.Channel == "" {
		return Question{}, errors.New("runs: question channel is required")
	}
	if params.QuestionText == "" {
		return Question{}, errors.New("runs: question text is required")
	}
	id := firstNonEmpty(params.ID, params.EventID)
	kind := firstNonEmpty(params.Kind, "clarification")
	requestedAt := params.RequestedAt
	if requestedAt.IsZero() {
		requestedAt = s.now()
	}
	optionsJSON, err := encodeStringSlice(params.Options)
	if err != nil {
		return Question{}, fmt.Errorf("runs: encode question options: %w", err)
	}
	producerJSON, err := encodeJSON(params.Producer, "{}")
	if err != nil {
		return Question{}, fmt.Errorf("runs: encode question producer: %w", err)
	}
	metadataJSON, err := encodeJSON(params.Metadata, "{}")
	if err != nil {
		return Question{}, fmt.Errorf("runs: encode question metadata: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `
INSERT INTO chat_questions (
  id, run_id, event_id, thread_id, actor_id, channel, kind, status,
  question_text, options_json, requested_at, expires_at, producer_json, metadata_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  run_id = excluded.run_id,
  event_id = excluded.event_id,
  thread_id = excluded.thread_id,
  actor_id = excluded.actor_id,
  channel = excluded.channel,
  kind = excluded.kind,
  question_text = excluded.question_text,
  options_json = excluded.options_json,
  requested_at = excluded.requested_at,
  expires_at = excluded.expires_at,
  producer_json = excluded.producer_json,
  metadata_json = excluded.metadata_json
`,
		id,
		params.RunID,
		params.EventID,
		params.ThreadID,
		params.ActorID,
		params.Channel,
		kind,
		QuestionStatusWaiting,
		params.QuestionText,
		optionsJSON,
		formatTime(requestedAt),
		formatOptionalTime(params.ExpiresAt),
		producerJSON,
		metadataJSON,
	); err != nil {
		return Question{}, fmt.Errorf("runs: record question requested: %w", err)
	}
	return s.GetQuestion(ctx, id)
}

func (s *Store) RecordQuestionAnswered(ctx context.Context, params RecordQuestionAnsweredParams) (Question, error) {
	if params.ID == "" {
		return Question{}, errors.New("runs: answer question id is required")
	}
	if params.AnswerRunID == "" {
		return Question{}, errors.New("runs: answer run id is required")
	}
	if params.AnswerEventID == "" {
		return Question{}, errors.New("runs: answer event id is required")
	}
	if params.Channel == "" {
		return Question{}, errors.New("runs: answer channel is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Question{}, fmt.Errorf("runs: begin answer question: %w", err)
	}
	defer tx.Rollback()

	q, err := s.getQuestionTx(ctx, tx, params.ID)
	if err != nil {
		return Question{}, err
	}
	if q.Channel != params.Channel {
		return Question{}, ErrQuestionChannelMismatch
	}
	if q.Status != QuestionStatusWaiting {
		return Question{}, ErrQuestionNotWaiting
	}

	answeredAt := params.AnsweredAt
	if answeredAt.IsZero() {
		answeredAt = s.now()
	}
	answerJSON, err := encodeJSON(map[string]any{
		"selected_option_ids": append([]string(nil), params.SelectedOptionIDs...),
		"has_free_text":       params.FreeText != "",
		"answered_message_id": params.AnsweredMessageID,
	}, "{}")
	if err != nil {
		return Question{}, fmt.Errorf("runs: encode question answer: %w", err)
	}
	metadataJSON := q.MetadataJSON
	if params.Metadata != nil {
		metadataJSON, err = encodeJSON(params.Metadata, "{}")
		if err != nil {
			return Question{}, fmt.Errorf("runs: encode question answer metadata: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE chat_questions
SET status = ?, answer_run_id = ?, answer_event_id = ?, actor_id = ?,
    answer_preview = ?, answer_json = ?, answered_at = ?, metadata_json = ?
WHERE id = ?`,
		QuestionStatusAnswered,
		params.AnswerRunID,
		params.AnswerEventID,
		firstNonEmpty(params.ActorID, q.ActorID),
		questionTextPreview(params.FreeText),
		answerJSON,
		formatTime(answeredAt),
		metadataJSON,
		params.ID,
	); err != nil {
		return Question{}, fmt.Errorf("runs: update question answer: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Question{}, fmt.Errorf("runs: commit answer question: %w", err)
	}
	return s.GetQuestion(ctx, params.ID)
}

func (s *Store) LatestPendingQuestion(ctx context.Context, threadID, channel string) (Question, bool, error) {
	if threadID == "" || channel == "" {
		return Question{}, false, nil
	}
	var q Question
	err := s.db.QueryRowContext(ctx, `
SELECT id, run_id, event_id, answer_run_id, answer_event_id, thread_id, actor_id,
       channel, kind, status, question_text, options_json, answer_preview,
       answer_json, requested_at, answered_at, expires_at, producer_json,
       metadata_json
FROM chat_questions
WHERE thread_id = ? AND channel = ? AND status = ?
ORDER BY requested_at DESC, id DESC
LIMIT 1`, threadID, channel, QuestionStatusWaiting).Scan(questionScanDest(&q)...)
	if errors.Is(err, sql.ErrNoRows) {
		return Question{}, false, nil
	}
	if err != nil {
		return Question{}, false, fmt.Errorf("runs: latest pending question: %w", err)
	}
	return q, true, nil
}

func (s *Store) GetQuestion(ctx context.Context, id string) (Question, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Question{}, fmt.Errorf("runs: begin get question: %w", err)
	}
	defer tx.Rollback()
	return s.getQuestionTx(ctx, tx, id)
}

func (s *Store) getQuestionTx(ctx context.Context, tx *sql.Tx, id string) (Question, error) {
	var q Question
	err := tx.QueryRowContext(ctx, `
SELECT id, run_id, event_id, answer_run_id, answer_event_id, thread_id, actor_id,
       channel, kind, status, question_text, options_json, answer_preview,
       answer_json, requested_at, answered_at, expires_at, producer_json,
       metadata_json
FROM chat_questions
WHERE id = ?`, id).Scan(questionScanDest(&q)...)
	if errors.Is(err, sql.ErrNoRows) {
		return Question{}, ErrQuestionNotFound
	}
	if err != nil {
		return Question{}, fmt.Errorf("runs: get question %q: %w", id, err)
	}
	return q, nil
}

func questionScanDest(q *Question) []any {
	var requestedAt string
	var answeredAt, expiresAt sql.NullString
	return []any{
		&q.ID,
		&q.RunID,
		&q.EventID,
		&q.AnswerRunID,
		&q.AnswerEventID,
		&q.ThreadID,
		&q.ActorID,
		&q.Channel,
		&q.Kind,
		&q.Status,
		&q.QuestionText,
		&q.OptionsJSON,
		&q.AnswerPreview,
		&q.AnswerJSON,
		scanTime(&q.RequestedAt, &requestedAt),
		scanOptionalTime(&q.AnsweredAt, &answeredAt),
		scanOptionalTime(&q.ExpiresAt, &expiresAt),
		&q.ProducerJSON,
		&q.MetadataJSON,
	}
}

func encodeStringSlice(values []string) (string, error) {
	if values == nil {
		values = []string{}
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func questionTextPreview(text string) string {
	runes := []rune(text)
	if len(runes) > 280 {
		runes = runes[:280]
	}
	return string(runes)
}
