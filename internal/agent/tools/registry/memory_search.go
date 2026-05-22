package tools

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/aura/aura/internal/storage/freshness"
	"github.com/aura/aura/internal/storage/memoryindex"
	"github.com/aura/aura/internal/storage/search"
)

// search_memory contract
//
// The tool is the LLM's read-side window into Aura's persistent wiki, source
// archive, conversation history, and proposed updates. It is the closest
// equivalent of the "search engine over the wiki" sketched in the wiki design
// notes (historical docs/llm-wiki.md, removed 2026-05-19): hybrid BM25 + cosine retrieval over
// the items the agent has curated over time.
//
// Output is plain markdown. There is no JSON envelope, no calibrated score
// curve, no "Evidence envelope:" appendix. Each hit is one line the model
// can read directly, optionally followed by an indented snippet. This avoids
// the prompt-rot pattern where the assistant echoes raw evidence JSON back
// to the user — the symptom we used to scrub with a regex.
//
// Scoring multiplies two factors:
//
//   - relevance: raw similarity from the underlying backend (Qdrant cosine
//     or SQLite FTS rank-normalized). Already in [0,1].
//   - recency:   exp(-age_days / halfLifeDays). Wiki and source kinds use a
//     long half-life because curated knowledge ages slowly; archive and
//     proposal use a short half-life because they're operational.
//
// The two factors multiply rather than sum: a fresh-but-irrelevant note
// must not outrank a very-relevant-but-old wiki page.

const (
	searchMemoryDefaultLimit   = 6
	searchMemoryMaxLimit       = 12
	searchMemorySnippetLimit   = 260
	searchMemoryDefaultTimeout = 5 * time.Second

	// Half-life in days for the 0.5^(age/halfLife) recency factor.
	// Curated knowledge (wiki, ingested sources, graph cards) ages slowly;
	// operational signals (archive, proposed updates) age fast. The split
	// is binary on purpose — see isArchiveKind in this file.
	recencyHalfLifeWikiDays    = 180.0
	recencyHalfLifeArchiveDays = 30.0
)

type compactMemorySearcher interface {
	Search(ctx context.Context, query string, filter memoryindex.Filter) ([]memoryindex.Document, error)
}

// freshnessGetter reads projection_state for a given projection ID.
// Satisfied by *freshness.Store; callers outside this package pass the concrete
// type without naming the interface.
type freshnessGetter interface {
	Get(ctx context.Context, projectionID string) (freshness.Row, bool, error)
}

// compactMemoryProjectionID is the canonical projection ID for compact memory.
const compactMemoryProjectionID = "compact_memory_documents"

type SearchMemoryTool struct {
	wiki                search.Searcher
	compact             compactMemorySearcher
	freshnessStore      freshnessGetter
	timeout             time.Duration
	halfLifeWikiDays    float64
	halfLifeArchiveDays float64
	now                 func() time.Time
}

// SetFreshnessStore injects a freshness getter so Execute can annotate
// compact-memory hits with freshness= tokens and degraded_read=true.
func (t *SearchMemoryTool) SetFreshnessStore(fs freshnessGetter) {
	t.freshnessStore = fs
}

func NewSearchMemoryTool(wiki search.Searcher, compact compactMemorySearcher) *SearchMemoryTool {
	return NewSearchMemoryToolConfigured(wiki, compact, searchMemoryDefaultTimeout, recencyHalfLifeWikiDays, recencyHalfLifeArchiveDays)
}

