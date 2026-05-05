package telegram

import (
	"strings"
	"testing"

	tele "gopkg.in/telebot.v4"
)

func TestRenderForTelegramEntitiesProducesPlainTextAndEntities(t *testing.T) {
	parts := renderForTelegramEntities("**Bold** and *italic*")
	if len(parts) != 1 {
		t.Fatalf("parts = %d, want 1", len(parts))
	}
	got := parts[0]
	if got.Text != "Bold and italic" {
		t.Fatalf("text = %q, want %q", got.Text, "Bold and italic")
	}
	if len(got.Entities) != 2 {
		t.Fatalf("entities = %+v, want 2", got.Entities)
	}
	assertEntity(t, got.Entities[0], tele.EntityBold, 0, 4)
	assertEntity(t, got.Entities[1], tele.EntityItalic, 9, 6)
}

func TestRenderForTelegramEntitiesKeepsCodePlainAndFormatted(t *testing.T) {
	parts := renderForTelegramEntities("```go\nfmt.Println(\"<hi>\")\n```")
	if len(parts) != 1 {
		t.Fatalf("parts = %d, want 1", len(parts))
	}
	got := parts[0]
	if got.Text != "fmt.Println(\"<hi>\")" {
		t.Fatalf("text = %q", got.Text)
	}
	if len(got.Entities) != 1 {
		t.Fatalf("entities = %+v, want 1", got.Entities)
	}
	assertEntity(t, got.Entities[0], tele.EntityCodeBlock, 0, len(got.Text))
	if got.Entities[0].Language != "go" {
		t.Fatalf("language = %q, want go", got.Entities[0].Language)
	}
}

func TestRenderForTelegramEntitiesSplitsLongMessages(t *testing.T) {
	parts := renderForTelegramEntities(strings.Repeat("a", 4200))
	if len(parts) < 2 {
		t.Fatalf("parts = %d, want at least 2", len(parts))
	}
	for i, part := range parts {
		if len(part.Text) > telegramMaxMessageUTF16 {
			t.Fatalf("part %d length = %d, want <= %d", i, len(part.Text), telegramMaxMessageUTF16)
		}
	}
}

func assertEntity(t *testing.T, got tele.MessageEntity, typ tele.EntityType, offset, length int) {
	t.Helper()
	if got.Type != typ || got.Offset != offset || got.Length != length {
		t.Fatalf("entity = %+v, want type=%s offset=%d length=%d", got, typ, offset, length)
	}
}
