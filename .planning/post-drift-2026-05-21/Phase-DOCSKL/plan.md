# Phase-DOCSKL — Restore Document Generation + Skill Tool

**Status:** active, source-audited and benchmark-ready for US-DOCSKL-01 (self-audited 2026-05-24)
**Estimated effort:** ~1 session (2 atomic stories)
**LOC delta:** ~+250 net

---

## Why this phase

Live observation 2026-05-24 evening (`/api/conversations` DB query, user-authorized):

### Gap 1 — Document generation regressed to runtime `pip install`

Conversation 19:48 (chat 1148481707, turn 217):

- **User**: "Genera un file PDF di esempoi5"
- **Aura turn 1**: `execute_code({"code":"from fpdf import FPDF\n..."})` → tool result: `ModuleNotFoundError: No module named 'fpdf'`
- **Aura turn 2 (recovery)**: `execute_code({"code":"import subprocess; subprocess.check_call([sys.executable, '-m', 'pip', 'install', 'fpdf'])"})` → exit 0 after 3119ms
- **Aura turn 3**: re-tries PDF generation, succeeds

This is wrong on multiple axes:

1. Aura ran `pip install fpdf` against PyPI **inside the Aura container, at request time**, with **no caching** between requests (next user gets the same fail+install pattern).
2. The Aura repo contains `internal/files/{pdf,docx,xlsx}.go` with full builders + tests, AND `internal/agent/tools/registry/files_{pdf,docx,xlsx}.go` with `CreatePDFTool`/`CreateXLSXTool`/`CreateDOCXTool` structs implementing the Tool interface. **All dormant — none registered in `cmd/aura/app_wire.go` or in the AURA_TOOL_ALLOWLIST.** Phase-TOOL's kill-the-RAG wave removed the consolidated `create_document` from the manifest but left the Go code in tree.
3. The fallback (execute_code python) requires the LLM to know fpdf syntax + handle import errors + drive pip — fragile + slow + non-deterministic across requests.

Fix: re-expose the 3 dormant builders under one `create_document(format=pdf|xlsx|docx)` action-enum tool. ~30 LOC of wiring, the heavy library is already there.

### Gap 2 — `install_skill` tool missing, Aura admits the gap

Conversation 19:42-19:43 (same chat):

- **User**: "Puoi installare skills?"
- **Aura**: "Non posso 'installarle' autonomamente... Se scrivi una procedura in Markdown e me la passi, posso usare il tool `file` per salvarla nella mia cartella delle skill."
- **User**: "In teoria il sistema può farlo ma non hai il tool"
- **Aura**: "Hai ragione, non ho un comando specifico chiamato `install_skill`."
- **Phantom-guard fires**: "Your previous reply named a tool but did not invoke it."

State today:

- `GET /api/skills` returns installed skills (currently 1: `aura-runtime-safety`).
- `GET /api/skills/catalog` returns the skills.sh registry (verified live: 25 entries from `vercel-labs/skills`, `anthropics/skills`, `microsoft/azure-skills`, `remotion-dev/skills`).
- `POST /api/skills/install` installs from a registry source+skill_id, gated by `SKILLS_ADMIN` capability.

The HTTP surface is complete. **No LLM-callable tool wraps it.** Aura can read the catalog (via web tools), but can't act on it.

Fix: new `skill(action=list|catalog|install|info|remove)` action-enum tool wrapping the in-process Go handlers. `install` + `remove` return a structured denial schema when `SKILLS_ADMIN` is missing so the LLM communicates the gate honestly instead of pretending to succeed.

---

## Stories

### US-DOCSKL-01 — `create_document(format=pdf|xlsx|docx)` action-enum tool

