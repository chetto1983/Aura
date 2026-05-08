package telegram

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aura/aura/internal/config"
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

func TestComposeTurnMemoryPackSkipsGenericTurns(t *testing.T) {
	repo := &recordingSearch{indexed: true, results: []search.Result{{
		Kind:    "wiki_page",
		Slug:    "aura-operating-memory",
		Title:   "Aura Operating Memory",
		Content: "Aura should use compact context.",
		Score:   0.9,
	}}}

	got := composeTurnMemoryPack(context.Background(), repo, "", "ciao come stai?", 25, slog.New(slog.NewTextHandler(io.Discard, nil)), "u1")
	if got != "" {
		t.Fatalf("generic turn memory pack = %q, want empty", got)
	}
	if !repo.deadline.IsZero() {
		t.Fatal("generic turn should not run speculative search")
	}
}

func TestComposeTurnMemoryPackSearchesMemoryTurnsWithoutGraphOrLog(t *testing.T) {
	repo := &recordingSearch{indexed: true, results: []search.Result{{
		Kind:    "wiki_page",
		Slug:    "aura-operating-memory",
		Title:   "Aura Operating Memory",
		Content: "Aura should use compact context.",
		Score:   0.9,
	}}}
	wikiDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wikiDir, "graph"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wikiDir, "graph", "context.md"), []byte("# Wiki Graph Context\n\n- Pages: 22\n- Edges: 53\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wikiDir, "log.md"), []byte("# Wiki Log\n\n| old | row |\n| 2026-05-08T10:00:00Z | update | [[aura-operating-memory]] |\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := composeTurnMemoryPack(context.Background(), repo, wikiDir, "cosa ricordi di Aura?", 25, slog.New(slog.NewTextHandler(io.Discard, nil)), "u1")
	for _, want := range []string{
		"## Memory Pack",
		"[[aura-operating-memory]]",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("memory pack missing %q:\n%s", want, got)
		}
	}
	for _, notWant := range []string{"### Graph Context", "### Recent Wiki Log", "update"} {
		if strings.Contains(got, notWant) {
			t.Fatalf("memory pack includes hot-path context %q:\n%s", notWant, got)
		}
	}
}

func TestComposeTurnMemoryPackLoadsGraphOnlyForGraphTurns(t *testing.T) {
	repo := &recordingSearch{indexed: true, results: []search.Result{{
		Kind:    "wiki_page",
		Slug:    "aura-operating-memory",
		Title:   "Aura Operating Memory",
		Content: "Aura should use compact context.",
		Score:   0.9,
	}}}
	wikiDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wikiDir, "graph"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wikiDir, "graph", "context.md"), []byte("# Wiki Graph Context\n\n- Pages: 22\n- Edges: 53\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := composeTurnMemoryPack(context.Background(), repo, wikiDir, "mostrami il grafo della wiki", 25, slog.New(slog.NewTextHandler(io.Discard, nil)), "u1")
	for _, want := range []string{"## Memory Pack", "### Graph Context", "- Pages: 22"} {
		if !strings.Contains(got, want) {
			t.Fatalf("graph memory pack missing %q:\n%s", want, got)
		}
	}
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
