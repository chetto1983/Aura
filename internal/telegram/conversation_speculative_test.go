package telegram

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/orchestration"
	"github.com/aura/aura/internal/search"
)

func TestSpeculativeSearchUsesConfiguredTimeout(t *testing.T) {
	repo := &recordingSearch{indexed: true, results: []search.Result{{
		Kind:    "wiki_page",
		Slug:    "aura-memory",
		Title:   "Aura Memory",
		Content: "Qdrant-backed memory should stay bounded.",
		Score:   0.9,
	}}}

	out := runSpeculativeSearch(context.Background(), repo, "aura memory", 25, slog.New(slog.NewTextHandler(io.Discard, nil)), "u1")
	if out == "" {
		t.Fatal("expected formatted speculative search context")
	}
	if repo.deadline.IsZero() {
		t.Fatal("search context did not receive a deadline")
	}
	remaining := time.Until(repo.deadline)
	if remaining <= 0 || remaining > 100*time.Millisecond {
		t.Fatalf("deadline remaining = %s, want a short configured timeout", remaining)
	}
	if repo.topK != 5 {
		t.Fatalf("topK = %d, want 5", repo.topK)
	}
}

func TestSpeculativeSearchTimeoutFallsBackToDefault(t *testing.T) {
	if got := speculativeSearchTimeout(0); got != time.Duration(config.DefaultSpeculativeSearchTimeoutMS)*time.Millisecond {
		t.Fatalf("timeout = %s", got)
	}
}

func TestHotPathManifestsAndSwarmAreExplicitOnly(t *testing.T) {
	if turnNeedsSkillManifest("ciao, come stai?") {
		t.Fatal("generic prompt should not inject skill manifest")
	}
	if !turnNeedsSkillManifest("usa la skill documenti") {
		t.Fatal("explicit skill prompt should inject skill manifest")
	}
	if turnAllowsSwarm("analizza tutta la wiki") {
		t.Fatal("broad prompt should not expose swarm without explicit request")
	}
	if !turnAllowsSwarm("usa subagenti paralleli per analizzare la wiki") {
		t.Fatal("explicit subagent prompt should expose swarm")
	}
}

func TestComposeTurnRetrievalCapsuleSkipsGenericTurns(t *testing.T) {
	repo := &recordingSearch{indexed: true, results: []search.Result{{
		Kind:    "wiki_page",
		Slug:    "aura-operating-memory",
		Title:   "Aura Operating Memory",
		Content: "Aura should use compact context.",
		Score:   0.9,
	}}}

	got := composeTurnRetrievalCapsule(context.Background(), repo, "", "ciao come stai?", 25, slog.New(slog.NewTextHandler(io.Discard, nil)), "u1")
	if got.Text != "" {
		t.Fatalf("generic turn retrieval capsule = %q, want empty", got.Text)
	}
	if !repo.deadline.IsZero() {
		t.Fatal("generic turn should not run speculative search")
	}
}

func TestComposeTurnRetrievalCapsuleSearchesMemoryTurnsWithoutGraphOrLog(t *testing.T) {
	repo := &recordingSearch{indexed: true, results: []search.Result{{
		Kind:    "wiki_page",
		Slug:    "aura-operating-memory",
		Title:   "Aura Operating Memory",
		Content: "Aura should use compact context.",
		Score:   0.9,
	}}}
	got := composeTurnRetrievalCapsule(context.Background(), repo, t.TempDir(), "cosa ricordi di Aura?", 25, slog.New(slog.NewTextHandler(io.Discard, nil)), "u1")
	for _, want := range []string{
		"## Retrieval Capsule",
		"### Route",
		"retrieve",
		"[[aura-operating-memory]]",
	} {
		if !strings.Contains(got.Text, want) {
			t.Fatalf("retrieval capsule missing %q:\n%s", want, got.Text)
		}
	}
	for _, notWant := range []string{"### Graph Context", "### Recent Wiki Log", "update"} {
		if strings.Contains(got.Text, notWant) {
			t.Fatalf("retrieval capsule includes hot-path context %q:\n%s", notWant, got.Text)
		}
	}
}

func TestComposeTurnRetrievalCapsuleDoesNotLoadGraphForGraphTurns(t *testing.T) {
	repo := &recordingSearch{indexed: true, results: []search.Result{{
		Kind:    "wiki_page",
		Slug:    "aura-operating-memory",
		Title:   "Aura Operating Memory",
		Content: "Aura should use compact context.",
		Score:   0.9,
	}}}
	got := composeTurnRetrievalCapsule(context.Background(), repo, t.TempDir(), "mostrami il grafo della wiki", 25, slog.New(slog.NewTextHandler(io.Discard, nil)), "u1")
	for _, want := range []string{"## Retrieval Capsule", "retrieve", "[[aura-operating-memory]]"} {
		if !strings.Contains(got.Text, want) {
			t.Fatalf("graph retrieval capsule missing %q:\n%s", want, got.Text)
		}
	}
	if strings.Contains(got.Text, "### Graph Context") || strings.Contains(got.Text, "- Pages: 22") {
		t.Fatalf("graph turn loaded graph context instead of search evidence:\n%s", got.Text)
	}
}

