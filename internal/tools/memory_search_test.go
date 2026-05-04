package tools

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/source"
)

func TestSearchMemoryTool_MetadataAndValidation(t *testing.T) {
	if NewSearchMemoryTool(nil, nil, nil) != nil {
		t.Fatal("expected nil tool when every store is unavailable")
	}
	tool := NewSearchMemoryTool(nil, newTestSourceStore(t), nil)
	if tool.Name() != "search_memory" || tool.Description() == "" {
		t.Fatal("search_memory metadata is incomplete")
	}
	if tool.Parameters()["type"] != "object" {
		t.Fatal("search_memory parameters should be an object schema")
	}
	if _, err := tool.Execute(context.Background(), map[string]any{}); err == nil {
		t.Fatal("expected missing query error")
	}
	if _, err := tool.Execute(context.Background(), map[string]any{"query": "x", "scope": "files"}); err == nil {
		t.Fatal("expected unsupported scope error")
	}
}

func TestSearchMemoryTool_SearchesSourcesAndArchive(t *testing.T) {
	ctx := context.Background()
	sourceStore := newTestSourceStore(t)
	src, _, err := sourceStore.Put(ctx, source.PutInput{
		Kind:     source.KindText,
		Filename: "renewal-note.txt",
		MimeType: "text/plain",
		Bytes:    []byte("Contract renewal deadline is 2026-06-15. Ask legal before sending the final offer."),
	})
	if err != nil {
		t.Fatalf("Put source: %v", err)
	}
	sched := newTestSchedStore(t)
	archive, err := conversation.NewArchiveStore(sched.DB())
	if err != nil {
		t.Fatalf("NewArchiveStore: %v", err)
	}
	if err := archive.Append(ctx, conversation.Turn{
		ChatID:    42,
		UserID:    7,
		TurnIndex: 3,
		Role:      "user",
		Content:   "Remember that the contract deadline needs a legal review.",
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	tool := NewSearchMemoryTool(nil, sourceStore, archive)
	out, err := tool.Execute(ctx, map[string]any{"query": "contract deadline", "limit": float64(5)})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{
		"Memory evidence",
		"Evidence envelope:",
		"[source]",
		src.ID,
		"renewal-note.txt",
		"[archive]",
		"conversation:",
		"contract deadline",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
	envelope := parseMemoryEvidenceEnvelope(t, out)
	if envelope.Query != "contract deadline" {
		t.Fatalf("unexpected evidence query: %q", envelope.Query)
	}
	if !hasEvidenceItem(envelope.Items, "source", src.ID, "renewal-note.txt", 0) {
		t.Fatalf("missing structured source evidence: %#v", envelope.Items)
	}
	if !hasEvidenceKind(envelope.Items, "archive") {
		t.Fatalf("missing structured archive evidence: %#v", envelope.Items)
	}
}

func TestSearchMemoryTool_OCRSourcePageNumber(t *testing.T) {
	ctx := context.Background()
	sourceStore := newTestSourceStore(t)
	src, _, err := sourceStore.Put(ctx, source.PutInput{
		Kind:     source.KindPDF,
		Filename: "agreement.pdf",
		MimeType: "application/pdf",
		Bytes:    []byte("%PDF fake"),
	})
	if err != nil {
		t.Fatalf("Put source: %v", err)
	}
	ocrBody := "# Source OCR: agreement.pdf\n\n## Page 1\n\nOpening terms.\n\n## Page 2\n\nThe cancellation clause requires thirty days notice."
	if err := os.WriteFile(sourceStore.Path(src.ID, "ocr.md"), []byte(ocrBody), 0o644); err != nil {
		t.Fatalf("write ocr.md: %v", err)
	}

	tool := NewSearchMemoryTool(nil, sourceStore, nil)
	out, err := tool.Execute(ctx, map[string]any{"query": "cancellation clause", "scope": "sources"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "page=2") || !strings.Contains(out, "agreement.pdf") {
		t.Fatalf("expected OCR page evidence:\n%s", out)
	}
	envelope := parseMemoryEvidenceEnvelope(t, out)
	if !hasEvidenceItem(envelope.Items, "source", src.ID, "agreement.pdf", 2) {
		t.Fatalf("expected structured page evidence:\n%#v", envelope.Items)
	}
}

func TestSearchMemoryTool_ArchiveScopeAndChatFilter(t *testing.T) {
	ctx := context.Background()
	sourceStore := newTestSourceStore(t)
	if _, _, err := sourceStore.Put(ctx, source.PutInput{
		Kind:     source.KindText,
		Filename: "source.txt",
		MimeType: "text/plain",
		Bytes:    []byte("private trip plan"),
	}); err != nil {
		t.Fatalf("Put source: %v", err)
	}
	sched := newTestSchedStore(t)
	archive, err := conversation.NewArchiveStore(sched.DB())
	if err != nil {
		t.Fatalf("NewArchiveStore: %v", err)
	}
	for _, turn := range []conversation.Turn{
		{ChatID: 10, UserID: 1, TurnIndex: 1, Role: "user", Content: "private trip plan for Berlin"},
		{ChatID: 20, UserID: 1, TurnIndex: 1, Role: "user", Content: "private trip plan for Rome"},
	} {
		if err := archive.Append(ctx, turn); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	tool := NewSearchMemoryTool(nil, sourceStore, archive)
	out, err := tool.Execute(ctx, map[string]any{"query": "private trip", "scope": "archive", "chat_id": float64(10)})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(out, "[source]") {
		t.Fatalf("archive scope should not include sources:\n%s", out)
	}
	if !strings.Contains(out, "chat=10") || strings.Contains(out, "chat=20") {
		t.Fatalf("chat filter not respected:\n%s", out)
	}
}

type testMemoryEvidenceEnvelope struct {
	Query    string                   `json:"query"`
	Items    []testMemoryEvidenceItem `json:"items"`
	Warnings []string                 `json:"warnings"`
}

type testMemoryEvidenceItem struct {
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Title string `json:"title"`
	Page  int    `json:"page"`
}

func parseMemoryEvidenceEnvelope(t *testing.T, out string) testMemoryEvidenceEnvelope {
	t.Helper()
	const marker = "Evidence envelope:\n"
	idx := strings.LastIndex(out, marker)
	if idx < 0 {
		t.Fatalf("missing evidence envelope:\n%s", out)
	}
	var envelope testMemoryEvidenceEnvelope
	if err := json.Unmarshal([]byte(out[idx+len(marker):]), &envelope); err != nil {
		t.Fatalf("invalid evidence envelope JSON: %v\n%s", err, out)
	}
	return envelope
}

func hasEvidenceKind(items []testMemoryEvidenceItem, kind string) bool {
	for _, item := range items {
		if item.Kind == kind {
			return true
		}
	}
	return false
}

func hasEvidenceItem(items []testMemoryEvidenceItem, kind, id, title string, page int) bool {
	for _, item := range items {
		if item.Kind == kind && item.ID == id && item.Title == title && item.Page == page {
			return true
		}
	}
	return false
}
