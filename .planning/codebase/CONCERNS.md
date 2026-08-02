# Codebase Concerns

**Analysis Date:** 2026-08-02

## Tech Debt

**Retired adaptive-learning plane is only partially removed:**
- Issue: The current worktree deletes `internal/adaptive/`, its sqlc queries, and most composition code, but production deprovisioning still exposes `AdaptiveIdentityFencer` and `AdaptiveGraphPurger`, journals `adaptive_fence` / `adaptive_graph`, and describes a shared adaptive graph that no longer exists.
- Files: `internal/agui/deprovision.go`, `internal/agui/saga_journal.go`, `cmd/aura/serve_provisioning.go`, `internal/db/queries/adaptive_outbox.sql` (deleted), `internal/adaptive/` (deleted)
- Impact: Dead ports, saga labels, tests, and comments obscure the actual erasure order and violate the repository's no-dark-code rule. Future changes can accidentally preserve or revive contracts for a removed subsystem.
- Fix approach: Finish the retirement as one coherent slice: remove the two adaptive ports and saga steps, remove or rewrite tests that assert the retired query/schema contracts, and update deprovisioning comments to the current Postgres + ArcadeDB topology.

**Quality contract references deleted executable evidence:**
- Issue: `internal/eval/` is absent in the current worktree, while the quality snapshot and three operator documents still prescribe commands and files below that directory. The snapshot references missing `internal/eval/harness_swarm_e2e_test.go`, `internal/eval/skills_cot_eval_test.go`, `internal/eval/skills_snippet_reuse_cot_eval_test.go`, and `internal/eval/testdata/adaptive_benchmark_dataset.json`.
- Files: `docs/aura-quality-snapshot.md`, `docs/aura-cot-eval-2026-05-30.md`, `docs/aura-skills-eval-2026-06-05.md`, `docs/aura-swarm-eval-2026-06-04.md`, `internal/eval/` (deleted)
- Impact: Gate documentation can claim an evidence path that cannot be executed. The live CoT, skill, snippet-reuse, and swarm results cannot be refreshed from the current tree.
- Fix approach: Either restore the still-required eval harness under a stable non-planning path or formally retire each affected metric, command, CI-gate glob, and runbook in the same change.

**Measured copy/paste debt remains across runtime and frontend code:**
- Issue: The project `jscpd` workflow reports 129 clone pairs and 2.0% duplication across the scanned production paths. High-value examples include duplicated SSRF policy in MCP/web, duplicated BM25 implementations, repeated AG-UI share handlers, repeated integration runner shell setup, and repeated upload logic.
- Files: `internal/mcp/ssrf.go`, `internal/web/ssrf.go`, `internal/bm25/bm25.go`, `internal/agent/tools/bm25.go`, `internal/agui/share_api.go`, `internal/agui/share_api_internal.go`, `scripts/run_identity_integration.sh`, `scripts/run_conversations_integration.sh`, `scripts/run_runner_integration.sh`, `web/src/documents/documentUpload.ts`, `web/src/chat/attachments/useAttachmentUploads.ts`
- Impact: Security fixes and behavior changes must be applied in multiple places; policy drift is especially risky for SSRF defenses and identity-scoped handlers.
- Fix approach: Start with policy clones: extract one shared SSRF primitive and one BM25 implementation with domain-specific adapters. Then consolidate script environment setup and frontend upload/share helpers. Re-run `npx jscpd@4 --reporters ai --min-lines 10` after each extraction.

**CI workflow is a maintenance hotspot:**
- Issue: `.github/workflows/ci.yml` is 1,329 lines and repeats repository/bootstrap setup across many jobs (23 checkout uses and 19 Go setup uses in the current file). Several comments retain superseded phase narratives; for example, the race DB gate says the main integration job runs without `-race`, while the main command includes `-race`.
- Files: `.github/workflows/ci.yml`, `.github/workflows/skills.yml`, `scripts/tagged_tier_compile.sh`
- Impact: Test-tier intent is hard to audit, updates must be repeated, and stale comments can mislead maintainers about which safety properties are actually gated.
- Fix approach: Move common setup into pinned composite actions or reusable workflows, retain job-local comments only for current invariants, and add a machine check that every discovered live tag is either executed or explicitly classified as compile/manual-only.

## Known Bugs

