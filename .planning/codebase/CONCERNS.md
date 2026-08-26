---
last_mapped_commit: 8e7893b6fd8fc4727ae81810a87dd49ce294689b
last_audited_commit: 3ab589ee6e2302e7bf3ff865e0083a4f7475b63e
---

# Codebase Concerns

**Analysis Date:** 2026-08-26

**Audit refresh:** Amendments #151 and #152 split coverage evidence by runtime tier and add
cross-package attribution inside the unit plus `db_integration` tier. Release readiness now
requires the independent unit/database, native Docker, and live ArcadeDB reports, plus the
exact package-local policy result; known low packages can no longer regress behind stronger
ones.

## Severity Summary

| Concern | Severity | Status | Primary evidence |
|---|---|---|---|
| Approval resume enforces a persisted per-pause decision policy | High | Closed 2026-08-25 | `internal/runner/resume_policy.go`, `internal/db/migrations/0102_paused_state_decision_policy.up.sql`, `internal/runner/live_e2e_policy_test.go` |
| Pending approvals have no expiry | High | Closed 2026-08-25 | `internal/runner/approval_expiry.go`, `cmd/aura/approval_expiry.go`, `internal/runner/live_e2e_expiry_test.go` |
| Empty accepted approval answers resumed the model silently | High | Closed 2026-08-25 | `internal/agui/server_run_test.go`, `internal/runner/runner_resume_test.go`, `internal/askuser/store_mutation_test.go` |
| Release disclosure register reported NO-GO | High | Closed 2026-08-25 | `docs/audit/README.md`, `scripts/audit_closure_gate.py` |
| Amendment #115 still requires a real-production document E2E whose runner no longer exists | High | Closed 2026-08-25 | PRD amendment #141, `scripts/ingest_reconcile_e2e.sh`, `cmd/aura/document_agent_live_test.go` |
| ArcadeDB tenant memory has no exercised backup/restore plane | High | Closed 2026-08-25 | `scripts/restore_drill.sh`, `scripts/release_readiness_gate.py`, `compose.yaml` |
| Shared deployment catalogs need administrator-only mutation in multi-user operation | Medium | Closed 2026-08-26 | `internal/agent/tools/skill_manage.go`, `cmd/aura/serve_webui.go`, `internal/config/config_validate.go` |
| Daemon-gated coverage tiers did not feed release readiness | Medium | Closed 2026-08-26 | `scripts/docker_coverage_gate.sh`, `scripts/agent_memory_eval.py`, `scripts/release_readiness_gate.py` |
| Owned unit + database coverage remains aggregate, not per-package | Medium | Closed 2026-08-26 | `scripts/coverage_package_gate.py`, `scripts/coverage_package_policy.json`, `scripts/release_readiness_gate.py` |
| Long-history compaction can disable itself silently and uses an unmeasured three-minute timeout | Medium | Closed 2026-08-26 | `internal/conversations/context_budget.go`, `internal/conversations/compaction.go`, `internal/runner/runner_context.go`, `web/src/chat/ContextBudgetGauge.tsx` |
| Calendar MCP admin-token fallback was removed by identity-scoped OAuth | Medium | Closed 2026-08-26 | `internal/agui/connect_pim_api.go`, `cmd/aura/serve_agui.go`, `compose.yaml` |
| Opt-in memory preload inserts recalled content into model-visible context with no dedicated poisoning threat model | Medium | Default off | `internal/runner/runner_context.go`, `internal/config/config.go` |
| Test/evaluation evidence contains skipped, stale, flaky, or non-reproducible legs | Medium | Open | `scripts/agent_memory_eval.py`, `docs/aura-quality-snapshot.md`, `.planning/STATE.md` |

## Tech Debt

