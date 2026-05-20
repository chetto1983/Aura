package tools

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/storage/freshness"
	"github.com/aura/aura/internal/storage/memoryindex"
	"github.com/aura/aura/internal/storage/search"
	"github.com/aura/aura/internal/storage/sources/store"
	"github.com/aura/aura/internal/wiki"
)

// fakeFreshnessGetter satisfies the freshnessGetter interface for tests.
type fakeFreshnessGetter struct {
	row   freshness.Row
	found bool
	err   error
}

func (f fakeFreshnessGetter) Get(_ context.Context, _ string) (freshness.Row, bool, error) {
	return f.row, f.found, f.err
}

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
	start := strings.Index(ocrBody, "The cancellation clause requires thirty days notice.")
	end := start + len("The cancellation clause requires thirty days notice.")
	if start < 0 {
		t.Fatal("test fixture missing page 2 body")
	}
	wantSpan := "span=bytes=" + strconv.Itoa(start) + "-" + strconv.Itoa(end)
	if !strings.Contains(out, wantSpan) {
		t.Fatalf("expected source byte span %q:\n%s", wantSpan, out)
	}
	wantFollowUp := "follow_up=source(action=read,source_id=" + src.ID + ",mode=ocr,byte_start=" + strconv.Itoa(start) + ",byte_end=" + strconv.Itoa(end) + ")"
	if !strings.Contains(out, wantFollowUp) {
		t.Fatalf("expected span-aware follow_up %q:\n%s", wantFollowUp, out)
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

func TestSearchMemoryTool_WikiFrontmatterMetadata(t *testing.T) {
	created := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	tool := NewSearchMemoryTool(fakeMemoryWikiSearch{
		indexed: true,
		results: []search.Result{{
			Kind:          "wiki_page",
			Slug:          "phase07f-metadata",
			Title:         "Phase07F Metadata",
			Content:       "Phase07F metadata marker.",
			Score:         0.9,
			CreatedAt:     created,
			SchemaVersion: wiki.CurrentSchemaVersion,
			PromptVersion: "ingest_v2",
			Unversioned:   true,
			Sources:       []string{"src_phase07f", "https://example.test/ref"},
		}},
	}, nil)
	out, err := tool.Execute(context.Background(), map[string]any{"query": "phase07f metadata", "scope": "wiki"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{
		"[wiki] [[phase07f-metadata]]",
		"schema=3",
		"prompt=ingest_v2",
		"created=2026-05-01T10:00:00Z",
		"unversioned=true",
		"sources=[src_phase07f,https://example.test/ref]",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, `"schema_version"`) || strings.Contains(out, "Evidence envelope") {
		t.Fatalf("output should stay compact markdown, got:\n%s", out)
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
	tool := NewSearchMemoryToolConfigured(wiki, nil, 50*time.Millisecond, recencyHalfLifeWikiDays, recencyHalfLifeArchiveDays)
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
	tool := NewSearchMemoryToolConfigured(blockingMemoryWikiSearch{}, nil, 10*time.Millisecond, recencyHalfLifeWikiDays, recencyHalfLifeArchiveDays)
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
		if filter.SourceID != "" && doc.SourceID != filter.SourceID {
			continue
		}
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

func TestSearchMemoryToolRejectsTypedTierScopes(t *testing.T) {
	tool := NewSearchMemoryTool(nil, fakeCompactMemorySearch{})
	for _, scope := range []string{memoryindex.KindUserMemory, memoryindex.KindOperational} {
		_, err := tool.Execute(context.Background(), map[string]any{"query": "typed tier", "scope": scope})
		if err == nil {
			t.Fatalf("scope %q should require its task-level recall tool instead of search_memory", scope)
		}
		if !strings.Contains(err.Error(), "unsupported scope") {
			t.Fatalf("scope %q err = %v, want unsupported scope", scope, err)
		}
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

func TestSearchMemoryToolSourceIDFilter(t *testing.T) {
	srcAlpha := memoryindex.Document{
		ID:       "source:src_alpha#page=1",
		Kind:     memoryindex.KindSource,
		Title:    "alpha-doc.txt",
		Body:     "alpha shared content",
		Handle:   "source:src_alpha#page=1",
		SourceID: "src_alpha",
		Score:    1,
	}
	srcBeta := memoryindex.Document{
		ID:       "source:src_beta#page=1",
		Kind:     memoryindex.KindSource,
		Title:    "beta-doc.txt",
		Body:     "beta shared content",
		Handle:   "source:src_beta#page=1",
		SourceID: "src_beta",
		Score:    1,
	}
	tool := NewSearchMemoryTool(nil, fakeCompactMemorySearch{docs: []memoryindex.Document{srcAlpha, srcBeta}})
	out, err := tool.Execute(context.Background(), map[string]any{
		"query":     "shared content",
		"scope":     "sources",
		"source_id": "src_alpha",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "src_alpha") {
		t.Fatalf("expected src_alpha in output:\n%s", out)
	}
	if strings.Contains(out, "src_beta") {
		t.Fatalf("src_beta should be filtered out:\n%s", out)
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

func TestSearchMemoryTool_ToolRoleExcludedFromDefaultSearch(t *testing.T) {
	// Regression for the 2026-05-15 live incident: search_memory was returning
	// raw tool-output rows (tool_schemas.json dumps, AGENT.md content, stdout
	// snippets) because IndexingTurnAppender.Append wrote every role to
	// compact_memory_documents. US-K02 added the eligibility gate (role=tool →
	// skip); this test proves the gate holds end-to-end through search_memory.
	ctx := context.Background()

	sched := newTestSchedStore(t)
	archive, err := conversation.NewArchiveStore(sched.DB())
	if err != nil {
		t.Fatalf("NewArchiveStore: %v", err)
	}
	index := newTestMemoryIndex(t)
	appender := memoryindex.NewIndexingTurnAppender(archive, index)

	// 4-row fixture: the raw tool row and assistant tool-call row contain
	// "tool_schemas.json". Both are excluded from compact_memory_documents,
	// so neither can surface in search_memory results.
	for _, turn := range []conversation.Turn{
		{ChatID: 1, UserID: 1, TurnIndex: 1, Role: "user", Content: "What capabilities do you have?"},
		{ChatID: 1, UserID: 1, TurnIndex: 2, Role: "assistant", Content: "I have many capabilities at my disposal."},
		{ChatID: 1, UserID: 1, TurnIndex: 3, Role: "tool", Content: `tool_schemas.json dump: [{"name":"search_memory"}]`},
		{ChatID: 1, UserID: 1, TurnIndex: 4, Role: "assistant", Content: "checking tool_schemas.json", ToolCalls: `[{"id":"call_1","function":{"name":"search_memory"}}]`},
	} {
		if err := appender.Append(ctx, turn); err != nil {
			t.Fatalf("Append role=%s: %v", turn.Role, err)
		}
	}

	tool := NewSearchMemoryTool(nil, index)
	out, err := tool.Execute(ctx, map[string]any{"query": "tool_schemas.json"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// The tool row is the only document that would match "tool_schemas.json";
	// the eligibility gate must have excluded it, so 0 archive results expected.
	if strings.Contains(out, "[archive]") {
		t.Fatalf("search_memory surfaced archive rows for tool_schemas.json query (2026-05-15 incident regression):\n%s", out)
	}
	if strings.Contains(out, "memory hit(s)") {
		t.Fatalf("expected 0 memory hits, but got results for tool_schemas.json:\n%s", out)
	}
}

// TestSearchMemoryTool_ScoreComponentsAndFollowUp verifies that
// formatMemoryResults emits [exact=… fts=… vector=…] when any component is
// non-zero AND the correct follow_up= token for each collection kind.
func TestSearchMemoryTool_ScoreComponentsAndFollowUp(t *testing.T) {
	srcDoc := memoryindex.Document{
		ID:          "src_aabb112233445566",
		Kind:        memoryindex.KindSource,
		Title:       "contract.txt",
		Body:        "contract renewal text",
		SourceID:    "src_aabb112233445566",
		ScoreExact:  0.8,
		ScoreFTS:    0,
		ScoreVector: 0.3,
		Score:       0.8,
	}
	archiveDoc := memoryindex.Document{
		ID:          "archive:42",
		Kind:        memoryindex.KindArchive,
		Title:       "chat=10 turn=2",
		Body:        "archive note about contract",
		Handle:      "conversation:42",
		ChatID:      10,
		ScoreExact:  0,
		ScoreFTS:    0.6,
		ScoreVector: 0,
		Score:       0.6,
	}
	proposalDoc := memoryindex.Document{
		ID:          "proposal:7",
		Kind:        memoryindex.KindProposal,
		Title:       "target-page",
		Body:        "proposal text about contract",
		Handle:      "proposal:7",
		ProposalID:  7,
		ScoreExact:  0,
		ScoreFTS:    0,
		ScoreVector: 0.5,
		Score:       0.5,
	}
	wikiHit := search.Result{
		Kind:        "wiki_page",
		Slug:        "contract-overview",
		Title:       "Contract Overview",
		Content:     "Overview of contract terms.",
		Score:       0.9,
		ScoreExact:  0.4,
		ScoreFTS:    0,
		ScoreVector: 0,
	}

	tool := NewSearchMemoryTool(fakeMemoryWikiSearch{
		indexed: true,
		results: []search.Result{wikiHit},
	}, fakeCompactMemorySearch{docs: []memoryindex.Document{srcDoc, archiveDoc, proposalDoc}})
	if tool == nil {
		t.Fatal("expected non-nil tool")
	}

	out, err := tool.Execute(context.Background(), map[string]any{
		"query": "contract",
		"scope": "all",
		"limit": float64(10),
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// source: bracket with exact score + source read follow_up
	if !strings.Contains(out, "[exact=0.80") {
		t.Errorf("source hit missing exact score bracket:\n%s", out)
	}
	if !strings.Contains(out, "follow_up=source(action=read,source_id=src_aabb112233445566,mode=ocr)") {
		t.Errorf("source follow_up missing:\n%s", out)
	}

	// archive: bracket with fts score + search_memory follow_up
	if !strings.Contains(out, "[archive]") {
		t.Errorf("archive hit missing from output:\n%s", out)
	}
	if !strings.Contains(out, "fts=0.60") {
		t.Errorf("archive hit missing fts score bracket:\n%s", out)
	}
	if !strings.Contains(out, "follow_up=search_memory(scope=archive)") {
		t.Errorf("archive follow_up missing:\n%s", out)
	}

	// wiki: bracket with exact score + wiki-link follow_up
	if !strings.Contains(out, "[wiki] [[contract-overview]]") {
		t.Errorf("wiki hit missing from output:\n%s", out)
	}
	if !strings.Contains(out, "follow_up=[[contract-overview]]") {
		t.Errorf("wiki follow_up missing:\n%s", out)
	}

	// proposal: bracket with vector score + search_memory follow_up
	if !strings.Contains(out, "[proposal]") {
		t.Errorf("proposal hit missing from output:\n%s", out)
	}
	if !strings.Contains(out, "vector=0.50") {
		t.Errorf("proposal hit missing vector score bracket:\n%s", out)
	}
	if !strings.Contains(out, "follow_up=search_memory(scope=proposals)") {
		t.Errorf("proposal follow_up missing:\n%s", out)
	}
}

// TestSearchMemoryTool_ZeroComponentsNoBracket verifies that hits with all-zero
// component scores produce historical-shape output (no score bracket).
func TestSearchMemoryTool_ZeroComponentsNoBracket(t *testing.T) {
	tool := NewSearchMemoryTool(nil, fakeCompactMemorySearch{docs: []memoryindex.Document{{
		ID:     "archive:99",
		Kind:   memoryindex.KindArchive,
		Title:  "chat=1 turn=1",
		Body:   "zero component scores",
		Handle: "conversation:99",
		Score:  0.5,
		// ScoreExact, ScoreFTS, ScoreVector all zero (default)
	}}})
	out, err := tool.Execute(context.Background(), map[string]any{"query": "test", "scope": "archive"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(out, "[exact=") {
		t.Errorf("zero-component hit must not emit score bracket:\n%s", out)
	}
}

// TestFreshnessTokenPresent verifies that formatMemoryResults emits a freshness=
// token when EmbeddingModelID or IndexBuildID is non-empty, and does NOT emit
// degraded_read=true when the projection status is 'fresh'.
func TestFreshnessTokenPresent(t *testing.T) {
	indexedAt := time.Date(2026, 5, 14, 8, 21, 33, 0, time.UTC)
	doc := memoryindex.Document{
		ID:               "archive:42",
		Kind:             memoryindex.KindArchive,
		Title:            "chat=10 turn=1",
		Body:             "freshness token test content",
		Handle:           "conversation:42",
		ChatID:           10,
		Score:            1,
		UpdatedAt:        indexedAt,
		EmbeddingModelID: "embeddinggemma-300m@256-mrl",
		IndexBuildID:     "01HXYZ8K2QR5678",
	}
	tool := NewSearchMemoryTool(nil, fakeCompactMemorySearch{docs: []memoryindex.Document{doc}})
	if tool == nil {
		t.Fatal("expected non-nil tool")
	}
	tool.SetFreshnessStore(fakeFreshnessGetter{
		row:   freshness.Row{ProjectionID: "compact_memory_documents", Status: "fresh"},
		found: true,
	})

	out, err := tool.Execute(context.Background(), map[string]any{"query": "freshness token", "scope": "archive"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !strings.Contains(out, "freshness=indexed_at=2026-05-14T08:21:33Z") {
		t.Errorf("expected indexed_at in freshness token:\n%s", out)
	}
	if !strings.Contains(out, "model=embeddingge") {
		t.Errorf("expected model= (first 12 chars) in freshness token:\n%s", out)
	}
	if !strings.Contains(out, "build=01HXYZ8K2QR") {
		t.Errorf("expected build= (first 12 chars) in freshness token:\n%s", out)
	}
	if strings.Contains(out, "degraded_read=true") {
		t.Errorf("fresh projection must not emit degraded_read=true:\n%s", out)
	}
}

// TestDegradedReadFlag verifies that formatMemoryResults emits degraded_read=true
// when the projection_state row status is not 'fresh'.
func TestDegradedReadFlag(t *testing.T) {
	doc := memoryindex.Document{
		ID:               "archive:99",
		Kind:             memoryindex.KindArchive,
		Title:            "chat=5 turn=2",
		Body:             "degraded read test content",
		Handle:           "conversation:99",
		ChatID:           5,
		Score:            1,
		UpdatedAt:        time.Now(),
		EmbeddingModelID: "embeddinggemma-300m",
		IndexBuildID:     "01HXYZ000000",
	}
	tool := NewSearchMemoryTool(nil, fakeCompactMemorySearch{docs: []memoryindex.Document{doc}})
	tool.SetFreshnessStore(fakeFreshnessGetter{
		row:   freshness.Row{ProjectionID: "compact_memory_documents", Status: "degraded"},
		found: true,
	})

	out, err := tool.Execute(context.Background(), map[string]any{"query": "degraded read", "scope": "archive"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "degraded_read=true") {
		t.Errorf("expected degraded_read=true for degraded projection:\n%s", out)
	}
	if !strings.Contains(out, "freshness=") {
		t.Errorf("expected freshness= token alongside degraded_read:\n%s", out)
	}
}

// TestEmptyFreshnessOmitted verifies backward compatibility: hits with empty
// freshness columns (pre-US-M03 docs) must not emit the freshness= token.
func TestEmptyFreshnessOmitted(t *testing.T) {
	doc := memoryindex.Document{
		ID:     "archive:77",
		Kind:   memoryindex.KindArchive,
		Title:  "chat=3 turn=1",
		Body:   "old doc with no freshness columns",
		Handle: "conversation:77",
		ChatID: 3,
		Score:  1,
		// EmbeddingModelID, IndexBuildID intentionally empty (default)
	}
	tool := NewSearchMemoryTool(nil, fakeCompactMemorySearch{docs: []memoryindex.Document{doc}})
	// freshnessStore nil — no projection state needed for empty-defaults case

	out, err := tool.Execute(context.Background(), map[string]any{"query": "old doc", "scope": "archive"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(out, "freshness=") {
		t.Errorf("back-compat: empty freshness columns must not emit freshness= token:\n%s", out)
	}
	if strings.Contains(out, "degraded_read=true") {
		t.Errorf("back-compat: empty freshness columns must not emit degraded_read=true:\n%s", out)
	}
}

// errorMemoryWikiSearch simulates a wiki searcher backed by an unreachable Qdrant
// instance. IsIndexed returns true (the collection was successfully built before
// the outage) so searchWiki proceeds to the Search call and observes the error.
type errorMemoryWikiSearch struct {
	err error
}

func (f errorMemoryWikiSearch) IsIndexed() bool { return true }

func (f errorMemoryWikiSearch) Search(_ context.Context, _ string, _ int) ([]search.Result, error) {
	return nil, f.err
}

// TestSearchMemoryTool_QdrantDownGracefulDegradation verifies that when the
// Qdrant vector store is unreachable (simulated as a connection-refused error
// from the wiki searcher), search_memory:
//   - does NOT return an error to the caller (graceful degradation)
//   - returns a non-empty, coherent response (no panic, no Go stack trace)
//   - surfaces the failure as a warning ("wiki search failed")
//   - emits the "No memory found" sentinel so the LLM knows the result set is empty
func TestSearchMemoryTool_QdrantDownGracefulDegradation(t *testing.T) {
	connRefused := fmt.Errorf("Post \"http://127.0.0.1:6333/collections/aura_memory_v1/points/query\": dial tcp 127.0.0.1:6333: connect: connection refused")
	tool := NewSearchMemoryTool(errorMemoryWikiSearch{err: connRefused}, nil)

	out, err := tool.Execute(context.Background(), map[string]any{"query": "venice trip planning", "scope": "wiki"})
	if err != nil {
		t.Fatalf("Execute() must not return error on qdrant-down; got: %v", err)
	}
	if out == "" {
		t.Fatal("Execute() returned empty string on qdrant-down; want coherent degraded response")
	}
	if !strings.Contains(out, "No memory found") {
		t.Fatalf("expected graceful no-results sentinel in output, got:\n%s", out)
	}
	if !strings.Contains(out, "wiki search failed") {
		t.Fatalf("expected 'wiki search failed' warning in output, got:\n%s", out)
	}
	// Ensure no Go panic / stack trace leaked to user-visible output.
	if strings.Contains(out, "goroutine") || strings.Contains(out, "runtime/debug") {
		t.Fatalf("panic/stack trace must not appear in user-visible output:\n%s", out)
	}
}
