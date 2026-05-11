package tools

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/reindex"
	"github.com/aura/aura/internal/wiki"
)

// newTestWikiStore returns a *wiki.Store rooted at t.TempDir() with a
// silent logger. Real signature (verified at internal/wiki/store.go:110):
//
//	func NewStore(dir string, logger *slog.Logger) (*Store, error)
func newTestWikiStore(t *testing.T) *wiki.Store {
	t.Helper()
	silent := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := wiki.NewStore(t.TempDir(), silent)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

type fakeSubmitter struct {
	calls int64
	last  reindex.Job
}

func (f *fakeSubmitter) Submit(j reindex.Job) bool {
	f.calls++
	f.last = j
	return true
}

func TestWriteWikiPage_Name(t *testing.T) {
	tool := NewWriteWikiPageTool(newTestWikiStore(t), nil)
	if tool == nil {
		t.Fatal("NewWriteWikiPageTool returned nil with non-nil store")
	}
	if got := tool.Name(); got != "write_wiki_page" {
		t.Fatalf("Name() = %q, want %q", got, "write_wiki_page")
	}
}

func TestWriteWikiPage_NilStore(t *testing.T) {
	if tool := NewWriteWikiPageTool(nil, nil); tool != nil {
		t.Fatal("NewWriteWikiPageTool(nil, ...) should return nil")
	}
}

func TestWriteWikiPage_Parameters_AdditionalPropertiesFalse(t *testing.T) {
	tool := NewWriteWikiPageTool(newTestWikiStore(t), nil)
	params := tool.Parameters()
	if params["type"] != "object" {
		t.Fatalf("type = %v, want object", params["type"])
	}
	if ap, ok := params["additionalProperties"].(bool); !ok || ap != false {
		t.Fatalf("additionalProperties = %v (type %T), want false bool", params["additionalProperties"], params["additionalProperties"])
	}
	req, ok := params["required"].([]string)
	if !ok {
		t.Fatalf("required type = %T, want []string", params["required"])
	}
	wantReq := []string{"title", "body", "expected_updated_at"}
	if len(req) != len(wantReq) {
		t.Fatalf("required len = %d, want %d", len(req), len(wantReq))
	}
	for _, k := range wantReq {
		found := false
		for _, r := range req {
			if r == k {
				found = true
			}
		}
		if !found {
			t.Fatalf("required missing %q: got %v", k, req)
		}
	}
	// Privileged keys MUST NOT appear in properties.
	props, _ := params["properties"].(map[string]any)
	for _, priv := range []string{"slug", "unversioned", "schema_version", "prompt_version", "created_at", "updated_at"} {
		if _, ok := props[priv]; ok {
			t.Fatalf("properties unexpectedly contains privileged key %q", priv)
		}
	}
}

func TestWriteWikiPage_HappyPath_Create(t *testing.T) {
	store := newTestWikiStore(t)
	sub := &fakeSubmitter{}
	tool := NewWriteWikiPageTool(store, sub)
	out, err := tool.Execute(context.Background(), map[string]any{
		"title":               "Test Page",
		"body":                "Hello world",
		"expected_updated_at": "", // create-only sentinel
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var resp map[string]string
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("non-JSON result: %q", out)
	}
	if resp["status"] != "ok" {
		t.Fatalf("status = %q, want ok", resp["status"])
	}
	if resp["slug"] != wiki.Slug("Test Page") {
		t.Fatalf("slug = %q, want %q", resp["slug"], wiki.Slug("Test Page"))
	}
	if sub.calls != 1 {
		t.Fatalf("submitter calls = %d, want 1", sub.calls)
	}
	if sub.last.Op != reindex.OpUpsert {
		t.Fatalf("submitted op = %v, want OpUpsert", sub.last.Op)
	}
}

func TestWriteWikiPage_Conflict_ETagMismatch(t *testing.T) {
	store := newTestWikiStore(t)
	tool := NewWriteWikiPageTool(store, nil)
	// Setup: create the page first.
	_, _ = tool.Execute(context.Background(), map[string]any{
		"title": "Conflict Page", "body": "v1", "expected_updated_at": "",
	})
	// Now try to update with a STALE expected_updated_at.
	out, err := tool.Execute(context.Background(), map[string]any{
		"title": "Conflict Page", "body": "v2", "expected_updated_at": "1999-01-01T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("Execute should return nil error on conflict (tool RESULT), got: %v", err)
	}
	var resp map[string]string
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("non-JSON conflict result: %q", out)
	}
	if resp["error"] != "conflict" {
		t.Fatalf("error = %q, want conflict", resp["error"])
	}
	if resp["expected_updated_at"] != "1999-01-01T00:00:00Z" {
		t.Fatalf("expected_updated_at = %q, want stale value passthrough", resp["expected_updated_at"])
	}
	if resp["actual_updated_at"] == "" {
		t.Fatal("actual_updated_at must be populated")
	}
	if resp["slug"] != wiki.Slug("Conflict Page") {
		t.Fatalf("slug = %q, want derived", resp["slug"])
	}
}

func TestWriteWikiPage_CreateOnly_AlreadyExists(t *testing.T) {
	store := newTestWikiStore(t)
	tool := NewWriteWikiPageTool(store, nil)
	_, _ = tool.Execute(context.Background(), map[string]any{
		"title": "Twice", "body": "v1", "expected_updated_at": "",
	})
	out, err := tool.Execute(context.Background(), map[string]any{
		"title": "Twice", "body": "v2", "expected_updated_at": "",
	})
	if err != nil {
		t.Fatalf("Execute should return nil error on create-only conflict: %v", err)
	}
	var resp map[string]string
	_ = json.Unmarshal([]byte(out), &resp)
	if resp["error"] != "conflict" {
		t.Fatalf("error = %q, want conflict", resp["error"])
	}
	if resp["expected_updated_at"] != "" {
		t.Fatalf("expected_updated_at = %q, want empty (create-only sentinel echo)", resp["expected_updated_at"])
	}
}

func TestWriteWikiPage_MissingRequiredArg_Wraps_ErrSchemaValidation(t *testing.T) {
	tool := NewWriteWikiPageTool(newTestWikiStore(t), nil)
	cases := []map[string]any{
		{"body": "x", "expected_updated_at": ""},  // missing title
		{"title": "x", "expected_updated_at": ""},  // missing body
		{"title": "x", "body": "y"},                // missing expected_updated_at
	}
	for i, args := range cases {
		_, err := tool.Execute(context.Background(), args)
		if err == nil {
			t.Fatalf("case %d: expected error, got nil", i)
		}
		if !errors.Is(err, llm.ErrSchemaValidation) {
			t.Fatalf("case %d: err = %v, want wraps llm.ErrSchemaValidation", i, err)
		}
	}
}

func TestWriteWikiPage_PrivilegedFieldsIgnored(t *testing.T) {
	store := newTestWikiStore(t)
	tool := NewWriteWikiPageTool(store, nil)
	// LLM passes privileged fields. additionalProperties:false would reject upstream;
	// even if they slip through to Execute, they MUST NOT be stored on the Page.
	// Deviation note: PromptVersion is "v1" (not "write_wiki_page/v1") because
	// wiki.Validate's promptVersionRe does not accept slash-separated formats.
	// See 02-04-SUMMARY.md for details.
	_, err := tool.Execute(context.Background(), map[string]any{
		"title": "Priv", "body": "x", "expected_updated_at": "",
		"unversioned":    true,        // should be ignored
		"schema_version": 99,          // should be ignored
		"prompt_version": "evil",      // should be ignored
		"slug":           "evil-slug", // should be ignored — slug is derived
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	page, err := store.ReadPage(wiki.Slug("Priv"))
	if err != nil {
		t.Fatal(err)
	}
	if page.Unversioned {
		t.Fatal("Unversioned was set from LLM input — privilege escalation!")
	}
	if page.SchemaVersion != wiki.CurrentSchemaVersion {
		t.Fatalf("SchemaVersion = %d, want %d (LLM cannot override)", page.SchemaVersion, wiki.CurrentSchemaVersion)
	}
	// PromptVersion must be the server-controlled value, not "evil".
	// We use "v1" (passes wiki.Validate's promptVersionRe) rather than
	// "write_wiki_page/v1" (plan spec) which is rejected by the regex.
	if page.PromptVersion != "v1" {
		t.Fatalf("PromptVersion = %q, want \"v1\" (server-controlled)", page.PromptVersion)
	}
}

func TestWriteWikiPage_NilSubmitter_DoesNotPanic(t *testing.T) {
	store := newTestWikiStore(t)
	tool := NewWriteWikiPageTool(store, nil) // nil Submitter is acceptable
	_, err := tool.Execute(context.Background(), map[string]any{
		"title": "NoReindex", "body": "x", "expected_updated_at": "",
	})
	if err != nil {
		t.Fatalf("Execute with nil submitter: %v", err)
	}
}
