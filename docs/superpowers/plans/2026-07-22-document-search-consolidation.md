# Document Search / Ingestion Consolidation — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop the agent confusing the user's uploaded/indexed documents (searchable Neo4j KB, via `document_search`) with its own `/workspace` files (filesystem), by adding a durable `<documents>` prompt doctrine, aligning tool descriptions, and adding an explicit `document_index` tool that indexes a workspace file into the calling identity's KB on demand.

**Architecture:** A static `<documents>` block in the system prompt teaches the two worlds + the bridge. `document_index` is a thin, deferred agent tool wrapping the existing `documents.Service.IngestPath` (the CLI `aura docs ingest` pipeline), scoped to the calling identity and path-confined to the workspace root. No store unification, no auto-indexing (D-03 "no silent searchable memory" preserved).

**Tech Stack:** Go 1.26, `internal/agent/tools` (Spec/Execute pattern), `internal/documents` (IngestPath/IngestRequest/Job), `internal/agent/prompt.go`, `cmd/aura` wiring.

**Design source:** `docs/superpowers/specs/2026-07-22-document-search-consolidation-design.md`.

## Global Constraints

- **PRD-first:** the Amendment #89 commit (Task 0) lands in `prd.md` BEFORE any code (CLAUDE.md PRD-first). The amendment NUMBER is the next free integer verified at execution time (`grep -n "Amendment #" prd.md` — do NOT assume 89 if a higher one has landed).
- **File size:** no file > 600 LOC; split on touch.
- **Post-edit gate (every Go edit):** `go vet ./...`, `go build ./...`, `go test ./internal/agent/tools/ ./internal/agent/ ./cmd/aura/`, then `go test -race` (WSL, `CGO_ENABLED=1`) on the touched packages.
- **KV-cache invariant (amendment #16/#29):** `messages[0]` is byte-identical turn-to-turn. The `<documents>` block is STATIC content (no per-turn data). Keep the existing `<workspace>`/`<memory>` blocks byte-identical. No golden prefix-hash pin exists (verified in Amendment #88; re-confirm with `grep -rn PrefixHash internal/`).
- **Security (memory-write surface, `[[l4-recall-injection-security-followup]]` / D-03):** `document_index` writes into searchable memory — it must be (a) explicit (agent-invoked, never silent), (b) identity-scoped via `ownerFromContext(ctx)`, (c) path-confined to `AURA_WORKSPACE_DIR`. The fs_* tools are full-host (D-15c/#50); `document_index` deliberately fences to the workspace because ingest-into-memory is a write-class action — document this rationale in a code comment so it is not read as an oversight.
- **No-skip-as-green:** the real E2E (Task 4) runs live on the stack, not a compile-check.
- **Commit discipline:** atomic; imperative subject + why body; `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`. Do NOT push (auto-sync + operator confirm).
- **Reuse, don't reinvent:** path fencing reuses `resolveFSPath`/`withinDir` from `internal/agent/tools/fs.go`. The tool mirrors `internal/agent/tools/document_search.go` (Spec/Execute/backend-interface/Provenance shape).

---

### Task 0: Ratify PRD Amendment #89

**Files:**
- Modify: `prd.md`

**Interfaces:**
- Produces: the ratified amendment that unblocks Tasks 1–4.

- [ ] **Step 1: Find the next free amendment number** — `grep -nE "Amendment #[0-9]+" prd.md | tail -5`. Confirm the highest is #88; the new one is that +1 (call it #N below; expected #89).
- [ ] **Step 2: Append the amendment** after the last existing amendment block, in the same `> **Amendment #N (date): …**` format, using the §3 text from the design doc (`docs/superpowers/specs/2026-07-22-document-search-consolidation-design.md`). Adjust the number to the verified #N.
- [ ] **Step 3: Verify** — `grep -c "document_index" prd.md` returns ≥1.
- [ ] **Step 4: Commit**

```bash
git add prd.md
git commit -m "docs(prd): ratify Amendment #N (document surface consolidation + document_index)"
```

---

### Task 1: `<documents>` prompt doctrine block

**Files:**
- Modify: `internal/agent/prompt.go` (add a `<documents>` block immediately after the `<workspace>` block)
- Test: `internal/agent/prompt_test.go` (add a needle test)

**Interfaces:**
- Produces: static `<documents>` doctrine in `SystemPrompt`; consumed by nothing in code (it is prompt text), asserted by the needle test.

- [ ] **Step 1: Write the failing needle test** — add to `internal/agent/prompt_test.go`:

```go
func TestPrompt_DocumentsDoctrine(t *testing.T) {
	for _, needle := range []string{
		"<documents>",
		"searchable knowledge base",
		"document_search FIRST",
		"document_index",
	} {
		if !strings.Contains(SystemPrompt, needle) {
			t.Errorf("system prompt missing documents doctrine %q", needle)
		}
	}
}
```

- [ ] **Step 2: Run it, verify FAIL** — `go test ./internal/agent/ -run TestPrompt_DocumentsDoctrine` → FAIL (needles absent).
- [ ] **Step 3: Add the `<documents>` block** in `internal/agent/prompt.go` directly after the `</workspace>` line (find it: `grep -n "</workspace>" internal/agent/prompt.go`). Insert verbatim:

```
<documents>
- Two separate document worlds — never confuse them:
  1. The user's UPLOADED/INDEXED documents live in a searchable knowledge base (Neo4j), NOT on the filesystem. For "this document", "the file I uploaded", the PDF/spreadsheet/manual, or what a document says/contains/lists → call document_search FIRST; do NOT fs_glob/fs_grep/shell for them.
  2. The files YOU create or work on live in /workspace (see <workspace>). Read and search them with fs_read/fs_grep, not document_search.
- A file you write under /workspace or deliver with send_file does NOT become searchable on its own. If the user will need to find or recall it later, index it explicitly with document_index — then document_search can find it.
</documents>
```

Keep the surrounding blocks byte-identical (do not reflow `<workspace>` or `<memory>`).

- [ ] **Step 4: Run it, verify PASS** — `go test ./internal/agent/ -run TestPrompt_DocumentsDoctrine` → PASS.
- [ ] **Step 5: Full gate** — `go vet ./... && go build ./... && go test ./internal/agent/`.
- [ ] **Step 6: Commit** — `feat(prompt): add <documents> doctrine block distinguishing KB vs workspace (Amendment #N)`.

---

### Task 2: `document_index` tool + wiring

**Files:**
- Create: `internal/agent/tools/document_index.go`
- Create: `internal/agent/tools/document_index_test.go`
- Modify: `cmd/aura/main.go` (register `DocumentIndex` next to `DocumentSearch`)
- Read first (do NOT edit yet): `internal/agent/tools/document_search.go` (mirror its shape), `internal/documents/types.go` (confirm `IngestRequest`/`Job` field names), `cmd/aura/docs.go` (how the CLI builds an INGEST-configured `documents.Service` via its factory), `cmd/aura/main.go` around the `DocumentSearch` registration.

**Interfaces:**
- Consumes: `documents.IngestRequest{SourceID, SourceKind, IdentityID string; …}`, `documents.Service.IngestPath(ctx, IngestRequest, path string) (*documents.Job, error)`, `documents.Job{DocumentID, FileName string; SparseChunks int; Status …}` (verify exact names/types against `internal/documents/types.go` — they are already used in `cmd/aura/docs.go`'s `writeJSON`), `resolveFSPath`/`withinDir` (fs.go), `ownerFromContext` (context.go), `NewResult`, `ToolResultProvenance`, `TrustUntrusted`, `Spec`.
- Produces: `tools.DocumentIndex{Indexer DocumentIndexBackend; WorkspaceRoot string}` and `tools.DocumentIndexBackend` interface with `IngestPath(ctx, documents.IngestRequest, string) (*documents.Job, error)`.

- [ ] **Step 1: Write the failing unit tests** — `internal/agent/tools/document_index_test.go`:

```go
package tools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/chetto1983/aura/internal/documents"
)

type fakeIndexer struct {
	calledPath string
	calledReq  documents.IngestRequest
	job        *documents.Job
	err        error
}

func (f *fakeIndexer) IngestPath(ctx context.Context, req documents.IngestRequest, path string) (*documents.Job, error) {
	f.calledPath = path
	f.calledReq = req
	if f.err != nil {
		return nil, f.err
	}
	return f.job, nil
}

func TestDocumentIndex_IndexesWorkspaceFile(t *testing.T) {
	root := t.TempDir()
	fi := &fakeIndexer{job: &documents.Job{DocumentID: "doc-1", FileName: "r.docx", Status: "searchable"}}
	tool := &DocumentIndex{Indexer: fi, WorkspaceRoot: root}
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"artifacts/r.docx"}`)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(root, "artifacts", "r.docx")
	if fi.calledPath != want {
		t.Fatalf("IngestPath path = %q, want %q", fi.calledPath, want)
	}
	if fi.calledReq.IdentityID == "" {
		t.Fatal("expected a non-empty owning identity (ownerFromContext) on the ingest request")
	}
}

func TestDocumentIndex_RejectsOutsideWorkspace(t *testing.T) {
	root := t.TempDir()
	fi := &fakeIndexer{job: &documents.Job{}}
	tool := &DocumentIndex{Indexer: fi, WorkspaceRoot: root}
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"/etc/passwd"}`)); err == nil {
		t.Fatal("expected rejection for a path outside the workspace")
	}
	if fi.calledPath != "" {
		t.Fatal("IngestPath must not run for an out-of-workspace path")
	}
}

func TestDocumentIndex_RequiresPath(t *testing.T) {
	tool := &DocumentIndex{Indexer: &fakeIndexer{}, WorkspaceRoot: t.TempDir()}
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"   "}`)); err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestDocumentIndex_NilBackendErrors(t *testing.T) {
	tool := &DocumentIndex{WorkspaceRoot: t.TempDir()}
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"a.txt"}`)); err == nil {
		t.Fatal("expected error when indexer is not configured")
	}
}
```

