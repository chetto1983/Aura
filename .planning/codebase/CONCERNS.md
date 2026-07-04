# Codebase Concerns

**Analysis Date:** 2026-07-04

**Sources of record:** This document synthesizes and re-verifies against current code:
- `docs/audit/` — 2026-06-21 industrial security/production-readiness audit (51 findings, `F-001`..`F-052`, score 4.6/10)
- `docs/audit/quality/` — 2026-06-29 maintainability/architecture audit (~64 findings, `QA-A/B/C/D-*`)
- `.planning/REQUIREMENTS.md` and `.planning/ROADMAP.md` — the v2.0.0 "Industrial Hardening & Multi-User Production" milestone that tracks every finding to a phase (31–41)

**Milestone status as of this analysis:** Phases 31–35 are **complete** (stabilization, quality cleanup, runtime profiles, agent-loop correctness/durable ledger, ToolGateway policy engine — the last closed today, 2026-07-04, including gap-closure `35-07` for code-review Critical `CR-01`). Phases 36–41 are **not started**. The concerns below are organized so that anything already closed by Phase 31–35 is marked `[RESOLVED]` and anything still open cites its target phase — do not re-open or re-fix `[RESOLVED]` items without checking `git log` first, and do not duplicate work already scoped into Phase 36–41 without checking `.planning/phases/`.

---

## Tech Debt

**Full-host tool authority is the core unresolved architectural gap (Phase 37 — not started):**
- Issue: `shell_exec` and filesystem tools (`fs_read`, `fs_write`, `fs_edit`, `fs_grep`, `fs_glob`) execute with the operator's full host privileges. There is no per-identity capability boundary — the `ToolGateway` shipped in Phase 35 centralizes *policy decisions* (approve/deny/reserve) but does not sandbox the tool's actual execution environment.
- Files: `internal/agent/tools/shell_exec.go` (596 LOC, near the 600-LOC cap), `internal/agent/tools/fs.go`, `internal/gateway/` (the Phase-35 policy engine)
- Impact: prompt injection, a compromised upstream document, or a model error can cause host-wide reads/writes/deletes/process launches under `dev`/`local_trusted`. This is an accepted, documented product decision for the single-trusted-operator profile (see `.planning/ROADMAP.md` "Locked decision (a): capability is contained (per-user sandbox), never removed" — the terminal is never stripped) but is an open gap for `server_production`.
- Fix approach: Phase 37 (`SBX-01..05`) — per-identity full-capability Docker sandbox routed by `SandboxRouter.Resolve(identityctx)`, `--network none` default egress, Docker-socket/`--privileged`/`--network host`/bind-mounts unrepresentable in config.

