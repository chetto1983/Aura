# Architecture Research

**Domain:** Industrial hardening + per-user sandbox isolation + multi-user identity isolation + ToolGateway, integrated into Aura's existing Go single-binary agent substrate
**Researched:** 2026-06-29
**Confidence:** HIGH (integration points verified line-by-line against the live codebase; sandbox-fork external backends MEDIUM pending STACK)

> **Grounding note.** The codebase map in `.planning/codebase/ARCHITECTURE.md` + `STRUCTURE.md` is the 2026-05-28 *skeleton* snapshot and is stale (it predates the whole v0.0.0/v1.0.0 build). This research is grounded in the ACTUAL code at HEAD: `internal/runner`, `internal/agent`, `internal/agent/tools`, `internal/agent/mcptools`, `internal/agui`, `internal/config`, `internal/conversations`, `internal/askuser`, `internal/identityctx`, `internal/identity`, `internal/scoring`, `internal/toolinvocations`, `internal/obs`. Two ground-truth surprises drive the recommendations:
>
> 1. **The sandbox tool is currently DORMANT.** `internal/sandboxagent/` and `internal/agent/tools/sandbox_exec.go` do **not** exist in the live tree (confirmed by `ls`/`grep`). The host `shell_exec` + `fs_*` tools are THE execution surface today (`internal/agent/tools/shell_exec.go` runs `exec.CommandContext` in-process with operator privileges). The rivetdev/sandbox-agent client (`:2468`, `AURA_SANDBOX_AGENT_*`) survives only as a *reference pattern* in `docs/`. **Per-user sandbox is therefore a near-greenfield build, not a swap of an existing live tool.**
> 2. **Identity already flows on the request context.** `internal/agui/auth.go:283` `withPrincipal` stashes the authenticated id on BOTH `principalKey{}` and `identityctx.WithIdentityID(ctx)`. So `identityctx.IdentityID(ctx)` is *already populated* in every authenticated AG-UI handler and propagated into `Runner.Turn` — the multi-user gap is that the **stores and APIs ignore it**, not that the plumbing is missing.

## Standard Architecture

### System Overview — target v2.0.0 integration (new = ★, modified = ◆)

```
┌──────────────────────────────────────────────────────────────────────────────┐
│  CLIENT / TRANSPORT                                                            │
│  cmd/aura (CLI) │ internal/channels/telegram │ internal/agui (AG-UI/SSE/Web)  │
│     identityctx.WithIdentityID(ctx) set at the auth boundary (auth.go:283)     │
│  ◆ conversations_api / approvals_api  → FILTER by identityctx principal        │
│  ◆ /readyz reflects listener + DB + migration + scheduler state (F-008/F-017) │
└───────────────────────────────────┬──────────────────────────────────────────┘
                                     │  ctx carries identity_id + (new) runtime profile
                                     ▼
┌──────────────────────────────────────────────────────────────────────────────┐
│  ORCHESTRATION — internal/runner.Runner                                        │
│  ◆ NewConversation inherits identityctx.IdentityID(ctx)  (fixes F-028)         │
│  ◆ owner-scoped Conv.List/Delete/Get + Pause.ListPendingAll  (F-028/F-039)     │
│  ◆ session eviction on conversation delete (F-039)                             │
│  Owns: per-thread lock, paused_states writer, durable ledger reservation       │
└───────────────────────────────────┬──────────────────────────────────────────┘
                                     │  builds fresh LlmAgent per turn (buildAgent)
                                     ▼
┌──────────────────────────────────────────────────────────────────────────────┐
│  AGENT LOOP — internal/agent.LlmAgent                                          │
│  dispatch (terminal/runnable split, F-003) → executeBatch → runTool → execTool │
│  ◆ runTool/execTool route EVERY tool through  ★ ToolGateway                    │
│     ┌──────────────────────────────────────────────────────────────────────┐ │
│     │ ★ internal/toolgateway.Gateway (the single Policy Enforcement Point)  │ │
│     │   1. PolicyEngine.Decide(actor, profile, spec, args) allow/deny/approve│ │
│     │   2. ledger RESERVE (started) — mutating tools block if reserve fails   │ │
│     │   3. SandboxRouter.Resolve(identityID) → exec target                    │ │
│     │   4. tool.Execute(ctx)  [host | per-user sandbox | MCP | swarm]         │ │
│     │   5. normalize + redact + ledger FINALIZE (succeeded/failed)            │ │
│     └──────────────────────────────────────────────────────────────────────┘ │
│  Existing seams kept: HookManager.BeforeTool/AfterTool, completion gate (F-031)│
└──────────┬───────────────────────────────────┬───────────────────────────────┘
           │                                    │
           ▼                                    ▼
┌────────────────────────────┐   ┌──────────────────────────────────────────────┐
│  EXECUTION BACKENDS         │   │  ★ internal/sandboxrouter                     │
│  shell_exec / fs_* (host)   │   │  identityID → Sandbox endpoint (pool / CRD)   │
│  mcptools.bridgedTool       │◄──┤  lifecycle owned by serve composition root    │
│  swarm_spawn                │   │  Docker-pool route  |  K8s/agent-sandbox route │
└────────────────────────────┘   └──────────────────────────────────────────────┘
           │
           ▼
┌──────────────────────────────────────────────────────────────────────────────┐
│  PERSISTENCE — PG aura.* (sqlc/pgx) │ Neo4j via mcp-neo4j-cypher │ $AURA_RUN_DIR │
│  ◆ tool_invocations (0011) upgraded best-effort → DURABLE reservation (F-020)  │
│  ★ migration 0025+: runtime profile audit, sandbox sessions, retention meta    │
│  ◆ owner-scoped conversation/approval queries  (F-028)                         │
│  ObservabilityLayer: OTel (internal/agent/tracing.go) ◆ extended to MCP/DB/cron│
└──────────────────────────────────────────────────────────────────────────────┘
```