- [ ] **Step 2: Run, verify FAIL** — `go test ./internal/agent/tools/ -run TestDocumentIndex` → FAIL (`DocumentIndex` undefined).
- [ ] **Step 3: Implement the tool** — `internal/agent/tools/document_index.go`:

```go
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/documents"
)

// DocumentIndexBackend ingests a local file into the calling identity's
// searchable knowledge base — the same pipeline the CLI `aura docs ingest`
// runs (markitdown extract -> chunk -> Neo4j sparse index -> async embed).
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
			"document_index when the user will need to find, recall, or ask about the file later. It ingests the " +
			"file into YOUR identity's knowledge base (the same store document_search reads): extract -> chunk -> " +
			"index. Give an absolute or workspace-relative path INSIDE /workspace. This is for your own local " +
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
	// Fence to the workspace root: unlike the full-host fs_* tools (D-15c/#50),
	// ingest-into-searchable-memory is a write-class action, so it is confined to
	// files the agent owns under /workspace. Intentional, not an oversight.
	if t.WorkspaceRoot != "" && !withinDir(t.WorkspaceRoot, resolved) {
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
	out, err := json.Marshal(map[string]any{
		"document_id": job.DocumentID,
		"file_name":   job.FileName,
		"chunks":      job.SparseChunks,
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
```