**Closed 2026-08-26 — deferred manifest aligned with the curated Calendar surface:**
- Resolution: the checked-in corpus now has 85 deferred specs: 10 built-ins, 1 Calendar, 55 Linear, 4 Memory, and 15 WhatsApp. The added `calendar__calendar` spec follows the immutable sidecar's single-tool flat-union contract, including its 29-action enum and sole root-required `action` field.
- Drift gate: `TestDeferredManifestFixture_SourceInventory` pins every expected source count, so a curation change cannot silently leave the snapshot behind. The real-corpus ranking gate now includes a Calendar event query and therefore exercises the mounted capability instead of merely counting JSON rows.
- Evidence: both new assertions were first observed red against the 84-entry fixture; after the atomic fixture update, source inventory is green and retrieval is **100% top-1 / 100% recall@3 (20/20)**.
- Boundary: this closure uses the documented atomic-snapshot route, not a new live production-builder generator. Any future curated-surface change must update the fixture, pinned inventory, and ranking cases together.
- Files: `internal/agent/tools/testdata/deferred_manifest.json`, `internal/agent/tools/search_gate_test.go`.

**Closed 2026-08-26 — quality snapshot distinguished historical rows from current gates:**
- Resolution: the retired Neo4j HNSW baseline is explicitly historical and no longer claims a live migration-path gate. ADR 0038 is superseded as an active store choice, the removed Skills North-Star and snippet-reuse harnesses are explicitly retired, and the old machine-card document metric remains superseded rather than being quoted for the CocoIndex/ArcadeDB path. The 2026-08-23 handoff is explicitly archived instead of masquerading as a current work queue.
- Boundary: the absence of a reproducible production document-retrieval baseline and the deliberately skipped LOCOMO/cross-lingual cases remain real evaluation gaps below. This closure removes stale claims; it does not manufacture replacement measurements.
- Files: `docs/aura-quality-snapshot.md`, `docs/HANDOFF.md`, `docs/adr/0038-graph-store-license-neo4j-gplv3-vs-arcadedb-apache.md`, `docs/adr/0045-evaluation-corpora-licensing.md`.

**Production document E2E contract has no runner — CLOSED 2026-08-25:**
- Resolution: amendment #141 replaces the retired catalog/version/stage contract with the measured production path: per-identity Garage, CocoIndex reconciliation, the real extractor/embedder, per-identity ArcadeDB, native retrieval, and a real OpenRouter agent. The replacement keeps the existing `scripts/ingest_reconcile_e2e.sh`; no withdrawn runner name was restored.
- Evidence: the WSL gate passed add, unchanged restart, modify, delete, live refresh, structural-schema, `.xls` extraction, two-identity isolation, migration 0102 up/down/up, production `document_search`, and a real-agent answer. `services/ingest/tests/test_arcade_integration.py` additionally proved live removal of retired values, properties, indexes, and types.
- Cleanup: the always-null/duplicated passage fields, `HAS_PASSAGE`, `DocumentProjection`, Go-side duplicate DDL, retired document-pipeline knobs, the ignored projection writer copy, and the catalog-backed live test were removed. CocoIndex is the sole document-schema writer and lifecycle reconciler.

**Closed 2026-08-26 — unit plus `db_integration` has package-local regression floors:**
- Resolution: `scripts/coverage_gate.sh` now collects one native covdata corpus with `-coverpkg=./internal/...`; tests in both `internal` and `cmd/aura` contribute execution before the owned-source exclusions are applied. The explicit 71-package policy fails on new, removed, missing, or unclassified packages.
- Contract: 58 packages have the exact 85% floor, 12 known low packages have pinned covered/total non-regression baselines plus the visible 85% target, and `internal/sandbox/usersandbox` is delegated only to the separately release-blocking native Docker coverage authority. Lowering a baseline requires a new measurement and PRD amendment.
- Evidence: the fresh disposable-PostgreSQL run passed every test and every package rule at **27,671/31,839 = 86.9091%** aggregate. Direct unit and database tests raised `internal/mcpregistry` from 0/75 to **71/75 = 94.6667%**, so no zero-coverage package was grandfathered. Contract and release-readiness regressions cover exact-ratio decline, denominator drift, inventory drift, missing evidence, and invalid delegation.
- Boundary: 12 package deficits remain named debt rather than being represented as 85%. Docker and ArcadeDB retain independent denominators; no cross-tier percentage is manufactured.
- Files: `scripts/coverage_gate.sh`, `scripts/coverage_package_gate.py`, `scripts/coverage_package_policy.json`, `scripts/release_readiness_gate.py`, `internal/mcpregistry/store_test.go`, `internal/mcpregistry/store_integration_test.go`.