### Component Responsibilities

| Component | Responsibility | New / Modified | Key files |
|-----------|----------------|----------------|-----------|
| `internal/toolgateway` (★ new) | Single policy-enforcement-point: every tool call passes through `Gateway.Execute`. Owns policy decision → ledger reserve → sandbox-route → execute → normalize/redact → ledger finalize | NEW package | `internal/toolgateway/gateway.go`, `policy.go`, `decision.go` |
| `internal/agent.LlmAgent` | Loop driver. `runTool`/`execTool` become the seam where the Gateway is invoked instead of `tool.Execute` directly | MODIFIED | `llm_agent.go:488` (runTool), `llm_agent_retry.go:36` (execTool) |
| `internal/sandboxrouter` (★ new) | Maps `identityctx.IdentityID(ctx)` → a per-identity sandbox endpoint; owns sandbox lifecycle (create/reuse/evict). Backend-pluggable (Docker-pool or K8s) | NEW package | `internal/sandboxrouter/router.go`, `docker.go` OR `k8s.go` |
| `internal/config` runtime profile (★ new, distinct from `internal/profile` which is Agent.md) | Parses + validates `dev`/`local_trusted`/`single_user_hardened`/`server_production`; gates feature wiring at boot; backs `aura config validate --profile` | NEW field + new validation; reuse existing `Validate()`/`GuardWebBind` posture | `internal/config/config.go`, `internal/config/runtime_profile.go` (new) |
| `internal/runner.Runner` | Orchestration: identity inheritance on `NewConversation` (F-028); owner-scoped list/delete; session eviction on delete (F-039); durable ledger reservation gate | MODIFIED | `runner_conversation.go:20`, `runner.go`, `runner_persist.go:180` |
| `internal/conversations.Store` | Add owner-scoped query variants (`ListForIdentity`, `GetForIdentity`, `DeleteForIdentity`) | MODIFIED (+ sqlc queries) | `internal/conversations/store.go:173` |
| `internal/askuser.Store` | Add `ListPendingAllForIdentity` (JOIN conversations on identity_id) | MODIFIED (+ sqlc query) | `internal/askuser/store.go:192` |
| `internal/agui` conv/approval APIs | Read `identityctx.IdentityID(r.Context())` and call owner-scoped store methods | MODIFIED | `conversations_api.go:48`, `approvals_api.go` |
| `internal/toolinvocations.Store` | Add `Reserve`/`Finalize` (or status transitions) so the ledger is a durable reservation, not append-only best-effort | MODIFIED | `internal/toolinvocations/store.go` |
| `internal/obs` + tracing | Extend OTel spans to MCP roundtrips, DB ops, scheduler ticks; standard span attributes (`actor_id`, `runtime_profile`, `policy_decision_id`, `tool_invocation_id`) | MODIFIED | `internal/agent/tracing.go`, `internal/obs/init.go`, `internal/mcp`, `internal/cron` |

## Recommended Project Structure

```
internal/
├── toolgateway/              # ★ NEW — single policy-enforcement-point
│   ├── gateway.go            #   Gateway.Execute(ctx, call, actor) — the seam runTool calls
│   ├── policy.go             #   PolicyEngine.Decide → allow/deny/require_approval
│   ├── decision.go           #   ToolPolicyDecision + ActorContext + reason codes
│   └── ledger.go             #   reserve/finalize bridge over toolinvocations.Store
├── sandboxrouter/            # ★ NEW — identity → sandbox endpoint
│   ├── router.go             #   SandboxRouter interface + Resolve(ctx) (reads identityctx)
│   ├── docker.go             #   Docker-pool backend (route A)  OR
│   ├── k8s.go                #   agent-sandbox CRD/REST backend (route B)
│   └── lifecycle.go          #   create/reuse/evict; owned by serve composition root
├── config/
│   ├── config.go             # ◆ add RuntimeProfile field + profile-gated wiring inputs
│   └── runtime_profile.go    # ★ NEW — profile enum, parse, ValidateProfile(profile)
├── agent/
│   ├── llm_agent.go          # ◆ runTool routes through Gateway (the minimal seam)
│   ├── llm_agent_retry.go    # ◆ execTool becomes Gateway-internal or Gateway-wrapped
│   └── tracing.go            # ◆ add policy/sandbox/actor span attributes
├── runner/
│   ├── runner_conversation.go# ◆ NewConversation inherits identityctx.IdentityID(ctx)
│   ├── runner.go             # ◆ owner-scoped list/get; session eviction on delete
│   └── interfaces.go         # ◆ widen ConversationStore/PauseStore with *ForIdentity
├── conversations/store.go    # ◆ owner-scoped query variants (+ db/queries + migration?)
├── askuser/store.go          # ◆ ListPendingAllForIdentity
├── agui/
│   ├── conversations_api.go  # ◆ filter by principal
│   ├── approvals_api.go      # ◆ filter by principal
│   └── readiness.go          # ◆ add migration + scheduler probes (F-008/F-017)
├── toolinvocations/store.go  # ◆ Reserve/Finalize durable states (F-020)
└── db/migrations/
    ├── 0025_tool_ledger_states.up.sql    # ★ planned|started|succeeded|failed
    ├── 0026_runtime_profile_audit.up.sql # ★ profile resolution + boot validation audit
    └── 0027_sandbox_sessions.up.sql      # ★ per-identity sandbox session + retention meta
```

