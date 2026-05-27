# Phase-CLEAN — Codex execution plan (2026-05-27)

Codex-ready, GSD-free. Each story below is a self-contained Codex prompt:
file paths, change sketch, acceptance, verify command, deep-refactor
checklist. Drive via `codex exec --skip-git-repo-check
--sandbox workspace-write "<prompt>"` one commit at a time, OR Claude
inline in this session.

Anchors:
- Detection run baseline: this file (2026-05-27, master `272579a2`)
- Rules: `CLAUDE.md` §Behavioral Rules + §Deep Refactor on Touch
- Existing baselines: `docs/deadcode-baseline-2026-05-22.json`,
  `docs/dupl-summary-2026-05-22.md`, `.file-size-baseline.txt`,
  `.golangci.yml`
- CI gates: `.github/workflows/ci.yml`, `lefthook.yml`

---

## 0. Locked decisions

| # | Question | LOCKED |
|---|---|---|
| 1 | Errcheck handling style | `_ = closer()` for defer Close, `slog.Warn` when failure is observable, never silently drop business errors |
| 2 | Helper placement on cross-package dupl | Helper lives in the **callee package** that the depguard boundary already allows, or in a new `internal/<domain>util/` if neither caller depguard-imports the other |
| 3 | Intra-file dupl with overlap | Treat as refactor red flag — read the function before extracting; many overlaps are schema/tool-definition tables that can collapse to a loop |
| 4 | Test cleanup scope | **Wave 5 on mainline** (user 2026-05-27) — runs after Wave 6 hard-gate so test fixture extractions are CI-protected from regression. 12 commits. |
| 5 | WIKI 6-way cluster (US-CLEAN-10) | **Active** — Phase-WIKI-B Wave A + B + FIX all shipped 2026-05-21/22; verified `dupl -t 60` on 2026-05-27 still flags the 6-way cluster + `graph_index.go` intra-file cluster. Active `prd.json` is Phase-CONS (no wiki touch). Gate lifted. |
| 6 | CI ratcheting | Wave 0 adds **warning** lines, Wave 6 promotes to **fail**; never break CI mid-sweep |
| 7 | Per-slice verify gate | `go vet ./... && go build ./... && go test ./internal/<touched-pkg>/... && golangci-lint run <touched files> && dupl -t 60 <touched files>` |

Plus 3 hardcoded from CLAUDE.md:
- **C1** 1 story = 1 commit; no batching outside Wave 5 mechanical sweeps.
- **C2** Deep-refactor-on-touch: any file touched is fully cleaned in the same commit (dead code, dupl, file-size, stale comments).
- **C3** Never modify tests to make lint pass; if a test exposes a real errcheck, fix the production code path the test exercises.

---

## 1. Commit sequence (29 core commits + 16 optional)

```
W0  →  W1 (9)  →  W4 (1)  →  W6 (2)  →  W2 (12, incl. US-CLEAN-10)  →  W3 (7)  →  W5 (12)
```

Decided 2026-05-27 (see §11):
- **W6 promoted to immediately after W4** — CI hard-gate guards Wave 2/3 commits live.
- **W5 retained on mainline, after W6** — test cleanup runs once CI gate is in place (no longer optional).
- US-CLEAN-15 + US-CLEAN-18 → both **real folds**, not false positives. Sketches updated in §5.

Status snapshot (will be updated as Codex ships each commit):

| Story | Wave | Status | Hash | Notes |
|---|---|---|---|---|
| US-CLEAN-00 | 0 | ⏳ pending | — | Baselines + CI warnings |
| US-CLEAN-01..09 | 1 | ⏳ pending | — | errcheck production (9 commits) |
| US-CLEAN-29 | 4 | ⏳ pending | — | staticcheck De Morgan |
| US-CLEAN-50..51 | 6 | ⏳ pending | — | CI hard-gate (runs after W4) |
| US-CLEAN-10 | 2 | ⏳ pending | — | wiki 6-way; ungated 2026-05-27 (Phase-WIKI-B shipped) |
| US-CLEAN-11..21 | 2 | ⏳ pending | — | dupl cross-file production (11 commits, incl. US-CLEAN-15 + US-CLEAN-18 generic-helper extracts) |
| US-CLEAN-22..28 | 3 | ⏳ pending | — | dupl intra-file production (7 commits, incl. US-CLEAN-27a wiki/graph_index.go) |
| US-CLEAN-30..41 | 5 | ⏳ pending | — | test cleanup (12 commits, runs after W6 gate) |

**Freshness rule:** update this snapshot immediately after every atomic commit, and mark the currently edited slice as in-flight before continuing.

---

## 2. Wave 0 — Baselines & guardrails

### US-CLEAN-00 — Baseline snapshot + non-blocking CI warnings

**Goal**: capture the pre-sweep state in version control, then wire `golangci-lint` and `dupl` into CI as **warnings** so regressions during the sweep are visible without breaking the build.

**Files**
- New: `docs/cleanup-baseline-2026-05-27.json` (machine-readable baseline)
- Edit: `.github/workflows/ci.yml`

**Change sketch**
1. Generate baseline JSON with three sections:
   - `deadcode`: array of finding strings (currently empty)
   - `golangci_lint`: `{ "errcheck": 50, "staticcheck": 2, "total": 52 }`
   - `dupl`: `{ "production_cross_file": 15, "production_intra_file": 13, "test": 75, "total_clusters": 103 }`
2. In `.github/workflows/ci.yml` job `test`, after `Go vet`, insert two **warning** steps (continue-on-error: true):
   - `golangci-lint run ./...` → `tee /tmp/lint-current.txt`; compare line count vs baseline; print delta only.
   - `dupl -t 60 ./cmd ./internal` → `tee /tmp/dupl-current.txt`; extract `Found total N clone groups.` and compare vs baseline.
3. Do NOT touch lefthook (already runs both pre-commit).

**Acceptance**
- `docs/cleanup-baseline-2026-05-27.json` exists with the three sections above.
- CI run on the commit shows two new green steps with `delta: 0` printout.
- No existing CI step is reordered or weakened.

