# Phase-DOCSKL Source Map

**Role:** source
**Status:** source-audited, self-audited 2026-05-24
**Current slice:** US-DOCSKL-01 `create_document(format=pdf|xlsx|docx)`.

## Objective

Restore deterministic document generation and skill lifecycle tool access without
adding manifest bloat or runtime package installs. DOCSKL has two atomic
stories:

- US-DOCSKL-01 exposes the existing PDF/XLSX/DOCX Go builders through one
  `create_document` action-enum facade.
- US-DOCSKL-02 exposes existing skill read/catalog/install/delete capabilities
  through one `skill` action-enum tool with visible admin denial.

## Canonical Inputs

| Source | Evidence Used | DOCSKL Decision |
| --- | --- | --- |
| `scripts/ralph/prd-phase-docskl-staged.json` | Two `passes:false` stories, priority order DOCSKL-01 then DOCSKL-02, with live failure evidence from 2026-05-24. | Use this staged queue because the current user explicitly selected it. Execute one story at a time. |
| `.planning/post-drift-2026-05-21/Phase-DOCSKL/plan.md` | Live audit records PDF fallback through `execute_code` plus runtime `pip install fpdf`, and missing LLM-callable skill install. | Keep the live observations as phase motivation; repair missing source/benchmark/progress files before code. |
| `.planning/post-drift-2026-05-21/INDEX.md` | Phase-CONS is closed through US-CONS-13; no later phase should reopen CONS by inertia. | Add DOCSKL as the next active post-CONS phase. |
| `PRD.md` section 7.5 | Phase-TOOL previously intended `create_document(format=)` as a kitchen-sink collapse; current PRD did not yet include DOCSKL as a post-CONS corrective phase. | Add a DOCSKL row so future agents do not rely on the untracked queue alone. |
| `AGENTS.md` and `CLAUDE.md` | One module per slice, atomic commits, no smoke benchmark completion, artifact-level checks, EN-only prompts/tool descriptions. | DOCSKL stories require dedicated slice QA, artifact bytes, and one local commit per story. |

## Aura Code Evidence

| Path | Observed Shape | DOCSKL Decision |
| --- | --- | --- |
| `internal/agent/tools/registry/files_pdf.go` | `NewCreatePDFTool(store, sender)` already builds and persists PDFs through `internal/files.BuildPDF`; store is required, sender is optional. | Reuse this builder behind `create_document(format="pdf")`; do not reimplement PDF generation or invoke Python. |
| `internal/agent/tools/registry/files_xlsx.go` | `NewCreateXLSXTool(store, sender)` already builds XLSX files from structured sheet rows and persists them as `KindXLSX`. | Reuse this builder behind `create_document(format="xlsx")`; keep the existing sheet grammar. |
| `internal/agent/tools/registry/files_docx.go` | `NewCreateDOCXTool(store, sender)` already builds DOCX files from title/blocks and persists them as `KindDOCX`. | Reuse this builder behind `create_document(format="docx")`; keep the same block grammar as PDF. |
| `internal/agent/tools/registry/files_test.go` | Existing tests cover each dormant builder, delivery false, missing sender, empty specs, and persistence kinds. | Add facade tests without weakening the underlying builder tests. |
| `internal/files/pdf.go`, `internal/files/xlsx.go`, `internal/files/docx.go` | Pure-Go document builders already exist in repo and are tested. | The correct fix is wiring and facade validation, not new dependencies. |
| `cmd/aura/app_wire.go` | The composition root registers currently exposed tools but not the dormant document builders. | Register only `NewCreateDocumentTool`; keep `create_pdf`, `create_xlsx`, and `create_docx` unregistered. |
| `compose.yaml` | Default `AURA_TOOL_ALLOWLIST` excludes document and skill tools. | Add `create_document` in DOCSKL-01 and `skill` in DOCSKL-02. |
| `internal/agent/tools/registry/source_unified.go` and `scheduler.go` | Canonical Aura action-enum tools use one tool name with an `action` or mode discriminator, strict schemas, and dispatch helpers. | Follow this shape for `create_document` and `skill`; reject split-tool manifest growth. |
| `internal/skills/catalog.go` | Existing catalog client searches `skills.sh` and returns `CatalogItem` entries with install commands. | Reuse in-process catalog logic for `skill(action="catalog")`; do not add a second network/cache layer. |
| `internal/skills/admin.go` | Existing installer shells out to `npx skills add` under an admin-gated API path. | DOCSKL-02 must wrap this path and preserve capability semantics instead of inventing a new installer. |
| `internal/skills/loader.go` | Existing loader reads local `SKILL.md` files and caches `LoadAll`; it can invalidate cache. | Use for `skill(action="list"|"info")`; invalidate after install/remove success. |
| `internal/api/skills_write.go` and `internal/api/types_skills.go` | HTTP handlers already validate install/delete, return structured responses, and gate writes on `SKILLS_ADMIN`. | Mirror or delegate the same validation and denial semantics in the tool; never silently claim success. |

## D:/tmp Example Sweep

