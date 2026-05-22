package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/storage/reindex"
	"github.com/aura/aura/internal/wiki"
)

// wikiPagePromptVersion stamps `prompt_version` on pages this tool
// creates or modifies. Matches the wiki schema regex (v{n}). Bumping
// requires a coordinated update of wiki.promptVersionRe.
const wikiPagePromptVersion = "v1"

// WikiPageTool is the single dynamic mutation surface Aura uses for her wiki.
// It exposes a four-action verb family in one tool, inspired by picobot's
// memory tool layout but adapted to Aura's structured-frontmatter, graph-aware
// wiki:
//
//   - create  — fresh new page with full frontmatter + body
//   - replace — overwrite an existing page's body and optional frontmatter
//     fields
//   - edit    — surgical find/replace on an existing page's body
//     (Anthropic Edit-tool style; the LLM doesn't have to round-trip
//     the whole body for a small fix)
//   - append  — add a new ## section at the bottom; when source_id
//     is provided, auto-emits `^[src_xxx]` provenance markers
//     (matches source-ingest merge semantics)
//
// Backlink maintenance fires after every action that writes a page, so any
// [[wiki-link]] introduced via this tool gets its reverse edge wired
// automatically. Graph index refreshes on the same path.
//
// Conflict responses (D-03) flow through here unchanged: the wiki
// Store returns a *wiki.ConflictError when expected_updated_at
// doesn't match the on-disk timestamp, and we surface it as a
// structured JSON result for the LLM to read+retry.
type WikiPageTool struct {
	store     *wiki.Store
	submitter reindex.Submitter // optional; nil = skip reindex enqueue
}

// NewWikiPageTool builds the tool. Returns nil if store is missing.
// submitter MAY be nil — the tool degrades gracefully (no reindex
// submission, the search index just lags until a real reindex pass).
func NewWikiPageTool(store *wiki.Store, submitter reindex.Submitter) *WikiPageTool {
	if store == nil {
		return nil
	}
	return &WikiPageTool{store: store, submitter: submitter}
}

func (t *WikiPageTool) Name() string { return "wiki_page" }

func (t *WikiPageTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:            t.Name(),
		Description:     t.Description(),
		Parameters:      t.Parameters(),
		DestructiveHint: true, // replace/edit actions can overwrite or delete page content
		VisibilityTier:  VisibilityActiveTurn,
	}
}

func (t *WikiPageTool) Description() string {
	return "Destructive. Create, replace, edit, or append to a wiki page. WRITER ONLY - does NOT read. Required per action: create(title,body), replace/edit/append(slug,body,expected_updated_at)."
}

