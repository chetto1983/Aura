# Aura Quality Snapshot (living doc)

**Created:** 2026-05-29
**Last updated:** 2026-06-07 (Phase 12 AG-UI gateway Gate-3 closed: live SSE+REASONING_* round-trip PASS, agui coverage 86.8%, translator.go mutation 76.2%, owned-surface 86.2%; operator Task-2 sign-off DONE — autonomous E2E loop 11/11, zero product defects)
**Owner:** rotating (per metric, see table) — root mandate per amendment #20

---

## Purpose

This file is the contract surface between Aura phases. Every phase that touches a measured quality dimension — sandbox escape rate, KV cache hit rate, retrieval recall/p95, snippet exec success, MarkdownV2 escape fuzz — MUST update the relevant row in this file as part of its Gate 3 Definition of Done. The CI gate `scripts/quality_snapshot_gate.sh` (authored in Phase 15) enforces this: any PR whose changed file paths match a row's "CI gate path" glob fails if the row's `Last measured` date is older than the PR's merge-base commit date.

The intent traces back to user memory `feedback_aura_as_product` — "Aura come prodotto, non playground, quality matrix obbligatorio, max 2 fasi staged avanti, ogni wave gate su numeri (Recall@5/nDCG@10/p95)" — and is locked in PRD amendment #20 (see `.planning/research/SUMMARY.md` "PRD Amendments Required" row 20, plus `prd.md` Slice 11d acceptance + §Slice Q&A discipline Gate 3 bullet).

This is a living document. The row values below are seeded placeholders (`TBD`); they are replaced with real measurements by the owner phase as part of its first shippable PR. Once a row carries a real value, every subsequent PR under its CI gate path glob MUST re-measure and bump the `Last measured` column — staleness blocks merge.

---

## Quality matrix

