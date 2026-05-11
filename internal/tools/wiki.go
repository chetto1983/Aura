package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

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

// Execute validates the LLM-supplied args, writes the wiki page, surfaces
// conflicts as structured JSON tool results (D-03), and submits a non-blocking
// reindex job on success.
//
// Error mapping:
//   - Missing/empty required field → fmt.Errorf("%w", llm.ErrSchemaValidation)
//     (CONTENT bucket — retry wrapper re-prompts the LLM)
//   - *wiki.ConflictError → nil error, JSON tool result with error:"conflict"
//     (D-03 — LLM re-reads and retries deterministically)
//   - Any other store error → propagated as-is (IO/other → retry wrapper decides)
func (t *WriteWikiPageTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	title := stringArg(args, "title")
	body := stringArg(args, "body")

	// Presence test for expected_updated_at: empty string "" is the
	// create-only sentinel — a valid value. Only a missing key is invalid.
	rawExpected, hasExpected := args["expected_updated_at"]
	var expectedUpdatedAt string
	if hasExpected {
		if s, ok := rawExpected.(string); ok {
			expectedUpdatedAt = s
		} else {
			return "", fmt.Errorf("write_wiki_page: expected_updated_at must be a string: %w", llm.ErrSchemaValidation)
		}
	}

	if strings.TrimSpace(title) == "" {
		return "", fmt.Errorf("write_wiki_page: title is required: %w", llm.ErrSchemaValidation)
	}
	if strings.TrimSpace(body) == "" {
		return "", fmt.Errorf("write_wiki_page: body is required: %w", llm.ErrSchemaValidation)
	}
	if !hasExpected {
		return "", fmt.Errorf("write_wiki_page: expected_updated_at is required: %w", llm.ErrSchemaValidation)
	}

	// Server-controlled metadata — LLM input is IGNORED for these keys
	// (privilege escalation prevention, T-02-B mitigation).
	// PromptVersion is the tool-write provenance marker. The historical plan
	// spec called for "write_wiki_page/v1" but wiki.Validate's promptVersionRe
	// only accepts the canonical v{n} shape. Until the regex is extended to
	// accept "tool/v{n}" we use "v1" — the divergence is intentional and
	// tracked here (F-039). Changing the constant requires a matching
	// promptVersionRe update in internal/wiki/validate.go.
	now := time.Now().UTC().Format(time.RFC3339)
	page := &wiki.Page{
		Title:         strings.TrimSpace(title),
		Body:          body,                       // full replace — no patch mode (D-01)
		Category:      stringArg(args, "category"),
		Tags:          stringSliceArg(args, "tags"),
		Related:       stringSliceArg(args, "related"),
		Sources:       stringSliceArg(args, "sources"),
		SchemaVersion: wiki.CurrentSchemaVersion,  // D-09 LOCK — NOT from args
		PromptVersion: "v1",                       // see F-039 note above — server-controlled
		CreatedAt:     now,                        // store.WritePage may preserve on update
		UpdatedAt:     now,
		// Unversioned: NEVER set from args (server-managed only — D-17/D-18 in wiki.Store)
	}

	if err := t.store.WritePage(ctx, page, expectedUpdatedAt); err != nil {
		var conflict *wiki.ConflictError
		if errors.As(err, &conflict) {
			// D-03: return a successful tool RESULT (nil error) containing
			// structured JSON. The LLM parses the result, re-reads the page
			// to get the current updated_at, and retries.
			payload := map[string]string{
				"error":               "conflict",
				"slug":                conflict.Slug,
				"expected_updated_at": conflict.Expected,
				"actual_updated_at":   conflict.Actual,
			}
			data, mErr := json.Marshal(payload)
			if mErr != nil {
				return "", fmt.Errorf("write_wiki_page: marshal conflict: %w", mErr)
			}
			return string(data), nil
		}
		return "", fmt.Errorf("write_wiki_page: %w", err)
	}

	slug := wiki.Slug(page.Title)
	if t.submitter != nil {
		// Non-blocking fire-and-forget (drop-newest semantics from Plan 02).
		// Return value (bool) is intentionally ignored — drop is safe because
		// disk is source of truth and the worker re-reads on drain.
		_ = t.submitter.Submit(reindex.Job{Slug: slug, Op: reindex.OpUpsert})
	}

	result, err := json.Marshal(map[string]string{
		"status":     "ok",
		"slug":       slug,
		"updated_at": page.UpdatedAt,
	})
	if err != nil {
		return "", fmt.Errorf("write_wiki_page: marshal result: %w", err)
	}
	return string(result), nil
}

// Ensure WriteWikiPageTool satisfies the Tool interface at compile time.
var _ Tool = (*WriteWikiPageTool)(nil)
