package search

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Result is one wiki hit returned by Search.
//
// UpdatedAt carries the page's frontmatter updated_at when the backend can
// supply it (zero time when unknown). Callers that score with a recency factor
// must guard against a zero time and choose whether to apply recency=1.0 or
// skip the multiplier.
//
// FilePath, Category, Tags, Related, Sources, and SizeBytes round out the
// per-hit metadata so search_memory can satisfy the LLM in one round-trip —
// previously the model had to follow up with list_files/read_file to map a
// slug back to a .md path or read the page's relations.
type Result struct {
	Kind          string
	Slug          string
	Title         string
	Content       string
	Score         float32
	ScoreExact    float32
	ScoreFTS      float32
	ScoreVector   float32
	UpdatedAt     time.Time
	CreatedAt     time.Time
	SchemaVersion int
	PromptVersion string
	Unversioned   bool
	FilePath      string
	Category      string
	Tags          []string
	Related       []string
	Sources       []string
	SizeBytes     int64
}

// EmbeddingFunc is Aura's embedding provider boundary. Same signature as
// chromem-go's func type so wrappers (EmbedCache, fakes) port cleanly; the
// dependency on chromem itself is gone.
type EmbeddingFunc func(ctx context.Context, text string) ([]float32, error)

// BatchEmbeddingFunction returns one vector per input text, in order.
type BatchEmbeddingFunction func(ctx context.Context, texts []string) ([][]float32, error)

// Document is the input shape for both Qdrant upserts and the SQLite mirror.
type Document struct {
	ID       string
	Content  string
	Metadata map[string]string
}

// EmbeddingFunction is the historical alias retained for one release while
// callers migrate. New code should use EmbeddingFunc directly.
type EmbeddingFunction = EmbeddingFunc

// Queryer is the minimal semantic retrieval boundary.
type Queryer interface {
	Search(ctx context.Context, query string, topK int) ([]Result, error)
}

// Searcher is the read-only wiki retrieval boundary used by tools and Telegram
// context injection.
type Searcher interface {
	Queryer
	IsIndexed() bool
}

// HybridSearcher extends Searcher with a 3-channel hybrid search combining
// exact title-match, FTS5 BM25, and vector cosine via RRF fusion.
// Implemented by qdrantRepository when wired with NewQdrantRepositoryWithDB.
type HybridSearcher interface {
	Searcher
	SearchHybrid(ctx context.Context, query string, topK int) ([]Result, error)
}

// HybridRepository combines the full Repository boundary with HybridSearcher.
// Returned by NewQdrantRepositoryWithDB; implements Repository so it can be
// assigned wherever a Repository is expected.
type HybridRepository interface {
	Repository
	SearchHybrid(ctx context.Context, query string, topK int) ([]Result, error)
}

// WikiPageReindexer is the wiki index maintenance boundary used after one wiki
// page changes.
type WikiPageReindexer interface {
	ReindexWikiPage(ctx context.Context, slug string) error
}

// WikiPageIndexer is the startup/full-rebuild wiki index boundary.
type WikiPageIndexer interface {
	IndexWikiPages(ctx context.Context) error
	WikiPageReindexer
}

// DocumentIndexer is the lower-level document indexing boundary used by debug
// and future non-wiki index feeds.
type DocumentIndexer interface {
	Index(ctx context.Context, id string, content string, metadata map[string]string) error
}

// WikiIndexRebuilder triggers a full Qdrant collection rebuild. Used by
// POST /api/wiki/reindex. When dryRun is true, returns current stats without
// modifying the collection.
type WikiIndexRebuilder interface {
	RebuildWikiIndex(ctx context.Context, dryRun bool) (QdrantRebuildReport, error)
}

// Repository is the full search/index boundary implemented by qdrantRepository.
type Repository interface {
	Searcher
	WikiPageIndexer
	DocumentIndexer
}

// rrfMergeConstants for search.Result fusion. Mirrors the constants in
// internal/memoryindex (rrfK=60, exact/fts/vector weights 1.0/0.6/0.8).
// Group weights are positional: first group treated as the exact-match
// channel, second as FTS, third as vector. Extra groups beyond the third
// fall back to weight=0.5 so a caller that fans out into more channels
// still gets sane fusion without a code change here.
const (
	rrfK                  = 60
	rrfWeightExactResult  = 1.0
	rrfWeightFTSResult    = 0.6
	rrfWeightVectorResult = 0.8
	rrfWeightExtraResult  = 0.5
)

// splitCSVPayloadField reverses the comma-join used at index time for
// list-valued frontmatter fields (tags, related, sources). Empty entries are
// dropped; whitespace around items is trimmed. Returns nil when the value is
// empty so callers do not have to distinguish "absent" from "empty list".
func splitCSVPayloadField(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// parseSearchPayloadTime accepts the RFC3339 and a couple of common YAML
// frontmatter layouts (older pages used a space separator). Empty / unparseable
// values surface as the zero time, which the recency multiplier knows to skip.
func parseSearchPayloadTime(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
	} {
		if ts, err := time.Parse(layout, raw); err == nil {
			return ts.UTC(), true
		}
	}
	return time.Time{}, false
}

func parseSearchPayloadBool(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "t", "true", "yes", "y":
		return true
	default:
		return false
	}
}

func extractTitle(data []byte) string {
	var partial struct {
		Title string `yaml:"title"`
	}
	if err := yaml.Unmarshal(data, &partial); err != nil {
		return ""
	}
	return partial.Title
}

func metadataToJSON(metadata map[string]string) string {
	b, err := json.Marshal(metadata)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func extractMetaField(metaJSON, field string) string {
	var m map[string]string
	if err := json.Unmarshal([]byte(metaJSON), &m); err != nil {
		return ""
	}
	return m[field]
}

func resultKey(result Result) string {
	kind := strings.TrimSpace(result.Kind)
	if kind == "" {
		kind = "wiki_page"
	}
	slug := strings.TrimSpace(result.Slug)
	if slug == "" {
		slug = strings.TrimSpace(result.Title)
	}
	if slug == "" {
		return ""
	}
	return kind + "\x00" + strings.ToLower(slug)
}

func resultKind(r Result) string {
	if strings.TrimSpace(r.Kind) == "" {
		return "wiki_page"
	}
	return r.Kind
}

func resultLabel(r Result) string {
	if r.Slug == "" {
		return ""
	}
	switch resultKind(r) {
	case "wiki_page", "graph_node":
		return "[[" + r.Slug + "]]"
	default:
		return r.Slug
	}
}