**Manual OOXML parser is a broad custom maintenance surface:**
- Issue: `internal/documents/filecard` manually parses ZIP/XML workbook and Office structures across `xlsx.go`, `ooxml.go`, `zip.go`, and `table.go` instead of using a workbook library. The implementation is split under the 600-line cap and well tested, but remains a large protocol parser owned by Aura.
- Files: `internal/documents/filecard/xlsx.go`, `internal/documents/filecard/ooxml.go`, `internal/documents/filecard/zip.go`, `internal/documents/filecard/table.go`, `internal/documents/filecard/card_test.go`.
- Impact: uncommon OOXML variants, relationships, cell encodings, and workbook features become Aura-specific compatibility work.
- Fix approach: inventory the installed/version-pinned library surface before replacing anything; either document measured reasons for retaining the parser or migrate behind the existing `filecard.Build` boundary with corpus parity tests.

**Closed 2026-08-26 — legacy staged user files were removed:**
- Resolution: the two recorded `aura-sendfile-*` directories are absent from the run root. The retired prefix no longer exists in source; the active sweeper continues to own only `$AURA_RUN_DIR/tmp/` by design.
- Evidence: `docs/audit/README.md` records the clean run root after operator-reviewed cleanup. No recurring leak was found, so no legacy-prefix deletion rule was added to the live sweeper.
- Files: `docs/audit/README.md`, `internal/conversations/orphan_scan.go`, `internal/agent/tools/send_file.go`.

## Known Bugs

**Closed 2026-08-25 — empty accepted approval answers resumed the model silently:**
- Resolution: the AG-UI boundary returns HTTP 400 for missing, empty, or whitespace-only resolved payloads before calling `SubmitAnswers`; Runner validates the effective answer used for both persistence and the `RoleTool` turn; and `askuser.Store` enforces the same invariant inside the transaction front door.
- Compatibility: decline/cancel still permits empty caller content, and scheduled approvals remain valid because Runner validates their server-authored outcome rather than the caller's empty placeholder.
- Files: `internal/agui/server_project.go`, `internal/runner/runner_resume.go`, `internal/askuser/store.go`, `.planning/todos/pending/approval-resume-defects.md`.
- Evidence: wire, Runner, Store, and rollback regressions are green; the full disposable-Postgres `db_integration` matrix measured 26827/31139 = 86.2% owned coverage; `ValidateResumeAnswer` mutation score is 4/5 = 80% killed.

**Closed 2026-08-25 — resume decisions could exceed the pause's policy:**
- Resolution: every production pause writer persists explicit server-authored `allowed_decisions`; Runner validates the persisted policy before any single/batch side effect; missing or invalid policy fails closed; AG-UI and Approval Center return HTTP 403 for a forbidden decision.
- Migration: 0102 backfills every existing pause without a runtime legacy fallback. A real disposable-PostgreSQL up/down/up test proves normalization, field preservation, rollback, and repeatability.
- Files: `internal/runner/resume_policy.go`, `internal/runner/runner_resume.go`, `internal/db/migrations/0102_paused_state_decision_policy.up.sql`, `internal/agui/server_run.go`, `internal/agui/server_run_detach.go`.
- Evidence: policy mutation 20/20 = 100%; Runner aggregate coverage 85.3%; targeted WSL race green; real OpenRouter-agent E2E proved forbidden accept stays pending and allowed decline re-drives the agent from 2 to 3 turns with 3750 prompt tokens.

**Closed 2026-08-25 — pending approvals did not expire:**
- Resolution: unanswered `kind=approval` pauses expire after an operator-configurable 48-hour default. The daemon performs an owner-scoped boot sweep and bounded periodic sweeps, using the existing atomic resume claim to append an `expired` refusal and its matching `RoleTool` turn together.
- Safety: `expired` is server-only; public clients cannot submit it. A late human decision returns Gone, and a real PostgreSQL expiry-versus-human race proved exactly one winner and one tool-result turn under RLS.
- Files: `internal/runner/approval_expiry.go`, `cmd/aura/approval_expiry.go`, `internal/askuser/store.go`, `internal/gateway/approvals.go`, `internal/runner/live_e2e_expiry_test.go`.
- Evidence: targeted WSL race/vet/build and disposable-PostgreSQL integration tests are green; migration 0102 still round-trips; expiry mutation testing killed 14/14 mutants; the real OpenRouter-agent E2E scored 10.0/10.

