# Feature Research

**Domain:** Industrial hardening + multi-user identity isolation for a Go-native single-binary agentic substrate (Aura v2.0.0). Capability classes: ToolGateway/PEP, runtime profiles, per-user sandbox lifecycle, multi-user identity isolation, capability eval suite, production observability.
**Researched:** 2026-06-29
**Confidence:** HIGH on the capability-class shapes (multiple converging industry sources + mature reference implementations); MEDIUM on specific Aura-internal partial-coverage notes (verified against source where load-bearing); HIGH on audit-finding mappings (read directly from `docs/audit/bug-report.md`).

---

## How to read this document

This is a **subsequent-milestone hardening** research, not greenfield. "Table stakes" here means **"required to honestly claim 10/10 production-readiness for this capability class"** — not "required for an MVP". Aura is already feature-complete; v2.0.0 closes the 51-finding 2026-06-21 audit. Every feature is traced to the audit finding ID(s) it closes (F-001..F-052) and to existing Aura components it depends on or extends.

**Operator constraint (non-negotiable):** minimal-industrial-form, no "atomic bombs", no RBAC/roles/OAuth scope creep, no new product features. Multi-user = **identity isolation only**. The full-host terminal is the core surface and is never removed — F-001 is resolved by **per-user full-capability isolated sandbox** (agent-sandbox-class), not by stripping capability.

Each category below has its own table-stakes / differentiator / anti-feature breakdown, then a consolidated dependency graph, MVP ordering, and prioritization matrix at the end.

---

## Category 1 — ToolGateway / Policy-Enforcement-Point

**What mature systems do.** The industry has converged on a single Policy-Enforcement-Point (PEP) + Policy-Decision-Point (PDP) chokepoint through which every tool call passes synchronously before it reaches the wire. The verdict vocabulary is consistently three values: **allow / deny / require-approval (pending)** (Microsoft Agent Governance Toolkit, OpenPort Protocol, AEGIS, PolicyLayer all agree). The decision is recorded in an **immutable audit record** capturing what was attempted, which policy fired, the verdict, the context, and the timestamp. Crucially, this is implemented in **deterministic application code**, not prompt-level guardrails — the recurring research finding is that prompt-level safety hits ~100% attack-success-rate, so enforcement must be structural.

**The bottleneck question is answered.** AEGIS measured **8.3 ms median / 23 ms P99** added latency per tool call across 1,000 calls — negligible against 1,000–30,000 ms of LLM inference. The techniques that keep it cheap: **policy compiled-and-cached once** (no per-request recompile), a per-agent rate limiter, and a lightweight in-process decision path. So "ToolGateway becomes a bottleneck" is a non-risk if you cache the compiled policy. This directly de-risks the operator's over-engineering concern.

**Minimal industrial form (operator-aligned).** AEGIS draws the essential/optional line cleanly: **essential** = interception + a policy engine + allow/deny + tamper-resistant audit trail. **Optional** = HITL approval workflows, dashboards, NL policy authoring, PII redaction, anomaly alerting. Aura already has the optional pieces (HITL, governance UI). What it lacks is the *mandatory single chokepoint* — today policy is scattered across shell destructive-patterns, command hooks, sandbox escalation, MCP trust, and a best-effort ledger. The minimal form is **one in-process `ToolGateway.Decide(ctx, call) → Decision` seam** that every tool dispatch funnels through, returning a logged decision, with a **durable pre-execution ledger reservation for mutating tools**. Do **not** build OPA/Cedar, a separate gateway process, Merkle-tree tamper-evidence, or an SDK — those are atomic bombs for a single-binary substrate.

### Table Stakes

