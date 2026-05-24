# Phase-DOCSKL Benchmark Contract

**Role:** benchmark
**Status:** benchmark-ready, self-audited 2026-05-24
**Rule:** Smoke checks are prechecks only. DOCSKL stories pass only when tests
and probes assert source-store bytes, API JSON, logs, or parsed artifacts.

## Global Slice Gates

Run these for every DOCSKL Go slice unless the story narrows them further:

| Gate | Command | Pass Threshold |
| --- | --- | --- |
| Patch hygiene | `git diff --check` | Pass |
| Narrow tests | story-specific `go test` command | Pass |
| Touched-path lint | `golangci-lint run <touched packages> --timeout=10m --new-from-rev=HEAD` | 0 new findings |
| Duplicate check | `dupl -t 60 <touched go files>` | 0 clone groups in touched production code |
| Shared build/vet | `go vet ./...` and `go build ./...` | Pass |
| Full Go suite | `go test ./... -count=1` | Pass before commit |
| Pre-commit hook | `lefthook run pre-commit` | Pass or record exact unavailable tool |
| Atomic commit | explicit staged files for one story only | One local commit, no push |

## US-DOCSKL-01 - `create_document`

### B-DOCSKL-01-A: Existing Builder Baseline

- **Command:** `go test ./internal/agent/tools/registry ./internal/files -run "TestCreate(PDF|XLSX|DOCX)Tool|TestBuild(PDF|XLSX|DOCX)" -count=1`
- **Fixture:** existing builder tests and file builder fixtures.
- **Artifact:** `go test` output.
- **Ground truth:** dormant PDF/XLSX/DOCX builders still persist valid source records and bytes before facade wiring.
- **Pass threshold:** all selected tests pass.
- **PRD gate:** re-expose existing deterministic builders instead of runtime Python fallback.
- **Result:** not run in planning repair.

### B-DOCSKL-01-B: Facade Unit Coverage

- **Command:** `go test ./internal/agent/tools/registry ./internal/files -run "TestCreateDocumentTool|TestCreate(PDF|XLSX|DOCX)Tool" -count=1 -race`
- **Fixture:** new `CreateDocumentTool` tests using isolated source-store fixtures and existing builder helpers.
- **Artifact:** returned tool JSON, source-store records, stored bytes.
- **Ground truth:** `format=pdf`, `format=xlsx`, and `format=docx` dispatch to the correct builder; unknown format is rejected; nil store returns nil; nil sender with `deliver=true` returns an explicit error.
- **Pass threshold:** exact branch assertions pass and the race run is green.
- **PRD gate:** one consolidated action-enum document tool with graceful failure modes.
- **Result:** not run.

### B-DOCSKL-01-C: App Wiring And Manifest Gate

- **Command:** `go test ./cmd/aura ./internal/agent/tools/registry -run "Test.*Tool|Test.*Allowlist|Test.*Manifest|Test.*CreateDocument" -count=1`
- **Fixture:** composition-root tests or source-level assertions over `cmd/aura/app_wire.go` and registry definitions.
- **Artifact:** registered tool catalog and allowlist state.
- **Ground truth:** `create_document` is registered once; `create_pdf`, `create_xlsx`, and `create_docx` are not exposed as separate production tools; `compose.yaml` default allowlist contains `create_document`.
- **Pass threshold:** exact tool-name assertions pass.
- **PRD gate:** manifest remains lean after tool consolidation.
- **Result:** not run.

### B-DOCSKL-01-D: Artifact Bytes Live Probe

- **Command:** container/live sequence:
  1. `docker compose build aura`
  2. `go run ./cmd/probe_chat --prompt "Genera un PDF di esempio con titolo Test e 3 bullet"`
  3. `go run ./cmd/probe_chat --prompt "Crea XLSX con 1 sheet e 3 righe Nome ed Eta"`
  4. `go run ./cmd/probe_chat --prompt "Crea DOCX con titolo Test e 2 paragrafi"`
  5. Fetch generated artifacts through the source API or source store.
- **Fixture:** rebuilt Aura container, configured local credentials/services, three user prompts.
- **Artifact:** `ChatReply.tools_used`, source IDs, raw source bytes, and Aura logs for the test window.
- **Ground truth:** each reply uses `tools_used=["create_document"]`; no `execute_code`; no `pip install`; PDF bytes are `>1024` and text extraction contains `Test`; XLSX unzip contains `xl/sharedStrings.xml` with `Nome` and `Eta`; DOCX unzip contains `word/document.xml` with `Test`.
- **Pass threshold:** all artifact assertions pass and `docker compose logs aura --since <window>` has zero `pip install` matches.
- **PRD gate:** production behavior no longer depends on ad hoc Python package installs.
- **Result:** not run.

