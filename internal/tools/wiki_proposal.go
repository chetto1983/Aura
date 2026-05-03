package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/aura/aura/internal/conversation/summarizer"
)

// ProposeWikiChangeTool inserts a pending wiki update into the review queue.
// It deliberately does not mutate wiki files; dashboard approval applies it.
type ProposeWikiChangeTool struct {
	store *summarizer.SummariesStore
}

func NewProposeWikiChangeTool(store *summarizer.SummariesStore) *ProposeWikiChangeTool {
	if store == nil {
		return nil
	}
	return &ProposeWikiChangeTool{store: store}
}

func (t *ProposeWikiChangeTool) Name() string { return "propose_wiki_change" }

func (t *ProposeWikiChangeTool) Description() string {
	return "Propose a durable wiki update for human review. Use this proactively when you discover useful new knowledge, missing pages, or safe improvements that should compound the second brain but should not be written directly. Inserts into the dashboard Summaries review queue; it never mutates wiki files."
}

func (t *ProposeWikiChangeTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"new", "patch"},
				"description": "new creates a proposed new page; patch appends a proposed note to target_slug.",
			},
			"fact": map[string]any{
				"type":        "string",
				"description": "Concise markdown proposal body. Use [[slug]] links when helpful. Do not include secrets or raw logs.",
			},
			"target_slug": map[string]any{
				"type":        "string",
				"description": "Existing wiki slug to patch. Required when action=patch; ignored for action=new.",
			},
			"category": map[string]any{
				"type":        "string",
				"description": "Optional wiki category, e.g. project, person, workflow, fact.",
			},
			"related": map[string]any{
				"type":        "array",
				"description": "Optional related wiki slugs.",
				"items":       map[string]any{"type": "string"},
			},
			"source_turn_ids": map[string]any{
				"type":        "array",
				"description": "Optional archived conversation turn IDs that support this proposal.",
				"items":       map[string]any{"type": "integer"},
			},
			"confidence": map[string]any{
				"type":        "number",
				"description": "Optional confidence from 0 to 1. Defaults to 1.",
			},
		},
		"required": []string{"action", "fact"},
	}
}

func (t *ProposeWikiChangeTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t.store == nil {
		return "", fmt.Errorf("propose_wiki_change: review store unavailable")
	}
	action, err := requiredString(args, "action")
	if err != nil {
		return "", err
	}
	fact, err := requiredString(args, "fact")
	if err != nil {
		return "", err
	}
	proposal, err := t.store.Propose(ctx, summarizer.ProposalInput{
		ChatID:        userIDAsInt64(ctx),
		Fact:          fact,
		Action:        strings.TrimSpace(action),
		TargetSlug:    strings.TrimSpace(stringArg(args, "target_slug")),
		Similarity:    numberArg(args, "confidence"),
		SourceTurnIDs: int64SliceArg(args, "source_turn_ids"),
		Category:      strings.TrimSpace(stringArg(args, "category")),
		RelatedSlugs:  stringSliceArg(args, "related"),
	})
	if err != nil {
		return "", fmt.Errorf("propose_wiki_change: %w", err)
	}
	resp := proposeWikiChangeResponse{
		OK:         true,
		ID:         proposal.ID,
		Status:     proposal.Status,
		Action:     proposal.Action,
		TargetSlug: proposal.TargetSlug,
		ReviewPath: "/summaries",
	}
	out, err := json.Marshal(resp)
	if err != nil {
		return "", fmt.Errorf("propose_wiki_change: marshal response: %w", err)
	}
	return string(out), nil
}

type proposeWikiChangeResponse struct {
	OK         bool   `json:"ok"`
	ID         int64  `json:"id"`
	Status     string `json:"status"`
	Action     string `json:"action"`
	TargetSlug string `json:"target_slug,omitempty"`
	ReviewPath string `json:"review_path"`
}

func userIDAsInt64(ctx context.Context) int64 {
	id := strings.TrimSpace(UserIDFromContext(ctx))
	if id == "" {
		return 0
	}
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func int64SliceArg(args map[string]any, key string) []int64 {
	v, ok := args[key]
	if !ok || v == nil {
		return nil
	}
	switch values := v.(type) {
	case []int64:
		return append([]int64(nil), values...)
	case []int:
		out := make([]int64, 0, len(values))
		for _, value := range values {
			out = append(out, int64(value))
		}
		return out
	case []any:
		out := make([]int64, 0, len(values))
		for _, value := range values {
			switch n := value.(type) {
			case int:
				out = append(out, int64(n))
			case int64:
				out = append(out, n)
			case float64:
				if n == float64(int64(n)) {
					out = append(out, int64(n))
				}
			case json.Number:
				if parsed, err := n.Int64(); err == nil {
					out = append(out, parsed)
				}
			}
		}
		return out
	default:
		return nil
	}
}

func numberArg(args map[string]any, key string) float64 {
	v, ok := args[key]
	if !ok || v == nil {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	default:
		return 0
	}
}