**Verify command**
```bash
go vet ./...
go build ./...
golangci-lint run .github/workflows/ci.yml || true  # YAML lint not required
git diff --stat HEAD~1
```

**Deep-refactor checklist for US-CLEAN-00**
- N/A — pure additive, no Go files touched.

---

## 3. Wave 1 — errcheck production (9 commits, ~5-15 LOC each)

**Pattern for all Wave-1 slices**: if the dropped error is a deferred `Close()` on resources that survive the function (file handles, DB rows, HTTP bodies), use `_ = x.Close()`. If the error indicates real failure (rollback, file delete, log writer), wrap in `slog.Warn("close failed", "err", err)` using the existing zap-sugar in the package.

### US-CLEAN-01 — `internal/install/download.go` close leaks

**Goal**: 3 deferred Close errors in the bootstrap downloader (`embeddinggemma`, `whisper.cpp`). Silent failures here can mask corrupted partial-download files left on disk.

**Files**
- `internal/install/download.go:43, :192, :268`

**Change sketch**
- Line 43, 268: `defer f.Close()` → `defer func() { _ = f.Close() }()`
- Line 192: `defer resp.Body.Close()` → `defer func() { _ = resp.Body.Close() }()`
- If the file is the "active artifact" being written (write side), prefer `defer func() { if cerr := f.Close(); cerr != nil && err == nil { err = cerr } }()` to surface a write-tail failure.

**Acceptance**
- `golangci-lint run internal/install/download.go` clean.
- `go test ./internal/install/...` green.

**Verify command**
```bash
go vet ./internal/install/...
go test ./internal/install/...
golangci-lint run internal/install/download.go
dupl -t 60 internal/install/download.go
```

**Deep-refactor checklist**
- Re-read `internal/install/embedding.go` + `whisper.go` while in the file; they share a download pattern (see US-CLEAN-12). Do NOT extract the helper here — that's Wave 2.

---

### US-CLEAN-02 — `internal/dbrecovery/recovery.go` rollback + stmt close

**Goal**: silent rollback failure can mask WAL/index corruption incidents (memory `feedback_sqlite_wal_windows_corruption`). High priority.

**Files**
- `internal/dbrecovery/recovery.go:228, :235`

**Change sketch**
- Line 228 `defer tx.Rollback()` → wrap with `_ =` only if we already committed; otherwise log via existing zap logger because Rollback failure during recovery is a real incident signal.
- Line 235 `defer stmt.Close()` → `_ = stmt.Close()`.

**Acceptance**
- `go test ./internal/dbrecovery/...` green (recovery test exists at `recovery_test.go`).
- `golangci-lint run internal/dbrecovery/recovery.go` clean.

**Verify command**
```bash
go test ./internal/dbrecovery/...
golangci-lint run internal/dbrecovery/recovery.go
```

**Deep-refactor checklist**
- Check if `internal/dbrecovery/recovery_test.go:41, :79` (also flagged by errcheck) can be silenced via the same `_ =` pattern in the same commit (test-side mechanical, allowed because already-touched module).

---

### US-CLEAN-03 — Query-loop close in 4 files

**Goal**: `rows.Close()` / `conn.Close()` left unchecked across SQLite query paths.

**Files**
- `internal/conversation/summarizer/proposals.go:209`
- `internal/secrets/store.go:100`
- `internal/storage/freshness/store.go:201`
- `internal/agentnote/store.go:63`

**Change sketch**
- All 4: `defer rows.Close()` / `defer conn.Close()` → `defer func() { _ = rows.Close() }()`.

**Acceptance**
- `go test ./internal/conversation/... ./internal/secrets/... ./internal/storage/freshness/... ./internal/agentnote/...` green.
- `golangci-lint run <touched files>` clean.

**Verify command**
```bash
go test ./internal/conversation/summarizer/... ./internal/secrets/... ./internal/storage/freshness/... ./internal/agentnote/...
golangci-lint run internal/conversation/summarizer/proposals.go internal/secrets/store.go internal/storage/freshness/store.go internal/agentnote/store.go
```

**Deep-refactor checklist**
- 4 files touched — re-read each header for stale comments referring to removed code; tidy in-commit.

---

### US-CLEAN-04 — HTTP/fsnotify body close (3 files)

**Goal**: HTTP response bodies and fsnotify watcher close.

**Files**
- `internal/llm/openai_stream.go:24`
- `internal/storage/sources/ocr/client.go:135`
- `internal/mcp/watcher.go:91`

**Change sketch**
- All: wrap defer Close in `func() { _ = x.Close() }()`.

**Acceptance**
- `go test ./internal/llm/... ./internal/storage/sources/... ./internal/mcp/...` green.
- `golangci-lint run <touched>` clean.

**Verify command**
```bash
go test ./internal/llm/... ./internal/storage/sources/ocr/... ./internal/mcp/...
golangci-lint run internal/llm/openai_stream.go internal/storage/sources/ocr/client.go internal/mcp/watcher.go
```

---

### US-CLEAN-05 — Logging lifecycle (4 leaks)

**Goal**: log writer Close/Remove. Silent failure here = silent log drops, which is the worst class of swallow.

**Files**
- `internal/logging/daily_writer.go:58, :97`
- `internal/logging/zap_slog.go:38, :57`

**Change sketch**
- `daily_writer.go:58` `dw.file.Close()` → use existing logger.Error before discard.
- `daily_writer.go:97` `os.Remove(...)` → wrap with logged Warn (log rotation failure should be visible).
- `zap_slog.go:38` `os.Stderr.WriteString(...)` → assign to `_, _`.
- `zap_slog.go:57` `dw.Close()` → `_ = dw.Close()`.

**Acceptance**
- `go test ./internal/logging/...` green.
- `golangci-lint run internal/logging/...` clean.

**Verify command**
```bash
go test ./internal/logging/...
golangci-lint run internal/logging/daily_writer.go internal/logging/zap_slog.go
```