| Feature | Why Expected | Complexity | Closes | Aura partial coverage |
|---------|--------------|------------|--------|----------------------|
| Single in-process PEP every tool call passes through (`Decide` → allow/deny/approve) | "No tool executes without a recorded policy decision" is the load-bearing 10/10 claim; today policy is scattered | HIGH | F-001, F-020 | None — policy currently scattered across shell/hook/sandbox/MCP. This is the keystone feature. |
| Policy decision **logged for every call** (read-only included), not just mutating | Auditability + the eval suite asserts on it | MEDIUM | F-001, F-011 | Partial — `tool_invocations` ledger exists but is best-effort + mutating-ish |
| **Durable mutating-tool ledger** with started/succeeded/failed state machine; reservation required *before* side effect in hardened/server profiles | Forensic guarantee; recovery can infer which side effects occurred | MEDIUM | F-011, F-020 | Partial — `internal/toolinvocations` has start/event rows + a CHECK constraint but **insert failure is logged not blocking** (F-011) |
| **Idempotency keys** for mutating tools (or explicit non-retryable + durable state) | Replay/duplicate-effect on retry is a recognised threat class (OpenPort lists it as a security threat) | MEDIUM | F-020 | None |
| Per-tool **timeout** enforced at the gateway (not per-tool ad hoc) | Uniform liveness; one place to reason about hangs | LOW | F-019, F-033 | Partial — some tools self-timeout; MCP mount has no per-server deadline (F-033) |
| **Sandbox selection** decided at the gateway (which sandbox / host / which user's sandbox) | The gateway is where "run this in user B's sandbox" is decided | MEDIUM | F-001 | None (depends on Category 3) |
| **Result normalization** — uniform success/error/redaction shape returned to the loop | Consistent downstream handling; secret redaction before persistence | MEDIUM | F-019, F-021 | Partial — `toolinvocations/redact.go` redacts args/results; not a uniform gateway result type |
| Terminal `text_response` cannot co-execute with mutating siblings | Final-answer must not hide side effects | MEDIUM | F-003 | None — bug: runnable siblings execute then terminal runs (F-003) |
| Mutating classification **survives panic** in the recovery result | Completion-gate must not be skipped after partial side effect + panic | LOW | F-031 | None — `runToolRecovering` drops `Mutating=true` (F-031) |
| Command **capability classes** (read-only vs mutating vs network vs destructive) drive the approval requirement | Policy keyed on class, not regex of command text | MEDIUM | F-001, F-002 | Partial — destructive-pattern list exists but `.env.example` disables it (F-002) |

### Differentiators

| Feature | Value Proposition | Complexity | Notes |
|---------|-------------------|------------|-------|
| Gateway-level **per-tool / per-actor rate limit** (e.g. 100 calls/min like AEGIS) | Caps runaway loops + cross-user blast radius cheaply | LOW | One token bucket per actor; pairs with loop-liveness eval |
| **Policy decision evidence** exported as a queryable record (decision + active policy + context) | Lets the eval suite and operator prove "this was denied because X" | MEDIUM | Microsoft AGT calls these "decision records"; cheap once the ledger exists |
| **Fail-closed gateway in server_production**: a tool with no policy decision = denied | Structural safety; "denied actions are structurally impossible" | LOW | Profile-gated default; dev stays permissive |

### Anti-Features (do NOT build)

| Feature | Why Requested | Why Problematic | Alternative |
|---------|---------------|-----------------|-------------|
| External gateway **process** / sidecar PEP | "Industry uses gateways" | Breaks single-binary invariant; adds a network hop + a thing to operate | In-process `ToolGateway` seam — same guarantee, zero ops |
| **OPA / Cedar / Rego** policy engine | "Real policy engines" | Atomic bomb for a single operator; a new DSL + runtime to maintain | Go-typed policy structs + capability classes, compiled once |
| **Merkle-tree / Ed25519 hash-chained** tamper-evident audit | AEGIS does it | Over-engineered for a self-hosted single-tenant appliance; key-mgmt burden | Append-only Postgres ledger with NOT-NULL CHECK (already the pattern) |
| Per-call **LLM-judge** risk scoring in the hot path | "Smarter policy" | Adds 1–30 s latency to every tool call — the bottleneck the gateway is meant to avoid | Deterministic class + pattern checks (8 ms), LLM only for offline eval |

---

## Category 2 — Runtime Profiles (validated config contract)

**What mature systems do.** Production-readiness reviews universally express "production mode" as an **enforceable contract**, not documentation. The pattern is a named profile (`dev` / `local_trusted` / `single_user_hardened` / `server_production`) plus a `validate` command that **fails on unmet requirements**. The recurring checklist items across Cortex, Port, getDX, Auth0, and Kubernetes production-readiness guides: no default/sample secrets, auth enabled, CORS restricted to known origins (not wildcard), health/readiness probes wired, fail-fast on malformed config, and storage redundancy. Microsoft AGT ships exactly this as `agt verify --strict` with evidence-JSON export for CI gates. This is the single highest-leverage, lowest-complexity capability in the milestone — it converts ~7 audit findings into one validated command.

**Minimal industrial form.** A `Profile` enum + a `Validate(profile) → []Violation` function + an `aura config validate --profile server_production` command that exits non-zero on any violation and prints all of them. Each profile is a **set of required predicates**, not a config framework. Dev profile = warnings; server_production = hard errors. This is the operator's preferred shape exactly: a contract that fails loudly, no machinery.

### Table Stakes

| Feature | Why Expected | Complexity | Closes | Aura partial coverage |
|---------|--------------|------------|--------|----------------------|
| `Profile` first-class config field (`dev`/`local_trusted`/`single_user_hardened`/`server_production`) | The contract needs a name to key requirements off | LOW | F-026 | None — "Profile" in config today = Agent.md identity profile, **not** a runtime profile. Genuinely new. |
| `aura config validate --profile <p>` that **reports ALL unmet requirements** and exits non-zero | Operators "cannot accidentally run local-trusted defaults in production" | MEDIUM | F-026 | None |
| Reject **default/sample secrets** in non-dev (object-store keys, Garage RPC secret, dev bucket) | Copied sample creds must not reach production | LOW | F-007 | None — `Validate` checks DB/Neo4j only, not object-store creds (F-007) |
| Reject the `.env.example` empty-override footgun (`AURA_SHELL_DESTRUCTIVE_PATTERNS=` disabling the gate) | A normal setup step silently weakens shell safety | LOW | F-002 | None — empty value currently = "disable gate" (F-002) |
| **Fail-fast on malformed env** for security/reliability knobs in production (warn in dev) | Operators believe they changed a timeout/security knob that was silently ignored | MEDIUM | F-016 | None — `envIntDefault`/`envBoolDefault` silently fall back (F-016) |
| Reject **non-absolute `AURA_RUN_DIR`** at config load (normalize or fail) | Relative run dir makes sidecars cwd-dependent/unreadable after restart | LOW | F-041 | Partial — `read_tool_output` refuses relative; config load does not normalize (F-041) |
| Refuse **permissive CORS + no-auth** except under explicit dev profile; allowlist origins, set `Vary: Origin` | Drive-by web page can drive a local no-auth instance | MEDIUM | F-022 | Partial — permissive CORS exists; not profile-gated (F-022) |
| **AG-UI listener failure** is fatal or reflected in `/readyz`; Compose healthcheck probes `/readyz` not `aura version` | Orchestrators must detect API-down | MEDIUM | F-008, F-017 | Partial — `/readyz` route exists; listener failure non-fatal + healthcheck = `aura version` (F-008, F-017) |
| Reject **object-store replication_factor=1** in production profile | Single-replica artifact store is not durable | LOW | F-018 | None — Garage defaults to RF=1 (F-018) |
| Server-production **denies host shell/filesystem by default** unless sandboxed (ties to Category 3) | The roadmap's explicit production acceptance criterion | HIGH | F-001 | None (depends on per-user sandbox) |
| Production-disable or migrate **legacy `AURA_MCP_SERVERS_JSON`** escape hatch | Bypasses managed governance metadata | LOW | F-014 | None — legacy path validates command presence only (F-014) |

### Differentiators

| Feature | Value Proposition | Complexity | Notes |
|---------|-------------------|------------|-------|
| **Evidence JSON** output from `config validate` for CI gates | CI can assert "server_production validation passes" as a release gate | LOW | Mirrors AGT `verify --strict --json`; cheap |
| **Config diagnostics report** (`aura config doctor`) listing every resolved knob + source (env/default) | Operator can see what was silently defaulted before it bites | MEDIUM | Closes F-016's "operators believe they changed X" complaint at inspection time |
| Profile baked into `/readyz` payload | Monitoring sees which posture is running | LOW | One field |

### Anti-Features (do NOT build)

| Feature | Why Requested | Why Problematic | Alternative |
|---------|---------------|-----------------|-------------|
| A **config management framework** (layered overlays, remote config server) | "Flexible config" | Massive surface for a single appliance; the operator wants a contract not a platform | Enum + predicate list + one validate command |
| **Per-environment secret manager** integration (Vault, etc.) | "Production secrets" | Out of scope for a self-hosted mini-PC appliance; `.env` + validation is the agreed shape | Validate that secrets are non-default; leave storage to `.env` |
| **Auto-remediation** (validate then fix the config) | "Convenience" | Hides the contract violation; operator must see and decide | Report-and-fail only |

---

## Category 3 — Per-User Sandbox Lifecycle

**What mature systems do.** E2B / Daytona / Modal have converged on a clear lifecycle state machine and a set of table-stakes features. **States:** E2B = Running → Paused → Killed; Daytona = Running → Stopped → Archived → Deleted (richer). **Auto-stop/TTL** is universal (Daytona auto-stops after 15 min idle, auto-archives after 7 days, configurable auto-delete). **Pause/resume preserves full state** — filesystem *and* memory/running processes (E2B, Daytona); Modal differs (restore creates a *new* sandbox from a snapshot rather than resuming). **Warm pools** of pre-booted snapshots cut cold-start from cold-boot to ~5–90 ms — but for self-hosted you must pre-generate snapshots and warm the pool yourself. **Persistent per-user storage** = a mounted volume (NFS/block) at `/workspace` that survives pause/resume. **Stable identity + metadata** (sandbox ID, template, custom metadata, timestamps) is table stakes for attribution. The agent-sandbox-class model: **full shell/fs/network INSIDE, isolation OUTSIDE.**

**Minimal industrial form (operator-aligned).** Aura already adopted **rivetdev/sandbox-agent** (`SandboxExec`, `:2468`, `make sandbox-up`) as deliberate escalation. The minimal form is **not** a full K8s/k3s rebuild — it is to make the *existing* sandbox **per-identity** (one sandbox keyed by identity, full-capability inside) with a lifecycle manager: create-on-demand, idle-stop with TTL, scheduled-delete, and a persistent per-identity `/workspace` volume (named volume — Windows bind-mount is forbidden per project constraints). The host is never the execution surface for a provisioned identity; the operator's own sessions keep the full-host terminal. This dissolves F-001 without removing the core surface. Build the lifecycle over the Docker/rivetdev path; defer K8s/warm-pools unless load testing demands them.

### Table Stakes

| Feature | Why Expected | Complexity | Closes | Aura partial coverage |
|---------|--------------|------------|--------|----------------------|
| **Per-identity sandbox** (full shell/fs/network inside, isolated outside) — capability never stripped | The F-001 resolution; host real surface never exposed to a provisioned identity | HIGH | F-001, R-001 | Partial — rivetdev/sandbox-agent adopted as escalation, but **not per-identity** and host is still primary surface |
| Lifecycle **create / stop(pause) / resume / scheduled-delete** state machine | Universal sandbox table-stakes (E2B/Daytona) | HIGH | F-001 | None — current sandbox is single, stateless-ish escalation |
| **Idle TTL auto-stop** + **age metrics** for sandboxes and background jobs | Long-running/forgotten work bounded + attributable | MEDIUM | F-012 | None — background shell jobs have no TTL/owner (F-012) |
| **Persistent per-identity `/workspace`** (named volume) surviving stop/resume | Multi-turn agent sessions need workspace persistence | MEDIUM | F-001 | None — must be named volume (Windows bind-mount forbidden per constraints) |
| **Stable sandbox identity + metadata** (id, owner/identity, created/last-used) | Attribution + isolation enforcement | LOW | F-001, F-032 | None |
| **Background jobs bound to session/actor** with random unguessable IDs; poll/kill require matching session | One conversation must not poll/kill/read another's background job | MEDIUM | F-012, F-032 | None — IDs are sequential `sh_1`, not session-bound (F-032) |
| **Background shell TTL** + process-group kill on expiry/shutdown | Bounded, kill the whole process tree | MEDIUM | F-012 | Partial — capped by count + shutdown only; no TTL (F-012) |
| **Conversation delete routes through a runner lifecycle method** that evicts session tool state (cwd, todo, approval maps, bg buffers) + handles running jobs | Deleting the DB row must not leave stale in-memory tool state to be inherited | MEDIUM | F-039 | None — some delete/clear flows bypass runner eviction (F-039) |
| **Workspace path fence** by default + explicit time-bound grants for outside-workspace absolute paths | File tools safe-by-default in hardened/server | HIGH | F-001 | Partial — fs tools resolve absolute paths directly; outside-workspace `send_file` approval advertised but not wired (F-009) |
| **send_file outside-workspace approval actually wired** (resume hook authorizing one path/session/expiry) — or removed | Approval flow must match behavior | MEDIUM | F-009 | None — advertised, resume hooks only handle skill/shell (F-009) |
| Sandbox writes **cannot escape mounted workspace** in hardened profile | The roadmap's explicit sandbox acceptance criterion | HIGH | F-001 | None |

### Differentiators

| Feature | Value Proposition | Complexity | Notes |
|---------|-------------------|------------|-------|
| **Warm pool** of pre-booted sandbox snapshots (~ms resume) | Sub-second sandbox availability per identity | HIGH | Only if load tests show cold-start hurts; otherwise an anti-feature for a mini-PC. Defer. |
| **Enforced network egress allowlist** inside the sandbox (proxy/firewall, not advisory env var) | "Sandboxed" MCP/tool with a real allowlist, not a voluntary one | HIGH | Closes F-036 (Docker net allowlist is advisory today). Real but heavy — minimal form = `--network none` unless an enforceable policy exists |
| **Per-identity sandbox resource caps** (CPU/mem) honoring the mini-PC shared budget | Prevents one identity saturating the 16-core shared box | MEDIUM | Aligns with the project's CPU-budget memory; Docker cgroup limits |

### Anti-Features (do NOT build)

| Feature | Why Requested | Why Problematic | Alternative |
|---------|---------------|-----------------|-------------|
| **Full K8s / k3s** rebuild of the sandbox layer | "Industrial isolation" | Atomic bomb for a single mini-PC appliance; massive ops surface | Per-identity lifecycle over existing rivetdev/Docker path |
| **Firecracker microVM** per user from scratch | "Strongest isolation" | Self-hosting microVM warm-pools is an infra project, not a feature | Container isolation per identity (the agreed blast boundary per project memory) |
| Warm pool **now** (before load evidence) | "Fast resume" | Pre-booting idle sandboxes burns the shared CPU/RAM budget for unproven benefit | Create-on-demand + idle-stop; add warm pool only if load testing demands it |
| **Stripping capability** from the sandbox to make it "safe" | "Least privilege" | Contradicts the core decision — capability is never removed; isolation is the boundary | Full-capability inside, isolation outside (agent-sandbox-class) |

---

## Category 4 — Multi-User Identity Isolation

**What mature systems do.** The table-stakes guarantee is **"user B cannot see or mutate user A's data"**, enforced structurally: every query is scoped to the authenticated principal, and **there is no code path that reads tenant data without a tenant filter derived from the verified claim** (AWS Prescriptive Guidance, Scalekit, Blaxel all state this). Implementation is **owner-scoped store methods + per-principal API filtering** (or DB row-level security on shared tables). The authorization envelope (action + resource id + tenant context) flows from the authenticated session into every store call. **Channel-owned identity** (one identity per channel/session) limits a compromised identity's blast radius to one user. Background/scheduled work must carry the originating principal so it stays scoped. The proof is a **two-identity integration test**: B cannot list/get/delete/archive/resolve A's data, and B's own new resources are owned by B.

**Minimal industrial form (operator-aligned, NO RBAC).** This is explicitly *not* RBAC — no roles, no permission matrices, no OAuth providers. The minimal form: (1) add **owner-aware store methods** (`ListByOwner`, `GetForOwner`, etc.) to conversation + approval stores; (2) **filter every list/get/mutate by `identityctx.IdentityID(ctx)`**, returning 404/403 cross-principal; (3) make `NewConversation` inherit the context identity (`local` only as CLI/no-principal fallback); (4) carry the principal into scheduled/background jobs. Authz stays `capability_grants`-per-route (already exists). Then the Authula cutover flips the default auth provider. The whole thing is "thread the identity through stores and APIs + one isolation test", not a new authz subsystem.

### Table Stakes

| Feature | Why Expected | Complexity | Closes | Aura partial coverage |
|---------|--------------|------------|--------|----------------------|
| **Owner-aware conversation + approval store methods** (no read path without an owner filter) | The core isolation guarantee | MEDIUM | F-028, R-022 | None — stores list/mutate global (F-028) |
| **Per-principal API filtering** on list/get/delete/archive/approval-resolve (404/403 cross-principal) | B must not see or mutate A's data via the API | MEDIUM | F-028 | None — conversation/approval APIs operate on global stores (F-028) |
| **`NewConversation` inherits `identityctx.IdentityID(ctx)`** (`local` only for CLI/no-principal) | B-created chats must be owned by B, not seeded `local` (which then fails runner identity check) | LOW | F-028 | None — new Web conversations created under seeded `local` (F-028) |
| **Background / scheduled jobs carry the originating principal** and stay scoped | Async work must not leak across identities | MEDIUM | F-028, F-032 | Partial — scheduler has origin-channel routing; needs principal scoping |
| **Two-identity isolation integration test** as a CI gate | The publishable proof of the guarantee | MEDIUM | F-028, R-022 | None |
| **Authula auth cutover** — flip default provider, `capability_grants` enforced per-route | Multi-user production needs the embeddable capability-per-route provider | MEDIUM | (milestone goal) | Partial — Authula flag-gated; default still passphrase |
| **Strict JSON request decoding** (size cap, content-type, `DisallowUnknownFields`, single-decode EOF) on privileged routes | Typoed security fields silently ignored / trailing-JSON accepted on privileged routes | MEDIUM | F-052 | None — handlers decode first value, ignore unknown fields (F-052) |

### Differentiators

| Feature | Value Proposition | Complexity | Notes |
|---------|-------------------|------------|-------|
| **Identity in the tool-invocation ledger + audit rows** (which identity did this) | Forensics across users; one query proves who ran what | LOW | Cheap once Category 1 ledger has an owner column |
| **Per-identity sidecar/run-dir partitioning** | A corrupted/guessed path can't cross identities | MEDIUM | Pairs with F-005 sidecar fencing; isolates blast radius by identity |
| **Non-loopback bind guards** on auxiliary consoles (validation console, integration proxy) refuse unless explicit unsafe+auth | Prevents LAN exposure of token-injecting proxies | LOW | Closes F-047; cheap default-deny |

### Anti-Features (do NOT build)

| Feature | Why Requested | Why Problematic | Alternative |
|---------|---------------|-----------------|-------------|
| **RBAC with roles/permissions** (admin vs user) | "Real multi-user" | Explicitly out of scope; atomic bomb; the milestone is identity isolation only | `capability_grants`-per-route + owner-scoping |
| **OAuth multi-provider / SaaS multi-tenant login** | "Enterprise auth" | Out of scope; Aura is a self-hosted appliance | Authula embedded, capability-per-route |
| **DB-level row-level-security policies** in Postgres | "Defense in depth" | Heavyweight + sqlc/pgx friction for a 2-identity appliance; harder to test than owner-scoped methods | Owner-scoped store methods + the two-identity test |
| **Cross-user sharing / collaboration** features | "Teams want to share" | New product surface, not hardening; explodes the isolation model | Strict isolation only; sharing is a post-v2 product decision |

---

## Category 5 — Capability Evaluation Suite

**What mature systems do.** A real agent eval harness scores on **traits, not exact outputs**: did the agent call a specific tool exactly once, never call a forbidden function, validate structured output against schema, cite sources, fail safely on hostile input. A golden test case has four parts — **input, expected trajectory, expected output, scoring functions**. The recommended first-version distribution is **5 happy paths + 5 edge cases + 3 adversarial cases per capability**. Adversarial cases cover **prompt injection in user input and in tool outputs** (PIArena, the tool-using-agent injection benchmark). **Loop-liveness** safety mechanisms: iteration/time limits (terminate at e.g. 50 steps / 5 min) and **repeated-cycle detection** (same tool-call sequence 3+ times = break). The whole suite runs at a **CI gate** with regression checks: a previously-passing scenario that now fails flags a logic/prompt regression. The audit (F-019) explicitly references `D:\tmp\agent-infra-sandbox\evaluation\README.md` as the reference.

**Minimal industrial form (operator-aligned).** A Go-test-tagged suite (e.g. `//go:build capability_eval`) of **golden scenario tests per capability class** — shell, files, MCP, memory, pause/resume, error, workflow — plus an adversarial set (prompt-injected shell/file/network requests must be **denied in production tests**), loop-liveness (budget/cycle), and chaos-cancellation (SIGTERM/ctx-cancel mid-tool). It **publishes a CI pass/fail report**. This is the regression net that makes every other category's fix permanent. It reuses the existing `internal/eval` harness pattern (CoT eval) and the golang-testing discipline (table-driven, golden, goleak, race). Do not build an LLM-judge scoring platform or a live-cost eval that runs on every PR — gate the live tiers behind explicit invocation like the existing `cot_eval`.

### Table Stakes

| Feature | Why Expected | Complexity | Closes | Aura partial coverage |
|---------|--------------|------------|--------|----------------------|
| **Golden scenario tests per capability class** (shell/files/MCP/memory/pause-resume/error/workflow) | "Agent safety and reliability tracked across releases" | HIGH | F-019, F-025 | Partial — `internal/eval` (CoT, live, not CI) exists; not capability-class golden tests |
| **Adversarial prompt-injection regression set** (injection in user input + tool output → denied in production) | Side-effect regressions can ship unnoticed; prompt-level safety is insufficient | HIGH | F-019 | None |
| **Terminal-sibling rejection** golden test (mutating sibling does not execute) | Locks the F-003 fix permanently | LOW | F-003, F-019 | None |
| **Loop-liveness** tests — runaway budget + repeated-cycle detection | Liveness regressions caught | MEDIUM | F-019 | Partial — budget/depth guards exist; no cycle-detection eval |
| **Pause/resume race + atomicity** golden tests (single + batch; append-failure-after-claim leaves retryable/repairable state) | Locks F-004/F-005/F-029/F-030 fixes | MEDIUM | F-004, F-005, F-029, F-030, F-019 | None |
| **Chaos-cancellation** tests — SIGTERM/ctx-cancel mid-tool, MCP timeout storm, fault-injection at each mutating state transition | Cancellation/recovery regressions caught | HIGH | F-019, F-020, F-042 | None |
| **CI publishes a pass/fail evaluation report** | The publishable artifact the audit asks for | MEDIUM | F-019, F-025 | None |
| **Sidecar path-traversal denial** tests (outside-root/traversal/symlink rejected) | Locks F-005 fence | LOW | F-005, F-019 | None — reads trust DB paths today (F-005) |
| **Background-job isolation** test (session B cannot poll/kill A's job) | Locks F-032 | LOW | F-032, F-019 | None |

### Differentiators

| Feature | Value Proposition | Complexity | Notes |
|---------|-------------------|------------|-------|
| **Trajectory scoring** (assert the tool-call sequence, not just final output) | Catches "did the right thing the wrong way" regressions | MEDIUM | Reuses live tool-selection trace as ground truth (project memory) |
| **Drift detection on tool signatures** (alert if a tool's spec/schema changed unexpectedly) | Catches tool-poisoning / accidental contract changes | LOW | Microsoft AGT lists this; cheap snapshot test |
| **Per-release eval delta report** (this release vs last) | Operator sees what got better/worse | MEDIUM | Mirrors golden-trace regression tooling |

### Anti-Features (do NOT build)

| Feature | Why Requested | Why Problematic | Alternative |
|---------|---------------|-----------------|-------------|
| **LLM-judge scoring platform** run on every PR | "Smarter eval" | Cost + flakiness + slow CI; the existing rule is no unsolicited paid runs | Deterministic trait scoring in CI; live LLM tiers behind explicit invocation (like `cot_eval`) |
| **Continuous live agent eval** in the default CI matrix | "Always know quality" | Burns API budget per PR; project memory forbids unsolicited paid runs | Tagged tier, explicit go, scheduled not per-PR |
| **A separate eval SaaS / external harness** | "Best-in-class tooling" | New external dependency for a self-hosted appliance | Go-test-tagged suite in-repo, reuse `internal/eval` |

---

## Category 6 — Production Observability (as a feature)

**What mature systems do.** The 2026-stable pattern is **OpenTelemetry GenAI semantic conventions** (LLM calls, token usage, model params — stable early 2026) emitted as spans, scraped by **Prometheus**, visualized in **Grafana** with an **SLO dashboard** and an **alert pack**. The canonical dashboard surfaces: request rate, **p95/p99 latency**, **tool success/error rate**, **token/cost**, agent-workflow step durations. Alerts fire on **latency spikes, error-rate increases, token-budget overrun**. For an agent runtime specifically the audit (F-023) names the SLOs that need alerts: loop error rate, tool timeout rate, queue lag, LLM latency, MCP timeout rate, resume failures, listener health. **Readiness probes** (`/readyz` reflecting DB/listener/migration/scheduler state) are table stakes. **Retention/cleanup** is a first-class operational workflow, not an afterthought.

**Minimal industrial form (operator-aligned).** Aura already exposes `/metrics` + `/debug/vars` and Prometheus metrics (per project state). The gap (F-023/F-024) is **the pack on top**: ship **Grafana dashboard JSON + Prometheus alert-rule YAML in-repo** (validated for syntax in CI — static validation, no live Grafana needed), wire **`/readyz` to actually reflect dependency state**, add **OTel spans** on the load-bearing paths (LLM, tool, MCP, pause/resume, DB, scheduler), and add **retention config + a cleanup command + disk-usage metrics** for sidecars/traces/learning stores. This is "ship the artifacts + validate them in CI", not "build a monitoring platform" — the operator runs Prometheus/Grafana themselves.

### Table Stakes

| Feature | Why Expected | Complexity | Closes | Aura partial coverage |
|---------|--------------|------------|--------|----------------------|
| **Grafana dashboard JSON** (loop error rate, tool timeout rate, queue lag, LLM latency, MCP timeout, resume failures, listener state) shipped in-repo | "Operators lack a ready production monitoring baseline" | MEDIUM | F-023 | Partial — `/metrics` + `internal/agui/metrics.go` exist; no dashboard |
| **Prometheus alert-rule YAML** for those SLOs, **syntax-validated in CI** | Alerts must not need to be invented by the operator | MEDIUM | F-023 | None |
| **`/readyz` reflects real dependency state** (DB, listener, migration state, scheduler) + Compose/K8s probes it | Containers marked healthy while API is down is a production-availability break | MEDIUM | F-008, F-017 | Partial — `/readyz` route exists; doesn't reflect listener/deps; healthcheck = `aura version` |
| **OTel spans** on LLM, tool, MCP, pause/resume, DB, scheduler paths | Diagnose failures quickly; GenAI semconv is the 2026 standard | HIGH | F-023 | Partial — Prometheus metrics exist; OTel spans not on all paths |
| **Retention config + cleanup command + disk-usage metrics** for sidecars/traces | Long-running systems accumulate sensitive/large local files unbounded | MEDIUM | F-024, F-021, F-049 | None — sidecar retention not a first-class workflow (F-024); learning stores uncapped (F-049) |
| **Background-job age metrics** + owner/status exposed | Forgotten jobs visible | LOW | F-012 | None |
| **Per-conversation export/delete** operation | Data hygiene + operator control over accumulated local content | MEDIUM | F-024 | None |
| **Scheduler drain on SIGTERM** (separate stop-admission from job-work context, bounded drain) + systemd stop budget ≥ longest handler | Backup/agent jobs killed mid-work despite "drain" comments | MEDIUM | F-042, F-043 | None — in-flight work cancels immediately (F-042); `TimeoutStopSec` < backup duration (F-043) |

### Differentiators

| Feature | Value Proposition | Complexity | Notes |
|---------|-------------------|------------|-------|
| **Backup/DR restore drill** with measured RPO/RTO (PG + Neo4j + sidecars + object store), CI or scheduled | "Recovery objectives are measured, not assumed" | HIGH | F-035 (action plan: backup/DR validation). The literal-10/10 operator decision includes this. |
| **Load + chaos harness** (DB outage, MCP timeout storm, object-store outage, process-kill mid-write) defining supported concurrency + degradation | Defines and proves the runtime's operational envelope | HIGH | Pairs with Category 5 chaos tests; the literal-10/10 decision includes load/chaos |
| **Encrypted trace/sidecar sink** option + production warning on full-trace mode | Full traces become sensitive records during incidents | MEDIUM | F-021 — minimal form = warning + retention; encryption is the differentiator |
| **Profile + version surfaced in `/readyz`** | Monitoring sees posture + build | LOW | One payload field |

### Anti-Features (do NOT build)

| Feature | Why Requested | Why Problematic | Alternative |
|---------|---------------|-----------------|-------------|
| **Bundled Grafana/Prometheus** inside the single binary | "Turnkey monitoring" | Breaks single-binary invariant; operator runs their own stack | Ship dashboard JSON + alert YAML as artifacts, validate in CI |
| **Custom metrics backend / TSDB** | "Full control" | Reinventing Prometheus | Standard `/metrics` + OTLP; let operators bring backends |
| **Always-on full reasoning-trace capture** in production | "Maximum observability" | Sensitive-data accumulation + disk blowup (F-021) | Redacted default + opt-in full mode with warning + retention |
| **Alerting/notification routing** built into Aura | "One-stop ops" | That's Alertmanager's job | Ship the rules; routing is the operator's Alertmanager |

---

## Feature Dependencies

```
[Runtime Profiles] ──────────────┐  (Category 2 — keystone, lowest cost, gates everything else's prod behavior)
    │                             │
    ├──gates──> [ToolGateway fail-closed in server_production]   (Cat 1)
    ├──gates──> [Sandbox-required-in-production]                 (Cat 3)
    └──gates──> [CORS/listener/secret/run-dir validation]       (Cat 2 self)

[ToolGateway / PEP] ─────────────┐  (Category 1 — keystone for safety)
    │                            │
    ├──requires──> [Durable mutating ledger + idempotency]      (Cat 1 self, extends internal/toolinvocations)
    ├──requires──> [Capability classes + per-tool timeout]      (Cat 1 self)
    ├──enables───> [Sandbox selection per identity]             (Cat 3)
    └──enables───> [Decision evidence / per-identity audit]     (Cat 4 + Cat 6)

[Per-User Sandbox Lifecycle] ────┐  (Category 3 — resolves F-001)
    │                            │
    ├──requires──> [ToolGateway sandbox selection]              (Cat 1)
    ├──requires──> [Workspace fence + path grants]              (Cat 3 self)
    └──requires──> [Per-identity run-dir/workspace volume]      (Cat 4 partitioning)

[Multi-User Identity Isolation] ─┐  (Category 4 — resolves F-028)
    │                            │
    ├──requires──> [Authula cutover (principal in ctx)]         (Cat 4 self)
    ├──requires──> [Owner-scoped stores + API filtering]        (Cat 4 self)
    └──enhances──> [Per-User Sandbox (one sandbox per identity)](Cat 3)

[Capability Eval Suite] ─────────> verifies ALL of the above    (Category 5 — the regression net)
    └──requires──> [the fixes exist to assert on]               (runs last per area, authored alongside)

[Production Observability] ───────> instruments ALL of the above (Category 6)
    └──requires──> [/readyz dependency wiring + ledger]         (Cat 2 + Cat 1)
```

### Dependency Notes

- **Runtime Profiles is the cheapest keystone:** it's LOW/MEDIUM complexity but gates the *production behavior* of the ToolGateway (fail-closed), the sandbox (required-in-prod), and CORS/listener/secret validation. Do it early — it converts ~7 findings into one validated command and unblocks "server_production denies X by default" acceptance criteria elsewhere.
- **ToolGateway requires the durable ledger:** "no tool executes without a recorded policy decision" needs the ledger to be mandatory (not best-effort) for mutating tools. Extend `internal/toolinvocations`, don't rebuild it.
- **Per-User Sandbox requires ToolGateway sandbox-selection:** the gateway is *where* "run in identity B's sandbox" is decided, so the gateway seam should land before/with the per-identity lifecycle.
- **Identity Isolation enhances the Sandbox:** one sandbox per identity is the natural join — the sandbox lifecycle keys on the same `identityctx.IdentityID` that the stores filter on.
- **Eval Suite runs last per area but is authored alongside each fix:** each correctness fix (F-003/004/005/029/030/031/032) ships *with* its golden test, then the suite aggregates them into a CI report.
- **Observability requires `/readyz` + ledger wiring:** dashboards/alerts assert on metrics the ledger and readiness probe must first emit.

---

## MVP Definition (for "honest 10/10", phased per the industrialization-roadmap)

### Launch With — Stabilization + foundations (Roadmap Phase 0–1; P0/P1)

These are the safety/correctness keystones the audit marks production-blocking.

- [ ] **Runtime Profiles + `config validate`** — keystone, lowest cost, gates everything (F-002, F-007, F-008, F-016, F-017, F-018, F-022, F-026, F-041)
- [ ] **Agent-loop correctness fixes** — terminal-sibling exclusivity, batch + single resume atomicity, pause-flush durability, mutating-panic classification (F-003, F-004, F-005, F-029, F-030, F-031)
- [ ] **Sidecar path fencing** + command-hook fail-closed default (F-005, F-006)
- [ ] **Multi-user identity isolation** — owner-scoped stores + per-principal API filtering + `NewConversation` identity inheritance + two-identity test (F-028, R-022)
- [ ] **MCP transport classifier + explicit trust** — reject mixed url+command, empty-trust-remote, empty trust bodies (F-013, F-027, F-038)
- [ ] **Durable mutating-tool ledger** (started/succeeded/failed, reservation required in prod) (F-011, F-020)

### Add After Foundations — Architecture + sandbox (Roadmap Phase 2–3; P1/P2)

- [ ] **ToolGateway / PEP** — single chokepoint, capability classes, idempotency keys, fail-closed in prod (F-001, F-020, F-031)
- [ ] **Per-user full-capability sandbox lifecycle** — create/stop/resume/delete, TTL, per-identity workspace volume, workspace fence + path grants (F-001, F-009, F-012, F-032, F-039)
- [ ] **MCP lifecycle hardening** — per-server mount timeout, stdio frame cap, bounded close, process-tree teardown, audited CLI writes, real HTTP probe (F-033, F-034, F-035, F-037, F-046)
- [ ] **Security pack** — secret redaction in output/traces, prompt-injection regression suite, strict JSON decoding, non-loopback bind guards, SBOM + pinned actions, token-in-URL fix (F-019, F-021, F-047, F-050, F-051, F-052)

### Future Consideration within v2.0.0 — Scale/ops + governance docs (Roadmap Phase 4–5; P2/P3)

- [ ] **Production observability pack** — Grafana JSON + Prometheus alerts + OTel spans + `/readyz` deps + retention/cleanup (F-023, F-024, F-048, F-049)
- [ ] **Scheduler drain + stop budgets** (F-042, F-043)
- [ ] **Capability eval suite as a CI report** aggregating all golden/adversarial/chaos tests (F-019, F-025)
- [ ] **Load/chaos harness + backup/DR restore drill with RPO/RTO** (F-035, the literal-10/10 decision)
- [ ] **Enforced network egress allowlist** inside sandbox (F-036) — heavy; minimal form = `--network none` default
- [ ] **ADRs + release-readiness checklist** (F-025, F-026, F-045, F-049)

---

## Feature Prioritization Matrix

| Feature (category) | Operator/Prod Value | Implementation Cost | Priority |
|--------------------|---------------------|---------------------|----------|
| Runtime Profiles + `config validate` (Cat 2) | HIGH | LOW | **P1** |
| Agent-loop correctness fixes (Cat 1) | HIGH | MEDIUM | **P1** |
| Multi-user identity isolation + two-identity test (Cat 4) | HIGH | MEDIUM | **P1** |
| Durable mutating-tool ledger + idempotency (Cat 1) | HIGH | MEDIUM | **P1** |
| MCP transport classifier + explicit trust (Cat 1/4) | HIGH | MEDIUM | **P1** |
| Sidecar fencing + hook fail-closed (Cat 1/3) | HIGH | LOW | **P1** |
| ToolGateway / PEP single chokepoint (Cat 1) | HIGH | HIGH | **P1** |
| Per-user sandbox lifecycle (Cat 3) | HIGH | HIGH | **P1** |
| Authula auth cutover (Cat 4) | HIGH | MEDIUM | **P2** |
| Background-job session-binding + TTL (Cat 3) | MEDIUM | MEDIUM | **P2** |
| MCP lifecycle hardening (Cat 1/3) | MEDIUM | MEDIUM | **P2** |
| Security pack — redaction, injection suite, strict JSON, pinned actions (Cat 1/5) | HIGH | MEDIUM | **P2** |
| `/readyz` dependency wiring + Compose healthcheck (Cat 2/6) | HIGH | MEDIUM | **P2** |
| Capability eval suite + CI report (Cat 5) | HIGH | HIGH | **P2** |
| Observability pack — dashboards/alerts/OTel (Cat 6) | MEDIUM | MEDIUM | **P2** |
| Retention/cleanup commands + disk metrics (Cat 6) | MEDIUM | MEDIUM | **P2** |
| Scheduler drain + stop budgets (Cat 6) | MEDIUM | MEDIUM | **P2** |
| Backup/DR restore drill + RPO/RTO (Cat 6) | HIGH | HIGH | **P2** (literal-10/10) |
| Load/chaos harness (Cat 5/6) | MEDIUM | HIGH | **P2** (literal-10/10) |
| Enforced sandbox egress allowlist (Cat 3) | MEDIUM | HIGH | **P3** |
| Warm pool / per-identity resource caps (Cat 3) | LOW | HIGH | **P3** (only if load demands) |
| ADRs + release-readiness checklist (governance) | MEDIUM | LOW | **P3** |

**Priority key:** P1 = production-blocking, stabilization/foundation (Roadmap Phase 0–2). P2 = required for honest 10/10 (Roadmap Phase 2–4). P3 = governance/long-term or load-gated (Roadmap Phase 4–5; defer warm pool/egress-enforcement unless evidence demands).

---

## Reference Comparison (how the chosen minimal forms map to industry)

| Capability | Industry reference (heavyweight) | Aura minimal-industrial form |
|------------|----------------------------------|------------------------------|
| ToolGateway/PEP | AEGIS gateway + SDK + Merkle audit; Microsoft AGT with OPA/Cedar | In-process `ToolGateway.Decide` seam + append-only Postgres ledger + capability classes (no DSL, no sidecar, no Merkle) |
| Runtime profiles | AGT `verify --strict` + 992 conformance tests | `Profile` enum + predicate list + `aura config validate --profile` (evidence JSON optional) |
| Per-user sandbox | E2B/Daytona/Modal microVM warm-pools + NFS | Per-identity lifecycle over existing rivetdev/Docker + named-volume `/workspace` (defer warm pool/microVM) |
| Identity isolation | AWS Bedrock tenant-scoped creds + Postgres RLS | Owner-scoped store methods + per-principal API filter + `identityctx` threading + Authula (no RBAC, no RLS) |
| Eval suite | Braintrust/Maxim eval platforms + LLM-judge | Go-test-tagged golden/adversarial/chaos suite + CI pass/fail report (reuse `internal/eval`; live LLM tiers gated) |
| Observability | Grafana AI Observability SaaS + OTel GenAI semconv | Ship dashboard JSON + alert YAML + OTel spans; operator runs Prometheus/Grafana (no bundled stack) |

---

## Sources

- [Microsoft Agent Governance Toolkit](https://github.com/microsoft/agent-governance-toolkit) — PEP/PDP, allow/deny/require_approval, `verify --strict`, container-per-agent, decision records, Agent SRE SLOs — HIGH (reference implementation, OWASP Agentic Top 10)
- [AEGIS: No Tool Call Left Unchecked (arXiv)](https://arxiv.org/html/2603.12621v1) — minimal pre-execution firewall architecture, 8.3 ms latency (bottleneck answer), essential-vs-optional split, tamper-evident audit — HIGH
- [OpenPort Protocol (arXiv)](https://arxiv.org/pdf/2602.20196) — deny-by-default gateway, risk-gated write lifecycle, idempotency as a security threat — HIGH
- [Microsoft: Authorization and Governance for AI Agents](https://techcommunity.microsoft.com/blog/microsoft-security-blog/authorization-and-governance-for-ai-agents-runtime-authorization-beyond-identity/4509161) — Authorization Fabric (PEP+PDP), single chokepoint, audit-in-one-place — HIGH
- [Composio: MCP Gateways Guide 2026](https://composio.dev/content/mcp-gateways-guide) — gateway architecture patterns — MEDIUM
- [E2B Sandbox Lifecycle docs](https://e2b.dev/docs/sandbox) — states, pause/resume state preservation, timeouts, metadata — HIGH (official)
- [ZenML: E2B vs Daytona](https://www.zenml.io/blog/e2b-vs-daytona) + [Northflank: Daytona vs Modal](https://northflank.com/blog/daytona-vs-modal) — state machines, auto-stop/archive/delete TTLs, warm pools, persistence, Modal's snapshot-vs-resume difference — MEDIUM (vendor comparison, cross-checked)
- [AgentMarketCap: AI Agent Sandbox Infrastructure 2026](https://agentmarketcap.ai/blog/2026/04/07/ai-agent-sandbox-infrastructure-e2b-modal-daytona-fly-machines-secure-code-execution) — sandbox feature landscape — MEDIUM
- [AWS Prescriptive Guidance: Enforcing tenant isolation](https://docs.aws.amazon.com/prescriptive-guidance/latest/agentic-ai-multitenant/enforcing-tenant-isolation.html) + [Implementing tenant isolation with Bedrock Agents](https://aws.amazon.com/blogs/machine-learning/implementing-tenant-isolation-using-agents-for-amazon-bedrock-in-a-multi-tenant-environment/) — no-read-without-tenant-filter, tenant-scoped creds, authorization envelope — HIGH
- [Scalekit: Access Control for Multi-Tenant AI Agents](https://www.scalekit.com/blog/access-control-multi-tenant-ai-agents) + [Blaxel: Multi-tenant isolation for AI agents](https://blaxel.ai/blog/multi-tenant-isolation-ai-agents) — channel-owned identity, per-principal filtering, row scoping — MEDIUM
- [Motomtech: AI Agent Eval Harness — Golden Tests](https://www.motomtech.com/blog-post/agentic-ai-eval-harness-golden-tests/) + [Medium/Lanham: Build an Evaluation Harness](https://medium.com/@Micheal-Lanham/how-to-build-an-evaluation-harness-for-your-ai-agent-before-it-books-the-wrong-flight-84de83a47207) — 4-part golden case, 5+5+3 distribution, trait scoring, loop-liveness, regression CI — MEDIUM (cross-checked)
- [PIArena: Prompt Injection Evaluation (arXiv)](https://arxiv.org/pdf/2604.08499) + [LlamaFirewall (arXiv)](https://arxiv.org/pdf/2505.03574) — adversarial prompt-injection benchmarking for tool-using agents — HIGH
- [Braintrust: What is agent evaluation](https://www.braintrust.dev/articles/agent-evaluation) — task/simulation/success-criteria eval — MEDIUM
- [Grafana: LLM observability with OpenTelemetry](https://grafana.com/blog/2024/07/18/a-complete-guide-to-llm-observability-with-opentelemetry-and-grafana-cloud/) + [Grafana AI Observability docs](https://grafana.com/docs/grafana-cloud/machine-learning/ai-observability/) — SLO dashboards, p95/p99, tool success rate, cost, alert rules — HIGH
- [OpenTelemetry: Observability for LLM apps](https://opentelemetry.io/blog/2024/llm-observability/) + [Glukhov: Observability for LLM Systems](https://www.glukhov.org/observability/observability-for-llm-systems/) — GenAI semantic conventions (stable early 2026), metrics/traces/logs — HIGH
- [Cortex: Production Readiness Checklist](https://www.cortex.io/post/how-to-create-a-great-production-readiness-checklist) + [getDX](https://getdx.com/blog/production-readiness-checklist/) + [Auth0 production checks](https://auth0.com/docs/deploy-monitor/pre-deployment-checks/production-check-required-fixes) + [learnkube K8s production](https://learnkube.com/production-best-practices) — default-secret rejection, auth-enabled, CORS-allowlist, readiness probes, fail-fast config — MEDIUM (multiple converging)
- Aura internal (read directly): `docs/audit/bug-report.md` (F-001..F-052), `docs/audit/action-plan.md`, `docs/audit/industrialization-roadmap.md`, `.planning/PROJECT.md`, `internal/config/config.go` + `config_profile_test.go` (confirmed "Profile" today = Agent.md identity, not a runtime profile), `internal/toolinvocations/store.go` (confirmed best-effort start/event ledger, no idempotency keys), `internal/agui/metrics.go` (Prometheus metrics exist, no dashboard) — HIGH (primary source)

---
*Feature research for: industrial hardening + multi-user identity isolation of the Aura agentic substrate (v2.0.0)*
*Researched: 2026-06-29*
