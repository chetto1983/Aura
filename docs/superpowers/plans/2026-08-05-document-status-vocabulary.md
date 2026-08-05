# Document Status Vocabulary Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the Go and TypeScript document status vocabularies match the CHECK constraints that actually close them, so the production recorder can insert a version at all.

**Architecture:** The database is the authority. `aura.documents_status_check` (migration 0093) and `aura.document_versions_status_check` (migration 0025) define two *different* closed sets; the Go enum mirrors only a stale version of the first and has no type at all for the second. This plan corrects both, gives version status a Go type so it can no longer drift silently, and adds a `db_integration` test that reads the constraints from a migrated database and fails the build when the two diverge again.

**Tech Stack:** Go 1.26, pgx/v5, sqlc, golang-migrate, PostgreSQL 17, Vite + React + TypeScript, vitest.

## Global Constraints

- Go toolchain is **WSL only**: `wsl -e bash -lc 'export PATH=$HOME/.local/go1.26.3/bin:$HOME/go/bin:$PATH; cd /mnt/d/Aura && <cmd>'`. Never run a `.exe` on the Windows host.
- Web tests run on **Windows** (`npm` in `D:\Aura\web`). WSL has no node.
- `db_integration` tests use **disposable databases only** — `pipelineDisposablePool(t)` provisions and drops one. Never point them at the live `aura` database.
- No file exceeds 600 LOC.
- Comments only where the *why* is non-obvious; identifier names carry the *what*.
- Commit directly on `master`. Do not push — pushing is gated on a separate quality-gate plan.
- `--no-verify` is forbidden. Lefthook lints a mixed tree (staged files staged, unstaged files at worktree state), so stage each task's full file set before committing.

## Decisions already locked (do not relitigate)

1. **The database wins.** Go and TS align to migration 0093's vocabulary, not the other way round.
2. **`archived` is retired for documents.** 0093 remaps it to `failed` with `error_code = 'legacy_archived'`. Delete it from the Go enum and the TS union. `document_versions` keeps its own `archived` — a different table, untouched.
3. **Version status becomes a Go type.** It is a bare `string` today, which is exactly why it drifted from migration 0025 unnoticed.

## Why `stored` and not `converting`

Migration 0093:279-288 remaps legacy in-flight rows `processing → converting`. That is a migration for rows *already mid-flight*; it is not the right starting value for a **fresh** row. At all four call sites the bytes are in Garage and hashed but conversion has not begun — `recordCatalogDocument`'s own comment says it "creates a non-visible candidate. It never activates it", and `RecordDocumentAsset` validates `hashes.SHA256` before calling. `stored` is the honest state. The version vocabulary agrees: `stored` sits immediately after `hash_calculated`.

## File Structure

| File | Responsibility | Change |
|---|---|---|
| `internal/documents/catalog_status.go` | Both closed status vocabularies + their enumeration slices | **Create** — split out of `catalog_types.go`, which stays the record structs |
| `internal/documents/catalog_types.go` | Document/version record structs and requests | Modify: remove the `DocumentStatus` block, type two fields |
| `internal/documents/status_vocabulary_integration_test.go` | Conformance guard against the live CHECK constraints | **Create** (`db_integration`) |
| `internal/documents/catalog_service.go` | Request normalization defaults | Modify 4 literals |
| `internal/documents/service.go` | CLI/runtime ingest catalog row | Modify 1 literal |
| `cmd/aura/document_version_recorder.go` | Production asset→version recorder | Modify 2 literals |
| `internal/documents/catalog_store.go` | sqlc row → domain decoder | Modify 1 conversion |
| `internal/documents/catalog_store_asset.go` | Reservation params | Modify 1 conversion |
| `web/src/documents/documentApi.ts` | Wire types | Modify the `DocumentStatus` union |
| `web/src/documents/documentViewModel.ts` | Tab predicate + status tone | Modify 2 functions |

---

### Task 1: Go vocabularies, typed version status, and the conformance guard