**Deep-refactor checklist**
- `daily_writer.go` is the log rotator — read the file header; the silent failures here historically caused dropped logs (no incident on record but the structure invites it).

---

### US-CLEAN-06 — File/dir handle leaks (3 files)

**Files**
- `internal/files/xlsx.go:189`
- `internal/probe/docinspect/docinspect.go:41`
- `internal/workspace/root.go:412`

**Change sketch**
- `xlsx.go:189` `defer f.Close()` → `_ =` wrap.
- `docinspect.go:41` `defer rc.Close()` → `_ =` wrap.
- `root.go:412` `defer dir.Close()` → `_ =` wrap.

**Acceptance**
- `go test ./internal/files/... ./internal/probe/docinspect/... ./internal/workspace/...` green.
- `golangci-lint run <touched>` clean.

**Notes / risks**
- `internal/workspace/root.go` is 940 LOC (memory note from OH1-I hook bypass) — DO NOT split in this commit; the file is exempt or near-baseline-limit. File-size baseline must not be modified.

---

### US-CLEAN-07 — Sandbox temp-dir cleanup

**Files**
- `internal/sandbox/process_runner.go:159, :165`

**Change sketch**
- Both `defer os.RemoveAll(...)` → wrap with logged `slog.Warn` on error. Silent failure = leftover temp dirs = disk-fill creep over weeks.

**Acceptance**
- `go test ./internal/sandbox/...` green.

**Verify command**
```bash
go test ./internal/sandbox/...
golangci-lint run internal/sandbox/process_runner.go
```

---

### US-CLEAN-08 — Skills atomic-write tail

**Files**
- `internal/skills/admin.go:257`

**Change sketch**
- `defer os.Remove(tmpName)` → `_ = os.Remove(tmpName)`. Best-effort cleanup of the tmp file after atomic rename; logging failure adds noise without value here.

**Acceptance**
- `go test ./internal/skills/...` green.

---

### US-CLEAN-09 — CLI/probe close leaks

**Goal**: bundle 4 main.go files that each have a single Close errcheck. Justification for batching: each is a CLI/probe entrypoint with no test surface, so a single atomic commit is cleaner than 4.

**Files**
- `cmd/build_icon/main.go:118, :135`
- `cmd/debug_searxng/main.go:100`
- `cmd/probe_reasoning/main.go:126`
- `cmd/aura/web_chat_helpers.go` (verify exact line via `golangci-lint`)

**Change sketch**
- All: `_ =` wrap.

**Acceptance**
- `go build ./...` green (no tests for these CLIs).
- `golangci-lint run <touched>` clean.

**Verify command**
```bash
go build ./...
golangci-lint run cmd/build_icon/main.go cmd/debug_searxng/main.go cmd/probe_reasoning/main.go cmd/aura/web_chat_helpers.go
```

**Deep-refactor checklist**
- 4 main.go files touched — each gets a header re-read; remove any stale `// TODO` referring to already-done work.

---

## 4. Wave 4 — staticcheck (1 commit, run BEFORE Wave 2)

### US-CLEAN-29 — De Morgan rewrites

**Goal**: kill the 2 staticcheck QF1001 findings. Trivial mechanical fix.

**Files**
- `internal/agent/promptplan_test.go:14`
- `internal/storage/memoryindex/priority_section_test.go:62`

**Change sketch**
- `if !(overlayIdx >= 0 && pinnedIdx > overlayIdx && toolsIdx > pinnedIdx) { ... }` → `if overlayIdx < 0 || pinnedIdx <= overlayIdx || toolsIdx <= pinnedIdx { ... }`
- Same shape for `priority_section_test.go:62`.

**Acceptance**
- `go test ./internal/agent/... ./internal/storage/memoryindex/...` green (semantics preserved).
- `golangci-lint run <touched>` reports 0 staticcheck QF1001.

**Verify command**
```bash
go test ./internal/agent/... ./internal/storage/memoryindex/...
golangci-lint run internal/agent/promptplan_test.go internal/storage/memoryindex/priority_section_test.go
```

**Notes**
- Both are test files; the De Morgan transform must preserve the negative-case assertion. Read the surrounding block (3 lines before/after) to confirm we're not flipping intent.

---

## 5. Wave 2 — dupl cross-file production (12 commits)

**Pattern for all Wave-2 slices**: extract a single shared helper into the package that depguard already permits both callers to import. If neither caller can reach the other (e.g., one is in `internal/api/` and another in `internal/telegram/`), place the helper in the upstream domain package (`internal/files/`, `internal/storage/...`, etc.) or create a new `internal/<domain>util/` package.

### US-CLEAN-10 — Wiki 6-way cluster → graph helper

**Goal**: cluster 6-way in `internal/wiki/{diff,graph,questions,repairs,surprise}.go` → extract single helper. **Highest single-cluster ROI in Phase-CLEAN.**

**Status note (2026-05-27)**: previously deferred behind Phase-WIKI-B. Phase-WIKI-B Wave A (shipped 2026-05-21 11:43, `prd-completed-phase-wiki-b-wave-a.json`), Wave B (shipped 2026-05-21 13:44, `prd-completed-phase-wiki-b-wave-b.json`) and Phase-WIKI-FIX (shipped 2026-05-22 09:14, `prd-completed-phase-wiki-fix.json`) are all closed. Active queue (`scripts/ralph/prd.json`) is Phase-CONS — no wiki touch. `dupl -t 60` on master 2026-05-27 still flags the cluster verbatim. Gate lifted.

**Files involved**
- `internal/wiki/diff.go:218-226`
- `internal/wiki/graph.go:109-117` and `:118-126`
- `internal/wiki/questions.go:43-51`
- `internal/wiki/repairs.go:102-110`
- `internal/wiki/surprise.go:55-63`
- New: `internal/wiki/graph_helpers.go` (≤200 LOC)

