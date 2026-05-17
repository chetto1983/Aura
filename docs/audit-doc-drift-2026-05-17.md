# Documentation Drift Audit — 2026-05-17

After today's 15+ cleanup commits (cmd/aura/app.go adapters extraction, internal/identity/store.go split, internal/agent/loop.go split), the following documentation claims no longer match the codebase.

---

## 1. CLAUDE.md Residual Stale Claims

### CLAUDE.md line 97 & 195: Prompt overlay file name mismatch
**Claim:** `AGENTS.md` is a prompt overlay file
**Reality:** The file is named `AGENT.md` (singular)
**Location:** d:\Aurauntime-workspace\AGENT.md exists; `AGENTS.md` does not
**Impact:** CLAUDE.md line 97 mentions `AGENTS.md`, line 195 mentions `AGENTS.md` — both should say `AGENT.md`

### CLAUDE.md line 105–122: Tool source paths mostly correct
**Verified:**
- ✓ `memory_search.go`, `tool_search.go` at `internal/agent/tools/registry/`
- ✓ `searxng.go`, `direct_fetch.go`, `web_common.go` at `internal/agent/tools/registry/`
- ✓ `source.go` at `internal/agent/tools/registry/`
- ✗ Files tools: CLAUDE.md says `internal/files/files.go` but actual files are `internal/agent/tools/registry/files_*.go` (files_xlsx.go, files_docx.go, files_pdf.go)

**Fix:** Line 118 should reference `internal/agent/tools/registry/files_*.go` not `internal/files/`

### CLAUDE.md lines 137–143: Old package paths
**Claim:** Source pipeline lives in `internal/source`, `internal/ocr`, `internal/ingest`
**Reality:** Refactored to `internal/storage/sources/{store, ocr, ingest, markitdown}`
**Verification:**
- `d:/Aura/internal/source` — does not exist ✗
- `d:/Aura/internal/ingest` — does not exist ✗
- `d:/Aura/internal/storage/sources/ingest/` — exists ✓
- `d:/Aura/internal/storage/sources/ocr/` — exists ✓
- `d:/Aura/internal/storage/sources/store/` — exists ✓

**Fix:** Line 137 should read: `internal/storage/sources/{ingest, ocr, store, markitdown}`

### CLAUDE.md line 145: Search architecture claim needs clarification
**Current:** "### Search (`internal/search`)"
**Reality:** Search functionality spans `internal/storage/{search, qdrant, memoryindex}`
**Fix:** Clarify that search is multi-package, not monolithic

---

## 2. Inline Cross-Reference Comments

**Grep results: All valid cross-references found**
- `internal/agent/executor.go:71` → references `internal/telegram/conversation_tool_exec.go` ✓ exists
- `internal/channels/telegram/invocation_builder.go:62` → historical reference to `Bot.buildTelegramInvocation` ✓ accurate
- `internal/agent/tools/registry/subagent.go:20,75` → mirrors `swarm.NodeSpec` and `chat.Result` ✓ both types exist

**No broken cross-ref comments detected.**

---

## 3. TODO/FIXME/HACK Comments

**All TODOs are in test code or example data:**
- `internal/conversation/system_prompt_test.go:104,114` — test fixture
- `internal/agentnote/lifecycle_test.go:23,50` — test fixture
- `internal/agent/tools/registry/tool_definitions.go:96` — tool argument example

**No actual code debt found.**

---

## 4. Prompt Overlay Files (SOUL.md / AGENT.md / TOOLS.md / USER.md)

### AGENT.md filename note
CLAUDE.md incorrectly references `AGENTS.md` (plural); actual file is `AGENT.md` (singular).

### Tool claims in TOOLS.md: All valid
Verified against actual registry:
- `file`, `wiki_page`, `search_memory`, `source`, `web`, `doc`, `task`, `execute_code`, `subagent_dispatch`, `propose_patch`, `tool_search`, `request_dashboard_token` — ✓ all exist
- Tool count and schema alignment matches production registry

**No tool name mismatches found.**

---

## 5. prd.md Drift

### Phases map (section 4) — CORRECT
All 10 planned phases exist in `.planning/deep-refactor/`:
- Phase01 through Phase10_Single_Source_Of_Truth_Config ✓

### Module map (section 4, lines 135–151) — CORRECT
All listed packages verified against `find d:/Aura/internal -maxdepth 1 -type d`

### Responsibility contracts (section 5) — ASPIRATIONAL (appropriate for PRD)
Describes intended post-refactor state, not current implementation. Correct for a north-star document.

**No prd.md drift detected.**

---

## 6. README.md Quick Start

### Commands verified (lines 54–76)
```powershell
git clone https://github.com/chetto1983/Aura
cd Aura
New-Item -ItemType Directory -Force data,runtime-workspace,garage | Out-Null
docker compose -f compose.yaml -f compose.image.yaml up -d
```

**All commands work:** compose files exist ✓, directory creation required ✓, image reference correct ✓

### Develop section commands (lines 78–98)
- `go test ./...` ✓
- `go build ./...` ✓
- `docker compose up -d --build` ✓
- `docker compose --profile test run --rm test` ✓

**No README drift detected.**

---

## 7. docs/ Directory Files

### Recent (last 2 days) — AUTHORITATIVE
- All 2026-05-17 audit files (dead-code, duplication, god-files, legacy, surfaces) — current cleanup phase output
- aura-cleanup-execution-map.md (May 15) — execution roadmap
- aura-conversation-inventory-2026-05-16.md — last inventory
- agent-parallel-loop-2026-reference-map.md — reference material

### Archive/Review files
- aura-master-plan-REVIEW.md, aura-restructure-prd-REVIEW-*.md — explicitly versioned, not live docs
- Root prd.md supersedes aura-restructure-prd.md

**No stale "current state" snapshots found. All docs are either active (< 2 days) or explicitly archived.**

---

## Summary Table

| File | Line(s) | Issue | Severity | Action |
|------|---------|-------|----------|--------|
| CLAUDE.md | 97, 195 | `AGENTS.md` should be `AGENT.md` | LOW | Replace 2 occurrences |
| CLAUDE.md | 118 | Files tools path incorrect | MEDIUM | Update to `internal/agent/tools/registry/files_*.go` |
| CLAUDE.md | 137 | Source pipeline paths stale | MEDIUM | Update to `internal/storage/sources/` |
| CLAUDE.md | 145 | Search package description ambiguous | LOW | Clarify multi-package structure |
| prd.md | all | No drift detected | — | None |
| README.md | all | No drift detected | — | None |
| Cross-refs | all | No broken comments | — | None |
| TODOs | all | All test fixtures | — | None |
| Prompt overlays | all | Consistent with registry | — | None |

---

## Recommended Execution Order

**Single commit cluster:**
1. Fix CLAUDE.md: `AGENTS.md` → `AGENT.md` (lines 97, 195)
2. Fix CLAUDE.md line 118: Files tool source paths
3. Fix CLAUDE.md line 137: Source pipeline package paths
4. Clarify CLAUDE.md line 145: Search multi-package architecture

All fixes are documentation-only; no code changes required.
