package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/chetto1983/aura/internal/documents"
)

// DocumentLibrary is the retrieval seam the document_search tool depends on,
// narrowed to the one method it calls so tests can supply a fake without the
// whole documents service.
type DocumentLibrary interface {
	Retrieve(context.Context, documents.RetrievalRequest) (documents.RetrievalResponse, error)
}

// DocumentSearch is the tool that answers questions about the user's uploaded
// documents, returning passages that carry their own provenance.
type DocumentSearch struct {
	Library DocumentLibrary
}

type documentSearchArgs struct {
	Query       string   `json:"query"`
	Limit       int      `json:"limit"`
	DocumentIDs []string `json:"document_ids"`
	Neighbours  int      `json:"neighbours"`
}

func (t *DocumentSearch) Spec() Spec {
	return Spec{
		Name:    "document_search",
		Summary: "Search the user's uploaded documents and return provenance-bearing passages and openable files.",
		// The shared half lives in documents.SearchToolContract so the MCP surface over the
		// same handlers cannot drift from it; only the sentences about THIS runtime are here.
		Description: documents.SearchToolContract +
			" document_open writes the file into /workspace for shell_exec; uploaded documents are not " +
			"otherwise on the filesystem. Files YOU created live under /workspace: read those with " +
			"read_file/search_files instead. " +
			"Example: {\"query\":\"customer code for WPT SRL\",\"document_ids\":[\"doc_9f2c\"]}.",
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": {"type": "string", "minLength": 1, "description": "Natural-language question, fact, topic, or filename."},
    "limit": {"type": "integer", "minimum": 1, "maximum": 50, "description": "Maximum documents to return. Default 8."},
    "document_ids": {"type": "array", "maxItems": 100, "items": {"type": "string", "minLength": 1}, "description": "Optional owner-scoped document ids."},
    "neighbours": {"type": "integer", "minimum": 0, "maximum": 3, "description": "Also return this many passages either side of every hit. Default 0."}
  },
  "required": ["query"]
}`),
		// This remains in the working set because choosing filesystem or public-web
		// search before the owner's private library is both a quality and privacy failure.
		Deferred: false,
	}
}

func (t *DocumentSearch) Execute(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	if t.Library == nil {
		return ToolResult{}, fmt.Errorf("document_search: document library is not configured")
	}
	var args documentSearchArgs
	if err := decodeDocumentSearchArgs(raw, &args); err != nil {
		return ToolResult{}, fmt.Errorf("document_search args: %w", err)
	}
	args.Query = strings.TrimSpace(args.Query)
	if args.Query == "" {
		return ToolResult{}, fmt.Errorf("document_search: query is required")
	}
	if args.Limit < 0 {
		return ToolResult{}, fmt.Errorf("document_search: limit must be positive")
	}
	if args.Neighbours < 0 || args.Neighbours > documents.MaxRetrievalNeighbours {
		return ToolResult{}, fmt.Errorf(
			"document_search: neighbours must be between 0 and %d", documents.MaxRetrievalNeighbours,
		)
	}

	response, err := t.Library.Retrieve(ctx, documents.RetrievalRequest{
		IdentityID: ownerFromContext(ctx), Query: args.Query,
		Limit: effectiveDocumentLimit(args.Limit), DocumentIDs: args.DocumentIDs,
		Neighbours: args.Neighbours, SourceScopes: documents.SourceScopesFromContext(ctx),
	})
	if err != nil {
		return ToolResult{}, fmt.Errorf("document_search: %w", err)
	}
	out, err := json.Marshal(response)
	if err != nil {
		return ToolResult{}, fmt.Errorf("document_search: marshal results: %w", err)
	}
	result, err := NewResult(ctx, string(out))
	if err != nil {
		return ToolResult{}, err
	}
	result.Provenance = &ToolResultProvenance{Source: "document_search", Trust: TrustTrusted}
	return result, nil
}

func decodeDocumentSearchArgs(raw json.RawMessage, dst *documentSearchArgs) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

func effectiveDocumentLimit(limit int) int {
	if limit <= 0 {
		return 8
	}
	if limit > 50 {
		return 50
	}
	return limit
}