**Change sketch**
- All 6 spots are ~8 LOC each — read all 6 in one pass; likely a `func collectNeighborsByRelation(...)` or `func renderRelatedSection(...)`-shape helper.
- Place helper in `internal/wiki/graph_helpers.go` (new file) — same package, no depguard concern.
- Replace all 6 call sites with a single helper invocation.

**Acceptance**
- `go test ./internal/wiki/...` green.
- `dupl -t 60 internal/wiki` reports 0 instance of the 6-way cluster.
- Net LOC delta: -~30 (6×8 LOC removed, helper +~20 LOC).

**Verify command**
```bash
go test ./internal/wiki/...
dupl -t 60 internal/wiki
golangci-lint run internal/wiki/diff.go internal/wiki/graph.go internal/wiki/questions.go internal/wiki/repairs.go internal/wiki/surprise.go internal/wiki/graph_helpers.go
```

**Deep-refactor checklist**
- Re-read frontmatter / package doc of `internal/wiki/` — purge stale comments referring to the now-deleted inline helpers.
- Confirm `wc -l` on each of the 6 touched files stays ≤600 LOC (none currently exempt per `.file-size-baseline.txt`).

---

### US-CLEAN-11 — debug_docx/debug_pdf writer helpers (2 clusters folded)

**Goal**: kill 2 cross-file dupl clusters between `cmd/debug_docx` and `cmd/debug_pdf`.

**Files**
- Affected:
  - `cmd/debug_docx/main.go:52-68` ↔ `cmd/debug_pdf/main.go:49-65`
  - `cmd/debug_docx/main.go:202-217` ↔ `cmd/debug_pdf/main.go:153-168`
- New: `cmd/internal/debugio/writer.go` (~80 LOC)

**Change sketch**
- Create `cmd/internal/debugio/` package with two exported helpers (the file-write preamble and the result-print routine).
- Replace both regions in both `main.go` files with a single call.

**Acceptance**
- `go build ./cmd/debug_docx/... ./cmd/debug_pdf/...` green.
- `dupl -t 60 cmd/debug_docx cmd/debug_pdf cmd/internal/debugio` reports 0 cross-cmd cluster between the two.

**Verify command**
```bash
go build ./cmd/...
dupl -t 60 cmd/debug_docx cmd/debug_pdf cmd/internal/debugio
golangci-lint run cmd/debug_docx/main.go cmd/debug_pdf/main.go cmd/internal/debugio/...
```

---

### US-CLEAN-12 — install model-fetch helper (embedding ↔ whisper)

**Goal**: collapse `internal/install/embedding.go:45-61` ↔ `internal/install/whisper.go:35-51` (~16-LOC duplicate) into a reusable downloader pattern. Memory `project_wave_2_10_install_bootstrap_shipped` references this as an intentional reusable substrate.

**Files**
- `internal/install/embedding.go:45-61`
- `internal/install/whisper.go:35-51`
- New or extended helper: `internal/install/fetch.go` (generic `FetchModelArchive(ctx, spec)` returning the path)

**Change sketch**
- Read both regions; they likely set up HTTP GET → write to disk → verify checksum.
- Extract `FetchModelArchive(ctx context.Context, spec ModelSpec) (string, error)`.
- Both callers reduced to a single line.

**Acceptance**
- `go test ./internal/install/...` green.
- `dupl -t 60 internal/install` reports 0 cluster between embedding.go and whisper.go.

**Verify command**
```bash
go test ./internal/install/...
dupl -t 60 internal/install
golangci-lint run internal/install/...
```

---

### US-CLEAN-13 — MCP setup builder helper

**Goal**: `internal/api/mcp_database_setup.go:31-62` ↔ `internal/api/mcp_setup.go:32-63` is a ~30-LOC near-clone. Extract a shared builder.

**Files**
- `internal/api/mcp_database_setup.go`
- `internal/api/mcp_setup.go`
- Helper either inline in `internal/api/mcp_setup_common.go` (new ≤150 LOC) or as a method on the existing setup struct.

**Acceptance**
- `go test ./internal/api/...` green (MCP setup tests are extensive — `mcp_setup_test.go` etc.).
- `dupl -t 60 internal/api/mcp_database_setup.go internal/api/mcp_setup.go internal/api/mcp_setup_common.go` clean.

**Verify command**
```bash
go test ./internal/api/...
dupl -t 60 internal/api
golangci-lint run internal/api/mcp_database_setup.go internal/api/mcp_setup.go
```

---

### US-CLEAN-14 — runs store row-builder helper

**Files**
- `internal/storage/runs/store_event.go:127-137`
- `internal/storage/runs/store_run.go:48-59`

**Change sketch**
- Both regions construct a struct from a `*sql.Rows` row scan. Extract `scanRunRow(rows *sql.Rows) (Run, error)` and equivalent for `RunEvent`.

**Acceptance**
- `go test ./internal/storage/runs/...` green.
- `dupl -t 60 internal/storage/runs` clean.

---

### US-CLEAN-15 — Store `GetByID` generic helper (summarizer ↔ cron)

**Goal**: `internal/conversation/summarizer/proposals.go:247-259` ↔ `internal/cron/issues.go:119-131` — 12-LOC duplicate. Different tables / different domain types / different not-found errors, but **identical Get-by-ID control flow** (QueryRow → scan-or-`sql.ErrNoRows`-to-domain-error → wrap on other errors).