// NewSearchMemoryToolConfigured is the env-driven constructor. wikiHalfLife
// applies to wiki/source/graph kinds (curated knowledge ages slowly);
// archiveHalfLife applies to archive/proposal kinds (operational notes age
// fast). Zero values fall back to the package defaults so test fixtures
// can stay terse.
func NewSearchMemoryToolConfigured(wiki search.Searcher, compact compactMemorySearcher, timeout time.Duration, wikiHalfLifeDays, archiveHalfLifeDays float64) *SearchMemoryTool {
	if wiki == nil && compact == nil {
		return nil
	}
	if wikiHalfLifeDays <= 0 {
		wikiHalfLifeDays = recencyHalfLifeWikiDays
	}
	if archiveHalfLifeDays <= 0 {
		archiveHalfLifeDays = recencyHalfLifeArchiveDays
	}
	return &SearchMemoryTool{
		wiki:                wiki,
		compact:             compact,
		timeout:             timeout,
		halfLifeWikiDays:    wikiHalfLifeDays,
		halfLifeArchiveDays: archiveHalfLifeDays,
		now:                 time.Now,
	}
}

func (t *SearchMemoryTool) Name() string { return "search_memory" }

func (t *SearchMemoryTool) Description() string {
	return "Search Aura's persistent memory - wiki pages, sources, conversation archive, proposed updates. Returns top-10 hits, INTERNAL — synthesize before replying; cite as [[slug]] or src_xxx."
}

func (t *SearchMemoryTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Natural-language query or keywords.",
			},
			"scope": map[string]any{
				"type":        "string",
				"description": "Which subset to search. Defaults to all.",
				"enum":        []string{"all", "wiki", "sources", "archive", "proposals"},
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum results to return (default 6, max 12).",
				"minimum":     1,
				"maximum":     searchMemoryMaxLimit,
			},
			"chat_id": map[string]any{
				"type":        "integer",
				"description": "Restrict conversation-archive results to this chat.",
			},
			"source_id": map[string]any{
				"type":        "string",
				"description": "Optional: scope results to chunks of this source (use the src_xxx ID from an earlier hit to walk the same document).",
			},
		},
		"required": []string{"query"},
	}
}

func (t *SearchMemoryTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t == nil {
		return "", errors.New("search_memory: tool unavailable")
	}
	query, err := requiredString(args, "query")
	if err != nil {
		return "", fmt.Errorf("search_memory: %w", err)
	}
	scopes, err := memoryScopes(stringArg(args, "scope"))
	if err != nil {
		return "", err
	}
	limit := intArg(args, "limit", searchMemoryDefaultLimit, 1, searchMemoryMaxLimit)
	chatID := int64Arg(args, "chat_id")
	sourceID := stringArg(args, "source_id")
	searchCtx, cancel := t.searchContext(ctx)
	defer cancel()

	var warnings []string
	results := make([]memoryResult, 0, limit*3)
	if scopes["wiki"] {
		wikiResults, wikiWarnings := t.searchWiki(searchCtx, query, limit)
		results = append(results, wikiResults...)
		warnings = append(warnings, wikiWarnings...)
	}
	var compactKinds []string
	if scopes["sources"] {
		compactKinds = append(compactKinds, memoryindex.KindSource)
	}
	if scopes["archive"] {
		compactKinds = append(compactKinds, memoryindex.KindArchive)
	}
	if scopes["proposals"] {
		compactKinds = append(compactKinds, memoryindex.KindProposal)
	}
	if len(compactKinds) > 0 {
		compactResults, compactWarnings := t.searchCompact(searchCtx, query, compactKinds, chatID, sourceID, limit)
		// Belt-and-suspenders: when the compact searcher is not a *memoryindex.Store
		// (e.g. a test fake or a future adapter), doc.DegradedRead may be unset.
		// Fall back to the tool's own freshnessStore lookup so degraded_read=true is
		// still surfaced to the LLM. When the Store has already annotated DegradedRead,
		// this write is a no-op (true || true = true).
		if t.freshnessStore != nil && len(compactResults) > 0 {
			if row, found, err := t.freshnessStore.Get(searchCtx, compactMemoryProjectionID); err == nil && found && row.Status != "fresh" {
				for i := range compactResults {
					compactResults[i].DegradedRead = true
				}
			}
		}
		results = append(results, compactResults...)
		warnings = append(warnings, compactWarnings...)
	}

	now := t.now()
	for i := range results {
		halfLife := t.halfLifeWikiDays
		if isArchiveKind(results[i].Kind) {
			halfLife = t.halfLifeArchiveDays
		}
		results[i].Score = relevanceTimesRecencyWithHalfLife(results[i].Score, results[i].UpdatedAt, now, halfLife)
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].Identifier < results[j].Identifier
		}
		return results[i].Score > results[j].Score
	})
	if len(results) > limit {
		results = results[:limit]
	}
	return formatMemoryResults(query, results, warnings, now), nil
}

