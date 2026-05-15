package telegram

import (
	"context"
	"testing"
	"time"

	"github.com/aura/aura/internal/conversation"
)

func TestArchiveAppenderForTurnKeepsBufferedWriterWhenMemoryCaptureInactive(t *testing.T) {
	buffered := &archiveNoopClosingArchiver{}
	store := &archiveRecordingRepository{}
	bot := &Bot{
		rt: &botRuntime{archiver: buffered, archiveDB: store},
	}

	got := bot.archiveAppenderForTurn()
	if got != buffered {
		t.Fatalf("archiveAppenderForTurn() = %T, want buffered archiver", got)
	}
}

// archiveNoopClosingArchiver is a noop ClosingTurnAppender used in bot archive tests.
type archiveNoopClosingArchiver struct{}

func (n *archiveNoopClosingArchiver) Append(context.Context, conversation.Turn) error { return nil }
func (n *archiveNoopClosingArchiver) Close(context.Context) error                     { return nil }

// archiveRecordingRepository implements conversation.ArchiveRepository for tests.
type archiveRecordingRepository struct{}

func (r *archiveRecordingRepository) Append(context.Context, conversation.Turn) error { return nil }
func (r *archiveRecordingRepository) ListByChat(context.Context, int64, int) ([]conversation.Turn, error) {
	return nil, nil
}
func (r *archiveRecordingRepository) ListAll(context.Context, int) ([]conversation.Turn, error) {
	return nil, nil
}
func (r *archiveRecordingRepository) Get(context.Context, int64) (conversation.Turn, error) {
	return conversation.Turn{}, nil
}
func (r *archiveRecordingRepository) MaxTurnIndex(context.Context, int64) (int64, error) {
	return -1, nil
}
func (r *archiveRecordingRepository) Stats(context.Context) (conversation.ArchiveStats, error) {
	return conversation.ArchiveStats{}, nil
}
func (r *archiveRecordingRepository) DeleteByChat(context.Context, int64) (int64, error) {
	return 0, nil
}
func (r *archiveRecordingRepository) DeleteOlderThan(context.Context, time.Time) (int64, error) {
	return 0, nil
}
func (r *archiveRecordingRepository) DeleteAll(context.Context) (int64, error) {
	return 0, nil
}
