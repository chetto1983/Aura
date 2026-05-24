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
	recallOperationalDefaultLimit = 5
	recallOperationalMaxLimit     = 20
	recallOperationalTimeout      = 5 * time.Second
)

// RecallOperationalTool surfaces Aura's validated operational lessons.
// Lessons are promoted from repeated tool failures via the Phase-N pipeline
// (learning.PromoteLessons → WriteApprovedLesson) and stored as
// compact_memory_documents with Kind=operational, Status=active.
// Pending proposals are never visible here — only approved entries.
type RecallOperationalTool struct {
	compact        compactMemorySearcher
	freshnessStore freshnessGetter
	timeout        time.Duration
	now            func() time.Time
}

type operationalRecallMarker interface {
	MarkOperationalRecalled(ctx context.Context, ids []string, recalledAt time.Time) error
}

// NewRecallOperationalTool returns a recall_operational tool backed by the
// given compact store. Returns nil when compact is nil.
func NewRecallOperationalTool(compact compactMemorySearcher) *RecallOperationalTool {
	if compact == nil {
		return nil
	}
	return &RecallOperationalTool{
		compact: compact,
		timeout: recallOperationalTimeout,
		now:     time.Now,
	}
}

// SetFreshnessStore injects a freshness getter so Execute can annotate hits
// with freshness= tokens and degraded_read=true (Phase 7C contract).
func (t *RecallOperationalTool) SetFreshnessStore(fs freshnessGetter) {
	t.freshnessStore = fs
}

func (t *RecallOperationalTool) Name() string { return "recall_operational" }

func (t *RecallOperationalTool) Description() string {
	return "Surface Aura's validated operational lessons (failed approaches, recurring errors, policy notes). Returns approved entries only - pending proposals excluded. Optional: tool_name, error_class."
}

func (t *RecallOperationalTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Natural-language query to find relevant lessons.",
			},
			"tool_name": map[string]any{
				"type":        "string",
				"description": "Optional: restrict results to lessons about this tool name.",
			},
			"error_class": map[string]any{
				"type":        "string",
				"description": "Optional: restrict results to lessons about this error class.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": fmt.Sprintf("Maximum results to return (default %d, max %d).", recallOperationalDefaultLimit, recallOperationalMaxLimit),
				"minimum":     1,
				"maximum":     recallOperationalMaxLimit,
			},
		},
		"examples": []any{
			map[string]any{},
			map[string]any{"query": "tool failed because of missing field"},
			map[string]any{"tool_name": "agent_note", "limit": 10},
			map[string]any{"error_class": "schema_validation"},
		},
	}
}

func (t *RecallOperationalTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t == nil {
		return "", errors.New("recall_operational: tool unavailable")
	}
	query := stringArg(args, "query")
	toolName := stringArg(args, "tool_name")
	errorClass := stringArg(args, "error_class")
	limit := intArg(args, "limit", recallOperationalDefaultLimit, 1, recallOperationalMaxLimit)

	searchCtx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	// Build FTS query from caller params. When all are empty fall back to
	// "lesson" — all operational titles begin with "Lesson:" so FTS5 matches
	// every row in the kind=operational slice without polluting other kinds.
	parts := make([]string, 0, 3)
	if query != "" {
		parts = append(parts, query)
	}
	if toolName != "" {
		parts = append(parts, toolName)
	}
	if errorClass != "" {
		parts = append(parts, errorClass)
	}
	ftsQuery := strings.Join(parts, " ")
	if ftsQuery == "" {
		ftsQuery = "lesson"
	}

	// Fetch extra rows to absorb the status post-filter.
	docs, err := t.compact.Search(searchCtx, ftsQuery, memoryindex.Filter{
		Kinds: []string{memoryindex.KindOperational},
		Limit: limit * 3,
	})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(searchCtx.Err(), context.DeadlineExceeded) {
			return "recall_operational: search timed out", nil
		}
		return "", fmt.Errorf("recall_operational: %w", err)
	}

	// Post-filter: approved (active) entries only; pending proposals omitted.
	filtered := make([]memoryindex.Document, 0, len(docs))
	for _, d := range docs {
		if d.Status != "active" {
			continue
		}
		// Narrow by tool_name when requested: title is "Lesson: <tool> <class>".
		if toolName != "" && !strings.Contains(strings.ToLower(d.Title), strings.ToLower(toolName)) {
			continue
		}
		if errorClass != "" && !strings.Contains(strings.ToLower(d.Title), strings.ToLower(errorClass)) {
			continue
		}
		filtered = append(filtered, d)
	}

	// Read projection freshness once for the Phase 7C degraded_read annotation.
	var degradedRead bool
	if t.freshnessStore != nil && len(filtered) > 0 {
		if row, found, err := t.freshnessStore.Get(searchCtx, compactMemoryProjectionID); err == nil && found && row.Status != "fresh" {
			degradedRead = true
		}
	}

	// Sort newest-first (FTS rank ordering replaced by recency for operational lessons).
	sort.SliceStable(filtered, func(i, j int) bool {
		return filtered[i].UpdatedAt.After(filtered[j].UpdatedAt)
	})
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}

	if len(filtered) == 0 {
		return "no operational lessons yet", nil
	}
	if marker, ok := t.compact.(operationalRecallMarker); ok {
		ids := make([]string, 0, len(filtered))
		for _, doc := range filtered {
			ids = append(ids, doc.ID)
		}
		if err := marker.MarkOperationalRecalled(searchCtx, ids, t.now().UTC()); err != nil {
			return "", fmt.Errorf("recall_operational: mark recalled: %w", err)
		}
	}

	return formatOperationalResults(filtered, degradedRead, t.now()), nil
}

func formatOperationalResults(docs []memoryindex.Document, degradedRead bool, now time.Time) string {
	return formatIndexedDocumentResults(docs, degradedRead, now, "operational", "operational lesson(s)")
}
