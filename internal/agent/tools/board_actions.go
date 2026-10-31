package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/chetto1983/aura/internal/board"
)

type boardArgs struct {
	Action      string    `json:"action"`
	Column      string    `json:"column"`
	Query       string    `json:"query"`
	ID          string    `json:"id"`
	Label       *string   `json:"label"`
	Description *string   `json:"description"`
	Priority    *int      `json:"priority"`
	Tags        *[]string `json:"tags"`
	Due         *string   `json:"due"`
	Before      string    `json:"before"`
}

func decodeBoardArgs(raw json.RawMessage) (boardArgs, error) {
	var a boardArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return a, fmt.Errorf("board args: %w", err)
	}
	return a, nil
}

func (t *BoardTool) actionList(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	a, err := decodeBoardArgs(raw)
	if err != nil {
		return ToolResult{}, err
	}
	identityID := boardIdentity(ctx)
	b, err := t.Store.Board(ctx, identityID)
	if err != nil {
		return ToolResult{}, err
	}
	cards, err := t.Store.Cards(ctx, identityID)
	if err != nil {
		return ToolResult{}, err
	}
	text, shown := renderBoard(b, cards, a.Column, time.Now())
	return t.render(ctx, text, shown)
}

func (t *BoardTool) actionSearch(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	a, err := decodeBoardArgs(raw)
	if err != nil {
		return ToolResult{}, err
	}
	cards, err := t.Store.Search(ctx, boardIdentity(ctx), a.Query, boardSnapshotCards)
	if err != nil {
		return ToolResult{}, err
	}
	var out strings.Builder
	fmt.Fprintf(&out, "%d card(s) match %s:\n", len(cards), a.Query)
	for _, c := range cards {
		writeCard(&out, c, true)
	}
	return t.render(ctx, out.String(), cards)
}

func (t *BoardTool) actionAdd(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	a, err := decodeBoardArgs(raw)
	if err != nil {
		return ToolResult{}, err
	}
	if a.Label == nil {
		return ToolResult{}, fmt.Errorf("board add: label is required")
	}
	due, err := parseDue(a.Due)
	if err != nil {
		return ToolResult{}, err
	}
	in := board.NewCard{Column: a.Column, Label: *a.Label}
	if a.Description != nil {
		in.Description = *a.Description
	}
	if a.Priority != nil {
		in.Priority = *a.Priority
	}
	if a.Tags != nil {
		in.Tags = *a.Tags
	}
	if due != nil {
		in.DueAt = *due
	}
	conversationID, source := boardOrigin(ctx)
	in.ConversationID = conversationID
	c, err := t.Store.AddCard(ctx, boardIdentity(ctx), in, source, board.ActorAgent)
	if err != nil {
		return ToolResult{}, err
	}
	var out strings.Builder
	out.WriteString("added:\n")
	writeCard(&out, c, true)
	return t.render(ctx, out.String(), []board.Card{c})
}

func (t *BoardTool) actionUpdate(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	a, err := decodeBoardArgs(raw)
	if err != nil {
		return ToolResult{}, err
	}
	if err := t.requireSeen(ctx, a.ID); err != nil {
		return ToolResult{}, err
	}
	due, err := parseDue(a.Due)
	if err != nil {
		return ToolResult{}, err
	}
	patch := board.CardPatch{Label: a.Label, Description: a.Description, Priority: a.Priority, Tags: a.Tags, DueAt: due}
	c, err := t.Store.UpdateCard(ctx, boardIdentity(ctx), a.ID, patch, board.ActorAgent)
	if err != nil {
		return ToolResult{}, err
	}
	var out strings.Builder
	out.WriteString("updated:\n")
	writeCard(&out, c, true)
	return NewResult(ctx, out.String())
}

func (t *BoardTool) actionMove(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	a, err := decodeBoardArgs(raw)
	if err != nil {
		return ToolResult{}, err
	}
	if err := t.requireSeen(ctx, a.ID); err != nil {
		return ToolResult{}, err
	}
	if a.Before != "" {
		if err := t.requireSeen(ctx, a.Before); err != nil {
			return ToolResult{}, err
		}
	}
	c, err := t.Store.MoveCard(ctx, boardIdentity(ctx), a.ID, a.Column, a.Before, board.ActorAgent)
	if err != nil {
		return ToolResult{}, err
	}
	return NewResult(ctx, fmt.Sprintf("moved to %s: [%s] %s", c.ColumnID, c.ID, c.Label))
}

