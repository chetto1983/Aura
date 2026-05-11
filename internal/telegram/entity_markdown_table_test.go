package telegram

import (
	"strings"
	"testing"

	tgmd "github.com/eekstunt/telegramify-markdown-go"
	tele "gopkg.in/telebot.v4"
)

// telegramify-markdown-go renders GFM tables as a single `pre` (code-block)
// entity containing an ASCII-rendered table. Inline cell formatting is lost
// by design — what reaches Telegram is monospace plain text. These tests
// pin that contract so a future library upgrade can't silently break tables.

func TestRenderForTelegramEntitiesTableBecomesSingleCodeBlock(t *testing.T) {
	md := "| Name | Age |\n|------|-----|\n| Alice | 30 |\n| Bob | 25 |"
	parts := renderForTelegramEntities(md)
	if len(parts) != 1 {
		t.Fatalf("parts = %d, want 1", len(parts))
	}
	got := parts[0]

	if !strings.Contains(got.Text, "Alice") || !strings.Contains(got.Text, "30") {
		t.Fatalf("text missing data cells: %q", got.Text)
	}
	if !strings.Contains(got.Text, "Bob") || !strings.Contains(got.Text, "25") {
		t.Fatalf("text missing second data row: %q", got.Text)
	}

	if len(got.Entities) != 1 {
		t.Fatalf("entities = %d, want 1 (single pre covering the table); got %+v", len(got.Entities), got.Entities)
	}
	e := got.Entities[0]
	if e.Type != tele.EntityCodeBlock {
		t.Fatalf("entity type = %s, want %s", e.Type, tele.EntityCodeBlock)
	}
	if e.Language != "" {
		t.Fatalf("language = %q, want empty (tables have no language)", e.Language)
	}
	if e.Offset != 0 {
		t.Fatalf("offset = %d, want 0 (table is the only content)", e.Offset)
	}
	if e.Length != tgmd.UTF16Len(got.Text) {
		t.Fatalf("length = %d, want %d (entity should cover full text)", e.Length, tgmd.UTF16Len(got.Text))
	}
}

func TestRenderForTelegramEntitiesTableHeaderSeparatorUsesPlusAtColumnBreaks(t *testing.T) {
	md := "| A | B | C |\n|---|---|---|\n| 1 | 2 | 3 |"
	parts := renderForTelegramEntities(md)
	if len(parts) != 1 {
		t.Fatalf("parts = %d, want 1", len(parts))
	}
	// Library renders the separator between header and body using `+` at
	// column intersections, e.g. "|---+---+---|". This is a visual contract
	// users may rely on (alignment in monospace).
	lines := strings.Split(parts[0].Text, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines, got %d: %q", len(lines), parts[0].Text)
	}
	sep := lines[1]
	if !strings.HasPrefix(sep, "|-") || !strings.HasSuffix(sep, "-|") {
		t.Fatalf("separator = %q, want pipe-bounded dashes", sep)
	}
	if strings.Count(sep, "+") != 2 {
		t.Fatalf("separator = %q, want exactly 2 column breaks (`+`) for a 3-col table", sep)
	}
}

func TestRenderForTelegramEntitiesTableColumnsAreUniformWidth(t *testing.T) {
	// Cells of differing widths should be padded so every row has the same
	// rendered character length. Pinning this prevents a regression where
	// the library stops padding and Telegram users see a ragged grid.
	md := "| short | x |\n|-------|---|\n| longer-cell | y |"
	parts := renderForTelegramEntities(md)
	if len(parts) != 1 {
		t.Fatalf("parts = %d, want 1", len(parts))
	}
	lines := strings.Split(parts[0].Text, "\n")
	if len(lines) != 3 {
		t.Fatalf("lines = %d, want 3 (header + sep + 1 body row): %q", len(lines), parts[0].Text)
	}
	headerLen := len([]rune(lines[0]))
	for i, ln := range lines {
		if len([]rune(ln)) != headerLen {
			t.Fatalf("line %d width = %d, want %d (uniform): %q", i, len([]rune(ln)), headerLen, ln)
		}
	}
}