**Files:**
- Create: `internal/documents/catalog_status.go`
- Create: `internal/documents/status_vocabulary_integration_test.go`
- Modify: `internal/documents/catalog_types.go:26-46` (remove block), `:85`, `:174`
- Modify: `internal/documents/catalog_service.go:126`, `:166`, `:224`, `:239`
- Modify: `internal/documents/service.go:133`
- Modify: `cmd/aura/document_version_recorder.go:65-66`
- Modify: `internal/documents/catalog_store.go:366`
- Modify: `internal/documents/catalog_store_asset.go:63`
- Test: `internal/documents/catalog_service_test.go:70-71`, `:168-169`; `internal/documents/service_test.go:51-52`; `internal/documents/pipeline_worker_test.go:265`; `internal/agui/documents_api_test.go:25`

**Interfaces:**
- Produces: `DocumentStatus` (12 constants), `DocumentVersionStatus` (16 constants), `AllDocumentStatuses []DocumentStatus`, `AllDocumentVersionStatuses []DocumentVersionStatus`. `DocumentVersion.Status` and `RecordAssetVersionRequest.VersionStatus` change type from `string` to `DocumentVersionStatus`.
- Consumes: `pipelineDisposablePool(t *testing.T) *pgxpool.Pool` from `internal/documents/pipeline_store_integration_helpers_test.go:18`.

- [ ] **Step 1: Add the enumeration slice the test needs, over today's (wrong) constants**

Append to `internal/documents/catalog_types.go`, directly after the existing `DocumentStatus` const block ending at line 46:

```go
// AllDocumentStatuses enumerates every declared DocumentStatus. Go has no enum
// reflection, so the conformance test against the database CHECK constraint needs
// this list; a constant declared above and missing here is invisible to it.
var AllDocumentStatuses = []DocumentStatus{
	DocumentStatusDraft, DocumentStatusQueued, DocumentStatusProcessing,
	DocumentStatusReady, DocumentStatusFailed, DocumentStatusDeleting,
	DocumentStatusDeleted, DocumentStatusArchived,
}

// AllDocumentVersionStatuses enumerates every declared DocumentVersionStatus.
var AllDocumentVersionStatuses = []DocumentVersionStatus{}
```

And directly above it, the placeholder type the slice needs to compile:

```go
// DocumentVersionStatus is one immutable version's pipeline state.
type DocumentVersionStatus string
```

- [ ] **Step 2: Write the failing conformance test**

Create `internal/documents/status_vocabulary_integration_test.go`:

