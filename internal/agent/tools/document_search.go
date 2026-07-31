package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/documents"
)

// DocumentLibrary ranks one identity's documents by what each one IS. It answers
// "which file", which is the only question an index still has to answer now that
// document_open can hand over the file itself.
type DocumentLibrary interface {
	SearchDigests(ctx context.Context, identityID, query string, limit int) ([]documents.DigestHit, error)
}

// DocumentSearch is the library index.
//
// It used to run a two-stage retrieval pipeline — vector/BM25 seed, cross-encoder
// rerank, 1-hop graph expansion — and return passages. That answered "what does
// this document say" and could not answer "how many", which is what people
// actually ask a spreadsheet: measured on a 5889-row customer list, an exact
// lookup scored 100% and every aggregate scored 0% at every k, because the answer
// is a property of the whole document and lives in no passage. The same held for
// a 29 MB manual (616 distinct parameters, no k).
//
// So it returns FILES now. The agent picks one and opens it with document_open,
// then computes with the LibreOffice/python already in its container.
type DocumentSearch struct {
	Library DocumentLibrary
}

type documentSearchArgs struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

func (t *DocumentSearch) Spec() Spec {
	return Spec{
		Name:    "document_search",
		Summary: "List the user's uploaded documents, ranked by what each one is, to pick which file to open.",
		Description: "THE tool for any question about the user's own uploaded documents (PDF, DOCX, XLSX, PPTX, " +
			"CSV, HTML, MD, TXT, and more). It returns the DOCUMENTS themselves — id, title, tags and a short " +
			"description of what each contains — NOT their text. Uploaded documents do not live on the filesystem, " +
			"so fs_glob/fs_grep will not find them; call this first. Then call document_open with the document_id " +
			"you chose: it writes the real file into /workspace, where you can read, convert and compute on it " +
			"with shell_exec (LibreOffice, python with openpyxl/pandas, PyMuPDF, pdftotext are all installed). " +
			"That is how you answer anything needing the whole file — a count, a sum, an average, a maximum, a " +
			"grouping, 'how many', or a conversion. Leave query empty to list the library, which is what 'the file " +
			"I just uploaded' means. This lists the user's OWN uploads; files YOU created live on the filesystem " +
			"under /workspace — read those with fs_read/fs_grep, and add one to this library with document_index. " +
			"Example: {\"query\":\"customer list with sales reps\"}.",
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": {"type": "string", "description": "What the document is about, in plain words. Empty lists the whole library, newest first."},
    "limit": {"type": "integer", "minimum": 1, "maximum": 50, "description": "Maximum documents to return. Default 8."}
  }
}`),
		// Deferred, with the regression that once un-deferred it in mind: hiding
		// retrieval behind tool_search made the agent answer document questions with
		// the visible fs_glob/fs_grep and never discover it. What changed is that
		// fs_glob/fs_grep are no longer visible either — with only the four primitives
		// always on there is no plausible-looking wrong tool left to grab — and a
		// deferred tool called by name now gets its schema back in the same step
		// rather than an errand. If retrieval regresses again, this is the first line
		// to revisit, and the fix is the <documents> prompt block naming it.
		Deferred: true,
	}
}

func (t *DocumentSearch) Execute(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	if t.Library == nil {
		return ToolResult{}, fmt.Errorf("document_search: document library is not configured")
	}
	var args documentSearchArgs
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return ToolResult{}, fmt.Errorf("document_search args: %w", err)
		}
	}
	if args.Limit < 0 {
		return ToolResult{}, fmt.Errorf("document_search: limit must be positive")
	}

	hits, err := t.Library.SearchDigests(
		ctx, ownerFromContext(ctx), strings.TrimSpace(args.Query), effectiveDocumentLimit(args.Limit))
	if err != nil {
		return ToolResult{}, fmt.Errorf("document_search: %w", err)
	}
	out, err := json.Marshal(map[string]any{"documents": hits})
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

func effectiveDocumentLimit(limit int) int {
	if limit <= 0 {
		return 8
	}
	if limit > 50 {
		return 50
	}
	return limit
}
