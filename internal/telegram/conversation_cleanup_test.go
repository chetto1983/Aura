package telegram

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"

	tele "gopkg.in/telebot.v4"
)

func TestLogPlaceholderDeleteFailure(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	logPlaceholderDeleteFailure(logger, "123", &tele.Message{ID: 99}, errors.New("telegram delete failed"))

	got := logs.String()
	for _, want := range []string{
		"telegram cleanup: placeholder delete failed",
		"user_id=123",
		"message_id=99",
		`error="telegram delete failed"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("log %q does not contain %q", got, want)
		}
	}
}
