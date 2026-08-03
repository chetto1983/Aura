package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	pathpkg "path"
	"strings"

	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// DocumentIndexBackend registers a local file in the calling identity's document
// catalog — the same path the CLI `aura docs ingest` runs. It READS the file it is
// handed, in the aura container: a content hash, plus the structural card
// document_search ranks on (internal/documents/filecard, which shells out to
// pdftotext for a PDF and unzips a workbook). That in-process read is the whole
// reason this tool copies bytes OUT of the box rather than passing a box path
// along, the way document_open passes one in.
type DocumentIndexBackend interface {
	IngestPath(ctx context.Context, req documents.IngestRequest, path string) (*documents.Job, error)
}

// DocumentIndex is the explicit bridge from a workspace file to document_search.
// A produced/delivered file is NOT searchable on its own (D-03: no silent
// searchable memory); the agent calls this when the user will need to recall it.
//
// The file it indexes lives inside the caller's per-identity BOX. The aura
// container and the box mount DIFFERENT volumes at /workspace, so resolving the
// agent's path host-side — as this did until the box became the only execution
// surface — aimed the ingest at a directory nothing the agent can run ever writes
// to, while its own description told the agent to call it on files it had just
// produced. That is the two-filesystems defect document_open was fixed for
// (Clienti.xlsx, 2026-08-03), and the fix here is its mirror image: document_open
// WRITES, so it hands a path into the box; ingestion READS, so this one stages the
// artifact out (send_file_sandbox.go's copy-out → fence → use pattern), ingests
// the staged copy, and deletes it.
type DocumentIndex struct {
	Indexer DocumentIndexBackend
	// Router is the per-identity box routing seam and the ONLY source of an indexable
	// file. A box that cannot be reached fails CLOSED (D-09/GATE-01) — there is no
	// host-filesystem arm left to read from.
	Router *usersandbox.SandboxRouter
}

// maxIndexedFileBytes caps what one call pulls out of the box, and is deliberately the
// SAME number stageBoxArtifact reads at most (maxSendFileBytes+1) rather than the ingest
// service's own ceiling. A file above the stage bound arrives TRUNCATED, so the gate has
// to sit exactly there: the service ceiling is operator-tunable and, raised, would
// catalog the short copy as a whole document — a spreadsheet cut off mid-file that then
// answers confidently and wrongly.
const maxIndexedFileBytes int64 = maxSendFileBytes

type documentIndexArgs struct {
	Path  string `json:"path"`
	Title string `json:"title"`
}

