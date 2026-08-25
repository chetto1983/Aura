---
last_mapped_commit: 8e7893b6fd8fc4727ae81810a87dd49ce294689b
---

# Codebase Concerns

**Analysis Date:** 2026-08-25

## Severity Summary

| Concern | Severity | Status | Primary evidence |
|---|---|---|---|
| Approval resume accepts decisions outside a per-pause policy and has no expiry | High | Open | `internal/agui/server_project.go`, `internal/runner/runner_resume.go`, `.planning/todos/pending/approval-resume-defects.md` |
| Empty accepted approval answers resumed the model silently | High | Closed 2026-08-25 | `internal/agui/server_run_test.go`, `internal/runner/runner_resume_test.go`, `internal/askuser/store_mutation_test.go` |
| Release disclosure register still has five open rows and therefore reports NO-GO | High | Open/operator or external action | `docs/audit/README.md`, `scripts/audit_closure_gate.py` |
| Amendment #115 still requires a real-production document E2E whose runner no longer exists | High | Open | `prd.md`, absent `scripts/document_pipeline_e2e.sh` |
| ArcadeDB tenant memory has no exercised backup/restore plane | High | Open | `scripts/restore_drill.sh`, `.github/workflows/ci.yml` |
| Shared settings, skills, and MCP catalog constrain safe multi-user operation | Medium | Mitigated by a strict-profile boot gate | `internal/config/config_validate.go`, `internal/db/migrations/0024_settings.up.sql` |
| Coverage is one aggregate at 86.4%; daemon-gated tiers do not feed that floor | Medium | Open/self-documented | `scripts/coverage_gate.sh`, `.github/workflows/ci.yml`, `docs/aura-quality-snapshot.md` |
| Long-history compaction can disable itself silently and uses an unmeasured three-minute timeout | Medium | Open | `internal/conversations/context_budget.go`, `internal/conversations/compaction.go` |
| Calendar MCP admin token has a well-known code fallback not rejected by strict validation | Medium | Open/compose mitigated | `internal/config/config.go`, `cmd/aura/integrations_proxy.go`, `compose.yaml` |
| Opt-in memory preload inserts recalled content into model-visible context with no dedicated poisoning threat model | Medium | Default off | `internal/runner/runner_context.go`, `internal/config/config.go` |
| Test/evaluation evidence contains skipped, stale, flaky, or non-reproducible legs | Medium | Open | `scripts/agent_memory_eval.py`, `docs/aura-quality-snapshot.md`, `.planning/STATE.md` |

## Tech Debt

**Deferred MCP manifest fixture no longer represents the mounted surface:**
- Issue: `internal/agent/tools/testdata/deferred_manifest.json` contains 55 entries, including 14 raw calendar tools and 14 WhatsApp tools. The live calendar surface is curated to one multiplexed tool, while the fixture predates the current WhatsApp surface. `.planning/phases/46-mcp-trust-and-facade/46-09-SUMMARY.md` records the fixture repair as not done.
- Files: `internal/agent/tools/testdata/deferred_manifest.json`, `.planning/phases/46-mcp-trust-and-facade/46-09-SUMMARY.md`, `internal/agent/tools/search_gate_test.go`.
- Impact: retrieval tests measure selection over a corpus that differs from the model's production manifest. A green search gate can therefore miss ranking regressions introduced by the current curated surface.
- Fix approach: generate the fixture from the same mounted/curated manifest builder used in production, or update it atomically whenever curation changes and pin the expected source counts in a test.

**Quality snapshot retains retired or unreproducible claims:**
- Issue: `docs/aura-quality-snapshot.md` still describes Neo4j HNSW labels and migrations after that store was removed, while other rows reference deleted harnesses or manual corpora. `docs/HANDOFF.md` identifies the snippet-reuse, skills North-Star, document recall, and LOCOMO rows as needing retirement or a new reproducible harness.
- Files: `docs/aura-quality-snapshot.md`, `docs/HANDOFF.md`, `docs/adr/0038-graph-store-license-neo4j-gplv3-vs-arcadedb-apache.md`, `docs/adr/0045-evaluation-corpora-licensing.md`.
- Impact: future planning can treat historical numbers or dead commands as current gates. The HNSW section even points at deleted Neo4j paths as witnesses.
- Fix approach: mark historical sections explicitly, retire rows whose capability or corpus is gone, and give every remaining row a command that runs against a permitted fixture.

