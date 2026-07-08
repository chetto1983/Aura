---
phase: 37A
slug: web-artifact-delivery-lane
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-07-08
---

# Phase 37A — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Authoritative source: `37A-RESEARCH.md` §Validation Architecture (line 291) + §Security Domain. The Per-Task map below is completed against plan task IDs by `/gsd-add-tests` / the planner.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework (backend)** | Go `testing` + `gotestsum`; table-driven; `-race`; `goleak` where goroutines/streams (golang-testing skill) |
| **Framework (property)** | `pgregory.net/rapid` or `gopter` for the RFC-6266 helper (property-based-testing skill) — **confirm vendored before use**; fall back to exhaustive table-driven cases |
| **Framework (frontend)** | vitest + @testing-library/react (`web/`), jsdom |
| **Config file** | none for `go test`; lint `.golangci.yml`; coverage `scripts/coverage_gate.sh` |
| **Backend quick run** | `go test ./internal/agent/tools/ ./internal/assets/ ./internal/agui/ -run 'SendFile\|Ingest\|AssetDownload\|Artifact'` |
| **Frontend quick run** | `cd web && npx vitest run src/chat` |
| **Migration roundtrip** | `go test -tags db_integration ./internal/db/ -run TestMigration0035` |
| **Full / gated suite** | `bash scripts/coverage_docker.sh` (containerized stack) or WSL `make quality-full` (`db_integration neo4j_integration`) |
| **Estimated quick runtime** | ~10–25 s (package-scoped) |

**Coverage-floor trap (CLAUDE.md — MANDATORY):** the gate runs `db_integration neo4j_integration` ONLY — there is no `docker_integration`/`garage_integration` job. Every new pure-logic surface (ingest orchestration, degrade decision, RFC-6266 helper, header forcing, stream logic) MUST be daemon-free unit-testable via `internal/objectstore/fake.go` or it drops the aggregate below 85% and fails CI ~20 min post-push. Do NOT gate these behind `garage_integration`.

---

## Sampling Rate

- **After every task commit:** package-scoped quick run for the touched package (`go test -race ./internal/<pkg>/`); for web-touching commits also `cd web && npx vitest run src/chat`. Post-edit Gate-2: `go vet ./... && go build ./...`.
- **After every plan wave:** full untagged `go test ./internal/agent/... ./internal/assets/ ./internal/agui/ ./internal/channels/telegram/` + `-race` on touched packages + `db_integration` migration roundtrip.
- **Before `/gsd-verify-work`:** `bash scripts/coverage_docker.sh` (owned-surface ≥85% across the tag matrix) + web coverage ≥85% + rebuilt `internal/webui/dist` committed. Full suite green.
- **Max feedback latency:** ~25 s (quick), full/gated on wave merge.

---

## Per-Task Verification Map

> Populated against plan task IDs (`37A-01-01` …) by the planner / `/gsd-add-tests`. The requirement→behavior→command rows below come verbatim from `37A-RESEARCH.md` §"Phase Requirements → Test Map"; each becomes ≥1 task row.