```go
//go:build db_integration

// The Go status vocabularies and the database CHECK constraints that close them.
//
// This test exists because the two drifted apart and nothing noticed. The production
// recorder wrote "processing" for a document version, which aura.document_versions_status_check
// has never admitted since migration 0025 — so RecordAssetVersion could not insert a row
// at all, and the live catalog reported three ready documents behind a single version.
// A constant that no longer matches its constraint is a 23514 in production and a green
// test suite everywhere else; this is the assertion that makes that divergence a red build.

package documents

import (
	"context"
	"maps"
	"regexp"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// checkLiteral matches the quoted values PostgreSQL renders back inside a CHECK
// definition: status = ANY (ARRAY['ready'::text, 'failed'::text, ...]).
var checkLiteral = regexp.MustCompile(`'([a-z_]+)'::text`)

func TestDocumentVocabulariesMatchTheDatabase(t *testing.T) {
	pool := pipelineDisposablePool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	documentValues := make([]string, 0, len(AllDocumentStatuses))
	for _, status := range AllDocumentStatuses {
		documentValues = append(documentValues, string(status))
	}
	versionValues := make([]string, 0, len(AllDocumentVersionStatuses))
	for _, status := range AllDocumentVersionStatuses {
		versionValues = append(versionValues, string(status))
	}

	for _, tc := range []struct {
		table      string
		constraint string
		declared   []string
	}{
		{"aura.documents", "documents_status_check", documentValues},
		{"aura.document_versions", "document_versions_status_check", versionValues},
	} {
		admitted := constraintValues(t, ctx, pool, tc.table, tc.constraint)
		declared := slices.Sorted(slices.Values(tc.declared))
		if !slices.Equal(declared, admitted) {
			t.Errorf("%s admits %v; Go declares %v", tc.constraint, admitted, declared)
		}
	}
}

// constraintValues returns the sorted literals one CHECK constraint admits, read from
// the migrated database rather than from the .sql files: the files are a history of
// ALTERs and only the server knows which one won.
func constraintValues(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	table, constraint string,
) []string {
	t.Helper()
	var def string
	if err := pool.QueryRow(ctx,
		`SELECT pg_get_constraintdef(oid) FROM pg_constraint
		 WHERE conrelid = $1::regclass AND conname = $2`,
		table, constraint).Scan(&def); err != nil {
		t.Fatalf("read %s on %s: %v", constraint, table, err)
	}
	seen := make(map[string]struct{})
	for _, match := range checkLiteral.FindAllStringSubmatch(def, -1) {
		seen[match[1]] = struct{}{}
	}
	if len(seen) == 0 {
		t.Fatalf("%s parsed to no values from %q", constraint, def)
	}
	return slices.Sorted(maps.Keys(seen))
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run:
```bash
wsl -e bash -lc 'export PATH=$HOME/.local/go1.26.3/bin:$HOME/go/bin:$PATH; cd /mnt/d/Aura && go test -tags db_integration -run TestDocumentVocabulariesMatchTheDatabase ./internal/documents/ -v'
```

Expected: FAIL, twice —
- `documents_status_check admits [accepted chunking converting dead_letter deleted deleting embedding failed projecting queued ready stored]; Go declares [archived deleted deleting draft failed processing queued ready]`
- `document_versions_status_check admits [archived chunked chunking deleted deleting embedded embedding failed hash_calculated indexed parsed parsing queued ready stored uploaded]; Go declares []`

If the run finishes in under a second, the tier skipped — export the composed DSNs and re-run. A skip is not a red.

- [ ] **Step 4: Create the corrected vocabulary file**

Create `internal/documents/catalog_status.go`:

```go
package documents

// DocumentStatus is the logical document lifecycle state.
//
// The set is closed by aura.documents_status_check (migration 0093). A value absent
// from that CHECK cannot be stored, so this enum and the constraint move together;
// TestDocumentVocabulariesMatchTheDatabase is what enforces it.
type DocumentStatus string

const (
	// DocumentStatusAccepted means the logical document exists before its bytes are stored.
	DocumentStatusAccepted DocumentStatus = "accepted"
	// DocumentStatusStored means the raw bytes are in object storage and hashed.
	DocumentStatusStored DocumentStatus = "stored"
	// DocumentStatusQueued means pipeline work has been queued.
	DocumentStatusQueued DocumentStatus = "queued"
	// DocumentStatusConverting means Docling is turning the raw bytes into a document.
	DocumentStatusConverting DocumentStatus = "converting"
	// DocumentStatusChunking means the converted document is being split into passages.
	DocumentStatusChunking DocumentStatus = "chunking"
	// DocumentStatusEmbedding means passages are being embedded.
	DocumentStatusEmbedding DocumentStatus = "embedding"
	// DocumentStatusProjecting means embeddings are being written to the search index.
	DocumentStatusProjecting DocumentStatus = "projecting"
	// DocumentStatusReady means the active document version is searchable.
	DocumentStatusReady DocumentStatus = "ready"
	// DocumentStatusFailed means the latest pipeline attempt failed and may be retried.
	DocumentStatusFailed DocumentStatus = "failed"
	// DocumentStatusDeadLetter means the pipeline exhausted its retries.
	DocumentStatusDeadLetter DocumentStatus = "dead_letter"
	// DocumentStatusDeleting means deletion has started but cleanup is still in progress.
	DocumentStatusDeleting DocumentStatus = "deleting"
	// DocumentStatusDeleted means the logical document has been soft-deleted.
	DocumentStatusDeleted DocumentStatus = "deleted"
)