func TestRenderForTelegramEntitiesTableInlineFormattingIsStripped(t *testing.T) {
	// GFM tables in this renderer flatten cells to plain text — bold, code,
	// and link markup vanish inside cells. Documenting this so callers know
	// not to rely on per-cell entities; if they need rich formatting, use a
	// list instead of a table.
	md := "| col1 | col2 |\n|------|------|\n| **bold** | `code` |\n| [link](https://example.com) | plain |"
	parts := renderForTelegramEntities(md)
	if len(parts) != 1 {
		t.Fatalf("parts = %d, want 1", len(parts))
	}
	got := parts[0]

	if strings.Contains(got.Text, "**") || strings.Contains(got.Text, "`") || strings.Contains(got.Text, "[link]") {
		t.Fatalf("expected markdown syntax to be stripped from cells, got %q", got.Text)
	}
	if !strings.Contains(got.Text, "bold") || !strings.Contains(got.Text, "code") || !strings.Contains(got.Text, "link") {
		t.Fatalf("expected plain cell text to survive, got %q", got.Text)
	}

	if len(got.Entities) != 1 {
		t.Fatalf("entities = %d, want 1 (only the outer pre block; inline formatting is lost): %+v", len(got.Entities), got.Entities)
	}
	if got.Entities[0].Type != tele.EntityCodeBlock {
		t.Fatalf("entity type = %s, want %s", got.Entities[0].Type, tele.EntityCodeBlock)
	}
}

func TestRenderForTelegramEntitiesTableWithSurroundingTextHasCorrectUTF16Offset(t *testing.T) {
	// Pre entity offset must be measured in UTF-16 units relative to the
	// final rendered text — including any emoji preceding the table. Tests
	// the integration with telegramify's UTF-16 offset computation, which
	// is the part Telegram cares about most.
	md := "Header 🌍 above\n\n| A | B |\n|---|---|\n| 1 | 2 |\n\nFooter"
	parts := renderForTelegramEntities(md)
	if len(parts) != 1 {
		t.Fatalf("parts = %d, want 1", len(parts))
	}
	got := parts[0]

	pres := preEntities(got.Entities)
	if len(pres) != 1 {
		t.Fatalf("pre entities = %d, want 1; all entities: %+v", len(pres), got.Entities)
	}
	pre := pres[0]

	// Locate the table inside the rendered text and compute its expected
	// UTF-16 offset directly. If the library's offset doesn't match this,
	// Telegram will format the wrong span.
	tableStart := strings.Index(got.Text, "| A")
	if tableStart < 0 {
		t.Fatalf("rendered table not found in text: %q", got.Text)
	}
	wantOffset := tgmd.UTF16Len(got.Text[:tableStart])
	if pre.Offset != wantOffset {
		t.Fatalf("pre offset = %d, want %d (UTF-16 prefix length of %q)", pre.Offset, wantOffset, got.Text[:tableStart])
	}

	tableEnd := strings.Index(got.Text[tableStart:], "\n\n")
	if tableEnd < 0 {
		tableEnd = len(got.Text) - tableStart
	}
	wantLen := tgmd.UTF16Len(got.Text[tableStart : tableStart+tableEnd])
	if pre.Length != wantLen {
		t.Fatalf("pre length = %d, want %d (UTF-16 length of table substring)", pre.Length, wantLen)
	}

	if !strings.Contains(got.Text, "Header 🌍 above") {
		t.Fatalf("prefix text lost: %q", got.Text)
	}
	if !strings.Contains(got.Text, "Footer") {
		t.Fatalf("suffix text lost: %q", got.Text)
	}
}

func preEntities(entities tele.Entities) []tele.MessageEntity {
	var out []tele.MessageEntity
	for _, e := range entities {
		if e.Type == tele.EntityCodeBlock {
			out = append(out, e)
		}
	}
	return out
}