If `documents.Job` field names differ from `DocumentID`/`FileName`/`SparseChunks`/`Status` (check `internal/documents/types.go`), use the real names — they are the same fields `cmd/aura/docs.go` marshals.

- [ ] **Step 4: Run unit tests, verify PASS** — `go test ./internal/agent/tools/ -run TestDocumentIndex` → PASS.
- [ ] **Step 5: Wire into the registry** — in `cmd/aura/main.go`, find the `reg.Register(&tools.DocumentSearch{…})` line. Build an INGEST-configured `documents.Service` the same way the CLI does (READ `cmd/aura/docs.go` — its `docsServiceFactory`/ingest builder — and reuse that construction; extract a shared helper if the CLI builder is not already reusable from the serve path). Register:

```go
reg.Register(&tools.DocumentIndex{
    Indexer:       ingestSvc, // *documents.Service (IngestPath), built like the CLI docs-ingest factory
    WorkspaceRoot: cfg.WorkspaceDir,
})
```

Guard it the same way `DocumentSearch` is guarded (if the docs service/pool is unavailable in a given boot path, register nothing rather than a nil-backed tool — mirror the existing pattern). Confirm `cfg.WorkspaceDir` is in scope at the registration site (it is used by the fs_* / shell_exec wiring in the same builder — Amendment #88).

- [ ] **Step 6: Gate** — `go vet ./... && go build ./... && go test ./internal/agent/tools/ ./cmd/aura/`, then `go test -race ./internal/agent/tools/` (WSL).
- [ ] **Step 7: Commit** — `feat(tools): add document_index — index a workspace file into the identity KB (Amendment #N)`.

---

### Task 3: Align document_search + send_file descriptions

**Files:**
- Modify: `internal/agent/tools/document_search.go` (append a clause to `Description`)
- Modify: `internal/agent/tools/send_file.go` (append a clause to `Description`)
- Test: `internal/agent/tools/document_index_test.go` or a new `descriptions_test.go` (needle assertions on the two specs)

**Interfaces:**
- Consumes: the `document_index` tool name (Task 2). No code coupling — description text only.

- [ ] **Step 1: Write the failing needle test** — add to `internal/agent/tools/document_index_test.go`:

```go
func TestDescriptions_CrossReferenceDocumentIndex(t *testing.T) {
	ds := (&DocumentSearch{}).Spec().Description
	if !strings.Contains(ds, "document_index") || !strings.Contains(ds, "/workspace") {
		t.Errorf("document_search description must point workspace-file questions at fs_* / document_index")
	}
	sf := (&SendFile{}).Spec().Description
	if !strings.Contains(sf, "document_index") {
		t.Errorf("send_file description must note delivered files are not searchable until document_index")
	}
}
```

(Add `"strings"` to the test imports. Verify the send_file tool type name with `grep -n "func.*Spec() Spec" internal/agent/tools/send_file.go` — use the real type instead of `SendFile` if it differs, and construct it with zero/nil fields sufficient for `Spec()`.)

- [ ] **Step 2: Run, verify FAIL** — `go test ./internal/agent/tools/ -run TestDescriptions_CrossReferenceDocumentIndex` → FAIL.
- [ ] **Step 3: Edit `document_search.go` Description** — append this sentence inside the existing description string (before the `Example:`):

```
Files YOU create live on the filesystem under /workspace — search those with fs_read/fs_grep, and make one searchable here by indexing it with document_index.
```

- [ ] **Step 4: Edit `send_file.go` Description** — append:

```
A delivered file is delivery-only and does NOT become searchable; if the user will need to find or recall it later, index it first with document_index.
```

- [ ] **Step 5: Run, verify PASS** — `go test ./internal/agent/tools/ -run TestDescriptions_CrossReferenceDocumentIndex` → PASS.
- [ ] **Step 6: Gate** — `go vet ./... && go build ./... && go test ./internal/agent/tools/`.
- [ ] **Step 7: Commit** — `docs(tools): cross-reference document_index in document_search/send_file descriptions (Amendment #N)`.

---

### Task 4: Full gate + real E2E

**Files:**
- Verify only (no new source unless a gap is found).

**Interfaces:**
- Consumes: Tasks 0–3.

- [ ] **Step 1: Full build + unit gate** — `go vet ./... && go build ./... && go test ./internal/agent/ ./internal/agent/tools/ ./cmd/aura/`, then `go test -race` (WSL) on those. All green.
- [ ] **Step 2: Rebuild + redeploy the aura image** — `docker compose build aura && docker compose up -d --force-recreate aura`; wait `docker inspect -f '{{.State.Health.Status}}' aura` = healthy. (`document_index` is baked into the aura binary + the `<documents>` prompt is in the image.)
- [ ] **Step 3: Real E2E on the live stack (score >9.8)** — drive an authenticated operator turn (Amendment #87/#88 login+run recipe; creds in `.env`):
  - (a) Ask Aura to *produce* a small document under `/workspace/artifacts` (e.g. a short Word report — the Task-2b toolchain is pre-baked, so no reinstall).
  - (b) In a later turn ask *"cosa dice quel documento?" / "trova quel report"*. Assert the agent recognizes the artifact is a local file: it does NOT `document_search` a purely-local artifact blindly, and if recall is requested it calls `document_index` then `document_search` and returns cited chunks scoped to the operator.
  - (c) Contrast: an actually-uploaded document is auto-searchable via `document_search` with no `document_index` call.
  - Confirm the agent never fs-hunts for an uploaded doc nor document_searches an un-indexed local artifact. Capture the transcript.
- [ ] **Step 4: Commit** (if any fix was needed) — otherwise note the E2E result in the phase notes. `feat(documents): finalize consolidation; document_index E2E (Amendment #N)`.
- [ ] **Step 5: Phase-close** — `go build ./...` + `go vet ./...` green; confirm with the operator before any `git push`.

---

## Self-Review

**Spec coverage:** §2.1 `<documents>` block → Task 1; §2.2 description alignment → Task 3; §2.3 `document_index` tool → Task 2; §2.4 security/cache/tests → Tasks 1 (needle/cache) + 2 (unit/fencing/scoping); §3 PRD → Task 0; §4 E2E → Task 4. All spec sections map to a task.

**Placeholder scan:** no "TBD/similar to Task N". Task 2 Step 5's wiring says READ the CLI ingest builder before editing — the exact construction is discovered from `cmd/aura/docs.go` at execution time (honest: the builder exists; only its reuse shape is decided in situ). Task 3's needle test names the real type after a `grep` check (send_file's Spec receiver type is verified, not assumed).

**Type consistency:** `DocumentIndexBackend.IngestPath(ctx, documents.IngestRequest, string) (*documents.Job, error)` (Task 2) matches `documents.Service.IngestPath` (the CLI signature). The tool struct `DocumentIndex{Indexer, WorkspaceRoot}` is consistent between the implementation, the tests, and the Task-2 wiring. `document_index` (the name) is consistent across the prompt block (Task 1), the tool Spec (Task 2), and the description clauses (Task 3). `documents.Job` field names are flagged for verification against `types.go` where used.