**Runner verification test is flaky under the full parallel gate:**
- Symptoms: `TestVerifyOnStopFiresOnARealTurn` is recorded failing in the full parallel gate while passing repeatedly in isolation.
- Files: `internal/runner/runner_verification_integration_test.go`, `docs/aura-quality-snapshot.md`.
- Trigger: run the repository-wide parallel test/coverage workload; the exact scheduler interaction is not yet isolated.
- Workaround: rerun the focused test to distinguish the known flake from a deterministic regression, but do not treat the rerun as a fix.

## Security Considerations

**Closed 2026-08-25 — per-pause resume authorization:**
- Former risk: a crafted authenticated POST could choose a decision not authorized when the pause was minted.
- Resolution: persisted server-authored policy is enforced before claims and side effects; migration 0102 removes the legacy-row exception; forbidden HTTP decisions return 403.
- Evidence: `internal/runner/resume_policy_test.go`, `internal/db/migrate_0102_integration_test.go`, and `internal/runner/live_e2e_policy_test.go` cover pure, real-database, and real-agent boundaries respectively.

**Closed 2026-08-26 — deployment-global catalogs are administrator control planes:**
- Former risk: settings, the MCP catalog, scheduler governance and skills were grouped as if every global row were tenant data. The real mismatch was narrower: `skill_manage` could mutate the shared skill roots for any `agent.run` caller.
- Resolution: settings and MCP web reads/writes were already behind `governance.read/write`; the scheduler's model-facing task operations were already owner-scoped; MCP OAuth/session/data were already identity-scoped. `skill_manage` now checks the authenticated caller for `governance.write` through the live identity store before parsing or dispatching any action and fails closed on a missing principal/checker, denial or store error. The in-box `/skills` tree is a copy, not a host-writable mount.
- Trust boundary: granting `governance.write` deliberately makes that identity a deployment administrator. Ordinary `agent.run` identities can use the approved shared skill library but cannot alter it.
- Evidence: `internal/agent/tools/skill_manage_auth_test.go`, `cmd/aura/skill_manage_auth_wiring_test.go`, `cmd/aura/serve_webui_auth_test.go`, `cmd/aura/serve_adapters.go`.

**Closed 2026-08-26 — identity-scoped OAuth for Aura-owned remote MCPs:**
- Former risk: Calendar carried a sidecar-specific admin fallback, and Calendar, WhatsApp, and Memory did not share one identity-scoped authorization/session model.
- Resolution: all three are standard OAuth MCP resource servers. Aura stores one grant and opens one client session per identity/server; the verified access-token `sub` is the sole tenant selector. No static built-in MCP bearer, sidecar-specific secret, proprietary identity header, model argument, or request metadata can select a tenant.
- Evidence: the production-like two-subject live tier passed 3/3 under `-race` and `goleak` against the real sidecars, including a forged metadata attempt; the real Cockpit/agent gate passed 2/2 and fresh production doctors opened Calendar, Memory, and WhatsApp without `aura mcp login`.

**Opt-in recalled memory is a prompt-injection and memory-poisoning seam:**
- Risk: when enabled, current-message retrieval is inserted into the model-visible prompt under text describing it as the model's "own knowledge." Stored facts can originate from earlier model/tool/document activity, so poisoned memory can influence a later turn before explicit tool selection.
- Files: `internal/runner/runner_context.go`, `internal/runner/runner_memory_context_test.go`, `internal/config/config.go`, `cmd/aura/chat_boot.go`.
- Current mitigation: `AURA_MEMORY_PRELOAD_ENABLED` defaults to false, recall is identity-scoped, and retrieval failures fail soft.
- Recommendations: write a dedicated trust-boundary threat model before enabling it by default; distinguish data from instructions in framing and add adversarial memory-poisoning tests.

**Closed 2026-08-26 — mounted MCP trusted-result provenance has a focused regression test:**
- Resolution: `TestBridgedTool_Execute_MarksResultTrusted` executes the bridge result seam and asserts non-nil provenance, exact source, and `TrustTrusted`.
- Evidence: the focused WSL test passes in `internal/agent/mcptools/bridge_test.go`; result size caps and operator-managed mount policy remain unchanged.
- Files: `internal/agent/mcptools/bridge_call.go`, `internal/agent/mcptools/bridge_test.go`.

