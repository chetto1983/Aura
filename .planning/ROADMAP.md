# Roadmap: Aura

## Overview

Aura ships in 16 phases — one no-code documentation phase (P0 PRD amendments) plus 15 implementation phases mapped 1:1 onto the PRD's locked 14-slice scope (Slice 0.5 → 11; Slice 13 is v2, gated on GPU). The journey is **architecture-validated horizontal layers**: Persistence (P1) → Agent Cornerstone (P2) → LLM client (P3) → Conversation/Identity/HITL cluster (P4) → Sandbox 2a (P5) → KV Cache (P6, deliberately near-late) → Web (P7) → Sandbox 2b (P8) → Swarm (P9) → Scheduler (P10) → Skills (P11) → AG-UI transport (P12) → Channels + Telegram (P13) → Onboarding (P14) → Memory subsystem (P15). The dependency chain is non-negotiable: Slice 0.9 (P2) is the cornerstone every later phase implements; KV cache (P6) must come after the stable system prompt is real or every later capability plants its own cache-poisoning site; Memory (P15) is the most downstream and gates on every prior phase. Each phase finishes Gate 3 DoD (coverage ≥75% unit / ≥60% integration, mutation ≥70% killed, goleak clean, race-detector clean) before the next begins.

## Phases

**Phase Numbering:**

- Integer phases (0, 1, 2, …, 15): Planned milestone work
- Decimal phases (e.g., 5.1): Urgent insertions (marked with INSERTED)

Decimal phases appear between their surrounding integers in numeric order.

- [x] **Phase 0: PRD Amendments** - 20 PRD edits in one no-code commit before any Slice 0.5 code
- [x] **Phase 1: Infra DB + Knowledge** - Postgres 17 + Neo4j 5.26 LTS + MCP server operational
- [x] **Phase 2: Agent Cornerstone** - `Agent` interface + workflow agents (Sequential/Loop/Parallel) + budget contract (completed 2026-05-30)
- [x] **Phase 3: LLM Client + ToolResult** - OpenAI-compat handrolled client + ToolResult preview+sidecar + SSE streaming
 (completed 2026-05-30)

- [x] **Phase 4: HITL + Identity + Conversations** - `ask_user` pause/resume, identity scaffolding, multi-thread conversations with FTS
 (completed 2026-05-30)

- [x] **Phase 5: Sandbox 2a Stateless** - Python 3.12 sidecar with positive seccomp allowlist + SandboxEscapeBench (completed 2026-06-02)
- [x] **Phase 6: KV Cache Builder** - stable-prefix discipline + provider-aware cache_control + cross-slice invariant CI (completed 2026-06-02)
- [x] **Phase 7: Web Tools** - SearXNG `web_search` + readeck-readability `web_fetch` with SSRF defense (IPv6 + DNS pin) (completed 2026-06-02)
- [ ] **Phase 8: Sandbox 2b Session-Bound** - per-conversation workspace + network allowlist + symlink escape guard
- [ ] **Phase 9: Swarm (Minimal)** - ParallelAgent reuse with 2-deep cap + child budget inheritance
- [ ] **Phase 10: Scheduler** - cron + persistent `agent_job` with `FOR UPDATE SKIP LOCKED` + advisory lock + heartbeat
- [ ] **Phase 11: Skills** - instruction-based skills (7a/b/c/d) + executable snippets v1 (7e-core) + audit trigger
- [ ] **Phase 12: AG-UI Gateway** - SSE event protocol transport with `agent ⇸ agui` import boundary enforced
- [ ] **Phase 13: Channels + Telegram + Multimodal** - Telegram primary channel, setup wizard, Gemma 4 voice+image
- [ ] **Phase 14: Onboarding + Agent.md** - User onboarding LoopAgent + Agent.md profile injected at `messages[1]`
- [ ] **Phase 15: Memory Subsystem** - Document ingest + entity resolution + GraphRAG hybrid retrieval + agent journal

## Phase Details

### Phase 0: PRD Amendments

