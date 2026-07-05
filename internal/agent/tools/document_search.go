package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/identityctx"
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
		Summary: "Search the user's uploaded/indexed documents and return cited chunks.",
		Description: "THE tool for any question about the user's own uploaded or indexed documents/files " +
			"(PDF, DOCX, PPTX, XLSX, HTML, CSV, MD, TXT, and more). Aura ingests uploaded documents into a " +
			"searchable Neo4j knowledge base (two-stage retrieval: vector/BM25 seed -> cross-encoder rerank -> " +
			"graph-expand). Uploaded documents do NOT live on the filesystem — do NOT use fs_glob, fs_grep, or the " +
			"shell to look for them; they will not be found there. When the user refers to 'this document', 'the " +
			"file I uploaded', 'the manual/spreadsheet/PDF', or asks what a document says/contains/lists, call " +
			"document_search FIRST. Results are chunks with document id, chunk id, file name, locator (page, " +
			"sheet/rows, or section), relevance score, and text — cite them. Set document_id to scope to one " +
			"specific indexed document (e.g. the attachment's document_id). This is for the user's OWN files, NOT " +
			"the public web (use web search/fetch for that). Example: {\"query\":\"safety valve pressure rating\",\"limit\":5}.",
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": {"type": "string", "description": "Plain text search query."},
    "document_id": {"type": "string", "description": "Optional indexed document id to restrict search."},
    "limit": {"type": "integer", "minimum": 1, "maximum": 20, "description": "Maximum hits to return. Default 8."}
  },
  "required": ["query"]
}`),
		// Always-visible (NOT deferred): document_search is the headline "chat with your
		// documents" capability. Hiding it behind tool_search made the agent default to the
		// visible fs_glob/fs_grep for uploaded-document questions and never discover retrieval
		// (the live upload->chat regression). Senior agentic-RAG systems (elysia, Neo4j
		// llm-graph-builder, LibreChat) keep retrieval permanently visible; mirror that.
		Deferred: false,
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
		// Scope retrieval to the authenticated principal. When AURA_MUSR_ISOLATION is on,
		// the documents plane fails closed on a foreign/empty identity (Phase 36 MUSR-01);
		// when off, this id is ignored and the pre-existing unscoped path runs (D-13).
		IdentityID: identityctx.IdentityID(ctx),
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