**Production document E2E contract has no runner:**
- Issue: PRD amendment #115 requires a real-agent, real-production document lifecycle score above 98%, and amendment #118 explicitly retains that gate. The named `scripts/document_pipeline_e2e.sh` does not exist in the current tree.
- Files: `prd.md`, `scripts/ingest_reconcile_e2e.sh`, `scripts/fixtures/document_pipeline_e2e/`, `.github/workflows/production-readiness.yml`.
- Impact: the strongest document-plane acceptance path cannot be executed as specified, so unit/integration success cannot satisfy the repository's own Definition of Done.
- Fix approach: either rebuild the production runner around the current CocoIndex/Tika/ArcadeDB path or amend the PRD after a measured replacement gate proves the same ingest, retrieval, isolation, deletion, and cleanup properties.

**Coverage gate is aggregate, not per-package:**
- Issue: `scripts/coverage_gate.sh` sums every filtered statement and performs one comparison against `AURA_COVERAGE_MIN`. There is no package loop or package minimum.
- Files: `scripts/coverage_gate.sh`, `docs/aura-quality-snapshot.md`, `CLAUDE.md`.
- Impact: a weak package can regress substantially while stronger packages keep the repository aggregate above 85%. The latest recorded owned-surface result is 26,820/31,045 = 86.4%, only 1.4 percentage points above the floor.
- Fix approach: keep the aggregate gate but publish package deltas for touched packages, or introduce tag-aware per-package floors only after every package's real coverage tier can feed the calculation.

**Manual OOXML parser is a broad custom maintenance surface:**
- Issue: `internal/documents/filecard` manually parses ZIP/XML workbook and Office structures across `xlsx.go`, `ooxml.go`, `zip.go`, and `table.go` instead of using a workbook library. The implementation is split under the 600-line cap and well tested, but remains a large protocol parser owned by Aura.
- Files: `internal/documents/filecard/xlsx.go`, `internal/documents/filecard/ooxml.go`, `internal/documents/filecard/zip.go`, `internal/documents/filecard/table.go`, `internal/documents/filecard/card_test.go`.
- Impact: uncommon OOXML variants, relationships, cell encodings, and workbook features become Aura-specific compatibility work.
- Fix approach: inventory the installed/version-pinned library surface before replacing anything; either document measured reasons for retaining the parser or migrate behind the existing `filecard.Build` boundary with corpus parity tests.

**Legacy staged user files sit outside the active sweeper:**
- Issue: the current audit found two `aura-sendfile-*` directories under the run root from a removed code path. The active sweeper only owns `$AURA_RUN_DIR/tmp/`, and the legacy prefix no longer exists in source.
- Files: `docs/audit/README.md`, `internal/conversations/orphan_scan.go`, `internal/agent/tools/send_file.go`.
- Impact: user content from the retired path remains indefinitely unless an operator removes it; this is retention debt, not a live recurring leak.
- Fix approach: perform a one-time operator-reviewed cleanup of the exact recorded directories and add a migration/sweep rule only if more legacy prefixes are discovered.

## Known Bugs

**Closed 2026-08-25 — empty accepted approval answers resumed the model silently:**
- Resolution: the AG-UI boundary returns HTTP 400 for missing, empty, or whitespace-only resolved payloads before calling `SubmitAnswers`; Runner validates the effective answer used for both persistence and the `RoleTool` turn; and `askuser.Store` enforces the same invariant inside the transaction front door.
- Compatibility: decline/cancel still permits empty caller content, and scheduled approvals remain valid because Runner validates their server-authored outcome rather than the caller's empty placeholder.
- Files: `internal/agui/server_project.go`, `internal/runner/runner_resume.go`, `internal/askuser/store.go`, `.planning/todos/pending/approval-resume-defects.md`.
- Evidence: wire, Runner, Store, and rollback regressions are green; the full disposable-Postgres `db_integration` matrix measured 26827/31139 = 86.2% owned coverage; `ValidateResumeAnswer` mutation score is 4/5 = 80% killed.