## Performance Bottlenecks

**Database history fetch still defaults to 50 turns before token budgeting:**
- Problem: history loading fetches at most `AURA_HISTORY_HARD_CAP_TURNS` rows, default 50, before the token-aware ladder and durable compaction run.
- Files: `internal/conversations/context.go`, `internal/config/config.go`, `internal/config/config_knobs.go`.
- Cause: the query/sidecar/tokenizer safety cap is a row count independent of the selected model's context window. Older rows that were never compacted are unavailable to the ladder.
- Improvement path: measure query/tokenizer cost at higher caps, then derive or advance the fetch window from durable compaction state and the model budget rather than relying on a fixed default.

**Closed 2026-08-26 — compaction budgets and degradation are explicit:**
- Resolution: the private three-minute deadline was removed. L2.4 now inherits the positive deployment-wide `AURA_LLM_TOTAL_TIMEOUT_SEC` budget while the transport retains its independent `AURA_LLM_STREAM_IDLE_TIMEOUT_SEC` stall watchdog. Production boot measures the rendered manifest and rejects an enabled early trigger whose remaining history allowance is not positive.
- Degradation contract: a failed, empty, or oversized LLM summary that forces L2.5 writes `compaction_failed_hard_drop`; a planned deterministic drop with no attempted compaction remains `hard_drop_pairs`. The rot-events API already carries the action, and the context gauge now renders the failed-fallback count as a visible danger message.
- Comparative evidence: LibreChat agents `6e7632cc33c2` exposes impossible budgets and summary lifecycle failures while preserving overflow history; Hermes `68518c1f9bca` separates inactivity/absolute budgets and reports timeout fallback without dropping messages. Amendment #153 records the adopted Aura-specific contract.
- Focused proof: the new configured-deadline, measured boot-gate, failed/oversized/nil-summarizer action, LLM timeout validation, and cockpit warning cases pass. Previously validated broad suites were not rerun.
- Files: `internal/conversations/compaction.go`, `internal/conversations/context.go`, `internal/conversations/context_budget.go`, `internal/conversations/context_rot.go`, `internal/runner/runner_context.go`, `cmd/aura/chat_boot.go`, `web/src/chat/ContextBudgetGauge.tsx`.

**Document retrieval quality has no current reproducible baseline:**
- Problem: the document routing recall row is recorded UNKNOWN, while its former corpus is not repository-owned and the replacement non-commercial corpus was declined.
- Files: `docs/aura-quality-snapshot.md`, `docs/HANDOFF.md`, `docs/adr/0045-evaluation-corpora-licensing.md`, `internal/documents/retrieval_abstention_eval_test.go`.
- Cause: the harness files compile, but no CI path executes them against a permitted, pinned reference corpus.
- Improvement path: define a permissively licensed synthetic/reference corpus, pin expected queries and locators, and run it after ingestion settings change.

## Fragile Areas

**Approval resume transaction boundary:**
- Files: `internal/agui/server_project.go`, `internal/runner/runner_resume.go`, `internal/runner/resume_committer.go`, `internal/askuser/store.go`.
- Why fragile: wire mapping, owner scoping, pause claims, answer-turn persistence, hooks, and idempotency span several packages. Validation added outside the cross-store transaction can recreate claimed-without-answer or double-resume failures.
- Safe modification: validate expiry before claims inside the existing committer boundary; retain persisted policy validation, accepted-content validation, sorted-token batch locking, and the `resumed_at IS NULL` conditional update.
- Test coverage: atomic single/batch resume tests exist under `db_integration`; empty accepted content, decision policy, and expiry are closed at wire, Runner, Store/database, migration, race, and real-agent boundaries.

**Context ladder and durable compaction:**
- Files: `internal/conversations/context.go`, `internal/conversations/context_budget.go`, `internal/conversations/compaction.go`, `internal/conversations/compaction_durable_test.go`, `internal/conversations/compaction_inforce_test.go`.
- Why fragile: system/always blocks, active rounds, tool-call/result pairing, durable watermarks, cache-prefix stability, transient context, and final request reserves interact in one path.
- Safe modification: preserve complete user-led rounds and assistant-tool/result adjacency; test stored-compaction replay, branch watermarks, impossible thresholds, provider reserves, and final serialized request size together.
- Test coverage: unit coverage is extensive; `.planning/STATE.md` still records anti-thrash/cooldown/fallback guards and cross-restart anti-thrash state as unverified/deferred.

