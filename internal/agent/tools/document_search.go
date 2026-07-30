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

type versionedDocumentSearchBackend interface {
	FreezeRetrievalPlans(
		context.Context,
		documents.SearchRequest,
	) (documents.FrozenRetrievalPlans, error)
	ExecuteRetrievalPlan(
		context.Context,
		documents.FrozenRetrievalPlans,
		string,
		documents.SearchRequest,
	) (documents.RetrievalResult, error)
}

// DocumentRetrievalInput is the query-free projection presented to adaptive
// control. The user's query and document scope remain exogenous execution input.
type DocumentRetrievalInput struct {
	OwnerID         string
	RequestID       string
	PointOrdinal    uint32
	QueryLength     int
	DocumentID      string
	MaxResults      int
	PlanIDs         []string
	CatalogRevision string
	Frozen          documents.FrozenRetrievalPlans
}

type DocumentRetrievalExecutor func(
	context.Context,
	string,
) (documents.RetrievalResult, error)

// DocumentRetrievalControl persists assignment and delivery around one frozen
// plan execution.
type DocumentRetrievalControl interface {
	RetrieveDocuments(
		context.Context,
		DocumentRetrievalInput,
		DocumentRetrievalExecutor,
		DocumentRetrievalExecutor,
	) (documents.RetrievalResult, error)
}

type DocumentSearch struct {
	Searcher DocumentSearchBackend
	Adaptive DocumentRetrievalControl
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
			"the public web (use web search/fetch for that). Files YOU create live on the filesystem under " +
			"/workspace — search those with fs_read/fs_grep, and make one searchable here by indexing it with " +
			"document_index. Example: {\"query\":\"safety valve pressure rating\",\"limit\":5}.",
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": {"type": "string", "description": "Plain text search query."},
    "document_id": {"type": "string", "description": "Optional indexed document id to restrict search."},
    "limit": {"type": "integer", "minimum": 1, "maximum": 20, "description": "Maximum hits to return. Default 8."}
  },
  "required": ["query"]
}`),
		// Deferred again, with the regression that un-deferred it in mind. Hiding retrieval
		// behind tool_search once made the agent answer document questions with the VISIBLE
		// fs_glob/fs_grep and never discover it — the live upload->chat regression. What
		// changed is that fs_glob/fs_grep are no longer visible either: with only the four
		// primitives always on, there is no plausible-looking wrong tool left to grab, which
		// is what actually caused that miss. If retrieval regresses again, this is the first
		// line to revisit — and the fix is the <documents> prompt block naming it, not a
		// permanent 391-token seat in every manifest.
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
	req := documents.SearchRequest{
		Query:      args.Query,
		DocumentID: strings.TrimSpace(args.DocumentID),
		Limit:      args.Limit,
		// Scope retrieval to the authenticated principal, mapping an empty CLI/no-principal
		// ctx to the seeded `local` UUID (…001) via ownerFromContext — parity with
		// shell_bg/runner (ME-01). The operator's own documents are owned by the `local`
		// UUID (documents/backfill.go), so with AURA_MUSR_ISOLATION on the CLI operator still
		// retrieves them instead of failing closed to zero results, while a web principal
		// stays scoped to itself. When the flag is off, this id is ignored and the
		// pre-existing unscoped path runs (D-13).
		IdentityID: ownerFromContext(ctx),
	}
	hits, err := t.retrieve(ctx, req)
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

func (t *DocumentSearch) retrieve(
	ctx context.Context,
	req documents.SearchRequest,
) ([]documents.SearchHit, error) {
	backend, ok := t.Searcher.(versionedDocumentSearchBackend)
	if t.Adaptive == nil || !ok {
		return t.Searcher.Retrieve(ctx, req)
	}
	frozen, err := backend.FreezeRetrievalPlans(ctx, req)
	if err != nil {
		return t.Searcher.Retrieve(ctx, req)
	}
	results := make(map[string]documents.RetrievalResult)
	execute := func(
		ctx context.Context,
		planID string,
	) (documents.RetrievalResult, error) {
		if result, exists := results[planID]; exists {
			return result, nil
		}
		result, err := backend.ExecuteRetrievalPlan(ctx, frozen, planID, req)
		if err == nil {
			results[planID] = result
		}
		return result, err
	}
	static := func(
		ctx context.Context,
		_ string,
	) (documents.RetrievalResult, error) {
		return execute(ctx, documents.RetrievalPlanStatic)
	}
	result, err := t.Adaptive.RetrieveDocuments(
		ctx,
		DocumentRetrievalInput{
			OwnerID: req.IdentityID, RequestID: RequestIDFromContext(ctx),
			PointOrdinal: adaptivePointOrdinal(ctx), QueryLength: len(req.Query),
			DocumentID: req.DocumentID, MaxResults: effectiveDocumentLimit(req.Limit),
			PlanIDs: frozen.PlanIDs(), CatalogRevision: frozen.CatalogRevision(),
			Frozen: frozen,
		},
		execute,
		static,
	)
	if err != nil || frozen.ValidateResult(result, req) != nil {
		result, err = static(ctx, documents.RetrievalPlanStatic)
	}
	if err != nil {
		return nil, err
	}
	if err := frozen.ValidateResult(result, req); err != nil {
		return nil, err
	}
	return result.Flatten(), nil
}

func effectiveDocumentLimit(limit int) int {
	if limit <= 0 {
		return 8
	}
	if limit > 20 {
		return 20
	}
	return limit
}
