package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/documents"
)

// DocumentSearchBackend retrieves cited document chunks for the document_search
// tool. Retrieve runs the two-stage pipeline (vector/BM25 seed -> rerank seeds ->
// 1-hop graph-expand winners) and is fail-soft: with no reranker configured it
// returns the current sparse-search order (no regression).
type DocumentSearchBackend interface {
	Retrieve(ctx context.Context, req documents.SearchRequest) ([]documents.SearchHit, error)
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
			"Set document_id when the user is asking about a specific indexed document. Keep limit small unless broad recall is needed. " +
			"Use it for the user's OWN uploaded/indexed files — NOT the public web (that is the web search/fetch tools). " +
			"Example: {\"query\":\"valvola di sicurezza della caldaia\",\"limit\":5}.",
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
	hits, err := t.Searcher.Retrieve(ctx, documents.SearchRequest{
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