// DocumentVersionStatus is one immutable version's pipeline state.
//
// The set is closed by aura.document_versions_status_check (migration 0025) and is a
// DIFFERENT vocabulary from DocumentStatus: a version names parse and embed stages the
// logical document does not, and it still admits "archived", which documents no longer
// do. The two must not be conflated. This was a bare string until 2026-08-05, which is
// how the recorder came to write "processing" — a value the constraint never admitted.
type DocumentVersionStatus string

const (
	DocumentVersionStatusUploaded       DocumentVersionStatus = "uploaded"
	DocumentVersionStatusHashCalculated DocumentVersionStatus = "hash_calculated"
	DocumentVersionStatusStored         DocumentVersionStatus = "stored"
	DocumentVersionStatusQueued         DocumentVersionStatus = "queued"
	DocumentVersionStatusParsing        DocumentVersionStatus = "parsing"
	DocumentVersionStatusParsed         DocumentVersionStatus = "parsed"
	DocumentVersionStatusChunking       DocumentVersionStatus = "chunking"
	DocumentVersionStatusChunked        DocumentVersionStatus = "chunked"
	DocumentVersionStatusEmbedding      DocumentVersionStatus = "embedding"
	DocumentVersionStatusEmbedded       DocumentVersionStatus = "embedded"
	DocumentVersionStatusIndexed        DocumentVersionStatus = "indexed"
	DocumentVersionStatusReady          DocumentVersionStatus = "ready"
	DocumentVersionStatusFailed         DocumentVersionStatus = "failed"
	DocumentVersionStatusDeleting       DocumentVersionStatus = "deleting"
	DocumentVersionStatusDeleted        DocumentVersionStatus = "deleted"
	DocumentVersionStatusArchived       DocumentVersionStatus = "archived"
)

// AllDocumentStatuses and AllDocumentVersionStatuses enumerate every declared value.
// Go has no enum reflection, so the conformance test against the database CHECK
// constraints needs these lists; a constant declared above and missing here is
// invisible to it.
var (
	AllDocumentStatuses = []DocumentStatus{
		DocumentStatusAccepted, DocumentStatusStored, DocumentStatusQueued,
		DocumentStatusConverting, DocumentStatusChunking, DocumentStatusEmbedding,
		DocumentStatusProjecting, DocumentStatusReady, DocumentStatusFailed,
		DocumentStatusDeadLetter, DocumentStatusDeleting, DocumentStatusDeleted,
	}

	AllDocumentVersionStatuses = []DocumentVersionStatus{
		DocumentVersionStatusUploaded, DocumentVersionStatusHashCalculated,
		DocumentVersionStatusStored, DocumentVersionStatusQueued,
		DocumentVersionStatusParsing, DocumentVersionStatusParsed,
		DocumentVersionStatusChunking, DocumentVersionStatusChunked,
		DocumentVersionStatusEmbedding, DocumentVersionStatusEmbedded,
		DocumentVersionStatusIndexed, DocumentVersionStatusReady,
		DocumentVersionStatusFailed, DocumentVersionStatusDeleting,
		DocumentVersionStatusDeleted, DocumentVersionStatusArchived,
	}
)
```

- [ ] **Step 5: Strip the old block out of `catalog_types.go` and type the two fields**

Delete lines 26-46 of `internal/documents/catalog_types.go` (the `DocumentStatus` type comment, the type, and its whole const block) plus the `AllDocumentStatuses` / `AllDocumentVersionStatuses` / `DocumentVersionStatus` additions from Step 1. They now live in `catalog_status.go`.

Then change two field types:

At `catalog_types.go:85`, inside `DocumentVersion`:
```go
	Status             DocumentVersionStatus `json:"status"`
```

At `catalog_types.go:174`, inside `RecordAssetVersionRequest`:
```go
	VersionStatus      DocumentVersionStatus
