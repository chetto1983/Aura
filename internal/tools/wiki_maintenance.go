package tools

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/aura/aura/internal/wiki"
)

// Output cap for wiki-maintenance tools — same budget as the source tools
// so a list_wiki / lint_wiki call can't bury the LLM context.
const maxWikiMaintToolChars = 8000

// listWikiDefaultLimit is what list_wiki returns when the LLM omits the
// limit arg; max enforces the upper bound to keep outputs bounded even
// for hostile inputs.
const (
	listWikiDefaultLimit = 50
	listWikiMaxLimit     = 200
)

// ListWikiTool exposes the page catalog so the LLM can navigate the wiki
// without re-reading index.md every time. Returns slug, title, and
// category for each page, sorted by category then slug.
type ListWikiTool struct {
	store *wiki.Store
}

func NewListWikiTool(store *wiki.Store) *ListWikiTool {
	return &ListWikiTool{store: store}
}

func (t *ListWikiTool) Name() string { return "list_wiki" }

func (t *ListWikiTool) Description() string {
	return "List wiki pages by category. Optional category filter; default limit 50, max 200. Use this to find existing pages before writing new ones."
}

func (t *ListWikiTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"category": map[string]any{
				"type":        "string",
				"description": "Filter to a single category (e.g. \"sources\", \"engineering\"). Empty returns all categories.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of pages to return. Default 50, max 200.",
			},
		},
	}
}

func (t *ListWikiTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t.store == nil {
		return "", errors.New("list_wiki: wiki store unavailable")
	}
	categoryFilter := strings.TrimSpace(stringArg(args, "category"))
	limit := intArg(args, "limit", listWikiDefaultLimit, 1, listWikiMaxLimit)

	slugs, err := t.store.ListPages()
	if err != nil {
		return "", fmt.Errorf("list_wiki: %w", err)
	}

	type entry struct {
		slug, title, category string
	}
	var entries []entry
	for _, slug := range slugs {
		page, err := t.store.ReadPage(slug)
		if err != nil {
			continue
		}
		cat := page.Category
		if cat == "" {
			cat = "uncategorized"
		}
		if categoryFilter != "" && !strings.EqualFold(cat, categoryFilter) {
			continue
		}
		entries = append(entries, entry{slug: slug, title: page.Title, category: cat})
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].category != entries[j].category {
			return entries[i].category < entries[j].category
		}
		return entries[i].slug < entries[j].slug
	})

	total := len(entries)
	truncated := false
	if total > limit {
		entries = entries[:limit]
		truncated = true
	}

	var sb strings.Builder
	if total == 0 {
		if categoryFilter != "" {
			fmt.Fprintf(&sb, "No wiki pages found in category %q.\n", categoryFilter)
		} else {
			sb.WriteString("Wiki is empty.\n")
		}
		return sb.String(), nil
	}

	if categoryFilter != "" {
		fmt.Fprintf(&sb, "%d page(s) in category %q", total, categoryFilter)
	} else {
		fmt.Fprintf(&sb, "%d wiki page(s)", total)
	}
	if truncated {
		fmt.Fprintf(&sb, " (showing first %d)", limit)
	}
	sb.WriteString(":\n\n")

	currentCat := ""
	for _, e := range entries {
		if e.category != currentCat {
			fmt.Fprintf(&sb, "## %s\n", e.category)
			currentCat = e.category
		}
		fmt.Fprintf(&sb, "- [[%s]] %s\n", e.slug, e.title)
	}

	return truncateForToolContext(sb.String(), maxWikiMaintToolChars), nil
}

// LintWikiTool surfaces wiki health problems (missing categories, broken
// [[wiki-links]], broken related refs). Returns a grouped report so the
// LLM can decide which pages to fix.
type LintWikiTool struct {
	store *wiki.Store
}

func NewLintWikiTool(store *wiki.Store) *LintWikiTool {
	return &LintWikiTool{store: store}
}

func (t *LintWikiTool) Name() string { return "lint_wiki" }

