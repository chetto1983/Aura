package tools

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	pathpkg "path"
	"strings"

	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// DocumentOpenBackend streams the original bytes behind an indexed document,
// scoped to the calling identity.
type DocumentOpenBackend interface {
	OpenDocument(
		ctx context.Context,
		identityID, documentID string,
	) (io.ReadCloser, documents.OpenedDocument, error)
}

// DocumentOpen is the inverse of document_index: that tool takes a workspace
// file into the searchable index, this one brings an indexed document back out
// as a real file the agent can compute on.
//
// It exists because retrieval has a ceiling that tuning does not move. Measured
// on a 5889-row customer spreadsheet: an exact lookup scores 100% (BM25), and
// "how many customers in Torino" scores 0% at every k — the answer is a property
// of the whole set and sits in no chunk. The box already carries LibreOffice,
// openpyxl and pandas, so once the agent holds the file the question is
// arithmetic rather than recall.
//
// The copy lands INSIDE the caller's per-identity box, through the same router
// seam fs_write uses, and that is the whole reason Router is here. Writing it
// host-side was a live defect (Clienti.xlsx, 2026-08-03): the aura container and
// the box mount DIFFERENT volumes at /workspace, so the tool reported a path that
// was real where nobody looks — shell_exec, fs_read and the agent's own eyes are
// all in the box. A path this tool returns must be one the agent can then open.
type DocumentOpen struct {
	Documents DocumentOpenBackend
	Router    *usersandbox.SandboxRouter
}

// openedDocumentsDir is the fixed subdirectory materialized copies land in, and
// openedDocumentsBoxDir is where that sits inside the box. The destination is
// never caller-chosen: a working copy of a user document is not something the
// agent should be able to scatter across the workspace.
const (
	openedDocumentsDir    = "documents"
	openedDocumentsBoxDir = boxWorkspaceRoot + "/" + openedDocumentsDir
)

type documentOpenArgs struct {
	DocumentID string `json:"document_id"`
	FileName   string `json:"file_name"`
}