```

- [ ] **Step 6: Correct the six production call sites**

`internal/documents/catalog_service.go:126` and `:166` — both read `req.Status = DocumentStatusDraft`:
```go
		req.Status = DocumentStatusAccepted
```

`internal/documents/catalog_service.go:224`:
```go
		req.DocumentStatus = DocumentStatusStored
```

`internal/documents/catalog_service.go:239`:
```go
		req.VersionStatus = DocumentVersionStatusStored
```

`internal/documents/service.go:133` — change only the `Status:` field of that composite literal:
```go
		Scope: DocumentScopeLibrary, Title: req.FileName, Status: DocumentStatusStored,
```

`cmd/aura/document_version_recorder.go:65-66`:
```go
		DocumentStatus:   documents.DocumentStatusStored,
		VersionStatus:    documents.DocumentVersionStatusStored,
```

- [ ] **Step 7: Fix the two conversion sites the new types break**

`internal/documents/catalog_store.go:366`, inside `catalogVersionFromSQL` (sqlc row is `string`):
```go
		Status:             DocumentVersionStatus(row.Status),
```

`internal/documents/catalog_store_asset.go:63`, inside the `candidateVersionParams` call (`CandidateVersionRequest.Status` is `string`):
```go
			SizeBytes: req.SizeBytes, Status: string(req.VersionStatus),
```

- [ ] **Step 8: Update the five tests that pin the retired literals**

Each pins a value the schema forbids, so the test is the broken thing — record that in the commit message.

`internal/documents/catalog_service_test.go:70-71`:
```go
	if store.createReq.Status != DocumentStatusAccepted {
		t.Fatalf("create status = %q, want %q", store.createReq.Status, DocumentStatusAccepted)
	}
```

`internal/documents/catalog_service_test.go:168-169`:
```go
	if store.recordReq.DocumentStatus != DocumentStatusStored ||
		store.recordReq.VersionStatus != DocumentVersionStatusStored {
		t.Fatalf("record statuses = %q/%q, want stored/stored",
			store.recordReq.DocumentStatus, store.recordReq.VersionStatus)
	}
```

`internal/documents/service_test.go:51-52`:
```go
	if catalog.created.Status != DocumentStatusStored {
		t.Fatalf("catalog status = %q, want stored", catalog.created.Status)
	}
```

`internal/documents/pipeline_worker_test.go:265`:
```go
				SearchDocumentID: "doc_canary", Status: DocumentStatusStored,
```

`internal/agui/documents_api_test.go:25`:
```go
			Status:     documents.DocumentStatusAccepted,
```

- [ ] **Step 9: Run the conformance test to verify it passes**

Run:
```bash
wsl -e bash -lc 'export PATH=$HOME/.local/go1.26.3/bin:$HOME/go/bin:$PATH; cd /mnt/d/Aura && go test -tags db_integration -run TestDocumentVocabulariesMatchTheDatabase ./internal/documents/ -v'
```
Expected: PASS, both constraints.

- [ ] **Step 10: Run the full package suites, unit and integration, with the race detector**

Run:
```bash
wsl -e bash -lc 'export PATH=$HOME/.local/go1.26.3/bin:$HOME/go/bin:$PATH; cd /mnt/d/Aura && go vet ./... && go build ./... && go test -race ./internal/documents/ ./internal/agui/ ./cmd/aura/ && go test -tags db_integration -race ./internal/documents/'
```
Expected: PASS throughout. `go build` catches cross-vocabulary confusion (e.g. `VersionStatus: documents.DocumentStatusStored`) as a compile error, because the two are now distinct named types — that is what the type change buys. It does NOT catch an illegal string literal (`VersionStatus: "processing"`): Go assigns untyped string constants to any string-derived named type, so that still compiles clean. The recurrence guard against an illegal literal is `TestDocumentVocabulariesMatchTheDatabase`, not the compiler.

- [ ] **Step 11: Commit**

```bash
git add internal/documents/catalog_status.go internal/documents/catalog_types.go \
  internal/documents/status_vocabulary_integration_test.go \
  internal/documents/catalog_service.go internal/documents/catalog_service_test.go \
  internal/documents/service.go internal/documents/service_test.go \
  internal/documents/catalog_store.go internal/documents/catalog_store_asset.go \
  internal/documents/pipeline_worker_test.go internal/agui/documents_api_test.go \
  cmd/aura/document_version_recorder.go
