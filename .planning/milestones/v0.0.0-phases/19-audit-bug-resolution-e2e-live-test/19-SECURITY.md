---
phase: 19
slug: audit-bug-resolution-e2e-live-test
status: verified
threats_open: 0
asvs_level: 1
created: 2026-06-10
---

# Phase 19 — Security

> Per-phase security contract: threat register, accepted risks, and audit trail.
> Phase 19 resolved the 2026-06-10 deep correctness audit (10 HIGH + 10 MEDIUM + 6 LOW + 1 INFO).
> The register was authored at plan time across 11 plans (19-01..19-11) and verified
> against the executed code by `gsd-security-auditor` (opus) on 2026-06-10.

---

## Trust Boundaries

| Boundary | Description | Data Crossing |
|----------|-------------|---------------|
| model → host shell | LLM-authored command crosses into host process execution (full-host-terminal posture, Phase 18 D-01) | shell command, exec output |
| child → grandchild process | bash spawns grandchildren (go run / python / npm) that can outlive the parent | OS process group |
| agent loop → user-facing answer | The terminal Event the user reads crosses out of the agent loop; a vetoed hand-off must not become the answer | assistant text |
| provider SSE stream → agent loop | The upstream LLM HTTP body crosses into the agent; a truncated/reset mid-stream body is untrusted-incomplete | streamed tokens |
| agent loop → in-process Fanout → Telegram/AG-UI consumers | Error strings + event JSON cross out of the agent; may embed DSNs/tokens | error messages, trace JSON (sensitive) |
| AG-UI client → server (RunAgentInput) | Untrusted client input (multimodal content) crosses into the turn driver | user message content |
| server → conformant AG-UI client (SSE) | Protocol frames cross to a client that runs ValidateSequence | AG-UI lifecycle/delta frames |
| agent loop → Telegram user surface | Error messages cross to the end user; a string may embed secrets | error text (sensitive) |
| Telegram user → HITL pause state | A `/cancel` command crosses into the paused_states lifecycle | pause-state mutation |
| compaction output → LLM provider | The reduced message history crosses to the provider; a wire-invalid shape causes a 500 | message history |
| dispatcher → notification sink (MCP route / stdout) | Run-outcome notification crosses to the user's channel; a failed send must not be silently lost | run-summary text |
| daemon → Postgres (aura_app role) | New durable notification state written under the DML-only app role | pending_notifications rows |
| recovery path → side-effecting handler | A catch-up fire crosses into a handler that may have committed side effects | scheduled-job re-fire |
| shutdown signal → run lifecycle write | A cancelled root ctx must still allow a terminal DB write | run-state ledger |
| MCP subprocess transport ↔ client | The stdin/stdout pipe to a spawned sidecar can die; client must recover | MCP JSON-RPC |
| sidecar stderr → client buffer | A chatty/error-looping sidecar writes unbounded stderr the client retains | stderr bytes (RSS pressure) |
| operator CLI → process env / .env | Operator subcommands must read the same .env as `aura serve` | env / config |
| model-controlled skill name → filesystem RemoveAll | A skill name reaching os.RemoveAll must be path-traversal-guarded | filesystem path |
| operator → host snippet exec | A runaway snippet on the operator CLI must be bounded | snippet process |
| empty user/model arg → external service (SearXNG) | An empty required arg should be rejected before the external call | search query / fetch URL |
| self-installed skill bundled scripts → host | Deliberate full-host execution surface (trust boundary, not a defect — amendment #50 / D-15c) | skill scripts |

---

## Threat Register

| Threat ID | Category | Component | Disposition | Mitigation | Status |
|-----------|----------|-----------|-------------|------------|--------|
| T-19-01 | Denial of Service | shell_exec orphan grandchild (H4, V12) | mitigate | Process group + `cmd.Cancel`(killProcessGroup) + `WaitDelay` 5s; whole-group reap (`shell_exec_unix.go:18` `-pid` SIGKILL / `shell_exec_windows.go:23` `taskkill /F /T`) | closed |
| T-19-02 | Elevation of Privilege | privileged orphan after kill (H4) | mitigate | 120s `WithTimeout` (`shell_exec.go:124-131`) preserved; `WaitDelay` (`:150`) rides on top, group-kill leaves no privileged survivor | closed |
| T-19-03 | Information Disclosure | `exec.ErrWaitDelay` leaked to model | mitigate | `shellStatusLine` (`shell_exec.go:423-439`) classifies ErrWaitDelay→`[command timed out]` / Canceled→`[command cancelled]`; raw Go err never emitted | closed |
| T-19-04 | Repudiation / Integrity | completion gate defeated on budget trip (M-b) | mitigate | Veto branches append feedback only, never durable RoleAssistant (`llm_agent.go:264-267`, `:391-393`); finalize cannot copy vetoed prose forward | closed |
| T-19-05 | Denial of Service (degraded) | transient stream-open blip defeats gate (M-a) | mitigate | completion/finalize/reasoning open through bounded `streamWithOpenRetry` (`llm_agent_completion.go:95`, `_finalize.go:217`, `_reasoning.go:45`) | closed |
| T-19-06 | Repudiation / Info loss | mid-stream SSE delivered as complete (H9, V7) | mitigate | `llm.Chunk.Err` (`client.go:83`); terminal `Chunk{Err}` emitted before close (`openai_compat/client.go:169-174`); main loop refuses partial (`llm_agent.go:483-491`) | closed |
| T-19-07 | Tampering (response integrity) | truncated provider response accepted | mitigate | Err on provider-neutral channel; drainers break on `c.Err` (completion `:101`, finalize `:225`, reasoning `:52`) → retryable infra failure | closed |
| T-19-08 | Information Disclosure | **Fanout RUN_ERROR + trace leak DSN/token (M-c, HIGH, V7)** | mitigate | `SanitizeString` on Message + `redactEvent` on traced JSON at Fanout boundary (`fanout.go:86,91,95,139`) — both paths sanitized | closed |
| T-19-09 | Tampering (protocol) | dropped boundary frame corrupts stream (H1) | mitigate | `isLifecycleFrame` widened (`server.go:271-291`); rewritten test runs `events.ValidateSequence` on survivors (`fanout_test.go:256`) | closed |
| T-19-10 | Spoofing / Input validation | silently-dropped multimodal input (M-d, V5) | mitigate | `lastUserMessage` rejects non-string content → 400 (`server.go:349-353`, `:149-152`) | closed |
| T-19-11 | Information Disclosure | user-facing error leaks secrets (H2, V7) | mitigate | `case *events.RunErrorEvent` routes Message through `agui.SanitizeString` (`renderer.go:103-110`, `:154`) | closed |
| T-19-12 | Repudiation / Info loss | async doc-convert failure swallowed (H3) | mitigate | async `asyncResult` sends `convertFailMessage` on `convErr != nil` (`bot_dispatch.go:398-409`) | closed |
| T-19-13 | Denial of Service (state) | orphaned paused_states + live keyboard after /cancel (M-e) | mitigate | `/cancel` → `cancelPendingPause` → `SubmitAnswer(ActionCancel)` + disarm keyboard (`bot_dispatch.go:120-122`, `bot_dispatch_hitl.go:65-72`) | closed |
| T-19-14 | Denial of Service / Integrity | wire-invalid compacted history → provider 500 (H8) | mitigate | `dropOldestPairs` boundary-aware drop + dangling-RoleTool-head skip; protected head intact (`context.go:236-282`) | closed |
| T-19-15 | Repudiation / Info loss | deferred + failed notifications silently dropped (H6/H7) | mitigate | `aura.pending_notifications` (0013) + store Insert/Sweep/Mark*; persist instead of log+return (`dispatch.go:202-217`); tick sweep (`:278-298`), no busy-poll | closed |
| T-19-16 | Elevation of Privilege | aura_app gaining DDL/DELETE on new table | mitigate | `0013_pending_notifications.up.sql:26` GRANT SELECT,INSERT,UPDATE to aura_app (no DELETE/DDL); ALL only to aura_migrate (`:27`) | closed |
| T-19-17 | Denial of Service | unbounded retry storm (H7) | mitigate | bounded re-attempt `status='failed' AND attempts < $1` (`pending_notifications.sql:14`, bound=3 `dispatch.go:274`); MarkFailed increments → not re-selected | closed |
| T-19-18 | Tampering / Integrity | dead ReschedulesOnRecovery re-fires side effects (M-g) | mitigate | `catchUpMissed` consults `reschedulesOnRecovery` seam (`recover.go:90,105-110`); false handler never auto-re-fired | closed |
| T-19-19 | Repudiation | shutdown leaves run stuck `running` (M-h) | mitigate | `complete` writes terminal state on `context.WithTimeout(context.WithoutCancel(ctx), 5s)` (`dispatch.go:171`) | closed |
| T-19-20 | Tampering (clarity) | inert SKIP LOCKED misleads maintainer (L5) | accept→fix | `FOR UPDATE SKIP LOCKED` removed from `DueTasks` (`scheduler_tasks.sql:26-37`); correctness held by per-task `pg_try_advisory_lock` (documented) | closed |
| T-19-21 | Denial of Service | dead MCP transport fails forever (H10) | mitigate | `reconnectingServer` re-Open+ListTools once, retry, `refreshSpecs`, no supervisor (`bridge_reconnect.go:21-116`); wired via `mount.go:23,57` ← `main.go:191,193` | closed |
| T-19-22 | Denial of Service | unbounded stderr buffer grows RSS (M-j, V12) | mitigate | bounded ring (`boundedbuffer/buffer.go:23-40`, default 4096B) at `mcp/client.go:92` + `knowledge/client.go:70`; stderrTail last-200B intact | closed |
| T-19-23 | Information Disclosure | reconnect error / refreshed description leaking detail | accept | reconnect reuses inline `error:` contract (`bridge.go:65`, `bridge_reconnect.go:94`); stderr redacted at stderrTail; no new leak surface | closed |
| T-19-24 | Tampering / Config confusion | operator edits wrong MCP config, .env invisible (M-i) | mitigate | single central `_ = godotenv.Load()` at `main.go:37`; env read at call-time after load; no internal init() env reads | closed |
| T-19-25 | Tampering (path traversal) | "../" skill name → os.RemoveAll (L2, V4/V5) | mitigate | `SanitizeName` guard before RemoveAll in Activate (`writer_activate.go:27`) + DiscardPending (`resume.go:67`, `:74`) | closed |
| T-19-26 | Denial of Service | runaway operator snippet hangs (L1, V12) | mitigate | `signal.NotifyContext(os.Interrupt)` + `context.WithTimeout(snippetExecTimeout)` (`skills_snippet.go:136-138`) | closed |
| T-19-27 | Information Disclosure | soft-deleted turns stay searchable (L4) | mitigate | `status == StatusDeleted` skip in Go wrapper (`store.go:503-505`); LOCKED FTS SQL not rewritten | closed |
| T-19-28 | Input Validation | empty query/url reaches SearXNG (L6, V5) | mitigate | empty-arg reject before external call (`web_search.go:82-84`, `web_fetch.go:71-73`) | closed |
| T-19-29 | Tampering | bundled skill scripts not blocklist-scanned (INFO) | accept | DELIBERATE per full-host trust model (amendment #50 / D-15c); `docs/audit/trust-boundary-info-2026-06-10.md` documents it; `loader.go` UNCHANGED (no scanner) | closed |
| T-19-30 | Validation only | live operator sign-off | accept | doc-only; `docs/audit/19-LIVE-SIGNOFF-2026-06-10.md` records before/after ground-truth for H1/H2/H3/H4/H5/H6/H7/H9/M-b/M-e | closed |
| T-19-SC | Tampering | npm/pip/cargo installs (supply chain) | accept | zero net-new third-party deps — `git diff 0e453c7a..HEAD -- go.mod go.sum` empty; godotenv/pgx/sqlc/migrate pre-existing | closed |

*Status: open · closed*
*Disposition: mitigate (implementation required) · accept (documented risk) · transfer (third-party)*

---

## Accepted Risks Log

| Risk ID | Threat Ref | Rationale | Accepted By | Date |
|---------|------------|-----------|-------------|------|
| AR-19-01 | T-19-20 (L5) | Inert `FOR UPDATE SKIP LOCKED` on the autocommit-pool `DueTasks` was dead defense-in-depth; correctness is held by the per-task `pg_try_advisory_lock`. Clause dropped (accept→fix); no security impact. | gsd-security-auditor | 2026-06-10 |
| AR-19-02 | T-19-23 | MCP reconnect path reuses the existing inline `error: ...` tool-failure contract and the stderrTail redaction; it introduces no new disclosure surface. | gsd-security-auditor | 2026-06-10 |
| AR-19-03 | T-19-29 (INFO) | Self-installed skill *bundled scripts* are not blocklist-scanned. Deliberate per the full-host-terminal trust model (PRD amendment #50 / D-15c) — equivalent to the model running `shell_exec`; a scanner would be security theater. Documented in `docs/audit/trust-boundary-info-2026-06-10.md`; `internal/skills/loader.go` left unchanged. | Operator (D-01a) / gsd-security-auditor | 2026-06-10 |
| AR-19-04 | T-19-30 | Live Layer-2 sign-off is operator-driven (paid, CDP-harnessed), not CI-automated — by design (D-03). Evidence recorded in `docs/audit/19-LIVE-SIGNOFF-2026-06-10.md`. | Operator | 2026-06-10 |
| AR-19-05 | T-19-SC | Phase installs zero new external packages; no supply-chain surface introduced. The one net-new package `internal/boundedbuffer` is first-party (not a dependency). | gsd-security-auditor | 2026-06-10 |

*Accepted risks do not resurface in future audit runs.*

---

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|------------|---------------|--------|------|--------|
| 2026-06-10 | 31 | 31 | 0 | gsd-security-auditor (opus, ASVS L1, block_on: high) |

**Verdict:** `## SECURED` — 26 `mitigate` threats verified present in the executed code (each with `file:line` evidence) + 5 `accept` threats confirmed documented. HIGH-severity block gate **T-19-08** (live Fanout secret-leak) verified MITIGATED. The three named false-green tests (H1 `ValidateSequence`, H4 PID-death assertion, H9 orphan-fixture consumption) confirmed rewritten, not just renamed.

**Auditor citation notes (non-blocking):**
- T-19-21 wiring lives in `internal/agent/mcptools/mount.go` (`MountServer`/`MountManagedServer`) invoked from `cmd/aura/main.go:191,193`, not `serve_channels.go` as the plan text cited — mitigation present and reachable; citation imprecision only.
- `SweepDueNotifications` (`pending_notifications.sql:17`) uses `FOR UPDATE SKIP LOCKED` correctly inside a real `db.WithTx` transaction (`store_runs.go:190`) — this is valid new usage on the new table, NOT a reintroduction of the L5 inert-clause defect.

---

## Sign-Off

- [x] All threats have a disposition (mitigate / accept / transfer)
- [x] Accepted risks documented in Accepted Risks Log
- [x] `threats_open: 0` confirmed
- [x] `status: verified` set in frontmatter

**Approval:** verified 2026-06-10