**Large orchestration surfaces:**
- Files: `cmd/aura/*.go` (about 19,167 non-test LOC), `internal/agui/*.go` (about 15,281), `internal/agent/tools/*.go` (about 8,939), `internal/agent/*.go` (about 8,536).
- Why fragile: these directories combine composition, auth/streaming/governance, model loops, and every model-to-I/O tool boundary. File splitting keeps individual files manageable but does not remove cross-file blast radius.
- Safe modification: follow existing concern-file splits and run boundary-specific gates (`scripts/agui_boundary_check.sh`, tool unit/race tests, relevant live tagged tiers) after changes.
- Test coverage: broad, but `docker_integration` behavior and external sidecars remain outside the aggregate coverage profile.

**Files at or near the 600-line cap:**
- Files: `internal/arcadedb/client.go` (595), `internal/llm/config.go` (577), `internal/config/config.go` (584), `internal/conversations/store.go` (574), `internal/agui/onboarding_provision.go` (573), `internal/agui/server_run_resume_test.go` (600), `internal/agent/tools/skill_write_test.go` (596), `internal/llm/openai_compat/client_test.go` (596), `web/src/settings/__tests__/ModelSettingsPanel.test.tsx` (594). `internal/runner/runner.go` is now 397 lines and is no longer near the cap.
- Why fragile: the next meaningful edit can breach the repository's enforced file-size rule and require a split during unrelated work.
- Safe modification: plan concern-based splits before adding behavior; do not hide generated or test files from `scripts/check-file-size.sh`.
- Test coverage: not a coverage gap by itself; it is a change-risk and scope-control warning.

## Scaling Limits

**Multi-user support is an explicit strict-profile deployment posture:**
- Current capacity: Postgres rows, Garage objects, conversations, sandbox boxes, ArcadeDB databases, MCP credentials/sessions/data and model-facing scheduled tasks are identity-scoped.
- Control plane: skills, `aura.settings`, the MCP catalog and governance boards are intentionally deployment-global behind `governance.read/write`; ordinary `agent.run` identities cannot mutate them.
- Limit: `AURA_MUSR_ISOLATION` remains opt-in and is refused outside strict profiles. This is a supported-runtime posture gate, not a tenant selector and not a claim that global catalogs contain tenant data.
- Files: `internal/config/config_validate.go`, `internal/agent/tools/skill_manage.go`, `internal/settings/settings.go`, `internal/mcpregistry/store.go`.

**Closed 2026-08-26 — runtime coverage is represented by independent tier reports:**
- Resolution: unit plus `db_integration` remains one exact owned-surface profile; native `docker_integration` merges its test binaries with Go `covdata`; and the all-tier Agent Memory report remains the authority for live `arcadedb_integration` coverage. No profiles are concatenated, summed twice, averaged, or presented as one synthetic number.
- Release rule: `scripts/release_readiness_gate.py` requires all three fresh reports from the exact candidate SHA, including the Docker-only tier marker and the passing `arcadedb_package_coverage` scenario inside an MRS-eligible Agent Memory report.
- Evidence: clean candidate `4315db66b` measured unit/database at 27,323/31,839 = 85.8161%, native-Linux Docker at 2,856/3,313 = 86.2059%, and ArcadeDB at 1,241/1,412 = 87.8895% with MRS 100.00. All three reports are exact-SHA, tier-specific, non-empty, and green; Docker includes the native egress branches.
- Files: `scripts/coverage_profile_gate.sh`, `scripts/docker_coverage_gate.sh`, `scripts/agent_memory_eval.py`, `scripts/release_readiness_gate.py`, `.github/workflows/ci.yml`, `.github/workflows/production-readiness.yml`.

