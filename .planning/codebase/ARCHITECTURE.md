<!-- refreshed: 2026-07-17 -->
# Architecture

**Analysis Date:** 2026-07-17

> **Scope + honesty statement (read first).** Every count and claim below was measured
> by a command run on 2026-07-17 against the working tree at branch `master`. Nothing is
> carried over from a prior map or from the roadmap.
>
> **This diagram is NOT exhaustive.** The repo has **68 packages under `internal/`** plus
> **2 under `cmd/`** (`go list ./internal/... | wc -l` → 68; `go list ./cmd/... | wc -l` → 2).
> The System Overview below names ~25 of them. Leaf utilities
> (`boundedbuffer`, `canonicaljson`, `envutil`, `pgnumeric`, `secret`, `obs`, `scoring`,
> `agentrender`, `askuser`, `profile`, `settings`, `setup`, `rerank`, `semindex`,
> `activelearn`, `reasoningfifo`/`reasoninglearn`/`reasoningstore`/`reasoningtrace`,
> `toolselectlearn`/`toolselectstore`, `skilladapters`, `neostore`, `multimodal`,
> `cachemetrics`, `memory`, `eval`, and others) are deliberately omitted from the picture.
> **Their absence from the diagram is not evidence they do not exist.** Use
> `go list ./internal/...` as the authoritative package inventory, never this document.

## System Overview

```text
┌──────────────────────────── ENTRY POINTS / CHANNELS ────────────────────────────┐
│  cmd/aura (CLI, 13,745 LOC)   │  internal/agui (HTTP API   │ internal/channels/  │
│  chat · serve · db · neo4j    │  + SSE, 55 files,          │ telegram            │
│  mcp · tools · doctor · web   │  10,111 LOC)               │ (5,795 LOC)         │
│  `cmd/aura/main.go`           │  `internal/agui/server.go` │                     │
└──────────┬────────────────────┴───────────┬────────────────┴──────────┬─────────┘
           │                                │                           │
           │   internal/webui = LEAF: embeds the built Vite dist        │
           │   (//go:embed all:dist), imports ONLY stdlib               │
           │   `internal/webui/embed.go` ← web/ (Vite+React, outDir     │
           │                                 ../internal/webui/dist)    │
           ▼                                ▼                           ▼
┌───────────────────── COMPOSITION ROOTS (the 3 NewLlmAgent sites) ────────────────┐
│  internal/runner          │  internal/swarm            │  internal/cron/handlers │
│  `runner.go:559`          │  `swarm.go:172`            │  `handler.go:124`       │
│  interactive, per-turn    │  fan-out workers           │  scheduled jobs         │
│  Gateway @ runner.go:571  │  Gateway @ swarm.go:182    │  Gateway @ handler.go:134│
└──────────┬────────────────┴──────────────┬─────────────┴───────────┬─────────────┘
           │                               │                         │
           └───────────────┬───────────────┴─────────────────────────┘
                           ▼
┌──────────────────── AGENT RUNTIME (internal/agent, 5,276 LOC) ───────────────────┐
│  LlmAgent loop: consume → dispatch → execTool → finalize                          │
│  `internal/agent/llm_agent.go` · `llm_agent_dispatch.go` · `llm_agent_retry.go`   │
│  MUST NOT import internal/agui (one-way boundary, CI-enforced)                    │
└──────────┬───────────────────────────────────────────────────────────────────────┘
           │  execTool interposes the PEP  (`llm_agent_retry.go:52`)
           ▼
┌──────────────── POLICY ENFORCEMENT POINT (internal/gateway, 1,174 LOC) ──────────┐
│  classify.go  →  decide.go  →  approve.go  →  reserve.go                         │
│  branches on config.RuntimeProfile; nil Gateway / non-Strict = Allow no-op        │
└──────────┬───────────────────────────────────────────────────────────────────────┘
           │  g.reserve() delegates the DURABLE write ↓
           ▼
┌──────────────────────── PERSISTENCE / EXTERNAL ──────────────────────────────────┐
│ Postgres (40 migrations, owner-RLS @0032)  │ Neo4j (2 cypher, HNSW 768-d)         │
│ `internal/db` + `internal/db/sqlc` (gen)   │ `internal/knowledge`, `internal/neostore` │
│ `internal/toolinvocations` (append ledger) │ objectstore (per-identity buckets)   │
│ sandbox/usersandbox (Docker, per-user)     │ mcp / llm / documents / assets       │
└──────────────────────────────────────────────────────────────────────────────────┘
```

