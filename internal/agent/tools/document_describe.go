package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// DocumentDescriber records what a document is, against the calling identity.
type DocumentDescriber interface {
	SetDigest(ctx context.Context, identityID, documentID, description string) error
}

// DocumentDescribe is how the library learns what the machine could not measure.
//
// Every ingested file already carries a CARD, written at ingest from the file
// itself: sheet names, column headers with their frequent values, sampled rows,
// or — for a PDF, whose words are glyph indices behind a font encoding — its
// metadata and page count. So this tool is no longer the author of a document's
// description. It is the editor: it records what a reader knows and an extractor
// cannot, and it writes to its OWN column, so a note can never delete the
// structure the card measured.
//
// The distinction matters at the prompt level, which is why the tool description
// leads with it: a model that believes an undescribed file is undescribed will
// spend a turn restating the column headers that are already indexed.
type DocumentDescribe struct {
	Documents DocumentDescriber
}

// maxDescriptionChars matches the digest column's own cap. A description is a
// filing-cabinet label; the file itself is one document_open away.
const maxDescriptionChars = 4000

type documentDescribeArgs struct {
	DocumentID  string `json:"document_id"`
	Description string `json:"description"`
}

func (t *DocumentDescribe) Spec() Spec {
	return Spec{
		Name:    "document_describe",
		Summary: "Add what you learned about a document to the description Aura already wrote for it at upload.",
		Description: "Every document ALREADY has a machine-written description, made from the file when it was " +
			"uploaded: sheets, column headers, frequent values, sample rows, page counts. document_search returns " +
			"it and ranks on it, so a file is findable without you. Do NOT restate it. Use this tool only to add " +
			"what reading the file taught you and the structure could not say: what the document is FOR, the " +
			"period it covers, the business meaning of a cryptic column or code, or the subject of a PDF (whose " +
			"text is never read at upload — for those this is the only description there is). This replaces your " +
			"OWN note in full, not the machine's description, so restate anything of yours that still applies. " +
			"One or two sentences. Example: {\"document_id\":\"doc_9f2c\",\"description\":\"Customer master for " +
			"Piedmont and Liguria used for the 2026 sales-rep review; 'Cliente' is the SAP account number and " +
			"'Filiale Ass.' the branch that owns the account.\"}.",
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "document_id": {"type": "string", "description": "The document to describe, as returned by document_search or document_open."},
    "description": {"type": "string", "description": "What the document contains, in the words someone would search by."}
  },
  "required": ["document_id", "description"]
}`),
		// A Mutating spec MUST carry all four fields: OperationFingerprint refuses a
		// partially-declared one with "tool operation metadata is incomplete", and
		// the tool then fails on every call. Caught live — the model retried four
		// times and the description never landed.
		Mutating:       true,
		OperationScope: OperationScopeAgent, OperationNormalizer: OperationNormalizerCanonical,
		ReplayPolicy: ReplayToolResult,
		// Deferred: a deliberate follow-up to having read a file. document_open's
		// description is where the model learns it exists, at the moment it becomes
		// relevant.
		Deferred: true,
	}
}

func (t *DocumentDescribe) Execute(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	if t.Documents == nil {
		return ToolResult{}, fmt.Errorf("document_describe: document store is not configured")
	}
	var args documentDescribeArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return ToolResult{}, fmt.Errorf("document_describe args: %w", err)
	}
	args.DocumentID = strings.TrimSpace(args.DocumentID)
	if args.DocumentID == "" {
		return ToolResult{}, fmt.Errorf("document_describe: document_id is required")
	}
	description := strings.TrimSpace(args.Description)
	if description == "" {
		// Blanking a description is not a thing to do by accident: it would make a
		// findable document unfindable, silently.
		return ToolResult{}, fmt.Errorf(
			"document_describe: description is required; to correct one, write the new description in full")
	}
	if len(description) > maxDescriptionChars {
		return ToolResult{}, fmt.Errorf(
			"document_describe: description is %d characters, cap is %d — describe the document, do not summarize its contents",
			len(description), maxDescriptionChars)
	}

	if err := t.Documents.SetDigest(ctx, ownerFromContext(ctx), args.DocumentID, description); err != nil {
		return ToolResult{}, fmt.Errorf("document_describe: %w", err)
	}
	out, err := json.Marshal(map[string]any{
		"document_id": args.DocumentID,
		"described":   true,
		"characters":  len(description),
	})
	if err != nil {
		return ToolResult{}, fmt.Errorf("document_describe: marshal result: %w", err)
	}
	result, err := NewResult(ctx, string(out))
	if err != nil {
		return ToolResult{}, err
	}
	result.Provenance = &ToolResultProvenance{Source: "document_describe", Trust: TrustUntrusted}
	return result, nil
}
