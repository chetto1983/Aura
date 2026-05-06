# v1.3 Memory Consolidation And Quality Validation

Date: 2026-05-06
Status: validation - live latency caveat

## Result

The deterministic memory cleanup and repo release gates are ready for daily use. The remaining blocker is live-model latency for memory answers on the current `glm-5.1:cloud` configuration.

## Passed

- Operational wiki files (`SCHEMA.md`, `index.md`, `log.md`) are excluded from ordinary wiki listing/search memory.
- Stale generated workflow docs were removed from active docs; `.planning/` is the workflow truth.
- `clean_wiki_memory` dry-runs by default and can apply deterministic hub/link/index/audit repairs.
- Nightly wiki maintenance runs the same deterministic cleanup before lint/defer work.
- Source wiki pages are compact anchors; OCR/extract previews stay in raw source evidence and `read_source`.
- Live wiki hygiene dry-run: 17 pages, 0 broken links, 0 orphans, 0 repairs.
- Hermetic memory scorecard: 20/20, evidence hit rate 1.0, proposal quality rate 1.0.
- Embedding/search checks use dedicated `EMBEDDING_*` settings and cache tests.
- Frontend audit passed after deterministic conversation route fixtures were added.
- Go verification, frontend lint/i18n/build, GoReleaser snapshot, and Windows GUI subsystem checks passed.

## Caveat

Full live memory scorecard on `glm-5.1:cloud` failed latency:

- `search_memory` calls: 20/20.
- Proposal calls: 4/4, only where expected.
- Unexpected proposals: 0.
- Slow scenarios: 7/20 over the 30s end-user budget.
- Deadline partials: 3.

The evaluator was tightened so answer-only scenarios expose only `search_memory`, use one tool call, cap tool results to 3000 chars, and request shorter final answers. A 5-scenario live sample still failed latency, so the next milestone decision should focus on model/runtime speed or a lower-latency answer path rather than memory routing correctness.

## Commands Run

```powershell
go run ./cmd/debug_memory_quality -json -report-dir reports/memory-quality
$env:AURA_LIVE_WIKI_PATH='D:\Aura\wiki'; Remove-Item Env:AURA_LIVE_WIKI_APPLY -ErrorAction SilentlyContinue; go test -tags=live_wiki ./internal/wiki -run TestLiveCleanMemory -count=1 -v
go run ./cmd/debug_memory_quality -live-llm -json -report-dir reports/memory-quality
go run ./cmd/debug_memory_quality -live-llm -limit 5 -json -report-dir reports/memory-quality
go test ./cmd/debug_memory_quality ./internal/agent ./internal/tools -count=1
powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1
npm --prefix web run i18n:check
npm --prefix web run lint
npm --prefix web run build
npm --prefix web run audit:frontend
npm exec playwright test e2e/confirm-modal.spec.ts -- --config playwright.config.ts
go run github.com/goreleaser/goreleaser/v2@latest release --snapshot --clean
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\check-windows-gui-subsystem.ps1 dist\aura-windows_windows_amd64_v1\aura.exe
```

## Next Decision

Do not mark v1.3 fully closed until the live memory latency gate is either passed with a faster configured model/runtime or explicitly accepted as a release caveat.
