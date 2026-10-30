package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/board"
)

// fakeBoard is an in-memory boardStore: one board, cards in insertion order per column.
type fakeBoard struct {
	b       board.Board
	cards   []board.Card
	next    int
	added   []board.NewCard
	sources []board.Source
	deleted []string
	// writes counts every call that writes, Board among them: the real one ensures the board.
	writes  int
	readErr error
}

func newFakeBoard() *fakeBoard {
	return &fakeBoard{b: board.Board{ID: "b", Name: "main", Columns: board.DefaultColumns()}}
}

func (f *fakeBoard) Board(context.Context, string) (board.Board, error) {
	f.writes++
	return f.b, nil
}

func (f *fakeBoard) Cards(context.Context, string) ([]board.Card, error) { return f.cards, nil }

func (f *fakeBoard) CardOnBoard(_ context.Context, _, id string) (board.Card, board.Board, error) {
	if f.readErr != nil {
		return board.Card{}, board.Board{}, f.readErr
	}
	for _, c := range f.cards {
		if c.ID == id {
			return c, f.b, nil
		}
	}
	return board.Card{}, board.Board{}, board.ErrNotFound
}

func (f *fakeBoard) Search(_ context.Context, _, q string, _ int) ([]board.Card, error) {
	var out []board.Card
	for _, c := range f.cards {
		if strings.Contains(c.Label, q) {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeBoard) AddCard(_ context.Context, _ string, in board.NewCard, src board.Source, _ board.Actor) (board.Card, error) {
	f.writes++
	f.next++
	col := in.Column
	if col == "" {
		col = "todo"
	}
	c := board.Card{ID: fmt.Sprintf("c%d", f.next), ColumnID: col, Label: in.Label, Priority: max(in.Priority, 2), Tags: in.Tags, DueAt: in.DueAt, Source: src}
	f.cards = append(f.cards, c)
	f.added, f.sources = append(f.added, in), append(f.sources, src)
	return c, nil
}

func (f *fakeBoard) UpdateCard(_ context.Context, _, id string, p board.CardPatch, _ board.Actor) (board.Card, error) {
	f.writes++
	for i, c := range f.cards {
		if c.ID == id {
			if p.Label != nil {
				f.cards[i].Label = *p.Label
			}
			if p.DueAt != nil {
				f.cards[i].DueAt = *p.DueAt
			}
			return f.cards[i], nil
		}
	}
	return board.Card{}, board.ErrNotFound
}

func (f *fakeBoard) MoveCard(_ context.Context, _, id, col, _ string, _ board.Actor) (board.Card, error) {
	f.writes++
	for i, c := range f.cards {
		if c.ID == id {
			f.cards[i].ColumnID = col
			return f.cards[i], nil
		}
	}
	return board.Card{}, board.ErrNotFound
}

func (f *fakeBoard) DeleteCard(_ context.Context, _, id string) (bool, error) {
	f.writes++
	f.deleted = append(f.deleted, id)
	return true, nil
}

const boardConv = "3b241101-e2bb-4255-8caf-4136c566a962"

func boardCtx(session string) context.Context {
	return WithToolCallContext(context.Background(), session, "tc", "", 8192)
}

func runBoard(t *testing.T, tool *BoardTool, ctx context.Context, args string) (string, error) {
	t.Helper()
	res, err := tool.Execute(ctx, json.RawMessage(args))
	return res.Preview, err
}

func TestBoardAddRecordsOriginFromTheSession(t *testing.T) {
	store := newFakeBoard()
	tool := &BoardTool{Store: store}
	out, err := runBoard(t, tool, boardCtx(boardConv), `{"action":"add","label":"call the supplier","priority":3,"tags":["ops"],"due":"2026-10-10T09:00:00Z"}`)
	if err != nil || !strings.Contains(out, "call the supplier") || !strings.Contains(out, "high priority") {
		t.Fatalf("add = %q, %v", out, err)
	}
	if store.sources[0] != board.SourceChat || store.added[0].ConversationID != boardConv {
		t.Errorf("a conversation's session must add a chat card linked to it: %+v %v", store.added[0], store.sources[0])
	}
	if _, err := runBoard(t, tool, boardCtx("agent_job-run-1"), `{"action":"add","label":"nightly check"}`); err != nil {
		t.Fatal(err)
	}
	if store.sources[1] != board.SourceBackground || store.added[1].ConversationID != "" {
		t.Errorf("a job's session must add an unlinked background card: %+v %v", store.added[1], store.sources[1])
	}
	if _, err := runBoard(t, tool, boardCtx(boardConv), `{"action":"add"}`); err == nil {
		t.Error("add without a label must be refused")
	}
	if _, err := runBoard(t, tool, boardCtx(boardConv), `{"action":"add","label":"x","due":"tomorrow"}`); err == nil {
		t.Error("a due date that is not RFC-3339 must be refused")
	}
}

// Ids the conversation never saw are refused; a list, a search or an add shows them.
func TestBoardActsOnlyOnSeenCards(t *testing.T) {
	store := newFakeBoard()
	store.cards = []board.Card{{ID: "old", ColumnID: "todo", Label: "pay the invoice", Priority: 2}}
	tool := &BoardTool{Store: store}
	ctx := boardCtx(boardConv)
	for _, args := range []string{
		`{"action":"update","id":"old","label":"x"}`,
		`{"action":"move","id":"old","column":"done"}`,
		`{"action":"delete","id":"old"}`,
		`{"action":"update"}`,
	} {
		if _, err := runBoard(t, tool, ctx, args); err == nil {
			t.Errorf("%s ran on an unseen card", args)
		}
	}
	if out, err := runBoard(t, tool, ctx, `{"action":"list"}`); err != nil || !strings.Contains(out, "[old] pay the invoice") {
		t.Fatalf("list = %q, %v", out, err)
	}
	if out, err := runBoard(t, tool, ctx, `{"action":"move","id":"old","column":"done"}`); err != nil || !strings.Contains(out, "moved to done: [old] pay the invoice") {
		t.Fatalf("move after list = %q, %v", out, err)
	}
	if _, err := runBoard(t, tool, boardCtx("other-session"), `{"action":"delete","id":"old"}`); err == nil {
		t.Error("another session must not inherit this conversation's seen ids")
	}
	tool.Evict(boardConv)
	if _, err := runBoard(t, tool, ctx, `{"action":"delete","id":"old"}`); err == nil {
		t.Error("an evicted session must have to list again")
	}
	if _, err := runBoard(t, tool, ctx, `{"action":"search","query":"invoice"}`); err != nil {
		t.Fatal(err)
	}
	if out, err := runBoard(t, tool, ctx, `{"action":"delete","id":"old"}`); err != nil || !strings.Contains(out, "deleted") {
		t.Fatalf("delete after search = %q, %v", out, err)
	}
	if _, err := runBoard(t, tool, ctx, `{"action":"move","id":"old","before":"never-seen"}`); err == nil {
		t.Error("a move before an unseen card must be refused")
	}
}

var _ ApprovalDescriber = (*BoardTool)(nil)

// The board names the card an update, move or delete acts on, for the operator alone, and
// checks the seen ids first: an id this conversation was not shown, which after a restart is
// every id, is refused before the operator is asked (spec 2026-10-12). Naming it reads the
// card and its board and writes nothing.
func TestBoardDescribesTheCardItActsOn(t *testing.T) {
	store := newFakeBoard()
	store.b.Columns = append(store.b.Columns, board.Column{ID: "later", Label: strings.Repeat("L", 30)})
	store.cards = []board.Card{
		{ID: "inv", ColumnID: "done", Label: "Fattura fornitore", Priority: 2},
		{ID: "long", ColumnID: "todo", Label: strings.Repeat("x", 200), Priority: 2},
		{ID: "lost", ColumnID: "archive", Label: "old column", Priority: 2},
	}
	tool := &BoardTool{Store: store}
	ctx := boardCtx(boardConv)
	describe := func(args string) (string, error) {
		return tool.DescribeForApproval(ctx, json.RawMessage(args))
	}
	for _, args := range []string{`{"action":"delete","id":"inv"}`, `{"action":"update","id":"inv"}`, `{"action":"move","id":"inv","column":"todo"}`, `{"action":"delete"}`} {
		if _, err := describe(args); err == nil {
			t.Errorf("%s was described before the card was shown", args)
		}
	}
	if _, err := describe(`{"action":"delete","id":"inv"}`); err == nil || !strings.Contains(err.Error(), "run board list or board search first") {
		t.Errorf("an unseen id must be refused with the seen check's own words: %v", err)
	}
	for _, args := range []string{`{"action":"list"}`, `{"action":"search","query":"old column"}`} {
		if _, err := runBoard(t, tool, ctx, args); err != nil {
			t.Fatal(err)
		}
	}
	writes := store.writes
	for args, want := range map[string]string{
		`{"action":"update","id":"inv","label":"x"}`:    "Fattura fornitore · Done",
		`{"action":"delete","id":"inv"}`:                "Fattura fornitore · Done",
		`{"action":"move","id":"inv","column":"todo"}`:  "Fattura fornitore · Done → To do",
		`{"action":"move","id":"inv"}`:                  "Fattura fornitore · Done → Done",
		`{"action":"move","id":"inv","column":"later"}`: "Fattura fornitore · Done → " + strings.Repeat("L", 15) + "…",
		`{"action":"delete","id":"long"}`:               strings.Repeat("x", 79) + "… · To do",
		`{"action":"delete","id":"lost"}`:               "old column · archive",
	} {
		if got, err := describe(args); err != nil || got != want {
			t.Errorf("%s = %q, %v; want %q", args, got, err, want)
		}
	}
	if store.writes != writes {
		t.Errorf("describing wrote to the board %d time(s)", store.writes-writes)
	}
	if _, err := describe(`{"action":"move","id":"inv","before":"never-seen"}`); err == nil {
		t.Error("a move before an unseen card must be refused before the prompt")
	}
	for _, args := range []string{`{"action":"list"}`, `{"action":"search","query":"x"}`, `{"action":"add","label":"x"}`, `{`, `{"action":"delete","id":7}`} {
		if got, err := describe(args); got != "" || err != nil {
			t.Errorf("%s = %q, %v; want nothing to describe", args, got, err)
		}
	}
	store.readErr = errors.New("board store unavailable")
	if _, err := describe(`{"action":"delete","id":"inv"}`); !errors.Is(err, store.readErr) {
		t.Errorf("a store failure = %v, want it to refuse the call", err)
	}
	store.readErr = nil
	store.cards = nil
	if got, err := describe(`{"action":"delete","id":"inv"}`); got != "" || err != nil {
		t.Errorf("a card that is gone = %q, %v; want nothing to describe, Execute reports it", got, err)
	}
	if got, err := (&BoardTool{}).DescribeForApproval(ctx, json.RawMessage(`{"action":"delete","id":"inv"}`)); got != "" || err != nil {
		t.Errorf("no store = %q, %v; want nothing to describe, Execute reports it", got, err)
	}
}

// The snapshot is bounded: 33 cards show 32 and count the rest, a long label is clipped, and
// only the cards shown become actionable.
func TestBoardListIsABoundedSnapshot(t *testing.T) {
	store := newFakeBoard()
	for i := range 33 {
		store.cards = append(store.cards, board.Card{ID: fmt.Sprintf("c%02d", i), ColumnID: "todo", Label: strings.Repeat("x", 161), Priority: 2})
	}
	store.b.Columns[0].CardLimit = 10
	tool := &BoardTool{Store: store}
	ctx := boardCtx(boardConv)
	out, err := runBoard(t, tool, ctx, `{"action":"list"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "(1 more card(s) not shown") || !strings.Contains(out, "over the limit") {
		t.Errorf("snapshot = %q", out)
	}
	if strings.Contains(out, strings.Repeat("x", 161)) || !strings.Contains(out, strings.Repeat("x", 159)+"…") {
		t.Error("a label over 160 runes must be clipped to 160")
	}
	if _, err := runBoard(t, tool, ctx, `{"action":"delete","id":"c32"}`); err == nil {
		t.Error("the card beyond the snapshot was never shown and must be refused")
	}
	if _, err := runBoard(t, tool, ctx, `{"action":"delete","id":"c31"}`); err != nil {
		t.Errorf("a shown card was refused: %v", err)
	}
	out, _ = runBoard(t, tool, ctx, `{"action":"list","column":"done"}`)
	if !strings.Contains(out, "Done (done): 0 card(s)") || strings.Contains(out, "To do") {
		t.Errorf("one column = %q", out)
	}
}

func TestBoardUpdateClearsAndSetsTheDueDate(t *testing.T) {
	store := newFakeBoard()
	due := time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC)
	store.cards = []board.Card{{ID: "a", ColumnID: "todo", Label: "x", Priority: 2, DueAt: &due}}
	tool := &BoardTool{Store: store}
	ctx := boardCtx(boardConv)
	_, _ = runBoard(t, tool, ctx, `{"action":"list"}`)
	if _, err := runBoard(t, tool, ctx, `{"action":"update","id":"a","due":""}`); err != nil {
		t.Fatal(err)
	}
	if store.cards[0].DueAt != nil {
		t.Error("an empty due on update must clear it")
	}
}

func TestBoardWithoutAStoreOrActionIsRefused(t *testing.T) {
	if _, err := (&BoardTool{}).Execute(context.Background(), json.RawMessage(`{"action":"list"}`)); err == nil {
		t.Error("no store must be refused")
	}
	tool := &BoardTool{Store: newFakeBoard()}
	for _, raw := range []string{`{}`, `{`, `{"action":"purge"}`} {
		if _, err := tool.Execute(context.Background(), json.RawMessage(raw)); err == nil {
			t.Errorf("%s accepted", raw)
		}
	}
	if !errors.Is(board.ErrNotFound, board.ErrNotFound) {
		t.Fatal("unreachable")
	}
}

func TestBoardSpecIsDeferredAndMultiplexed(t *testing.T) {
	spec := (&BoardTool{}).Spec()
	if spec.Name != "board" || !spec.Deferred || !spec.Mutating || !spec.Multiplexed {
		t.Fatalf("spec = %+v", spec)
	}
	for _, word := range []string{"kanban", "backlog", "to-do"} {
		if !strings.Contains(strings.ToLower(spec.Summary), word) {
			t.Errorf("summary lacks %q, which tool_search needs to find the board", word)
		}
	}
}

// An overdue card past the 32-card cap is the one a daily briefing must not miss: the whole-board
// list shows what is due within 24 hours first, overdue first, each card once.
func TestBoardListShowsWhatIsDueFirst(t *testing.T) {
	store := newFakeBoard()
	now := time.Now()
	due := func(d time.Duration) *time.Time { at := now.Add(d); return &at }
	for i := range 40 {
		store.cards = append(store.cards, board.Card{ID: fmt.Sprintf("c%02d", i), ColumnID: "todo", Label: fmt.Sprintf("card %d", i), Priority: 2})
	}
	store.cards[39].DueAt = due(-2 * time.Hour)
	store.cards[38].DueAt = due(3 * time.Hour)
	store.cards[37].DueAt = due(72 * time.Hour)
	store.cards = append(store.cards, board.Card{ID: "d00", ColumnID: "done", Label: "shipped late", Priority: 2, DueAt: due(-48 * time.Hour)})
	tool := &BoardTool{Store: store}
	ctx := boardCtx(boardConv)

	out, err := runBoard(t, tool, ctx, `{"action":"list"}`)
	if err != nil {
		t.Fatal(err)
	}
	section, rest, found := strings.Cut(out, "## To do")
	if !found || !strings.Contains(section, "## Due within 24 hours") {
		t.Fatalf("no due section before the columns:\n%s", out)
	}
	late, overdue, soon := strings.Index(section, "[d00]"), strings.Index(section, "[c39]"), strings.Index(section, "[c38]")
	if late < 0 || overdue < 0 || soon < 0 || late > overdue || overdue > soon || strings.Contains(section, "[c37]") {
		t.Errorf("due section = %q, want d00, c39, c38 in due order and not c37", section)
	}
	for _, id := range []string{"[d00]", "[c39]", "[c38]"} {
		if n := strings.Count(out, id); n != 1 {
			t.Errorf("%s listed %d times, want once", id, n)
		}
	}
	if !strings.Contains(rest, "(9 more card(s) not shown") {
		t.Errorf("41 cards, 32 shown: want 9 more, got %q", rest)
	}
	if _, err := runBoard(t, tool, ctx, `{"action":"delete","id":"c39"}`); err != nil {
		t.Errorf("the overdue card was shown and must be actionable: %v", err)
	}

	out, _ = runBoard(t, tool, ctx, `{"action":"list","column":"todo"}`)
	if strings.Contains(out, "## Due") {
		t.Errorf("a column listing is a request for that column, got a due section:\n%s", out)
	}
}
