---
last_mapped_commit: a357a7b75b34d4d31f0ee6cf716626675610e39a
last_audited_commit: 8e15e14b12f6cfb4eb9951f4563505478c1e3a70
---

# Codebase Concerns

**Analysis date:** 2026-08-28

This is the only current concern ledger. It was rebuilt adversarially from the live code,
configuration and tests at `last_audited_commit`, with verified closures mapped through
`last_mapped_commit`. Old plans, summaries and spike findings are
historical evidence, not a work queue. The two former handoff files were removed after their
current claims and references were consolidated here; Git history remains their provenance.

## Active concerns

| Severity | Concern | Audit verdict |
|---|---|---|
| **Medium** | Aura cannot prove action-level coverage of the externally pinned Calendar/PIM fork | **Confirmed at the consumption boundary; fork internals not re-auditable here** |
| **Low** | Server-generated approval questions remain hard-coded in English | **Confirmed from the old handoff** |

### Medium — Calendar/PIM action behavior is proved mainly by Aura's integration sample

- **What is true in this repository:** Compose and CI pin the external sidecar by tag and digest
  (`compose.yaml:1084-1086`, `.github/workflows/ci.yml:1204-1223`). Aura's live tier verifies the
  single 29-action schema, tenant isolation, `list_accounts` and one event-list/detail chain
  (`internal/mcp/calendar_integration_test.go:104-181,254-296`); CI runs that tier under `-race`
  (`.github/workflows/ci.yml:1276-1283`). It does not execute every multiplexed action.
- **Evidence boundary:** Phase 46 records that the fork's 14 raw-tool MSTest files were deleted
  without action-tool replacements, but the external repository is not vendored here. This audit
  therefore confirms Aura's partial consumption gate, not the current test inventory of the fork.
- **Action:** make the fork publish action-level test evidence for the exact pinned image, covering
  every dispatch arm and destructive validation path. Retain Aura's OAuth/two-identity live tier as
  the cross-repository contract rather than pretending its sample is fork unit coverage.

### Low — approval question prose is not localizable

- **What is true:** gateway and scheduled-task approval questions are formatted as English prose in
  Go (`internal/gateway/approve.go:181-190`, `internal/agent/tools/task.go:290`). The browser
  localizes frames, button labels and outcomes, but the persisted question itself is displayed
  verbatim and is part of the byte-equality informed-consent binding
  (`internal/gateway/approvals.go:119-170`).
- **Why it matters:** an Italian cockpit still shows the consequential action description in
  English. Translating only in the client would break or obscure the exact question binding.
- **Action:** persist a stable semantic message key plus bounded structured parameters beside the
  canonical question; render localized prose on each surface while retaining a server-verifiable
  consent payload. Cover gateway, scheduled and shell approvals together.

## Recent closures and scope boundaries

These compact entries prevent stale handoffs from reopening resolved work.

- **Managed conversation history:** closed. `AURA_HISTORY_HARD_CAP_TURNS=50` remains the existing
  compatibility knob but now controls keyset page size, not recall. Linear and selected-branch
  loaders traverse to the exact durable compaction watermark or root, never accept a watermark
  across a gap, never open covered sidecars, and fail explicitly at independent row/byte/sidecar
  work ceilings instead of returning partial history (`internal/conversations/store_managed_history.go`,
  `internal/db/queries/conversation_turns.sql`). Disposable-Postgres integration tests cover
  multi-page linear/branch histories, invalid paths, missing watermarks and work limits. The owned
  benchmark measured a 50-sidecar tail at 0.491/0.470/0.546 s with 250/1,000/5,000 total turns,
  showing covered-prefix-independent I/O on this NTFS/WSL appliance; amendment #169 records the
  boundary and the wider baseline.
- **`read_file` OOXML expansion:** closed. Exact DOCX/XLSX extraction now rejects declared
  oversized members before opening them, verifies the actual stream through a `limit+1` reader,
  shares a 32 MiB decompression budget across the package, and caps XML depth, node count,
  character data and rendered output (`internal/agent/tools/document_extract_ooxml.go`,
  `internal/agent/tools/document_extract.go:145-288`,
  `internal/agent/tools/document_extract_xlsx.go:27-111`). Security-limit errors are propagated
  through XLSX's compatibility skips instead of being mistaken for malformed optional parts.
  Regressions cover a DOCX that compresses below the workspace cap but expands past the member
  limit, aggregate expansion, deep XML and shared-string output amplification
  (`internal/agent/tools/document_extract_test.go`).
- **GSD planning consistency:** closed. `STATE.md` now derives 45 total plans, 37 matching plan
  summaries, three completed phases and the Phase 51 focus from the live planning tree; ROADMAP's
  Phase 52 row now records the actual 8/8 executed plans while retaining its honest In Progress
  validation status. `scripts/check_planning_consistency.py` independently checks those counts,
  every phase/progress row, current phase/name/status and the Git input commit recorded by
  `state_head`; the build-and-lint job runs it on every change (`.github/workflows/ci.yml`).
- **Expired idempotency recovery:** closed by choosing the existing production contract:
  indeterminate operations remain terminal and destructive work is never reclaimed implicitly.
  The unreachable `Store.RecoverExpired` method, generated `TryRecoverExpiredOperation` query,
  fake hooks and unit/integration tests for that nonexistent product path were deleted; CLI
  refusal and stale-claim fencing remain covered. `git grep` now finds no recovery symbol outside
  this historical closure note.
