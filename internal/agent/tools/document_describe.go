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

// DocumentDescribe is how the library learns.
//
// Nothing reads a document at upload time any more — the bytes go to storage and
// the catalog gets a name, tags and a size. That is enough to find `Clienti.xlsx`
// and useless for `export_2024_final_v3.xlsx`. Rather than pay an extraction
// pass over every file on the chance someone asks about it, the description is
// written by the one party who has actually looked: the agent, the first time it
// opens the file.
//
// So this is the counterpart of document_open. Open, look, and say what it was —
// after which document_search can find that file by its columns, its subject or
// its period, for every question after this one.
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
		Summary: "Record what a document contains, so document_search can find it later by its subject or columns.",
		Description: "After you open a document and see what is in it, write that down here — it is stored on the " +
			"document and is what document_search ranks on from then on. Do this whenever you opened a file whose " +
			"NAME did not say what it holds, or when you learn something about it a future search would need: the " +
			"sheets and their column headers, the subject, the period covered, the kind of records. Write it in the " +
			"words the user would search by, not in a summary of the content — the content stays in the file, and " +
			"document_open hands that over. One or two sentences is right. This replaces the description entirely, " +
			"so include what still applies. Example: {\"document_id\":\"doc_9f2c\",\"description\":\"Customer list, " +
			"5889 rows. Columns: area, branch, customer code, company name, town, sales rep. Piedmont and Liguria, " +
			"2026.\"}.",
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "document_id": {"type": "string", "description": "The document to describe, as returned by document_search or document_open."},
    "description": {"type": "string", "description": "What the document contains, in the words someone would search by."}
  },
  "required": ["document_id", "description"]
}`),
		Mutating: true,
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
