# v1.3 Memory Consolidation And Quality Spec

Date: 2026-05-06
Status: validation

## Goal

Make Aura's memory graph useful, clean, and evidence-backed. v1.2 proved more sources can enter Aura; v1.3 must make sure those sources become durable knowledge instead of disconnected debris.

## Problem

The dashboard graph currently shows many isolated nodes. The audit found three root causes:

- Operational wiki files such as `SCHEMA.md` can be listed as pages unless explicitly excluded.
- Generated workflow docs under `docs/superpowers/` duplicated `.planning/` and contained stale implementation claims.
- Live wiki pages contain orphan source pages, broken aliases, and topic pages that need hub pages or explicit archival.

## Scope

- Clean checked-in docs so active workflow memory lives in `.planning/`.
- Exclude operational wiki files from `ListPages` and search indexing.
- Give the agent an automated `clean_wiki_memory` tool that dry-runs by default and can apply hub creation, alias repair, related-frontmatter repair, index rebuilds, and audit logging.
- Run the same deterministic cleanup from nightly wiki maintenance so graph repair does not depend on manual operator sessions.
- Audit `D:\Aura\wiki` for isolated pages, broken links, source-page leaves, and low-value test artifacts.
- Add or repair wiki hub links where the pages are valuable.
- Archive/delete only pages that are clearly test debris or obsolete operational material.
- Verify embedding/search wiring and cache behavior.
- Run or extend `search_memory` quality checks with real project questions.

## Out Of Scope

- MarkItDown integration.
- New upload formats.
- A dashboard redesign.
- Automatic deletion of personal/business source evidence without explicit confirmation.

## Initial Findings

- `docs/superpowers/specs/2026-05-06-v1-2-closure-polish-design.md` and `docs/superpowers/plans/2026-05-06-v1-2-closure-polish-plan.md` were stale generated workflow artifacts. They contradicted the shipped v1.2 DOCX reality and should not remain active docs.
- `D:\Aura\wiki` currently has source pages and trading-signal pages with no outgoing links. Some are useful evidence, but they need hub pages or archival decisions.
- Broken wiki links include old aliases such as `golang-agenti-ai-personali`, `goa-ai-framework`, `segnali-di-trading`, `trading`, `crypto`, `forex`, and `mercati`.
- Embedding setup is intentionally separate from chat model setup. The production path creates an OpenAI-compatible embedding function from `EMBEDDING_*`, wraps it with `EmbedCache`, and wires the wrapped function into the search engine.
- The first automation slice adds `clean_wiki_memory` and wires it into nightly wiki maintenance so Aura can reproduce this audit pattern itself: shared missing concepts become hub pages, obvious aliases are rewritten to canonical slugs, isolated pages are linked back to hubs, `index.md` is rebuilt, and `log.md` records the run.

## Verification Plan

- `go test ./internal/wiki ./internal/search -count=1`
- `go test ./internal/wiki ./internal/tools ./internal/search ./internal/telegram -count=1`
- `go test ./internal/tools -run SearchMemory -count=1`
- `go test ./internal/search -run EmbedCache -count=1`
- Live wiki graph audit before and after cleanup.
- `cmd/debug_memory_quality` once the wiki cleanup is complete and live keys are available.

## Validation Update

See `VALIDATION.md` for the current closure truth. Deterministic cleanup, embedding/search tests, live wiki hygiene, frontend audit, Go verification, and snapshot packaging passed. Live LLM routing uses `search_memory` correctly, but the configured `glm-5.1:cloud` runtime remains too slow for the 30s memory quality gate.