git commit -m "$(cat <<'EOF'
Speak the status vocabulary the database admits

The recorder hard-coded "processing" for both the document and its version.
aura.document_versions_status_check has never admitted that value since
migration 0025, so RecordAssetVersion could not insert a version at all — the
live catalog shows three ready documents behind a single version row. The
document half was latent instead of live: 0093 replaces documents_status_check
with a richer set that drops draft, processing and archived, so the first
migrate would have broken catalog create and update too.

Align both vocabularies with the constraints and give version status a Go type,
which is what it lacked when it drifted. Fresh rows start at "stored", not at
0093's legacy converting remap: at every call site the bytes are hashed and in
Garage but conversion has not begun.

Five tests pinned the retired literals. They asserted values the schema
forbids, so the tests were the broken thing, not the code under them.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01KzRkrbwUqo2oWsryXKc34F
EOF
)"
```

---

### Task 2: Web vocabulary

**Files:**
- Modify: `web/src/documents/documentApi.ts:2-10`
- Modify: `web/src/documents/documentViewModel.ts:52`, `:59-64`
- Test: `web/src/documents/__tests__/documentViewModel.test.ts:57-72`

**Interfaces:**
- Consumes: the `DocumentStatus` union from Task 1's Go enum, over the wire as `document.status`.
- Produces: nothing later tasks depend on.

`DocumentTab` keeps its `'processing'` member. That is a **UI** name meaning "in flight", not a status value — it is a translated tab label, and renaming it would churn i18n keys in both `en` and `it` for no gain. Only the predicate behind it changes.

- [ ] **Step 1: Write the failing test**

Replace `web/src/documents/__tests__/documentViewModel.test.ts:67-72` with:

```ts
  it('maps status tones and tag drafts', () => {
    expect(statusToneFor('ready')).toBe('success');
    expect(statusToneFor('failed')).toBe('danger');
    expect(statusToneFor('dead_letter')).toBe('danger');
    expect(statusToneFor('converting')).toBe('warning');
    expect(statusToneFor('projecting')).toBe('warning');
    expect(statusToneFor('accepted')).toBe('secondary');
    expect(parseDocumentTags('robot, manual, robot')).toEqual(['robot', 'manual']);
  });

  it('treats every in-flight status as the processing tab', () => {
    for (const status of ['queued', 'converting', 'chunking', 'embedding', 'projecting'] as const) {
      expect(documentMatchesTab({ ...doc, status }, undefined, 'processing')).toBe(true);
    }
    expect(documentMatchesTab({ ...doc, status: 'ready' }, undefined, 'processing')).toBe(false);
  });
```

- [ ] **Step 2: Run it to verify it fails**

Run (Windows, from `D:\Aura\web`):
```bash
npm run test -- src/documents/__tests__/documentViewModel.test.ts
```
Expected: FAIL — `'dead_letter'`, `'converting'`, `'projecting'`, `'accepted'` are not assignable to `DocumentStatus`, and the in-flight cases return `false`.

- [ ] **Step 3: Correct the union**

`web/src/documents/documentApi.ts:2-10`:

```ts
export type DocumentStatus =
  | 'accepted'
  | 'stored'
  | 'queued'
  | 'converting'
  | 'chunking'
  | 'embedding'
  | 'projecting'
  | 'ready'
  | 'failed'
  | 'dead_letter'
  | 'deleting'
  | 'deleted';
```

- [ ] **Step 4: Correct the predicate and the tone map**

`web/src/documents/documentViewModel.ts:52`:

```ts
  if (tab === 'processing') return inFlightStatuses.has(document.status);