**Pending approvals do not expire:**
- Symptoms: a pending pause remains actionable until it is resumed, auto-resolved, or removed with its conversation. No approval-specific TTL or expiry transition exists.
- Files: `internal/gateway/approvals.go`, `internal/askuser/store.go`, `internal/runner/runner_resume.go`, `.planning/todos/pending/approval-resume-defects.md`.
- Trigger: leave a gateway/ask-user pause unanswered indefinitely.
- Workaround: cancel/delete the conversation or explicitly resolve the pause; there is no scheduled expiry.

**Runner verification test is flaky under the full parallel gate:**
- Symptoms: `TestVerifyOnStopFiresOnARealTurn` is recorded failing in the full parallel gate while passing repeatedly in isolation.
- Files: `internal/runner/runner_verification_test.go`, `docs/aura-quality-snapshot.md`.
- Trigger: run the repository-wide parallel test/coverage workload; the exact scheduler interaction is not yet isolated.
- Workaround: rerun the focused test to distinguish the known flake from a deterministic regression, but do not treat the rerun as a fix.

## Security Considerations

**Resume authorization lacks per-pause decision policy:**
- Risk: `resumeAnswers` maps any resolved entry to accept, and `SubmitAnswer`/`SubmitAnswers` act on the supplied action. A pause cannot express "decline/respond only," so a crafted authenticated POST can approve any pause whose token the caller owns.
- Files: `internal/agui/server_project.go`, `internal/runner/runner_resume.go`, `internal/askuser/types.go`, `.planning/todos/pending/approval-resume-defects.md`.
- Current mitigation: the route is authenticated and owner-scoped, the token is required, and transactional `resumed_at IS NULL` claims prevent duplicate resumes. No exploit has been demonstrated.
- Recommendations: persist allowed decisions with each pause and validate them at the transaction's front door, preserving `CommitResumeBatch` atomicity and sorted-token locking. Non-empty accepted content is already enforced there.

**Deployment-wide settings can contain secrets without identity scope or RLS:**
- Risk: `aura.settings` is keyed only by `key`, grants full DML to `aura_app`, stores secret values in plaintext, and includes `OPENROUTER_API_KEY` and `TELEGRAM_BOT_TOKEN` in its allowlist.
- Files: `internal/db/migrations/0024_settings.up.sql`, `internal/settings/settings.go`, `internal/agui/settings_api.go`, `internal/config/config_validate.go`.
- Current mitigation: API responses redact secret values; writes require `governance.write`; `gateMultiUserNeedsASandbox` refuses the multi-user flag on a non-strict profile and explicitly names this shared plane.
- Recommendations: keep the single-user/strict-profile gate fail-closed. Before broad multi-user support, choose intentionally between identity-scoped encrypted settings and an explicitly operator-global settings plane with capability rules that prevent tenant writes.

**Calendar MCP admin token has a well-known fallback:**
- Risk: both configuration paths fall back to `changeme-aura-pim-local`, and strict-profile validation does not reject it.
- Files: `internal/config/config.go`, `internal/config/config_validate.go`, `cmd/aura/integrations_proxy.go`, `compose.yaml`.
- Current mitigation: the Compose deployment requires `AURA_PIM_MCP_ADMIN_TOKEN` and refuses to render without it; the sidecar admin API is internal to the Compose network and the proxy is capability-gated.
- Recommendations: add strict-profile validation that rejects the well-known value and remove the duplicate literal so bare-binary deployments receive the same defense as Compose.