## Component Responsibilities

Selected components only — see the scope statement above.

| Component | Responsibility | File |
|-----------|----------------|------|
| CLI entry | Sub-command dispatch (`switch os.Args[1]`), `godotenv.Load()` | `cmd/aura/main.go` |
| Agent loop | LLM turn loop, tool dispatch, retry, pause/finalize | `internal/agent/llm_agent.go` |
| PEP intercept | `execTool` — where the Gateway is interposed above `tool.Execute` | `internal/agent/llm_agent_retry.go:52` |
| Gateway core | Profile + ledger seam + approval carrier; `New()` | `internal/gateway/gateway.go:115` |
| Gateway decide | The PEP proper; profile branch, classify, route, reserve | `internal/gateway/decide.go:30` |
| Ledger primitive | **Exported** `Reserve` — the durable INSERT + rows-affected idempotency key | `internal/toolinvocations/store_reserve.go:28` |
| Runtime profile | `RuntimeProfile` enum, total `ParseProfile`, `Strict()` | `internal/config/config_runtimeprofile.go:20` |
| RLS carrier | `WithIdentityTx` — sets `app.current_identity` GUC per tx | `internal/db/tx.go:55` |
| Tx seam | `WithTx` — the DRY multi-statement write seam | `internal/db/tx.go:22` |
| Interactive root | Per-turn agent construction + auto-title + resume | `internal/runner/runner.go` |
| Web API | Cockpit's entire HTTP surface (55 files) | `internal/agui/server.go` |
| Static host | Embedded Vite dist, SPA fallback; **stdlib-only leaf** | `internal/webui/embed.go` |
| Per-user sandbox | Docker backend, egress, materialize, reap | `internal/sandbox/usersandbox/router.go` |

### Correcting a known mis-attribution

An earlier agent attributed `Reserve` to the `gateway` package. **Verified today — both
exist and they are different things:**

- `internal/toolinvocations/store_reserve.go:28` — `func (s *Store) Reserve(...) (acquired bool, replay *Event, err error)`.
  This is the **only** exported `Reserve` in the repo (`grep -rn "func .*) Reserve(" internal/ --include='*.go' | grep -v _test`
  returns exactly this one line). It owns the durable ledger write.
- `internal/gateway/reserve.go` — `func (g *Gateway) reserve(...)`, **lowercase/unexported**.
  It is gateway-side *orchestration* that calls the store seam.

So: the gateway orchestrates the reservation; `toolinvocations` owns it.

## Pattern Overview

**Overall:** Layered modular monolith — a single Go binary (`cmd/aura`) with
`internal/*` packages, fronted by an embedded SPA, with sidecars (Postgres, Neo4j,
Docker sandbox, MCP servers, llama.cpp) reached over the network.

**Key Characteristics:**
- **Three composition roots, one agent runtime.** `runner`, `swarm`, `cron/handlers`
  are the only `agent.NewLlmAgent(...)` call sites (verified by grep). Cross-cutting
  policy is injected at all three rather than embedded in the agent.
- **Narrow interface seams over concrete stores.** The Gateway holds a
  `reservationStore` interface (`internal/gateway/gateway.go:27`), not a `*Store`.
- **Nil-as-no-op degradation.** `A nil *Gateway` and any non-`Strict()` profile
  short-circuit to `Allow` with no store write (`decide.go:31`).
- **One-way transport boundary.** The agent runtime never depends on its transport.
- **Generated code is quarantined.** `internal/db/sqlc` (7,046 LOC) is sqlc output.

## Layers