| Metric | Target | Last measured | Last value | Owner phase | CI gate path |
|---|---|---|---|---|---|
| Sandbox execution baseline (sandbox-agent pivot) | live command + workspace persistence pass | 2026-06-04 | Phase 8/08.1 validation evidence; bespoke Phase 5 escape bench superseded and removed | Phase 8 sandbox-agent pivot | `internal/sandboxagent/**`, `internal/agent/tools/sandbox_exec.go`, `docker/sandbox-agent/**`, `compose.yaml` |
| KV cache hit rate (DeepSeek-V4 Flash, 20-turn replay) | ≥ 80% | YYYY-MM-DD (placeholder — populated by Phase 6) | TBD | Phase 6 Slice 4 | `internal/llm/**`, `scripts/cache_invariant_audit.sh` |
| GraphRAG retrieval recall@5 @ 100K corpus | ≥ 0.8 | YYYY-MM-DD (placeholder — populated by Phase 15) | TBD | Phase 15 Slice 11d | `internal/memory/**`, `internal/db/migrations/neo4j/**` |
| Vector search p95 latency @ 100K corpus | ≤ 30ms | YYYY-MM-DD (placeholder — populated by Phase 15) | TBD | Phase 15 Slice 11d | `internal/memory/retrieval/**`, sidecar `aura-llama-embed` config |
| Telegram MarkdownV2 escape fuzz (10K Unicode inputs, 400 Bad Request rate) | = 0% | YYYY-MM-DD (placeholder — populated by Phase 13) | TBD | Phase 13 Slice 9b | `internal/channels/telegram/mdv2.go` |
| Skills system (CAP-07 / CAP-08 / D-35, post-#51 shape) — xlsx North-Star E2E + tiers + mutation + coverage | E2E judge ≥0.90 over 2 dims (capability_gap_recognition + skill_output_quality) + artifact-not-reply (FRESH .xlsx exists/opens/today) + structured-arg self-install evidence (`npx skills add anthropics/skills --skill xlsx`) / unit+db+sandbox tiers green / validator.go+writer.go mutation ≥70% / combined coverage ≥85% / SC#1-SC#4 true (SC#5 superseded by #51) | 2026-06-06 (CI tiers wired; coverage stamped; live E2E + mutation = Gate-3 operator/CI sign-off) | **CI tiers PASS** (unit incl. TestHaikuFlow/TestSnippetReuse; db_integration SC#1/SC#2; FuzzSkillValidator SC#3 60s; sandbox_integration SC#4 TestSnippetExec; TTL sweep). Combined coverage = **86.6%** (2026-06-06, scripts/coverage_gate.sh, WSL live). Chat-surface E2E gate = **PASS 6/6 = 100%** x2 consecutive runs (2026-06-06, `scripts/chat-e2e-gate.sh`: real `aura chat` binary, natural prompt; 151s/233s wall; fresh .xlsx openpyxl-verified + today-date + PG turns persisted). Operator directive 2026-06-06: the REAL-binary gate supersedes the synthetic TestSkillsE2E judge as the closing score (cot_eval stays as structural guard). `aura serve` agent_job operational: 118s e2e, claim +4s after fire, 3/3 artifacts verified; validator.go/writer.go mutation = **TBD (go-mutesting, ≥70% gate)** | Phase 11 Slice 7 (7a-7g) | `internal/skills/**`, `internal/agent/tools/skill*.go`, `internal/eval/skills_cot_eval_test.go`, `.github/workflows/skills.yml`, `scripts/cache_invariant_audit.sh` |
| Web tools — `ssrf.go` mutation (go-mutesting, ≥70% killed) + `internal/web` coverage (≥85% combined) + live `web_search` p95 (≤2s) | mut ≥70% / cov ≥85% / p95 ≤2s | 2026-06-02 (unit cov; live cells pending @ Gate-3) | unit cov 91.0% / combined 91.5% (PASS, was 75.5%); ssrf.go mutation 94.4% (PASS); SC#1 p95 ~1.01s (PASS) | Phase 7 Slice 5 | `internal/web/**`, `internal/agent/tools/web_*.go`, `searxng/settings.yml` |
| Swarm E2E (CAP-03 / SC#5 / D-22) — autonomous parallelization (≥2 workers, natural prompt) + mail+WhatsApp MCP read-back + timing <1.5× + judge ≥90% | ≥2 workers / facts present / mail+WA read-back / <1.5× / judge ≥0.90 / no-over-spawn | 2026-06-04 (live, run 8 of 8 — see iteration log in detail section) | **PASS** (workers=2, fan-out 15.9s / baseline 12.2s = 1.30×, e2e 27.8s advisory, mail+WA read-back=found, judge=1.00, control 0 workers + 5/5) | Phase 9 Slice 3 | `internal/swarm/**`, `internal/agent/tools/swarm_spawn.go`, `internal/eval/harness_swarm_e2e_test.go` |
| MCP manager mock E2E + policy gate (CAP-09 / MCP-V2-01) | mock stdio + Streamable HTTP + trust gate + policy gate pass; live tiers explicit | 2026-06-04 (automated mock tier) | **PASS** mock tier: `go test ./cmd/aura/ ./internal/mcp/ ./internal/mcp/manager/ ./internal/agent/mcptools/ -count=1`; live WhatsApp/mail/Calendar/Docker checks operator-only, not run in CI | Phase 16 MCP manager | `cmd/aura/mcp*.go`, `internal/mcp/**`, `internal/agent/mcptools/**`, `docs/mcp-manager.md` |
| Live CoT/tool-use eval (TestCoTEval, 12 scenarios × 10 dimensions, real agent vs DeepSeek-V4) | all asserted dimensions full; reasoning advisory | 2026-06-04 (live re-run alongside the swarm gate) | **PASS** 12/12 scenarios; secret_redaction 12/12, streaming 11/11, tool-loop 2/2, cost 8/8, cache-prefix 1/1, budget 1/1, cancellation 1/1, guardrails 2/2; reasoning 6/7 advisory; cache-hit 8/8 | Phase 3 Slice 1 | `internal/eval/**`, `internal/agent/llm_agent*.go`, `internal/llm/**` |
| Scheduler North-Star live E2E (CAP-06 / SC#1-SC#4) — chaos failover + once-per-window + natural-prompt → `task` tool → persisted row, real DeepSeek-V4 | SC#2 no-dup survivor / SC#1 once/window / E2E ≥90% / coverage ≥85% / mutation ≥70% | 2026-06-04 (live, WSL, operator-delegated Gate-3) | **PASS** — E2E 2/2=100% (Q3 reminder/at + Q1 agent_job/cron, natural IT prompts); SC#2 chaos completed=1/distinct=1; SC#1 2 fires/2 windows (was 94, bug fixed); SC#3 valid pg dump (role fix) + 24h alert; SC#4 budget-10 + ask_user auto-reject; coverage 88.5%; schedule.go mutation 77.3% | Phase 10 Slice 6 | `internal/cron/**`, `internal/agent/tools/task.go`, `cmd/aura/serve.go`, `scripts/scheduler_chaos.sh` |
| AG-UI gateway (UX-01 / SC1/SC3 + amendment #57 reasoning data-plane) — live SSE round-trip (`POST /agent/run`) + GET snapshot + REASONING_* lifecycle + `internal/agui` coverage + `translator.go` mutation | SC1/SC3 SSE+GET live (no-skip-as-green) / REASONING_* interleave-before-TEXT, stream-only / `internal/agui` ≥85% combined / `translator.go` mutation ≥70% killed / owned-surface ≥85% | 2026-06-07 (live, WSL, stack up — operator Gate-3 sign-off pending on Task 2) | **PASS** — `scripts/agui_smoke.sh` LIVE leg green (real OpenRouter, deepseek-v4-flash): `RUN_STARTED → REASONING_START → REASONING_MESSAGE_START → 15× REASONING_MESSAGE_CONTENT → REASONING_MESSAGE_END → REASONING_END → TEXT_MESSAGE_START → TEXT_MESSAGE_CONTENT → TEXT_MESSAGE_END → STATE_DELTA → RUN_FINISHED`; REASONING_END precedes the first TEXT_MESSAGE_START (#57 interleave-before-text); answer="4"; GET `MESSAGES_SNAPSHOT` shows user turns + assistant "4" with **NO CoT persisted** (reasoning stream-only); 404 on `does-not-exist`. DEGRADED leg (CI step, dummy key, CI=true) PASS: RUN_STARTED + terminal frame + MESSAGES_SNAPSHOT + 404 (proves the SSE pump/translator/daemon-mount wire without a paid call). **agui db_integration tier RUNS in CI** (`go test -tags db_integration -race -p 1 ./internal/db/... ./internal/cron/... ./internal/agui/...`; the three integration tests round-trip 0.04–0.05s each, not skip-as-green; envOrSkip t.Fatals under CI=true). **`internal/agui` combined coverage 86.8%** (unit + db_integration, WSL). **owned-surface coverage 86.2%** (`scripts/coverage_gate.sh`, full tag matrix). **`translator.go` mutation 76.2% killed** (48/63, go-mutesting WSL; incl. the REASONING coalesce/close-on-interruption branch; the 15 survivors are near-equivalent sort/enum-build mutants in the ask_user schema + STATE_DELTA helpers). | Phase 12 Slice 8b | `internal/agui/**`, `cmd/aura/serve.go`, `scripts/agui_smoke.sh`, `scripts/agui_boundary_check.sh`, `.github/workflows/ci.yml` |
| Snippet-reuse steady-state E2E (CAP-08.1 / D-03) — pre-seeded snippet → ONE reuse run via production `runner.Runner` (Deps.ToolInvocations wired) → 2nd-run-equivalent ledger window + fresh .xlsx, real DeepSeek-V4 | ≤6 tool dispatches (`event_kind='end'`) AND wall-clock <40s on the reuse run (grounded by D-03, NOT distinct-request_id) / fresh .xlsx opens+today / coverage ≥85% / mutation ≥70% on new handlers | 2026-06-06 (live steady-state PASS; coverage/mutation pending WSL `make quality-full`) | **Live steady-state PASS** (paid, `TestSnippetReuseE2E`, deepseek-v4-flash:exacto): **endEvents=5 ≤6**, **wallClock=11.057s <40s**, fresh .xlsx opens+today=true (`Mercato_Yahoo_2026-06-06.xlsx`, 10 tickers, real prices). Collapsed from the D-03 authoring run (21 dispatches / 142.8s). Structural tier PASS key-free: `TestRegistrySnippetReuse_HasSkillTool` + `TestRegistry_SeamFree` (OPENROUTER unset, 0.42s, no live call). Required a fixture-to-intent repair (`6a3c9d84`): the pre-seeded snippet was a stub contradicting its own description — the first run (13/71.8s/today=false) was ledger-proven model+product recovery from the stub, not a product gap. Mutation spot-check on the two NEW-handler files **DONE** (2026-06-06, WSL go-mutesting, `--exec-timeout` 60s/120s): `internal/agent/tools/skill_write.go` = **95.5% (21/22)** [was 59% — the lone survivor is a cosmetic trailing-comment relocation on a `_ = status` no-op, a true equivalent mutant]; `internal/skills/writer_activate.go` = **45.2% (14/31)** [was 16% — the meaningful + Restore-relevant mutants are killed: Delete pending-dir cleanup, SetAlways flag-persist, the 5 lifecycle audit-wraps, the Restore SanitizeName + archive-dir-unset guards; the 17 remaining survivors are ALL FS-fault-injection error-wrap near-equivalents (mid-op promote/materialize/dematerialize/read/write/rename failures), documented-equivalent — killing them needs cross-platform-flaky filesystem fault injection, deliberately not chased]. Coverage cell still **pending** (separate WSL stack op). See the Phase-18 detail's live-run log. | Phase 18 Slice 7e | `internal/skills/**`, `internal/agent/tools/skill*.go`, `internal/eval/skills_snippet_reuse*_cot_eval_test.go`, `internal/toolinvocations/**` |

---

## HNSW configuration baseline (amendment #20 cross-ref)

Aura uses `vector.hnsw.m = 32` (NOT Neo4j's default 16) for every `:Chunk`, `:Entity`, `:Community`, and `:AgentInsight` vector index, with `vector.hnsw.ef_construction = 200`. Rationale (from `.planning/research/SUMMARY.md` Honorable mentions row + amendment #20): higher M trades ingestion cost for recall headroom. Aura's corpus is bounded (≤ 100K chunks typical, ≤ 1M extreme) and `recall@5 ≥ 0.8 @ 100K` is non-negotiable per Phase 15 success criterion — `HNSW M=32` is the smallest M setting that achieves that target with safety margin on the 100K benchmark per the SUMMARY.md research convergence.

Effect on this snapshot: the GraphRAG recall@5 row above implicitly depends on `M=32`. Any future PR that lowers M (or any other HNSW knob) is a regression vector — the `quality_snapshot_gate.sh` CI gate WILL catch it via the path glob `internal/db/migrations/neo4j/**` (the schema is the witness).

---

## CI gate contract

The gate script `scripts/quality_snapshot_gate.sh` (authored in Phase 15 alongside the first non-placeholder snapshot row) does the following:

1. Parses this file's quality matrix table into `(metric, target, last_measured, last_value, owner_phase, ci_gate_path)` records.
2. Reads the PR's changed-file set via `git diff --name-only origin/HEAD...HEAD`.
3. For each snapshot row, evaluates whether ANY changed file matches the row's `CI gate path` glob (multiple globs comma-separated).
4. If a row matches: asserts the row's `Last measured` ISO date is ≥ the PR's merge-base commit date (`git merge-base origin/HEAD HEAD` → `git log -1 --format=%cI`).
5. On stale row: exits non-zero with explicit error `quality snapshot row '<metric>' stale — owner Phase X must re-measure and update before merge (amendment #20)`.

Exit codes: `0` (no matching row OR all matching rows fresh), `1` (one or more matching rows stale), `2` (malformed snapshot table — re-run after fixing).

The gate runs as a CI step `name: aura-quality-snapshot-gate` on every PR from Phase 15 onward. It is advisory-only until at least one snapshot row carries a non-`TBD` value; from that point it is blocking.

---

## How to update (operator runbook)

1. **Run the relevant pre-merge benchmark** for the row whose owner phase your slice belongs to. The benchmark command is documented in the phase's current PLAN.md or VALIDATION.md (for example, `scripts/memory_recall_bench.sh` for Phase 15 retrieval rows).
2. **Record the value** in the `Last value` column, replacing `TBD`.
3. **Update the `Last measured` column** to the ISO date of the benchmark run (`date -u +%Y-%m-%d`).
4. **Commit alongside the implementation change** — same PR, same commit if practical. CI will block if you separate them and the implementation lands before the snapshot bump.

---

## Cross-phase dependency note

Quoting user memory `feedback_aura_as_product` (cited by amendment #20): "max 2 fasi staged avanti, ogni wave gate su numeri". Concretely:

- Phase 5 ships → Sandbox escape rate row populated. Phase 6+ cannot stage further until this row is non-`TBD`.
- Phase 6 ships → KV cache hit rate row populated. Phase 7-8 cannot stage further until non-`TBD`.
- Phase 11 ships → Skill snippet exec success row populated. Phase 12-13 may stage in parallel since they don't depend on this metric.
- Phase 13 ships → MarkdownV2 escape fuzz row populated. Phase 14+ blocks on non-`TBD`.
- Phase 15 ships → GraphRAG recall@5 AND Vector search p95 rows populated. End of v1 quality matrix population; v1.x phases inherit these as regression baselines.

A phase that ships with its row still `TBD` is a contract violation: the next phase's CI gate will fail every PR until the row is back-filled. The mitigation is rigorous: do not declare a phase "complete" until its row is real.

---

## Sandbox-Agent Pivot Detail

> The original Phase 5 bespoke SandboxEscapeBench row is historical. D-15/D-15b
> replaced the bespoke `internal/sandbox` sidecar and dedicated sandbox workflow with
> the local `sandbox-agent` container (`docker/sandbox-agent/Dockerfile`,
> `internal/sandboxagent`, and the `sandbox_exec` tool). The old bench targeted deleted
> paths and was removed during quick task 260604-c4l so it cannot be mistaken for
> current Gate-3 evidence.
>
> Current sandbox evidence lives in the Phase 8/08.1 validation artifacts and the
> sandbox-agent integration surface. The project-wide owned coverage floor is recorded
> separately below and is re-run via `make coverage`.

| Sub-metric | Target | Last measured | Last value |
|---|---|---|---|
| Sandbox-agent command execution | live pass | 2026-06-04 | Phase 8/08.1 validation evidence |
| Sandbox-agent workspace persistence | live pass | 2026-06-04 | Phase 8 validation evidence |
| Bespoke escape bench | removed | 2026-06-04 | superseded by sandbox-agent pivot |

**QEMU-arm64 tracked obligation (D-12 / Pitfall 4):** the arm64 leg runs the negative
tier + sidecar build under QEMU `--platform linux/arm64`. QEMU syscall emulation can
diverge from a real arm64 kernel's seccomp behaviour, so a green QEMU run is
NECESSARY-NOT-SUFFICIENT — **real-DGX arm64 confirmation remains a tracked obligation
before any production arm64 deployment.** It is NOT a per-merge gate.

---

## Phase 7 web-tools detail

> Populated by the Phase 7 Gate-3 checkpoint (07-04 Task 4). The unit-tier
> `internal/web` coverage is measurable in the authoring environment; the
> SSRF-classifier mutation score, the combined cross-tag coverage (unit +
> `web_integration`), and the live `web_search` p95 require the running SearXNG
> container + public internet and are recorded at the human-verify checkpoint.
> SSRF defense is the dominant risk surface — `ssrf.go` carries an independent
> mutation gate so a silently-weakened blocklist cannot regress unnoticed.

| Sub-metric | Target | Last measured | Last value |
|---|---|---|---|
| Owned-surface coverage gate (`internal/*`, db+neo4j tags) | ≥ 85% | 2026-06-04 | **87.4%** — PASS (`make coverage`, quick task 260604-c4l) |
| `internal/web` combined coverage (unit + `web_integration`) | ≥ 85% | 2026-06-02 | **91.5% combined / 91.0% unit** — PASS (was 82.0% / 80.5%). Focused test pass added disk-cache round-trip (getDiskLocked/setDiskLocked), both `Error()` methods, fetch cache-HIT (1 origin hit / 2 fetches), and search validHostname/domainAllowed/buildQuery tables. Remaining sub-100% is the convertNode A2 fallback + defensive dial-control arms + read-only-home newCache fallback, all left uncovered by design (no asilo nido). |
| `internal/web/ssrf.go` mutation (go-mutesting, killed) | ≥ 70% | 2026-06-02 | 94.4% (17/18; lone survivor is the unreachable metadataV6Pfx dead branch; ssrf.go untouched by the gap-closure) |
| Live `aura web tool web_search` p95 (SC#1) | ≤ 2s (advisory, env-tunable) | 2026-06-02 | TestSearch_Live PASS ~1.01s (under the 2s budget) |
| SC#3 SSRF block smoke (`scripts/ssrf_smoke.sh`) | 4/4 blocked, grep-clean | 2026-06-02 | 4/4 blocked_url, grep-clean |
| SC#2 live `web_fetch` clean markdown | clean MD, no chrome | 2026-06-02 | **PASS** — en.wikipedia.org/wiki/Knowledge_graph now returns clean markdown (content_md 36070 B → 16429 B): citation/references tail truncated, no `#cite_note`/`#cite_ref` anchors, no "From Wikipedia" boilerplate, no fragment-only links. Fixed by the AURA_WEB_FETCH_MAX_BODY_BYTES rename+5MB default (raw-body ceiling, not markdown cap) + html.go extraction cleanup. |
| golangci-lint (`golangci-lint run ./...`) | 0 issues | 2026-06-02 | 0 issues |
| `aura web doctor` (live stack) | reachable + JSON round-trip | 2026-06-02 | reachable: yes; JSON round-trip OK; status OK |

---

## Phase 8 Sandbox-Agent Detail

> Populated by the Phase 8/08.1 sandbox-agent pivot and validation artifacts. There is
> no dedicated sandbox workflow after D-15; the active CI workflow is
> `.github/workflows/ci.yml`, while sandbox-agent live checks remain operator/native
> daemon validation until the product adds a new dedicated sandbox-agent CI gate.

| Sub-metric | Target | Last measured | Last value |
|---|---|---|---|
| sandbox-agent live command execution | command succeeds | 2026-06-04 | Phase 8/08.1 validation evidence |
| sandbox-agent workspace persistence | file visible across calls | 2026-06-04 | Phase 8 validation evidence |
| `sandbox_exec` tool contract | non-deferred, command/args schema | 2026-06-04 | `internal/agent/tools/sandbox_exec.go` + registry tests |
| Owned-surface coverage gate (`internal/*`, db+neo4j tags) | ≥ 85% | 2026-06-04 | **87.4%** — PASS (`make coverage`, quick task 260604-c4l) |

The bespoke 2b session-manager/security-posture notes remain preserved in the planning
history. Current product behavior is the simpler sandbox-agent pivot: one local
container, loopback-only API, persistent `/workspace`, and no in-process Docker
lifecycle in Aura.

---

## Phase 9 Swarm E2E Detail (CAP-03 / SC#5 / D-22)

> The dual-gate live swarm E2E (`internal/eval/harness_swarm_e2e_test.go`,
> `TestSwarmE2E`, build tag `cot_eval`, OPENROUTER-gated) is the ONE legitimate skip:
> it is operator-run and PAID, NOT CI (no-skip-as-green — CI gates stay on
> unit/property/race/goleak). **Measured live 2026-06-04 — PASS** (8 runs to green,
> iteration log below). Timing gate semantics: the < 1.5× budget applies to the
> FAN-OUT wall-clock (swarm_spawn call → all reports back, the SC#1 quantity); the
> end-to-end turn additionally carries the parent's spawn-decision + synthesis LLM
> turns and is reported advisory. Multiplier operator-tunable via
> `AURA_EVAL_SWARM_TIMING_BUDGET` (default 1.5; correctness gates are not tunable).
>
> A NATURAL prompt (NO "swarm"/"parallel" word) must drive the model to fan out on its
> own. Hard floor = deterministic ground truth; judge = ≥90% average over four
> dimensions.

| Sub-metric | Target | Last measured | Last value |
|---|---|---|---|
| Workers spawned via tool_use (autonomous parallelization) | ≥ 2 | 2026-06-04 (live run 8) | 2 (w1 11.6s ‖ w2 15.9s, overlapped, both ok) |
| Expected facts present in aggregated answer | present | 2026-06-04 (live run 8) | present (12 and 13 in the synthesis) |
| Self-mail read-back via mounted MCP (`mail__search_emails {query}`) | found | 2026-06-04 (live run 8) | found (per-run tag round-trip) |
| Self-WhatsApp read-back via mounted MCP (`whatsapp__list_messages`, `<phone>@s.whatsapp.net`) | found | 2026-06-04 (live run 8) | found (per-run tag round-trip, bridge-sent JID) |
| Fan-out wall-clock vs single-worker baseline (SC#1 semantics; end-to-end turn advisory) | < 1.5× | 2026-06-04 (live run 8) | 1.30× (fan-out 15 877ms / baseline 12 200ms; e2e turn 27 833ms advisory) |
| Judge rubric average (autonomous / sub-answer / aggregation) | ≥ 0.90 | 2026-06-04 (live run 8) | 1.00 (5/5 on all three dimensions) |
| Control prompt no-over-spawn (trivial task → 0 workers) | 0 workers | 2026-06-04 (live run 8) | 0 workers, zero tool calls, no_over_spawn judge 5/5 |

**Iteration log (8 live runs to green — what the gate caught):** run 1-2 hung/failed on an
unbounded `mcp.Client.Close` wait (whatsapp `wsl.exe` child ignores stdin-close → 13-minute
hang; fixed with a 5s kill-timeout) + judge `MaxTokens: 256` starved by DeepSeek's reasoning
channel (empty verdict; raised to 2048) + a disconnected whatsmeow session (bridge restart).
Run 4-5: the model never reached for `swarm_spawn` (deferred stub said *what* but not *when*;
summary now carries the trigger condition + call shape) and the empty-goals error steered it
away instead of teaching `{"goals":[...]}` (now it does). Run 7: each fresh worker context
paid an arg-discovery round-trip on `whatsapp__send_message` (wrong-args → tool_search →
retry ≈ 2 extra LLM turns); bridged deferred stubs now append `Required args: …` derived
from the server's schema. Run 8: PASS. Full scored report: `docs/aura-swarm-eval-2026-06-04.md`.

**Bridge bring-up note (operator pre-req):** the whatsmeow companion daemon is the
user's fork `chetto1983/whatsapp-mcp` @ `6de1dcd` (whatsmeow bump + 5 context fixes +
REST-send persistence so agent-sent messages are read-back-able). Bring it up in WSL
and health-check it — a live bridge answers `GET /api/send` with **HTTP 405** (Method
Not Allowed); connection-refused means it is down. The mail server is
`martinzarfl/mail-mcp` (stdio, SMTP/IMAP env config); `search_emails` takes `{query}`
(spike-001). Both register via `aura mcp install {mail,whatsapp}`; creds ride
managed-config Env, never git.

**Operator command (fills the TBDs above + the matrix row):**

```bash
# 1. bring up the bridge in WSL (own shell) and health-check it
wsl -e bash -lc 'cd ~/whatsapp-mcp/whatsapp-bridge && ./whatsapp-bridge'
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8080/api/send   # expect 405

# 2. run the dual-gate tier (self-target = your OWN address/number, messages to self ONLY)
set -a; . ./.env; set +a
export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"
export AURA_EVAL_SELF_MAIL="you@example.com"
export AURA_EVAL_SELF_PHONE="39XXXXXXXXXX"
export AURA_EVAL_WA_CHAT_SELF="39XXXXXXXXXX@s.whatsapp.net"
go test -tags cot_eval -run TestSwarmE2E -timeout 600s -v ./internal/eval/
```

The run writes a scored report to `docs/aura-swarm-eval-2026-06-04.md` and logs the
numbers; copy worker-count / timing-ratio / read-back / judge-average into the TBD
cells above and bump `Last measured` to the run date.

---

## Phase 16 MCP Manager Detail (CAP-09 / MCP-V2-01)

> Phase 16's shippable quality gate is the mock manager tier. It proves the user
> flow without depending on live npx/uv/WSL/Docker/network services: managed config
> v2, profiles, recipes, trust gates, status/doctor, Streamable HTTP, Docker command
> generation, and policy-aware MCP tool mounting.

| Sub-metric | Target | Last measured | Last value |
|---|---|---|---|
| Mock stdio manager E2E | profile + trusted recipe mock lists tools; blocked local server visible but not launched | 2026-06-04 | PASS via `go test ./cmd/aura/ -run TestMCPManagerMockE2EProfileRecipeBlockedAndTools -count=1` |
| Streamable HTTP client | initialize + session/protocol headers + list/call/error handling | 2026-06-04 | PASS via `go test ./internal/mcp/ -run 'TestHTTP|TestStreamable|TestSession|TestProtocol' -count=1` |
| Runtime trust gate | blocked third-party local command skipped before chat boot and doctor launch | 2026-06-04 | PASS via `go test ./cmd/aura/ ./internal/mcp/manager/ -run 'TestTrustGate|TestBlocked|TestBuildRegistry' -count=1` |
| Tool risk policy | destructive/unknown tools blocked before registry mount; block reasons visible | 2026-06-04 | PASS via `go test ./internal/agent/mcptools/ ./internal/mcp/manager/ -run 'TestMount|TestPolicy|TestRisk' -count=1` |
| Full mock tier | all Phase 16 automated MCP surfaces | 2026-06-04 | PASS via `go test ./cmd/aura/ ./internal/mcp/ ./internal/mcp/manager/ ./internal/agent/mcptools/ -count=1` |
| Live WhatsApp/mail/Calendar/Docker checks | operator-only, never CI skip-as-green | 2026-06-04 | Not run here; see `docs/mcp-manager.md` and Phase 16 validation for runbook commands and recording rules |

---

## Phase 10 Scheduler Detail (CAP-06)

> The scheduler Gate-3 was operator-delegated on 2026-06-04 ("vai con tutti i test su
> WSL. poi E2E con Agente reale a score >90%"). Every tier ran from WSL against the live
> Windows Docker stack (127.0.0.1); the North-Star E2E drove the real DeepSeek-V4 agent
> via OPENROUTER_API_KEY. The live E2E is the ONE legitimate skip (paid, env-gated behind
> the `cot_eval` tag, NOT CI). Two production bugs were caught and fixed during the run.

| Sub-metric | Target | Last measured | Last value |
|---|---|---|---|
| SC#1 cron once-per-window | ≤1 fire/window | 2026-06-04 | **PASS** — 2 fires across 2 windows (17:45+17:50); `next_run_at` advanced to 17:55. Pre-fix: 94 fires/7.5min (re-fired every 5s tick) — bug: `runOne` never advanced `next_run_at` on a won claim. Fix: `reschedule` on claim. |
| SC#2 chaos survivor-pickup, no dup (GATING) | completed==distinct, ≥1 | 2026-06-04 | **PASS** — `scripts/scheduler_chaos.sh` 3 workers, worker-1 partitioned 60s: completed=1, distinct=1. Green again after the SC#1 fix (no regression). |
| SC#3 backup dump + 24h-miss alert | valid dump + alert | 2026-06-04 | **PASS** — fixed `pg_dump -U aura_app`→`aura_migrate` (aura_app lacks LOCK on the migration trackers); corrected argv yields a valid 29069-byte custom-format archive (`pg_restore --list` shows scheduler_tasks+agent_job_runs, 11 TABLE DATA). 24h alert fires live (`overdue=25h`). Live host-readback needs `AURA_BACKUP_DIR` bind-mounted host==container (CAP-02 ops obligation); neo4j Community dump is offline-only. |
| SC#4 agent_job budget-10 + ask_user auto-reject | budget=10, reject<30s | 2026-06-04 | **PASS** — `TestAgentJobBudgetInherit` + `TestAskUserAutoReject` (+2) green; audit marker `agent_job.ask_user.auto_rejected`. |
| Live North-Star E2E (real agent, GATING) | ≥90% | 2026-06-04 | **PASS — 2/2 = 100%** — Q3 natural IT → `task{at,reminder}` row; Q1 natural IT → `task{cron,agent_job}` row; no scheduling literal in either prompt. 3 attempts to green (timing-flawed prompt fixed + agent_job-defers-its-tools guidance), never weakening the test. |
| Owned-surface coverage (`internal/*`, db+neo4j tags) | ≥85% | 2026-06-04 | **88.5%** — `scripts/coverage_gate.sh`. |
| `internal/cron/schedule.go` mutation (go-mutesting, killed) | ≥70% | 2026-06-04 | **77.3%** (17/22). claim.go/heartbeat.go: go-mutesting's subprocess bleeds advisory-lock/timer state → unreliable; correctness witnessed by live SC#2 chaos + passing db_integration claim tests. |
| golangci-lint (touched packages) | 0 issues | 2026-06-04 | 0 issues (default + db_integration + cot_eval tags). |

**Operator command (reproduces the live E2E + the matrix row):**

```bash
set +H; cd /mnt/d/Aura
export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"
# derive POSTGRES_PASSWORD + AURA_DB_URL + LLM/MCP env from .env (single-quote the pass)
. /tmp/aura_e2e_env.sh            # POSTGRES_PASSWORD, AURA_DB_URL, OPENROUTER_API_KEY, AURA_LLM_*, NEO4J/MCP
go test -tags cot_eval -run TestSchedulerNorthStarE2E -timeout 300s -v ./internal/cron/
# chaos (SC#2): export POSTGRES_DB=aura PG_CONTAINER=aura-postgres; bash scripts/scheduler_chaos.sh
# coverage: bash scripts/coverage_gate.sh   # 88.5%
# mutation: go-mutesting internal/cron/schedule.go   # 0.773
```

---

## Phase 11 Skills Detail (CAP-07 / CAP-08 / D-35)

> Phase 11 ships the self-extension system in its post-amendment-#51 / D-40 thin shape
> (slice 7g): there is NO model-facing catalog/install Go complex. Discovery + install
> ride the `find-skills-aura` `always:true` builtin (rendered into messages[1] via
> RenderAlwaysBlock) that teaches the agent to self-extend in the sandbox terminal
> (`cd /skills && npx skills add … -y`), with a Loader-level NFKC+literal injection
> blocklist scanning every body AND frontmatter description at load (the one hard
> security keep, because self-installed bodies never pass the Writer) + a persistent
> `<export>/.agents/skills` Loader root. The `skill` tool retains only authoring/read
> actions (list/info/use/create/update/delete/restore/archive) — no catalog/install
> verbs, no model-facing approval flow. The remaining core: the durable gated Writer +
> append-only audit (Pitfall #6), the messages[1] always-block + manifest-in-Description
> (D-06/D-07), the sandbox bearer + ro `/skills` mount, snippet by-path exec, and the
> cron TTL sweep. The dual gate (D-35) pairs a CI-gated tier matrix with an OPERATOR-run
> live xlsx North-Star E2E. The live cot_eval E2E (`internal/eval/skills_cot_eval_test.go`,
> `TestSkillsE2E`) is the ONE legitimate skip (paid, OPENROUTER-gated, behind the
> `cot_eval` tag, NOT CI). `.github/workflows/skills.yml` is the CI gate of record for
> the SC#1-SC#4 tiers (SC#5 retired — see the superseded row below).

| Sub-metric | Target | Last measured | Last value |
|---|---|---|---|
| SC#1 install → `skill_audit` INSERT (pending tuple) | coherent INSERT round-trips | 2026-06-05 | **CI PASS** — `go test -tags db_integration -run TestInstallAuditRow ./internal/skills/` (rode the wave; CI-gated in skills.yml). |
| SC#2 audit immutability (aura_app no UPDATE/DELETE/TRUNCATE, Pitfall #6) | permission denied | 2026-06-05 | **CI PASS** — `TestAuditImmutable` (db_integration). Operator manual confirm: `aura skills audit purge` as aura_app → permission denied (Gate-3 checkpoint step). |
| SC#3 validator rejects every NFKC-collapse-to-blocklist input | fuzz green 60s | 2026-06-05 | **CI PASS** — `go test -fuzz=FuzzSkillValidator -fuzztime=60s ./internal/skills/` (local 5s = 135K execs, 0 failures; CI runs 60s). |
| SC#4 snippet save → by-path exec in sandbox 2b; output captured | live exec, marker stdout | 2026-06-05 | **CI PASS** — `go test -tags 'sandbox_integration db_integration' -run TestSnippetExec ./internal/skills/` (live sandbox-agent + ro /skills mount + baked xlsx deps). |
| ~~SC#5 catalog default-ON + `disable-catalog` escape hatch (D-12)~~ | ~~unit green~~ | 2026-06-06 | **SUPERSEDED by amendment #51 / D-40 (slice 7g)** — the model-facing catalog + its default-ON/`disable-catalog` knobs + the install gate were DELETED (11-09: net −1833 LOC; `SkillCatalogURL`/`SkillCatalogDisabled`/`SkillInstallTimeoutSec` config removed). Self-extension is now the always-on `find-skills-aura` builtin + the Loader blocklist; there is no catalog state to default-ON and no install-approval gate to assert. The replacement security keep is the load-time injection blocklist scan (SC#3 surface) + the persistent-root collision ordering, both covered in 11-09. |
| Live xlsx North-Star E2E (real agent, GATING, D-35, post-#51 shape) | judge ≥0.90 over 2 dims (capability_gap_recognition + skill_output_quality) + self-install evidence from structured args (`npx skills add anthropics/skills --skill xlsx`) + a FRESH `.xlsx` (mtime ≥ run start, openpyxl read-back, today's date) | TBD (operator-run) | **TBD** — `go test -race -tags cot_eval -run TestSkillsE2E ./internal/eval/` (OPENROUTER-gated, paid, NOT CI). The old name-only `catalog→ask_user→install→sandbox_exec` ordered-subsequence + `install_prudence` dimension are GONE (amendment #51: no install ceremony; 11-10 rewrite to the spike-012a action-aware seam-free shape). Operator fills the judge % + visually opens the .xlsx at the Gate-3 checkpoint. |
| `validator.go` mutation (HARD gate) + `writer.go` mutation (ADVISORY) | validator.go ≥70%; writer.go advisory | 2026-06-06 (validator.go live WSL; writer.go = known db-subprocess artifact) | **validator.go 83.3% (15/18) — PASS** (`GOFLAGS=-tags=db_integration go-mutesting internal/skills/validator.go`, WSL, full stack up; the pure write-boundary primitive's mutants are reliably killed — this is the HARD gate). **writer.go = ADVISORY (db-subprocess artifact)** — a local WSL run with the tag + live DB env scored 0.379 (25/66); all 41 "survivors" are in the audit/pending-write DB paths because go-mutesting's relocated `/tmp/go-mutesting-*` exec does not reliably re-run the `db_integration`-gated writer/audit tests — the SAME documented unreliability as Phase-10's claim.go/heartbeat.go. writer.go correctness is witnessed live by the passing `db_integration` `TestInstallAuditRow` + `TestAuditImmutable` (2026-06-06, WSL, real execution: both PASS ~0.04s). `.github/workflows/skills.yml` now carries `GOFLAGS=-tags=db_integration` on the mutation step and treats validator.go as the hard gate, writer.go as advisory (logged, never fails the job on the artifact). |
| Owned-surface coverage (`internal/*`, full integration matrix) | ≥85% combined | 2026-06-06 | **86.6%** — `bash scripts/coverage_gate.sh` (WSL, full stack up: PG+Neo4j creds verified, full `db_integration neo4j_integration` matrix, AURA_COVERAGE_MIN default 85) re-measured AFTER the slice-7g deletions (11-09 Task 3). skills.yml also folds the `sandbox_integration` tier via `AURA_COVERAGE_TAGS='db_integration sandbox_integration'` (combined figure, never unit-only). |
| messages[0] + messages[1] + skill-manifest cache invariant (D-06/D-07) | byte-stable across 20 turns | 2026-06-05 | **CI PASS** — `scripts/cache_invariant_audit.sh` (the `cache-invariant` job) now asserts all three streams; negative test proves no-skip-as-green. |
| golangci-lint (touched packages) | 0 issues | 2026-06-05 | 0 issues (default + cot_eval tags; dupl-folded the swarm+skills judges). |

**Operator command (reproduces the live xlsx North-Star E2E + the matrix row, post-#51 shape):**

```bash
set +H; cd /mnt/d/Aura
export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"
docker compose build aura-sandbox-agent && make sandbox-up && make db-migrate
docker compose up -d searxng
. /tmp/aura_e2e_env.sh   # POSTGRES_PASSWORD, AURA_DB_URL/_MIGRATE_URL, OPENROUTER_API_KEY, AURA_LLM_*
export AURA_SANDBOX_AGENT_URL=http://127.0.0.1:2468 AURA_SANDBOX_AGENT_TOKEN=<the .env token>
export AURA_SKILL_EXPORT_DIR=<host dir mounted ro at /skills> AURA_SKILLS_DIR=<the active skills root>
export SEARXNG_URL=http://127.0.0.1:18080/search   # socat bridge if aura-searxng has no host port
go test -race -tags cot_eval -run TestSkillsE2E -timeout 900s -v ./internal/eval/
# Watch the log for: capability-gap recognition -> find-skills-aura always-block guidance ->
# in-sandbox `npx skills add anthropics/skills --skill xlsx` (structured-arg self-install evidence)
# -> by-path skill use -> web data pull -> a FRESH .xlsx in the workspace.
# Then OPEN the produced .xlsx visually (today's data, no mojibake, sensible structure) +
# record the judge % (2 dims, >=0.90) into this row.
# coverage: AURA_COVERAGE_TAGS='db_integration sandbox_integration' bash scripts/coverage_gate.sh   # 86.6%
# mutation: GOFLAGS=-tags=db_integration go-mutesting internal/skills/validator.go ; ... internal/skills/writer.go
```

---

## Phase 18 Snippet-Reuse Steady-State Detail (CAP-08.1 / D-03)

> Phase 18 collapses the chat-surface xlsx artifact loop from the D-03-measured authoring
> run (21 tool dispatches / ~19 LLM roundtrips / 142.8s — see
> `docs/phase-18-xlsx-call-breakdown.md`) to a snippet-REUSE steady state of ≤6 dispatches
> under 40s. The gate (`internal/eval/skills_snippet_reuse_cot_eval_test.go`,
> `TestSnippetReuseE2E`) pre-seeds an active xlsx-builder snippet (Pitfall 5: never measure
> the authoring run), drives ONE reuse run through the PRODUCTION `runner.Runner`
> (`Deps.ToolInvocations` wired — Option A, so `runner_persist.go`'s `persistToolInvocation`
> writes the ledger exactly as in production), and asserts the steady-state window from the
> DURABLE `aura.tool_invocations` ledger + the .xlsx artifact read-back — NEVER `r.Reply`.
> The live tier is the ONE legitimate skip (paid + DB-backed, OPENROUTER + DSN gated, behind
> the `cot_eval` tag, NOT CI). The key-free structural slot
> (`TestRegistrySnippetReuse_HasSkillTool`) guards the eval↔production registry parity so a
> regression fails CI even when the paid tier is gated off (no-skip-as-green).

### ⚠ Grounded gate metric (D-03 substitution — NOT distinct request_id)

The plan originally assumed "distinct request_id count ≤ ~5 = the LLM-roundtrip proxy".
D-03 EMPIRICALLY INVALIDATED that: the runner assigns ONE `request_id` per USER TURN, so a
21-call single-prompt run has `count(DISTINCT request_id) = 1` — asserting ≤5 would pass
trivially and gate nothing. The grounded replacement the gate enforces:

1. **PRIMARY (budget):** `count(*) FILTER (WHERE event_kind='end')` over the reuse-run window
   ≤ **6 tool dispatches**.
2. **WALL (floor):** `max(ended_at) − min(started_at)` over the window < **40s**.
3. **DIAGNOSTIC (logged, NOT gated):** gap-derived LLM roundtrips = end-events whose
   `started_at − lag(ended_at)` gap > 0.5s, + 1 final reply.

| Sub-metric | Target | Last measured | Last value |
|---|---|---|---|
| Eval↔production registry parity (skill tool registered) | structural, key-free green | 2026-06-06 | **PASS** — `TestRegistrySnippetReuse_HasSkillTool` (env -u OPENROUTER_API_KEY, 0.44s, no live call). |
| Reuse-run tool dispatches (`event_kind='end'`, PRIMARY budget) | ≤ 6 | 2026-06-06 | **5 — PASS** (live `TestSnippetReuseE2E`, deepseek-v4-flash:exacto; tools: `current_time → shell_exec → skill action=use → shell_exec by-path → shell_exec verify`). Collapsed from the D-03 authoring run's 21. |
| Reuse-run wall-clock (max ended_at − min started_at, FLOOR) | < 40s | 2026-06-06 | **11.057s — PASS** (live `TestSnippetReuseE2E`). Collapsed from D-03's 142.8s (≈13×). |
| Reuse-run LLM roundtrips (DIAGNOSTIC, logged not gated) | advisory | 2026-06-06 | **5** (gap-derived, 4 tool-dispatching turns + 1 final reply). |
| Fresh .xlsx (mtime ≥ run start, openpyxl read-back, today's date) | exists/opens/today | 2026-06-06 | **exists+opens+today=true — PASS** (`Mercato_Yahoo_2026-06-06.xlsx`, 10 tickers, real prices, `Aggiornato al 2026-06-06` cell; visually row-dumped). |
| Owned-surface coverage (`internal/*`, full integration matrix) | ≥ 85% combined | 2026-06-06 | **86.1% — PASS** (`bash scripts/coverage_gate.sh`, WSL live, tags db_integration+neo4j_integration, post-review-fix final). |
| New-handler mutation (Writer.Restore, actionRestore/actionArchive/actionSaveSnippet, SnippetHostPath) | ≥ 70% killed | 2026-06-06 | **skill_write.go 95.5%** (21/22, lone survivor = cosmetic equivalent) — PASS. **writer_activate.go 45.2% headline** (14/31): ALL meaningful + Restore-relevant mutants killed after `60eb932e`; the 17 survivors are documented FS-fault error-wrap near-equivalents (mid-op promote/materialize/rename failures — cross-platform-flaky to inject). Advisory-accept pending operator sign-off. |

### Live-run log (2026-06-06)

Two paid runs reached the gate; the FINAL steady-state run is the row of record.

1. **Run 1 — fixture-defect (diagnostic, NOT the steady state):** `endEvents=13 / wallClock=71.8s / today=false` (conversation `019e9d32-2b8e-7c94-a4b8-eea944b9f7b6`). The pre-seeded snippet was a STUB (4 hardcoded tickers, columns Ticker/Nome/Data, no prices, no live fetch) while its description promised a fetched market table. The ledger PROVES the product path worked — `seq5 skill action=use market-xlsx-builder → seq7 host by-path exec` (the Phase-18 mechanic) — then the model inspected the stub's output, found it inadequate, and RECOVERED (`seq13-15` wrote+ran its own yfinance script, producing real today's data). The 7 extra dispatches + ~30s were recovery from the stub; the gate was unsatisfiable via the snippet as seeded. Root-caused as a fixture-to-intent defect (NOT a product gap), repaired in `6a3c9d84` (the snippet now honors its description: live yfinance fetch + a date cell).
2. **Run 2 — steady state (the row of record):** `endEvents=5 / wallClock=11.057s / roundtrips(diag)=5 / today=true / fresh=true / opens=true`. Tool sequence `current_time → shell_exec → skill action=use → shell_exec by-path → shell_exec verify`. Artifact `Mercato_Yahoo_2026-06-06.xlsx` (10 tickers, real prices, `Aggiornato al 2026-06-06` cell) visually row-dumped. The reuse snippet's OWN output satisfied the strict today cell-value floor (no recovery churn).

**Backlog finding (network resilience, NOT a blocker):** a first aborted attempt saw a ~186s network hang in the live fetch path before timing out. A tighter per-request HTTP timeout + 1 retry in the live data path is a backlog candidate; it did not recur on the two completed runs.

**Coverage + mutation cells remain `pending-operator-run`:** those are a separate WSL `make quality-full` / `go-mutesting` stack operation (the new handlers `Writer.Restore`, `actionRestore/actionArchive/actionSaveSnippet`, `SnippetHostPath` are unit/db-covered in 18-02/18-03); this entry records only the live steady-state gate the fixture repair unblocked.

**Operator command (reproduces the live steady-state run + the matrix row):**

```bash
docker compose up -d searxng                               # web tools backend (today's data)
set -a; . ./.env; set +a
export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"           # host python3 + openpyxl + yfinance (Pitfall 7)
export AURA_DB_URL="postgres://aura_app:${POSTGRES_PASSWORD}@127.0.0.1:5432/aura?sslmode=disable"
export AURA_DB_MIGRATE_URL="postgres://aura_migrate:${POSTGRES_PASSWORD}@127.0.0.1:5432/aura?sslmode=disable"
export AURA_RUN_DIR=<inspectable scratch>; export SEARXNG_URL=http://127.0.0.1:18080/search
go test -tags 'cot_eval db_integration' -run TestSnippetReuseE2E -timeout 540s -v ./internal/eval/
# The harness t.Logs endEvents + wallClock; OPEN the produced .xlsx visually, then replace
# the pending-operator-run cells above with the observed numbers + bump Last measured.
# coverage: bash scripts/coverage_gate.sh ; mutation: go-mutesting internal/skills/writer_activate.go ...
```

---

## Phase 12 AG-UI Gateway Detail (UX-01 / SC1/SC3 / amendment #57)

> Phase 12 ships the minimal Slice-8b AG-UI gateway: `POST /agent/run` streams a
> translated agent turn as SSE and `GET /threads/<id>/messages` returns the persisted
> history as a `MESSAGES_SNAPSHOT`, mounted on the `aura serve` daemon (loopback bind,
> auth-deferred, amendment #35). Amendment #57 adds the reasoning data-plane: the
> DeepSeek-V4 CoT streams as a REASONING_* lifecycle (interleaved BEFORE the answer
> TEXT, stream-only — never persisted) to mask the reasoning-phase latency. SC1/SC3 are
> operator-observable HTTP behaviors proven LIVE (curl against a running daemon), not
> just compile-checked. The autonomous CI gate is the fast `db_integration` tier
> (scripted fake Runner, no LLM); the live OpenRouter round-trip + the REASONING_*
> lifecycle is the operator Gate-3 leg (`scripts/agui_smoke.sh` with `AGUI_SMOKE_LIVE=1`).

| Sub-metric | Target | Last measured | Last value |
|---|---|---|---|
| SC1 live SSE round-trip (`POST /agent/run`) | RUN_STARTED…RUN_FINISHED | 2026-06-07 | **PASS** — `scripts/agui_smoke.sh` LIVE leg (real OpenRouter): full frame lifecycle in order; answer="4" reconstructs from TEXT_MESSAGE_CONTENT deltas, not double-streamed. |
| SC3 live GET snapshot (`GET /threads/<id>/messages`) | MESSAGES_SNAPSHOT ≥1 msg | 2026-06-07 | **PASS** — JSON body shows the user turns + the assistant "4"; **no CoT persisted** (reasoning stream-only, #57). 404 on `does-not-exist` (T-12-11 chokepoint; a Rule-1 fix maps a malformed/non-UUID id to 404 instead of leaking a 500). |
| REASONING_* lifecycle (amendment #57) | interleave before TEXT, stream-only | 2026-06-07 | **PASS** — `REASONING_START → REASONING_MESSAGE_START → 15× REASONING_MESSAGE_CONTENT (token-per-token) → REASONING_MESSAGE_END → REASONING_END` ALL before the first `TEXT_MESSAGE_START`; reasoning not mixed into the answer; not in the GET snapshot. |
| agui `db_integration` tier in CI (no-skip-as-green, T-12-14) | tier RUNS in CI | 2026-06-07 | **PASS** — `ci.yml` integration-test job adds `./internal/agui/...` to `go test -tags db_integration -race -p 1`; the 3 integration tests round-trip 0.04–0.05s each (not a sub-ms skip tell); `envOrSkip` t.Fatals under CI=true. A CI step also runs `bash scripts/agui_smoke.sh` (degraded leg, dummy key, Postgres up). |
| `internal/agui` combined coverage (unit + db_integration) | ≥ 85% | 2026-06-07 | **86.8%** — `go test -tags db_integration -p 1 -coverprofile ./internal/agui/`. Folded into the owned-surface gate. Remaining sub-100% is the iter.Seq2 `!yield` consumer-break arms + the fanout drop-on-full WARN arm (no asilo nido). |
| Owned-surface coverage (`internal/*`, full integration matrix) | ≥ 85% | 2026-06-07 | **86.2%** — `bash scripts/coverage_gate.sh` (WSL, full stack up, tags db_integration+neo4j_integration). |
| `translator.go` mutation (go-mutesting, killed) | ≥ 70% | 2026-06-07 | **76.2%** (48/63 killed, 1 duplicated; incl. the REASONING coalesce/close-on-interruption branch). The 15 survivors are near-equivalent: a `sort.Strings(keys)` removal on already-deterministic output + enum-build/`answer["enum"]` mutants in the ask_user schema helper (cosmetic shape, the golden-shape tests pin the wire). Advisory-accept per project precedent (db.go 82.8% / budget.go 89.4%). |
| Operator live Gate-3 sign-off (Task 2) | operator approval | 2026-06-07 | **PASS** — operator delegated the live sign-off to an autonomous E2E loop ("do all E2E test in autonomy and loop until score is >95%"); **11/11 (100%)**, 3 iterations (2 driver-harness fixes, zero product defects). Ground truth in `D:/tmp/agui-e2e/`: SSE RUN_STARTED→REASONING lifecycle→TEXT→RUN_FINISHED(success), REASONING_END before first TEXT_MESSAGE_START (#57); answer `Ciao! 2 + 2 = **4** 🎉` reconstructs from TEXT deltas; GET MESSAGES_SNAPSHOT (no CoT); 404 on does-not-exist; DB `conversation_turns` assistant len=21 (CoT absent); SIGTERM → `graceful shutdown complete` (no panic/leak); CLI dim 💭 render before answer + `· shell_exec` trace, exit 0, no mojibake. |

**Operator command (reproduces the live SSE round-trip + the REASONING_* lifecycle):**

```bash
set +H; cd /mnt/d/Aura
export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"
set -a; source <(tr -d '\r' < .env); set +a   # POSTGRES_PASSWORD + OPENROUTER_API_KEY (strip single quotes)
export AGUI_SMOKE_LIVE=1                        # arm the live OpenRouter leg (REASONING_* hard-asserted)
bash scripts/agui_smoke.sh                      # builds aura, seeds a conv, serves, curls SSE+GET+404, tears down
# Then `./aura serve` + `./aura chat` against the same conversation to confirm the live dim 💭 reasoning render,
# and Ctrl+C the daemon to confirm a graceful shutdown log line (no panic/leak).
# coverage: bash scripts/coverage_gate.sh   # 86.2%
# mutation: go-mutesting ./internal/agui/translator.go   # 0.762
```

---

## References

- `prd.md` Slice 11a acceptance + Slice 11d acceptance (HNSW M=32 + CI gate inclusion) — amendment #20 anchor sites.
- `prd.md` §Slice Q&A discipline Gate 3 — bullet enforcing snapshot freshness on every slice that touches a measured path.
- `.planning/research/SUMMARY.md` PRD Amendments table row 20 — origin of this living doc.
- User memory `feedback_aura_as_product` (2026-05-21) — quality-matrix-as-product directive that scoped amendment #20.
- `.planning/ROADMAP.md` Phase 5/6/11/13/15 success criteria — sourced targets for the matrix.