**Opt-in recalled memory is a prompt-injection and memory-poisoning seam:**
- Risk: when enabled, current-message retrieval is inserted into the model-visible prompt under text describing it as the model's "own knowledge." Stored facts can originate from earlier model/tool/document activity, so poisoned memory can influence a later turn before explicit tool selection.
- Files: `internal/runner/runner_context.go`, `internal/runner/runner_memory_context_test.go`, `internal/config/config.go`, `cmd/aura/chat_boot.go`.
- Current mitigation: `AURA_MEMORY_PRELOAD_ENABLED` defaults to false, recall is identity-scoped, and retrieval failures fail soft.
- Recommendations: write a dedicated trust-boundary threat model before enabling it by default; distinguish data from instructions in framing and add adversarial memory-poisoning tests.

**Mounted MCP output is deliberately stamped trusted without a focused regression test:**
- Risk: `newResult` marks every mounted server result `TrustTrusted`. This is a ratified operator-infrastructure posture, but a future refactor could drop or invert the stamp without a test failing at the exact seam.
- Files: `internal/agent/mcptools/bridge_call.go`, `internal/agent/mcptools/bridge_trust_test.go`, `.planning/phases/46-mcp-trust-and-facade/46-09-SUMMARY.md`.
- Current mitigation: description tests indirectly pin the no-distrust-framing behavior, result size caps still apply, and mounts are operator-configured.
- Recommendations: add a focused `newResult` test asserting `TrustTrusted` and keep it separate from description-cap tests.

## Performance Bottlenecks

**Database history fetch still defaults to 50 turns before token budgeting:**
- Problem: history loading fetches at most `AURA_HISTORY_HARD_CAP_TURNS` rows, default 50, before the token-aware ladder and durable compaction run.
- Files: `internal/conversations/context.go`, `internal/config/config.go`, `internal/config/config_knobs.go`.
- Cause: the query/sidecar/tokenizer safety cap is a row count independent of the selected model's context window. Older rows that were never compacted are unavailable to the ladder.
- Improvement path: measure query/tokenizer cost at higher caps, then derive or advance the fetch window from durable compaction state and the model budget rather than relying on a fixed default.

**Compaction can silently fall through after a long summarizer wait:**
- Problem: a summary call can occupy a turn for up to three minutes; timeout or summarizer failure is intentionally silent and falls through to deterministic history dropping.
- Files: `internal/conversations/compaction.go`, `internal/conversations/context.go`, `internal/conversations/context_budget.go`.
- Cause: `compactionTimeout` is a hard-coded 3 minutes, and `earlyCompactionTokens` returns zero when fixed prompt overhead consumes the configured percentage.
- Improvement path: measure long-history summary latency on the deployed model, expose or derive a bounded timeout, reject impossible trigger settings visibly, and emit an operator-visible degradation event when fallback drops history.

**Document retrieval quality has no current reproducible baseline:**
- Problem: the document routing recall row is recorded UNKNOWN, while its former corpus is not repository-owned and the replacement non-commercial corpus was declined.
- Files: `docs/aura-quality-snapshot.md`, `docs/HANDOFF.md`, `docs/adr/0045-evaluation-corpora-licensing.md`, `internal/documents/retrieval_abstention_eval_test.go`.
- Cause: the harness files compile, but no CI path executes them against a permitted, pinned reference corpus.
- Improvement path: define a permissively licensed synthetic/reference corpus, pin expected queries and locators, and run it after ingestion settings change.

## Fragile Areas

**Approval resume transaction boundary:**
- Files: `internal/agui/server_project.go`, `internal/runner/runner_resume.go`, `internal/runner/resume_committer.go`, `internal/askuser/store.go`.
- Why fragile: wire mapping, owner scoping, pause claims, answer-turn persistence, hooks, and idempotency span several packages. Validation added outside the cross-store transaction can recreate claimed-without-answer or double-resume failures.
- Safe modification: validate decision policy and expiry before claims inside the existing committer boundary; retain the now-shared accepted-content validation, sorted-token batch locking, and the `resumed_at IS NULL` conditional update.
- Test coverage: atomic single/batch resume tests exist under `db_integration`; empty accepted content is closed by wire, Runner, Store, and rollback tests. Decision policy and expiry still have no closing tests.

