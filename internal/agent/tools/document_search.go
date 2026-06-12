package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/documents"
)

type DocumentSearchBackend interface {
	Search(ctx context.Context, req documents.SearchRequest) ([]documents.SearchHit, error)
}

type DocumentSearch struct {
	Searcher DocumentSearchBackend
}

type documentSearchArgs struct {
	Query      string `json:"query"`
	DocumentID string `json:"document_id"`
	Limit      int    `json:"limit"`
}

func (t *DocumentSearch) Spec() Spec {
	return Spec{
		Name:    "document_search",
		Summary: "Search indexed user documents and return cited chunks.",
		Description: "Search documents that Aura has indexed through the native Neo4j document pipeline. " +
			"Use this for questions about uploaded PDFs, spreadsheets, and DOCX files. " +
			"Results are chunks with document id, chunk id, file name, locator (page, sheet/rows, or section), score, and text. " +
			"Set document_id when the user is asking about a specific indexed document. Keep limit small unless broad recall is needed.",
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": {"type": "string", "description": "Plain text search query."},
    "document_id": {"type": "string", "description": "Optional indexed document id to restrict search."},
    "limit": {"type": "integer", "minimum": 1, "maximum": 20, "description": "Maximum hits to return. Default 8."}
  },
  "required": ["query"]
}`),
		Deferred: true,
	}
}

func (t *DocumentSearch) Execute(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	if t.Searcher == nil {
		return ToolResult{}, fmt.Errorf("document_search: searcher is not configured")
	}
	var args documentSearchArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return ToolResult{}, fmt.Errorf("document_search args: %w", err)
	}
	args.Query = strings.TrimSpace(args.Query)
	if args.Query == "" {
		return ToolResult{}, fmt.Errorf("document_search: query is required")
	}
	if args.Limit < 0 {
		return ToolResult{}, fmt.Errorf("document_search: limit must be positive")
	}
	if args.Limit > 20 {
		args.Limit = 20
	}
	hits, err := t.Searcher.Search(ctx, documents.SearchRequest{
		Query:      args.Query,
		DocumentID: strings.TrimSpace(args.DocumentID),
		Limit:      args.Limit,
	})
	if err != nil {
		return ToolResult{}, err
	}
	out, err := json.Marshal(map[string]any{"hits": hits})
	if err != nil {
		return ToolResult{}, fmt.Errorf("document_search: marshal results: %w", err)
	}
	result, err := NewResult(ctx, string(out))
	if err != nil {
		return ToolResult{}, err
	}
	result.Provenance = &ToolResultProvenance{Source: "document_search", Trust: TrustUntrusted}
	return result, nil
}
