package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/storage/memoryindex"
	"github.com/aura/aura/internal/storage/search"
	"github.com/aura/aura/internal/storage/sources/store"
	"github.com/aura/aura/internal/wiki"
)

func TestSearchMemoryTool_MetadataAndValidation(t *testing.T) {
	if NewSearchMemoryTool(nil, nil) != nil {
		t.Fatal("expected nil tool when every store is unavailable")
	}
	tool := NewSearchMemoryTool(nil, newTestMemoryIndex(t))
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

	index := newTestMemoryIndex(t)
	if _, err := memoryindex.Rebuild(ctx, index, memoryindex.RebuildInput{Sources: sourceStore, Archive: archive}); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	tool := NewSearchMemoryTool(nil, index)
	out, err := tool.Execute(ctx, map[string]any{"query": "contract deadline", "limit": float64(5)})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{
		"memory hit(s) for",
		"[source] " + src.ID,
		"renewal-note.txt",
		"handle=source:" + src.ID,
		"[archive] conversation:",
		"contract deadline",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, `"items"`) || strings.Contains(out, "Evidence envelope") {
		t.Fatalf("output should not contain JSON envelope:\n%s", out)
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

	index := newTestMemoryIndex(t)
	if _, err := memoryindex.Rebuild(ctx, index, memoryindex.RebuildInput{Sources: sourceStore}); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	tool := NewSearchMemoryTool(nil, index)
	out, err := tool.Execute(ctx, map[string]any{"query": "cancellation clause", "scope": "sources"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "page=2") || !strings.Contains(out, "agreement.pdf") {
		t.Fatalf("expected OCR page evidence:\n%s", out)
	}
	if !strings.Contains(out, "handle=source:"+src.ID+"#page=2") {
		t.Fatalf("expected page-stable source handle:\n%s", out)
	}
}

func TestSearchMemoryTool_GraphNodeEvidence(t *testing.T) {
	// search_memory must surface graph_node results (the synthetic "card"
	// documents produced by buildGraphDocuments) verbatim, including the
	// [[slug]] citation form expected by the LLM. We feed the tool a
	// pre-built graph_node Result so the test exercises the formatter
	// without spinning up a live wiki engine.
	tool := NewSearchMemoryTool(fakeMemoryWikiSearch{
		indexed: true,
		results: []search.Result{{
			Kind:    "graph_node",
			Slug:    "alpha-contract",
			Title:   "Alpha Contract",
			Content: "Graph node [[alpha-contract]] backlinks: beta-legal-review",
			Score:   0.9,
		}},
	}, nil)
	out, err := tool.Execute(context.Background(), map[string]any{"query": "backlinks beta", "scope": "wiki"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "[graph_node] [[alpha-contract]]") {
		t.Fatalf("expected graph_node evidence:\n%s", out)
	}
}

type fakeMemoryWikiSearch struct {
	indexed bool
	results []search.Result
}

func (f fakeMemoryWikiSearch) IsIndexed() bool { return f.indexed }

func (f fakeMemoryWikiSearch) Search(context.Context, string, int) ([]search.Result, error) {
	return f.results, nil
}

func TestSearchMemoryToolAcceptsWikiSearchInterface(t *testing.T) {
	tool := NewSearchMemoryTool(fakeMemoryWikiSearch{
		indexed: true,
		results: []search.Result{{
			Kind:    "wiki_page",
			Slug:    "memory-boundary",
			Title:   "Memory Boundary",
			Content: "Search memory should depend on the wiki search interface.",
			Score:   0.8,
		}},
	}, nil)
	if tool == nil {
		t.Fatal("expected search_memory tool")
	}
	out, err := tool.Execute(context.Background(), map[string]any{"query": "memory boundary", "scope": "wiki"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "[wiki] [[memory-boundary]]") {
		t.Fatalf("output = %q", out)
	}
}

func TestSearchMemoryToolCalibratesMixedScoresBeforeMerging(t *testing.T) {
	ctx := context.Background()
	sourceStore := newTestSourceStore(t)
	if _, _, err := sourceStore.Put(ctx, source.PutInput{
		Kind:     source.KindText,
		Filename: "very-repetitive-source.txt",
		MimeType: "text/plain",
		Bytes:    []byte(strings.Repeat("memory boundary ", 40)),
	}); err != nil {
		t.Fatalf("Put source: %v", err)
	}
	index := newTestMemoryIndex(t)
	if _, err := memoryindex.Rebuild(ctx, index, memoryindex.RebuildInput{Sources: sourceStore}); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	tool := NewSearchMemoryTool(fakeMemoryWikiSearch{
		indexed: true,
		results: []search.Result{{
			Kind:    "wiki_page",
			Slug:    "memory-boundary",
			Title:   "Memory Boundary",
			Content: "Memory boundary canonical page.",
			Score:   0.75,
		}},
	}, index)

	out, err := tool.Execute(ctx, map[string]any{"query": "memory boundary", "limit": float64(3)})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	wikiIdx := strings.Index(out, "[wiki] [[memory-boundary]]")
	sourceIdx := strings.Index(out, "[source]")
	if wikiIdx < 0 || sourceIdx < 0 {
		t.Fatalf("expected wiki and source results:\n%s", out)
	}
	if wikiIdx > sourceIdx {
		t.Fatalf("wiki vector score was buried by raw source lexical score:\n%s", out)
	}
}

func TestSearchMemoryToolWithTimeoutPassesDeadlineToWikiSearch(t *testing.T) {
	wiki := &deadlineMemoryWikiSearch{results: []search.Result{{
		Kind:    "wiki_page",
		Slug:    "bounded-memory",
		Title:   "Bounded Memory",
		Content: "search_memory calls should receive a bounded context.",
		Score:   0.9,
	}}}
	tool := NewSearchMemoryToolWithTimeout(wiki, nil, 50*time.Millisecond)
	out, err := tool.Execute(context.Background(), map[string]any{"query": "bounded memory", "scope": "wiki"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "[wiki] [[bounded-memory]]") {
		t.Fatalf("output = %q", out)
	}
	if wiki.deadline.IsZero() {
		t.Fatal("wiki search did not receive a deadline")
	}
	if remaining := time.Until(wiki.deadline); remaining <= 0 || remaining > time.Second {
		t.Fatalf("deadline remaining = %s, want bounded future deadline", remaining)
	}
}

func TestSearchMemoryToolWithTimeoutReturnsWarningOnWikiTimeout(t *testing.T) {
	tool := NewSearchMemoryToolWithTimeout(blockingMemoryWikiSearch{}, nil, 10*time.Millisecond)
	start := time.Now()
	out, err := tool.Execute(context.Background(), map[string]any{"query": "slow qdrant", "scope": "wiki"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Execute took %s, want bounded timeout", elapsed)
	}
	if !strings.Contains(out, "wiki search timed out") {
		t.Fatalf("expected timeout warning:\n%s", out)
	}
	if !strings.Contains(out, "No memory found") {
		t.Fatalf("expected no-results sentinel:\n%s", out)
	}
}

type deadlineMemoryWikiSearch struct {
	results  []search.Result
	deadline time.Time
}

func (f *deadlineMemoryWikiSearch) IsIndexed() bool { return true }

func (f *deadlineMemoryWikiSearch) Search(ctx context.Context, _ string, _ int) ([]search.Result, error) {
	f.deadline, _ = ctx.Deadline()
	return f.results, nil
}

type blockingMemoryWikiSearch struct{}

func (blockingMemoryWikiSearch) IsIndexed() bool { return true }

func (blockingMemoryWikiSearch) Search(ctx context.Context, _ string, _ int) ([]search.Result, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

type fakeCompactMemorySearch struct {
	docs []memoryindex.Document
}

func (f fakeCompactMemorySearch) Search(_ context.Context, _ string, filter memoryindex.Filter) ([]memoryindex.Document, error) {
	var out []memoryindex.Document
	for _, doc := range f.docs {
		if len(filter.Kinds) > 0 {
			match := false
			for _, kind := range filter.Kinds {
				if doc.Kind == kind {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}
		if filter.ChatID > 0 && doc.Kind == memoryindex.KindArchive && doc.ChatID != filter.ChatID {
			continue
		}
		out = append(out, doc)
	}
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

func TestSearchMemoryToolAcceptsCompactSearchInterface(t *testing.T) {
	tool := NewSearchMemoryTool(nil, fakeCompactMemorySearch{docs: []memoryindex.Document{{
		ID:     "archive:12",
		Kind:   memoryindex.KindArchive,
		Title:  "chat=42 turn=3",
		Body:   "Archive boundary should not require concrete SQLite storage.",
		Handle: "conversation:12",
		ChatID: 42,
		Score:  1,
	}}})
	if tool == nil {
		t.Fatal("expected search_memory tool")
	}
	out, err := tool.Execute(context.Background(), map[string]any{"query": "archive boundary", "scope": "archive"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "[archive] conversation:12") {
		t.Fatalf("output = %q", out)
	}
}

func TestSearchMemoryToolUsesOneCompactSearchForAllScope(t *testing.T) {
	compact := &countingCompactMemorySearch{}
	tool := NewSearchMemoryTool(nil, compact)
	out, err := tool.Execute(context.Background(), map[string]any{"query": "hybrid memory", "scope": "all"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if compact.calls != 1 {
		t.Fatalf("compact calls = %d, want 1; output=%q", compact.calls, out)
	}
	if !sameStringSetForTools(compact.kinds, []string{memoryindex.KindSource, memoryindex.KindArchive, memoryindex.KindProposal}) {
		t.Fatalf("compact kinds = %#v", compact.kinds)
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

	index := newTestMemoryIndex(t)
	if _, err := memoryindex.Rebuild(ctx, index, memoryindex.RebuildInput{Sources: sourceStore, Archive: archive}); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	tool := NewSearchMemoryTool(nil, index)
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

type countingCompactMemorySearch struct {
	calls int
	kinds []string
}

func (f *countingCompactMemorySearch) Search(_ context.Context, _ string, filter memoryindex.Filter) ([]memoryindex.Document, error) {
	f.calls++
	f.kinds = append([]string(nil), filter.Kinds...)
	return nil, nil
}

func sameStringSetForTools(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := map[string]int{}
	for _, value := range got {
		seen[value]++
	}
	for _, value := range want {
		if seen[value] == 0 {
			return false
		}
		seen[value]--
	}
	return true
}

func writeMemoryTestPage(t *testing.T, dir string, page *wiki.Page) {
	t.Helper()
	data, err := wiki.MarshalMD(page)
	if err != nil {
		t.Fatalf("MarshalMD: %v", err)
	}
	path := filepath.Join(dir, wiki.Slug(page.Title)+".md")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func newTestMemoryIndex(t *testing.T) *memoryindex.Store {
	t.Helper()
	sched := newTestSchedStore(t)
	store, err := memoryindex.NewStore(sched.DB())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return store
}

func memoryKeywordEmbedding(_ context.Context, text string) ([]float32, error) {
	lower := strings.ToLower(text)
	keywords := []string{"backlinks", "beta", "project", "contract"}
	vec := make([]float32, len(keywords)+1)
	for i, keyword := range keywords {
		if strings.Contains(lower, keyword) {
			vec[i] = 1
		}
	}
	empty := true
	for _, v := range vec {
		if v != 0 {
			empty = false
			break
		}
	}
	if empty {
		vec[len(vec)-1] = 1
	}
	return vec, nil
}

// Envelope helpers were removed when search_memory dropped the JSON
// "Evidence envelope:" appendix. Tests now assert on the plain-text output.

func TestRelevanceTimesRecencyAppliesHalfLife(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// fresh archive (age 0d) full weight at the 30d half-life
	fresh := relevanceTimesRecencyWithHalfLife(0.9, now, now, recencyHalfLifeArchiveDays)
	if fresh < 0.89 || fresh > 0.91 {
		t.Fatalf("fresh archive score = %f, want ~0.9", fresh)
	}
	// archive aged one half-life (~30d) should halve
	thirty := relevanceTimesRecencyWithHalfLife(0.9, now.AddDate(0, 0, -30), now, recencyHalfLifeArchiveDays)
	if thirty < 0.40 || thirty > 0.50 {
		t.Fatalf("30d archive score = %f, want ~0.45", thirty)
	}
	// wiki aged 30d should barely move at the 180d half-life
	wiki30 := relevanceTimesRecencyWithHalfLife(0.9, now.AddDate(0, 0, -30), now, recencyHalfLifeWikiDays)
	if wiki30 < 0.75 || wiki30 > 0.85 {
		t.Fatalf("30d wiki score = %f, want ~0.79", wiki30)
	}
}

func TestRelevanceTimesRecencyZeroUpdatedAtKeepsRelevance(t *testing.T) {
	got := relevanceTimesRecencyWithHalfLife(0.7, time.Time{}, time.Now(), recencyHalfLifeArchiveDays)
	if got != 0.7 {
		t.Fatalf("zero updated_at should keep relevance, got %f", got)
	}
}

func TestIsArchiveKindClassifiesOperationalTier(t *testing.T) {
	for _, kind := range []string{"archive", "proposal"} {
		if !isArchiveKind(kind) {
			t.Errorf("isArchiveKind(%q) = false, want true", kind)
		}
	}
	for _, kind := range []string{"wiki", "wiki_page", "graph_node", "graph_index", "source"} {
		if isArchiveKind(kind) {
			t.Errorf("isArchiveKind(%q) = true, want false (curated tier)", kind)
		}
	}
}

func TestSearchMemoryToolRanksFreshHitsAboveStaleOnesAtEqualRelevance(t *testing.T) {
	now := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	tool := NewSearchMemoryTool(nil, fakeCompactMemorySearch{docs: []memoryindex.Document{
		{
			ID:        "archive:old",
			Kind:      memoryindex.KindArchive,
			Title:     "old chat",
			Body:      "stale note",
			Handle:    "conversation:old",
			ChatID:    1,
			Score:     0.8,
			UpdatedAt: now.AddDate(0, 0, -90), // 90 days old → recency ≈ 0.125
		},
		{
			ID:        "archive:new",
			Kind:      memoryindex.KindArchive,
			Title:     "new chat",
			Body:      "fresh note",
			Handle:    "conversation:new",
			ChatID:    1,
			Score:     0.8,
			UpdatedAt: now.AddDate(0, 0, -1), // 1 day old → recency ≈ 0.977
		},
	}})
	tool.now = func() time.Time { return now }
	out, err := tool.Execute(context.Background(), map[string]any{"query": "note", "scope": "archive"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	freshIdx := strings.Index(out, "conversation:new")
	staleIdx := strings.Index(out, "conversation:old")
	if freshIdx < 0 || staleIdx < 0 {
		t.Fatalf("missing hits in output:\n%s", out)
	}
	if freshIdx > staleIdx {
		t.Fatalf("fresh hit ranked below stale at equal relevance:\n%s", out)
	}
}
