package telegram

import (
	"strings"
	"testing"

	tgmd "github.com/eekstunt/telegramify-markdown-go"
	tele "gopkg.in/telebot.v4"
)

// These tests pin the contract of the atomic tables layer: GFM pipe
// tables are NOT rendered as `pre` (code-block) ASCII grids — that path
// produces unreadable horizontal-scroll blocks on mobile Telegram.
// Instead the layer picks a strategy based on shape:
//
//   2-col KV    → bullet list, key bolded
//   3-4 col cmp → per-row blocks, first cell bolded, other headers italic
//   wide table  → ASCII grid wrapped in expandable_blockquote
//
// If any of these break, mobile UX for table-heavy answers regresses to
// the old "monospace block with horizontal scroll" behaviour.

// --- KV strategy (2-col, ≤20 rows) -----------------------------------------

func TestAtomicTable_TwoColumnRendersAsBulletListWithBoldKeys(t *testing.T) {
	md := "| Name | Age |\n|------|-----|\n| Alice | 30 |\n| Bob | 25 |"
	parts := renderForTelegramEntities(md)
	if len(parts) != 1 {
		t.Fatalf("parts = %d, want 1", len(parts))
	}
	got := parts[0]

	mustContain(t, got.Text, "• Alice")
	mustContain(t, got.Text, "30")
	mustContain(t, got.Text, "• Bob")
	mustContain(t, got.Text, "25")
	mustNotContain(t, got.Text, "|---")
	mustNotContain(t, got.Text, "+---")

	for _, e := range got.Entities {
		if e.Type == tele.EntityCodeBlock {
			t.Fatalf("KV table must not emit pre/code_block entities, got %+v", e)
		}
		if string(e.Type) == "expandable_blockquote" {
			t.Fatalf("KV table must not emit expandable_blockquote, got %+v", e)
		}
	}

	// Both keys must be bolded somewhere.
	bolds := entitiesOfType(got.Entities, tele.EntityBold)
	if len(bolds) != 2 {
		t.Fatalf("bold entities = %d, want 2 (one per key), got %+v", len(bolds), got.Entities)
	}
	mustSpanText(t, got.Text, bolds[0], "Alice")
	mustSpanText(t, got.Text, bolds[1], "Bob")
}

func TestAtomicTable_KVDropsHeaderRow(t *testing.T) {
	// Headers for 2-col tables are almost always uninformative
	// ("Key | Value", "Name | Description"). Pin that we drop them so
	// users don't see noise lines like "• Key: Value".
	md := "| Key | Value |\n|-----|-------|\n| host | localhost |"
	parts := renderForTelegramEntities(md)
	if len(parts) != 1 {
		t.Fatalf("parts = %d", len(parts))
	}
	got := parts[0]
	// Expect a single bullet for the single data row.
	if strings.Count(got.Text, "•") != 1 {
		t.Fatalf("bullet count = %d, want 1; text = %q", strings.Count(got.Text, "•"), got.Text)
	}
	mustContain(t, got.Text, "host")
	mustContain(t, got.Text, "localhost")
}

// --- Comparison strategy (3-4 col, short cells, ≤8 rows) -------------------

func TestAtomicTable_ThreeColComparisonRendersAsPerRowBlocks(t *testing.T) {
	md := "| Name | Role | Years |\n|------|------|-------|\n| Alice | Eng | 5 |\n| Bob | Design | 3 |"
	parts := renderForTelegramEntities(md)
	if len(parts) != 1 {
		t.Fatalf("parts = %d", len(parts))
	}
	got := parts[0]

	mustContain(t, got.Text, "▸ Alice")
	mustContain(t, got.Text, "▸ Bob")
	mustContain(t, got.Text, "Role: Eng")
	mustContain(t, got.Text, "Years: 5")
	mustContain(t, got.Text, "Role: Design")
	mustContain(t, got.Text, "Years: 3")
	mustNotContain(t, got.Text, "|---")

	for _, e := range got.Entities {
		if e.Type == tele.EntityCodeBlock {
			t.Fatalf("comparison table must not emit pre/code_block: %+v", e)
		}
	}

	// Two row-titles bolded (Alice, Bob).
	bolds := entitiesOfType(got.Entities, tele.EntityBold)
	if len(bolds) != 2 {
		t.Fatalf("bold count = %d, want 2; entities = %+v", len(bolds), got.Entities)
	}
	mustSpanText(t, got.Text, bolds[0], "Alice")
	mustSpanText(t, got.Text, bolds[1], "Bob")

	// Italic on the non-first headers (Role, Years) — twice each.
	italics := entitiesOfType(got.Entities, tele.EntityItalic)
	if len(italics) != 4 {
		t.Fatalf("italic count = %d, want 4 (2 headers × 2 rows); entities = %+v", len(italics), got.Entities)
	}
}