func (t *SearchMemoryTool) searchContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if t == nil || t.timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, t.timeout)
}

type memoryResult struct {
	Kind          string
	Identifier    string
	Title         string
	Role          string
	Snippet       string
	Page          int
	ChunkIndex    int
	ByteStart     int
	ByteEnd       int
	Score         float64
	ScoreExact    float64
	ScoreFTS      float64
	ScoreVector   float64
	UpdatedAt     time.Time
	CreatedAt     time.Time
	SchemaVersion int
	PromptVersion string
	Unversioned   bool
	Handle        string
	FilePath      string
	Category      string
	Tags          []string
	Related       []string
	Sources       []string
	SizeBytes     int64
	// Freshness fields — compact memory only, populated when US-M03 columns are set.
	IndexedAt        time.Time
	EmbeddingModelID string
	IndexBuildID     string
	DegradedRead     bool
}

func (t *SearchMemoryTool) searchWiki(ctx context.Context, query string, limit int) ([]memoryResult, []string) {
	if t.wiki == nil {
		return nil, []string{"wiki search unavailable"}
	}
	if !t.wiki.IsIndexed() {
		return nil, []string{"wiki index not ready"}
	}
	var results []search.Result
	var err error
	if hybrid, ok := t.wiki.(search.HybridSearcher); ok {
		results, err = hybrid.SearchHybrid(ctx, query, limit)
	} else {
		results, err = t.wiki.Search(ctx, query, limit)
	}
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, []string{"wiki search timed out"}
		}
		return nil, []string{"wiki search failed: " + err.Error()}
	}
	out := make([]memoryResult, 0, len(results))
	for _, r := range results {
		snippet, _ := snippetAround(r.Content, query, searchMemorySnippetLimit)
		kind := strings.TrimSpace(r.Kind)
		if kind == "" || kind == "wiki_page" {
			kind = "wiki"
		}
		identifier := "[[" + r.Slug + "]]"
		if kind == "graph_index" {
			identifier = r.Slug
		}
		out = append(out, memoryResult{
			Kind:          kind,
			Identifier:    identifier,
			Title:         r.Title,
			Snippet:       snippet,
			Score:         float64(r.Score),
			ScoreExact:    float64(r.ScoreExact),
			ScoreFTS:      float64(r.ScoreFTS),
			ScoreVector:   float64(r.ScoreVector),
			UpdatedAt:     r.UpdatedAt,
			CreatedAt:     r.CreatedAt,
			SchemaVersion: r.SchemaVersion,
			PromptVersion: r.PromptVersion,
			Unversioned:   r.Unversioned,
			Handle:        identifier,
			FilePath:      r.FilePath,
			Category:      r.Category,
			Tags:          r.Tags,
			Related:       r.Related,
			Sources:       r.Sources,
			SizeBytes:     r.SizeBytes,
		})
	}
	return out, nil
}