### Structure Rationale

- **`internal/toolgateway/` as a NEW package, not new methods on `LlmAgent`:** the Gateway is consumed by `internal/agent` but must be definable without importing `agent` (avoid the cycle). Define a narrow `Gateway` interface *where it is consumed* (in `internal/agent`, per `golang-structs-interfaces` "interfaces belong to consumers"), implemented by `*toolgateway.Gateway`. The agent injects a `Gateway` like it already injects `*tools.Registry` and `*HookManager`.
- **`internal/sandboxrouter/` separate from `toolgateway`:** the Gateway *decides* (policy) and the router *resolves* (where to run). Splitting them keeps each ≤600 LOC and lets the sandbox-fork swap (Docker vs K8s) happen behind one interface without touching policy logic.
- **`runtime_profile.go` in `internal/config`, NOT a new package named `profile`:** `internal/profile` already exists and owns Agent.md rendering. Naming a runtime-profile package `profile` would collide. Keep runtime-profile parsing inside `config` (it gates `config`-driven wiring anyway).
- **Owner-scoping as NEW query variants, not mutated signatures:** add `ListForIdentity`/`GetForIdentity`/`DeleteForIdentity` alongside the existing `List`/`Get`/`Delete`. The CLI and background workers (auto-title, scheduler) legitimately run un-scoped; only the AG-UI authenticated path scopes. This avoids a breaking change to every caller.

## Architectural Patterns

### Pattern 1: ToolGateway as the single Policy Enforcement Point (the minimal seam)

**What:** Every tool call — host `shell_exec`/`fs_*`, MCP-bridged tools, `swarm_spawn` — already funnels through ONE method: `LlmAgent.runTool` (`llm_agent.go:488`) → `execTool` (`llm_agent_retry.go:36`) → `tool.Execute`. The Gateway slots in at exactly that choke point. No tool needs to know it exists.

**When to use:** Now — this is the F-001/F-011/F-020/F-031 keystone. It supersedes the scattered enforcement that exists today (the `ShellApprovals` destructive-pattern ledger in `shell_exec.go`, the `HookManager.BeforeTool` veto in `dispatch`, the best-effort `tool_invocations` insert in `runner_persist.go:180`).

**Trade-offs:** + One enforceable boundary, one audit trail, profile-aware default-deny. − A new mandatory dependency in the hot path; it MUST fail-open in `dev`/`local_trusted` to preserve "the host shell IS the capability" (memory `feedback_aura_full_host_terminal_primary`) and default-deny only in `server_production`.

**The minimal seam (illustrative):**
```go
// internal/agent — interface defined where consumed (golang-structs-interfaces)
type ToolGateway interface {
    // Execute decides policy, reserves the ledger, routes to the sandbox, runs,
    // normalizes/redacts, finalizes. Returns the same ToolResult tool.Execute would.
    Execute(ctx context.Context, spec tools.Spec, call llm.ToolCall, actor ActorContext) (tools.ToolResult, error)
}

// llm_agent_retry.go execTool — the ONE line that changes (conceptually):
//   was: res, err = tool.Execute(ctx, args)
//   now: res, err = a.gateway.Execute(ctx, tool.Spec(), call, a.actor(ctx))
// The retry loop stays; the Gateway wraps Execute, NOT the retry, so mutating
// tools still get at-most-once (execTool already refuses to retry Mutating).
```
**Relationship to existing seams (keep all three, do not replace):**
- **HookManager** (`dispatch`, `hooks.go`): stays as the *pre-execution rewrite/veto* hook (FailClosed/FailOpen). The Gateway is downstream of the hook (hooks rewrite the call shape; the Gateway then authorizes the rewritten call). Order: `BeforeTool` (rewrite) → `Gateway.Decide` (authorize) → `Gateway.Execute` (reserve+run) → `AfterTool` (rewrite result).
- **tool_invocations ledger** (`runner_persist.go:180`): today the *runner* inserts best-effort, post-hoc, off the `ToolInvocation` event. v2.0.0 moves the *reservation* into the Gateway (pre-execution `started` row) so a mutating tool is **blocked when reservation fails in `server_production`** (F-020). The runner's existing event-sourced insert becomes the `finalize` write (or is subsumed). This is why **ledger work must land before idempotency** in the build order.
- **Mutating classification / completion gate (F-031):** `Spec.Mutating` (`spec.go:45`) already drives `a.sideEffected` (`dispatch.go:100`) and the completion critic (`llm_agent_completion.go`). The Gateway reads the SAME `Spec.Mutating` bit for its reserve/deny decision — no reclassification, one source of truth. `runToolRecovering` (`llm_agent_parallel.go:71`) already resolves Mutating before exec for panic-safety; the Gateway reuses that.