| Requirement | Behavior | Threat Ref | Test Type | Automated Command | File | Status |
|-------------|----------|------------|-----------|-------------------|------|--------|
| WEBART-01 | `IngestAgentFile`: bytes→fake `Put`→`Create(source_kind=agent)`→`MarkUploaded`→`MarkAccepted`; `processAsset` NOT called; `Limits.Validate` NOT called | — | unit (fakes) | `go test ./internal/assets/ -run TestIngestAgentFile` | ❌ W0 | ⬜ pending |
| WEBART-01 | `send_file` degrade matrix: nil `Assets` / empty identity / empty thread (`sessionID==""`) / `Put` error → descriptor `path`, no `asset_id`; never errors the turn | — | unit (fakes) | `go test ./internal/agent/tools/ -run TestSendFile_Degrade` | ❌ W0 | ⬜ pending |
| WEBART-01 | Both tails ingest: host-path `Execute` AND routed `deliverFromBox` produce `asset_id` (Landmine 1) | — | unit (fakes) | `go test ./internal/agent/tools/ -run 'TestSendFile_Ingest'` | ❌ W0 | ⬜ pending |
| WEBART-01 | migration `0035` roundtrip: INSERT `agent` OK after up; `23514` before-up / after-down | — | integration `db_integration` | `go test -tags db_integration ./internal/db/ -run TestMigration0035` | ❌ W0 | ⬜ pending |
| WEBART-02 | descriptor carries `asset_id`+`filename`+`size_bytes`+`mime_type`+`tool_call_id` on success | — | unit | `go test ./internal/agent/tools/ -run TestSendFile_DescriptorFields` | ❌ W0 | ⬜ pending |
| WEBART-02 | **Telegram unregressed:** descriptor still carries `path`; `artifact.consumeEvent` still sends a document; missing-path still no-op | — | regression unit | `go test ./internal/channels/telegram/ -run 'Artifact'` | ⚠️ extend `artifact_test.go:161` | ⬜ pending |
| WEBART-02 | Meta→ArtifactDelta lift passes new keys through (no llm_agent_events / translator change) | — | unit | `go test ./internal/agent/ -run 'Artifact' && go test ./internal/agui/ -run 'Artifact'` | ⚠️ extend | ⬜ pending |
| WEBART-03 | download: owner → 200, `Content-Disposition: attachment`+`filename*`, `Content-Type: application/octet-stream`, body == bytes | T-IDOR / T-XSS | integration/unit (httptest + fake store) | `go test ./internal/agui/ -run TestAssetDownload_Owner` | ❌ W0 | ⬜ pending |
| WEBART-03 | **non-owner → 404** (D-12 regression); **unauthenticated → 401/302** (RequireAuth) | T-IDOR | integration/unit | `go test ./internal/agui/ -run 'TestAssetDownload_NonOwner\|TestAssetDownload_Unauth'` | ❌ W0 | ⬜ pending |
| WEBART-03 | client-disconnect cancels the Garage read (ctx-scoped `io.Copy`, D-09) | T-DoS | unit (cancel ctx + goleak) | `go test ./internal/agui/ -run TestAssetDownload_ClientDisconnect` | ❌ W0 | ⬜ pending |
| WEBART-03 | RFC-6266 helper: unicode/CRLF/quote/`;`/empty/long → exactly one `filename=`+one `filename*=`, no raw CRLF (Landmine 4) | T-HdrInj | **property-based** unit | `go test ./internal/agui/ -run TestContentDisposition_Property` | ❌ W0 | ⬜ pending |
| WEBART-04 | `sseAdapter` reducer: `aura.artifact` CUSTOM frame → `local_artifact` display attached by `tool_call_id`; degraded (no `asset_id`) → chip only | — | unit (vitest) | `cd web && npx vitest run src/chat/__tests__/sseAdapter.test.ts` | ⚠️ **rewrite** `:383` | ⬜ pending |
| WEBART-04 | `LocalArtifactDisplay`: `asset_id` → `<a href="/api/assets/{id}/download" download={filename}>`; absent → path chip; never a raw host path when `asset_id` set | T-XSS | unit (vitest) | `cd web && npx vitest run src/chat/displays/__tests__/LocalArtifactDisplay.test.tsx` | ⚠️ extend | ⬜ pending |
| WEBART-04 | CLI/no-identity degrade end-to-end (nil `Assets` → path chip, no download button) | — | unit (both tiers) | covered by degrade-matrix + LocalArtifactDisplay tests | — | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

**Threat refs** (from `37A-RESEARCH.md` §Security Domain / §Known Threat Patterns): **T-IDOR** = cross-identity asset download (V4, `GetForIdentity`→404); **T-XSS** = stored XSS via served HTML/SVG (V5, force `attachment`+`octet-stream`+`nosniff`); **T-HdrInj** = CRLF response-header injection via filename (V5, RFC-6266 helper); **T-DoS** = unbounded stream / goroutine leak on disconnect (request-ctx-scoped `io.Copy`).

---

## Wave 0 Requirements

- [ ] `internal/assets/ingest_agent_test.go` — `TestIngestAgentFile` (skip-Limits / skip-processAsset / `SourceAgent`) with `objectstore/fake.go`
- [ ] `internal/agent/tools/send_file_ingest_test.go` — degrade matrix + both-tails-ingest + descriptor fields (fake `AssetDeliverer` interface)
- [ ] `internal/agui/asset_download_test.go` — owner-200 / non-owner-404 / unauth / disconnect / header assertions (httptest + `objectstore/fake.go`)
- [ ] `internal/agui/content_disposition_test.go` — property-based RFC-6266 (rapid/gopter; table-driven fallback)
- [ ] `internal/db/migrate_0035_integration_test.go` — `db_integration` roundtrip (mirror `migrate_0033_integration_test.go`)
- [ ] `internal/channels/telegram/artifact_test.go` — **extend** with enriched-descriptor-still-sends assertion (non-regression)
- [ ] `web/src/chat/__tests__/sseAdapter.test.ts` — **rewrite** the `:383` no-op test to assert `aura.artifact` now attaches the card
- [ ] `web/src/chat/displays/__tests__/LocalArtifactDisplay.test.tsx` — **extend** with download-button-when-asset_id + chip-when-degraded
- [ ] Framework: none to install — confirm the property lib (`pgregory.net/rapid`/`gopter`) is vendored (Package Legitimacy Gate if adding one)

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Real-browser download UX: agent produces a file → web chat renders the button → click streams the file with the correct name and `attachment` disposition | WEBART-04 | End-to-end browser download (save-dialog + on-disk bytes) is outside vitest/jsdom | Live E2E per `/gsd-verify-work`: run a real turn where the agent `send_file`s a DOCX; confirm the download button appears, click it, verify the saved file matches and the browser never received a raw host/container path |
| Telegram artifact still delivered after the descriptor enrichment | WEBART-02 | Cross-channel live delivery via the Bot API | Live E2E: same turn on the Telegram channel; confirm the document still arrives (non-regression) |

*All other phase behaviors have automated verification.*

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 25s (quick)
- [ ] Every new pure-logic surface is daemon-free unit-tested (counts toward the `db_integration neo4j_integration` 85% gate)
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