// --- Wide strategy (>4 col, or many rows, or long cells) -------------------

func TestAtomicTable_WideTableRendersAsExpandableBlockquote(t *testing.T) {
	// 5 columns trips the cols > 4 threshold → wide.
	md := "| A | B | C | D | E |\n|---|---|---|---|---|\n| 1 | 2 | 3 | 4 | 5 |\n| 6 | 7 | 8 | 9 | 10 |"
	parts := renderForTelegramEntities(md)
	if len(parts) != 1 {
		t.Fatalf("parts = %d", len(parts))
	}
	got := parts[0]

	mustContain(t, got.Text, "| A ")
	mustContain(t, got.Text, "|---")
	mustContain(t, got.Text, "10")

	expandable := 0
	for _, e := range got.Entities {
		if string(e.Type) == "expandable_blockquote" {
			expandable++
			// The blockquote must cover the whole ASCII grid — pin that
			// it starts at the table and ends within the message bounds.
			if e.Length <= 0 {
				t.Fatalf("expandable_blockquote length = %d, want > 0", e.Length)
			}
		}
	}
	if expandable != 1 {
		t.Fatalf("expandable_blockquote count = %d, want 1; entities = %+v", expandable, got.Entities)
	}
}

func TestAtomicTable_LongCellsForceWideShape(t *testing.T) {
	// 2-col but the value cell exceeds the comparison max-cell-width
	// threshold (20 runes). However 2-col always wins KV first → wide
	// only triggers via cols > 2 path. So make this 3-col with a long
	// cell to force wide.
	long := strings.Repeat("x", 40)
	md := "| A | B | C |\n|---|---|---|\n| 1 | 2 | " + long + " |"
	parts := renderForTelegramEntities(md)
	if len(parts) != 1 {
		t.Fatalf("parts = %d", len(parts))
	}
	got := parts[0]
	hasExpandable := false
	for _, e := range got.Entities {
		if string(e.Type) == "expandable_blockquote" {
			hasExpandable = true
		}
	}
	if !hasExpandable {
		t.Fatalf("long-cell 3-col table should fall back to wide/expandable_blockquote; entities = %+v", got.Entities)
	}
}

func TestAtomicTable_ManyRowsForceWideShape(t *testing.T) {
	// 3-col with > maxComparisonRows (8) data rows → wide.
	var b strings.Builder
	b.WriteString("| A | B | C |\n|---|---|---|\n")
	for i := 0; i < 10; i++ {
		b.WriteString("| 1 | 2 | 3 |\n")
	}
	parts := renderForTelegramEntities(b.String())
	if len(parts) != 1 {
		t.Fatalf("parts = %d", len(parts))
	}
	hasExpandable := false
	for _, e := range parts[0].Entities {
		if string(e.Type) == "expandable_blockquote" {
			hasExpandable = true
		}
	}
	if !hasExpandable {
		t.Fatalf("many-row 3-col table should fall back to wide; entities = %+v", parts[0].Entities)
	}
}

// --- Splicing & offsets ----------------------------------------------------

func TestAtomicTable_SurroundingTextPreservedWithCorrectOffsets(t *testing.T) {
	md := "Header above\n\n| Name | Age |\n|------|-----|\n| Alice | 30 |\n\nFooter below"
	parts := renderForTelegramEntities(md)
	if len(parts) != 1 {
		t.Fatalf("parts = %d", len(parts))
	}
	got := parts[0]
	mustContain(t, got.Text, "Header above")
	mustContain(t, got.Text, "Footer below")
	mustContain(t, got.Text, "• Alice")

	// The bold entity on "Alice" must land exactly on the substring.
	bolds := entitiesOfType(got.Entities, tele.EntityBold)
	if len(bolds) != 1 {
		t.Fatalf("bold count = %d, want 1; entities = %+v", len(bolds), got.Entities)
	}
	mustSpanText(t, got.Text, bolds[0], "Alice")
}