**Context ladder and durable compaction:**
- Files: `internal/conversations/context.go`, `internal/conversations/context_budget.go`, `internal/conversations/compaction.go`, `internal/conversations/compaction_durable_test.go`, `internal/conversations/compaction_inforce_test.go`.
- Why fragile: system/always blocks, active rounds, tool-call/result pairing, durable watermarks, cache-prefix stability, transient context, and final request reserves interact in one path.
- Safe modification: preserve complete user-led rounds and assistant-tool/result adjacency; test stored-compaction replay, branch watermarks, impossible thresholds, provider reserves, and final serialized request size together.
- Test coverage: unit coverage is extensive; `.planning/STATE.md` still records anti-thrash/cooldown/fallback guards and cross-restart anti-thrash state as unverified/deferred.

**Large orchestration surfaces:**
- Files: `cmd/aura/*.go` (about 19,034 non-test LOC), `internal/agui/*.go` (about 15,143), `internal/agent/tools/*.go` (about 8,928), `internal/agent/*.go` (about 8,373).
- Why fragile: these directories combine composition, auth/streaming/governance, model loops, and every model-to-I/O tool boundary. File splitting keeps individual files manageable but does not remove cross-file blast radius.
- Safe modification: follow existing concern-file splits and run boundary-specific gates (`scripts/agui_boundary_check.sh`, tool unit/race tests, relevant live tagged tiers) after changes.
- Test coverage: broad, but `docker_integration` behavior and external sidecars remain outside the aggregate coverage profile.

**Files at or near the 600-line cap:**
- Files: `internal/arcadedb/client.go` (595), `internal/runner/runner.go` (577), `internal/llm/config.go` (577), `internal/config/config.go` (577), `internal/conversations/store.go` (574), `internal/agui/onboarding_provision.go` (573), `internal/agui/server_run_resume_test.go` (600), `internal/agent/tools/skill_write_test.go` (596), `internal/llm/openai_compat/client_test.go` (596), `web/src/settings/__tests__/ModelSettingsPanel.test.tsx` (594).
- Why fragile: the next meaningful edit can breach the repository's enforced file-size rule and require a split during unrelated work.
- Safe modification: plan concern-based splits before adding behavior; do not hide generated or test files from `scripts/check-file-size.sh`.
- Test coverage: not a coverage gap by itself; it is a change-risk and scope-control warning.

## Scaling Limits

**Multi-user support depends on deployment profile because three planes remain global:**
- Current capacity: per-identity Postgres rows, Garage objects, conversations, sandbox boxes, and ArcadeDB databases are isolated, but skills roots, `aura.settings`, and the MCP catalog remain deployment-wide.
- Limit: `AURA_MUSR_ISOLATION` is refused outside strict profiles; loosening that gate would expose cross-identity shared configuration and content.
- Scaling path: either identity-scope the three shared planes or formalize them as operator-global resources with read/write capabilities that tenants cannot acquire.
- Files: `internal/config/config_validate.go`, `internal/settings/settings.go`, `internal/skills/identity_root.go`, `internal/mcpregistry/store.go`.

**Coverage cannot represent all runtime tiers in one number:**
- Current capacity: the default floor runs only `db_integration`; the latest recorded aggregate is 86.4%.
- Limit: `docker_integration` runs in its own CI job and contributes zero coverage; `arcadedb_integration` produces its own package profile in the memory evaluator but is not aggregated into the owned-surface floor.
- Scaling path: keep daemon-free unit tests for pure logic, publish tier-specific coverage reports, and combine them only with an explicit rule for duplicate statements and required live services.
- Files: `scripts/coverage_gate.sh`, `scripts/agent_memory_eval.py`, `.github/workflows/ci.yml`, `CLAUDE.md`.

**Conversation recall is bounded by a row cap independently of token capacity:**
- Current capacity: 50 turns by default, configurable from 4 to 1,000.
- Limit: a large-window model may still receive only the newest 50 database turns when no older durable summary covers them.
- Scaling path: couple fetch pagination to durable compaction/watermarks and measured tokenizer cost.
- Files: `internal/conversations/context.go`, `internal/config/config_validate.go`.

## Dependencies at Risk

