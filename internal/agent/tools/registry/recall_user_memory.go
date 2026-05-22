package tools

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aura/aura/internal/storage/memoryindex"
)

const (
	recallUserMemoryDefaultLimit = 5
	recallUserMemoryMaxLimit     = 20
	recallUserMemoryTimeout      = 5 * time.Second
)

// RecallUserMemoryTool surfaces validated user facts and preferences about the
// operator (name, location, interests, work context, habits, scheduled todos).
// Facts are promoted from conversation candidates through proposed_updates
// (kind='user_memory') → operator approval → WriteApprovedUserFact →
// compact_memory_documents (kind='user_memory', status='active').
// Pending proposals are never visible here — only approved entries.
type RecallUserMemoryTool struct {
	compact        compactMemorySearcher
	freshnessStore freshnessGetter
	timeout        time.Duration
	now            func() time.Time
}

// NewRecallUserMemoryTool returns a recall_user_memory tool backed by the
// given compact store. Returns nil when compact is nil.
func NewRecallUserMemoryTool(compact compactMemorySearcher) *RecallUserMemoryTool {
	if compact == nil {
		return nil
	}
	return &RecallUserMemoryTool{
		compact: compact,
		timeout: recallUserMemoryTimeout,
		now:     time.Now,
	}
}

// SetFreshnessStore injects a freshness getter so Execute can annotate hits
// with freshness= tokens and degraded_read=true (Phase 7C contract).
func (t *RecallUserMemoryTool) SetFreshnessStore(fs freshnessGetter) {
	t.freshnessStore = fs
}

func (t *RecallUserMemoryTool) Name() string { return "recall_user_memory" }

func (t *RecallUserMemoryTool) Description() string {
	return "Surface validated user facts about the operator (name, location, interests, habits, todos). Returns approved entries only. Optional: query, category (person|preference|fact|todo)."
}

func (t *RecallUserMemoryTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Natural-language query to find relevant user facts.",
			},
			"category": map[string]any{
				"type":        "string",
				"enum":        []string{"person", "preference", "fact", "todo"},
				"description": "Optional: restrict results to a specific memory category.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": fmt.Sprintf("Maximum results to return (default %d, max %d).", recallUserMemoryDefaultLimit, recallUserMemoryMaxLimit),
				"minimum":     1,
				"maximum":     recallUserMemoryMaxLimit,
			},
		},
	}
}

func (t *RecallUserMemoryTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t == nil {
		return "", errors.New("recall_user_memory: tool unavailable")
	}
	query := stringArg(args, "query")
	category := stringArg(args, "category")
	limit := intArg(args, "limit", recallUserMemoryDefaultLimit, 1, recallUserMemoryMaxLimit)

	searchCtx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	// Build FTS query from caller params. When query is empty fall back to
	// "user_memory" — the kind column in compact_memory_fts contains this
	// token for every user_memory row, so FTS5 matches the full slice without
	// leaking into other kinds (which the Kinds filter also enforces).
	ftsQuery := query
	if ftsQuery == "" {
		ftsQuery = "user_memory"
	}

	docs, err := t.compact.Search(searchCtx, ftsQuery, memoryindex.Filter{
		Kinds: []string{memoryindex.KindUserMemory},
		Limit: limit * 3,
	})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(searchCtx.Err(), context.DeadlineExceeded) {
			return "recall_user_memory: search timed out", nil
		}
		return "", fmt.Errorf("recall_user_memory: %w", err)
	}

	// Post-filter: approved (active) entries only; pending proposals omitted.
	// Optional category filter matched against Tags (US-O03 stores category there).
	filtered := make([]memoryindex.Document, 0, len(docs))
	for _, d := range docs {
		if d.Status != "active" {
			continue
		}
		if category != "" && !docHasTag(d, category) {
			continue
		}
		filtered = append(filtered, d)
	}

	// Read projection freshness for the Phase 7C degraded_read annotation.
	var degradedRead bool
	if t.freshnessStore != nil && len(filtered) > 0 {
		if row, found, err := t.freshnessStore.Get(searchCtx, compactMemoryProjectionID); err == nil && found && row.Status != "fresh" {
			degradedRead = true
		}
	}

	// Sort newest-first.
	sort.SliceStable(filtered, func(i, j int) bool {
		return filtered[i].UpdatedAt.After(filtered[j].UpdatedAt)
	})
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}

	if len(filtered) == 0 {
		return "no user facts yet", nil
	}

	return formatUserMemoryResults(filtered, degradedRead, t.now()), nil
}

// docHasTag returns true when any of doc.Tags matches tag (case-insensitive).
func docHasTag(doc memoryindex.Document, tag string) bool {
	for _, t := range doc.Tags {
		if strings.EqualFold(t, tag) {
			return true
		}
	}
	return false
}

// formatUserMemoryResults renders the user memory list as plain markdown,
// mirroring the recall_operational format for consistency.
func formatUserMemoryResults(docs []memoryindex.Document, degradedRead bool, now time.Time) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d user fact(s):", len(docs))
	for _, doc := range docs {
		fmt.Fprintf(&sb, "\n- [user_memory] %s — %s", doc.Handle, compactMemoryLine(doc.Title))
		if age := formatAge(now, doc.UpdatedAt); age != "" {
			fmt.Fprintf(&sb, " (%s)", age)
		}
		// Phase 7C freshness annotation — only when columns are populated.
		if doc.EmbeddingModelID != "" || doc.IndexBuildID != "" {
			var parts []string
			if !doc.UpdatedAt.IsZero() {
				parts = append(parts, "indexed_at="+doc.UpdatedAt.UTC().Format(time.RFC3339))
			}
			if doc.EmbeddingModelID != "" {
				model := doc.EmbeddingModelID
				if len(model) > 12 {
					model = model[:12]
				}
				parts = append(parts, "model="+model)
			}
			if doc.IndexBuildID != "" {
				build := doc.IndexBuildID
				if len(build) > 12 {
					build = build[:12]
				}
				parts = append(parts, "build="+build)
			}
			if len(parts) > 0 {
				fmt.Fprintf(&sb, " freshness=%s", strings.Join(parts, ","))
			}
		}
		if degradedRead {
			sb.WriteString(" degraded_read=true")
		}
		if doc.Body != "" {
			snippet, _ := snippetAround(doc.Body, "", 200)
			if snippet != "" {
				fmt.Fprintf(&sb, "\n  %s", snippet)
			}
		}
	}
	return sb.String()
}
