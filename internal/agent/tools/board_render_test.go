package tools

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/board"
)

var renderNow = time.Date(2026, 11, 2, 6, 30, 0, 0, time.UTC)

func dueIn(d time.Duration) *time.Time {
	at := renderNow.Add(d)
	return &at
}

// The due section's window is 24 hours, both edges, and a card it lists is not listed again
// in its column.
func TestBoardDueSectionWindowAndOneListingPerCard(t *testing.T) {
	cards := []board.Card{
		{ID: "late", ColumnID: "todo", Label: "late", Priority: 2, DueAt: dueIn(-time.Hour)},
		{ID: "plain", ColumnID: "todo", Label: "plain", Priority: 2},
		{ID: "inside", ColumnID: "doing", Label: "inside", Priority: 2, DueAt: dueIn(23*time.Hour + 30*time.Minute)},
		{ID: "outside", ColumnID: "done", Label: "outside", Priority: 2, DueAt: dueIn(24*time.Hour + 30*time.Minute)},
	}
	out, shown := renderBoard(board.Board{Columns: board.DefaultColumns()}, cards, "", renderNow)
	section, _, _ := strings.Cut(out, "## To do")
	if !strings.Contains(section, "[late]") || !strings.Contains(section, "[inside]") || strings.Contains(section, "[outside]") {
		t.Errorf("due section = %q, want late and inside, not outside", section)
	}
	if !strings.Contains(section, "(now 2026-11-02T06:30:00Z)") {
		t.Errorf("due section does not state the instant it compared against: %q", section)
	}
	for _, id := range []string{"[late]", "[inside]", "[outside]", "[plain]"} {
		if n := strings.Count(out, id); n != 1 {
			t.Errorf("%s listed %d times in\n%s", id, n, out)
		}
	}
	if len(shown) != 4 || strings.Contains(out, "not shown") {
		t.Errorf("shown %d cards, want all 4 and no hidden count:\n%s", len(shown), out)
	}
}

func TestBoardWithNothingDueHasNoDueSection(t *testing.T) {
	cards := []board.Card{{ID: "a", ColumnID: "todo", Label: "a", Priority: 2, DueAt: dueIn(48 * time.Hour)}}
	out, _ := renderBoard(board.Board{Columns: board.DefaultColumns()}, cards, "", renderNow)
	if strings.Contains(out, "## Due") {
		t.Errorf("a board with nothing due grew a due section:\n%s", out)
	}
}

func TestBoardDueSectionKeepsTheCap(t *testing.T) {
	var cards []board.Card
	for i := range 40 {
		cards = append(cards, board.Card{ID: fmt.Sprintf("d%02d", i), ColumnID: "todo", Label: "due", Priority: 2, DueAt: dueIn(-time.Duration(i) * time.Minute)})
	}
	out, shown := renderBoard(board.Board{Columns: board.DefaultColumns()}, cards, "", renderNow)
	if len(shown) != boardSnapshotCards || strings.Count(out, "- [d") != boardSnapshotCards {
		t.Errorf("shown %d, listed %d, want the cap %d", len(shown), strings.Count(out, "- [d"), boardSnapshotCards)
	}
	if !strings.Contains(out, "(8 more card(s) not shown") {
		t.Errorf("want 8 more not shown:\n%s", out)
	}
	if !strings.Contains(out, "[d39]") || strings.Contains(out, "[d00]") {
		t.Error("the earliest deadlines come first")
	}
}

