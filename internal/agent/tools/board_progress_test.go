package tools

import (
	"strings"
	"testing"
)

func TestBoardToolAddsUpdatesAndReadsProgress(t *testing.T) {
	store := newFakeBoard()
	tool := &BoardTool{Store: store}
	ctx := boardCtx(boardConv)
	text, err := runBoard(t, tool, ctx, `{"action":"add","label":"renew the contract","progress":0.55}`)
	if err != nil || !strings.Contains(text, "55% complete") || len(store.added) != 1 || store.added[0].Progress != 0.55 {
		t.Fatalf("add: %q, %v; stored %+v", text, err, store.added)
	}
	text, err = runBoard(t, tool, ctx, `{"action":"update","id":"c1","label":"confirm renewal"}`)
	if err != nil || !strings.Contains(text, "55% complete") {
		t.Fatalf("omitted progress: %q, %v", text, err)
	}
	text, err = runBoard(t, tool, ctx, `{"action":"update","id":"c1","progress":1}`)
	if err != nil || !strings.Contains(text, "100% complete") {
		t.Fatalf("complete: %q, %v", text, err)
	}
	text, err = runBoard(t, tool, ctx, `{"action":"update","id":"c1","progress":0}`)
	if err != nil || !strings.Contains(text, "0% complete") {
		t.Fatalf("reset: %q, %v", text, err)
	}
	text, err = runBoard(t, tool, ctx, `{"action":"list"}`)
	if err != nil || !strings.Contains(text, "0% complete") {
		t.Fatalf("list: %q, %v", text, err)
	}
}
