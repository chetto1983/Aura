package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/chetto1983/aura/internal/documents"
)

// DocumentIndexBackend registers a local file in the calling identity's document
// catalog — the same path the CLI `aura docs ingest` runs. Ingestion does not read
// the file beyond hashing it: it writes the catalog row document_search ranks and
// document_open resolves (internal/documents.Service.IngestPath).
type DocumentIndexBackend interface {
	IngestPath(ctx context.Context, req documents.IngestRequest, path string) (*documents.Job, error)
}

// DocumentIndex is the explicit bridge from a workspace file to document_search.
// A produced/delivered file is NOT searchable on its own (D-03: no silent
// searchable memory); the agent calls this when the user will need to recall it.
type DocumentIndex struct {
	Indexer       DocumentIndexBackend
	WorkspaceRoot string
}

type documentIndexArgs struct {
	Path  string `json:"path"`
	Title string `json:"title"`
}

func (t *DocumentIndex) Spec() Spec {
	return Spec{
		Name:    "document_index",
		Summary: "Index a workspace file into your searchable document knowledge base so document_search can find it.",
		Description: "Make a file on the filesystem searchable via document_search. Files you produce (a report/" +
			"spreadsheet/doc under /workspace) or deliver with send_file are NOT searchable on their own — call " +
			"document_index when the user will need to find, recall, or ask about the file later. It registers the " +
			"file in YOUR identity's document catalog — the same catalog document_search ranks and document_open " +
			"opens. Give an absolute or workspace-relative path INSIDE /workspace. This is for your own local " +
			"files; the user's uploaded documents are already indexed automatically. " +
			"Example: {\"path\":\"artifacts/q3-report.docx\"}.",
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "Absolute or workspace-relative path of the file to index (must be inside the workspace)."},
    "title": {"type": "string", "description": "Optional display name for the indexed document."}
  },
  "required": ["path"]
}`),
		// Deferred: an occasional deliberate action. The <documents> prompt block names
		// it so the agent knows to tool_search + load it; document_search stays visible.
		Deferred: true,
	}
}

func (t *DocumentIndex) Execute(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	if t.Indexer == nil {
		return ToolResult{}, fmt.Errorf("document_index: indexer is not configured")
	}
	var args documentIndexArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return ToolResult{}, fmt.Errorf("document_index args: %w", err)
	}
	args.Path = strings.TrimSpace(args.Path)
	if args.Path == "" {
		return ToolResult{}, fmt.Errorf("document_index: path is required")
	}
	resolved := resolveFSPath(t.WorkspaceRoot, args.Path)
	root := t.WorkspaceRoot
	// Fence to the workspace root: unlike the full-host fs_* tools (D-15c/#50),
	// ingest-into-searchable-memory is a write-class action, so it is confined to
	// files the agent owns under /workspace. Intentional, not an oversight.
	//
	// Resolve symlinks so a link created INSIDE /workspace that points outside
	// cannot smuggle a host file into searchable memory (defense-in-depth). Only
	// when the target exists (the real ingest case) — then resolve the root too,
	// so a symlinked root (macOS /var -> /private/var, temp dirs) does not cause a
	// false rejection from a resolved-vs-lexical mismatch. If the target does not
	// exist yet, keep both paths lexical: the lexical fence still catches lexical
	// escapes and IngestPath surfaces the not-found.
	if realTarget, err := filepath.EvalSymlinks(resolved); err == nil {
		resolved = realTarget
		if realRoot, err := filepath.EvalSymlinks(root); err == nil {
			root = realRoot
		}
	}
	if root != "" && !withinDir(root, resolved) {
		return ToolResult{}, fmt.Errorf("document_index: path %q is outside the workspace; only workspace files can be indexed", args.Path)
	}
	req := documents.IngestRequest{
		SourceID:   strings.TrimSpace(args.Title),
		SourceKind: "workspace",
		IdentityID: ownerFromContext(ctx),
	}
	if req.SourceID == "" {
		req.SourceID = resolved
	}
	job, err := t.Indexer.IngestPath(ctx, req, resolved)
	if err != nil {
		return ToolResult{}, fmt.Errorf("document_index: %w", err)
	}
	// No "chunks" field: it reported job.SparseChunks, which ingest hard-wires to 0 now
	// that a document is one catalog row and not a pile of fragments. A field that is
	// always zero teaches the model the indexing failed.
	out, err := json.Marshal(map[string]any{
		"document_id": job.DocumentID,
		"file_name":   job.FileName,
		"status":      job.Status,
	})
	if err != nil {
		return ToolResult{}, fmt.Errorf("document_index: marshal result: %w", err)
	}
	result, err := NewResult(ctx, string(out))
	if err != nil {
		return ToolResult{}, err
	}
	result.Provenance = &ToolResultProvenance{Source: "document_index", Trust: TrustUntrusted}
	return result, nil
}