func (t *DocumentIndex) Spec() Spec {
	return Spec{
		Name:    "document_index",
		Summary: "Index a workspace file into your searchable document knowledge base so document_search can find it.",
		Description: "Make a file in your workspace searchable via document_search. Files you produce (a report/" +
			"spreadsheet/doc you wrote under /workspace) or deliver with send_file are NOT searchable on their own — " +
			"call document_index when the user will need to find, recall, or ask about the file later. Give an " +
			"absolute or workspace-relative path under /workspace: the SAME /workspace shell_exec, fs_write and " +
			"fs_read see, so a file you just created is indexable at exactly the path they reported. Aura copies it " +
			"out of your workspace container to read it, then registers it in YOUR identity's document catalog — the " +
			"same catalog document_search ranks and document_open opens. A path outside /workspace, or a file over " +
			"50 MB, is refused. This is for your own workspace files; the user's uploaded documents are already " +
			"indexed automatically. " +
			"Example: {\"path\":\"artifacts/q3-report.docx\"}.",
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "Path of the file to index, inside your workspace container and under /workspace (absolute, or relative to /workspace)."},
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
	boxPath, err := boxPathArg("document_index", args.Path)
	if err != nil {
		return ToolResult{}, err
	}
	// Fence to the box workspace root BEFORE anything is resolved or copied: unlike the
	// full-host fs_* tools of old (D-15c/#50), ingest-into-searchable-memory is a
	// write-class action, so it is confined to files the agent owns under /workspace.
	// A box path cannot be host-stat'd, so this is the literal-prefix half of the fence
	// (withinBoxWorkspace); the symlink-resolving half runs over the STAGED copy, which
	// is a real host file (vetStagedForIngest).
	if !withinBoxWorkspace(boxPath) {
		return ToolResult{}, fmt.Errorf(
			"document_index: path %q is outside %s; only files in your workspace can be indexed, so copy it under %s first",
			args.Path, boxWorkspaceRoot, boxWorkspaceRoot)
	}
	handle, routeErr := t.Router.Route(ctx)
	if routeErr != nil {
		return sandboxUnavailableResult("document_index", routeErr), nil
	}
	stageDir, staged, deny, err := t.stageForIngest(ctx, handle, boxPath)
	if deny != nil {
		return *deny, nil
	}
	if err != nil {
		return ToolResult{}, err
	}
	// The staged copy exists only for this call: IngestPath hashes and cards the file
	// synchronously, so nothing needs it once it returns. That is the difference from
	// send_file, whose descriptor hands the channel a PATH opened after the turn and so
	// cannot delete eagerly — here an eager delete is what keeps a copy of every indexed
	// file (up to 50 MiB each) out of the run dir.
	defer func() { _ = os.RemoveAll(stageDir) }()

	req := documents.IngestRequest{
		SourceID:   strings.TrimSpace(args.Title),
		SourceKind: "workspace",
		IdentityID: ownerFromContext(ctx),
		// The name and origin recorded are the BOX ones, never the staging path: that
		// path is deleted before this function returns, so filing the document under it
		// would name a location nobody can resolve. It is load-bearing beyond cosmetics —
		// the ingest's supported-type gate and the card's reader both key off FileName.
		FileName:     pathpkg.Base(boxPath),
		OriginalPath: boxPath,
	}
	if req.SourceID == "" {
		req.SourceID = boxPath
	}
	job, err := t.Indexer.IngestPath(ctx, req, staged)
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

// stageForIngest copies boxPath out of the caller's box into a fresh host staging dir the
// ingest pipeline can read, and vets the copy before returning it.
//
// Three outcomes, kept distinct like boxReadFileCapped's. A non-nil deny means the box could
// not be reached (or cannot copy artifacts out at all) and the caller fails CLOSED — never a
// host-filesystem read. A non-nil err is the model's own problem — a missing file, an
// over-cap one — and must read as one so it can self-correct. On success the caller owns
// stageDir and MUST remove it; every failure path here removes its own.
func (t *DocumentIndex) stageForIngest(
	ctx context.Context,
	h usersandbox.BoxHandle,
	boxPath string,
) (stageDir, staged string, deny *ToolResult, err error) {
	rc, copyErr := t.Router.CopyArtifactOut(ctx, h, boxPath)
	if copyErr != nil {
		d := sandboxUnavailableResult("document_index", copyErr)
		return "", "", &d, nil
	}
	defer func() { _ = rc.Close() }()

	stageDir, staged, err = stageBoxArtifact(ctx, rc, pathpkg.Base(boxPath))
	if err != nil {
		return "", "", nil, fmt.Errorf("document_index: cannot read %s from your workspace: %w", boxPath, err)
	}
	resolved, verr := vetStagedForIngest(stageDir, staged, boxPath)
	if verr != nil {
		_ = os.RemoveAll(stageDir)
		return "", "", nil, verr
	}
	return stageDir, resolved, nil, nil
}

// vetStagedForIngest applies the two host-side invariants to the staged copy and returns the
// symlink-resolved path the ingest reads: the copy is COMPLETE (the stage bound did not cut
// it short, so a truncated file can never be cataloged as a whole one), and it stays inside
// its staging dir.
func vetStagedForIngest(stageDir, staged, boxPath string) (string, error) {
	info, err := os.Stat(staged)
	if err != nil {
		return "", fmt.Errorf("document_index: cannot read the staged copy of %s: %w", boxPath, err)
	}
	if info.Size() > maxIndexedFileBytes {
		return "", fmt.Errorf(
			"document_index: %s is larger than the %d-byte indexing limit; index a smaller file or an extract of it",
			boxPath, maxIndexedFileBytes)
	}
	resolved, ok, err := fenceWithinRoot(stageDir, staged)
	if err != nil {
		return "", fmt.Errorf("document_index: cannot resolve the staged copy of %s: %w", boxPath, err)
	}
	if !ok {
		return "", fmt.Errorf("document_index: the staged copy of %s resolved outside its staging directory", boxPath)
	}
	return resolved, nil
}
