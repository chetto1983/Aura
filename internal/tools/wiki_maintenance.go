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

// CleanWikiMemoryTool turns the lint-and-fix playbook into an automated agent
// action: dry-run by default, apply only when explicitly requested.
type CleanWikiMemoryTool struct {
	store *wiki.Store
}

func NewCleanWikiMemoryTool(store *wiki.Store) *CleanWikiMemoryTool {
	return &CleanWikiMemoryTool{store: store}
}

func (t *CleanWikiMemoryTool) Name() string { return "clean_wiki_memory" }

func (t *CleanWikiMemoryTool) Description() string {
	return "Audit wiki memory quality and optionally repair it automatically. Dry-run by default; apply=true renames safely inferred opaque source pages, creates shared hub pages, repairs obvious alias links, updates related refs, rebuilds index.md, and appends an audit log entry."
}

func (t *CleanWikiMemoryTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"apply": map[string]any{
				"type":        "boolean",
				"description": "When false or omitted, only report planned repairs. When true, write safe source renames, hub pages, and link repairs.",
			},
		},
	}
}

func (t *CleanWikiMemoryTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t.store == nil {
		return "", errors.New("clean_wiki_memory: wiki store unavailable")
	}
	apply := boolArg(args, "apply")
	report, err := t.store.CleanMemory(ctx, wiki.MemoryHygieneOptions{Apply: apply})
	if err != nil {
		return "", fmt.Errorf("clean_wiki_memory: %w", err)
	}
	return truncateForToolContext(formatMemoryHygieneReport(report, apply), maxWikiMaintToolChars), nil
}

func formatMemoryHygieneReport(report *wiki.MemoryHygieneReport, applied bool) string {
	var sb strings.Builder
	if applied {
		fmt.Fprintf(&sb, "Applied wiki memory hygiene to %d page(s).\n", report.Pages)
	} else {
		fmt.Fprintf(&sb, "Dry run: wiki memory hygiene scanned %d page(s).\n", report.Pages)
	}
	fmt.Fprintf(&sb, "Broken links: %d\n", len(report.BrokenLinks))
	fmt.Fprintf(&sb, "Orphans: %d\n", len(report.Orphans))

	if applied {
		for _, rename := range report.RenamedPages {
			fmt.Fprintf(&sb, "- renamed [[%s]] -> [[%s]] (%s)\n", rename.From, rename.To, rename.Reason)
		}
		for _, slug := range report.CreatedHubs {
			fmt.Fprintf(&sb, "- created hub [[%s]]\n", slug)
		}
		for _, repair := range report.RepairedLinks {
			fmt.Fprintf(&sb, "- repaired [[%s]] -> [[%s]] in [[%s]] (%s)\n", repair.Target, repair.Replacement, repair.From, repair.Location)
		}
		if len(report.TouchedPages) > 0 {
			fmt.Fprintf(&sb, "Touched pages: [[%s]]\n", strings.Join(report.TouchedPages, "]], [["))
		}
		return sb.String()
	}

	for _, slug := range report.PlannedHubs {
		fmt.Fprintf(&sb, "- would create hub [[%s]]\n", slug)
	}
	for _, rename := range report.RenamedPages {
		fmt.Fprintf(&sb, "- would rename [[%s]] -> [[%s]] (%s)\n", rename.From, rename.To, rename.Reason)
	}
	for _, repair := range report.RepairedLinks {
		fmt.Fprintf(&sb, "- would repair [[%s]] -> [[%s]] in [[%s]] (%s)\n", repair.Target, repair.Replacement, repair.From, repair.Location)
	}
	if len(report.PlannedHubs) == 0 && len(report.RenamedPages) == 0 && len(report.RepairedLinks) == 0 && len(report.BrokenLinks) == 0 {
		sb.WriteString("Wiki memory graph is already connected and clean.\n")
	}
	return sb.String()
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