| Example Path | Adopt | Reject / Boundary |
| --- | --- | --- |
| `D:/tmp/hermes-agent/tools/skill_manager_tool.py` | Action-enum skill management, validation helpers, path traversal checks, and explicit operation dispatch. | Reject agent-authored skill editing for DOCSKL-02; Aura only needs registry install/remove/list/info now. |
| `D:/tmp/hermes-agent/tests/tools/test_skill_manager_tool.py` | Unit tests pin invalid names, traversal rejection, malformed frontmatter, and write/remove paths. | Do not copy the full authoring surface; use the validation mindset for admin actions only. |
| `D:/tmp/hermes-agent/tools/skills_tool.py` | Progressive disclosure: list metadata first, load full `SKILL.md` content only when asked. | Do not inject every skill body into normal tool results. `skill(info)` is on-demand. |
| `D:/tmp/hermes-agent/tests/tools/test_skills_tool.py` | Tests create temp skill roots and assert frontmatter/body parsing. | Aura already has `internal/skills/loader_test.go`; extend local Go tests instead of porting Python fixtures. |
| `D:/tmp/cli-printing-press/skills/printing-press-retro/references/artifact-packaging.md` | Artifact workflows should preserve bytes for inspection and avoid treating upload success as proof of content. | Do not add external upload/packaging. DOCSKL benchmark inspects source-store raw bytes. |
| `D:/tmp/assistant-ui/examples/with-artifacts/app/api/chat/route.ts` | Tool results can carry explicit artifact signals to the assistant UI. | DOCSKL is backend/source-store first; no frontend artifact runtime is part of this phase. |
| `D:/tmp/nanobot/nanobot/utils/artifacts.py` | Generated artifacts get validated, typed, and returned as compact structured handles. | Reject raw local paths in LLM replies; Aura should return source IDs/handles and inspect bytes through API/source store. |

Search queries used:

- `rg --files D:/tmp | rg -i "(skill.*manager|skill.*tool|document|docx|xlsx|pdf|artifact|tools/)"`
- `rg -n "skill_manage|skills_list|skill_view|artifact|SKILL.md" D:/tmp/hermes-agent D:/tmp/nanobot D:/tmp/assistant-ui D:/tmp/cli-printing-press`

## 2026 Practice Sweep

Checked 2026-05-24:

| Source | Relevant Practice | DOCSKL Use |
| --- | --- | --- |
| Anthropic Engineering, "Writing effective tools for AI agents", published 2025-09-11: `https://www.anthropic.com/engineering/writing-tools-for-agents` | Build fewer thoughtful tools, consolidate workflows where useful, return meaningful context, and evaluate with verifiable outcomes. | Supports one `create_document` tool and one `skill` tool instead of split endpoint wrappers. |
| OpenAI API function-calling guide, checked 2026-05-24: `https://developers.openai.com/api/docs/guides/function-calling` | Tool inputs are JSON-schema contracts; strict schemas use required fields plus `additionalProperties:false`. | DOCSKL tools should expose tight enums and object schemas, then validate again server-side. |
| GoFPDF package docs: `https://pkg.go.dev/github.com/go-pdf/fpdf` | GoFPDF is a Go PDF generator with high-level text/drawing/image support. | Existing `internal/files/pdf.go` remains the backend; do not use Python `fpdf` at request time. |
| Excelize docs: `https://xuri.me/excelize/en/` | Excelize is pure Go and handles OOXML spreadsheet files with read/write support. | Existing `internal/files/xlsx.go` remains the backend; artifact QA should unzip/read XLSX XML. |
| Gooxml document docs: `https://pkg.go.dev/baliance.com/gooxml/document` | The document package creates or opens OOXML `.docx` files. | Existing `internal/files/docx.go` remains the backend; artifact QA should unzip/read `word/document.xml`. |
| OpenAI Academy, "Using skills", published 2026-04-10: `https://openai.com/academy/skills/` | Skills are reusable workflows with `SKILL.md`, metadata, instructions, and resources. | DOCSKL-02 list/info must respect progressive disclosure and avoid flooding normal context. |
| OpenAI `openai/skills` catalog: `https://github.com/openai/skills` | Public Codex skill catalog examples exist and individual skills carry their own licenses. | `skill(catalog)` should expose enough metadata for user choice; installs remain admin-gated. |

## Adopted For US-DOCSKL-01

- Add `internal/agent/tools/registry/create_document.go` as a facade over the
  three existing builders.
- Tool name is exactly `create_document`; no `create_pdf`, `create_xlsx`, or
  `create_docx` registry exposure.
- `format` is an enum: `pdf`, `xlsx`, `docx`.
- `spec` is the format-specific payload object. The facade flattens it into the
  existing builder argument map to preserve old parser behavior.
- Store is required; nil store returns nil constructor. Sender is optional; a
  delivery request without sender must fail explicitly.
- Unit tests must cover all formats, unknown format, nil store, and nil sender.
- Live benchmark must assert artifact bytes, not just assistant prose.

## Adopted For US-DOCSKL-02

- Add one `skill` action-enum tool with actions `list`, `catalog`, `info`,
  `install`, and `remove`.
- Read actions use `internal/skills.Loader` and `CatalogClient`.
- Write actions preserve the existing admin gate and return a structured denial
  schema when admin capability is missing.
- Install/remove must invalidate loader cache on success.
- Do not roundtrip through `/api/skills/*` HTTP handlers from inside the tool.

## Rejected

- No runtime `pip install`, Python `fpdf`, or package install in the agent loop.
- No split document tools in the LLM manifest.
- No PPTX support in DOCSKL-01.
- No skill authoring/editor tool in DOCSKL-02.
- No raw skill bodies or generated artifact bytes in ordinary chat logs.
- No smoke-only completion claim.

## Missing / Blocked Evidence

- Live container probes are not run in the planning repair slice. They are
  benchmark rows for the implementation commits.
- Fresh independent plan verifier was not spawned in this run because the user
  did not authorize subagent/delegation work in the current turn. The plan is
  self-audited, not externally verified.