**Entry / channel layer:**
- Purpose: accept work from an operator or a schedule
- Location: `cmd/aura`, `internal/agui`, `internal/channels/telegram`
- Depends on: composition roots
- Used by: humans, HTTP clients, Telegram

**Composition-root layer:**
- Purpose: build a per-turn agent and inject cross-cutting deps (Gateway, breaker, stores)
- Location: `internal/runner`, `internal/swarm`, `internal/cron/handlers`
- Contains: `Deps` structs, wiring, lifecycle (`Stop`, drain timeouts)
- Depends on: `internal/agent`, `internal/gateway`, `internal/llm`, stores

**Agent-runtime layer:**
- Purpose: the LLM loop and tool dispatch
- Location: `internal/agent` (+ `tools`, `prompt`, `mcptools`, `workflow`, `display`)
- Depends on: `internal/llm`, `internal/agent/tools`, `internal/gateway`
- **Must not depend on:** `internal/agui`

**Policy layer:**
- Purpose: classify → decide → approve → reserve, per runtime profile
- Location: `internal/gateway`
- Depends on: `internal/config`, `internal/scoring`, `internal/toolinvocations`

**Persistence layer:**
- Location: `internal/db` (+ generated `internal/db/sqlc`), `internal/knowledge`,
  `internal/neostore`, `internal/objectstore`, `internal/toolinvocations`

## Data Flow

### Primary interactive turn

1. Operator input enters via `cmd/aura` chat REPL or an `agui` HTTP route (`internal/agui/server_run_request.go`)
2. `Runner.Turn` acquires the per-thread lock; `WithThreadLockHeld` lets an HTTP gateway reject busy threads up front (`internal/runner/runner.go:53`)
3. A **fresh per-turn** `LlmAgent` is constructed (`internal/runner/runner.go:559`) with the shared `*llm.Breaker` and the `*gateway.Gateway` (`runner.go:571`)
4. The agent streams from the LLM and dispatches tool calls (`internal/agent/llm_agent_dispatch.go`)
5. `execTool` calls `Gateway.Decide` **before** `tool.Execute` (`internal/agent/llm_agent_retry.go:52` → `internal/gateway/decide.go:30`)
6. Decide branches:
   - nil Gateway or `!profile.Strict()` → `Allow`, no store write (dev/local_trusted parity)
   - read-only tool → `recordDecisionFact` (a **start** row, never an end row) → `Allow` (`decide.go:67`)
   - mutating + `scoring.GateRecommended(tier)` → `routeApprove` (`approve.go`)
   - all mutating-Allow paths converge on one `g.reserve` (`decide.go:56`) → `toolinvocations.Store.Reserve`
7. `Reserve` rows-affected is the idempotency key: rows==1 acquire → execute; rows==0 → replay the recorded end, **do not** re-execute; error → fail-closed Deny
8. Events fan out; `agui` translates agent events to SSE (`internal/agui/translator.go`, `fanout.go`)

### Owner-scoped web read/mutate (Phase 36)

1. Request authenticated via Authula (`internal/webauth/authula.go`, `session_validate.go`)
2. Identity placed on ctx (`internal/identityctx/identityctx.go`)
3. Store call routes through `db.WithIdentityTx(ctx, pool, identityID, fn)` (`internal/db/tx.go:55`)
4. `set_config('app.current_identity', …, is_local => true)` scopes the GUC to **that tx only** — a bare `SET` would leak onto the pooled connection
5. Migration `0032_owner_rls.up.sql` policies filter every statement to the caller's rows

**RLS semantics (read from the migration, not assumed):** fail-closed on *mismatch*,
**permissive on unset** — `NULLIF(current_setting('app.current_identity', true), '') IS NULL`
means "no identity context" resolves to the legacy unscoped path. `ENABLE`, not `FORCE`:
`aura_migrate` owns the tables and bypasses RLS by design; `aura_app` is a non-owner
non-`BYPASSRLS` role, so `ENABLE` suffices for the runtime pool.

### Conversation compaction ladder (Phase 42)