func (t *DocumentOpen) Spec() Spec {
	return Spec{
		Name:    "document_open",
		Summary: "Download an indexed document to /workspace as a real file you can open, convert, or compute on.",
		Description: "Get the ORIGINAL file of an indexed document written into /workspace/documents/, then work on " +
			"it with shell_exec — LibreOffice (soffice --headless), python with openpyxl/pandas, PyMuPDF and " +
			"pdftotext are all installed. Use this INSTEAD of document_search whenever the answer needs the whole " +
			"file rather than a passage: any count, sum, average, maximum, grouping, sort, cross-column filter or " +
			"'how many' over a spreadsheet or table, any conversion, and any question document_search answered with " +
			"chunks that do not actually contain the answer. document_search finds WHICH document (use the " +
			"document_id from its hits); document_open hands you the file itself. Spreadsheets especially: chunked " +
			"text cannot answer aggregates at any relevance, the file answers them exactly. Returns the workspace " +
			"path, file name, size and sha256. Once you have looked inside, if the file name did not already say " +
			"what it holds, record it with document_describe — that is what makes it findable next time. " +
			"Example: {\"document_id\":\"doc_9f2c…\"}.",
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "document_id": {"type": "string", "description": "Document id from a document_search hit (doc_…) or a catalog uuid."},
    "file_name": {"type": "string", "description": "Optional name for the written file (no directories). Defaults to the original file name."}
  },
  "required": ["document_id"]
}`),
		// NOT deferred, because it is the second half of one motion. document_search
		// answers WHICH file; this one puts it on disk so it can be computed on. Leaving
		// it deferred meant every document question paid a search round trip in the
		// middle of a flow the operator experiences as a single question.
		Deferred: false,
	}
}

func (t *DocumentOpen) Execute(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	if t.Documents == nil {
		return ToolResult{}, fmt.Errorf("document_open: document backend is not configured")
	}
	var args documentOpenArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return ToolResult{}, fmt.Errorf("document_open args: %w", err)
	}
	args.DocumentID = strings.TrimSpace(args.DocumentID)
	if args.DocumentID == "" {
		return ToolResult{}, fmt.Errorf("document_open: document_id is required")
	}
	if err := validateOpenFileName(args.FileName); err != nil {
		return ToolResult{}, err
	}

	// Route BEFORE the object-store read: a call that will be denied must not pay for
	// bytes it can never write, and a box that cannot be reached DENIES — there is no
	// host arm to fall back to (D-09/GATE-01).
	handle, routeErr := t.Router.Route(ctx)
	if routeErr != nil {
		return sandboxUnavailableResult("document_open", routeErr), nil
	}

	body, meta, err := t.Documents.OpenDocument(ctx, ownerFromContext(ctx), args.DocumentID)
	if err != nil {
		return ToolResult{}, fmt.Errorf("document_open: %w", err)
	}
	defer func() { _ = body.Close() }()

	name := strings.TrimSpace(args.FileName)
	if name == "" {
		name = strings.TrimSpace(meta.FileName)
	}
	// The backend's own name goes through the SAME rule as the caller's, and an empty one is
	// refused outright: the name becomes a path component, so a "../.." in it would let
	// the join walk out of the documents directory and an empty string would resolve to
	// the directory itself. StagedDocumentPath owns both checks, and the delete resolves
	// the file to remove through that same function.
	boxPath, err := StagedDocumentPath(meta.DocumentID, name)
	if err != nil {
		return ToolResult{}, fmt.Errorf("document_open: document %s: %w", args.DocumentID, err)
	}
	// A write failure is a plain error, NOT the sandbox_unavailable deny the route uses: it is
	// just as likely to be the object store dying mid-download, and telling the model its
	// container is down and an operator must restore it is advice it can only act on by
	// retrying forever. The route above is where the containment answer belongs.
	if err := t.write(ctx, handle, boxPath, meta.SizeBytes, body); err != nil {
		return ToolResult{}, fmt.Errorf("document_open: %w", err)
	}

	out, err := json.Marshal(map[string]any{
		"path":      boxPath,
		"file_name": pathpkg.Base(boxPath),
		"mime_type": meta.MIMEType,
		// The catalog's size is what was written, not merely what was claimed: the copy-in
		// declares it to tar up front and fails if the stream delivers any other count, so a
		// short file cannot be reported as a whole one.
		"size_bytes":  meta.SizeBytes,
		"sha256":      meta.SHA256,
		"document_id": meta.DocumentID,
	})
	if err != nil {
		return ToolResult{}, fmt.Errorf("document_open: marshal result: %w", err)
	}
	result, err := NewResult(ctx, string(out))
	if err != nil {
		return ToolResult{}, err
	}
	result.Provenance = &ToolResultProvenance{Source: "document_open", Trust: TrustUntrusted}
	return result, nil
}

// write streams size bytes of body into boxPath inside the caller's box. Nothing is
// buffered on the way: an indexed document is an operator-chosen file, up to the
// 100 MiB ingest ceiling, and the aura container has 768 MiB in total.
//
// A failed copy takes its partial file with it. The daemon extracts the tar as it
// reads it, so a source that dies mid-stream leaves a SHORT file behind, and a
// truncated spreadsheet that looks like a whole one is worse than no file at all:
// the agent would compute a confident wrong answer from it.
func (t *DocumentOpen) write(
	ctx context.Context,
	h usersandbox.BoxHandle,
	boxPath string,
	size int64,
	body io.Reader,
) error {
	if err := t.Router.WriteFileStream(ctx, h, boxPath, size, body); err != nil {
		_, _ = t.Router.Exec(ctx, h, usersandbox.ExecRequest{Command: "rm -f -- " + ShellQuoteArg(boxPath)})
		return fmt.Errorf("write %s: %w", boxPath, err)
	}
	return nil
}

// StagedDocumentDirectory derives the single sandbox directory for a logical
// catalog id. It is exported so delete removes exactly the parent that open uses.
func StagedDocumentDirectory(documentID string) (string, error) {
	documentID = strings.TrimSpace(documentID)
	if documentID == "" {
		return "", fmt.Errorf("staged document directory: document id is empty")
	}
	sum := sha256.Sum256([]byte(documentID))
	return pathpkg.Join(openedDocumentsBoxDir, fmt.Sprintf("document-%x", sum[:12])), nil
}

// StagedDocumentPath validates a bare alias and places it below the logical
// document directory.
func StagedDocumentPath(documentID, fileName string) (string, error) {
	name := strings.TrimSpace(fileName)
	if err := validateOpenFileName(name); err != nil {
		return "", err
	}
	if name == "" {
		return "", fmt.Errorf("staged document path: file name is empty")
	}
	dir, err := StagedDocumentDirectory(documentID)
	if err != nil {
		return "", err
	}
	return pathpkg.Join(dir, name), nil
}

func validateOpenFileName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf(
			"document_open: file_name %q must be a bare file name, not a path; "+
				"the file is always written into %s", name, openedDocumentsBoxDir)
	}
	if name == "." || name == ".." || strings.HasPrefix(name, ".") {
		return fmt.Errorf("document_open: file_name %q is not a usable file name", name)
	}
	return nil
}