### Pattern 2: Per-identity sandbox routing — the central fork (TWO fully-sketched designs)

**What:** F-001 is resolved by giving each identity a FULL-capability isolated sandbox (shell/fs/network all intact inside it) instead of stripping capability. The agent still "sees a full host" — it just sees the *user's* sandbox host, never the real one. The host `shell_exec`/`fs_*` tools either run host-direct (dev/local_trusted profile) or are *re-targeted* to the user's sandbox endpoint (hardened/server profile). The re-target point is the same `SandboxRouter.Resolve(ctx)` for both designs; only the backend differs.

**When to use:** `single_user_hardened` + `server_production` profiles. In `dev`/`local_trusted` the router returns the host-direct target (zero behavior change — preserves the core terminal surface).

**Where it slots relative to existing tools:** the router is consumed *inside the Gateway* (step 3). When the decision says "run in sandbox", the Gateway hands the router-resolved endpoint to a sandbox-aware execution path. The cleanest implementation: the host tools (`ShellExec`, `fs_*`) gain an injected `Executor` seam (host-exec by default) that the Gateway swaps for a sandbox-exec client per call, keyed by `identityctx.IdentityID(ctx)`. The dormant rivetdev/sandbox-agent HTTP client (`POST /v1/processes/run` at `:2468`, documented in `docs/aura-toolset-design-claude-code-parity-2026-06-05.md`) is the *reference shape* for that sandbox-exec client.

---
**Route A — K8s / k3s controller (agent-sandbox CRD/REST/MCP):**
```
Aura serve (Go controller)
  └─ internal/sandboxrouter/k8s.go
       ├─ on first call for identity X: create a per-identity Sandbox CR
       │    (kind: Sandbox, spec: full shell+fs, per-user PVC, NetworkPolicy)
       ├─ wait Ready → record endpoint in aura.sandbox_sessions (identity_id PK)
       ├─ shell_exec/fs_* re-targeted: tool.Execute body POSTs to the Sandbox's
       │    exec endpoint (REST) or its MCP exec tool, NOT exec.CommandContext
       └─ evict: delete the CR on idle TTL or conversation delete (F-039)
```
- **New packages:** `internal/sandboxrouter` (k8s backend), a thin k8s client (`client-go` or a REST shim). **Modified tools:** `shell_exec.go`, `fs_*.go` gain an `Executor` interface; the in-process `exec.CommandContext` becomes the default `hostExecutor`, a new `sandboxExecutor` POSTs to the CR endpoint.
- **identity routing:** `SandboxRouter.Resolve(ctx)` reads `identityctx.IdentityID(ctx)` (already on ctx from auth/runner), looks up or creates the Sandbox CR, returns its endpoint.
- **lifecycle ownership:** the **serve composition root** owns the router; the runner does NOT (the runner is per-turn, sandboxes are cross-turn/per-identity). Conversation delete signals the router to evict only when no other live conversation for that identity remains.
- **preserves "full host":** the CR's container is a real full OS userspace; the agent gets an unrestricted shell *inside it*. Capability is never stripped — only the blast boundary moves to the pod.
- **Cost/fit:** heaviest; needs a k3s control plane on the mini-PC or the DGX Spark target. Best for the eventual multi-tenant appliance; overkill for a single-operator box. STACK research should weigh idle RAM (the mini-PC budget is ~6 GB idle per memory `feedback_minipc_cpu_budget`).