**Conversation recall is bounded by a row cap independently of token capacity:**
- Current capacity: 50 turns by default, configurable from 4 to 1,000.
- Limit: a large-window model may still receive only the newest 50 database turns when no older durable summary covers them.
- Scaling path: couple fetch pagination to durable compaction/watermarks and measured tokenizer cost.
- Files: `internal/conversations/context.go`, `internal/config/config_validate.go`.

## Dependencies at Risk

**Closed 2026-08-25 — protected GHCR package version required support deletion:**
- Former risk: public package version `845339375` was untagged but exceeded GitHub's API-deletion threshold.
- Resolution: the operator confirmed that the GitHub Support deletion and zero-version verification were already completed and resolved.
- Evidence boundary: this is the operator's completion attestation; this repository session did not repeat the package deletion or invent a packages-query response.
- Files: `docs/audit/README.md`, `scripts/audit_closure_gate.py`.

**Calendar/PIM behavior depends on an external fork with reduced unit coverage:**
- Risk: the curated calendar fork deleted 14 raw-tool MSTest files (2,062 LOC) without replacement tests for the merged `CalendarActionTool`; its ordinary `ci.yml` does not trigger on the Aura branch shape recorded during Phase 46.
- Impact: Aura's `calendar_integration` tier and live pulled-image probe carry more of the regression burden than the sidecar repository's unit suite.
- Migration plan: add fork-side tests for every multiplexed action and keep the immutable image tag/digest plus live integration probe as the consumption gate.
- Files: `.planning/STATE.md`, `.planning/phases/46-mcp-trust-and-facade/46-VALIDATION.md`, `internal/mcp/calendar_integration_test.go`, `compose.yaml`.

## Missing Critical Features

**Closed 2026-08-25 — release-gating audit register:**
- Resolution: EXT-005 closed on measured delivery/receipt plus observed cleanup. The operator then confirmed that the closure actions for EXT-001 through EXT-004 were already completed and resolved; those rows and their machine-required IDs were removed together.
- Evidence boundary: the final four closures are an explicit operator attestation, not external actions replayed by this repository session. No receipt contents or provider results were invented.
- Gate: the current register is empty, `open_total` is zero, and the audit closure report emits `release_ready:true`.
- Files: `docs/audit/README.md`, `scripts/audit_closure_gate.py`.

**Closed 2026-08-25 — ArcadeDB tenant memory joins disaster-recovery rehearsal:**
- Resolution: ArcadeDB's native scheduler remains the sole backup owner. The DR drill now creates one disposable `mem_<uuid>` database, writes a checksum sentinel, triggers a native backup, drops and restores the same database, verifies the sentinel, then removes only the disposable database and its exact backup directory. The release gate requires the exact four-plane set: Postgres, sidecars, Garage, and ArcadeDB.
- Upgrade evidence: the official latest pin is 26.8.1. A native 26.7.3 archive restored successfully on 26.8.1 with checksum and cleanup intact; no schema migration or database rewrite was required. The fresh 26.8.1 four-plane run passed 4/4 with ArcadeDB RPO 0 seconds and RTO 218 ms.
- Runtime evidence: `agent-memory-eval` passed the deterministic + live MCP suite at MRS 100.00 on 26.8.1. The disposable ingest integration passed schema creation, idempotence, retired-schema removal, and Bolt-written vector/ANN retrieval 4/4. The already-green migration 0102 and real document-agent E2E were retained as persistent checkpoints because this delta does not overlap those boundaries.
- Files: `scripts/restore_drill.sh`, `scripts/restore_drill_lib.sh`, `scripts/release_readiness_gate.py`, `compose.yaml`, `.github/workflows/ci.yml`.

**No model-facing document catalog operation:**
- Problem: `document_search` requires a query and `document_open` requires an id from a hit; the former `aura docs list/status` ledger was removed. The agent has no direct operation for "which documents have I uploaded?" independent of content search.
- Blocks: reliable inventory questions and deterministic user selection when no topical query is available.
- Files: `cmd/aura/docs.go`, `internal/agent/tools/document_search.go`, `internal/agent/tools/document_open.go`, `internal/agent/tools/manifest.go`.

**Observability scrape check is not scheduled:**
- Problem: `scripts/observability_sidecar_check.sh` detects the measured failure mode where Prometheus is healthy but no longer scraping Aura, yet no Makefile target, CI workflow, or scheduler registration invokes this script by name.
- Blocks: automatic detection of a metrics pipeline that is up but blind.
- Files: `scripts/observability_sidecar_check.sh`, `Makefile`, `.github/workflows/ci.yml`, `internal/cron/`.