- **Scope:** Wire the 3 dormant builders (`NewCreatePDFTool` / `NewCreateXLSXTool` / `NewCreateDOCXTool`) into one consolidated LLM-callable tool. The library + builders + tests are already in master.
- **Files:** NEW `internal/agent/tools/registry/create_document.go` (~120 LOC), NEW `_test.go` (~80 LOC), MODIFY `cmd/aura/app_wire.go` (~10 LOC register), MODIFY `compose.yaml` (~1 LOC allowlist).
- **LOC delta:** +210 / -0 (no deletions; the existing 3 files become library code only).
- **Acceptance gate:** live probe_chat with 3 prompts (one per format); `tools_used=['create_document']` on each; **NO execute_code, NO pip install in logs** during the test window; bytes-of-artifact verification per format (unzip xlsx sharedStrings, parse pdf text, unzip docx word/document.xml).

### US-DOCSKL-02 — `skill(action=list|catalog|install|info|remove)` action-enum tool

- **Scope:** Brand-new tool wrapping `internal/skills/{catalog,admin,loader}.go` in-process. `install`/`remove` admin-gated with structured denial schema `{error:'capability_denied', capability:'skills.install', hint:'…', schema:'denial'}`.
- **Files:** NEW `internal/agent/tools/registry/skill.go` (~180 LOC), NEW `_test.go` (~120 LOC), MODIFY `cmd/aura/app_wire.go` (~10 LOC register), MODIFY `compose.yaml` (~1 LOC allowlist).
- **LOC delta:** +310.
- **Acceptance gate:** live probe_chat 3 turns (list / catalog / install); admin denial visible in reply text (not silent); zero phantom-guard warnings in logs for the test window.

---

## Reference patterns

- **Action-enum convention:** Aura's own `task`, `source`, `wiki_page` (canonical examples) + hermes-agent `skill_manage(action=…)` (D:/tmp/hermes-agent/tools/skill_manager_tool.py).
- **Rejected:** split tools (`install_skill`, `list_skills`) — codex CLI pattern, valid for permission-gating per surface but bloats Aura's manifest. Aura's convention beats codex here.
- **Denial schema:** structured JSON the LLM parses cleanly, per `feedback_no_regex_for_nlp`.

---

## Sequencing

US-DOCSKL-01 → US-DOCSKL-02. Each is one atomic commit per CLAUDE.md, one dedicated live probe pass.

---

## Risks

