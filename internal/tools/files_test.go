package tools

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/aura/aura/internal/source"
)

type stubDocSender struct {
	mu    sync.Mutex
	calls []docCall
	err   error
}

type docCall struct {
	userID, filename, caption string
	bodyLen                   int
}

func (s *stubDocSender) SendDocumentToUser(userID, filename string, body []byte, caption string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, docCall{userID, filename, caption, len(body)})
	return s.err
}

func newCreateXLSXTest(t *testing.T) (*CreateXLSXTool, *stubDocSender, *source.Store) {
	t.Helper()
	store, err := source.NewStore(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("source.NewStore: %v", err)
	}
	sender := &stubDocSender{}
	tool := NewCreateXLSXTool(store, sender)
	if tool == nil {
		t.Fatal("NewCreateXLSXTool returned nil")
	}
	return tool, sender, store
}

func TestCreateXLSXTool_HappyPath_DeliversAndPersists(t *testing.T) {
	tool, sender, store := newCreateXLSXTest(t)
	ctx := WithUserID(context.Background(), "12345")
	args := map[string]any{
		"filename": "report",
		"sheets": []any{
			map[string]any{
				"name": "Q1",
				"rows": []any{
					[]any{"month", "revenue"},
					[]any{"jan", 100.0},
					[]any{"feb", 120.5},
				},
			},
		},
		"caption": "Here's your report",
	}
	out, err := tool.Execute(ctx, args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp["filename"] != "report.xlsx" {
		t.Errorf("filename = %v, want report.xlsx", resp["filename"])
	}
	if resp["delivered"] != true {
		t.Errorf("delivered = %v, want true", resp["delivered"])
	}
	if resp["duplicate"] != false {
		t.Errorf("duplicate = %v, want false", resp["duplicate"])
	}

	// Sender must have been invoked.
	if len(sender.calls) != 1 {
		t.Fatalf("sender called %d times, want 1", len(sender.calls))
	}
	got := sender.calls[0]
	if got.userID != "12345" || got.filename != "report.xlsx" || got.caption != "Here's your report" || got.bodyLen == 0 {
		t.Errorf("sender call = %+v", got)
	}

	// Source persisted with KindXLSX + StatusIngested.
	id := resp["source_id"].(string)
	rec, err := store.Get(id)
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if rec.Kind != source.KindXLSX {
		t.Errorf("kind = %q, want xlsx", rec.Kind)
	}
	if rec.Status != source.StatusIngested {
		t.Errorf("status = %q, want ingested", rec.Status)
	}
}

func TestCreateXLSXTool_DeliverFalse_PersistsOnly(t *testing.T) {
	tool, sender, _ := newCreateXLSXTest(t)
	ctx := WithUserID(context.Background(), "12345")
	args := map[string]any{
		"filename": "silent",
		"sheets": []any{
			map[string]any{"name": "x", "rows": []any{[]any{"a"}}},
		},
		"deliver": false,
	}
	out, err := tool.Execute(ctx, args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, `"delivered":false`) {
		t.Errorf("expected delivered=false in response: %s", out)
	}
	if len(sender.calls) != 0 {
		t.Errorf("sender should not be called when deliver=false, got %d calls", len(sender.calls))
	}
}

func TestCreateXLSXTool_DeliverWithoutUserContext_Errors(t *testing.T) {
	tool, sender, _ := newCreateXLSXTest(t)
	args := map[string]any{
		"filename": "x",
		"sheets":   []any{map[string]any{"name": "s", "rows": []any{[]any{"a"}}}},
	}
	if _, err := tool.Execute(context.Background(), args); err == nil {
		t.Error("expected error when deliver=true without user context")
	}
	if len(sender.calls) != 0 {
		t.Errorf("sender should not be called on context error, got %d", len(sender.calls))
	}
}

func TestCreateXLSXTool_NoSenderConfigured(t *testing.T) {
	store, err := source.NewStore(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	tool := NewCreateXLSXTool(store, nil)
	ctx := WithUserID(context.Background(), "12345")
	args := map[string]any{
		"filename": "x",
		"sheets":   []any{map[string]any{"name": "s", "rows": []any{[]any{"a"}}}},
	}
	if _, err := tool.Execute(ctx, args); err == nil {
		t.Error("expected error when sender is nil and deliver=true")
	}
	// With deliver=false the same call should succeed.
	args["deliver"] = false
	if _, err := tool.Execute(ctx, args); err != nil {
		t.Errorf("deliver=false with nil sender: %v", err)
	}
}

func TestCreateXLSXTool_RejectsMissingFilename(t *testing.T) {
	tool, _, _ := newCreateXLSXTest(t)
	args := map[string]any{
		"sheets": []any{map[string]any{"name": "s", "rows": []any{[]any{"a"}}}},
	}
	if _, err := tool.Execute(context.Background(), args); err == nil {
		t.Error("expected error for missing filename")
	}
}

func TestCreateXLSXTool_RejectsEmptySheets(t *testing.T) {
	tool, _, _ := newCreateXLSXTest(t)
	args := map[string]any{"filename": "x", "sheets": []any{}}
	if _, err := tool.Execute(context.Background(), args); err == nil {
		t.Error("expected error for empty sheets")
	}
}

func TestCreateXLSXTool_DedupsSecondCall(t *testing.T) {
	tool, _, _ := newCreateXLSXTest(t)
	ctx := WithUserID(context.Background(), "12345")
	args := map[string]any{
		"filename": "dup",
		"sheets":   []any{map[string]any{"name": "s", "rows": []any{[]any{"a"}}}},
		"deliver":  false,
	}
	out1, err := tool.Execute(ctx, args)
	if err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	out2, err := tool.Execute(ctx, args)
	if err != nil {
		t.Fatalf("second Execute: %v", err)
	}
	var r1, r2 map[string]any
	_ = json.Unmarshal([]byte(out1), &r1)
	_ = json.Unmarshal([]byte(out2), &r2)
	if r1["source_id"] != r2["source_id"] {
		t.Errorf("source_id changed across identical calls: %v vs %v", r1["source_id"], r2["source_id"])
	}
	if r2["duplicate"] != true {
		t.Errorf("second call duplicate = %v, want true", r2["duplicate"])
	}
}

func newCreateDOCXTest(t *testing.T) (*CreateDOCXTool, *stubDocSender, *source.Store) {
	t.Helper()
	store, err := source.NewStore(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("source.NewStore: %v", err)
	}
	sender := &stubDocSender{}
	tool := NewCreateDOCXTool(store, sender)
	if tool == nil {
		t.Fatal("NewCreateDOCXTool returned nil")
	}
	return tool, sender, store
}

func TestCreateDOCXTool_HappyPath_DeliversAndPersists(t *testing.T) {
	tool, sender, store := newCreateDOCXTest(t)
	ctx := WithUserID(context.Background(), "12345")
	args := map[string]any{
		"filename": "memo",
		"title":    "Quarterly Memo",
		"blocks": []any{
			map[string]any{"kind": "paragraph", "text": "Summary follows."},
			map[string]any{"kind": "heading", "level": 2.0, "text": "Highlights"},
			map[string]any{"kind": "bullet", "text": "Revenue up 12%"},
			map[string]any{"kind": "bullet", "text": "Two new customers"},
			map[string]any{"kind": "table", "rows": []any{
				[]any{"month", "revenue"},
				[]any{"jan", 100.0},
			}},
		},
		"caption": "Q1 memo",
	}
	out, err := tool.Execute(ctx, args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["filename"] != "memo.docx" {
		t.Errorf("filename = %v, want memo.docx", resp["filename"])
	}
	if resp["delivered"] != true {
		t.Errorf("delivered = %v, want true", resp["delivered"])
	}
	if len(sender.calls) != 1 {
		t.Fatalf("sender called %d times, want 1", len(sender.calls))
	}
	if sender.calls[0].filename != "memo.docx" || sender.calls[0].caption != "Q1 memo" {
		t.Errorf("sender call wrong: %+v", sender.calls[0])
	}

	// Persisted with KindDOCX + StatusIngested.
	id := resp["source_id"].(string)
	rec, err := store.Get(id)
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if rec.Kind != source.KindDOCX {
		t.Errorf("kind = %q, want docx", rec.Kind)
	}
	if rec.Status != source.StatusIngested {
		t.Errorf("status = %q, want ingested", rec.Status)
	}
}

func TestCreateDOCXTool_TitleOnly_NoBlocks(t *testing.T) {
	tool, _, _ := newCreateDOCXTest(t)
	ctx := WithUserID(context.Background(), "1")
	args := map[string]any{
		"filename": "tiny",
		"title":    "Just a title",
		"deliver":  false,
	}
	if _, err := tool.Execute(ctx, args); err != nil {
		t.Errorf("title-only spec should succeed: %v", err)
	}
}

func TestCreateDOCXTool_RejectsEmpty(t *testing.T) {
	tool, _, _ := newCreateDOCXTest(t)
	ctx := WithUserID(context.Background(), "1")
	args := map[string]any{"filename": "x", "deliver": false}
	if _, err := tool.Execute(ctx, args); err == nil {
		t.Error("expected error for empty spec (no title, no blocks)")
	}
}

func TestCreateDOCXTool_DeliverFalse_PersistsOnly(t *testing.T) {
	tool, sender, _ := newCreateDOCXTest(t)
	ctx := WithUserID(context.Background(), "1")
	args := map[string]any{
		"filename": "silent",
		"title":    "x",
		"deliver":  false,
	}
	if _, err := tool.Execute(ctx, args); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(sender.calls) != 0 {
		t.Errorf("sender invoked despite deliver=false")
	}
}

func TestCreateDOCXTool_RejectsBlockMissingKind(t *testing.T) {
	tool, _, _ := newCreateDOCXTest(t)
	ctx := WithUserID(context.Background(), "1")
	args := map[string]any{
		"filename": "x",
		"blocks":   []any{map[string]any{"text": "no kind here"}},
	}
	if _, err := tool.Execute(ctx, args); err == nil {
		t.Error("expected error for block with empty kind")
	}
}

func newCreatePDFTest(t *testing.T) (*CreatePDFTool, *stubDocSender, *source.Store) {
	t.Helper()
	store, err := source.NewStore(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("source.NewStore: %v", err)
	}
	sender := &stubDocSender{}
	tool := NewCreatePDFTool(store, sender)
	if tool == nil {
		t.Fatal("NewCreatePDFTool returned nil")
	}
	return tool, sender, store
}

func TestCreatePDFTool_HappyPath_DeliversAndPersists(t *testing.T) {
	tool, sender, store := newCreatePDFTest(t)
	ctx := WithUserID(context.Background(), "12345")
	args := map[string]any{
		"filename": "report",
		"title":    "Quarterly Report",
		"blocks": []any{
			map[string]any{"kind": "paragraph", "text": "Summary follows."},
			map[string]any{"kind": "heading", "level": 2.0, "text": "Highlights"},
			map[string]any{"kind": "bullet", "text": "Revenue up"},
			map[string]any{"kind": "table", "rows": []any{
				[]any{"month", "revenue"},
				[]any{"jan", 100.0},
			}},
		},
		"caption": "Q1",
	}
	out, err := tool.Execute(ctx, args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["filename"] != "report.pdf" {
		t.Errorf("filename = %v, want report.pdf", resp["filename"])
	}
	if resp["delivered"] != true {
		t.Errorf("delivered = %v, want true", resp["delivered"])
	}
	if len(sender.calls) != 1 {
		t.Fatalf("sender called %d times, want 1", len(sender.calls))
	}
	if sender.calls[0].filename != "report.pdf" || sender.calls[0].caption != "Q1" {
		t.Errorf("sender call wrong: %+v", sender.calls[0])
	}

	id := resp["source_id"].(string)
	rec, err := store.Get(id)
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if rec.Kind != source.KindPDFGen {
		t.Errorf("kind = %q, want pdf_generated", rec.Kind)
	}
	if rec.Status != source.StatusIngested {
		t.Errorf("status = %q, want ingested", rec.Status)
	}
}

func TestCreatePDFTool_TitleOnly(t *testing.T) {
	tool, _, _ := newCreatePDFTest(t)
	ctx := WithUserID(context.Background(), "1")
	args := map[string]any{
		"filename": "tiny",
		"title":    "Just a title",
		"deliver":  false,
	}
	if _, err := tool.Execute(ctx, args); err != nil {
		t.Errorf("title-only spec should succeed: %v", err)
	}
}

func TestCreatePDFTool_RejectsEmpty(t *testing.T) {
	tool, _, _ := newCreatePDFTest(t)
	ctx := WithUserID(context.Background(), "1")
	args := map[string]any{"filename": "x", "deliver": false}
	if _, err := tool.Execute(ctx, args); err == nil {
		t.Error("expected error for empty spec")
	}
}

func TestCreatePDFTool_DeliverFalse_PersistsOnly(t *testing.T) {
	tool, sender, _ := newCreatePDFTest(t)
	ctx := WithUserID(context.Background(), "1")
	args := map[string]any{
		"filename": "silent",
		"title":    "x",
		"deliver":  false,
	}
	if _, err := tool.Execute(ctx, args); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(sender.calls) != 0 {
		t.Errorf("sender invoked despite deliver=false")
	}
}

func TestCreatePDFTool_RejectsBlockMissingKind(t *testing.T) {
	tool, _, _ := newCreatePDFTest(t)
	ctx := WithUserID(context.Background(), "1")
	args := map[string]any{
		"filename": "x",
		"blocks":   []any{map[string]any{"text": "no kind"}},
	}
	if _, err := tool.Execute(ctx, args); err == nil {
		t.Error("expected error for block with empty kind")
	}
}

func TestStringifyCell(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{nil, ""},
		{"hello", "hello"},
		{true, "true"},
		{false, "false"},
		{42.0, "42"},
		{3.14, "3.14"},
		{int64(7), "7"}, // falls through to json.Marshal
	}
	for _, tc := range cases {
		got := stringifyCell(tc.in)
		if got != tc.want {
			t.Errorf("stringifyCell(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