func (t *LintWikiTool) Description() string {
	return "Check the wiki for broken links, missing categories, and orphaned references. Returns a list of issues grouped by page."
}

func (t *LintWikiTool) Parameters() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}

func (t *LintWikiTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t.store == nil {
		return "", errors.New("lint_wiki: wiki store unavailable")
	}
	issues, err := t.store.Lint(ctx)
	if err != nil {
		return "", fmt.Errorf("lint_wiki: %w", err)
	}
	if len(issues) == 0 {
		return "Wiki is clean: no issues found.", nil
	}

	bySlug := make(map[string][]string)
	for _, issue := range issues {
		bySlug[issue.Slug] = append(bySlug[issue.Slug], issue.Message)
	}
	slugs := make([]string, 0, len(bySlug))
	for slug := range bySlug {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)

	var sb strings.Builder
	fmt.Fprintf(&sb, "%d wiki issue(s) across %d page(s):\n\n", len(issues), len(bySlug))
	for _, slug := range slugs {
		fmt.Fprintf(&sb, "## [[%s]]\n", slug)
		for _, msg := range bySlug[slug] {
			fmt.Fprintf(&sb, "- %s\n", msg)
		}
	}

	return truncateForToolContext(sb.String(), maxWikiMaintToolChars), nil
}

// RebuildIndexTool regenerates index.md from the on-disk wiki pages.
// Useful after manual edits or recovery — WritePage / DeletePage already
// keep the index current automatically.
type RebuildIndexTool struct {
	store *wiki.Store
}

func NewRebuildIndexTool(store *wiki.Store) *RebuildIndexTool {
	return &RebuildIndexTool{store: store}
}

func (t *RebuildIndexTool) Name() string { return "rebuild_index" }

func (t *RebuildIndexTool) Description() string {
	return "Regenerate wiki index.md from the current pages on disk. Use after manual edits or to recover from a corrupted index."
}

func (t *RebuildIndexTool) Parameters() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}

func (t *RebuildIndexTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t.store == nil {
		return "", errors.New("rebuild_index: wiki store unavailable")
	}
	t.store.RebuildIndex(ctx)
	slugs, err := t.store.ListPages()
	if err != nil {
		return "", fmt.Errorf("rebuild_index: %w", err)
	}
	return fmt.Sprintf("Index rebuilt: %d page(s) cataloged in index.md.", len(slugs)), nil
}

// AppendLogTool lets the LLM record events that don't go through
// WritePage / DeletePage — query logs, lint passes, periodic syntheses.
// Slug is optional; empty means the entry is not tied to a single page.
type AppendLogTool struct {
	store *wiki.Store
}

func NewAppendLogTool(store *wiki.Store) *AppendLogTool {
	return &AppendLogTool{store: store}
}

func (t *AppendLogTool) Name() string { return "append_log" }

func (t *AppendLogTool) Description() string {
	return "Append a chronological entry to wiki/log.md. Use for actions that don't already auto-log (query summaries, periodic syntheses, manual notes). action is required; slug is optional."
}

func (t *AppendLogTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "Short action verb or phrase (e.g. \"query\", \"summary\", \"lint-pass\"). Max 50 chars.",
			},
			"slug": map[string]any{
				"type":        "string",
				"description": "Optional wiki slug the entry pertains to. Empty for global log entries.",
			},
		},
		"required": []string{"action"},
	}
}

const maxLogActionChars = 50

func (t *AppendLogTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t.store == nil {
		return "", errors.New("append_log: wiki store unavailable")
	}
	action, err := requiredString(args, "action")
	if err != nil {
		return "", err
	}
	action = strings.TrimSpace(action)
	if action == "" {
		return "", errors.New("append_log: action must not be empty")
	}
	if len(action) > maxLogActionChars {
		action = action[:maxLogActionChars]
	}
	slug := strings.TrimSpace(stringArg(args, "slug"))

	t.store.AppendLog(ctx, action, slug)

	if slug == "" {
		return fmt.Sprintf("Logged: %s", action), nil
	}
	return fmt.Sprintf("Logged: %s [[%s]]", action, slug), nil
}