**Route B — Pattern-over-Docker (per-identity container pool):**
```
Aura serve
  └─ internal/sandboxrouter/docker.go
       ├─ pool: map[identity_id] → running container (full shell/fs inside,
       │    per-user named volume, network policy = none|allowlist)
       ├─ on first call for identity X: docker run (or reuse) → record endpoint
       ├─ shell_exec/fs_* re-targeted: sandboxExecutor POSTs to the container's
       │    sandbox-agent HTTP endpoint (the dormant :2468 client, revived per-user)
       └─ evict: stop+rm container on idle TTL / conversation delete (F-039)
```
- **New packages:** `internal/sandboxrouter` (docker backend) + revival of the dormant `internal/sandboxagent` HTTP client as the per-container exec transport. **Modified tools:** same `Executor` seam as Route A.
- **identity routing:** identical `Resolve(ctx)` contract; backend resolves identity → container endpoint via the pool map (guarded by `sync.RWMutex`, per `golang-concurrency`).
- **lifecycle ownership:** serve composition root; pool eviction worker with idle TTL (mirror the existing `BackgroundShells` TTL pattern + the cron drain pattern). Named volumes mandatory on Windows (PROJECT.md constraint).
- **preserves "full host":** the per-user container has a full shell/fs; same "host inside the sandbox" property as Route A with far less control-plane weight.
- **Cost/fit:** light; reuses the existing Docker Compose stack and the proven sandbox-agent shape. Best fit for the current mini-PC + the single→few-user reality v2.0.0 actually targets (identity isolation, NO RBAC). **This is the route the integration leans toward** unless STACK surfaces a hard multi-tenant requirement.

**Shared trade-off:** both designs add a per-call network hop for hardened/server profiles. The `dev`/`local_trusted` host-direct path stays in-process (no hop), so the operator's daily experience is unchanged. The `Executor` seam is the single abstraction that makes the two routes interchangeable — commit to it in the roadmap before either backend.

### Pattern 3: Runtime profiles gate boot wiring (fail-fast validation)

**What:** A new `RuntimeProfile` (`dev`/`local_trusted`/`single_user_hardened`/`server_production`) read from `AURA_RUNTIME_PROFILE`. It is parsed in `config.Load`, validated in a new `ValidateProfile`, and gates which features the serve composition root wires (default-deny Gateway policy, sandbox-required executor, CORS, object-store secret rejection). It rides the *existing* fail-fast posture (`config.Validate` at `config.go:264`, `GuardWebBind` at `config.go:291`).

**When to use:** Every boot. `aura config validate --profile server_production` runs the full validation matrix and reports all unmet requirements without starting the daemon.

**Trade-offs:** + One switch separates "trusted local" from "shared production", closing F-002/F-007/F-008/F-016/F-017/F-022/F-041. − Must enumerate every profile-conditional requirement explicitly; a missed one is a false sense of safety.

**Structure (illustrative):**
```go
// internal/config/runtime_profile.go
type RuntimeProfile string
const ( ProfileDev RuntimeProfile = "dev"; ProfileLocalTrusted = "local_trusted"
        ProfileSingleUserHardened = "single_user_hardened"; ProfileServerProduction = "server_production" )

// ValidateProfile returns ALL unmet requirements (not first-fail) so the CLI
// prints a complete checklist — mirrors config.Validate's accumulate-then-join.
func (c *Config) ValidateProfile(p RuntimeProfile) []error {
    var errs []error
    if p == ProfileServerProduction {
        if c.ObjectStoreAccessKey == defaultObjectStoreAccessKey { errs = append(errs, errDefaultObjStoreKey) }
        if c.ObjectStoreSecretKey == defaultObjectStoreSecretKey { errs = append(errs, errDefaultObjStoreSecret) }
        // GuardWebBind already covers a non-loopback bind without auth; reuse it here
        // require sandbox executor (host-direct forbidden), strict secret validation, etc.
    }
    return errs
}
```
`aura config validate --profile X` = a thin cmd wrapper calling `ValidateProfile` and printing the slice (exit non-zero if non-empty). It reuses the existing `config_validate_test.go` accumulate pattern.

### Pattern 4: Observability — OTel spans wrap the same seams that already span LLM/tool

**What:** `internal/agent/tracing.go` already mints a real `TracerProvider` and spans `llm.request` + `tool.execute` (`startToolSpan`/`endToolSpan` in `runTool`). v2.0.0 extends spans to MCP roundtrips (`mcptools.bridgedTool.Execute`), DB ops (sqlc seam), and scheduler ticks (`cron.scheduler`), and stamps the standard attribute set (`run_id`, `conversation_id`, `step_id`, `tool_invocation_id`, `actor_id`, `runtime_profile`, `policy_decision_id`, `mcp_server_id`) — exactly the identifiers the target architecture lists.

**When to use:** Production observability pack (F-023/F-024/F-048). `/readyz` (`readiness.go`, already probe-injectable) gains migration-state + scheduler-state probes alongside the existing PG+Neo4j probes (F-008/F-017). The Gateway is the natural place to stamp `policy_decision_id` + `actor_id` on the tool span.

**Trade-offs:** + Replayable, attributable traces with one root span per run. − Span attribute discipline must be enforced (a new span without `actor_id` is a forensic hole); add a test that asserts required attributes on the root span (parallel to `tracing_spans_test.go`).

## Data Flow

### Request Flow — a mutating tool call in `server_production`