**Goal**: Apply 20 PRD amendments in a single no-code commit `prd: pre-implementation drift fixes from independent research convergence`, sealing all Stack drift, Architecture spec gaps, and cross-cutting Pitfall mitigations BEFORE any Slice 0.5 code commit. PRD-first principle enforcement.
**Depends on**: Nothing (first phase)
**Requirements**: PRD-01
**Slices**: (no code — PRD `prd.md` + CLAUDE.md edits only)
**Success Criteria** (what must be TRUE):

  1. Operator runs `git log --oneline -1` and observes the amendment commit (subject matches `prd: pre-implementation drift fixes…`) with zero `.go` files changed
  2. Operator greps `prd.md` for `Go 1.25` and `5.26-community` and `codeberg.org/readeck/go-readability/v2` and observes all 3 strings present (Amendments #1, #2, #3)
  3. Operator greps `prd.md` for `AURA_LOOP_MAX_STEPS`, `AURA_EMBED_DIMENSIONS`, `cache_invariant_audit.sh`, `AURA_SETUP_TOKEN` and observes all 4 strings present (Amendments #19, #18, #16, #10)
  4. Operator reads `docs/aura-quality-snapshot.md` and observes the file exists with seed schema (Amendment #20)

**Plans:** 6 plans (00-01 through 00-06; 5 cluster plans + 1 commit aggregator)

- [x] 00-01-PLAN.md — Stack drift cluster (Amendments #1-6: Go 1.25, Neo4j 5.26-community, readability lib, MarkdownV2 escaper, telebot SHA, AG-UI SDK SHA)
- [x] 00-02-PLAN.md — Feature gaps cluster (Amendments #7-10: Slice 1.8.5 FTS, /cost command, OTel hooks, AURA_SETUP_TOKEN)
- [x] 00-03-PLAN.md — Architecture spec gaps (Amendments #11-14: AgentInsight cache TTL, swarm scope reduction, Slice 7e split, skill.catalog opt-in)
- [x] 00-04-PLAN.md — Cross-cutting pitfalls (Amendments #15-19: goleak extension, cache invariant CI, audit role separation, embedding dim contract, loop budget contract)
- [x] 00-05-PLAN.md — Quality gate (Amendment #20: docs/aura-quality-snapshot.md seed + Slice 11d HNSW M=32)
- [x] 00-06-PLAN.md — Phase commit aggregator (single git commit, STATE/ROADMAP bookkeeping)

### Phase 1: Infra DB + Knowledge

**Goal**: Stand up Postgres 17 + Neo4j 5.26-community LTS so the rest of the substrate has somewhere to persist application state and knowledge graph data. Postgres ships with `aura.*` schema, golang-migrate-managed migrations, and role separation (`aura_app` vs `aura_migrate`). Neo4j ships with APOC + GDS + HNSW 768d vector index + `mcp-neo4j-cypher` MCP server subprocess.
**Depends on**: Phase 0
**Requirements**: INFRA-01, INFRA-02
**Slices**: 0.5, 0.7 (parallelizable — independent stores)
**Success Criteria** (what must be TRUE):

  1. Operator runs `aura db migrate` and observes idempotent migration (re-run is a no-op with explicit "no pending migrations" message)
  2. Operator runs `aura db migrate` as `aura_app` role and observes permission denied (role separation working); migration only succeeds as `aura_migrate`
  3. Operator runs nightly restore drill `scripts/restore_drill.sh` and observes Postgres restore from `pg_dump` completing in under 90 seconds
  4. Operator runs `aura neo4j ping` and observes `mcp-neo4j-cypher` subprocess returning Neo4j server version 5.26.x; embed sidecar `/v1/embeddings round-trip returns 768d (Pattern 5 dim probe)` matching `AURA_EMBED_DIMENSIONS` (D-05 + Pattern 5 amendment)
  5. Operator runs the spike smoke `scripts/neo4j_smoke.sh` and observes recall@5 = 5/5 on the Italian fixture corpus, p95 vector search ≤ 30ms

**Plans**: TBD

### Phase 2: Agent Cornerstone

**Goal**: Implement the unified `Agent` interface (Name/Description/Run/SubAgents/FindAgent yielding `iter.Seq2[*Event, error]`) and the three exported workflow agents (Sequential/Loop/Parallel) — ~420 LOC adapted from `google/adk-go` with Apache-2.0 attribution. Wire the budget contract (`AURA_LOOP_MAX_STEPS=25`, `AURA_LOOP_MAX_WALLCLOCK_SEC=300`, `AURA_LOOP_DEDUP_WINDOW=3` plus dedup/soft-cap hardening vars) with child-inherits-parent's-remaining semantics. This is the substrate cornerstone; every later phase implements this interface.
**Depends on**: Phase 1
**Requirements**: INFRA-03
**Slices**: 0.9
**Success Criteria** (what must be TRUE):

  1. Operator runs `go test -race ./internal/agent/...` with `goleak.VerifyNone` in TestMain and observes zero goroutine leaks across all workflow agent tests
  2. Operator runs the loop-cap fixture (`scripts/loop_budget_smoke.sh`) with a mock agent that returns the same tool call forever and observes termination at exactly `AURA_LOOP_MAX_STEPS=25` with an explicit budget-exhausted Event
  3. Operator runs the swarm depth fixture and observes a depth-3 spawn chain consuming the parent's remaining step budget (NOT fresh per child) — total steps across the tree ≤ 25, not 25^3
  4. Operator runs `aura agent dry-run --request-id auto` and observes an OTel-compatible UUIDv7 `request_id` attached to every emitted Event for correlation, while SpanID/ParentSpanID remain 8-byte OTel/W3C-compatible IDs internally

**Plans:** 8/8 plans executed

- [x] 02-00-PLAN.md — Gate 0 artifact convergence: A1-A7 truth-source sync, full validation map, fail-closed plan commands, adk-go attribution
- [x] 02-01-PLAN.md — Deps (uuid direct + rapid) + internal/canonicaljson deterministic serializer (D-08/A3/A6)
- [x] 02-02-PLAN.md — Agent interface + InvocationContext + Event/Actions/LLMResponse + trace IDs + ErrBudgetExhausted (D-01/02/16/17/24)
- [x] 02-03-PLAN.md — Budget tree: shared atomic + TOCTOU ConsumeStep + fail-fast env + two-tier dedup + soft cap (D-06/09/10/11/12/13/18)
- [x] 02-04-PLAN.md — agenttest reusable Agent mocks (D-07)
- [x] 02-05-PLAN.md — SequentialAgent + LoopAgent + goleak TestMain + SC#2 budget-exhausted Event (Req#4/5, SC#1/2)
- [x] 02-06-PLAN.md — ParallelAgent (errgroup + ack + escalate-cancel) + SC#3 depth shared-counter + break-early goleak (Req#6, SC#1/3)
- [x] 02-07-PLAN.md — aura agent dry-run CLI (SC#4) + loop_budget_smoke.sh (SC#2) + loop.go deletion + .env.example + A1-A7 amendment verification

### Phase 3: LLM Client + ToolResult

**Goal**: Handrolled OpenAI-compatible HTTP+SSE client (~280 LOC, no SDK) targeting DeepSeek-V4 via OpenRouter as default. Implement the ToolResult preview+sidecar pattern (preview cap 2048 bytes, spill to `$AURA_RUN_DIR/conversations/<id>/<tool_call>.result`). Ctx-cancel must propagate end-to-end through the HTTP request; OTel span emitted per LLM call.
**Depends on**: Phase 2
**Requirements**: CORE-01
**Slices**: 1
**Success Criteria** (what must be TRUE):

  1. Operator runs `aura llm ping` and observes a streamed response from DeepSeek-V4 Flash via OpenRouter with token + USD cost reported
  2. Operator hits Ctrl+C during a long generation and observes the HTTP request cancelling within 100ms (no orphaned goroutine via `goleak`); `runtime.NumGoroutine()` baseline returns to pre-call value
  3. Operator runs a tool returning 50KB of output and observes the conversation history containing only a ≤2048-byte preview plus a footer pointer; full content readable via `read_tool_output(tool_call_id)`; sidecar file exists at `$AURA_RUN_DIR/conversations/<id>/<tool_call_id>.result`
  4. Operator inspects emitted OTel spans and observes per-LLM-call spans with `model`, `prompt_tokens`, `completion_tokens`, `cache_hit_tokens` attributes

**Plans:** 5/5 plans complete

- [x] 03-01-PLAN.md — PRD amendments A1-A5 + OTel v1.44.0 deps + llm.Config load-order + A3 price table (wave 1)
- [x] 03-02-PLAN.md — handrolled openai_compat SSE client: parser + index accumulator + HTTPError + usage + 5 golden fixtures + ctx-cancel zero-leak (wave 2)
- [x] 03-03-PLAN.md — ToolResult coupled migration + tools.NewResult spillover + read_tool_output (byte ranges) + current_time builtins (wave 1)
- [x] 03-04-PLAN.md — LlmAgent.Run loop + budget terminal Events + per-call OTel span + SpanID minting + byte-stable prompt (wave 3)
- [x] 03-05-PLAN.md — aura chat REPL + cost footer + two-stage Ctrl+C + aura config + manual llm_smoke.sh + live acceptance checkpoint (wave 4)

### Phase 4: HITL + Identity + Conversations

**Goal**: Tight cluster of agent-loop primitives. `ask_user` tool with sentinel `ErrAwaitingUserInput` + FIFO multi-pause persisted in `aura.paused_states`. Identity minimal + `capability_grants` scaffolding (single-user `local` default with wildcard `*`). Conversation persistence multi-thread Claude.ai-style with microcompact L1 + budget L2 + auto-title + per-conversation token+USD aggregation. FTS via `pg_trgm` GIN index on `conversation_turns.content` + `aura chat search` CLI.
**Depends on**: Phase 3
**Requirements**: CORE-02, CORE-03, CORE-04, CORE-05
**Slices**: 1.5, 1.7, 1.8, 1.8.5
**Success Criteria** (what must be TRUE):

  1. Operator triggers `ask_user` during a tool call and observes the loop pausing with a `PausedState` row in `aura.paused_states`; operator answers via CLI and observes the loop resuming from the saved `ResumeContext` with the answer injected as `RoleTool`
  2. Operator triggers 3 simultaneous `ask_user` calls in one turn and observes 3 `PausedState` rows; answering all 3 in FIFO order (priority DESC, created_at ASC) resumes the loop with all 3 answers
  3. Operator runs `aura chat list` and observes multiple persisted conversations with auto-generated titles, per-conversation cumulative token + USD totals
  4. Operator runs `aura chat search "specific phrase"` and observes matching turn excerpts from `pg_trgm` GIN index; same query as Telegram `/search` later returns identical results
  5. Operator inspects `aura.capability_grants` after a fresh boot and observes one row `(identity='local', capability='*')`; `HasCapability("local", "any_tool")` returns true (scaffolding stub working)

**Plans:** 5/5 plans complete

- [x] 04-01-PLAN.md — Substrate: PRD amendments (AM-01/02/03) + tiktoken-go + ContextWindow/MaxOutputTokens + AURA_* env + db.WithTx + migrations 0003-0006 + 6 query files + sqlc regen (wave 1)
- [x] 04-02-PLAN.md — Identity slice (1.7): identity.Store + HasCapability wildcard + grant/revoke idempotency + aura identity CLI (proves Store pattern) (wave 2)
- [x] 04-03-PLAN.md — HITL pause primitive (1.5): ask_user tool + ErrAwaitingUserInput sentinel + Actions.AwaitingInput + llm_agent_pause.go detection + askuser.Store FIFO (wave 2)
- [x] 04-04-PLAN.md — Conversations (1.8): conversations.Store (atomic AppendTurn SC-2, byte-identical LoadHistory, token/USD agg, locked FTS) + context L1/L2/L2.5 (offline tiktoken) + symlink-guarded orphan_scan + auto-title body (wave 3)
- [x] 04-05-PLAN.md — Orchestration: runner.Runner (Turn/SubmitAnswer/Stop, resume-as-fresh-Run SC-4) + aura chat REPL drives Runner + paused-states CLI + chat search + boot composition + microcompact_smoke.sh (wave 4)

### Phase 5: Sandbox 2a Stateless

**Goal**: Python 3.12 slim sidecar for stateless per-call code execution, with a **curated, hash-pinned `sandbox/requirements.txt` baked at IMAGE-BUILD time** (numpy/pandas/scipy/sympy/matplotlib/pillow/beautifulsoup4/lxml/pyyaml/python-dateutil/openpyxl — D-20, "not a toy" user directive) so user code is batteries-included while the **runtime** container stays `network_mode: none` + `read_only: true` + stateless with **no runtime pip** (arbitrary on-demand `pip install` remains a 2b/Phase-8 capability gated by the `AURA_SANDBOX_NETWORK_ALLOW_HOSTS=pypi.org` allowlist; the `sidecar.py` server itself stays stdlib-only). Seccomp **positive allowlist** ~80 syscalls (NOT deny-list — Pitfall #1 fix), ulimits, `network_mode: none` default, `cap_drop: ALL`, `no-new-privileges: true`, `read_only: true`, `userns-remap`, pids_limit. SandboxEscapeBench (UK AISI March 2026) run during Gate 3, escape rate documented in `docs/aura-quality-snapshot.md`. This is the highest-stakes P0 sandbox slice.
**Depends on**: Phase 4
**Requirements**: CAP-01
**Slices**: 2a
**Success Criteria** (what must be TRUE):

  1. Operator runs `aura exec python "print(2+2)"` and observes `4` with the sidecar returning to idle within `AURA_SANDBOX_CALL_TIMEOUT_SEC`
  2. Operator runs `aura exec python "import ctypes; ctypes.CDLL(None).ptrace(0,0,0,0)"` and observes EPERM (ptrace blocked by positive allowlist); same for `open('/proc/self/root/etc/shadow').read()` returning ENOENT/EPERM
  3. Operator runs `aura exec python "__import__('socket').socket().connect(('1.1.1.1',80))"` and observes EPERM (socket syscall absent from allowlist); even `unshare(CLONE_NEWNET)` returns EPERM (allowlist excludes unshare)
  4. Operator runs `scripts/sandbox_escape_bench.sh` (SandboxEscapeBench port) and observes escape rate < 5% recorded in `docs/aura-quality-snapshot.md`
  5. Operator inspects compose service `aura-sandbox` and observes `cap_drop: ALL`, `no-new-privileges: true`, `read_only: true`, `pids_limit: 64`, `userns-remap` (daemon.json) all set; **gVisor `runsc` is default-on x86 (D-05/D-06/D-07 re-decision, amendment #36 — gVisor is the PRIMARY x86 boundary, not a >5%-only escalation seam; container+seccomp+userns-remap is the defense-in-depth floor inside gVisor on x86 and the standalone fallback boundary on arm64). The SandboxEscapeBench escape-rate is measured against the gVisor-primary x86 production profile, with the container+seccomp floor bench-validated as the arm64 fallback.**

**Plans:** 4/4 plans authored (05-04 tasks 1-2 committed; Gate-3 human-verify checkpoint pending live DinD sign-off — CAP-01 not yet closed)

**Wave 1**

- [x] 05-01-PLAN.md — PRD-amendment gate: re-decide D12 to gVisor-primary (D-05/06/07) + Slice 2a acceptance #4 auto-start (D-09) + D-20 build-time package bake across prd.md + DECISIONS.md + ROADMAP.md (doc-only, gates all code waves) — COMPLETE

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 05-02-PLAN.md — Sidecar artifacts: stdlib sidecar.py (/exec/python+/exec/shell, D-16) + python:3.12-slim Dockerfile (non-root, no pip) + multi-arch positive seccomp allowlist (D-10/D-11) + hardened aura-sandbox compose service + runsc overlay (D-04/D-14/D-15)

**Wave 3** *(blocked on Wave 2 completion)*

- [x] 05-03-PLAN.md — Go runner: DockerRunner HTTP client + sentinels (D-09/D-18) + AURA_SANDBOX_* config (D-07) + Deferred:true execute tool with lean preview (D-16/D-17) + aura exec CLI exit-70 (D-19) + sandbox_integration negative-test tier

**Wave 4** *(blocked on Wave 3 completion)*

- [ ] 05-04-PLAN.md — Gate-3 evidence: deterministic 18-scenario escape bench (D-01/D-02) + gating CI DinD workflow (runsc + userns-remap + QEMU arm64, D-11/D-12/D-13) + quality-snapshot escape-rate + human-verify sign-off

### Phase 6: KV Cache Builder

**Goal**: PromptBuilder with stable-prefix discipline. **Two system messages** invariant: `messages[0]` byte-identical turn-on-turn (system + tool manifest, alphabetically sorted); `messages[1]` mutable (Agent.md + cached AgentInsight). Provider-aware `cache_control` injection (Anthropic `ephemeral`, DeepSeek auto + parse `usage.prompt_cache_hit_tokens`, OpenAI prefix-only). Deliberately near-late: must come AFTER P3+P4 so stable prefix is real, not theoretical. Cross-slice CI job `scripts/cache_invariant_audit.sh` runs from this phase onward and gates every subsequent merge.
**Depends on**: Phase 5
**Requirements**: CAP-04
**Slices**: 4
**Success Criteria** (what must be TRUE):

  1. Operator triggers a 20-turn replay via `scripts/cache_invariant_audit.sh` and observes SHA-256(`messages[0]`) constant across all 20 turns (printed to stdout, asserted by the script)
  2. Operator runs `aura chat send "hello"` 3 times in sequence on DeepSeek-V4 Flash and observes `usage.prompt_cache_hit_tokens` rising monotonically from turn 2 onward (cache warming)
  3. Operator inspects the generated prompt for an Anthropic-direct provider and observes `cache_control: {"type":"ephemeral"}` on the system block + tools block, NOT on history messages
  4. Operator runs `aura cache-stats --since=1h` after a typical session and observes cache hit rate ≥ 80% on DeepSeek-V4 Flash (PRD performance target)
  5. CI gate: any PR after Phase 6 that breaks `scripts/cache_invariant_audit.sh` fails the build with an explicit "messages[0] mutated at <site>" error message

**Plans:** 5/5 plans complete

- [x] 06-01-PLAN.md — PRD-amendment gate (doc-only, FIRST): OQ2 in-memory→Postgres cache_metrics (D-02), file-target internal/llm/prompt.go→internal/agent/prompt/ (D-01a), drop cache_deepseek.go, CAP-03→CAP-04 label fix (wave 1)
- [x] 06-02-PLAN.md — PromptBuilder extraction into internal/agent/prompt/ (builder.go + index-set hash.go + dormant anthropic cache_control seam) + llm.Request.ToolsCacheControl + corrected client.go comment, byte-identity preserved (D-01/D-03/D-03a/D-06a, wave 2)
- [x] 06-03-PLAN.md — aura.cache_metrics migration 0007 + sqlc queries + cachemetrics.Store + narrow CacheMetricStore + sibling persist seam + db_integration test (D-02/D-02a, wave 3)
- [x] 06-04-PLAN.md — cmd/aura cache-stats (--since window) + hidden cache-audit (20-turn runner.Turn replay against FakeClient) + in-memory fakes + fixtures + Go-level SC#5 negative test (D-04/D-05/D-06/D-06a, wave 4)
- [x] 06-05-PLAN.md — scripts/cache_invariant_audit.sh wrapper + SC#5 negative test + Postgres-free CI gate wiring (amendment #16, wave 5)

### Phase 7: Web Tools

**Goal**: `web_search` via SearXNG container (privacy-respecting meta-search) and `web_fetch` via `codeberg.org/readeck/go-readability/v2` (Amendment #3 — go-shiori deprecated 2025-12-05) + `JohannesKaufmann/html-to-markdown/v2`. SSRF defense: per-conversation DNS pin (`AURA_WEB_DNS_PIN_TTL_SEC=60`), IPv6 blocklist (`::1/128`, `fe80::/10`, `fc00::/7`, `::ffff:0:0/96`), explicit hostname blocks for cloud metadata services (`metadata.google.internal`, `metadata.amazonaws.com`, `metadata.azure.com`, `kubernetes.default.svc`, `host.docker.internal`).
**Depends on**: Phase 6
**Requirements**: CAP-05
**Slices**: 5
**Success Criteria** (what must be TRUE):

  1. Operator runs `aura tool web_search "Neo4j HNSW vector index"` and observes ranked SearXNG results with title + URL + snippet within p95 ≤ 2s
  2. Operator runs `aura tool web_fetch https://en.wikipedia.org/wiki/Knowledge_graph` and observes clean markdown (no nav, no footer chrome) extracted via readability + html-to-markdown
  3. Operator runs the SSRF integration test (`scripts/ssrf_smoke.sh`) attempting `web_fetch http://169.254.169.254/latest/meta-data/` and observes block with explicit "blocked: cloud metadata" error; same for `http://[::ffff:169.254.169.254]/`, `http://[fe80::1]/`, `http://metadata.google.internal/`
  4. Operator runs the DNS-rebinding test (Python `dnslib` fixture returns 1.2.3.4 then 127.0.0.1) and observes the second `web_fetch` call to the same hostname reusing the pinned IP from the first call within `AURA_WEB_DNS_PIN_TTL_SEC=60`

**Plans:** 4/4 plans complete
**Wave 1**

- [x] 07-01-PLAN.md — Infra + config: SearXNG Compose service (no host port) + read-only settings.yml (JSON) + go-readability/v2 + html-to-markdown/v2 deps + AURA_WEB_*/SEARXNG_URL root config + goleak skeleton (wave 1) ✅ 2026-06-02

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 07-02-PLAN.md — SSRF engine: netip IP classifier (ssrf.go critical) + per-conv DNS pin + pinned-IP transport + CheckRedirect + non-leaky error taxonomy (SC#3/SC#4, wave 2)

**Wave 3** *(blocked on Wave 2 completion)*

- [x] 07-03-PLAN.md — Clients: SearXNG search (query build/parse/domain filter/unavailable) + fetch state machine (scheme/redirect-revalidate/MIME/size gate) + readability→markdown + cache (SC#1-parse/SC#2, wave 3)

**Wave 4** *(blocked on Wave 3 completion)*

- [x] 07-04-PLAN.md — Adapters + surface: Deferred web_search/web_fetch tools + NewResult spillover + aura web CLI (doctor/tool verbs) + ssrf_smoke + live web_integration tier + Gate-3 acceptance (SC#1/2/3/4 live, wave 4)

### Phase 8: Sandbox 2b Session-Bound

**Goal**: Extend Sandbox 2a with session-bound containers per `conversation_id`. Per-conversation workspace mount with `nosuid,nodev,noexec` flags. Network allowlist via iptables (default deny, explicit per-session allow rules). TTL (`AURA_SANDBOX_SESSION_TTL_SEC=1800`). Host-side walkers (`WorkspaceManager.walkSize`, `Conversations.Delete` cascade) use `O_NOFOLLOW` symlink escape guard. Needed by P11 Skills 7e snippet execution.
**Depends on**: Phase 7
**Requirements**: CAP-02
**Slices**: 2b
**Success Criteria** (what must be TRUE):

  1. Operator triggers two `execute` calls in the same conversation and observes both routing to the same long-lived sidecar container; workspace files written in call 1 are visible in call 2 (session-bound persistence)
  2. Operator triggers an `execute` call from the sidecar attempting `ln -s /etc /workspace/escape` followed by host-side `aura chat delete <conv_id>`; observes the host cascade cleanup detecting the symlink and refusing to follow (no host `/etc` deletion); workspace cleared only after `docker exec` rm
  3. Operator inspects `aura sandbox sessions list` after `AURA_SANDBOX_SESSION_TTL_SEC` elapses with no activity and observes the session reaped, container removed, workspace dir cleaned up
  4. Operator triggers `execute` with `network_allow: ["pypi.org"]` and observes `pip install` to pypi.org succeeding; same call attempting `curl 1.1.1.1` observes connection refused (allowlist enforced via iptables)

**Plans**: TBD

### Phase 9: Swarm (Minimal)

**Goal**: Minimal swarm coordinator — reuses `ParallelAgent` from Phase 2 with `MAX_SPAWN_DEPTH=2` cap for v1 (Amendment #12 scope reduction). NO DM-by-ID, NO tier-mapped models in v1 (deferred to post-MVP). Child budget inheritance from parent's remaining (NOT fresh — otherwise depth=3 × 25 = 15625 steps). Multi-pause FIFO propagation through nested children with `proxied_from_child_id` mapping.
**Depends on**: Phase 8
**Requirements**: CAP-03
**Slices**: 3
**Success Criteria** (what must be TRUE):

  1. Operator runs a swarm fixture spawning 3 parallel ParallelAgent children and observes wall-clock < 1.5× single-worker time (race detector clean, `goleak` clean)
  2. Operator attempts spawn at depth 3 and observes explicit error "MAX_SPAWN_DEPTH=2 exceeded" (Amendment #12 cap enforced)
  3. Operator runs `TestSpawnInteractive_MultiPause_AllResolved` (5 children all pause simultaneously, user answers 3 + ctx-cancels 2) and observes all 5 children either resumed or cancelled within 1s — no stuck-paused goroutines
  4. Operator runs a depth-2 swarm with parent budget = 20 steps remaining and observes total steps across the tree ≤ 20 (child budget inheritance correct — NOT 20×2)

**Plans**: TBD

### Phase 10: Scheduler

**Goal**: Cron + persistent `agent_job` queue on Postgres. `FOR UPDATE SKIP LOCKED` initial pickup + `pg_try_advisory_lock(task_hash)` continuous ownership + heartbeat update to `aura.agent_job_runs.last_heartbeat_at` every 30s + boot-time orphan scan (tasks with stale heartbeat > 90s marked `unknown_recovery`). Backup TaskKind handlers (`backup_postgres`, `backup_neo4j`) cron-scheduled. Scheduler-spawned jobs inherit step + wall-clock budget from `agent_job_runs` row.
**Depends on**: Phase 9
**Requirements**: CAP-06
**Slices**: 6
**Success Criteria** (what must be TRUE):

  1. Operator schedules a cron task `aura task schedule --cron "*/5 * * * *" --kind reminder --args '{"text":"test"}'` and observes the task firing exactly once per 5-minute window (no double-execution under concurrent worker setup)
  2. Operator runs the chaos test `scripts/scheduler_chaos.sh` (3 workers, network-partition one for 60s) and observes the partitioned worker's task picked up by a survivor; no duplicate side-effects (idempotency via `agent_job_runs.completed_with_hash`)
  3. Operator observes nightly `backup_postgres` and `backup_neo4j` tasks producing dump artifacts in `$AURA_BACKUP_DIR`; missing the nightly run for 24h triggers an alert log line
  4. Operator triggers a scheduler-spawned agent job with a 10-step budget and observes the job terminating at step 10 (budget inherited from `agent_job_runs`, not a fresh 25)

**Plans**: TBD

### Phase 11: Skills

**Goal**: Skills system instruction-based — 7a loader (FS scan multi-root, TTL cache 1s, YAML frontmatter parser) + 7b validator (NFKC normalization + Unicode TR15 + literal blocklist + 10K fuzz on body) + 7c writer (atomic pending→active + audit trigger with role separation + TRUNCATE trigger fix per Pitfall #6) + 7d installer (`npx skills add --ignore-scripts`, `skill.catalog` hidden behind `aura skills enable-catalog` opt-in per Amendment #14). Plus 7e-core: executable code snippets v1 — save/execute multi-lang via sandbox 2b session-bound + pattern analysis + TTL archived. Cross-conv cluster auto-suggest deferred to 7f (v1.x).
**Depends on**: Phase 10
**Requirements**: CAP-07, CAP-08
**Slices**: 7a, 7b, 7c, 7d, 7e-core
**Success Criteria** (what must be TRUE):

  1. Operator runs `aura skills list` and observes installed skills with name + summary + tier; running `aura skills install <repo>` invokes `npx skills add --ignore-scripts` and persists an `aura.skill_audit` INSERT row
  2. Operator runs `aura skills audit --tier=risky --since=24h` and observes audit rows; attempting `aura skills audit purge` as `aura_app` role observes permission denied (role separation + TRUNCATE trigger enforced — Pitfall #6 fix)
  3. Operator runs the validator fuzz suite (`go test -fuzz=FuzzSkillValidator -fuzztime=60s`) with 10K Unicode mutations of the blocklist patterns and observes every NFKC-collapse-to-blocklist input rejected
  4. Operator saves a Python snippet via `aura skills snippet save --lang python --name fetch-issue` and runs it via `aura skills snippet exec fetch-issue` — observes execution in sandbox 2b session, output captured, TTL archive after `AURA_SKILL_SNIPPET_TTL_DAYS`
  5. Operator runs `aura skills catalog list` on a fresh install and observes "catalog disabled — run `aura skills enable-catalog` to opt in" (Amendment #14 verified)

**Plans**: TBD

### Phase 12: AG-UI Gateway

**Goal**: AG-UI SSE event protocol transport. Thin wrapper over in-process emitter — pure translator `iter.Seq2[*agent.Event, error] → iter.Seq2[agui.Event, error]`. **Boundary enforced**: `internal/agent` MUST NOT import `internal/agui` (verified via static analysis lint in CI). HTTP `POST /agent/run` (SSE) + `GET /threads/<id>/messages`. Pinned to AG-UI Go SDK commit SHA post-2026-05-14 (Amendment #6).
**Depends on**: Phase 11
**Requirements**: UX-01
**Slices**: 8
**Success Criteria** (what must be TRUE):

  1. Operator runs `aura serve` and `curl -N -X POST http://127.0.0.1:9080/agent/run -d '{"thread_id":"t1","messages":[...]}'` and observes a stream of AG-UI events (RUN_STARTED, TEXT_MESSAGE_*, TOOL_CALL_*, RUN_FINISHED) in SSE format
  2. CI gate: any commit attempting to import `internal/agui` from any file under `internal/agent/` fails the build with explicit boundary-violation error
  3. Operator runs `curl http://127.0.0.1:9080/threads/t1/messages` after a streamed run and observes the persisted turn history matching what was emitted in the SSE stream
  4. Operator runs `go.mod` inspection and observes AG-UI SDK pinned to a specific commit SHA (NOT `latest` or pseudo-version `v0.0.0-...`)

**Plans**: TBD

### Phase 13: Channels + Telegram + Multimodal

**Goal**: Channels framework (`internal/channels/<name>/` with `Channel` interface) + Telegram primary user-facing channel via `gopkg.in/telebot.v4` SHA-pinned post-2026-05-08 (Amendment #5) + custom ~80 LOC MarkdownV2 escaper (Amendment #4, supply-chain risk avoidance) + `/cancel`, `/cost`, `/search` commands (Amendment #8). Setup wizard at `http://127.0.0.1:9081/setup` with one-time token `AURA_SETUP_TOKEN` printed to stdout on first boot (Amendment #10) + QR for bot token paste. Multimodal Gemma 4 sidecar (E4B Q4) for STT + image input via `ghcr.io/ggml-org/llama.cpp:server` + markitdown sidecar for document→markdown conversion.
**Depends on**: Phase 12
**Requirements**: UX-02, UX-03, UX-04
**Slices**: 9a, 9b, 9c
**Success Criteria** (what must be TRUE):

  1. Operator runs `aura serve` on first boot and observes `AURA_SETUP_TOKEN=<random>` printed to stdout; navigating to `http://127.0.0.1:9081/setup?token=<token>` shows the wizard with QR code for Telegram bot token paste; second navigation without valid token shows 401
  2. User sends a message in Telegram and observes Aura's streamed reply with status pane + content reply rendered correctly (no `400 Bad Request: can't parse entities` errors — Pitfall #18 escape fuzz green)
  3. User sends `/cancel` in Telegram during a long-running tool call and observes immediate abort + Aura confirmation message; ctx-cancel propagated through HTTP + subprocess + DB query (`goleak` clean)
  4. User sends a voice note and observes Aura transcribing via Gemma 4 STT (multimodal sidecar) and responding; same for an image with caption "what's in this picture?" returning a description
  5. User runs `/cost` and observes today's cumulative USD spend across all conversations; `/search "<query>"` returns matching turn excerpts (FTS from P4 wired into Telegram)

**Plans**: TBD
**UI hint**: yes

### Phase 14: Onboarding + Agent.md

**Goal**: User onboarding flow as `LoopAgent[InterviewStepAgent]` — escalates on "Conferma" via `Event{Actions.Escalate=true}`. Per-identity `Agent.md` profile persisted at `~/.aura/agents/<id>/Agent.md` (filesystem, NOT Neo4j). **Critical**: Agent.md injected as `messages[1]` (NOT `messages[0]`) to preserve KV cache invariant from P6 — verified by `cache_invariant_audit.sh` CI gate.
**Depends on**: Phase 13
**Requirements**: UX-05
**Slices**: 10
**Success Criteria** (what must be TRUE):

  1. First-time user on Telegram observes the onboarding LoopAgent walking through interview questions; user confirms summary; `~/.aura/agents/local/Agent.md` is written to filesystem with extracted facts
  2. Operator inspects the generated prompt for a user with non-empty Agent.md and observes Agent.md content present at `messages[1]` (NOT `messages[0]`); `cache_invariant_audit.sh` 20-turn replay still passes (SHA-256(`messages[0]`) constant)
  3. Operator runs `aura profile show --identity local` and observes the parsed Agent.md sections (preferences, context, custom instructions) rendered as a tree
  4. Operator triggers a profile update via `aura profile add-fact "I prefer Italian responses"` and observes `Agent.md` re-written atomically; next conversation injects the updated content at `messages[1]`

**Plans**: TBD

### Phase 15: Memory Subsystem

**Goal**: Most downstream phase — needs every prior slice. Document ingestion (11a) → chunk + embed via Document → Chunk → Entity pipeline (11b, idempotent via content hash) → entity resolution + community detection via GDS Leiden (11c, UNIQUE constraint + chaos test 10 concurrent goroutines) → GraphRAG hybrid retrieval (11d, HNSW vector + fulltext BM25 + graph traversal + LLM re-rank, HNSW `M=32` per Amendment #20) → agent journal `:AgentEpisode` + `:AgentInsight` (11e, retrieval cached N minutes per Amendment #11 to preserve `messages[2]` KV cache stability). Pre-merge benchmark: recall@5 ≥ 0.8 @ 1K/10K/100K corpus, p95 vector search ≤ 30ms, snapshot recorded in `docs/aura-quality-snapshot.md`.
**Depends on**: Phase 14
**Requirements**: UX-06, UX-07, UX-08, UX-09
**Slices**: 11a, 11b, 11c, 11d, 11e
**Success Criteria** (what must be TRUE):

  1. Operator runs `aura memory ingest <file.pdf>` twice (same file) and observes second run skipped via content-hash dedup; first run produces `:Document` + N `:Chunk` (with 768d `embedding`) + extracted `:Entity` nodes; embed boot assertion (Pitfall #7) confirms `model.output_dim == AURA_EMBED_DIMENSIONS`
  2. Operator runs the entity-resolution chaos test (10 goroutines concurrently ingesting "Mario Rossi" with whitespace + case variations) and observes exactly one `:Entity {name:'mario rossi', type:'Person'}` node afterward (UNIQUE constraint + MERGE pattern correct — Pitfall #15 fix)
  3. Operator runs `aura memory search "<query>"` and observes hybrid retrieval (HNSW + BM25 + 1-hop graph) results with LLM re-rank, recall@5 ≥ 0.8 on 100K-chunk synthetic corpus, p95 ≤ 30ms vector search; result snapshot appended to `docs/aura-quality-snapshot.md`
  4. After a multi-turn conversation, operator observes a new `:AgentEpisode` written to the graph; cross-conv pattern triggers a new `:AgentInsight` node; `aura memory recall-insights --since=7d` returns top-K with retrieval cached for `AURA_AGENT_INSIGHT_CACHE_TTL_SEC` (Pitfall #3 + Amendment #11 — `messages[2]` cache-stable)
  5. CI gate: `docs/aura-quality-snapshot.md` MUST be updated on any P15 PR; missing update fails the build (Amendment #20 quality regression gate)

**Plans**: TBD

## Progress

**Execution Order:**
Phases execute in numeric order: 0 → 1 → 2 → 3 → 4 → 5 → 6 → 7 → 8 → 9 → 10 → 11 → 12 → 13 → 14 → 15

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 0. PRD Amendments | 6/6 | Complete | 2026-05-29 |
| 1. Infra DB + Knowledge | 0/TBD | Not started | - |
| 2. Agent Cornerstone | 8/8 | Complete | 2026-05-30 |
| 3. LLM Client + ToolResult | 5/5 | Complete   | 2026-05-30 |
| 4. HITL + Identity + Conversations | 5/5 | Complete   | 2026-05-30 |
| 5. Sandbox 2a Stateless | 4/4 | Complete | 2026-06-02 |
| 6. KV Cache Builder | 5/5 | Complete    | 2026-06-02 |
| 7. Web Tools | 4/4 | Complete    | 2026-06-02 |
| 8. Sandbox 2b Session-Bound | 0/TBD | Not started | - |
| 9. Swarm (Minimal) | 0/TBD | Not started | - |
| 10. Scheduler | 0/TBD | Not started | - |
| 11. Skills | 0/TBD | Not started | - |
| 12. AG-UI Gateway | 0/TBD | Not started | - |
| 13. Channels + Telegram + Multimodal | 0/TBD | Not started | - |
| 14. Onboarding + Agent.md | 0/TBD | Not started | - |
| 15. Memory Subsystem | 0/TBD | Not started | - |