func (t *WikiPageTool) Parameters() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"action"},
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"create", "replace", "edit", "append"},
				"description": "Which mutation to apply: create (new page), replace (overwrite body), edit (find/replace), append (add a section).",
			},
			"title": map[string]any{
				"type":        "string",
				"description": "Page title. Required for create. Optional for replace (preserves existing if omitted). Ignored for edit/append.",
			},
			"slug": map[string]any{
				"type":        "string",
				"description": "Target page slug (lowercase, dash-separated). Required for replace/edit/append. Ignored for create — derived from title there.",
			},
			"body": map[string]any{
				"type":        "string",
				"description": "For create/replace: the full markdown body. For append: the new section's content only (the heading is added separately). Ignored for edit.",
			},
			"expected_updated_at": map[string]any{
				"type":        "string",
				"description": "RFC3339 timestamp from the page you just read. Required for replace/edit/append. For create, leave empty or omit — the create path uses the '' create-only sentinel automatically.",
			},
			"category": map[string]any{
				"type":        "string",
				"description": "Optional category. Common values: 'entity', 'concept', or empty. Applied on create/replace.",
			},
			"tags": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Optional tag list. Applied on create/replace.",
			},
			"related": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Optional list of slugs for INTENTIONAL cross-references that don't appear as [[wiki-links]] in body. Body [[wiki-links]] already produce auto-backlinks — DO NOT mirror body links here. Applied on create/replace.",
			},
			"sources": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Optional list of source URLs or src_xxx identifiers. Applied on create/replace.",
			},
			"old_text": map[string]any{
				"type":        "string",
				"description": "edit only: the exact substring to find. Must match exactly one occurrence in the page body.",
			},
			"new_text": map[string]any{
				"type":        "string",
				"description": "edit only: replacement text. Empty string deletes the match.",
			},
			"heading": map[string]any{
				"type":        "string",
				"description": "append only: the ## section heading text (without the leading '## ').",
			},
			"source_id": map[string]any{
				"type":        "string",
				"description": "append only, optional: when provided, each non-empty body line in the new section gets an ^[src_xxx] provenance marker appended, and source_id is added to Sources if not already there.",
			},
		},
		"oneOf": ActionDispatchOneOf([]ActionVariant{
			{Name: "create", RequiredKeys: []string{"title", "body"}},
			{Name: "replace", RequiredKeys: []string{"slug", "body", "expected_updated_at"}},
			{Name: "edit", RequiredKeys: []string{"slug", "old_text", "new_text", "expected_updated_at"}},
			{Name: "append", RequiredKeys: []string{"slug", "heading", "body", "expected_updated_at"}},
		}),
	}
}

// Execute dispatches to the per-action handler. Action-specific
// argument validation happens inside each handler to keep the
// top-level Execute readable.
func (t *WikiPageTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	action := strings.TrimSpace(stringArg(args, "action"))
	switch action {
	case "create":
		return t.doCreate(ctx, args)
	case "replace":
		return t.doReplace(ctx, args)
	case "edit":
		return t.doEdit(ctx, args)
	case "append":
		return t.doAppend(ctx, args)
	case "read", "view", "get", "show":
		// QW-2 (2026-05-21): the model kept calling wiki_page action=read despite
		// the writer-only hint. Each rejection cost a turn. Switched to silent
		// passthrough: serve the read from t.store.ReadPage. Description still
		// nudges callers to use file({action:read}) but if they slip we return
		// the content instead of erroring. Saves a tool round-trip per
		// occurrence.
		return t.doRead(ctx, args)
	case "":
		return "", ActionRequiredError("wiki_page", wikiValidActions, args, wikiActionHints, "create")
	default:
		return "", UnknownActionError("wiki_page", action, wikiValidActions, args)
	}
}

var (
	wikiValidActions = []string{"create", "replace", "edit", "append"}
	wikiActionHints  = []ActionHint{
		// edit + patch shape: old + new on a specific slug
		{Name: "edit", RequiredKeys: []string{"old_text", "new_text"}},
		// replace = full body rewrite
		{Name: "replace", RequiredKeys: []string{"body"}},
		// append = new ## section on an existing page; heading is unique to this action
		{Name: "append", RequiredKeys: []string{"slug", "heading", "body"}},
		// create = brand-new page
		{Name: "create", RequiredKeys: []string{"title", "body"}},
	}
)