**Resolution 2026-05-27**: confirmed real fold via generics (read 2026-05-27 §11 #3). Helper goes in a new `internal/storage/sqlitex/` substrate package (domain-neutral; both stores already independently use `database/sql`).

**Files**
- New: `internal/storage/sqlitex/getbyid.go` (~30 LOC)
- Edit: `internal/conversation/summarizer/proposals.go`
- Edit: `internal/cron/issues.go`

**Change sketch**
```go
// internal/storage/sqlitex/getbyid.go
package sqlitex

import (
    "context"
    "database/sql"
    "errors"
    "fmt"
)

// GetByID runs a single-row SELECT by ID, maps sql.ErrNoRows to notFoundErr,
// and wraps any other error with the wrap prefix. The scan callback turns
// the matched row into the domain type T.
func GetByID[T any](
    ctx context.Context,
    db *sql.DB,
    query string,
    id int64,
    scan func(*sql.Row) (T, error),
    notFoundErr error,
    wrap string,
) (T, error) {
    var zero T
    row := db.QueryRowContext(ctx, query, id)
    v, err := scan(row)
    if err != nil {
        if errors.Is(err, sql.ErrNoRows) {
            return zero, notFoundErr
        }
        return zero, fmt.Errorf("%s: %w", wrap, err)
    }
    return v, nil
}
```

Callers reduce to:
```go
// proposals.go
func (s *SummariesStore) Get(ctx context.Context, id int64) (ProposedUpdate, error) {
    return sqlitex.GetByID(ctx, s.db, getProposalSQL, id, scanProposal, ErrProposalNotFound, "summaries get")
}
// issues.go — symmetric
```

Push the SQL strings into package-level `const` (`getProposalSQL`, `getIssueSQL`) for testability. `scanProposal`/`scanIssue` already exist locally — keep them.

**Acceptance**
- `go test ./internal/conversation/summarizer/... ./internal/cron/... ./internal/storage/sqlitex/...` green.
- `dupl -t 60 internal/conversation/summarizer/proposals.go internal/cron/issues.go internal/storage/sqlitex/` reports 0 cluster between the two original spots.
- `golangci-lint run --enable-only=depguard` green — verify summarizer + cron can import `internal/storage/sqlitex/` (substrate, not a domain package).

**Verify command**
```bash
go test ./internal/conversation/summarizer/... ./internal/cron/... ./internal/storage/sqlitex/...
dupl -t 60 internal/conversation/summarizer internal/cron internal/storage/sqlitex
golangci-lint run --enable-only=depguard internal/conversation/summarizer/proposals.go internal/cron/issues.go internal/storage/sqlitex/getbyid.go
```

**Deep-refactor checklist**
- After the extract, scan both store files for similar `List`/`Resolve`-by-ID single-row patterns. If 2+ more match the same shape, extend `sqlitex/` in the SAME commit (per CLAUDE.md deep-refactor-on-touch) — but only if the additional call sites are in the touched files. Do NOT sweep other packages.
- Add a brief godoc on `sqlitex/GetByID` documenting the contract.

---

### US-CLEAN-16 — memoryindex/search row-builder helper

**Goal**: `internal/storage/memoryindex/store_helpers.go:215-227` ↔ `internal/storage/search/graph_documents.go:270-282`.

**Files**
- `internal/storage/memoryindex/store_helpers.go`
- `internal/storage/search/graph_documents.go`
- Helper placement: shared parent → likely a new `internal/storage/rowx/` package OR fold into the upstream type's package if there's a clean dependency direction.

**⚠ depguard check**
- Verify `.golangci.yml` `storage-boundary` and `rag-boundary` rules allow memoryindex to import from search (or vice versa). If not, the helper goes in a third location both can reach.

**Acceptance**
- `go test ./internal/storage/...` green.
- `golangci-lint run --enable-only=depguard` green.
- `dupl -t 60 internal/storage/memoryindex internal/storage/search` clean.

**Verify command**
```bash
go test ./internal/storage/...
golangci-lint run --enable-only=depguard ./...
dupl -t 60 internal/storage/memoryindex internal/storage/search
```

---

### US-CLEAN-17 — Search-fusion helper (qdrant ↔ sqlite within storage/search)

**Goal**: `internal/storage/search/qdrant_hybrid.go:116-133` ↔ `internal/storage/search/sqlite.go:196-213` is a 17-LOC duplicate inside the same package — easiest extract of Wave 2.

**Files**
- `internal/storage/search/qdrant_hybrid.go`
- `internal/storage/search/sqlite.go`
- New: `internal/storage/search/fusion_helpers.go` (≤200 LOC)

**Acceptance**
- `go test ./internal/storage/search/...` green.
- `dupl -t 60 internal/storage/search` no longer reports this cluster.

---

### US-CLEAN-18 — Generic `UniqueNonEmpty` helper → new `internal/sliceutil/`

**Goal**: `internal/agent/tools/registry/direct_fetch.go:460-474 uniqueStrings(values []string) []string` ↔ `internal/identity/store_helpers.go:270-284 uniqueNonEmptyCapabilities(capabilities []Capability) []Capability`. **Identical algorithm**, only the element type differs (`string` vs `Capability`, where `Capability` is a string-typed alias).

**Resolution 2026-05-27**: confirmed real fold via generics (read 2026-05-27 §11 #4). Helper lifts to a new substrate package `internal/sliceutil/` (domain-neutral, depguard-importable from everywhere — per memory `feedback_aura_is_platform_shaped`).

**Files**
- New: `internal/sliceutil/unique.go` (~15 LOC)
- Edit: `internal/agent/tools/registry/direct_fetch.go` (delete `uniqueStrings`, import + call `sliceutil.UniqueNonEmpty`)
- Edit: `internal/identity/store_helpers.go` (delete `uniqueNonEmptyCapabilities`, import + call `sliceutil.UniqueNonEmpty`)

**Change sketch**
```go
// internal/sliceutil/unique.go
package sliceutil

// UniqueNonEmpty returns values with empty entries dropped and duplicates removed,
// preserving the first-seen order. Works for any comparable type whose zero value
// represents "empty" (string, string-typed aliases, etc.).
func UniqueNonEmpty[T comparable](values []T) []T {
    var zero T
    seen := make(map[T]struct{}, len(values))
    out := make([]T, 0, len(values))
    for _, v := range values {
        if v == zero {
            continue
        }
        if _, ok := seen[v]; ok {
            continue
        }
        seen[v] = struct{}{}
        out = append(out, v)
    }
    return out
}
```

Callers reduce to one line each:
```go
// direct_fetch.go: was uniqueStrings(s); becomes:
sliceutil.UniqueNonEmpty(s)

// store_helpers.go: was uniqueNonEmptyCapabilities(caps); becomes:
sliceutil.UniqueNonEmpty(caps)
```

**Acceptance**
- `go test ./internal/agent/tools/registry/... ./internal/identity/... ./internal/sliceutil/...` green.
- `dupl -t 60 internal/agent/tools/registry/direct_fetch.go internal/identity/store_helpers.go` no longer reports this cluster.
- `golangci-lint run --enable-only=depguard ./...` green — `internal/sliceutil/` is substrate, no inbound boundary rules.

**Verify command**
```bash
go test ./internal/agent/tools/registry/... ./internal/identity/... ./internal/sliceutil/...
dupl -t 60 internal/agent/tools/registry internal/identity internal/sliceutil
golangci-lint run --enable-only=depguard ./...
golangci-lint run internal/agent/tools/registry/direct_fetch.go internal/identity/store_helpers.go internal/sliceutil/unique.go
```

**Deep-refactor checklist**
- After the extract, grep for the dropped function names (`uniqueStrings`, `uniqueNonEmptyCapabilities`) across the repo and re-route any other callers (likely 0; verify in-commit). Per CLAUDE.md REUSABLE CODE rule.
- Add minimal table test in `internal/sliceutil/unique_test.go` covering: empty slice, all-zero values, duplicates, mixed order preservation, mixed types via `string` AND a typed alias.
- Godoc the contract precisely: "zero value of T is treated as empty" — important for non-string comparable types (`int`, `bool`).

---

### US-CLEAN-19 — Ask-user lifecycle helper

**Goal**: `internal/channels/telegram/ask_user_resume.go:110-124` ↔ `internal/chat/hub_lifecycle.go:213-227`. ~14-LOC duplicate at the channel/chat boundary.

**Files**
- `internal/channels/telegram/ask_user_resume.go`
- `internal/chat/hub_lifecycle.go`
- Helper placement: `internal/chat/` (upstream of channels per depguard).

**⚠ depguard check**
- `.golangci.yml` `agent-core-boundary`: `internal/agent/**` must NOT import channels. Helper is in `internal/chat/` which can be imported by both `internal/chat/hub_lifecycle.go` and `internal/channels/telegram/ask_user_resume.go`. Verify.

**Acceptance**
- `go test ./internal/channels/telegram/... ./internal/chat/...` green.
- `golangci-lint run --enable-only=depguard` green.
- `dupl -t 60` on both files clean.

---

### US-CLEAN-20 — `internal/telegram/documents.go` split + dedup

**Goal**: 3 dupl signals point at `internal/telegram/documents.go`:
- Intra-file 215-245 ↔ 251-281 (30 LOC each)
- Intra-file 299-308 referenced from voice_handler.go (see US-CLEAN-27)
- API↔Telegram via `upload.go:292` ↔ `documents.go:457` (US-CLEAN-21)

This story addresses ONLY the intra-file 215-245/251-281 dedup. May reveal that documents.go is approaching the 600-LOC cap; if so, split is in-scope per deep-refactor-on-touch.

**Files**
- `internal/telegram/documents.go`

**Change sketch**
- Read both 30-LOC regions; likely two parallel branches for `document` vs `audio` paths. Extract `func handleAttachmentXxx(...)` helper inside the file.
- If `documents.go` exceeds 600 LOC after extraction, split into `documents.go` + `documents_audio.go` keeping public surface stable.

**Acceptance**
- `go test ./internal/telegram/...` green.
- `dupl -t 60 internal/telegram/documents.go` reports 0 intra-file cluster.
- `wc -l internal/telegram/documents.go` ≤ 600 (or baseline value if currently exempt — check `.file-size-baseline.txt`).

**Verify command**
```bash
go test ./internal/telegram/...
dupl -t 60 internal/telegram
bash scripts/check-file-size.sh .file-size-baseline.txt
golangci-lint run internal/telegram/documents.go
```

---

### US-CLEAN-21 — Upload pipeline helper (API ↔ Telegram)

**Goal**: `internal/api/upload.go:292-307` ↔ `internal/telegram/documents.go:457-473`. Both channels feed the same source-ingestion pipeline; the duplicate is post-ingest dispatch.

**Files**
- `internal/api/upload.go`
- `internal/telegram/documents.go`
- Helper placement: likely `internal/storage/sources/store/` or a new `internal/sources/ingestdispatch/` since both channels feed into source ingestion.

**Acceptance**
- `go test ./internal/api/... ./internal/telegram/... ./internal/storage/sources/...` green.
- `dupl -t 60` on both files clean.

---

## 6. Wave 3 — dupl intra-file production (7 commits)

### US-CLEAN-22 — `workspace_files.go` 2-cluster cleanup

**Goal**: `internal/agent/tools/registry/workspace_files.go:230-243 ↔ 245-258` + `:260-278 ↔ 280-298` — 2 intra-file 14- and 19-LOC clusters.

**Files**
- `internal/agent/tools/registry/workspace_files.go`

**Change sketch**
- Both pairs are likely action-arm parallel structures (one for `read`, one for `write` or similar). Extract a single helper with the differing arg passed in.

**Acceptance**
- `go test ./internal/agent/tools/registry/... -run File` green.
- `dupl -t 60 internal/agent/tools/registry/workspace_files.go` clean.

---

### US-CLEAN-23 — `tool_definitions.go` overlap

**Goal**: `internal/agent/tools/registry/tool_definitions.go:46-84 ↔ 66-104` — OVERLAPPING duplicate (lines 46-84 vs 66-104 share lines 66-84). Suggests a schema-builder table that repeats and can be folded to a loop.

**Files**
- `internal/agent/tools/registry/tool_definitions.go`

**Change sketch**
- Read 46-104 (~60 LOC). Likely two tool-definition entries that share a common parameter block. Replace with a constructor + 2 calls.

**Acceptance**
- `go test ./internal/agent/tools/registry/...` green.
- `dupl -t 60 internal/agent/tools/registry/tool_definitions.go` reports 0 cluster in that range.

**Notes**
- Tool definitions are LLM-visible manifest; semantic preservation is non-negotiable. Verify the resulting JSON Schema is byte-identical via a probe.

**Verify command**
```bash
go test ./internal/agent/tools/registry/...
go run ./cmd/probe_chat -manifest-only -compare /tmp/manifest-before.json /tmp/manifest-after.json
# or whatever the probe_chat manifest-dump flag is
```

---

### US-CLEAN-24 — `extractor.go` adjacent regions

**Goal**: `internal/storage/sources/ingest/extractor.go:434-448 ↔ 449-463` — adjacent 14-LOC duplicates. Likely 2 case arms of a switch or 2 iterations of a list.

**Files**
- `internal/storage/sources/ingest/extractor.go`

**Acceptance**
- `go test ./internal/storage/sources/ingest/...` green.

---

### US-CLEAN-25 — `setup_locale.go` halves

**Goal**: `internal/api/setup_locale.go:23-57 ↔ 59-93` — 34-LOC duplicate. Two halves of locale-init that should be a loop.

**Files**
- `internal/api/setup_locale.go`

**Change sketch**
- Likely two locale chains for IT and EN (or similar). Replace with a slice of locale specs + loop.

**Acceptance**
- `go test ./internal/api/...` green (setup tests in `setup_*_test.go`).
- `dupl -t 60 internal/api/setup_locale.go` clean.

---

### US-CLEAN-26 — `attempts/sqlite.go` query dedup

**Goal**: `internal/agent/tools/attempts/sqlite.go:127-140 ↔ 195-208` — 13-LOC duplicate. Two SQL query methods.

**Files**
- `internal/agent/tools/attempts/sqlite.go`

**Acceptance**
- `go test ./internal/agent/tools/attempts/...` green.

---

### US-CLEAN-27 — Single-file helpers batch (5 files, one commit each)

**Goal**: 5 files with single-cluster intra-file dupl. ONE COMMIT PER FILE per rule C1. Listed together for inventory; each ships as its own US-CLEAN-27a..e.

**Files & sub-stories**
- **US-CLEAN-27a** — `internal/wiki/graph_index.go:404-417 ↔ 420-433` (ungated 2026-05-27 — Phase-WIKI-B closed)
- **US-CLEAN-27b** — `internal/telegram/setup.go:63-75 ↔ 76-90`
- **US-CLEAN-27c** — `internal/agent/tools/registry/exec.go:49-60 ↔ 62-73`
- **US-CLEAN-27d** — `internal/config/runtime_settings.go:97-111 ↔ 167-181`
- **US-CLEAN-27e** — `internal/api/tool_compaction.go:167-180 ↔ 211-224`

**Acceptance per sub-story**
- `go test ./internal/<pkg>/...` green.
- `dupl -t 60 <touched file>` clean for that intra-file cluster.

---

### US-CLEAN-28 — `cmd/debug_telegram/main.go` intra

**Goal**: `cmd/debug_telegram/main.go:37-49 ↔ 60-72` — 12-LOC duplicate in a CLI.

**Files**
- `cmd/debug_telegram/main.go`

**Acceptance**
- `go build ./cmd/debug_telegram/...` green.
- `dupl -t 60 cmd/debug_telegram` clean.

---

## 7. Wave 5 — Test cleanup (12 commits, ~3-5h Codex-paced; runs after Wave 6)

**Sequencing note 2026-05-27**: Wave 5 was originally optional. User decision confirmed it stays on the mainline, runs **after Wave 6 hard-gate is in place** so the new CI guards catch any regression introduced by test-fixture extraction. Production tightness from Waves 0+1+4+6+2+3 must already be green when Wave 5 starts.

When ready:

### US-CLEAN-30 — 17 × test errcheck batched

**Goal**: mechanical `_ = ` wrap for 17 test-only errcheck findings. Single commit, single file diff per package allowed (mechanical sweep, batching allowed per rule C1 exception).

**Files** (all 17 from Wave-1 readout):
- `internal/conversation/summarizer/proposals_test.go:21`
- `internal/db/db_test.go:105, :111, :227`
- `internal/dbrecovery/recovery_test.go:41, :79` (already done in US-CLEAN-02)
- `internal/files/docx_test.go:25`
- `internal/llm/openai_test.go:26, :59, :81`
- `internal/mcp/watcher_test.go:93, :133, :166`
- `internal/probe/docinspect/docinspect_test.go:77`
- `internal/skills/admin_test.go:91`
- `internal/skills/catalog_test.go:25, :26`
- `internal/storage/qdrant/client_test.go:44, :131, :196, :202, :243, :250`
- `internal/testutil/dbhelpers.go:27`

**Acceptance**
- `go test ./... -count=1` green.
- `golangci-lint run ./...` reports 0 errcheck.

---

### US-CLEAN-31..38 — Test-fixture extraction per cluster family

Per-family stories (one commit each):
- **US-CLEAN-31** — `internal/llm/openai_test.go` 3-way cluster (lines 306, 351, 410 + others) → `internal/llm/openaifixture_test.go` helpers.
- **US-CLEAN-32** — `internal/telegram/documents_test.go` 5-way + 6-way clusters → `internal/telegram/documentsfixture_test.go`.
- **US-CLEAN-33** — store_test 4-way (`auth/store_test.go ↔ config/store_test.go ↔ cron/scheduler_test.go ↔ swarm/store_test.go`) → `internal/testutil/storetest/`.
- **US-CLEAN-34** — qdrant_test 3-way + compact_qdrant_test 3-way → `internal/storage/search/qdrantfixture_test.go`.
- **US-CLEAN-35** — `files_test.go` 3-way (registry) → fixture helper.
- **US-CLEAN-36** — `mcp/client_test.go` 4-way → fixture helper.
- **US-CLEAN-37** — `voice_handler_test.go` 4-way → fixture helper.
- **US-CLEAN-38** — remaining ~12 × 2-way test clusters batched per package (allowed exception).

Each story:
- Goal: kill the cluster(s) by lifting common scaffold into a test helper file.
- Acceptance: `go test ./internal/<pkg>/...` green + `dupl -t 60 internal/<pkg>` cluster count for that family drops to 0.

### US-CLEAN-39..41 — Reserved for fallout / overflow.

---

## 8. Wave 6 — Hard-gate (2 commits)

### US-CLEAN-50 — Promote CI lint + dupl to fail-on-delta

**Goal**: convert the Wave-0 warning steps into hard-fail steps once production is clean.

**Files**
- `.github/workflows/ci.yml`
- `docs/dupl-baseline.txt` (new, per-cluster format similar to `.file-size-baseline.txt`)

**Change sketch**
1. Replace the Wave-0 warning step with `golangci-lint run --new-from-rev=HEAD~1 ./...` (similar to existing depguard).
2. Add `dupl -t 60 ./cmd ./internal | diff - docs/dupl-baseline.txt` step — fails if new clusters appear vs baseline.
3. Regenerate `docs/dupl-baseline.txt` from current state (should be near-empty for production).

**Acceptance**
- CI run on the commit passes.
- A test commit that introduces a new errcheck fails CI.

---

### US-CLEAN-51 — Make `.golangci.yml` explicit

**Goal**: the current `.golangci.yml` only enables `depguard`; v2 defaults still include errcheck/staticcheck/unused/ineffassign/govet/gosimple. Make the project's intent explicit.

**Files**
- `.golangci.yml`

**Change sketch**
```yaml
version: "2"
linters:
  enable:
    - depguard
    - errcheck
    - staticcheck
    - unused
    - ineffassign
    - govet
  settings:
    depguard: { ... existing ... }
  exclusions:
    paths:
      - "web/node_modules"
      # remove _archive_phaseG_dead_dispatch if directory no longer exists
```

**Acceptance**
- `golangci-lint run ./...` reports 0 findings.
- `git grep _archive_phaseG_dead_dispatch` returns no Go file references.

---

## 9. Execution recipe (Codex)

**Per-story Codex invocation**:

```bash
codex exec --skip-git-repo-check --sandbox workspace-write '<<PROMPT
You are working on Aura, master branch. Repo at d:\Aura.

STORY: US-CLEAN-XX (see docs/phase-clean-plan-2026-05-27.md §N)

Read the story section completely. Apply EXACTLY one atomic commit:
1. Make the change.
2. Run the per-story verify command.
3. Run lefthook gate: golangci-lint run <touched> + dupl -t 60 <touched>.
4. Commit with message: "refactor(cleanup): US-CLEAN-XX — <one-line goal>"
   with body listing: files touched, LOC delta, helper extracted (if any),
   tests affected.

CONSTRAINTS:
- Do NOT batch with other stories.
- Touching internal/wiki/* is OK from 2026-05-27 onward (Phase-WIKI-B closed); previous reservation lifted.
- Do NOT modify .file-size-baseline.txt.
- Do NOT modify tests to silence linter; fix the production path.
- If verify fails 3 times, STOP and write a NOTE block in commit body
  describing what blocked; do not push.

DONE when the commit exists and verify passes.
PROMPT'
```

**Status update**: after each commit, edit the snapshot table in §1 of this file (status `✅ shipped` + hash + one-line note).

---

## 10. Estimated effort

Execution order (decided 2026-05-27): `W0 → W1 → W4 → W6 → W2 → W3 → W5`.

| Order | Wave | Commits | Codex-paced | Hand-paced |
|---|---|---|---|---|
| 1 | 0 | 1 | 20 min | 30 min |
| 2 | 1 | 9 | 60-90 min | 2-3 h |
| 3 | 4 | 1 | 5 min | 10 min |
| 4 | 6 (promoted) | 2 | 30 min | 1 h |
| 5 | 2 (incl. US-CLEAN-10, 15, 18) | 12 | 4.5-7 h | 9-14 h |
| 6 | 3 (incl. US-CLEAN-27a) | 7 | 2-3 h | 4-6 h |
| 7 | 5 (mainline, after W6 gate) | 12 | 3-5 h | 6-10 h |
| | **Total** | **44** | **~12-17 h** | **~23-35 h** |

Sub-gates:
- After step 3 (W4 done): expected 50→0 errcheck + 2→0 staticcheck on production.
- After step 4 (W6 done): CI hard-gates lint + dupl; further regressions fail PR.
- After step 6 (W3 done): all production dupl cluster eliminated.
- After step 7 (W5 done): test fixture cleanup done; zero-finding tools across the board.

---

## 11. Open decisions for the user

All 4 questions resolved 2026-05-27.

| # | Question | Decision |
|---|---|---|
| 1 | Wave 5 (test cleanup): now or defer? | **On mainline, after Wave 6** — runs once CI hard-gate is in place so fixture extractions are guarded against regression. |
| 2 | Wave 6 timing: after W4 or after W3? | **Immediately after W4** — CI gate guards remaining Wave 2/3 commits live. |
| 3 | US-CLEAN-15 (summarizer `Get` ↔ cron `Get`) | **Extract generic helper anyway** — new `internal/storage/sqlitex/GetByID[T]` substrate package; sketch in §5. |
| 4 | US-CLEAN-18 (`uniqueStrings` ↔ `uniqueNonEmptyCapabilities`) | **Extract to `internal/sliceutil/`** — `UniqueNonEmpty[T comparable]()`; sketch in §5. |
| (closed earlier) | ~~US-CLEAN-10 + 27a wiki gate~~ | Resolved 2026-05-27: Phase-WIKI-B closed, both stories active. |

---

## 12. Anti-goals

- ❌ Refactoring beyond what dupl/errcheck/staticcheck flag.
- ❌ Renaming exported symbols (out of scope).
- ❌ Behavior changes (this is hygiene, not feature work).
- ❌ Touching `.file-size-baseline.txt` to "make room" for new files.
- ❌ Adding `// nolint:` markers; fix the code, not the linter.
- ❌ Updating dependencies (`go.mod`) unless a refactor literally requires it.

---