func (t *SearchMemoryTool) searchCompact(ctx context.Context, query string, kinds []string, chatID int64, sourceID string, limit int) ([]memoryResult, []string) {
	if t.compact == nil {
		return nil, []string{"compact memory index unavailable"}
	}
	docs, err := t.compact.Search(ctx, query, memoryindex.Filter{
		Kinds:    kinds,
		ChatID:   chatID,
		SourceID: sourceID,
		Limit:    limit,
	})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, []string{"compact memory search timed out"}
		}
		return nil, []string{"compact memory search failed: " + err.Error()}
	}
	out := make([]memoryResult, 0, len(docs))
	for _, doc := range docs {
		snippet, _ := snippetAround(doc.Body, query, searchMemorySnippetLimit)
		identifier := compactIdentifier(doc)
		// Only set IndexedAt when freshness columns are populated (US-M03), so
		// back-compat docs with empty defaults don't emit a spurious freshness token.
		var indexedAt time.Time
		if doc.EmbeddingModelID != "" || doc.IndexBuildID != "" {
			indexedAt = doc.UpdatedAt
		}
		out = append(out, memoryResult{
			Kind:             doc.Kind,
			Identifier:       identifier,
			Title:            doc.Title,
			Snippet:          snippet,
			Page:             doc.Page,
			ChunkIndex:       doc.ChunkIndex,
			ByteStart:        doc.ByteStart,
			ByteEnd:          doc.ByteEnd,
			Score:            doc.Score,
			ScoreExact:       doc.ScoreExact,
			ScoreFTS:         doc.ScoreFTS,
			ScoreVector:      doc.ScoreVector,
			UpdatedAt:        doc.UpdatedAt,
			Handle:           doc.Handle,
			IndexedAt:        indexedAt,
			EmbeddingModelID: doc.EmbeddingModelID,
			IndexBuildID:     doc.IndexBuildID,
			DegradedRead:     doc.DegradedRead,
		})
	}
	return out, nil
}

func compactIdentifier(doc memoryindex.Document) string {
	if doc.Handle != "" {
		switch doc.Kind {
		case memoryindex.KindArchive, memoryindex.KindProposal:
			return doc.Handle
		}
	}
	if doc.Kind == memoryindex.KindSource && doc.SourceID != "" {
		return doc.SourceID
	}
	return doc.ID
}

// relevanceTimesRecencyWithHalfLife is the per-result ranking score.
//
// The recency factor uses true half-life decay: 0.5^(age_days / halfLife).
// With halfLife=30:
//   - age 0 days   → recency 1.00
//   - age 30 days  → recency 0.50
//   - age 60 days  → recency 0.25
//   - age 180 days → recency ≈ 0.016
//
// Multiplying by relevance rather than averaging means a fresh-but-noise
// item cannot displace a relevant-but-aged one. The user can still find
// old wiki pages via direct query; the ranker simply prefers a recent hit
// when both are similarly relevant.
//
// The half-life is supplied by the caller because the operator can override
// it via env (MEMORY_RECENCY_HALFLIFE_WIKI_DAYS / _ARCHIVE_DAYS) — see
// SearchMemoryTool's two halfLife*Days fields.
func relevanceTimesRecencyWithHalfLife(relevance float64, updatedAt, now time.Time, halfLifeDays float64) float64 {
	if relevance <= 0 {
		return 0
	}
	rel := clampUnit(relevance)
	if updatedAt.IsZero() {
		return rel
	}
	if halfLifeDays <= 0 {
		return rel
	}
	ageDays := now.Sub(updatedAt).Hours() / 24.0
	if ageDays < 0 {
		ageDays = 0
	}
	recency := math.Pow(0.5, ageDays/halfLifeDays)
	return rel * recency
}

// isArchiveKind reports whether a hit comes from the operational tier (chat
// archive / proposed updates), as opposed to curated knowledge (wiki pages,
// graph cards, ingested sources). Picks the matching half-life bucket.
func isArchiveKind(kind string) bool {
	switch strings.TrimSpace(kind) {
	case "archive", "proposal":
		return true
	default:
		return false
	}
}

func clampUnit(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		// Some backends (FTS rank) return values >1. Compress to [0,1] with
		// a soft cap so very-high BM25 scores still rank above mid-cosine
		// hits.
		return 1 - 1/(1+v)
	}
	return v
}