func TestComposeTurnRetrievalCapsuleMarksDocumentTurnsAsProduce(t *testing.T) {
	repo := &recordingSearch{indexed: true, results: []search.Result{{
		Kind:    "wiki_page",
		Slug:    "aura-documents",
		Title:   "Aura Documents",
		Content: "Document generation should be boring.",
		Score:   0.9,
	}}}

	got := composeTurnRetrievalCapsule(context.Background(), repo, "", "crea un documento word sui documenti e note", 25, slog.New(slog.NewTextHandler(io.Discard, nil)), "u1")
	for _, want := range []string{"## Retrieval Capsule", "produce", "[[aura-documents]]"} {
		if !strings.Contains(got.Text, want) {
			t.Fatalf("produce retrieval capsule missing %q:\n%s", want, got.Text)
		}
	}
	if !got.HasEvidence || !got.SuppressSearchMemory {
		t.Fatalf("produce capsule with evidence metadata = %+v, want evidence and search suppression", got)
	}
}

func TestComposeTurnRetrievalCapsuleKeepsDocumentTurnsBroadWhenEmpty(t *testing.T) {
	repo := &recordingSearch{indexed: true}

	got := composeTurnRetrievalCapsule(context.Background(), repo, "", "crea un documento word sui documenti e note", 25, slog.New(slog.NewTextHandler(io.Discard, nil)), "u1")
	for _, want := range []string{"## Retrieval Capsule", "produce", "No compact wiki evidence was found"} {
		if !strings.Contains(got.Text, want) {
			t.Fatalf("empty produce retrieval capsule missing %q:\n%s", want, got.Text)
		}
	}
	if got.HasEvidence || got.SuppressSearchMemory {
		t.Fatalf("empty broad document capsule metadata = %+v, want no evidence and search_memory available", got)
	}
}

func TestComposeTurnRetrievalCapsuleCanSuppressSearchForPromptOnlyDocuments(t *testing.T) {
	repo := &recordingSearch{indexed: true}

	got := composeTurnRetrievalCapsule(context.Background(), repo, "", "crea un documento word con una checklist per il deploy", 25, slog.New(slog.NewTextHandler(io.Discard, nil)), "u1")
	if got.Text == "" {
		t.Fatal("prompt-only document turn should still receive a route capsule")
	}
	if got.HasEvidence || !got.SuppressSearchMemory {
		t.Fatalf("prompt-only document capsule metadata = %+v, want no evidence and search suppression", got)
	}
}

func TestRuntimeToolsetForTurnRemovesSearchMemoryWhenDocumentCapsuleExists(t *testing.T) {
	got, err := runtimeToolsetForTurn(orchestration.ToolsetDocument, turnRetrievalCapsule{
		Text:                 "## Retrieval Capsule\n\n### Evidence\nx",
		HasEvidence:          true,
		SuppressSearchMemory: true,
	}).Tools(orchestration.ToolsetContext{
		Toolset: orchestration.ToolsetDocument,
		Availability: orchestration.Availability{
			Swarm:          true,
			WorkspaceFiles: true,
		},
	})
	if err != nil {
		t.Fatalf("runtimeToolsetForTurn: %v", err)
	}
	if containsString(got, "search_memory") {
		t.Fatalf("document capsule toolset exposed search_memory: %+v", got)
	}
	for _, want := range []string{"create_docx", "create_xlsx", "create_pdf"} {
		if !containsString(got, want) {
			t.Fatalf("document capsule toolset missing %q: %+v", want, got)
		}
	}
}

func TestRuntimeToolsetForTurnKeepsSearchMemoryWithoutCapsule(t *testing.T) {
	got, err := runtimeToolsetForTurn(orchestration.ToolsetDocument, turnRetrievalCapsule{}).Tools(orchestration.ToolsetContext{
		Toolset: orchestration.ToolsetDocument,
	})
	if err != nil {
		t.Fatalf("runtimeToolsetForTurn: %v", err)
	}
	if !containsString(got, "search_memory") {
		t.Fatalf("document toolset without capsule should expose search_memory: %+v", got)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

type recordingSearch struct {
	indexed  bool
	results  []search.Result
	deadline time.Time
	topK     int
}

func (r *recordingSearch) IsIndexed() bool { return r.indexed }

func (r *recordingSearch) Search(ctx context.Context, _ string, topK int) ([]search.Result, error) {
	r.topK = topK
	r.deadline, _ = ctx.Deadline()
	return r.results, nil
}
