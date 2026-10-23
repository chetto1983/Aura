package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/chetto1983/aura/internal/docfields"
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
	// Fields resolves and counts the field filters. Nil refuses them: there is no store to read.
	Fields DocumentFieldFilters
}

type documentSearchArgs struct {
	Query       string   `json:"query"`
	Limit       int      `json:"limit"`
	DocumentIDs []string `json:"document_ids"`
	Neighbours  int      `json:"neighbours"`
	documentFieldArgs
}

// documentSearchResult is the retrieval answer plus what each field filter did and the lists a
// filter can name. Those two come first: encoding/json writes fields in declaration order, and a
// result past the preview cap is cut inside the passages, while the agent picks its next filter
// from them.
type documentSearchResult struct {
	Filters []documentFilterReport  `json:"filters,omitempty"`
	Lists   *docfields.LibraryLists `json:"library_lists,omitempty"`
	documents.RetrievalResponse
}

func (t *DocumentSearch) Spec() Spec {
	return Spec{
		Name:    "document_search",
		Summary: "Search the user's uploaded documents and return provenance-bearing passages and openable files.",
		// This text is the ONE description of the capability: cmd/aura's MCP server exposes
		// the same handlers for evaluation and takes its description from this Spec, so what
		// is measured there is what the agent is actually told here.
		Description: "THE tool for questions about the operator's uploaded documents (PDF, DOCX, XLSX, PPTX, " +
			"CSV, HTML, MD, TXT and more). It returns reconciled documents with bounded passages; each passage " +
			"carries a citation_token, the source SHA-256 and a locator holding the document's OWN heading_path " +
			"and character span, each document carries per-leg retrieval evidence with its size, passage count " +
			"and index time, and the answer carries an explicit degradation or abstention status. " +
			"Read the passages before answering: a filename match alone is not evidence, and abstained:true " +
			"means this library does not hold the answer -- say so rather than answering from your own " +
			"knowledge. " +
			"Before calling an empty answer final, read indexing: it is how far the ingest has got, and " +
			"status \"running\" means the library has not caught up yet, so a document added moments ago may " +
			"simply not be there YET rather than be absent -- say that, and search again instead of " +
			"concluding the file does not exist. status \"ready\" means the ingest has caught up, so an " +
			"empty answer really is an empty answer. " +
			"Cite ONLY the citation_token and the locator's heading_path returned here. Never cite a section, " +
			"chapter or page number you read inside the passage text: a passage starts wherever its chunk " +
			"starts, so the numbering visible in it is usually not its own. heading_path names the " +
			"section the passage STARTS in, and a long passage runs on past it: it places the passage, " +
			"not every line inside it, so quote the text you are relying on rather than telling a reader " +
			"that section holds it. " +
			"When a passage stops mid-table or mid-definition, the rest of it is in the ADJACENT chunk, which " +
			"no rephrasing of the query will ever rank -- repeat the search with neighbours to pull the " +
			"passages either side of every hit, each with its own citation_token and locator. " +
			"When a hit reports requires_open, or the question is about the whole file rather than one passage " +
			"-- any count, sum, average, maximum, grouping, sort, cross-column filter or 'how many' over a " +
			"spreadsheet or table, and any conversion -- call document_open with that document_id; it writes " +
			"the real file into /workspace, and uploaded documents are not otherwise on the filesystem. " +
			"query is required and may be a question, a topic, an entity, an exact identifier or a filename; " +
			"document_ids optionally narrows the search to ids already returned to this operator. " +
			"type, counterparty and tags narrow it to documents whose fields hold those names, from the " +
			"operator's own lists (case-insensitive, exact); tags_mode all asks for every tag; date_from and " +
			"date_to bound the document's date (YYYY-MM-DD); include_unreviewed false keeps only values the " +
			"operator set or accepted. filters in the result says what each admitted, whether a match rests on " +
			"suggestions, and the closest names on the list when one matched nothing -- correct the name or drop " +
			"the filter. " +
			"library_lists in every result names the types and tags the operator's library holds, with how " +
			"many documents carry each: when the question asks for a kind of document or a quality its words " +
			"do not spell (internal, draft), filter by the listed name. " +
			"Files YOU created live under /workspace: read those with read_file/search_files instead. " +
			"Example: {\"query\":\"customer code for WPT SRL\",\"document_ids\":[\"doc_9f2c\"]}.",
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": {"type": "string", "minLength": 1, "description": "Natural-language question, fact, topic, or filename."},
    "limit": {"type": "integer", "minimum": 1, "maximum": 50, "description": "Maximum documents to return. Default 8."},
    "document_ids": {"type": "array", "maxItems": 100, "items": {"type": "string", "minLength": 1}, "description": "Optional owner-scoped document ids."},
    "neighbours": {"type": "integer", "minimum": 0, "maximum": 3, "description": "Also return this many passages either side of every hit. Default 0."},
    "type": {"type": "array", "maxItems": 20, "items": {"type": "string", "minLength": 1}, "description": "Document type names, e.g. Fattura."},
    "counterparty": {"type": "array", "maxItems": 20, "items": {"type": "string", "minLength": 1}, "description": "Counterparty names."},
    "tags": {"type": "array", "maxItems": 20, "items": {"type": "string", "minLength": 1}, "description": "Tag names."},
    "tags_mode": {"type": "string", "enum": ["any", "all"], "description": "any (default) or all of tags."},
    "date_from": {"type": "string", "pattern": "^[0-9]{4}-[0-9]{2}-[0-9]{2}$", "description": "Earliest document date, YYYY-MM-DD."},
    "date_to": {"type": "string", "pattern": "^[0-9]{4}-[0-9]{2}-[0-9]{2}$", "description": "Latest document date, YYYY-MM-DD."},
    "include_unreviewed": {"type": "boolean", "description": "Match suggested values too. Default true."}
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
	if err := decodeStrictArgs(raw, &args); err != nil {
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

	owner := ownerFromContext(ctx)
	fields, filters, err := t.fieldFilter(ctx, owner, args.documentFieldArgs)
	if err != nil {
		return ToolResult{}, fmt.Errorf("document_search: %w", err)
	}
	response := filterAbstention(args.Query)
	if !admitsNothing(filters) {
		response, err = t.Library.Retrieve(ctx, documents.RetrievalRequest{
			IdentityID: owner, Query: args.Query,
			Limit: effectiveDocumentLimit(args.Limit), DocumentIDs: args.DocumentIDs,
			Neighbours: args.Neighbours, SourceScopes: documents.SourceScopesFromContext(ctx), Fields: fields,
		})
		if err != nil {
			return ToolResult{}, fmt.Errorf("document_search: %w", err)
		}
	}
	out, err := json.Marshal(documentSearchResult{RetrievalResponse: response, Filters: filters, Lists: t.libraryLists(ctx, owner)})
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

// decodeStrictArgs decodes exactly one JSON value into dst, refusing unknown fields, so a
// tool whose schema forbids extra properties also refuses them at call time.
func decodeStrictArgs(raw json.RawMessage, dst any) error {
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