**Copy-pasted helpers across packages — mostly resolved, watch for regressions:**
- Issue: the 2026-06-29 quality audit found ~10 duplicated helpers (store helpers `hashText`/`asString`/`asFloats` ×3, `GraphClient` ×2, env-var helpers ×3, agent `canonArgs`/`canonicalArgs`, `isTransientToolErr`/`retryableStreamOpenError`, web `getJSON` ×3, focus-trap). **[RESOLVED — Phase 32]**: verified current code — `internal/canonicaljson.CanonicalArgs` is the single call site from both `internal/agent/workflow/loop.go` and the LLM-agent dispatch path; `internal/neostore`, `internal/envutil`, `internal/agentrender` packages now exist as the extraction targets.
- Residual risk: this is a "drift hazard" pattern (a fix in one copy doesn't reach siblings) — when adding new store/env helpers, check `internal/neostore` and `internal/envutil` first before writing a new one; do not reintroduce a fourth `decode*Body` variant (see Security Considerations, `F-052`) or a second MCP trust-normalization path (`F-027`, Phase 38).

**Uncatalogued `AURA_*` env knobs read ad-hoc in hot paths — partially resolved:**
- Issue: several `AURA_LOOP_MAX_PARALLEL_TOOLS`, `AURA_FS_*`, `AURA_SHELL_*` knobs were read via bare `os.Getenv` at call time instead of through `internal/config`, making them invisible to `aura config validate` and undiscoverable ops tuning.
- Files: `internal/config/config.go` (now has a `KnobSpec` registry — Phase 33), `internal/agent/tools/*` (call sites)
- Status: Phase 33 shipped the `KnobSpec` registry (single source of truth, Tier A+B knobs) and `aura config validate [--profile] [--json]`. Confirm any *new* `AURA_*` knob added after 2026-06-29 is registered in the `KnobSpec` table, not read ad-hoc — this is a recurring pattern that will regress if not enforced at review time.

**Two files sit near the 600-LOC no-god-class cap** (not violations, but the next touch should watch the ceiling):
- `internal/agui/server.go` — 598 LOC
- `internal/agent/tools/shell_exec.go` — 596 LOC
- `internal/db/sqlc/document_control_plane.sql.go` (1037 LOC) and `internal/db/sqlc/assets.sql.go` (722 LOC) are sqlc-generated and excluded from the cap/coverage floor per `CLAUDE.md`.
- Fix approach: on next edit to either near-cap file, split by concern (`<name>_<concern>.go`) before adding new logic — do not wait for the pre-commit `file-size` hook to turn red (it blocked all commits once already, in Phase 31, when `serve_webui.go` hit 628 LOC).

---

## Known Bugs (historical — now fixed; listed for regression awareness)

The following were confirmed **[RESOLVED]** via commit history (`F-002`, `F-006`, `F-010`, `F-031`, `F-035`, `F-038`, `F-041` closed in the "Audit Tier A remediation" PR and Phases 33–35). They are listed so future work does not reintroduce the same footgun:

- **`.env.example` disabling destructive-shell approval** (`F-002`): `AURA_SHELL_DESTRUCTIVE_PATTERNS=` used to mean "gate disabled" when copied verbatim from the sample. Fixed in Phase 33 (`33-02`): unset OR empty now means "use defaults"; only an explicit case-insensitive `off` disables the gate. File: `internal/agent/tools/shell_exec_env.go` (`destructiveShellPatterns`). **Do not** reintroduce empty-means-disabled semantics on any similar gate.
- **Command hooks failing open** (`F-006`): default policy is now fail-closed. File: `internal/agent/hooks_command.go` (507 LOC, also near-cap — watch on next touch).
- **`fs_write` non-atomic writes** (`F-010`): now temp-file + rename.
- **Mutating flag lost across tool-panic recovery** (`F-031`): panic recovery now preserves the `Mutating` classification — relevant to `internal/agent/llm_agent_parallel.go`.
- **MCP HTTP transport `Close` could hang** (`F-035`): now bounded with a timeout.
- **MCP trust endpoint accepted empty body as `trusted_local`** (`F-038`): now requires explicit class + non-empty reason.
- **Relative `AURA_RUN_DIR`** (`F-041`): now normalized to absolute at config load.
- **Terminal `text_response` racing mutating siblings** (`F-003`) and **HITL resume/pause non-atomicity** (`F-004`/`F-029`): closed in Phase 34 (`ResumeCommitter`, single cross-store transaction, `os.Root` sidecar fence).
- **No policy decision recorded before tool execution** (`F-001` gateway half, `F-011`, `F-020`): closed in Phase 35 — every tool call now passes through `internal/gateway`'s `Decide` PEP with a durable reservation; a resume-path confused-deputy bug (code-review `CR-01`, approval could be granted without matching the operator-visible question) was found and closed same-day in gap-closure plan `35-07`.

**Currently open, tracked to a not-yet-started phase — do not treat as "someone else's problem" if touching adjacent code:**

- **Multi-user data leakage across identities** (`F-028`, Phase 36 `MUSR-01/02`, **OPEN**): conversation and approval APIs list/mutate largely unscoped stores rather than filtering by the authenticated principal; new Web conversations are created under the seeded `local` identity rather than `identityctx.IdentityID(ctx)`. Files: `internal/agui/conversations_api.go`, `internal/agui/approvals_api.go`, `internal/runner/runner_conversation.go` (`NewConversation`, `NewConversationWithID`).
- **Background shell jobs are predictable and unscoped** (`F-032`, `F-012`, Phase 36 `MUSR-03/04`, **OPEN**): sequential shell IDs, no TTL, no owner/session binding — one session can currently poll/kill another session's background job. Files: `internal/agent/tools/shell_bg.go` (499 LOC).
- **Conversation delete doesn't evict all session tool state** (`F-039`, Phase 36 `MUSR-05`, **OPEN**).
- **Mixed `url`+`command` MCP entries can bypass local-command trust blocking** (`F-027`, Phase 38 `MCPH-01`, **OPEN**, also the quality-audit `QA-C-03` trust-norm duplication — do not merge casually, security-relevant): trust classification treats any non-empty URL as remote-HTTP trust while type normalization can still open a `url`+`command` entry as stdio. Files: `internal/mcp/manager/runtime.go` (`normalizedTrustForServer`, `RunnableManagedServers`), `internal/mcp/managed_config.go` (`normalizedServerType`), `internal/agent/mcptools/mount.go` (`MountManagedServer`), `internal/mcp/transport.go` (`OpenServer`).
- **Docker MCP network allowlist is advisory, not enforced** (`F-036`, Phase 37 `SBX-04`, **OPEN**): a configured egress allowlist is not actually firewalled/proxied.
- **CI/release uses mutable action/tool references** (`F-051`, Phase 40 `SEC-05`, **OPEN**): no SBOM published yet (`syft`/`cyclonedx-gomod` absent from `.github/workflows/`); confirmed `govulncheck` **is** wired into `ci.yml`/`codeql.yml` already, but Actions pinning-to-SHA and SBOM publication are still missing.
- **Privileged JSON routes accept trailing/unknown-field bodies** (`F-052`, Phase 40 `SEC-06`, **OPEN**, also `QA-C-01` — 4 duplicated `decode*Body` helpers, unify during the same phase): `/agent/run`, approvals-resolve, onboarding, assets, governance-write routes lack strict decoding (size cap, content-type check, `DisallowUnknownFields`, single-decode EOF check).

---

## Security Considerations

**Trust model mismatch for shared/remote deployment (the audit's headline finding, `F-001`):**
- Risk: `shell_exec` and filesystem tools intentionally run with full host authority and no workspace boundary. This is a deliberate, documented product decision for the single-trusted-operator use case (`dev`/`local_trusted` profiles), not an oversight — but it is the primary blocker for `server_production`.
- Files: `internal/agent/tools/shell_exec.go`, `internal/agent/tools/fs.go`
- Current mitigation: Phase 33 shipped 4 runtime profiles (`dev`/`local_trusted`/`single_user_hardened`/`server_production`) with `aura config validate --profile server_production` failing fast on unsafe defaults; Phase 35's `ToolGateway` adds a policy-decision + durable-reservation layer that is a no-op (fail-open, host-direct) under `dev`/`local_trusted` but enforces fail-closed for mutating tools under hardened/production profiles.
- Recommendation (Phase 37, not started): per-identity full-capability Docker sandbox so the agent still experiences a full host but the real host is never exposed; `--network none` default egress with enforced (not advisory) allowlisting.

**CodeQL open alert — weak sensitive-data hashing (`SEC-09`, Phase 40, OPEN):**
- Risk: `internal/agui/recovery_hash.go` uses a hash for identity-recovery material that CodeQL flags as `go/weak-sensitive-data-hashing` (not a cryptographically strong salted KDF).
- File: `internal/agui/recovery_hash.go`
- Recommendation: replace with Argon2id/bcrypt per the `golang-security` skill's "MD5/SHA1 for passwords" anti-pattern guidance; this is the one CodeQL-surfaced finding not already in the `F-001..F-052` set.

**Permissive/wildcard CORS not yet profile-gated (`F-022`, Phase 40 `SEC-02`, OPEN):**
- Risk: permissive CORS should be refused when auth is disabled outside an explicit dev profile; not yet enforced.

**Integration validation console can bind non-loopback (`F-047`, Phase 40 `SEC-03`, OPEN).**

**Reasoning-trace / tool-output secret redaction not yet first-class (`F-021`, Phase 40 `SEC-01`, OPEN):**
- Risk: full reasoning-trace mode has no production warning/fail-fast, retention config, or optional encrypted sink; secret-like values are not yet guaranteed redacted before persistence.
- Relates: `internal/reasoningtrace/`, `internal/reasoningstore/`.

**MCP lifecycle hardening incomplete (`F-013/033/034/037/046`, Phase 38, OPEN):**
- No bounded per-server mount timeout (a hung MCP helper can wedge registry construction); stdio frames are not capped in size; CLI MCP mutations (`add`/`trust`/`enable`/`disable`/`remove`) do not all route through the audited atomic writer (`mcp_audit`); a dead/typoed HTTP MCP endpoint can report healthy-by-config rather than probing.
- Files: `internal/mcp/manager/`, `internal/agent/mcptools/mount.go`, `cmd/aura/mcp.go` (503 LOC).

**Object-store single-replica topology (`F-019` ops part, Phase 41 `OPS-01`, OPEN):**
- `AURA_OBJECTSTORE_REPLICATION_FACTOR` defaults to `1` (`internal/config/config.go:136,416`) — an explicit, declared-intent default (Phase 33 `D-13`/`PROF-06`), not silently unsafe, but production topology validation and documentation is still open.

**No prompt-injection / tool-policy-bypass regression suite yet (`F-019` security part, Phase 40 `SEC-04`, OPEN):**
- There is no automated suite asserting injected shell/file/network/MCP requests are denied under `server_production`. The `ToolGateway` (Phase 35) provides the enforcement point; the adversarial regression coverage against it is not yet built.

---

## Performance Bottlenecks

**Sandbox-per-identity is an explicitly accepted latency/throughput trade-off, not yet built:**
- The `.planning/ROADMAP.md` "won't do" table documents that gVisor/Kata/microVM defaults cost "+10–125% on IO-heavy shell/build work" and are deliberately opt-in (`server_production` runtime only), and that sandbox warm pools were rejected as "trade idle compute for latency — counter-productive on one 16-core box." When Phase 37 lands, benchmark shell/fs-heavy workloads under the sandboxed path before assuming parity with today's host-direct path.

**`AURA_*` knobs read via `os.Getenv` at call time (residual from the quality audit):**
- Minor per-call `os.Getenv` cost in hot paths (tool dispatch); low-impact but the `KnobSpec` registry (Phase 33) is the fix — confirm any new hot-path knob is added there, not re-introduced as a bare `os.Getenv`.

**Discarded `Build()` call when `adaptiveTierOK`** — noted as a quick-win in the quality audit (`internal/agent/llm_agent.go:235`), saves one `RenderToolDefs()` per turn. Verify current status before re-flagging; this was a Wave-1 "quick win" candidate in the 2026-06-29 audit and may already be folded into Phase 32's cleanup — check `internal/agent/llm_agent.go` before re-filing.

---

## Fragile Areas

**MCP transport/trust classification (`F-027`/`QA-C-03`, Phase 38, OPEN):**
- Files: `internal/mcp/manager/runtime.go`, `internal/mcp/managed_config.go`, `internal/agent/mcptools/mount.go`, `internal/mcp/transport.go`
- Why fragile: trust normalization and transport-type normalization are two independent code paths that can disagree on the same config entry (`url`+`command` both present). A change to one classifier without the other silently reopens `F-027`.
- Safe modification: per the quality audit, do NOT merge/simplify this casually — any change needs full trust tests verifying every call site's inference matches, and should land inside Phase 38 with the explicit `MCPH-01` canonical-classifier requirement, not as an incidental refactor.

**`bootChatEnvWithConfig` double-`Validate` / potential pool leak (`QA-A-03`, tracked as `QUAL-04`, marked [RESOLVED] in REQUIREMENTS.md — re-verify before assuming closed):**
- File: boot path in `cmd/aura/` (config load + pool open sequence)
- The 2026-06-29 audit flagged this as "risky/uncertain — verify before acting" because the failing-overlay-after-pool-open repro was missing at audit time. `.planning/REQUIREMENTS.md` marks `QUAL-04` (Phase 33/34) complete, but because this specific sub-item needed an integration-test repro to confirm the fix actually prevents the leak, confirm a passing `bootChatEnvWithConfig`-path integration test exists before relying on this being fully closed.

**Command-hook and shell-exec files sit near the 600-LOC cap while also being security-critical** (`internal/agent/hooks_command.go` 507 LOC, `internal/agent/tools/shell_exec.go` 596 LOC): any future addition to either (e.g., new hook types, new shell guardrails) should split proactively rather than pushing past the cap under time pressure.

---

## Scaling Limits

**Single-user / single-trusted-operator is still the load-bearing assumption for shell/fs/MCP/object-store, despite Phase 36 targeting multi-user isolation:**
- Current capacity: one operator identity is the only fully-supported trust boundary; `MUSR-01..06` (Phase 36, OPEN) is the work item that generalizes conversations, approvals, background shell jobs, and MCP/Garage/skills directories to per-identity scoping.
- Limit: today, a second provisioned identity (`B`) can observe/mutate identity `A`'s conversations, approvals, and background shell jobs (`F-028`, `F-032`).
- Scaling path: Phase 36 (Authula cutover + owner-scoped stores) then Phase 37 (per-identity sandbox) then Phase 38 (per-identity MCP trust).

**Neo4j Community Edition backup is offline-only:**
- `compose.yaml` runs Neo4j Community + APOC + GDS; Community edition only supports offline `neo4j-admin database dump`/`load` (no online/hot backup). This is explicitly documented as a caveat to carry into the Phase 41 DR-drill work (`OPS-01`), not yet drilled with measured RPO/RTO.

**No load/chaos testing harness yet (`F-019` load/chaos part, Phase 41 `OPS-04/05`, OPEN):**
- No k6/vegeta load harness, no toxiproxy chaos suite (DB outage, MCP timeout storm, object-store outage, process-kill-during-write), no capability-evaluation CI report yet. Current confidence in behavior under contention/failure is based on unit/integration tests and manual review, not a runnable degradation-behavior suite.

---

## Dependencies at Risk

**Go 1.26.4 / Node ecosystem (React 19.2.7, Vite 8.0.16, TypeScript 6.0.3):** current, no stale major-version risk detected at this pass. `govulncheck` is wired into CI (`ci.yml`, `codeql.yml`) so known Go CVEs are gated; no CVE-scanning gap was found for the Go module graph. The gap is **SBOM publication** (`F-051`, Phase 40 `SEC-05`) and **Action/tool-version SHA pinning** — both open.

**`mcp-neo4j-cypher`** is the sole LLM interface to the Neo4j graph (no native Go driver adapter is used in production paths per `CLAUDE.md`); an upstream break in that MCP server has no fallback path documented yet.

---

## Missing Critical Features

**Multi-user identity isolation** (Phase 36, `MUSR-01..06`, OPEN) — blocks any deployment beyond a single trusted operator.

**Per-identity sandbox** (Phase 37, `SBX-01..05`, OPEN) — blocks `server_production` for the full-host tool surface.

**Production observability pack** (Phase 39, `OBS-01..06`, OPEN):
- `/readyz` and `/healthz` already exist (`cmd/aura/serve.go`, `internal/agui/server.go`) and `/readyz` already reflects required-backend health (`serveReadinessProbes`) — this is *better* than the 2026-06-21 audit's original `F-008`/`F-017` description, which predates this implementation. Still open: whether listener startup/runtime failure is unconditionally fatal (vs. only reflected in `/readyz`), the OTel **metrics** path (only traces are wired today per `F-023`), Prometheus alert rules/Grafana dashboards in-repo, and sidecar/trace retention as a first-class operation.

**Load/chaos/capability-eval harness** (Phase 41, `OPS-04/05`, OPEN) — no automated adversarial or degradation-behavior suite yet; production-readiness claims beyond unit/integration testing are not yet backed by a runnable evaluation report.

**Backup/DR drill with measured RPO/RTO** (Phase 41, `OPS-01`, OPEN) — backup exists (`pg_dump` + `neo4j-admin database dump`) but is not drilled/measured.

---

## Test Coverage Gaps

**Owned-surface coverage is currently strong (90.3% per `CLAUDE.md`, re-measured 2026-06-13) but two structural gaps remain:**

**Windows/POSIX test-parity gap:** a meaningful set of behaviors are tested only on POSIX/WSL and explicitly `t.Skip` on Windows (verified current in this pass): `internal/agent/tools/shell_bg_test.go` (cmd.exe fallback lacks `sleep`/`;` semantics), `internal/agent/tools/shell_exec_test.go` (interleave, cwd-tracking, heredocs), `internal/agent/tools/fs_test.go` (POSIX permission-bit assertions), `internal/agent/tools/send_file_test.go` (Windows symlink privilege requirement), `internal/agent/tools/result_test.go` (0600-mode assertion). This is intentional platform-gating (not a no-skip-as-green violation — these are unconditional `t.Skip`, not env-gated CI skips) but means Windows CI runs do not exercise these code paths; `CLAUDE.md` designates WSL as "the full primary dev environment" for this reason.

**GPU/live-tier deferred verification (documented, not hidden):** several tiers compile-check on the CI runner but require a GPU host or live external service to actually execute — `rerank_integration`, `document_ingest_live`, `graphrag_live`, `retrieval_eval` (Phase 30), the `memory_integration` MCP-sidecar tier, and paid-LLM-gated tiers (`live_finalize`, `cot_live_e2e`, scheduler E2E) that `t.Fatal` under `$CI` when their required env is unset (enforcing no-skip-as-green) but otherwise skip locally. These are tracked as a known, accepted "deferred verification tier" pattern per `.planning/STATE.md`, carried from v1.0.0 and expected to recur through Phase 41 pending an adequate GPU host (DGX Spark).

**No prompt-injection / adversarial regression suite** (`F-019`/`SEC-04`, Phase 40, OPEN) — see Missing Critical Features above; this is a coverage gap specifically for *security* behavior under the `ToolGateway`, distinct from ordinary unit/integration coverage.

**Askuser `int32` narrowing guard** (`QA-B-08`) — **[RESOLVED]**: verified current code has explicit overflow guards at `internal/askuser/store.go` (`ListRecent`/`ListPendingAll`-style limit clamping with inline comments referencing `QUAL-04a`/`D-15a` and the CodeQL `go/incorrect-integer-conversion` candidate). No action needed; flagged here only so a future audit doesn't re-open it without checking the current source.

---

*Concerns audit: 2026-07-04*
