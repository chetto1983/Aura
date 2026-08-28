---
last_mapped_commit: 8e15e14b12f6cfb4eb9951f4563505478c1e3a70
last_audited_commit: 8e15e14b12f6cfb4eb9951f4563505478c1e3a70
---

# Codebase Concerns

**Analysis date:** 2026-08-27

This is the only current concern ledger. It was rebuilt adversarially from the live code,
configuration and tests at the commit above. Old plans, summaries and spike findings are
historical evidence, not a work queue. The two former handoff files were removed after their
current claims and references were consolidated here; Git history remains their provenance.

## Active concerns

| Severity | Concern | Audit verdict |
|---|---|---|
| **Medium** | The owned document-agent oracle does not exercise the full-file or scanned-PDF lanes | **Confirmed; prior closure was too broad** |
| **Medium** | Managed conversation history is truncated by a fixed 50-row default before token budgeting | **Confirmed; duplicated in the prior ledger** |
| **Medium** | Aura cannot prove action-level coverage of the externally pinned Calendar/PIM fork | **Confirmed at the consumption boundary; fork internals not re-auditable here** |
| **Low** | Server-generated approval questions remain hard-coded in English | **Confirmed from the old handoff** |
| **Low** | Expired idempotency recovery is implemented and tested but unreachable in production | **Confirmed dark code / unresolved product decision** |
| **Low** | Several owned files have almost no room under the enforced 600-line cap | **Confirmed; prior counts were stale** |

### Medium — the document-agent release oracle covers only small text/XLSX retrieval

- **What is true:** the owned corpus is 21 files: 17 small XLSX files and four text files; it has
  no PDF, image-only PDF or large workbook
  (`scripts/fixtures/document_retrieval_eval/corpus.sha256:1-21`). Every behavior case requires
  only `document_search`; none requires `document_open`, `read_file` or `shell_exec`
  (`cmd/aura/document_agent_live_test.go:355-407`). The original-file handoff does exist and tells
  the model to use LibreOffice/openpyxl/pandas for aggregates
  (`internal/agent/tools/document_open.go:63-74`), but the live agent oracle does not prove that
  route.
- **Why it matters:** 9/9 is valid for the checked corpus, but it does not close the old spike's
  two remaining behavior questions: an aggregate that cannot be answered from retrieved chunks,
  and OCR of a real Italian scan with no text layer. It also does not prove that `document_open`
  and sandbox computation work as one agent motion on a realistically large workbook.
- **Action:** add owned, hash-pinned fixtures for (1) a large spreadsheet whose requested aggregate
  is absent from every indexed chunk and (2) an Italian image-only PDF. The production Runner cases
  must require the appropriate full-file/OCR tools and exact answers. Keep the ranking report
  diagnostic; do not restore the dead reranker or invent an abstention threshold.

### Medium — a fixed database-row cap precedes the token-aware context ladder

- **What is true:** `AURA_HISTORY_HARD_CAP_TURNS` defaults to 50
  (`internal/config/config.go:414`, `internal/config/config_knobs.go:93`, `.env.example:60`). Both
  linear and selected-branch managed history pass that normalized row cap into their database
  loaders before L1/L2/L2.5 token work (`internal/conversations/context.go:70-84,87-118`). Values
  outside 4–1,000 silently normalize back to 50 (`internal/conversations/context.go:27-29,121-125`).
- **Why it matters:** when no durable summary covers older turns, a large-window model cannot see
  them even if its token budget has room. The row cap bounds legitimate database/sidecar/tokenizer
  work, but the default is also the effective recall horizon.
- **Action:** measure query, sidecar-rehydration and tokenizer cost at larger windows, then advance
  or paginate from the durable compaction watermark. Until that evidence exists, describe 50 as a
  work ceiling and recall limit, not as token-aware capacity.

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

### Low — `RecoverExpired` has no production caller

- **What is true:** `(*idempotency.Store).RecoverExpired` and its atomic SQL path exist and are
  tested (`internal/idempotency/store.go:55-95`), but repository-wide callers are tests only. Normal
  CLI execution maps an expired/indeterminate operation to a refusal and never invokes recovery
  (`cmd/aura/idempotency.go:245-270`).
- **Why it matters:** the method is dark code under the repository rule, while the product behavior
  for an explicitly authorized retry remains ambiguous.
- **Action:** decide one contract: expose recovery through an authenticated, explicit operator
  action with discovery evidence, or delete `RecoverExpired`, its query and tests and keep
  indeterminate as deliberately terminal. Do not auto-retry destructive work.

### Low — line-cap pressure is immediate

- **Current counts:** `internal/agui/server_run_resume_test.go` is 600 lines;
  `internal/agent/tools/skill_write_test.go` 598; `internal/arcadedb/client.go` 595;
  `web/src/settings/__tests__/ModelSettingsPanel.test.tsx` 594; `internal/config/config.go` 584;
  `internal/llm/config.go` 579; `internal/conversations/store.go` 574; and
  `internal/agui/onboarding_provision.go` 568.
- **Action:** split by concern before adding behavior to any of these files. This is a maintenance
  constraint, not a coverage or runtime defect.

## Recent closures and scope boundaries

These compact entries prevent stale handoffs from reopening resolved work.

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
- **Memory preload:** closed. Agent-written, identity-scoped memory is trusted recalled knowledge,
  not untrusted third-party output; instruction-shaped remembered text does not gain operator or
  system authority. Boundary escaping and precedence live in
  `docs/aura-memory-preload-threat-model.md`, `internal/runner/runner_context.go:74-137` and
  `internal/agent/prompt.go:83-92`.
- **`filecard` OOXML parser:** closed only as a bounded, lossy routing-card reader. The owned
  openpyxl 3.1.5 parity gate covers 17 XLSX files/18 sheets and records its producer limits in
  `docs/aura-ooxml-routing-card-boundary.md`. It does **not** close the separate `read_file` parser
  above.
- **Garage `@file` / `@folder`:** closed. The composer uses the existing assistant-ui trigger
  pattern without `unstable_useLiveCompletionAdapter`; folder navigation lists descendants, and
  the authenticated run carries normalized file keys/folder prefixes into every retrieval leg
  (`web/src/chat/composer/GarageMentionPicker.tsx`, `internal/documents/source_scope.go:37-105`,
  `internal/documents/retrieval.go:188-218`).
- **Selected-model image/audio context:** closed within the measured capability boundary. The
  existing primary model setting is reused; OpenRouter `/models` and llama.cpp `/props` metadata
  gate native projection, owner/thread/size/digest are rechecked, and text summary/transcript is the
  fallback (`internal/llm/model_content_caps.go`, `internal/agui/asset_content_projection.go:28-57`,
  `internal/llm/openai_compat/request.go:150-183`). No second cloud-model selector was added.
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
  chosen exact-table lane; the unclosed issue is proving that lane with the stronger oracle above.
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

*Concerns audit: 2026-08-27 at `8e15e14b12f6cfb4eb9951f4563505478c1e3a70`.*