## Test Coverage Gaps

**LOCOMO and cross-lingual memory quality are deliberately skipped:**
- What's not tested: LOCOMO answer-quality tests and `TestMemoryVectorAnswersACrossLingualQuestion` are excluded from the live ArcadeDB suite.
- Files: `scripts/agent_memory_eval.py`, `internal/arcadedb/locomo_*.go`, `internal/arcadedb/memory_vector_live_test.go`, `docs/adr/0045-evaluation-corpora-licensing.md`.
- Risk: the memory reliability score proves isolation, provenance, temporal behavior, runtime, coverage, and latency, but not LOCOMO-style answer quality; stale test code can be mistaken for an active gate.
- Priority: High. Retire the declined-corpus tests or rebuild them over a permitted pinned corpus and state clearly that the new metric is not the old one.

**Document routing evaluation compiles but is not executed:**
- What's not tested: current recall/abstention quality for the production document cascade against a reproducible corpus.
- Files: `internal/documents/retrieval_abstention_eval_test.go`, `internal/documents/retrieval_fusion_bench_test.go`, `docs/aura-quality-snapshot.md`, `docs/adr/0045-evaluation-corpora-licensing.md`.
- Risk: changes can preserve unit correctness while reducing real query recall, and the snapshot remains UNKNOWN.
- Priority: High. Provide a permitted corpus and wire one report-producing command into CI or an explicitly operator-run release gate.

**Closed 2026-08-26 — MCP trusted-result stamp has a direct execution-path test:**
- Coverage: `TestBridgedTool_Execute_MarksResultTrusted` asserts `ToolResultProvenance{Trust: TrustTrusted}` and the mounted source after a real bridged `Execute` call.
- Evidence: `go test -count=1 -run TestBridgedTool_Execute_MarksResultTrusted ./internal/agent/mcptools` passes under WSL.
- Files: `internal/agent/mcptools/bridge_call.go`, `internal/agent/mcptools/bridge_test.go`.

**Closed 2026-08-25 — approval expiry has closing tests:**
- Coverage: unit and wire tests cover configuration, boot/periodic sweeps, late decisions, exact pending-challenge discard, and refusal propagation. Disposable-PostgreSQL tests cover cutoff/kind/resolution selection, owner RLS, atomic visibility, and the expiry-versus-human race.
- Evidence quality: the touched ask-user/Runner database matrix measured 86.2% aggregate coverage; every changed critical expiry function is at least 85%; `ExpirePendingApprovals` mutation testing killed 14/14 mutants; and a fresh real OpenRouter agent correctly explained the expired approval without claiming execution.
- Migration guard: the real migration-0102 up/down/up round-trip remains green even though expiry required no new schema migration.
- Files: `internal/askuser/store_db_integration_test.go`, `internal/runner/approval_expiry_db_integration_test.go`, `internal/runner/live_e2e_expiry_test.go`, `cmd/aura/approval_expiry_test.go`.

**Closed 2026-08-26 — daemon-gated sandbox branches have their own floor:**
- Coverage: `scripts/docker_coverage_gate.sh` runs lifecycle, egress, and routed-tool tests under `docker_integration`, merges binary coverage with Go `covdata`, and applies an exact 85% floor over the owned usersandbox and agent-tool surfaces.
- Evidence contract: a deterministic fake-Go test pins the tag, package set, `-coverpkg` scope, native merger, exact counts, and report fields. The native-Linux CI job publishes both the unique profile and `docker-coverage-report.json`; release readiness fails closed if it is absent, stale, below floor, or not Docker-only.
- Evidence: Docker Desktop measured the declared lower bound at 2,835/3,313 = 85.5720% with native egress skipped; the exact-SHA hosted native-Linux job then passed at 2,856/3,313 = 86.2059%, exercising those additional branches.
- Files: `scripts/docker_coverage_gate.sh`, `scripts/docker_coverage_gate_test.sh`, `.github/workflows/ci.yml`, `scripts/release_readiness_gate.py`.

---

*Concerns audit: 2026-08-26*