**Current untagged Go suite is red after subsystem removal:**
- Symptoms: `go test -count=1 ./...` fails in `cmd/aura` and `internal/db`. The DB failures attempt to open the deleted `queries/adaptive_outbox.sql`. The command package reports six onboarding-memory contract failures: the implementation makes nine calls where tests require one atomic profile mutation, omits expected entities, and suppresses an expected status error.
- Files: `internal/db/adaptive_owner_lock_migration_test.go`, `internal/db/adaptive_typed_ledger_migration_test.go`, `internal/db/queries/adaptive_outbox.sql` (deleted), `cmd/aura/memory_onboarding.go`, `cmd/aura/memory_onboarding_test.go`, `cmd/aura/memory_test.go`
- Trigger: Run `go test -count=1 ./...` in WSL against the current worktree.
- Workaround: None suitable for merge. Complete the retirement and reconcile the onboarding implementation with its atomicity/error contract before relying on any broader quality result.

**Identity deprovisioning can fail after irreversible external erasure:**
- Symptoms: `buildDeprovisioner` documents that `aura.audit_logs.actor_identity_id` uses `ON DELETE RESTRICT`. Once a production writer inserts rows, the final identity-row delete fails after conversations and the identity's ArcadeDB memory database have already been purged.
- Files: `cmd/aura/serve_provisioning.go`, `internal/agui/deprovision.go`, `internal/db/migrations/0025_document_control_plane.up.sql`
- Trigger: Insert an `aura.audit_logs` row for an identity, let the grace window expire, then run the deprovision purge saga.
- Workaround: No safe runtime workaround is encoded. Before activating an audit-log writer, define the retention/actor model (nullable tombstone, retained principal, or explicit audit reassignment) and add an integration test that proves the full purge ordering and retry behavior.

## Security Considerations

**Migration 0086 is destructive and intentionally irreversible:**
- Risk: Applying the migration drops twelve tables and all functions with `adaptive` in their name. Its down migration restores nothing, and explicitly states that rolling back farther will fail because migration 0084 expects `aura.adaptive_outbox`.
- Files: `internal/db/migrations/0086_drop_adaptive_learning_plane.up.sql`, `internal/db/migrations/0086_drop_adaptive_learning_plane.down.sql`, `internal/db/migrations/0084_drop_document_retrieval_plane.down.sql`
- Current mitigation: The migration is explicit about the one-way door and requires a pre-0086 `pg_dump` to recover data.
- Recommendations: Make a verified backup and restore drill a release prerequisite, add a migration-0086 integration test that inventories only the intended objects before/after, and make operator tooling refuse the step without an acknowledged backup witness.

**Sandbox enforcement has live CI coverage but no contribution to the coverage floor:**
- Risk: The most security-sensitive Docker behavior (container lifecycle, routed tools, egress DROP enforcement) executes only under `docker_integration`; the owned-surface coverage gate runs `db_integration` only. A branch can be exercised in CI yet remain invisible to the 85% statement-coverage floor.
- Files: `.github/workflows/ci.yml`, `scripts/coverage_gate.sh`, `internal/sandbox/usersandbox/docker_backend_integration_test.go`, `internal/sandbox/usersandbox/egress_integration_test.go`, `internal/agent/tools/shell_exec_sandbox_docker_test.go`
- Current mitigation: The `sandbox-docker-integration` job runs on native Linux, fails loudly under `CI`, and serializes packages sharing Docker.
- Recommendations: Continue requiring daemon-free tests for pure policy/spec/path logic and publish a separate Docker-tier coverage artifact or explicit branch checklist; do not weaken native-Linux egress assertions to make Desktop/WSL pass.

**External delivery and account authorization remain unproven:**
- Risk: Calendar, email, WhatsApp, and native `send_file` cannot be release-certified without authorized accounts/devices and delivery receipts. This is an operational trust gap, not an implementation pass.
- Files: `docs/audit/README.md`, `.github/workflows/ci.yml`, `internal/mcp/calendar_integration_test.go`, `internal/mcp/whatsapp_integration_test.go`, `internal/agent/tools/send_file.go`
- Current mitigation: The audit gate reports `release_ready:false`, integrations fail closed or skip only in explicitly manual tiers, and the unresolved items are assigned to operator/external owners.
- Recommendations: Preserve NO-GO status until reversible send/read/update/delete drills are witnessed and cleaned up; never substitute compile-only or mock evidence for provider receipts.

## Performance Bottlenecks

