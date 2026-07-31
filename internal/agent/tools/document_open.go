package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/chetto1983/aura/internal/documents"
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
// of the whole set and sits in no chunk. The container already carries
// LibreOffice, openpyxl and pandas, so once the agent holds the file the
// question is arithmetic rather than recall.
type DocumentOpen struct {
	Documents     DocumentOpenBackend
	WorkspaceRoot string
}

// openedDocumentsDir is the fixed subdirectory materialized copies land in. The
// destination is never caller-chosen: a working copy of a user document is not
// something the agent should be able to scatter across the host.
const openedDocumentsDir = "documents"

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
		// Deferred, like document_index: a deliberate action the agent reaches for once
		// retrieval has told it which document matters. document_search's description
		// names it, so it is discoverable at the moment it becomes relevant.
		Deferred: true,
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

	body, meta, err := t.Documents.OpenDocument(ctx, ownerFromContext(ctx), args.DocumentID)
	if err != nil {
		return ToolResult{}, fmt.Errorf("document_open: %w", err)
	}
	defer func() { _ = body.Close() }()

	name := strings.TrimSpace(args.FileName)
	if name == "" {
		name = meta.FileName
	}
	path, written, err := t.write(body, name)
	if err != nil {
		return ToolResult{}, fmt.Errorf("document_open: %w", err)
	}

	out, err := json.Marshal(map[string]any{
		"path":        path,
		"file_name":   filepath.Base(path),
		"mime_type":   meta.MIMEType,
		"size_bytes":  written,
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

// write streams the body into <workspace>/documents/<name>. A failed copy takes
// its partial file with it: a truncated spreadsheet that looks like a whole one
// is worse than no file, because the agent would compute a confident wrong
// answer from it.
func (t *DocumentOpen) write(body io.Reader, name string) (string, int64, error) {
	dir, err := t.destinationDir()
	if err != nil {
		return "", 0, err
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", 0, fmt.Errorf("create %s: %w", dir, err)
	}
	path := filepath.Join(dir, name)
	file, err := os.Create(path) //nolint:gosec // path is a fixed workspace dir + a separator-free base name.
	if err != nil {
		return "", 0, fmt.Errorf("create %s: %w", path, err)
	}
	written, copyErr := io.Copy(file, body)
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(path)
		if copyErr != nil {
			return "", 0, fmt.Errorf("write %s: %w", path, copyErr)
		}
		return "", 0, fmt.Errorf("close %s: %w", path, closeErr)
	}
	return path, written, nil
}

// destinationDir resolves <workspace>/documents and refuses a root that escapes
// the workspace through a symlink — the same defense document_index applies in
// the other direction.
func (t *DocumentOpen) destinationDir() (string, error) {
	root := expandHomePath(strings.TrimSpace(t.WorkspaceRoot))
	if root == "" {
		return "", fmt.Errorf("no workspace root is configured")
	}
	dir := filepath.Join(root, openedDocumentsDir)
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		resolvedRoot := root
		if realRoot, err := filepath.EvalSymlinks(root); err == nil {
			resolvedRoot = realRoot
		}
		if !withinDir(resolvedRoot, real) {
			return "", fmt.Errorf("%s resolves outside the workspace", dir)
		}
		return real, nil
	}
	return dir, nil
}

// validateOpenFileName rejects anything that is not a bare file name. Silently
// basename-ing a caller's "../../etc/passwd" would accept a path traversal and
// write it somewhere else instead of saying no.
func validateOpenFileName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	if strings.ContainsAny(name, `/\`) || filepath.IsAbs(name) {
		return fmt.Errorf(
			"document_open: file_name %q must be a bare file name, not a path; "+
				"the file is always written into %s/ inside the workspace", name, openedDocumentsDir)
	}
	if name == "." || name == ".." || strings.HasPrefix(name, ".") {
		return fmt.Errorf("document_open: file_name %q is not a usable file name", name)
	}
	return nil
}
