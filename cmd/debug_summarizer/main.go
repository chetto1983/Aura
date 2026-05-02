// debug_summarizer is a standalone integration harness for the summarizer
// pipeline. It spins up a temp wiki + SQLite DB, seeds mock turns, runs the
// Runner end-to-end with SUMMARIZER_MODE=auto against a httptest LLM stub,
// and asserts the expected wiki mutations. Exits 0 on PASS, 1 on FAIL.
//
// NOTE: AutoApplier.applyNew does not set SchemaVersion or PromptVersion on
// the page it creates, which fails wiki.Validate. This is a known bug tracked
// for Backend. The harness works around it via patchingWikiWriter (see below)
// so the orchestration path can still be exercised end-to-end.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/conversation/summarizer"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/scheduler"
	"github.com/aura/aura/internal/search"
	"github.com/aura/aura/internal/wiki"

	_ "modernc.org/sqlite"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()
	base := filepath.Join(os.TempDir(), fmt.Sprintf("debug_summarizer_%d", time.Now().UnixNano()))
	if err := os.MkdirAll(base, 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	defer os.RemoveAll(base)

	// ── 1. Spin up temp wiki ──────────────────────────────────────────────────
	wikiDir := filepath.Join(base, "wiki")
	if err := os.MkdirAll(wikiDir, 0755); err != nil {
		return fmt.Errorf("mkdir wiki: %w", err)
	}
	wikiStore, err := wiki.NewStore(wikiDir, nil)
	if err != nil {
		return fmt.Errorf("wiki.NewStore: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	preExisting := []*wiki.Page{
		{Title: "Aura Architecture", Category: "tech", Body: "Aura uses Go 1.22+ and SQLite.", SchemaVersion: wiki.CurrentSchemaVersion, PromptVersion: "v1", CreatedAt: now, UpdatedAt: now},
		{Title: "Marco Info", Category: "person", Body: "Marco is a friend.", SchemaVersion: wiki.CurrentSchemaVersion, PromptVersion: "v1", CreatedAt: now, UpdatedAt: now},
		{Title: "General Notes", Category: "fact", Body: "Some general notes.", SchemaVersion: wiki.CurrentSchemaVersion, PromptVersion: "v1", CreatedAt: now, UpdatedAt: now},
	}
	for _, p := range preExisting {
		if err := wikiStore.WritePage(ctx, p); err != nil {
			return fmt.Errorf("WritePage %q: %w", p.Title, err)
		}
	}

	// ── 2. Spin up temp SQLite ────────────────────────────────────────────────
	dbPath := filepath.Join(base, "aura.db")
	store, err := scheduler.OpenStore(dbPath)
	if err != nil {
		return fmt.Errorf("OpenStore: %w", err)
	}
	defer store.DB().Close()

	archiveStore, err := conversation.NewArchiveStore(store.DB())
	if err != nil {
		return fmt.Errorf("NewArchiveStore: %w", err)
	}

	// ── 3. Seed 5 mock turns ──────────────────────────────────────────────────
	const chatID = int64(42)
	turns := []conversation.Turn{
		{ChatID: chatID, UserID: 1, TurnIndex: 0, Role: "user", Content: "Hey, my friend Marco lives in Bologna and works in fintech."},
		{ChatID: chatID, UserID: 1, TurnIndex: 1, Role: "assistant", Content: "That's interesting! Bologna is a great city."},
		{ChatID: chatID, UserID: 1, TurnIndex: 2, Role: "user", Content: "Yeah, and he loves pasta al ragù."},
		{ChatID: chatID, UserID: 1, TurnIndex: 3, Role: "assistant", Content: "The backend for this project uses Go 1.22+."},
		{ChatID: chatID, UserID: 1, TurnIndex: 4, Role: "user", Content: "The weather was nice yesterday."},
	}
	for _, t := range turns {
		if err := archiveStore.Append(ctx, t); err != nil {
			return fmt.Errorf("Append turn %d: %w", t.TurnIndex, err)
		}
	}

	// ── 4. Build LLM mock ─────────────────────────────────────────────────────
	// Three candidates from the stub:
	//   score 0.92 → new fact (Marco / Bologna / fintech)
	//   score 0.88 → redundant (overlaps aura-architecture, sim 0.91 → skip)
	//   score 0.20 → trivia (below min salience 0.7, filtered by scorer)
	candidatesJSON := `{"candidates":[
		{"fact":"Marco lives in Bologna and works in fintech","score":0.92,"category":"person","related_slugs":[],"source_turn_ids":[3]},
		{"fact":"Backend uses Go 1.22+","score":0.88,"category":"tech","related_slugs":["aura-architecture"],"source_turn_ids":[4]},
		{"fact":"weather was nice yesterday","score":0.2,"category":"trivia","related_slugs":[],"source_turn_ids":[5]}
	]}`

	llmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"choices": []map[string]any{{
				"message":       map[string]any{"role": "assistant", "content": candidatesJSON},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 20, "total_tokens": 30},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer llmSrv.Close()

	llmClient := llm.NewOpenAIClient(llm.OpenAIConfig{
		APIKey:  "test",
		BaseURL: llmSrv.URL,
		Model:   "test-model",
	})
	scorer := summarizer.NewScorer(llmClient, "test-model", 0.7)

	// ── 5. Controlled deduper (no real embedding server) ─────────────────────
	deduper := summarizer.NewDeduper(
		&controlledSearcher{
			rules: map[string]float32{
				"Marco lives in Bologna and works in fintech": 0.10, // new (sim < 0.5)
				"Backend uses Go 1.22+":                      0.91, // skip (sim > 0.85)
			},
			defaultSlug: "aura-architecture",
		},
		0.85, 0.5,
	)

	// ── 6. AutoApplier via patchingWikiWriter workaround ─────────────────────
	// patchingWikiWriter fills in SchemaVersion + PromptVersion which
	// AutoApplier.applyNew omits (Bug: tracked for Backend to fix in
	// applier.go — applyNew must set SchemaVersion/PromptVersion).
	patcher := &patchingWikiWriter{inner: wikiStore}
	applier := summarizer.NewAutoApplier(patcher)

	cfg := summarizer.RunnerConfig{
		Enabled:       true,
		TurnInterval:  5,
		LookbackTurns: 5,
		CooldownSecs:  0,
		Applier:       applier,
	}
	runner := summarizer.NewRunner(cfg, archiveStore, scorer, deduper)

	triggered, extraction, err := runner.MaybeExtract(ctx, chatID)
	if err != nil {
		return fmt.Errorf("MaybeExtract: %w", err)
	}

	// ── 7. Assertions ─────────────────────────────────────────────────────────
	pass := true
	check := func(name string, ok bool, detail string) {
		if ok {
			fmt.Printf("  PASS  %s\n", name)
		} else {
			fmt.Printf("  FAIL  %s — %s\n", name, detail)
			pass = false
		}
	}

	check("runner triggered", triggered, "MaybeExtract returned triggered=false")
	check("extraction non-nil", extraction != nil, "extraction is nil")

	if extraction != nil {
		newCount, skipCount := 0, 0
		for _, d := range extraction.Decisions {
			switch d.Action {
			case summarizer.ActionNew:
				newCount++
			case summarizer.ActionSkip:
				skipCount++
			}
		}
		check("exactly one ActionNew decision", newCount == 1,
			fmt.Sprintf("got %d ActionNew decisions", newCount))
		check("exactly one ActionSkip decision", skipCount == 1,
			fmt.Sprintf("got %d ActionSkip decisions", skipCount))
		check("low-salience filtered (2 decisions total)", len(extraction.Decisions) == 2,
			fmt.Sprintf("got %d decisions", len(extraction.Decisions)))
	}

	newSlug := wiki.Slug("Marco lives in Bologna and works in fintech")
	newPage, readErr := wikiStore.ReadPage(newSlug)
	check("new wiki page created", readErr == nil && newPage != nil,
		fmt.Sprintf("ReadPage(%q): %v", newSlug, readErr))
	if newPage != nil {
		check("new page body contains fact", strings.Contains(newPage.Body, "Marco lives in Bologna"),
			fmt.Sprintf("body: %q", newPage.Body))
	}

	logPath := filepath.Join(wikiDir, "log.md")
	logBytes, logErr := os.ReadFile(logPath)
	logContent := string(logBytes)
	check("log.md readable", logErr == nil, fmt.Sprintf("ReadFile log.md: %v", logErr))
	check("log.md has auto-sum new entry", strings.Contains(logContent, "auto-sum new"),
		"no 'auto-sum new' line in log.md")
	check("log.md has auto-sum skip entry", strings.Contains(logContent, "auto-sum skip"),
		"no 'auto-sum skip' line in log.md")

	weatherSlug := wiki.Slug("weather was nice yesterday")
	_, weatherErr := wikiStore.ReadPage(weatherSlug)
	check("low-salience produced no wiki page", weatherErr != nil,
		"weather page unexpectedly created")

	fmt.Println()
	if pass {
		fmt.Println("PASS")
		return nil
	}
	fmt.Println("FAIL")
	os.Exit(1)
	return nil
}

// patchingWikiWriter wraps wiki.Store and fills in SchemaVersion+PromptVersion
// on pages that AutoApplier creates without them (workaround for applier bug).
type patchingWikiWriter struct {
	inner *wiki.Store
}

func (p *patchingWikiWriter) WritePage(ctx context.Context, page *wiki.Page) error {
	if page.SchemaVersion == 0 {
		page.SchemaVersion = wiki.CurrentSchemaVersion
	}
	if page.PromptVersion == "" {
		page.PromptVersion = "v1"
	}
	return p.inner.WritePage(ctx, page)
}

func (p *patchingWikiWriter) ReadPage(slug string) (*wiki.Page, error) {
	return p.inner.ReadPage(slug)
}

func (p *patchingWikiWriter) AppendLog(ctx context.Context, action, slug string) {
	p.inner.AppendLog(ctx, action, slug)
}

// controlledSearcher returns fixed similarity scores keyed by query text,
// allowing deterministic dedup decisions without a real embedding server.
type controlledSearcher struct {
	rules       map[string]float32
	defaultSlug string
}

func (c *controlledSearcher) Search(_ context.Context, query string, _ int) ([]search.Result, error) {
	score, ok := c.rules[query]
	if !ok {
		score = 0.05
	}
	return []search.Result{{Slug: c.defaultSlug, Score: score}}, nil
}