- **600-line headroom:** reclassified as an enforced maintenance constraint, not an active defect.
  `scripts/check-file-size.sh` checks every tracked Go/TypeScript source file (tests included), CI
  runs it, and the current 2,460-file inventory has zero violations. Files exactly at the limit
  cannot accept another line: `CLAUDE.md` already requires a concern split on touch. Lowering the
  global cap would force dozens of behavior-neutral test-file moves without reducing runtime risk,
  so no cosmetic refactor was manufactured to close this ledger.
- **Memory preload:** closed. Agent-written, identity-scoped memory is trusted recalled knowledge,
  not untrusted third-party output; instruction-shaped remembered text does not gain operator or
  system authority. Boundary escaping and precedence live in
  `docs/aura-memory-preload-threat-model.md`, `internal/runner/runner_context.go:74-137` and
  `internal/agent/prompt.go:83-92`.
- **`filecard` OOXML parser:** closed only as a bounded, lossy routing-card reader. The owned
  openpyxl 3.1.5 parity gate covers 18 XLSX files/19 sheets and records its producer limits in
  `docs/aura-ooxml-routing-card-boundary.md`. It does **not** close the separate `read_file` parser
  above.
- **Garage `@file` / `@folder`:** closed. The composer uses the existing assistant-ui trigger
  pattern without `unstable_useLiveCompletionAdapter`; folder navigation lists descendants, and
  the authenticated run carries normalized file keys/folder prefixes into every retrieval leg
  (`web/src/chat/composer/GarageMentionPicker.tsx`, `internal/documents/source_scope.go:37-105`,
  `internal/documents/retrieval.go:188-218`).
- **Selected-model image/audio/scanned-PDF context:** closed within the measured capability
  boundary. The
  existing primary model setting is reused; OpenRouter `/models` and llama.cpp `/props` metadata
  gate native projection, owner/thread/size/digest are rechecked, and text summary/transcript is the
  fallback (`internal/llm/model_content_caps.go`, `internal/agui/asset_content_projection.go:28-57`,
  `internal/llm/openai_compat/request.go:150-183`). Digital PDFs remain on Tika; only an entirely
  blank text layer enters the existing selected vision route. Rasterization is private, bounded to
  20 pages/2048 pixels, probes page 21 to reject rather than truncate larger scans, and fails the
  whole document on any empty/error page (`services/ingest/media.py`,
  `cmd/aura-media-index/main.go`). No second cloud-model selector was added.
- **MCP HTTP and Compose networking:** closed. First-party Calendar, Memory and WhatsApp resources
  are HTTP/OAuth service endpoints (`compose.yaml:661,878,1106`); observability services no longer
  share Aura's network namespace, and static tests forbid `network_mode: service:aura`
  (`cmd/aura/container_artifacts_test.go:347-373`). `compose.minipc.yaml` is absent from the tracked
  tree.
- **Observability/Grafana:** closed on the current stack. Datasources use Compose service DNS, the
  scheduled bounded checker verifies direct readiness plus Prometheus/Tempo through Grafana, and
  the operator confirmed dashboard data arrives (`observability/grafana/provisioning/datasources/aura.yml`,
  `internal/obs/sidecar_check.go`, `internal/cron/handlers/observability_check.go`).
- **Document-pipeline spike:** the stable cross-language `doc_` identity, Garage S3 source,
  CocoIndex `auto_refresh`, schema-first ArcadeDB writes and original-file open path are implemented
  (`services/ingest/identity.py`, `services/ingest/app.py:279-397`,
  `services/ingest/arcade.py:144-245`, `internal/documents/open.go:53-94`). The reranker stays
  retired. A bespoke `table_query` is not missing: `document_open` plus sandbox computation is the
  chosen exact-table lane. Commit `a357a7b75` closes the former oracle gap with a deterministic
  5,000-row XLSX whose answer is absent from indexed chunks, an image-only Italian PDF, ordered
  `document_search -> document_open -> shell_exec` evidence, and a 23/23 disposable live run. A
  separate public 96-page Constitution witness proved digital PDFs stay on Tika when the vision
  binary is unavailable and the real Gemma/Ollama agent recovered an edition-specific footnote by
  opening the full file in seven calls. The operator-supplied large private workbook independently
  returned the expected aggregate `262`; neither operator file nor path is retained. Amendment
  #170 records the measured boundary and deliberately leaves ranking diagnostic.
- **File-manager JSON gate and compaction/DR:** closed. File mutations use the shared strict JSON
  decoder (`internal/agui/files_api_write.go:70-96`); impossible compaction thresholds fail at boot,
  small-window reserves scale, and ArcadeDB restore is part of the four-plane drill
  (`internal/conversations/context_budget.go:90-170`, `scripts/restore_drill.sh`).

## Inventory disposition

- `.planning/codebase/CONCERNS.md` — this canonical current ledger.
- Former `docs/HANDOFF.md` and `spikes/cocoindex-ingestion/HANDOFF.md` — removed after current
  claims were audited and consolidated here. Historical revisions remain available in Git; live
  references now point to this ledger or the spike's `FINDINGS.md`.
- `.planning/intel/classifications/*handoff*.json` — ingestion classifications, not handoffs.
- `.planning/tmp/**`, `web/node_modules/**/serverHandoff.js` — ignored/generated artifacts, not
  repository state documents.

---

*Concerns map: 2026-08-28 through `a357a7b75b34d4d31f0ee6cf716626675610e39a`;
last full adversarial audit: 2026-08-27 at `8e15e14b12f6cfb4eb9951f4563505478c1e3a70`.*
