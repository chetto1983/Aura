# v1.3 Memory Consolidation And Quality Validation

Date: 2026-05-07
Status: closed - strict live gate passed

## Result

v1.3 is closed. Aura's Docker runtime now passes deterministic memory cleanup, embedding/search validation, dashboard settings smoke, full Go/frontend verification, and the strict live memory quality gate with the DB-configured model.

The previous latency caveat on `glm-5.1:cloud` is resolved for the active container configuration. The live quality harness now applies dashboard settings from `aura.db`, so it tests the same `LLM_BASE_URL` and `LLM_MODEL` that the running app uses instead of stale env-only values.

## Passed

- Operational wiki files (`SCHEMA.md`, `index.md`, `log.md`) are excluded from ordinary wiki listing/search memory.
- Stale generated workflow docs were removed from active docs; `.planning/` is the workflow truth.
- `clean_wiki_memory` dry-runs by default and can apply deterministic hub/link/index/audit repairs.
- Nightly wiki maintenance runs the same deterministic cleanup before lint/defer work.
- Source wiki pages are compact anchors; OCR/extract previews stay in raw source evidence and `read_source`.
- Memory closure audit after Docker rebuild: 18 wiki pages, 45 expected index docs, 45 actual index docs, `issues=0`.
- Hermetic memory scorecard: 20/20, evidence hit rate 1.0, proposal quality rate 1.0.
- Qdrant/SQLite comparison smoke: Qdrant reachable at `127.0.0.1:6333`, p50 8 ms, overlap 1.0, recommendation `ok`.
- Live memory scorecard on DB-selected `deepseek/deepseek-v4-flash`: 20/20, `search_memory` 20/20, proposal calls 4/4 where expected, unexpected proposals 0, slow scenarios 0/20, max scenario 20.349 s under the 30 s budget.
- Settings/dashboard smoke passed after exposing the `agent` settings group and orchestration controls.
- Go verification, full Go test suite, frontend i18n/build, Docker rebuild, `/status`, and Playwright settings E2E passed.

## Notes

- The independent stale-reference audit found only harmless operational `log.md` history for deleted source slugs and compact source anchor pages. `log.md` remains excluded from user-facing memory.
- `golem-agente-ai-personale-in-go` is a real canonical wiki page, not a broken short alias. The old short `golem` slug remains covered by alias-resolution tests.
- No manual wiki cleanup was applied during closure. Runtime memory cleanup remains automated through `clean_wiki_memory` and the closure audit.

## Commands Run

```powershell
go run ./cmd/debug_memory_closure -wiki D:\Aura\wiki -db D:\Aura\data\aura.db -json
$env:QDRANT_URL='http://127.0.0.1:6333'; $env:QDRANT_COLLECTION='aura_memory_v1'; go run ./cmd/debug_qdrant -url http://127.0.0.1:6333 -q 'Aura memory consolidation Project PMS trading Telegram settings' -compare -json -runs 3 -warmup 1
go run ./cmd/debug_memory_quality -json -report-dir reports/memory-quality
$env:AURA_ENV_PATH='D:\Aura\data\.env'; $env:DB_PATH='D:\Aura\data\aura.db'; go run ./cmd/debug_memory_quality -live-llm -limit 5 -json -report-dir reports/memory-quality
$env:AURA_ENV_PATH='D:\Aura\data\.env'; $env:DB_PATH='D:\Aura\data\aura.db'; go run ./cmd/debug_memory_quality -live-llm -json -report-dir reports/memory-quality
go test ./cmd/debug_memory_quality ./internal/config ./internal/settings ./internal/api -count=1
powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1
go test ./... -count=1
npm --prefix web run i18n:check
npm --prefix web run build
docker compose config --quiet
$env:AURA_HOST_PORT='18080'; docker compose up -d --build aura
Invoke-WebRequest -UseBasicParsing http://127.0.0.1:18080/status
cd web; npm exec -- playwright test e2e/settings.spec.ts --reporter=line
```

## Evidence Artifacts

- Hermetic report: `reports/memory-quality/20260507-090439-hermetic.json`
- Live sample after DB-settings fix: `reports/memory-quality/20260507-091104-live-llm.json`
- Full live strict gate: `reports/memory-quality/20260507-091401-live-llm.json`

## Next Decision

Proceed to v3.1 Agent Orchestration And System Prompt Versioning before v4.0 MCP Marketplace expands the tool surface.
