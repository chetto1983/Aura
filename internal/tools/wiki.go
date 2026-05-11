package tools

import (
	"context"
	"errors"
	"fmt"

	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/reindex"
	"github.com/aura/aura/internal/wiki"
)

// WriteWikiPageTool exposes wiki page creation/update to the LLM via the
// agent loop. Required arguments are enforced both by the JSON Schema (D-01)
// and by Execute's own field validation (which wraps llm.ErrSchemaValidation
// so the retry wrapper buckets failures as CONTENT — Plan 01).
//
// Conflict responses are returned as a successful tool RESULT containing
// structured JSON (D-03), NOT as an error string — the LLM parses the
// result and re-reads + retries deterministically.
//
// Privileged frontmatter keys (slug, unversioned, schema_version,
// prompt_version, created_at, updated_at) are server-controlled:
// they are absent from Parameters().properties and never read from args
// in Execute (T-02-B privilege escalation prevention).
type WriteWikiPageTool struct {
	store     *wiki.Store
	submitter reindex.Submitter // optional; nil = skip reindex enqueue
}

// NewWriteWikiPageTool builds the tool. Returns nil if store is missing.
// submitter MAY be nil — the tool degrades gracefully (no reindex submission).
func NewWriteWikiPageTool(store *wiki.Store, submitter reindex.Submitter) *WriteWikiPageTool {
	if store == nil {
		return nil
	}
	return &WriteWikiPageTool{store: store, submitter: submitter}
}

func (t *WriteWikiPageTool) Name() string { return "write_wiki_page" }

func (t *WriteWikiPageTool) Description() string {
	return "Create or update a wiki page. Always read the page first to obtain `updated_at`; pass `expected_updated_at=''` only when creating a brand-new page; on conflict, re-read and retry. Slug is derived from title — do not supply it. Server controls schema_version, prompt_version, created_at, updated_at, and unversioned."
}

func (t *WriteWikiPageTool) Parameters() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"title", "body", "expected_updated_at"},
		"properties": map[string]any{
			"title": map[string]any{
				"type":        "string",
				"description": "Human-readable page title. Slug is derived from this.",
			},
			"body": map[string]any{
				"type":        "string",
				"description": "Full markdown body (replaces existing body — there is no patch mode).",
			},
			"expected_updated_at": map[string]any{
				"type":        "string",
				"description": "RFC3339 timestamp from the page you read. Empty string '' to create a brand-new page (rejected if a page with this slug already exists).",
			},
			"category": map[string]any{
				"type":        "string",
				"description": "Optional category tag.",
			},
			"tags": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Optional tag list.",
			},
			"related": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Optional list of related page slugs.",
			},
			"sources": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Optional list of source URLs or identifiers.",
			},
		},
	}
}

// Execute is implemented in Task 2.
func (t *WriteWikiPageTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	return "", errors.New("not implemented")
}

// Ensure WriteWikiPageTool satisfies the Tool interface at compile time.
var _ Tool = (*WriteWikiPageTool)(nil)

// Silence unused import during Task 1 stub phase.
var _ = llm.ErrSchemaValidation
var _ = fmt.Sprintf