func TestBoardColumnLimitsAreNamed(t *testing.T) {
	b := board.Board{Columns: []board.Column{
		{ID: "todo", Label: "To do", CardLimit: 2},
		{ID: "doing", Label: "Doing", CardLimit: 1},
		{ID: "done", Label: "Done"},
	}}
	cards := []board.Card{
		{ID: "a", ColumnID: "todo", Label: "a", Priority: 2}, {ID: "b", ColumnID: "todo", Label: "b", Priority: 2},
		{ID: "c", ColumnID: "doing", Label: "c", Priority: 2}, {ID: "d", ColumnID: "doing", Label: "d", Priority: 2},
	}
	out, _ := renderBoard(b, cards, "", renderNow)
	for _, want := range []string{
		"## To do (todo): 2 card(s), limit 2\n",
		"## Doing (doing): 2 card(s), limit 1, over the limit\n",
		"## Done (done): 0 card(s)\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
}

func TestBoardCardLineCarriesItsMeta(t *testing.T) {
	due := time.Date(2026, 11, 3, 9, 0, 0, 0, time.FixedZone("CET", 3600))
	label160 := strings.Repeat("y", boardSnapshotRunes)
	for name, tc := range map[string]struct {
		card       board.Card
		withColumn bool
		want       string
	}{
		"no meta":          {board.Card{ID: "a", Label: "plain", Priority: 2}, false, "- [a] plain\n"},
		"all meta":         {board.Card{ID: "b", ColumnID: "doing", Label: "full", Priority: 3, DueAt: &due, Tags: []string{"x", "y"}}, true, "- [b] full (in doing, high priority, due 2026-11-03T08:00:00Z, #x #y)\n"},
		"low priority":     {board.Card{ID: "c", Label: "low", Priority: 1}, false, "- [c] low (low priority)\n"},
		"description":      {board.Card{ID: "d", Label: "d", Priority: 2, Description: "line one\nline two"}, false, "- [d] d\n  line one line two\n"},
		"label at the cap": {board.Card{ID: "e", Label: label160, Priority: 2}, false, "- [e] " + label160 + "\n"},
	} {
		var out strings.Builder
		writeCard(&out, tc.card, tc.withColumn)
		if out.String() != tc.want {
			t.Errorf("%s: got %q, want %q", name, out.String(), tc.want)
		}
	}
}

func TestBoardColumnListingCountsOnlyThatColumn(t *testing.T) {
	var cards []board.Card
	for i := range 34 {
		cards = append(cards, board.Card{ID: fmt.Sprintf("t%02d", i), ColumnID: "todo", Label: "t", Priority: 2})
	}
	cards = append(cards, board.Card{ID: "x", ColumnID: "done", Label: "x", Priority: 2})
	out, _ := renderBoard(board.Board{Columns: board.DefaultColumns()}, cards, "todo", renderNow)
	if !strings.Contains(out, "(2 more card(s) not shown") {
		t.Errorf("34 in todo, 32 shown: want 2 more, got\n%s", out)
	}
	out, _ = renderBoard(board.Board{Columns: board.DefaultColumns()}, cards, "doing", renderNow)
	if strings.Contains(out, "not shown") {
		t.Errorf("an empty column has nothing hidden:\n%s", out)
	}
}

func TestBoardSearchAddUpdateAndDeleteSayWhatHappened(t *testing.T) {
	store := newFakeBoard()
	store.cards = []board.Card{
		{ID: "a", ColumnID: "todo", Label: "invoice one", Priority: 2},
		{ID: "b", ColumnID: "doing", Label: "invoice two", Priority: 2},
	}
	store.gone = map[string]bool{"b": true}
	tool := &BoardTool{Store: store}
	ctx := boardCtx(boardConv)

	out, err := runBoard(t, tool, ctx, `{"action":"search","query":"invoice"}`)
	if err != nil || !strings.Contains(out, "2 card(s) match invoice:\n- [a] invoice one (in todo)\n- [b] invoice two (in doing)\n") {
		t.Fatalf("search = %q, %v", out, err)
	}
	out, err = runBoard(t, tool, ctx, `{"action":"add","label":"call","description":"the bank","due":"2026-11-03T09:00:00Z"}`)
	if err != nil || !strings.HasPrefix(out, "added:\n- [c1] call (in todo, due 2026-11-03T09:00:00Z)\n") {
		t.Fatalf("add = %q, %v", out, err)
	}
	if got := store.added[0]; got.Description != "the bank" || got.DueAt == nil || !got.DueAt.Equal(time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("added %+v, want the description and the due date", got)
	}
	out, err = runBoard(t, tool, ctx, `{"action":"update","id":"a","label":"invoice paid"}`)
	if err != nil || out != "updated:\n- [a] invoice paid (in todo)\n" {
		t.Errorf("update = %q, %v", out, err)
	}
	if out, err = runBoard(t, tool, ctx, `{"action":"delete","id":"b"}`); err != nil || out != "card b was already gone" {
		t.Errorf("delete of a gone card = %q, %v", out, err)
	}
}