```
authenticated AG-UI request
   ↓ withPrincipal → identityctx.WithIdentityID(ctx) [auth.go:283]
Runner.Turn(ctx, convID)
   ↓ scopeContextToConversation verifies identity owns conv [runner.go:475]
LlmAgent.dispatch  → BeforeTool hook (rewrite/veto)  [dispatch.go:39]
   ↓ executeBatch → runTool [llm_agent.go:488]
   ↓ ★ Gateway.Execute(ctx, spec, call, actor)
       1. PolicyEngine.Decide → require_approval? → persist scoped approval, NO side effect
       2. allow → ledger RESERVE 'started' (block if reserve fails — F-020)
       3. SandboxRouter.Resolve(identityID) → per-user sandbox endpoint
       4. sandboxExecutor.Run(endpoint, command)   [shell_exec re-targeted]
       5. normalize + redact (RedactForLedger) → ledger FINALIZE 'succeeded'
   ↓ AfterTool hook → history append → ToolInvocation event
completion gate (if sideEffected) [llm_agent_completion.go]
   ↓ text_response terminal [dispatch.go:116]
persistAssistantAnswer + cache_metric  [runner_persist.go:249]
```

### State Management — durable ledger reservation (F-020)

```
tool_invocations row lifecycle (migration 0025 adds the state machine):
  planned ─(Gateway reserve)→ started ─(exec ok)→ succeeded
                                  └────(exec fail/timeout)→ failed
  On restart: recover by status (target-architecture.md §Checkpointing):
    planned   → discard or retry-if-idempotent
    started   → tool-specific recovery
    succeeded_unpersisted → reconcile if external proof
    failed    → append normalized failure if not already model-visible
```

### Key Data Flows

1. **Identity isolation (F-028):** `NewConversation` reads `identityctx.IdentityID(ctx)` instead of hard-coding `localIdentityName` (`runner_conversation.go:31`). AG-UI list/get/delete call `*ForIdentity` store variants gated on the principal. `aura.identity_auth_links` (migration 0019) is already UNIQUE on `authula_user_id` (1:N-ready) — no new identity table needed.
2. **Session eviction on delete (F-039):** `Conv.Delete` (or a runner wrapper) signals the `SandboxRouter` to evict the identity's sandbox iff no other live conversation remains, AND clears the per-thread lock in `runner.threadLocks` (`runner.go:162`) + `askuser.AutoResolveForConversation`.
3. **Approval queue scoping (F-028):** `approvals_api` calls a new `askuser.ListPendingAllForIdentity` (JOIN `aura.conversations` on `identity_id`) instead of `ListPendingAll` (`askuser/store.go:192`).

## Scaling Considerations

| Scale | Architecture Adjustments |
|-------|--------------------------|
| 1 operator (today, dev/local_trusted) | Gateway fail-open, host-direct executor, no sandbox hop — zero behavior change. The whole v2.0.0 hardening is *latent* until the profile flips. |
| 2–10 identities (single_user_hardened / small server) | Docker-pool sandbox route (Route B): one container per active identity, idle-TTL evicted. Mini-PC RAM is the bound — cap concurrent live sandboxes (mirror `AURA_SANDBOX_MAX_CONCURRENT_SESSIONS`). |
| Multi-tenant appliance (DGX Spark bundle) | K8s/agent-sandbox route (Route A): per-identity Sandbox CR + PVC + NetworkPolicy, control-plane-managed eviction. The `Executor` seam means this is a backend swap, not a rewrite. |

### Scaling Priorities

1. **First bottleneck: per-identity sandbox memory.** Each live full-capability sandbox is a container/pod with real userspace. Idle-TTL eviction + a concurrent-sandbox cap is the first control. Measure idle footprint before committing the route (STACK).
2. **Second bottleneck: ledger write amplification.** Durable reserve+finalize doubles `tool_invocations` writes vs today's single best-effort insert. The table is append-mostly; partition/retention (F-048) keeps it bounded. The reserve must be a fast single INSERT, not a transaction with the turn.

## Anti-Patterns

### Anti-Pattern 1: Enforcing policy inside each tool

**What people do:** Add capability checks inside `shell_exec.Execute`, `fs_write.Execute`, each MCP bridge.
**Why it's wrong:** N enforcement points drift; MCP-bridged tools (`mcptools.bridgedTool`) and `swarm_spawn` would be missed (exactly the F-001 "tool authority broader than loop authority" finding). The current `ShellApprovals` destructive-pattern gate inside `shell_exec.go` is a symptom of this — it only covers shell, not fs or MCP.
**Do this instead:** One Gateway at `runTool`/`execTool`. Every tool — host, MCP, swarm — already passes through it. Tools stay dumb executors.

### Anti-Pattern 2: Mutating the ToolGateway into the agent as methods

**What people do:** Add `LlmAgent.decidePolicy`, `LlmAgent.reserveLedger` directly on the agent.
**Why it's wrong:** Bloats `llm_agent.go` past the 600-LOC cap, couples policy to the loop, and makes the Gateway untestable in isolation. It also forces `internal/agent` to import the ledger/sandbox stores, widening the dependency graph.
**Do this instead:** `internal/toolgateway` package; inject a narrow `ToolGateway` interface defined in `internal/agent` (consumer-side), satisfied by `*toolgateway.Gateway` built in the serve composition root — same pattern as `*tools.Registry`/`*HookManager` injection in `buildAgent` (`runner.go:565`).

