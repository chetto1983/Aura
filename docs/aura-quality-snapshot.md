# Aura Quality Snapshot (living doc)

**Created:** 2026-05-29
**Last updated:** 2026-06-04 (Phase 16 MCP manager)
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
| Skill snippet exec success rate (sandbox-agent workspace, Phase 11 corpus 50 snippets) | ≥ 95% | YYYY-MM-DD (placeholder — populated by Phase 11) | TBD | Phase 11 Slice 7e-core | `internal/skills/snippet/**`, `internal/agent/tools/sandbox_exec.go`, `internal/sandboxagent/**` |
| Web tools — `ssrf.go` mutation (go-mutesting, ≥70% killed) + `internal/web` coverage (≥85% combined) + live `web_search` p95 (≤2s) | mut ≥70% / cov ≥85% / p95 ≤2s | 2026-06-02 (unit cov; live cells pending @ Gate-3) | unit cov 91.0% / combined 91.5% (PASS, was 75.5%); ssrf.go mutation 94.4% (PASS); SC#1 p95 ~1.01s (PASS) | Phase 7 Slice 5 | `internal/web/**`, `internal/agent/tools/web_*.go`, `searxng/settings.yml` |
| Swarm E2E (CAP-03 / SC#5 / D-22) — autonomous parallelization (≥2 workers, natural prompt) + mail+WhatsApp MCP read-back + timing <1.5× + judge ≥90% | ≥2 workers / facts present / mail+WA read-back / <1.5× / judge ≥0.90 / no-over-spawn | 2026-06-04 (live, run 8 of 8 — see iteration log in detail section) | **PASS** (workers=2, fan-out 15.9s / baseline 12.2s = 1.30×, e2e 27.8s advisory, mail+WA read-back=found, judge=1.00, control 0 workers + 5/5) | Phase 9 Slice 3 | `internal/swarm/**`, `internal/agent/tools/swarm_spawn.go`, `internal/eval/harness_swarm_e2e_test.go` |
| MCP manager mock E2E + policy gate (CAP-09 / MCP-V2-01) | mock stdio + Streamable HTTP + trust gate + policy gate pass; live tiers explicit | 2026-06-04 (automated mock tier) | **PASS** mock tier: `go test ./cmd/aura/ ./internal/mcp/ ./internal/mcp/manager/ ./internal/agent/mcptools/ -count=1`; live WhatsApp/mail/Calendar/Docker checks operator-only, not run in CI | Phase 16 MCP manager | `cmd/aura/mcp*.go`, `internal/mcp/**`, `internal/agent/mcptools/**`, `docs/mcp-manager.md` |
| Live CoT/tool-use eval (TestCoTEval, 12 scenarios × 10 dimensions, real agent vs DeepSeek-V4) | all asserted dimensions full; reasoning advisory | 2026-06-04 (live re-run alongside the swarm gate) | **PASS** 12/12 scenarios; secret_redaction 12/12, streaming 11/11, tool-loop 2/2, cost 8/8, cache-prefix 1/1, budget 1/1, cancellation 1/1, guardrails 2/2; reasoning 6/7 advisory; cache-hit 8/8 | Phase 3 Slice 1 | `internal/eval/**`, `internal/agent/llm_agent*.go`, `internal/llm/**` |

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

## References

- `prd.md` Slice 11a acceptance + Slice 11d acceptance (HNSW M=32 + CI gate inclusion) — amendment #20 anchor sites.
- `prd.md` §Slice Q&A discipline Gate 3 — bullet enforcing snapshot freshness on every slice that touches a measured path.
- `.planning/research/SUMMARY.md` PRD Amendments table row 20 — origin of this living doc.
- User memory `feedback_aura_as_product` (2026-05-21) — quality-matrix-as-product directive that scoped amendment #20.
- `.planning/ROADMAP.md` Phase 5/6/11/13/15 success criteria — sourced targets for the matrix.