The context ladder is **L1 microcompact → L2 budget gate → L2.4 LLM compaction →
L2.5 oldest-pair drop**. L2.5 is a *degradation*, not a normal rung: it requires a
validated `L24Waiver` (`internal/conversations/compaction_budget.go:25`), whose closed
vocabulary is `disabled`, `no_eligible_prefix`, `provider_unsupported`,
`quality_rejected`. `ErrInvalidL24Waiver` rejects degradation without an allowed L2.4
outcome (`compaction_budget.go:22`).

**State Management:** Postgres is the durable store; per-turn agents are stateless and
rebuilt each round. Cross-turn approval state lives in the Gateway's `GatewayApprovals`
carrier — deliberately **outside** the tool registry, which is why `EvictSession` must be
called explicitly (`gateway.go:156`).

## Key Abstractions

**`RuntimeProfile`** (`internal/config/config_runtimeprofile.go:20`):
- Values: `dev` | `local_trusted` | `single_user_hardened` | `server_production`
- `Strict()` is true for the **latter two only** (`config_runtimeprofile.go:56`)
- `ParseProfile` is **total**: unknown/empty → `ProfileDev`, never a stricter tier
- **Posture is a config axis, not a constant.** "Full host access by design" describes
  `dev`/`local_trusted` only.
- ⚠️ **Documented tension, verified today:** `config_runtimeprofile.go:19` says the profile
  "does NOT itself enforce any runtime capability (Tool Gateway / Phase 35+)", yet
  `gateway/decide.go:31` branches directly on `g.profile.Strict()`. The comment predates
  Phase 35. **Treat `decide.go` as the truth.** Not resolved — flagged, not fixed.

**`Verdict` / `Decision`** (`internal/gateway/gateway.go:33-72`):
- `Allow` | `Deny` | `Approve`. `Approve` is **not** a pause sentinel — it carries an
  `ApprovalRequest` returned as a *normal* tool result; the model relays via `ask_user`
  and retries the exact call.

**`Deps`** (`internal/runner/runner.go:66`): constructor-injection struct; most fields are
nil-tolerant with a documented degradation.

## Entry Points

**`cmd/aura`** (13,745 LOC):
- Location: `cmd/aura/main.go`; dispatch is a hand-rolled `switch os.Args[1]`, **not** Cobra
  (verified: `grep "Use:" cmd/aura/*.go` returns nothing)
- Sub-commands per the file header: `serve`, `shell`, `agent dry-run`, `web`, `doctor`,
  `tools`, `db`, `neo4j`, `identity`, `profile`, `paused-states`, `chat`, `version`; the
  `main` switch also routes `mcp`, `memory`, `swarm-demo`
- `godotenv.Load()` runs first (`main.go`)

**`cmd/compaction-test-worker`**: second binary, multi-process compaction-claim testing.

**`internal/agui`**: the cockpit HTTP API — auth, onboarding, governance, documents,
assets, approvals, graph, connect, voice, settings, audit, storage-orphans. **Not** a
one-way SSE bridge.

## Architectural Constraints

- **Import boundary (CI-enforced, partially).** `scripts/agui_boundary_check.sh` enforces
  exactly **two** invariants via dependency *closure* (`go list -deps`, not a source grep):
  1. `internal/agent` must not transitively import `internal/agui`
  2. `internal/webui` must import **no** other `internal/*` package (stdlib-only leaf)

  ⚠️ **The briefing for this map claimed the script also covers `internal/swarm` and
  `internal/runner`. It does not** — I read the script. Verified separately today:
  neither `runner` nor `swarm` imports `agui` (`go list -deps ./internal/runner/... | grep -x .../agui`
  → no match; same for swarm). **So the invariant holds today for runner/swarm but is
  NOT CI-enforced for them** — a future import would compile and pass CI. Real importers
  of `agui` today: `cmd/aura`, `internal/breakglass`, `internal/channels/telegram`, `internal/obs`.
- **NO GOD CLASS ≤600 LOC — holds for all hand-written code.** The only files over 600
  LOC are sqlc-generated: `internal/db/sqlc/document_control_plane.sql.go` (1,037),
  `models.go` (744), `assets.sql.go` (722).
