package tools

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/aura/aura/internal/llm"
)

// fakePatchProposalStore is an in-memory patchProposalInserter for tests.
type fakePatchProposalStore struct {
	mu   sync.Mutex
	rows []patchProposalRow
}

func (f *fakePatchProposalStore) Insert(_ context.Context, row patchProposalRow) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.SignatureHash == row.SignatureHash {
			return false, nil // idempotent skip
		}
	}
	f.rows = append(f.rows, row)
	return true, nil
}

func (f *fakePatchProposalStore) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.rows)
}

func (f *fakePatchProposalStore) first() patchProposalRow {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.rows) == 0 {
		return patchProposalRow{}
	}
	return f.rows[0]
}

// TestProposePatch_WikiProposalCreatesRow verifies a wiki proposal inserts a
// row with kind='wiki' and action='new' (status='pending' is set by the SQL store).
func TestProposePatch_WikiProposalCreatesRow(t *testing.T) {
	store := &fakePatchProposalStore{}
	tool := NewProposePatchTool(store)

	result, err := tool.Execute(context.Background(), map[string]any{
		"action":         "wiki",
		"target_slug":    "my-page",
		"body":           "# My Page\n\nUpdated content.",
		"change_summary": "Added updated content section.",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if store.count() != 1 {
		t.Fatalf("expected 1 row in store, got %d", store.count())
	}
	row := store.first()
	if row.Kind != "wiki" {
		t.Errorf("kind = %q, want %q", row.Kind, "wiki")
	}
	if row.Action != "new" {
		t.Errorf("action = %q, want %q", row.Action, "new")
	}
	if row.TargetSlug != "my-page" {
		t.Errorf("target_slug = %q, want %q", row.TargetSlug, "my-page")
	}
	if len(row.SignatureHash) != 16 {
		t.Errorf("signature_hash %q: want 16-char hex", row.SignatureHash)
	}
	if !strings.Contains(result, "my-page") {
		t.Errorf("result %q: missing target_slug", result)
	}
}

// TestProposePatch_UserMemoryProposalCreatesRow verifies user_memory proposals.
func TestProposePatch_UserMemoryProposalCreatesRow(t *testing.T) {
	store := &fakePatchProposalStore{}
	tool := NewProposePatchTool(store)

	_, err := tool.Execute(context.Background(), map[string]any{
		"action":         "user_memory",
		"fact":           "User prefers dark mode in the dashboard.",
		"category":       "preference",
		"change_summary": "Observed from repeated user comments.",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if store.count() != 1 {
		t.Fatalf("expected 1 row, got %d", store.count())
	}
	row := store.first()
	if row.Kind != "user_memory" {
		t.Errorf("kind = %q, want %q", row.Kind, "user_memory")
	}
	if row.Category != "preference" {
		t.Errorf("category = %q, want %q", row.Category, "preference")
	}
	if len(row.SignatureHash) != 16 {
		t.Errorf("signature_hash %q: want 16-char hex", row.SignatureHash)
	}
}

// TestProposePatch_OperationalProposalCreatesRow verifies operational proposals.
func TestProposePatch_OperationalProposalCreatesRow(t *testing.T) {
	store := &fakePatchProposalStore{}
	tool := NewProposePatchTool(store)

	result, err := tool.Execute(context.Background(), map[string]any{
		"action":         "operational",
		"tool_name":      "web_search",
		"error_class":    "timeout",
		"lesson":         "Retry with a shorter query when web_search times out.",
		"change_summary": "Observed during long-query runs.",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if store.count() != 1 {
		t.Fatalf("expected 1 row, got %d", store.count())
	}
	row := store.first()
	if row.Kind != "operational_memory" {
		t.Errorf("kind = %q, want %q", row.Kind, "operational_memory")
	}
	if row.TargetSlug != "web_search" {
		t.Errorf("target_slug = %q, want %q", row.TargetSlug, "web_search")
	}
	if row.Category != "timeout" {
		t.Errorf("category = %q, want %q", row.Category, "timeout")
	}
	if len(row.SignatureHash) != 16 {
		t.Errorf("signature_hash %q: want 16-char hex", row.SignatureHash)
	}
	if !strings.Contains(result, "web_search") {
		t.Errorf("result %q: missing tool_name", result)
	}
}

// TestProposePatch_Idempotency verifies a second call with identical args
// produces no new row (signature_hash collision → idempotent skip).
func TestProposePatch_Idempotency(t *testing.T) {
	store := &fakePatchProposalStore{}
	tool := NewProposePatchTool(store)

	args := map[string]any{
		"action":         "wiki",
		"target_slug":    "duplicate-page",
		"body":           "Same body content.",
		"change_summary": "First proposal.",
	}

	if _, err := tool.Execute(context.Background(), args); err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	if store.count() != 1 {
		t.Fatalf("after first call: expected 1 row, got %d", store.count())
	}

	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("second Execute: %v", err)
	}
	if store.count() != 1 {
		t.Errorf("after second call: expected still 1 row (idempotent), got %d", store.count())
	}
	if !strings.Contains(result, "idempotent skip") {
		t.Errorf("second result %q: want 'idempotent skip'", result)
	}
}

// TestProposePatch_MissingRequiredField verifies missing per-action required
// fields return a schema validation error.
func TestProposePatch_MissingRequiredField(t *testing.T) {
	store := &fakePatchProposalStore{}
	tool := NewProposePatchTool(store)

	cases := []struct {
		name string
		args map[string]any
	}{
		{
			name: "wiki missing target_slug",
			args: map[string]any{
				"action": "wiki", "body": "b", "change_summary": "r",
			},
		},
		{
			name: "wiki missing body",
			args: map[string]any{
				"action": "wiki", "target_slug": "s", "change_summary": "r",
			},
		},
		{
			name: "user_memory missing fact",
			args: map[string]any{
				"action": "user_memory", "category": "preference", "change_summary": "r",
			},
		},
		{
			name: "user_memory missing category",
			args: map[string]any{
				"action": "user_memory", "fact": "f", "change_summary": "r",
			},
		},
		{
			name: "operational missing tool_name",
			args: map[string]any{
				"action": "operational", "error_class": "timeout", "lesson": "l", "change_summary": "r",
			},
		},
		{
			name: "missing change_summary",
			args: map[string]any{
				"action": "wiki", "target_slug": "p", "body": "b",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tool.Execute(context.Background(), tc.args)
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}
			if !errors.Is(err, llm.ErrSchemaValidation) {
				t.Errorf("error %v: want errors.Is(err, llm.ErrSchemaValidation)=true", err)
			}
		})
	}
	if store.count() != 0 {
		t.Errorf("expected 0 rows after validation failures, got %d", store.count())
	}
}

// TestProposePatch_WriteProposalAllowlistBlocksDirectWrites demonstrates the
// write_proposal allowlist contract: propose_patch IS available; direct write
// tools (wiki_page, source, file) are NOT in the filtered definition surface.
//
// Uses Registry.DefinitionsFor to mirror how a write_proposal subagent receives
// its tool surface (US-S02/S03 wire this at the swarm layer).
func TestProposePatch_WriteProposalAllowlistBlocksDirectWrites(t *testing.T) {
	store := &fakePatchProposalStore{}
	reg := NewRegistry(nil)
	reg.Register(NewProposePatchTool(store))
	reg.Register(&WikiPageTool{}) // direct write tool — must be filtered out

	writeProposalAllowlist := []string{"propose_patch", "search_memory", "web_search"}
	defs := reg.DefinitionsFor(writeProposalAllowlist)

	inAllowlist := map[string]bool{}
	for _, d := range defs {
		inAllowlist[d.Name] = true
	}

	if !inAllowlist["propose_patch"] {
		t.Error("propose_patch must be visible to write_proposal subagents")
	}
	if inAllowlist["wiki_page"] {
		t.Error("wiki_page (direct write tool) must NOT be in write_proposal allowlist definitions")
	}
}