### Anti-Pattern 3: Breaking the host-shell experience to achieve "10/10"

**What people do:** Default-deny the host shell everywhere, fence fs to a workspace in all profiles.
**Why it's wrong:** Violates the core invariant (memory `feedback_aura_full_host_terminal_primary`: "the host shell IS the surface"). The audit's own resolution (PROJECT.md F-001) is *per-user full-capability sandbox*, NOT fencing.
**Do this instead:** Profile-gate it. `dev`/`local_trusted` keep host-direct full capability. Only `single_user_hardened`/`server_production` route to the per-user sandbox — where the agent STILL gets a full shell, just inside its own box.

### Anti-Pattern 4: Scoping by mutating every store signature

**What people do:** Change `Store.List(ctx, includeArchived)` to `Store.List(ctx, identityID, includeArchived)`.
**Why it's wrong:** Breaks every caller (CLI, auto-title worker, scheduler) that legitimately runs un-scoped, and risks an un-scoped default (empty identity = all rows) leaking cross-user.
**Do this instead:** Add `ListForIdentity` variants. The AG-UI authenticated path uses them; un-scoped internal callers keep `List`. Enforce with a two-identity E2E integration test (the F-028 acceptance criterion).

## Integration Points

### External Services

| Service | Integration Pattern | Notes |
|---------|---------------------|-------|
| Per-user sandbox (Docker pool, Route B) | HTTP exec client per container (revive dormant `internal/sandboxagent` shape, `:2468`) | Named volumes mandatory (Windows); idle-TTL eviction; network policy none/allowlist |
| Per-user sandbox (k3s, Route A) | Go controller → Sandbox CRD / REST / MCP exec | Heavier; control-plane on host or DGX Spark; PVC + NetworkPolicy per identity |
| OTel collector | OTLP/gRPC (already wired, `tracing.go`) | Extend instrumentation to MCP/DB/cron; `none` exporter = zero overhead |
| mcp-neo4j-cypher | stdio MCP subprocess (existing, `internal/mcp`) | Add per-server mount timeout, frame cap, process-tree teardown (F-013/F-027/F-037) |
| Authula | embedded auth provider (existing, `internal/webauth`) | Cutover default passphrase→Authula; `identity_auth_links` already 1:N-ready |

### Internal Boundaries

| Boundary | Communication | Notes |
|----------|---------------|-------|
| `internal/agent` ↔ `internal/toolgateway` | narrow `ToolGateway` interface (defined in agent, impl in toolgateway) | Avoids import cycle; same injection pattern as Registry/HookManager |
| `internal/toolgateway` ↔ `internal/sandboxrouter` | `SandboxRouter` interface | Backend (Docker/K8s) swappable; reads `identityctx.IdentityID(ctx)` |
| `internal/toolgateway` ↔ `internal/toolinvocations` | reserve/finalize methods | Durable reservation gate (F-020); the existing runner best-effort insert becomes finalize |
| `internal/runner` ↔ `internal/conversations`/`internal/askuser` | widened narrow interfaces (`*ForIdentity` methods in `interfaces.go`) | Owner-scoping; un-scoped variants retained |
| `internal/agui` ↔ stores | via runner / store `*ForIdentity` using `identityctx.IdentityID(r.Context())` | Identity already on ctx from `withPrincipal`; APIs just need to use it |

## Build Order (dependency-ordered) — what must land before what

> Rule: **ToolGateway + runtime profiles before sandbox enforcement; durable ledger before idempotency; identity scoping is independent and can parallelize.** Migrations and breaking changes flagged inline.

1. **Runtime profiles** (`config.RuntimeProfile` + `ValidateProfile` + `aura config validate --profile`). *No migration; not breaking* (new env, defaults to `local_trusted`). **Foundational** — the Gateway's default-deny and the sandbox executor both key off the profile. Closes F-002/F-007/F-016/F-022/F-041 (validation) early. Also lands the P0 `AURA_SHELL_DESTRUCTIVE_PATTERNS` empty-sample fix (action-plan).
2. **Durable ledger states** (`toolinvocations` reserve/finalize + **migration 0025** state machine). *Migration; not breaking* (additive columns/states). MUST precede the Gateway's reservation gate and ALL idempotency work (F-020).
3. **ToolGateway skeleton + PolicyEngine** (`internal/toolgateway`, narrow interface injected into `LlmAgent`, `runTool`/`execTool` reroute). *No migration; cross-cutting* (touches the hot path; must fail-open in dev/local_trusted to preserve current behavior — guard with the profile from step 1). Reuses `Spec.Mutating` (F-031) + wraps `tool_invocations` (step 2). Closes F-001/F-011 boundary. **Must land before sandbox enforcement.**
4. **Identity isolation** (parallelizable with 1–3): NewConversation identity inheritance (`runner_conversation.go`, **fixes F-028**), owner-scoped store variants (`conversations`/`askuser` + sqlc queries; *possible migration only if an index is added — otherwise none*), AG-UI conv/approval filtering, session eviction on delete (F-039). *Breaking risk contained* by additive `*ForIdentity` variants. Two-identity E2E test is the gate.
5. **Per-user sandbox** (`internal/sandboxrouter` + `Executor` seam in `shell_exec`/`fs_*` + **migration 0027** sandbox sessions). *Migration; cross-cutting* (modifies the keystone host tools). Depends on the Gateway (step 3 routes to it) and profiles (step 1 decides when). **Route A vs Route B decided by STACK** before this lands — but the `Executor` seam is committed regardless. Closes F-001/R-001 fully.
6. **MCP governance hardening** (transport classifier, explicit remote trust, mount timeout, frame cap, process-tree teardown; `internal/mcp` + `mcptools`; **migration 0022 `mcp_audit` already exists**, may extend). *Mostly no migration; not breaking.* Independent of sandbox; can parallelize with 4–5. Closes F-013/F-014/F-027/F-031..F-038/F-046.
7. **Idempotency keys** (pause/resume + tool calls; builds on the durable ledger of step 2). *No new migration if keys ride existing rows; otherwise additive.* Closes F-004/F-005/F-029/F-030. **After the ledger (step 2), not before.**
8. **Observability pack** (OTel span extension to MCP/DB/cron + standard attributes incl. `policy_decision_id` from the Gateway; `/readyz` migration+scheduler probes; retention/cleanup command + **migration 0026** retention meta). *Migration; not breaking.* Best last — it instruments the surfaces the prior steps create (F-008/F-017/F-023/F-024/F-048).