func TestAtomicTable_EmojiInSurroundingTextKeepsUTF16Offsets(t *testing.T) {
	// 🌍 is a supplementary char (2 UTF-16 code units). If we mis-shift
	// offsets when splicing, the bold on "Alice" will land off-target.
	md := "Header 🌍 above\n\n| Name | Age |\n|------|-----|\n| Alice | 30 |"
	parts := renderForTelegramEntities(md)
	if len(parts) != 1 {
		t.Fatalf("parts = %d", len(parts))
	}
	got := parts[0]
	bolds := entitiesOfType(got.Entities, tele.EntityBold)
	if len(bolds) != 1 {
		t.Fatalf("bold count = %d, want 1", len(bolds))
	}
	mustSpanText(t, got.Text, bolds[0], "Alice")
}

func TestAtomicTable_EmojiInCellsKeepsUTF16Offsets(t *testing.T) {
	// Emoji INSIDE a cell — the bold on the key must measure its length
	// in UTF-16 code units, not runes.
	md := "| name | who |\n|------|-----|\n| Alice 🌍 | yes |"
	parts := renderForTelegramEntities(md)
	if len(parts) != 1 {
		t.Fatalf("parts = %d", len(parts))
	}
	got := parts[0]
	bolds := entitiesOfType(got.Entities, tele.EntityBold)
	if len(bolds) != 1 {
		t.Fatalf("bold count = %d", len(bolds))
	}
	mustSpanText(t, got.Text, bolds[0], "Alice 🌍")
}

func TestAtomicTable_MultipleTablesInOneMessage(t *testing.T) {
	md := strings.Join([]string{
		"First:",
		"",
		"| A | B |",
		"|---|---|",
		"| 1 | 2 |",
		"",
		"Then second:",
		"",
		"| C | D |",
		"|---|---|",
		"| 3 | 4 |",
	}, "\n")
	parts := renderForTelegramEntities(md)
	if len(parts) != 1 {
		t.Fatalf("parts = %d", len(parts))
	}
	got := parts[0]
	mustContain(t, got.Text, "1")
	mustContain(t, got.Text, "3")
	// Each table is 2-col → bullet list with bold key. We had A=1 and C=3
	// (only one data row each), so two bolds total.
	bolds := entitiesOfType(got.Entities, tele.EntityBold)
	if len(bolds) != 2 {
		t.Fatalf("bold count = %d, want 2 (one per table's row); entities = %+v", len(bolds), got.Entities)
	}
}

func TestAtomicTable_TableAtMessageStartAndEnd(t *testing.T) {
	for _, tc := range []struct {
		name string
		md   string
	}{
		{
			"start",
			"| A | B |\n|---|---|\n| 1 | 2 |\n\nTail",
		},
		{
			"end",
			"Head\n\n| A | B |\n|---|---|\n| 1 | 2 |",
		},
		{
			"only",
			"| A | B |\n|---|---|\n| 1 | 2 |",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parts := renderForTelegramEntities(tc.md)
			if len(parts) != 1 {
				t.Fatalf("parts = %d", len(parts))
			}
			mustContain(t, parts[0].Text, "1")
			// Bold on the key cell ("1") must survive.
			bolds := entitiesOfType(parts[0].Entities, tele.EntityBold)
			if len(bolds) != 1 {
				t.Fatalf("bold count = %d, want 1; entities = %+v", len(bolds), parts[0].Entities)
			}
			mustSpanText(t, parts[0].Text, bolds[0], "1")
		})
	}
}

// --- Non-table markdown isn't accidentally captured -----------------------