// doRead serves wiki_page action=read/view/get/show as a silent passthrough.
// The tool is conceptually writer-only but the LLM keeps calling read on it.
// Returning the page content (instead of an error) saves a tool round-trip.
// A trailing hint nudges the caller toward file({action:read}) for next time
// without blocking the current turn.
func (t *WikiPageTool) doRead(_ context.Context, args map[string]any) (string, error) {
	slug := strings.TrimSpace(stringArg(args, "slug"))
	if slug == "" {
		return "", fmt.Errorf("wiki_page read: slug is required: %w", llm.ErrSchemaValidation)
	}
	page, err := t.store.ReadPage(slug)
	if err != nil {
		return "", fmt.Errorf("wiki_page read [[%s]]: %w", slug, err)
	}
	var sb strings.Builder
	sb.WriteString("# ")
	sb.WriteString(page.Title)
	sb.WriteString("\n")
	sb.WriteString("slug: ")
	sb.WriteString(slug)
	sb.WriteString("\n")
	if page.UpdatedAt != "" {
		sb.WriteString("updated_at: ")
		sb.WriteString(page.UpdatedAt)
		sb.WriteString("\n")
	}
	if page.Category != "" {
		sb.WriteString("category: ")
		sb.WriteString(page.Category)
		sb.WriteString("\n")
	}
	if len(page.Tags) > 0 {
		sb.WriteString("tags: ")
		sb.WriteString(strings.Join(page.Tags, ", "))
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	sb.WriteString(page.Body)
	sb.WriteString("\n\n---\nhint: for next reads prefer file({\"action\":\"read\",\"path\":\"wiki/")
	sb.WriteString(slug)
	sb.WriteString(".md\"}). wiki_page is intended for writes.\n")
	return sb.String(), nil
}

// doCreate writes a brand-new page. expected_updated_at is implicitly
// the '' create-only sentinel — the wiki Store rejects the write with
// a ConflictError if the slug already exists, and we surface that as
// a structured JSON result so the LLM can decide to use "replace" or
// "append" on the existing page instead.
func (t *WikiPageTool) doCreate(ctx context.Context, args map[string]any) (string, error) {
	title := strings.TrimSpace(stringArg(args, "title"))
	body := stringArg(args, "body")
	if title == "" {
		return "", fmt.Errorf("wiki_page create: title is required: %w", llm.ErrSchemaValidation)
	}
	if strings.TrimSpace(body) == "" {
		return "", fmt.Errorf("wiki_page create: body is required: %w", llm.ErrSchemaValidation)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	page := &wiki.Page{
		Title:         title,
		Body:          body,
		Category:      stringArg(args, "category"),
		Tags:          stringSliceArg(args, "tags"),
		Related:       wiki.RelatedFromSlugs(stringSliceArg(args, "related")),
		Sources:       stringSliceArg(args, "sources"),
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: wikiPagePromptVersion,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	return t.writeAndRespond(ctx, page, "")
}

// doReplace overwrites the body (and frontmatter, when present in
// args) of an existing page. Frontmatter fields that aren't in args
// are preserved from the existing page so the LLM doesn't have to
// re-echo every tag/related/source entry to keep them.
func (t *WikiPageTool) doReplace(ctx context.Context, args map[string]any) (string, error) {
	slug := strings.TrimSpace(stringArg(args, "slug"))
	body := stringArg(args, "body")
	expected := stringArg(args, "expected_updated_at")
	if slug == "" {
		return "", fmt.Errorf("wiki_page replace: slug is required: %w", llm.ErrSchemaValidation)
	}
	if strings.TrimSpace(body) == "" {
		return "", fmt.Errorf("wiki_page replace: body is required: %w", llm.ErrSchemaValidation)
	}
	if expected == "" {
		return "", fmt.Errorf("wiki_page replace: expected_updated_at is required: %w", llm.ErrSchemaValidation)
	}
	existing, err := t.store.ReadPage(slug)
	if err != nil {
		return "", fmt.Errorf("wiki_page replace: read existing: %w", err)
	}
	title := strings.TrimSpace(stringArg(args, "title"))
	if title == "" {
		title = existing.Title
	}
	now := time.Now().UTC().Format(time.RFC3339)
	page := &wiki.Page{
		Title:         title,
		Body:          body,
		Category:      pickStringArg(args, "category", existing.Category),
		Tags:          pickStringSliceArg(args, "tags", existing.Tags),
		Related:       wiki.RelatedFromSlugs(pickStringSliceArg(args, "related", wiki.RelatedSlugs(existing.Related))),
		Sources:       pickStringSliceArg(args, "sources", existing.Sources),
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: wikiPagePromptVersion,
		CreatedAt:     existing.CreatedAt,
		UpdatedAt:     now,
	}
	return t.writeAndRespond(ctx, page, expected)
}

// doEdit performs a surgical find/replace on the body of an existing
// page. old_text must match exactly one occurrence — if zero or
// multiple matches are found, the tool errors and the LLM is expected
// to provide a more specific old_text on retry (Anthropic Edit tool
// semantics, robust against accidental whole-word replacements).
func (t *WikiPageTool) doEdit(ctx context.Context, args map[string]any) (string, error) {
	slug := strings.TrimSpace(stringArg(args, "slug"))
	oldText := stringArg(args, "old_text")
	newText := stringArg(args, "new_text")
	expected := stringArg(args, "expected_updated_at")
	if slug == "" {
		return "", fmt.Errorf("wiki_page edit: slug is required: %w", llm.ErrSchemaValidation)
	}
	if oldText == "" {
		return "", fmt.Errorf("wiki_page edit: old_text is required: %w", llm.ErrSchemaValidation)
	}
	if expected == "" {
		return "", fmt.Errorf("wiki_page edit: expected_updated_at is required: %w", llm.ErrSchemaValidation)
	}
	existing, err := t.store.ReadPage(slug)
	if err != nil {
		return "", fmt.Errorf("wiki_page edit: read existing: %w", err)
	}
	occurrences := strings.Count(existing.Body, oldText)
	switch occurrences {
	case 0:
		return "", fmt.Errorf("wiki_page edit: old_text not found in body of %q; provide a verbatim substring", slug)
	case 1:
		// fall through — single match is the safe case.
	default:
		return "", fmt.Errorf("wiki_page edit: old_text appears %d times in %q; widen the context so it matches exactly once", occurrences, slug)
	}
	newBody := strings.Replace(existing.Body, oldText, newText, 1)
	now := time.Now().UTC().Format(time.RFC3339)
	page := *existing // copy frontmatter
	page.Body = newBody
	page.SchemaVersion = wiki.CurrentSchemaVersion
	page.PromptVersion = wikiPagePromptVersion
	page.UpdatedAt = now
	return t.writeAndRespond(ctx, &page, expected)
}

// doAppend adds a new "## {heading}" section to the page's body.
// When source_id is supplied, each non-empty non-heading line in the
// new section gets a ^[src_xxx] marker appended (idempotent — won't
// double-tag) and source_id is added to Sources if not already
// present. Matches source-ingest merge semantics so chat-written citations
// look identical to LLM-extracted ones.
func (t *WikiPageTool) doAppend(ctx context.Context, args map[string]any) (string, error) {
	slug := strings.TrimSpace(stringArg(args, "slug"))
	heading := strings.TrimSpace(stringArg(args, "heading"))
	body := stringArg(args, "body")
	expected := stringArg(args, "expected_updated_at")
	sourceID := strings.TrimSpace(stringArg(args, "source_id"))
	if slug == "" {
		return "", fmt.Errorf("wiki_page append: slug is required: %w", llm.ErrSchemaValidation)
	}
	if heading == "" {
		return "", fmt.Errorf("wiki_page append: heading is required: %w", llm.ErrSchemaValidation)
	}
	if strings.TrimSpace(body) == "" {
		return "", fmt.Errorf("wiki_page append: body is required: %w", llm.ErrSchemaValidation)
	}
	if expected == "" {
		return "", fmt.Errorf("wiki_page append: expected_updated_at is required: %w", llm.ErrSchemaValidation)
	}
	existing, err := t.store.ReadPage(slug)
	if err != nil {
		return "", fmt.Errorf("wiki_page append: read existing: %w", err)
	}
	annotatedBody := body
	if sourceID != "" {
		annotatedBody = annotateLines(body, sourceID)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	page := *existing
	page.Body = strings.TrimRight(existing.Body, "\n") + "\n\n## " + heading + "\n\n" + annotatedBody + "\n"
	if sourceID != "" {
		tag := "source:" + sourceID
		if !containsStringSlice(page.Sources, tag) && !containsStringSlice(page.Sources, sourceID) {
			page.Sources = append(page.Sources, tag)
		}
	}
	page.SchemaVersion = wiki.CurrentSchemaVersion
	page.PromptVersion = wikiPagePromptVersion
	page.UpdatedAt = now
	return t.writeAndRespond(ctx, &page, expected)
}

// writeAndRespond is the shared tail of every action: validate, write
// through the wiki Store (which updates backlinks and graph indexes), submit
// the reindex job, and emit a structured JSON result. ConflictError is
// translated into a successful
// {"error":"conflict",...} result so the LLM retry path stays
// deterministic.
func (t *WikiPageTool) writeAndRespond(ctx context.Context, page *wiki.Page, expectedUpdatedAt string) (string, error) {
	if err := t.store.WritePage(ctx, page, expectedUpdatedAt); err != nil {
		var conflict *wiki.ConflictError
		if errors.As(err, &conflict) {
			payload := map[string]string{
				"error":               "conflict",
				"slug":                conflict.Slug,
				"expected_updated_at": conflict.Expected,
				"actual_updated_at":   conflict.Actual,
			}
			data, mErr := json.Marshal(payload)
			if mErr != nil {
				return "", fmt.Errorf("wiki_page: marshal conflict: %w", mErr)
			}
			return string(data), nil
		}
		return "", fmt.Errorf("wiki_page: %w", err)
	}
	slug := wiki.Slug(page.Title)
	if t.submitter != nil {
		_ = t.submitter.Submit(reindex.Job{Slug: slug, Op: reindex.OpUpsert})
	}
	result, err := json.Marshal(map[string]string{
		"status":     "ok",
		"slug":       slug,
		"updated_at": page.UpdatedAt,
	})
	if err != nil {
		return "", fmt.Errorf("wiki_page: marshal result: %w", err)
	}
	return string(result), nil
}

// annotateLines walks a multi-line body and appends a provenance
// marker to each non-empty, non-heading, non-blockquote line. Headings
// (#-prefixed) and blockquote markers (>-prefixed) skip the appendix
// so the rendered structure stays clean. Matches source-ingest line annotation
// so a chat-driven append produces the same shape as a source-driven
// multi-page-touch update.
func annotateLines(body, sourceID string) string {
	if sourceID == "" {
		return body
	}
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		// Blockquote lines: annotate the inner content, keeping the
		// "> " prefix intact.
		if strings.HasPrefix(trimmed, ">") {
			inner := strings.TrimPrefix(trimmed, ">")
			inner = strings.TrimSpace(inner)
			if inner == "" {
				continue
			}
			lines[i] = "> " + wiki.AnnotateProvenance(inner, sourceID)
			continue
		}
		lines[i] = wiki.AnnotateProvenance(line, sourceID)
	}
	return strings.Join(lines, "\n")
}

// pickStringArg returns args[key] when present and non-empty, else
// fallback. Used by doReplace so missing optional frontmatter fields
// preserve the existing page's value instead of clearing it.
func pickStringArg(args map[string]any, key, fallback string) string {
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return fallback
}

// pickStringSliceArg mirrors pickStringArg for []string. An explicitly
// empty array in args clears the field (intentional); a missing key
// preserves existing.
func pickStringSliceArg(args map[string]any, key string, fallback []string) []string {
	if _, ok := args[key]; !ok {
		return fallback
	}
	return stringSliceArg(args, key)
}

// containsStringSlice is a tiny membership test kept local to the
// tools package to avoid importing slices.Contains everywhere on Go
// builds where the stdlib version is fine but the lint cost of an
// extra import bothers.
func containsStringSlice(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// Ensure WikiPageTool satisfies the Tool interface at compile time.
var _ Tool = (*WikiPageTool)(nil)
