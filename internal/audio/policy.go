package audio

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Mode represents the per-chat TTS voice mode.
//
// User-facing /voice subcommand mapping:
//
//	/voice on  → ModeAll        (voice in addition to text)
//	/voice tts → ModeVoiceOnly  (voice only, suppress text)
//	/voice off → ModeOff        (text only, default)
type Mode string

const (
	ModeOff       Mode = "off"
	ModeVoiceOnly Mode = "voice_only"
	ModeAll       Mode = "all"
)

// ErrInvalidMode is returned by Parse for unknown mode strings.
var ErrInvalidMode = errors.New("audio: invalid voice mode")

// Parse converts a stored or user-supplied string into a Mode.
func Parse(s string) (Mode, error) {
	switch Mode(s) {
	case ModeOff, ModeVoiceOnly, ModeAll:
		return Mode(s), nil
	default:
		return ModeOff, fmt.Errorf("%w: %q", ErrInvalidMode, s)
	}
}

// ShouldSynth reports whether the mode requires TTS synthesis.
func ShouldSynth(m Mode) bool { return m == ModeVoiceOnly || m == ModeAll }

// ShouldSuppressText reports whether the mode suppresses the text reply.
func ShouldSuppressText(m Mode) bool { return m == ModeVoiceOnly }

// Store manages per-chat voice mode persistence.
type Store interface {
	Get(ctx context.Context, chatID string) (Mode, error)
	Set(ctx context.Context, chatID string, m Mode) error
}

// NoopStore is a Store that always returns ModeOff and discards writes.
// Used when no database is wired (tests, optional feature disabled).
type NoopStore struct{}

func (NoopStore) Get(_ context.Context, _ string) (Mode, error) { return ModeOff, nil }
func (NoopStore) Set(_ context.Context, _ string, _ Mode) error  { return nil }

// SQLiteStore persists voice mode in the chat_settings table (migration v23).
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore creates a SQLiteStore backed by db.
func NewSQLiteStore(db *sql.DB) *SQLiteStore {
	return &SQLiteStore{db: db}
}

// Get returns the voice mode for chatID. A missing row returns ModeOff.
func (s *SQLiteStore) Get(ctx context.Context, chatID string) (Mode, error) {
	var raw string
	err := s.db.QueryRowContext(ctx,
		`SELECT voice_mode FROM chat_settings WHERE chat_id = ?`, chatID,
	).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return ModeOff, nil
	}
	if err != nil {
		return ModeOff, fmt.Errorf("audio: get voice mode for %s: %w", chatID, err)
	}
	return Parse(raw)
}

// Set upserts the voice mode for chatID.
func (s *SQLiteStore) Set(ctx context.Context, chatID string, m Mode) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO chat_settings (chat_id, voice_mode, updated_at) VALUES (?, ?, ?)`,
		chatID, string(m), time.Now().UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("audio: set voice mode for %s: %w", chatID, err)
	}
	return nil
}