func (t *BoardTool) actionDelete(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	a, err := decodeBoardArgs(raw)
	if err != nil {
		return ToolResult{}, err
	}
	if err := t.requireSeen(ctx, a.ID); err != nil {
		return ToolResult{}, err
	}
	removed, err := t.Store.DeleteCard(ctx, boardIdentity(ctx), a.ID)
	if err != nil {
		return ToolResult{}, err
	}
	if !removed {
		return NewResult(ctx, "card "+a.ID+" was already gone")
	}
	return NewResult(ctx, "deleted card "+a.ID)
}

// render remembers exactly the cards the model was shown, then wraps the text.
func (t *BoardTool) render(ctx context.Context, text string, shown []board.Card) (ToolResult, error) {
	t.remember(ctx, shown...)
	return NewResult(ctx, text)
}

// renderBoard is the bounded snapshot: columns with their counts and limits, then at most
// boardSnapshotCards cards, the ones beyond the cap counted rather than silently dropped. The
// whole board starts with what is due within 24 hours, overdue first, so no deadline hides
// behind the cap; each card is listed once.
func renderBoard(b board.Board, cards []board.Card, only string, now time.Time) (string, []board.Card) {
	var out strings.Builder
	var shown []board.Card
	listed := map[string]bool{}
	if only == "" {
		shown = renderDue(&out, cards, now)
		for _, c := range shown {
			listed[c.ID] = true
		}
	}
	for _, col := range b.Columns {
		if only != "" && col.ID != only {
			continue
		}
		var in []board.Card
		for _, c := range cards {
			if c.ColumnID == col.ID {
				in = append(in, c)
			}
		}
		limit := ""
		if col.CardLimit > 0 {
			limit = fmt.Sprintf(", limit %d", col.CardLimit)
			if len(in) > col.CardLimit {
				limit += ", over the limit"
			}
		}
		fmt.Fprintf(&out, "## %s (%s): %d card(s)%s\n", col.Label, col.ID, len(in), limit)
		for _, c := range in {
			if len(shown) == boardSnapshotCards {
				break
			}
			if listed[c.ID] {
				continue
			}
			writeCard(&out, c, false)
			shown = append(shown, c)
		}
	}
	if total := countIn(cards, only); total > len(shown) {
		fmt.Fprintf(&out, "(%d more card(s) not shown; use search or list one column)\n", total-len(shown))
	}
	return out.String(), shown
}

// renderDue lists the cards due before now+24h, earliest first, in any column: overdue is a
// deadline before now, as the cockpit's filter says (web/src/board/boardModel.ts), and the tool
// has no zone to call a day "today". It returns the cards it listed, at most the snapshot cap.
func renderDue(out *strings.Builder, cards []board.Card, now time.Time) []board.Card {
	horizon := now.Add(24 * time.Hour)
	var due []board.Card
	for _, c := range cards {
		if c.DueAt != nil && c.DueAt.Before(horizon) {
			due = append(due, c)
		}
	}
	if len(due) == 0 {
		return nil
	}
	slices.SortStableFunc(due, func(a, b board.Card) int { return a.DueAt.Compare(*b.DueAt) })
	due = due[:min(len(due), boardSnapshotCards)]
	fmt.Fprintf(out, "## Due within 24 hours, overdue first (now %s)\n", now.UTC().Format(time.RFC3339))
	for _, c := range due {
		writeCard(out, c, true)
	}
	return due
}

func countIn(cards []board.Card, only string) int {
	if only == "" {
		return len(cards)
	}
	n := 0
	for _, c := range cards {
		if c.ColumnID == only {
			n++
		}
	}
	return n
}

var priorityWords = map[int]string{1: "low", 2: "medium", 3: "high"}

func writeCard(out *strings.Builder, c board.Card, withColumn bool) {
	fmt.Fprintf(out, "- [%s] %s", c.ID, clipRunes(c.Label, boardSnapshotRunes))
	var meta []string
	if withColumn {
		meta = append(meta, "in "+c.ColumnID)
	}
	if c.Priority != 2 {
		meta = append(meta, priorityWords[c.Priority]+" priority")
	}
	if c.DueAt != nil {
		meta = append(meta, "due "+c.DueAt.UTC().Format(time.RFC3339))
	}
	if len(c.Tags) > 0 {
		meta = append(meta, "#"+strings.Join(c.Tags, " #"))
	}
	if len(meta) > 0 {
		fmt.Fprintf(out, " (%s)", strings.Join(meta, ", "))
	}
	out.WriteString("\n")
	if c.Description != "" {
		fmt.Fprintf(out, "  %s\n", clipRunes(strings.ReplaceAll(c.Description, "\n", " "), boardSnapshotRunes))
	}
}

func clipRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}