**Protected GHCR package version cannot be deleted through the API:**
- Risk: public package version `845339375` is untagged but exceeds GitHub's API-deletion threshold.
- Impact: an orphaned historical image remains publicly stored and cannot be retired solely by repository changes.
- Migration plan: GitHub Support must delete it; verify zero versions through an authenticated packages query afterward.
- Files: `docs/audit/README.md`, `scripts/audit_closure_gate.py`.

**Calendar/PIM behavior depends on an external fork with reduced unit coverage:**
- Risk: the curated calendar fork deleted 14 raw-tool MSTest files (2,062 LOC) without replacement tests for the merged `CalendarActionTool`; its ordinary `ci.yml` does not trigger on the Aura branch shape recorded during Phase 46.
- Impact: Aura's `calendar_integration` tier and live pulled-image probe carry more of the regression burden than the sidecar repository's unit suite.
- Migration plan: add fork-side tests for every multiplexed action and keep the immutable image tag/digest plus live integration probe as the consumption gate.
- Files: `.planning/STATE.md`, `.planning/phases/46-mcp-trust-and-facade/46-VALIDATION.md`, `internal/mcp/calendar_integration_test.go`, `compose.yaml`.

## Missing Critical Features

**Five release-gating audit rows remain open:**
- Problem: release readiness is explicitly NO-GO while `EXT-001` through `EXT-005` remain. Calendar needs an operator-authorized reversible CRUD; email needs a real send receipt; WhatsApp needs a fresh approved outbound flow; GHCR deletion needs GitHub Support; artifact delivery still needs cleanup observed after its TTL.
- Blocks: the audit closure gate and release publication. These actions require operator/external authority and must not be performed autonomously.
- Files: `docs/audit/README.md`, `scripts/audit_closure_gate.py`.

**ArcadeDB tenant memory is absent from disaster-recovery rehearsal:**
- Problem: the DR script explicitly drills only Postgres, run sidecars, and Garage. It states that per-identity ArcadeDB databases are not dumped and must be snapshotted out of band.
- Blocks: a tested restore path for long-term memory after volume loss. The CI step is labelled "Four-plane" even though the generated report requires only three planes.
- Files: `scripts/restore_drill.sh`, `.github/workflows/ci.yml`, `internal/arcadedb/tenant.go`.

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

**MCP trusted-result stamp lacks a direct test:**
- What's not tested: `bridgedTool.newResult` always produces `ToolResultProvenance{Trust: TrustTrusted}` for mounted infrastructure.
- Files: `internal/agent/mcptools/bridge_call.go`, `internal/agent/mcptools/bridge_trust_test.go`.
- Risk: a refactor can silently change prompt framing/trust semantics without failing the current description-cap tests.
- Priority: Medium. Add one focused unit test at the result seam.

**Approval policy and expiry regressions have no closing tests:**
- What's not tested: allowed-decision enforcement and deterministic expiry of pending approvals while retaining batch atomicity. Empty accepted payload rejection now has closing tests at the wire, Runner, Store, and database-transaction boundaries.
- Files: `.planning/todos/pending/approval-resume-defects.md`, `internal/agui/server_project.go`, `internal/runner/runner_resume.go`, `internal/gateway/approvals_test.go`.
- Risk: authorization-granularity and stale-approval bugs remain open and can survive broader happy-path resume coverage.
- Priority: High. Add wire-level and `db_integration` tests with the implementation, not as test-only expectation changes.

**Daemon-gated sandbox branches do not contribute to the coverage floor:**
- What's not tested by the coverage metric: Docker lifecycle, egress enforcement, and routed tool branches under `docker_integration`.
- Files: `internal/sandbox/usersandbox/*_integration_test.go`, `internal/agent/tools/*_docker_test.go`, `.github/workflows/ci.yml`, `scripts/coverage_gate.sh`.
- Risk: the live job can fail behaviorally, but aggregate coverage does not reveal newly uncovered pure branches in these files.
- Priority: Medium. Continue pairing every daemon branch with daemon-free unit tests and publish a separate Docker-tier profile if feasible.

---

*Concerns audit: 2026-08-25*