### B-DOCSKL-01-E: Dedicated Slice QA

- **Command:** self-audited micro QA after patch: inspect `git diff -- internal/agent/tools/registry/create_document.go internal/agent/tools/registry/create_document_test.go cmd/aura/app_wire.go compose.yaml`; rerun B-DOCSKL-01-B; run negative unknown-format and nil-sender assertions.
- **Fixture:** completed DOCSKL-01 diff only.
- **Artifact:** diff inspection notes, command output, negative test output.
- **Ground truth:** facade does not bypass existing builder validation, no split tools are registered, error messages are explicit, and no runtime Python/pip path is introduced.
- **Pass threshold:** PASS verdict only if tests and negative checks pass.
- **PRD gate:** dedicated QA before the atomic local commit.
- **Result:** not run.

## US-DOCSKL-02 - `skill`

### B-DOCSKL-02-A: Existing Skill API Baseline

- **Command:** `go test ./internal/skills ./internal/api -run "Test(Catalog|Loader|SkillInstall|SkillDelete|Skill)" -count=1`
- **Fixture:** existing isolated skill loader/catalog/admin tests.
- **Artifact:** `go test` output and response structs.
- **Ground truth:** catalog parse/search, local skill loading, install validation, delete validation, and loader cache invalidation still pass before tool wiring.
- **Pass threshold:** all selected tests pass.
- **PRD gate:** preserve the existing skill lifecycle backend.
- **Result:** not run.

### B-DOCSKL-02-B: Skill Tool Unit Coverage

- **Command:** `go test ./internal/agent/tools/registry ./internal/skills -run "TestSkillTool|TestCatalog|TestLoader" -count=1 -race`
- **Fixture:** temp skill roots, fake catalog client/installer/deleter, admin and non-admin tool contexts.
- **Artifact:** tool JSON for `list`, `catalog`, `info`, `install`, `remove`, denied install/remove, nil deps, and invalid action.
- **Ground truth:** read actions return compact structured data; write actions with no admin capability return `{schema:"denial", error:"capability_denied"}`; admin write actions call existing install/remove dependencies and invalidate loader cache.
- **Pass threshold:** exact schema assertions pass and the race run is green.
- **PRD gate:** LLM can honestly surface skill capability state without phantom tool claims.
- **Result:** not run.

### B-DOCSKL-02-C: Skill Live Probe And Admin Denial

- **Command:** container/live sequence:
  1. `docker compose build aura`
  2. `go run ./cmd/probe_chat --prompt "Lista le skill installate"`
  3. `go run ./cmd/probe_chat --prompt "Mostrami il catalogo skills disponibili"`
  4. `go run ./cmd/probe_chat --prompt "Installa la skill frontend-design da anthropics/skills"`
  5. Compare with `GET /api/skills` and `GET /api/skills/catalog`.
- **Fixture:** rebuilt Aura container, configured local credentials/services, default no-admin user unless an admin token is explicitly configured.
- **Artifact:** `ChatReply.tools_used`, assistant-visible denial text, API JSON, and Aura logs.
- **Ground truth:** first two replies use `tools_used=["skill"]`; catalog mentions at least three entries matching API ground truth; install without admin produces visible denial, not success text; logs show zero phantom-guard warnings for the three turns.
- **Pass threshold:** exact tool usage, API comparison, and denial assertions pass.
- **PRD gate:** skill lifecycle is LLM-callable with honest capability gating.
- **Result:** not run.

### B-DOCSKL-02-D: Dedicated Slice QA

- **Command:** self-audited micro QA after patch: inspect `git diff -- internal/agent/tools/registry/skill.go internal/agent/tools/registry/skill_test.go cmd/aura/app_wire.go compose.yaml`; rerun B-DOCSKL-02-B; run denied install/remove negative tests.
- **Fixture:** completed DOCSKL-02 diff only.
- **Artifact:** diff inspection notes, command output, denial JSON.
- **Ground truth:** tool does not loop back through HTTP, does not bypass admin gates, does not expose raw full skill bodies by default, and does not mutate local skills on denied writes.
- **Pass threshold:** PASS verdict only if tests and negative checks pass.
- **PRD gate:** dedicated QA before the atomic local commit.
- **Result:** not run.

## Residue Gate

- **Command:** `git status --short --untracked-files=all`
- **Fixture:** repository after each story commit.
- **Artifact:** status output.
- **Ground truth:** only intentional files are staged/committed; no generated probe artifacts remain in the repo; `D:/tmp` probe scripts/screenshots are outside git.
- **Pass threshold:** clean or only explicitly named unrelated user files.
- **Result:** not run.