**Cross-cutting / breaking flags summary:**
- **Migrations:** 0025 (ledger states), 0026 (retention/profile audit), 0027 (sandbox sessions); optional index migrations for owner-scoped queries.
- **Cross-cutting (hot path):** ToolGateway (step 3), sandbox `Executor` seam in keystone tools (step 5), OTel attributes (step 8).
- **Behavior-preserving guards:** every step must be a no-op in `dev`/`local_trusted` (Gateway fail-open, host-direct executor) — the operator's daily experience is unchanged; hardening activates only under the production profile.
- **Not breaking (additive):** runtime profile (defaults preserve today), owner-scoped store variants (un-scoped retained), ledger states (additive).

## Sources

- Live codebase (HIGH): `internal/agent/llm_agent.go:488` (runTool), `llm_agent_retry.go:36` (execTool), `llm_agent_dispatch.go` (terminal/runnable split + hook + dedup), `llm_agent_completion.go` (F-031 completion gate), `llm_agent_parallel.go:71` (Mutating pre-resolution), `tools/spec.go:45` (Mutating flag), `tools/shell_exec.go` (host exec, in-process), `agent/mcptools/bridge.go` (MCP tool adaptation), `runner/runner.go:475/565` (identity scope + buildAgent), `runner/runner_conversation.go:31` (F-028 hard-coded local), `runner/runner_persist.go:180` (best-effort ledger), `runner/interfaces.go` (narrow store interfaces), `identityctx/identityctx.go`, `agui/auth.go:283` (withPrincipal sets identityctx), `agui/conversations_api.go` + `approvals_api.go` (unscoped), `agui/readiness.go` (injectable probes), `config/config.go:264/291` (Validate/GuardWebBind), `internal/profile/*` (Agent.md — name-collision avoidance), `internal/scoring/scoring.go` (RiskTier), `agent/tracing.go` (OTel), `db/migrations/0019_authula_schema.up.sql` (identity_auth_links 1:N-ready), `db/migrations/` (24 migrations, next 0025).
- `docs/audit/target-architecture.md` (HIGH) — AgentLoop/ToolGateway/PolicyEngine/SandboxManager interfaces, runtime profiles table, checkpointing states, observability identifiers.
- `docs/audit/architecture-review.md` + `docs/audit/action-plan.md` (HIGH) — weaknesses (implicit capability model, non-atomic host mutation+audit, terminal-not-a-barrier), the four subagent deltas (identity/MCP/pause-resume/lifecycle boundaries), and the medium/long-term ToolGateway + workspace grants + sandbox backend tasks.
- `.planning/PROJECT.md` (HIGH) — F-001 resolution decision (per-user full-capability sandbox, NOT fencing), multi-user = identity isolation NOT RBAC, Authula cutover, sandbox-fork (K8s vs Docker) deferred to research.
- `docs/aura-quality-snapshot.md` + `docs/aura-toolset-design-claude-code-parity-2026-06-05.md` (MEDIUM) — dormant sandbox-agent shape (`:2468`, `/v1/processes/run`, `AURA_SANDBOX_AGENT_*`) as the per-user sandbox-exec client reference.
- Skills (HIGH): `golang-structs-interfaces` (interfaces defined where consumed; accept-interfaces-return-structs — drives the Gateway/Router injection), `golang-concurrency` (sandbox pool sync, eviction worker exit), `golang-context` (identityctx propagation), `golang-database` (sqlc owner-scoped queries, durable ledger transactions).

---
*Architecture research for: Aura v2.0.0 industrial hardening + per-user sandbox + multi-user isolation*
*Researched: 2026-06-29*