func TestAtomicTable_PipeInTextWithoutSeparatorIsNotATable(t *testing.T) {
	md := "Choose: option A | option B | option C"
	parts := renderForTelegramEntities(md)
	if len(parts) != 1 {
		t.Fatalf("parts = %d", len(parts))
	}
	got := parts[0]
	// Pipes should survive as plain text.
	mustContain(t, got.Text, "option A")
	mustContain(t, got.Text, "option C")
	// No bullet, no expandable_blockquote — it's not a table.
	mustNotContain(t, got.Text, "• option")
	for _, e := range got.Entities {
		if string(e.Type) == "expandable_blockquote" {
			t.Fatalf("plain pipes triggered expandable_blockquote: %+v", got.Entities)
		}
	}
}

func TestAtomicTable_HeaderWithoutSeparatorIsNotATable(t *testing.T) {
	md := "| A | B |\n| 1 | 2 |"
	parts := renderForTelegramEntities(md)
	if len(parts) != 1 {
		t.Fatalf("parts = %d", len(parts))
	}
	// Without a separator row this isn't a GFM table. Lines come through
	// however tgmd renders pipe-text (likely as plain paragraphs).
	for _, e := range parts[0].Entities {
		if e.Type == tele.EntityCodeBlock {
			t.Fatalf("header-without-separator should not become a code block: %+v", parts[0].Entities)
		}
		if string(e.Type) == "expandable_blockquote" {
			t.Fatalf("header-without-separator should not become expandable_blockquote: %+v", parts[0].Entities)
		}
	}
}

func TestAtomicTable_TableWithoutBodyRowsIsNotEmitted(t *testing.T) {
	// Header + separator only, no body. Treat as not-a-table so we don't
	// emit an empty bullet block.
	md := "| A | B |\n|---|---|\n"
	parts := renderForTelegramEntities(md)
	// The line above contains pipes but the function must not crash and
	// must not emit a code-block ASCII grid or an expandable_blockquote.
	for _, p := range parts {
		for _, e := range p.Entities {
			if e.Type == tele.EntityCodeBlock {
				t.Fatalf("empty-body table should not emit code_block: %+v", e)
			}
			if string(e.Type) == "expandable_blockquote" {
				t.Fatalf("empty-body table should not emit expandable_blockquote: %+v", e)
			}
		}
	}
}

// --- Sanity: nothing in the regular non-table path regressed --------------

func TestAtomicTable_NonTableMarkdownIsUnchanged(t *testing.T) {
	// Smoke check: bold and italic outside tables still work.
	parts := renderForTelegramEntities("**Bold** and *italic*")
	if len(parts) != 1 {
		t.Fatalf("parts = %d", len(parts))
	}
	got := parts[0]
	if got.Text != "Bold and italic" {
		t.Fatalf("text = %q, want %q", got.Text, "Bold and italic")
	}
	if len(got.Entities) != 2 {
		t.Fatalf("entities = %d, want 2; %+v", len(got.Entities), got.Entities)
	}
}

// --- Helpers ---------------------------------------------------------------

func mustContain(t *testing.T, s, sub string) {
	t.Helper()
	if !strings.Contains(s, sub) {
		t.Fatalf("text missing %q\nfull text: %q", sub, s)
	}
}

func mustNotContain(t *testing.T, s, sub string) {
	t.Helper()
	if strings.Contains(s, sub) {
		t.Fatalf("text unexpectedly contains %q\nfull text: %q", sub, s)
	}
}

func entitiesOfType(entities tele.Entities, typ tele.EntityType) []tele.MessageEntity {
	var out []tele.MessageEntity
	for _, e := range entities {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}

// mustSpanText asserts that an entity's (offset, length) in UTF-16 code
// units lines up exactly with the given substring inside text.
func mustSpanText(t *testing.T, text string, e tele.MessageEntity, want string) {
	t.Helper()
	idx := strings.Index(text, want)
	if idx < 0 {
		t.Fatalf("substring %q not found in %q", want, text)
	}
	wantOffset := tgmd.UTF16Len(text[:idx])
	wantLen := tgmd.UTF16Len(want)
	if e.Offset != wantOffset || e.Length != wantLen {
		t.Fatalf("entity span = (offset=%d, len=%d), want (offset=%d, len=%d) for %q",
			e.Offset, e.Length, wantOffset, wantLen, want)
	}
}