```

`web/src/documents/documentViewModel.ts:59-64`, replacing `statusToneFor` and adding the set above it:

```ts
// The pipeline stages a document passes through between acceptance and ready. The tab
// is named for the state the operator cares about, not for any single status value.
const inFlightStatuses = new Set<DocumentStatus>([
  'queued',
  'converting',
  'chunking',
  'embedding',
  'projecting',
]);

export function statusToneFor(status: DocumentStatus): StatusTone {
  if (status === 'ready') return 'success';
  if (status === 'failed' || status === 'dead_letter' || status === 'deleted') return 'danger';
  if (inFlightStatuses.has(status) || status === 'deleting') return 'warning';
  return 'secondary';
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run:
```bash
npm run test -- src/documents/__tests__/documentViewModel.test.ts
```
Expected: PASS.

- [ ] **Step 6: Run the full web gate**

Run, from `D:\Aura\web`:
```bash
npm run lint && npx tsc --noEmit && npm run test
```
Expected: PASS. `tsc` is what catches any other consumer of the union — a `status === 'processing'` comparison elsewhere becomes an error against the new type.

- [ ] **Step 7: Commit**

```bash
git add web/src/documents/documentApi.ts web/src/documents/documentViewModel.ts \
  web/src/documents/__tests__/documentViewModel.test.ts
git commit -m "$(cat <<'EOF'
Show the document statuses the pipeline can actually reach

The wire type mirrored the same stale vocabulary the Go enum did: it offered
draft, processing and archived, which the database can no longer produce, and
omitted accepted, stored, converting, chunking, projecting and dead_letter,
which it can. A document mid-conversion therefore fell out of the processing
tab and rendered with the neutral tone.

The tab keeps the name "processing" — it labels a state the operator cares
about, not a status value — so the i18n keys are untouched.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01KzRkrbwUqo2oWsryXKc34F
EOF
)"
```

---

## Out of scope for this plan

Each gets its own plan, in this order:

1. **Repeat-source `23505`** — replace `0093:306`'s `ADD CONSTRAINT documents_source_unique UNIQUE (identity_id, source_kind, source_key)` with a partial unique index `WHERE deleted_at IS NULL`, mirror the DROP in `.down.sql`, then let `CreateDocument` take an `ON CONFLICT ... DO UPDATE`. 0093 is unpushed *and* unapplied (live `aura` is at `schema_migrations = 92`), so this is an in-place edit, not a new migration slot.
2. **Production E2E** (`scripts/document_pipeline_e2e.sh`, PRD amendments #115/#116) — unblocked once this plan lands.
3. **Quality gates and push** — `make quality-full`, combined coverage ≥85%, mutation ≥70%, the five stale quality-snapshot rows, then the nine unpushed commits.

## Self-Review

**Spec coverage.** Decision 1 (DB wins) → Task 1 Step 4, Task 2 Step 3. Decision 2 (`archived` retired) → absent from both corrected vocabularies; `DocumentVersionStatusArchived` retained deliberately, since `document_versions_status_check` still admits it. Decision 3 (typed version status) → Task 1 Steps 4-7. The `stored`-not-`converting` call → Task 1 Step 6, all four sites. Recurrence guard → Task 1 Steps 2-3, 9.

**Placeholder scan.** Every code step carries the literal text to write; every run step carries the exact command and the expected result, including the two failure messages Step 3 must produce and the skip-tell that invalidates them.

**Type consistency.** `DocumentVersionStatus` is declared once (Step 4), consumed at `catalog_types.go:85` and `:174` (Step 5), at four call sites (Step 6), and at both conversion boundaries (Step 7) where the sqlc row and `CandidateVersionRequest` stay `string`. `AllDocumentStatuses` / `AllDocumentVersionStatuses` are introduced in Step 1, consumed by the test in Step 2, and redeclared in their final home in Step 4 with the Step 1 copies deleted in Step 5 — no duplicate declaration survives any single step boundary.
