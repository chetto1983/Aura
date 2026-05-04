package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/aura/aura/internal/conversation/summarizer"
	"github.com/aura/aura/internal/scheduler"
	"github.com/aura/aura/internal/wiki"
)

func TestWriteAndReadWikiTools(t *testing.T) {
	store, err := wiki.NewStore(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	write := NewWriteWikiTool(store, nil)
	if write.Name() != "write_wiki" || write.Description() == "" || write.Parameters()["type"] != "object" {
		t.Fatal("write_wiki metadata is incomplete")
	}

	result, err := write.Execute(t.Context(), map[string]any{
		"title":    "Tool Calling Notes",
		"body":     "Aura uses [[tool-calling]] for wiki writes.",
		"tags":     []any{"tools", "wiki", ""},
		"category": "engineering",
		"related":  []any{"tool-calling"},
		"sources":  []any{"https://example.com"},
	})
	if err != nil {
		t.Fatalf("write.Execute() error = %v", err)
	}
	if !strings.Contains(result, "[[tool-calling-notes]]") {
		t.Fatalf("write result = %q, want slug", result)
	}

	read := NewReadWikiTool(store)
	if read.Name() != "read_wiki" || read.Description() == "" || read.Parameters()["type"] != "object" {
		t.Fatal("read_wiki metadata is incomplete")
	}
	out, err := read.Execute(t.Context(), map[string]any{"slug": "tool-calling-notes"})
	if err != nil {
		t.Fatalf("read.Execute() error = %v", err)
	}
	for _, want := range []string{"# Tool Calling Notes", "Category: engineering", "Tags: tools, wiki", "Related: [[tool-calling]]", "Sources: https://example.com"} {
		if !strings.Contains(out, want) {
			t.Fatalf("read output missing %q: %s", want, out)
		}
	}
}

func TestWikiToolValidation(t *testing.T) {
	store, err := wiki.NewStore(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	write := NewWriteWikiTool(store, nil)
	if _, err := write.Execute(t.Context(), map[string]any{"title": "Missing Body"}); err == nil {
		t.Fatal("expected validation error for missing body")
	}

	read := NewReadWikiTool(store)
	if _, err := read.Execute(t.Context(), map[string]any{"slug": ""}); err == nil {
		t.Fatal("expected validation error for empty slug")
	}
}

func TestSearchWikiToolMetadata(t *testing.T) {
	searchTool := NewSearchWikiTool(nil)
	if searchTool.Name() != "search_wiki" || searchTool.Description() == "" || searchTool.Parameters()["type"] != "object" {
		t.Fatal("search_wiki metadata is incomplete")
	}
}

func TestProposeWikiChangeTool(t *testing.T) {
	db := scheduler.NewTestDB(t)
	store := summarizer.NewSummariesStore(db)
	tool := NewProposeWikiChangeTool(store)
	if tool.Name() != "propose_wiki_change" || tool.Description() == "" || tool.Parameters()["type"] != "object" {
		t.Fatal("propose_wiki_change metadata is incomplete")
	}

	out, err := tool.Execute(WithUserID(t.Context(), "12345"), map[string]any{
		"action":          "patch",
		"fact":            "Add a note about proactive review-gated wiki proposals.",
		"target_slug":     "aurabot-swarm",
		"category":        "project",
		"related":         []any{"aurabot", "second-brain"},
		"source_turn_ids": []any{float64(7), float64(8)},
		"origin_tool":     "search_memory",
		"origin_reason":   "source-backed second brain improvement",
		"evidence": []any{
			map[string]any{"kind": "source", "id": "src_abc", "title": "note.pdf", "page": float64(2), "snippet": "review-gated wiki proposals"},
			map[string]any{"kind": "archive", "id": "conversation:7"},
		},
		"confidence": 0.8,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var resp proposeWikiChangeResponse
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("response JSON: %v", err)
	}
	if !resp.OK || resp.ID == 0 || resp.Status != "pending" || resp.TargetSlug != "aurabot-swarm" {
		t.Fatalf("response = %+v", resp)
	}
	got, err := store.Get(t.Context(), resp.ID)
	if err != nil {
		t.Fatalf("Get proposal: %v", err)
	}
	if got.ChatID != 12345 || got.Action != "patch" || got.Category != "project" || got.Similarity != 0.8 {
		t.Fatalf("proposal = %+v", got)
	}
	if len(got.RelatedSlugs) != 2 || got.RelatedSlugs[0] != "aurabot" || got.RelatedSlugs[1] != "second-brain" {
		t.Fatalf("related = %+v", got.RelatedSlugs)
	}
	if len(got.SourceTurnIDs) != 2 || got.SourceTurnIDs[0] != 7 || got.SourceTurnIDs[1] != 8 {
		t.Fatalf("turn ids = %+v", got.SourceTurnIDs)
	}
	if got.Provenance.OriginTool != "search_memory" || got.Provenance.OriginReason != "source-backed second brain improvement" {
		t.Fatalf("provenance = %+v", got.Provenance)
	}
	if len(got.Provenance.Evidence) != 2 || got.Provenance.Evidence[0].ID != "src_abc" || got.Provenance.Evidence[0].Page != 2 {
		t.Fatalf("evidence = %+v", got.Provenance.Evidence)
	}
}

func TestProposeWikiChangeToolValidation(t *testing.T) {
	db := scheduler.NewTestDB(t)
	tool := NewProposeWikiChangeTool(summarizer.NewSummariesStore(db))

	if _, err := tool.Execute(t.Context(), map[string]any{"action": "new"}); err == nil {
		t.Fatal("expected missing fact error")
	}
	if _, err := tool.Execute(t.Context(), map[string]any{"action": "patch", "fact": "missing target"}); err == nil {
		t.Fatal("expected missing target error")
	}
	if _, err := tool.Execute(t.Context(), map[string]any{
		"action":      "patch",
		"fact":        "missing evidence",
		"target_slug": "aura-memory",
		"origin_tool": "search_memory",
	}); err == nil || !strings.Contains(err.Error(), "evidence refs are required") {
		t.Fatalf("expected missing search_memory evidence error, got %v", err)
	}
}