**Database integration packages serialize on one shared database:**
- Problem: CI and coverage use `-p 1` because more than two package binaries running `EnsureRoles` / migrations against one Postgres can deadlock on the migration advisory lock.
- Files: `.github/workflows/ci.yml`, `.github/workflows/skills.yml`, `scripts/coverage_gate.sh`, `scripts/coverage_docker.sh`, `internal/db/db.go`
- Cause: Package-level integration harnesses share one mutable database and independently run role/migration setup.
- Improvement path: Allocate isolated per-package databases or perform migration once in a job-level fixture, then allow safe package parallelism. Keep destructive tests on disposable databases only.

**Paid snippet reuse exceeds its dispatch-efficiency target:**
- Problem: The latest recorded production-shaped run used 10 tool dispatches against a target of at most 6, although wall-clock and workbook validity passed.
- Files: `docs/aura-quality-snapshot.md`, `internal/agent/tools/skill.go`, `internal/agent/tools/skill_read.go`, `internal/toolinvocations/`
- Cause: The current evidence records extra tool-selection/execution turns; the executable eval that diagnosed the path is now deleted with `internal/eval/`.
- Improvement path: Restore a runnable ledger-based benchmark, capture the exact dispatch sequence, remove avoidable discovery/cwd retries, and re-measure with the production workspace shape.

## Fragile Areas

**Multi-plane identity lifecycle:**
- Files: `internal/agui/deprovision.go`, `cmd/aura/serve_provisioning.go`, `cmd/aura/serve_provisioning_objectstore.go`, `internal/arcadedb/tenant.go`, `internal/conversations/store_identity.go`
- Why fragile: A purge crosses Postgres, Authula, ArcadeDB, object storage, local directories, sessions, jobs, and a best-effort journal. Ordering matters because some deletions are irreversible and a missing memory purger deliberately blocks identity deletion to prevent orphaned databases.
- Safe modification: Preserve fail-closed preflight, add an audit-log-aware deletion design, test every failure boundary and resume point, and verify postconditions in each external plane before marking a saga step done.
- Test coverage: Unit/resume tests exist, and ArcadeDB deprovision runs live in CI, but the audit-log `ON DELETE RESTRICT` landmine has no end-to-end closure test.

**Memory substrate and its environment-dependent quality suite:**
- Files: `internal/arcadedb/memory.go`, `internal/arcadedb/memory_vector.go`, `internal/arcadedb/memory_integration_test.go`, `internal/arcadedb/locomo_test.go`, `.github/workflows/ci.yml`
- Why fragile: Correctness depends on live ArcadeDB schema/query behavior, per-identity credentials, optional embedding/spaCy sidecars, and external LOCOMO data.
- Safe modification: Run the live `arcadedb_integration` tier for schema/query changes, retain tenant-isolation checks, and separately run the corpus/embedding measurement suite when its data and models are available.
- Test coverage: CI explicitly excludes `TestLocomo*` and `TestMemoryVector*`; those families can compile while recall, cross-lingual ranking, and operator-corpus behavior remain unmeasured.

**Files near the repository's 600-line ceiling:**
- Files: `web/src/chat/Composer.tsx` (573 lines), `web/src/chat/ExternalStoreChat.tsx` (572), `internal/channels/telegram/bot_dispatch.go` (544), `internal/mcp/client.go` (538), `internal/agui/onboarding_provision.go` (535), `internal/agent/tools/shell_exec.go` (533), `internal/agent/llm_agent.go` (521), `internal/conversations/context.go` (520)
- Why fragile: These files combine orchestration, policy, serialization, and error paths close to the enforced cap; several also appear in duplication results.
- Safe modification: Split by cohesive responsibility before adding behavior, keep consumer-declared interfaces at boundaries, and preserve focused race/integration tests around each extracted seam.
- Test coverage: Coverage is generally strong, but current untagged failures mean no clean whole-tree baseline exists for a refactor.

## Scaling Limits

**Sandbox capacity is hardware-gated:**
- Current capacity: The soak test is designed for a real 32 GB Linux host; development WSL with approximately 15.47 GiB explicitly skips the D-14 soak.
- Limit: Concurrent boxes, sidecars, volumes, and network namespaces are not continuously proven at appliance capacity on ordinary developer or GitHub-hosted runs.
- Scaling path: Run `internal/sandbox/usersandbox/bench_soak_test.go` on the target appliance class for releases that change sandbox resource accounting, and retain serialized Docker CI as the lower-capacity correctness tier.

**Memory quality is personal-corpus rather than large-corpus certified:**
- Current capacity: The current CI proves live bitemporal fact/schema behavior but excludes LOCOMO and vector measurement families.
- Limit: Recall/latency behavior with larger personal histories, cross-lingual facts, and sidecar embeddings has no current hard CI threshold.
- Scaling path: Restore a versioned, legally distributable benchmark fixture or a reproducible fetch step, pin the embedding model, and publish recall@k plus p95 against stated corpus sizes.