- **R1 (DOCSKL-01)**: the 3 dormant builders may have a stale block-grammar contract drift from when they were consolidated. **Mitigation**: re-read `files_pdf.go` / `files_xlsx.go` / `files_docx.go` fully before wiring; the doc comment says "Same block grammar as create_docx so the LLM only has to learn one DSL" — verify that's still true. If divergence found, normalize in same commit.
- **R2 (DOCSKL-02)**: `SKILLS_ADMIN` capability semantics may be more complex than read at face value (e.g., per-user vs global). **Mitigation**: read `internal/api/skills_write.go` for the canonical check path; replicate exactly (don't re-implement).
- **R3 (DOCSKL-02)**: `skills.sh` registry pull is a network call. Catalog should be cached. **Mitigation**: rely on the existing `/api/skills/catalog` handler's caching (it serves quickly today — verified live); don't add a second cache layer.

---

## Verification

Per `feedback_aura_as_product` + `feedback_inspect_artifact_visually_not_just_pass_status` + `feedback_probe_must_verify_artifact_not_reply`:

- Each story's acceptance includes **artifact-level** ground-truth: unzipped xlsx XML, parsed PDF text, parsed DOCX XML, `GET /api/skills` JSON after install.
- Smoke "200 OK + nonzero body" checks are NOT sufficient. Bench numbers (artifact bytes + structural assertions) land in commit body.
- Live test runs against the rebuilt container, NOT in-process unit tests alone.

## Implementation Contract

Source of truth:

- `scripts/ralph/prd-phase-docskl-staged.json`
- this phase folder: `source.md`, `plan.md`, `benchmark.md`, `progress.md`
- `PRD.md` section 7.5 post-DRIFT catalog row for Phase-DOCSKL

Canonical store / transaction boundary:

- US-DOCSKL-01 canonical output store is the source store. Document generation
  writes source records and artifact bytes through existing builders; no DB
  transaction spans network calls or package installs.
- US-DOCSKL-02 canonical skill state is the configured local skills directory
  plus the existing `internal/skills.Loader` cache. Catalog reads are
  projections from `skills.sh`; install/remove preserve the existing admin gate.

Operational gates:

- Pick the lowest-priority `passes:false` story first.
- One story equals one atomic local commit.
- Run baseline tests before code edits when possible.
- Run dedicated slice QA before committing.
- Do not push unless the current user turn explicitly asks.

## PRD Coverage Matrix

| PRD / Phase Item | Plan Location | Benchmark Location | Source Evidence | Status |
| --- | --- | --- | --- | --- |
| Restore deterministic document generation without runtime `pip install`. | US-DOCSKL-01 | `benchmark.md` B-DOCSKL-01-A..E | `source.md` Aura Code Evidence + 2026 Practice Sweep | covered |
| Keep Aura's action-enum tool convention and avoid split manifest bloat. | US-DOCSKL-01, Reference patterns | `benchmark.md` B-DOCSKL-01-C | `source.md` Aura action-enum rows + Anthropic/OpenAI tool docs | covered |
| Verify generated document artifacts by bytes/structure, not assistant prose. | Verification | `benchmark.md` B-DOCSKL-01-D | `source.md` artifact examples + library docs | covered |
| Add LLM-callable skill lifecycle access while preserving admin gates. | US-DOCSKL-02 | `benchmark.md` B-DOCSKL-02-A..D | `source.md` `internal/skills/*`, API skill handlers, OpenAI skills docs | covered |
| Expose denied skill writes as structured capability denial. | US-DOCSKL-02 | `benchmark.md` B-DOCSKL-02-B..D | `source.md` API/admin evidence + local Hermes validation pattern | covered |
| Every story has atomic commit and dedicated QA. | Sequencing, Verification | `benchmark.md` dedicated slice QA rows | `AGENTS.md`, `CLAUDE.md`, `aura-implementation-loop` | covered |

## First Slice Readiness

Target: Phase-DOCSKL.
Mode: implementation after planning repair.
Slice: US-DOCSKL-01.
Source of truth: staged DOCSKL JSON plus this phase folder.
D:/tmp examples: Hermes skill tools/tests, assistant-ui artifact route, printing-press artifact packaging, Nanobot artifact helpers.
2026 best-practice sources: Anthropic tool-writing guidance, OpenAI function-calling guide, GoFPDF, Excelize, Gooxml, OpenAI skills docs/catalog.
Affected files: `internal/agent/tools/registry/create_document.go`, `internal/agent/tools/registry/create_document_test.go`, `cmd/aura/app_wire.go`, `compose.yaml`.
Baseline verification: B-DOCSKL-01-A.
Post-edit verification: B-DOCSKL-01-B..E plus global slice gates.
Benchmark ground truth: persisted source records, raw artifact bytes, parsed PDF/XLSX/DOCX content, and logs without `pip install`.
Dirty state: `scripts/ralph/prd-phase-docskl-staged.json` is untracked and belongs to this phase.
Non-goals: PPTX, skill authoring, execute_code changes, markitdown read-side changes.
Language: chat Italian; repository planning and prompts English.

---

## Out of scope (parking lot)

- PPTX format (different lib `unioffice` or `pptx-go`; separate story when needed).
- Markdown → PDF/DOCX via pandoc (heavier dep; revisit if user asks).
- Skill **authoring** (create new SKILL.md via tool); skills today come from registry pulls.
- Sandbox isolation (separate Phase-SBX).

---

*Created 2026-05-24 evening after live conversation audit. Trigger: user statement "manca una sandbox come si deve e l'installazione delle skills (un tool)" + clarification "non ho chiesto Aura ho chiesto cosa hai te (Claude Code)" → confirmed both gaps via DB query.*