- **Threading:** per-thread runner lock serializes runs per conversation
  (`ErrThreadBusy`, `internal/runner/runner.go:46`); bounded drain on `Stop`
  (`defaultStopTimeout` 10s, `runner.go:34`).
- **Global state:** the LLM circuit breaker is deliberately process-lifetime and shared
  across turns (`Deps.Breaker`, `runner.go:84`).
- **Circular imports:** none — Go forbids them; not otherwise audited.

## Anti-Patterns

### Attributing `Reserve` to the gateway

**What happens:** Docs say the Gateway "reserves in the ledger", implying `Reserve` lives in
`internal/gateway`.
**Why it's wrong:** The exported durable primitive is `internal/toolinvocations/store_reserve.go:28`.
`gateway/reserve.go` holds only the unexported `g.reserve` orchestrator. Chasing the wrong
package wastes time and hides the real ledger owner.
**Do this instead:** Gateway orchestrates; `toolinvocations` owns the durable write.

### Recording a decision-fact as an `end` row

**What happens:** Logging a read-only Allow as an `end` event.
**Why it's wrong:** It pre-claims the `(conv,req,toolCall,'end')` slot, so the async
observer's real outcome is silently lost to `ON CONFLICT DO NOTHING` (`decide.go:59-66`).
**Do this instead:** Decision-facts are **start** rows. A start races harmlessly.

### Assuming "full host access by design"

**What happens:** Treating the permissive posture as the system's identity.
**Why it's wrong:** It is `dev`/`local_trusted` only. Under `single_user_hardened` /
`server_production`, `Strict()` is true and the mutating funnel fails closed.
**Do this instead:** Branch on `profile.Strict()` (`config_runtimeprofile.go:56`).

### Using a bare `SET` for the RLS identity

**What happens:** `SET app.current_identity` outside a tx-local scope.
**Why it's wrong:** It leaks the identity onto the pooled connection and the next borrower —
an elevation-of-privilege (`internal/db/tx.go:41-54`).
**Do this instead:** `db.WithIdentityTx`, which uses `set_config(..., is_local => true)`.

## Error Handling

**Strategy:** typed sentinel errors + fail-closed policy defaults.

**Patterns:**
- Typed error carrying policy context: `*gateway.ErrDenied{Reason, Tier}` (`gateway.go:87`)
- Sentinels: `runner.ErrThreadBusy` (`runner.go:46`), `conversations.ErrInvalidL24Waiver`
- **Fail-closed:** a reservation that cannot be durably taken blocks execution even
  post-approval (`gateway/reserve.go` doc comment; `decide.go`)
- **Fail-soft where it is observability only:** a decision-fact insert failure is a `WARN`,
  the Allow still stands (`decide.go:84`)
- Panic-safe tx: `WithTx`/`WithIdentityTx` rollback then **re-panic**, never swallow (`tx.go:27`)

## Cross-Cutting Concerns

**Logging:** `log/slog` throughout.
**Policy/authorization:** `internal/gateway` (tool calls) + Postgres RLS (data rows) —
two independent layers; the RLS is explicitly a *backstop* to the app-level filter, not a
replacement (`tx.go:52-54`).
**Authentication:** Authula (`internal/webauth`), identity on ctx (`internal/identityctx`),
break-glass path (`internal/breakglass`).
**Observability:** `internal/obs`, `internal/agent/panicobs`, `internal/cachemetrics`.

## Testing posture that affects architecture

- Build tags in `internal/` (counted today): `db_integration` (86), `cot_eval` (15),
  `docker_integration` (9), `neo4j_integration` (8), `reasoning_live` (5), `live_e2e` (5),
  `web_integration` (2), `retrieval_eval` (2), `windows` (2).
- ⚠️ **`internal/sandbox/usersandbox` (1,545 LOC) has ZERO CI coverage.** Its Docker
  runtime is `docker_integration`-tagged and **there is no `docker_integration` CI job** —
  those tests compile and skip. Lifecycle/exec/egress paths are effectively untested in CI.
  Do not read this package's test-file count as coverage.

---

*Architecture analysis: 2026-07-17*