## Dependencies at Risk

**Core protocol/channel dependencies are pre-1.0 or commit-pinned:**
- Risk: AG-UI is consumed at a pseudo-version, Telegram uses `gopkg.in/telebot.v4 v4.0.0-beta.10`, and Markdown conversion uses a commit-derived pseudo-version. API churn can arrive without stable semantic-version guarantees.
- Impact: Agent streaming, channel rendering, and message conversion sit on moving contracts; upgrades can cause compile or wire-format regressions across `internal/agui/` and `internal/channels/telegram/`.
- Migration plan: Keep versions exact, isolate them behind existing adapters, run protocol/channel integration tiers on every bump, and prefer stable releases when compatible.

**Frontend spreadsheet parser is installed from a direct CDN tarball:**
- Risk: `xlsx` is sourced directly rather than by a normal registry version.
- Impact: Dependency automation and provenance review require special handling; a URL change or upstream availability issue can block reproducible installs.
- Migration plan: Preserve the lockfile integrity pin, mirror/attest the artifact for production builds, and document the update procedure in the dependency gate.

## Missing Critical Features

**Release certification is externally blocked:**
- Problem: Five unresolved audit rows remain: authorized Calendar CRUD, authorized email delivery, WhatsApp QR pairing plus media round trips, deletion of a protected untagged GHCR version, and a native `send_file` delivery receipt.
- Blocks: `docs/audit/README.md` declares release readiness NO-GO while any row remains.

**Current paid behavioral certification is incomplete:**
- Problem: The quality snapshot marks the 12-scenario live CoT evaluation invalidated, the scheduler natural-prompt run due, the skill xlsx North-Star not rerun, and snippet reuse red on dispatch count.
- Blocks: Current behavior cannot be claimed at the repository's product Definition-of-Done score using only unit, mock, or deterministic integration evidence.

## Test Coverage Gaps

**Retirement change has no green baseline:**
- What's not tested: The present tree cannot complete untagged unit tests because retired adaptive query contracts and changed memory-onboarding behavior remain inconsistent.
- Files: `internal/db/adaptive_owner_lock_migration_test.go`, `internal/db/adaptive_typed_ledger_migration_test.go`, `cmd/aura/memory_onboarding_test.go`, `cmd/aura/memory_test.go`
- Risk: Additional regressions are masked behind known red packages, and no coverage percentage from this worktree is trustworthy as a closeout metric.
- Priority: High

**New migrations lack focused round-trip/safety tests:**
- What's not tested: No test references migration 0085 or 0086. Migration 0085 changes GIN maintenance behavior; migration 0086 drops an entire subsystem and has a no-op down path.
- Files: `internal/db/migrations/0085_document_digest_gin_fastupdate_off.up.sql`, `internal/db/migrations/0085_document_digest_gin_fastupdate_off.down.sql`, `internal/db/migrations/0086_drop_adaptive_learning_plane.up.sql`, `internal/db/migrations/0086_drop_adaptive_learning_plane.down.sql`, `internal/db/`
- Risk: Ownership/privilege assumptions, object-selection scope, clean-install behavior, upgrade behavior, and the documented rollback boundary can fail only on a live database.
- Priority: High

**Paid and corpus-dependent E2E suites are absent or excluded:**
- What's not tested: Live CoT/tool-use, scheduler natural-language scheduling, skill xlsx self-extension, snippet dispatch efficiency, LOCOMO recall, and vector cross-lingual/operator-corpus behavior are not current automated gates.
- Files: `docs/aura-quality-snapshot.md`, `.github/workflows/ci.yml`, `.github/workflows/skills.yml`, `internal/arcadedb/locomo_test.go`, `internal/arcadedb/memory_vector_live_test.go`, `internal/eval/` (deleted)
- Risk: Tool-selection quality, model-dependent routing, artifact completion, and retrieval relevance can regress while deterministic tests remain green.
- Priority: High

**Docker runtime coverage is separate from the 85% floor:**
- What's not tested: Coverage accounting does not include the `docker_integration` execution paths.
- Files: `scripts/coverage_gate.sh`, `.github/workflows/ci.yml`, `internal/sandbox/usersandbox/`, `internal/agent/tools/`
- Risk: Daemon-only branches can regress without lowering the enforced coverage percentage.
- Priority: Medium

---

*Concerns audit: 2026-08-02*
