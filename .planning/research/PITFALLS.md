# Pitfalls Research

**Domain:** Industrial hardening + per-user sandbox + multi-user identity isolation + ToolGateway + Authula cutover on an existing trusted-single-operator Go agent runtime (Aura v2.0.0, mini-PC / DGX Spark target)
**Researched:** 2026-06-29
**Confidence:** HIGH (audit findings + actual code read; external ecosystem facts verified via current sources)

> Scope note: these are pitfalls of **ADDING** isolation/gateway/auth/profiles/ops to *this* system, not generic advice. Every pitfall maps to the audit finding(s) it relates to and the owning v2.0.0 phase. The minimal-industrial-form LINE for the ToolGateway and the sandbox is stated explicitly (Pitfall 11, Pitfall 1), and the honest-10/10 evidence bar is in the dedicated section near the end. Phase numbers below are *logical owners* (Sandbox / Multi-user+Auth / ToolGateway / Profiles / Agent-loop / MCP / Observability / Security / Ops / Eval) — the roadmapper assigns the actual phase IDs (31+).

---

## Critical Pitfalls

### Pitfall 1: "Full-capability inside" accidentally becomes "full host" (mount/socket/network leaks)

**What goes wrong:**
The locked decision (F-001) is per-user **full-capability isolated sandbox** — the agent keeps full shell/fs/network *inside* its box, the real host is never exposed. The classic failure is implementing "full capability" by handing the container the host: mounting the Docker socket (`/var/run/docker.sock`) so the agent can build/run things, `--network host` or `--privileged` so networking/devices "just work", bind-mounting `/` or `$HOME` or `~/.aura` read-write so the agent "sees its files", or sharing the user's real `/tmp`/run-dir. Each of those re-exposes exactly the host F-001/R-001 was meant to fence — and worse, a Docker-socket mount is *root-on-host* RCE by design.

**Why it happens:**
The product value is "the agent experiences a full host." The path of least resistance to deliver that experience is to give it a real one. The operator's own memory (`feedback_aura_full_host_terminal_primary`) makes this seductive: the team already believes the terminal is THE surface.

**How to avoid:**
- The sandbox gets a *full but synthetic* host: its own writable rootfs/overlay, its own loopback, its own `/tmp`, its own workspace volume — never a host bind. Use **named volumes only** (already mandated in PROJECT.md constraints), never bind-mounts (Windows-corruption rule + escape vector).
- Hard-ban at construction time: no `docker.sock` mount, no `--privileged`, no `--network host`, no `--pid host`, no `--cap-add SYS_ADMIN`, no host-path bind. Make these *unrepresentable* in the sandbox profile struct (no field to set them), not merely defaulted off.
- If the agent needs to build containers, give it a nested/rootless builder (buildkit rootless or a per-user nested daemon), never the host daemon.
- Per-user network: deny-by-default egress (`--network none` baseline, already the Slice-2 default), explicit allowlist enforced by a **real** mechanism (proxy/firewall), not an advisory env var (see Pitfall 3 / F-036).

**Warning signs:**
- Any sandbox spec field, env, or compose line containing `docker.sock`, `privileged`, `network_mode: host`, `cap_add`, or a host absolute path under `volumes:`.
- The agent can `docker ps` and see *host* containers, or can reach a host-only service (Postgres on 5432, Neo4j on 7687) from inside the sandbox.
- A test that proves isolation passes in <1s (it's not actually launching a container — skip-as-green).

**THE LINE (where to stop):** A per-user **container** (over the existing rivetdev/sandbox-agent or plain Docker), one warm container per active identity, full capability *inside* the namespace, host never bind-mounted, egress proxy-enforced. Do **not** reach for full K8s/k3s, microVMs (Firecracker/Kata), or gVisor for v2.0.0 — see Pitfall 2 for why on this hardware.

**Phase to address:** Per-user sandbox phase. **Finding: F-001 / R-001 (also F-036/R-030 for egress).**

---

### Pitfall 2: Choosing a heavyweight isolation runtime that doesn't fit a 16-core/32GB mini-PC

**What goes wrong:**
Reaching for the "most secure" isolation tier (gVisor/runsc, Kata/Firecracker microVMs, or orchestrating with k3s/k0s) crushes the resource budget and/or destroys the very workload Aura runs. Concrete, verified numbers:
- **gVisor** imposes ~10-30% overhead on typical workloads but **+125% execution time on syscall/IO-heavy work** (e.g. SQLite inserts, `Build ABSL`). A "full host shell" agent is *the* syscall/IO-heavy workload (file reads/writes, builds, `git`, `npm`, compilers). gVisor would make the headline feature feel broken.
- **k3s single-node** consumes ~565MB-768MB RAM idle plus continuous CPU heartbeat/health/metrics churn that "isn't negligible" even at idle. On a host already at ~5.7-6.2GB idle / ~7GB peak (PROJECT.md), adding a full control plane is pure tax for a need (one container per user) that Docker already serves.
- **Firecracker/Kata microVMs** allocate a dedicated kernel + ~1GB RAM *per sandbox* (E2B default). At even 3-4 concurrent identities that's the whole RAM budget gone, before LLM sidecars.

**Why it happens:**
Security-maximalism + "industrial = Kubernetes" cargo-culting. The audit's word "industrial" gets misread as "enterprise orchestration."

**How to avoid:**
- Match the threat model to the trust model: v2.0.0 is **identity isolation, NOT a hostile-multi-tenant SaaS** (PROJECT.md: NO RBAC, isolation = data+process+fs per identity). Containers (namespaces + cgroups + seccomp + user-ns remap) are the correct industrial tier for trusted-but-separated users. The operator explicitly rejects "atomic bombs."
- Keep the existing **rivetdev/sandbox-agent + Docker** substrate (already adopted, D-15). Add: one warm container per active identity, cgroup mem/CPU caps, seccomp default-deny (already a Slice-2 requirement), user-namespace remap so container-root ≠ host-root, named-volume workspace per identity.
- Reserve gVisor/Kata as a *documented future escalation* gated on the DGX Spark hardware, not v2.0.0. Record the decision as an ADR (F-025).

**Warning signs:**
- A roadmap phase titled "k3s setup" or "Firecracker integration."
- RAM idle creeps past ~7GB after the sandbox phase lands; OOM-kills of Neo4j/embed sidecars under 2-3 concurrent users.
- Build/compile tasks inside the sandbox run 2x+ slower than host (gVisor tell).

**Phase to address:** Per-user sandbox phase (research-locked design fork: container-per-user vs K8s/k3s). **Finding: F-001 / R-001; mini-PC budget per `feedback_minipc_cpu_budget`.**

---

### Pitfall 3: Per-user egress/volume "isolation" that is advisory, not enforced

**What goes wrong:**
Two specific leaks already flagged: (1) the Docker MCP network allowlist (`AURA_MCP_NETWORK_ALLOW`) is passed as *env data the container may ignore* while the container actually runs on a bridge network with full egress — F-036/R-030. The same trap repeats when you add per-user sandboxes: you set an "allowlist" but the network namespace still routes everywhere. (2) Per-user **volume leakage** — two identities' sandboxes share a workspace root, a `/tmp`, or a run-dir, so user B reads user A's artifacts.

**Why it happens:**
Egress enforcement is genuinely harder than passing a string; teams ship the string and call it done. Volume sharing is the default when you reuse one named volume or one workspace path across sessions.

**How to avoid:**
- Egress: keep `--network none` unless a *real* enforcement backend exists (per-user egress proxy with host allowlist, or an iptables/nftables OUTPUT policy generated from resolved IPs — the Slice-2b mechanism). An allowlist with bridge networking = no allowlist. Write an integration test that proves a disallowed host is unreachable (F-036 suggested coverage).
- Volumes: one named volume per identity, derived from the authenticated principal (`aura-ws-<identityID>`), never a shared path. On identity delete, the volume is destroyed (ties to Pitfall 6 / F-039).
- Per-user `/tmp` and run-dir: each sandbox gets its own tmpfs and its own `$AURA_RUN_DIR` subtree; never the host's.

**Warning signs:**
- A sandbox with a non-`none` network and a non-empty allowlist but no proxy/firewall in the data path.
- `docker volume ls` shows one shared workspace volume reused across identities.
- A test reads a file written by "user A" from "user B"'s sandbox and it succeeds.

**Phase to address:** Per-user sandbox phase. **Finding: F-036 / R-030 (egress); F-001 + cross-ref F-028 (per-user volume scoping).**

---

### Pitfall 4: Cold-start latency breaking UX (and the warm-pool / container-reuse security trap)

**What goes wrong:**
Per-user sandboxing adds container spin-up to the critical path. If you cold-start a container per request, the first tool call after idle stalls for seconds — the agent feels broken vs today's instant host shell. The "fix" — keeping warm pools or reusing one long-lived container — re-introduces the exact security tension the isolation was for: **reused containers leak state between tasks and weaken per-execution isolation**, and idle warm pools burn the mini-PC's scarce RAM.

**Why it happens:**
Latency is visible immediately; the isolation regression from reuse is invisible until an incident. On a constrained host, "just keep N warm" silently consumes the RAM headroom.

**How to avoid:**
- Right-size the pool to the trust model: this is **identity isolation, not per-request ephemeral isolation**. One warm container **per active identity** (not per request) is the correct grain — state *within* one user's session is fine to keep; state *across* users must never share. That matches the locked decision and keeps warm-count = active-users (small on a mini-PC).
- Lazy-start + keep-warm-with-TTL: start a user's container on first tool call, keep it for the session, evict on idle TTL or identity logout/delete. Tie eviction to the SessionEvictor path (Pitfall 6).
- Never reuse one container across *different* identities. Reuse within one identity's conversations is acceptable (same trust principal).
- Budget the warm pool: cap concurrent warm containers (`AURA_SANDBOX_MAX_CONCURRENT_SESSIONS` already exists, default 5) so the pool can't exceed RAM headroom.

**Warning signs:**
- First-tool-call p95 jumps from ~ms to seconds after the sandbox phase.
- RAM grows linearly with *total ever-seen* identities instead of *currently active* ones (pool never evicts).
- A container is handed to identity B that previously served identity A.

**Phase to address:** Per-user sandbox phase. **Finding: F-001 / R-001 (UX + isolation grain); cross-ref F-039/R-033 (eviction).**

---

### Pitfall 5: Half-done identity isolation — some stores scoped, others global

**What goes wrong:**
This is the headline multi-user trap and it's *already present* (F-028/R-022): conversation and approval APIs list/mutate **global** stores, while the runner later enforces context-identity against conversation-identity. So a provisioned user B can list/get/archive/delete/resolve user A's conversations and approvals; and a B-created web conversation is born owned by `local` (confirmed in `runner_conversation.go`: `NewConversationWithID` hard-codes `GetIdentityByName(ctx, localIdentityName)` regardless of `identityctx.IdentityID(ctx)`), then `/agent/run` fails later with an identity mismatch. Half-scoped isolation is *worse* than none because it looks done.

**Why it happens:**
Isolation was retrofitted onto a single-operator design where `local` was the only principal. The `identityctx` plumbing exists (`internal/identityctx`) but isn't threaded into every store/API call. Each store/handler must be individually converted; it's easy to convert the obvious ones (conversations list) and miss the long tail (approvals, search, background shells, sandbox volumes, sidecar dirs, learning stores).

**How to avoid:**
- **Enumerate every store and surface that holds per-user data and convert each to owner-scoped**: conversations (list/get/create/archive/delete/search incl. spilled-content search F-048), approvals/pauses, background shells (F-032), sandbox containers+volumes (Pitfall 3), tool-output sidecars + run-dir (F-041), learning stores (`reasoningstore`, `toolselectstore`, `activelearn` — F-049), memory subgraph, Agent.md profile dir. Make a checklist artifact; the roadmap should treat "the list of owner-scoped surfaces" as a deliverable, not implicit.
- Fix `NewConversation` to use `identityctx.IdentityID(ctx)` with `local` *only* as the CLI/no-principal fallback (exact F-028 recommended fix).
- Cross-principal get/mutate returns **404 (not 403)** to avoid an existence oracle, or 403 by deliberate choice — pick one and apply uniformly.
- Filter at the **store layer**, not just the handler, so a future caller can't bypass a handler-only filter (defense in depth — golang-security "every layer protects itself").

**Warning signs:**
- A list/search query with no `WHERE identity_id = $1`.
- A new conversation's `identity_id` is `local` when created by an authenticated non-local principal.
- Any store method that takes a conversation ID but not a principal.
- Tests assert "isolation" with a single identity (can't prove cross-identity denial — see Pitfall 13).

**Phase to address:** Multi-user identity isolation phase. **Finding: F-028 / R-022 (primary); F-032/R-026, F-039/R-033, F-041/R-034, F-048, F-049 (the long-tail surfaces).**

---

### Pitfall 6: Session eviction misses on delete; stale in-memory tool state survives

**What goes wrong:**
Some delete/clear flows delete the *persisted* conversation row directly without routing through a runner lifecycle method that evicts session-scoped in-memory state (F-039/R-033). Result: todo state, shell cwd, approval maps, and background-shell buffers survive deletion. With **deterministic conversation IDs** (Telegram chat → UUIDv5), a later chat reuses the same ID and **inherits another context's stale tool state** — a cross-identity leak once multi-user lands. Background shells make this worse: they're process-scoped, not session-keyed (confirmed in `shell_bg.go`: `Evict(string)` prunes by *completion*, ignoring the session arg), so a running job started by A is not evicted when A's conversation is deleted.

**Why it happens:**
There are multiple deletion entry points (AG-UI delete, Telegram `/clear`, CLI clear) and only some go through the runner. Background-shell registry was deliberately process-scoped for a single operator; that assumption breaks under multi-user.

**How to avoid:**
- Route **all** deletion through one runner lifecycle method that: cancels active work → auto-resolves/expires pending pauses → invokes every registered `SessionEvictor` → handles background jobs by policy → *then* deletes persistence (exact F-039 fix).
- Bind background shells to session/actor (Pitfall 8 / F-032) so eviction can target a specific owner's jobs, not just finished ones.
- On identity delete (multi-user), cascade: evict sessions, destroy the per-user sandbox container + volume (Pitfall 3), invalidate sessions (auth, Pitfall 9).

**Warning signs:**
- Two code paths call the conversation store's delete directly.
- A deterministic-ID conversation shows stale cwd/todos after a `/clear` + re-chat.
- A background job keeps running after its conversation is deleted.

**Phase to address:** Multi-user identity isolation phase (lifecycle), with the agent-loop phase for pause auto-resolution. **Finding: F-039 / R-033; cross-ref F-032/R-026.**

---

### Pitfall 7: IDOR via predictable, unscoped resource IDs

**What goes wrong:**
Background shell IDs are sequential and process-scoped (`sh_1`, `sh_2`, … — confirmed in `shell_bg.go`: `id := fmt.Sprintf("sh_%d", b.seq)`), and poll/kill accept only the ID with no owner check. Another conversation/identity in the same daemon can guess `sh_1`, poll its output (which may contain secrets), or kill it (F-032/R-026). The same IDOR class applies to any new per-user resource you add with a guessable ID and no owner binding (sandbox container IDs, approval IDs, conversation IDs if not UUIDs).

**Why it happens:**
Sequential IDs are the simplest thing that works for one operator. Owner checks feel redundant when there's only one user.

**How to avoid:**
- Random unguessable IDs (UUIDv4/crypto-random) for any cross-task-visible resource. (Note: conversation IDs already use UUIDv7 — keep that; `sh_N` is the offender.)
- Bind each background job to session/actor metadata; require matching session/actor for poll/kill (admin capability is the only override) — exact F-032 fix.
- Apply the rule uniformly to new v2 resources: per-user sandbox handles, idempotency keys, approval tokens.

**Warning signs:**
- Any ID built from a monotonic counter (`fmt.Sprintf("...%d", seq)`).
- A poll/kill/get handler that takes an ID but not a principal/session.
- Test: session B polls `sh_1` started by session A and succeeds.

**Phase to address:** Multi-user identity isolation phase. **Finding: F-032 / R-026.**

---

### Pitfall 8: ToolGateway fail-open by default (the command-hook trap, generalized)

**What goes wrong:**
Command hooks already default to **fail-open** (F-006/R-006): if the security hook crashes, times out, or is misconfigured, execution proceeds. A new central ToolGateway can inherit or repeat this: if the policy check, ledger reservation, or approval lookup errors, does the tool run or not? Fail-open means a transient infra failure silently disables the entire security boundary you just built. Related: the mutating-panic path **loses the mutating classification** (F-031/R-025), so a tool that side-effects then panics is treated as non-mutating and the completion gate is skipped.

**Why it happens:**
Fail-open feels "robust" — you don't want a hook bug to block the operator. For a trusted single operator that was defensible; for the gateway that is the production security boundary it's a critical bug. The panic-classification loss happens because the recovered result doesn't copy the descriptor's `Mutating` flag.

**How to avoid:**
- The ToolGateway and security hooks default **fail-closed** for mutating/high-risk tools (F-006 fix: default configured command hooks fail-closed, or require explicit policy when configured). Fail-open is allowed *only* for non-security enrichment hooks, and only with an explicit opt-in.
- Resolve and preserve the tool's `Mutating` flag **before** execution and copy it into panic-recovery results so the completion gate always fires after a side effect (F-031 fix).
- Make the default a function of **runtime profile**: `dev` may fail-open with a loud warning; `single_user_hardened` / `server_production` fail-closed, no override.

**Warning signs:**
- A policy/ledger/hook error path that returns "allow" or falls through to execution.
- Panic-recovery result with `Mutating=false` for a tool whose descriptor says `Mutating=true`.
- A profile where fail-open survives into production.

**Phase to address:** ToolGateway phase (defaults + panic classification), Runtime-profiles phase (profile-gated default). **Finding: F-006 / R-006, F-031 / R-025.**

---

### Pitfall 9: Auth cutover (passphrase → Authula) — lockout, session-format break, capability regression

**What goes wrong:**
Flipping the default from the HMAC passphrase cookie to Authula has several sharp edges, several already visible in the code:
- **Lockout / no-enrollment bootstrap:** Authula is configured `DisableSignUp: true` (single operator provisioned out-of-band, `authula.go`). If the cutover ships without a provisioning path, the operator is locked out — there's no sign-up and no passphrase fallback.
- **Session-format incompatibility:** the two providers issue *different cookies* (`__Host-authula_session` vs `__Host-aura_session`) and different validation cores (`validateSession` dispatches on `SessionValidator != nil`). Existing logged-in sessions break across the flip; mid-flight users get 401s.
- **CORS + auth interplay (F-022/R-020):** permissive CORS + no-auth loopback already lets a drive-by page drive the local instance. The Authula path adds CSRF (double-submit + Fetch-Metadata) but the legacy passphrase path's CSRF posture is *SameSite=Strict only* and the code itself flags "Re-evaluate if Phase 28/29 introduces a cross-origin write surface" — multi-user web is exactly that surface.
- **Capability-grants boundary break:** authz must stay `capability_grants`-based per-route (`RequireCapability`). Authula ships an access-control plugin that is *deliberately omitted* (`buildPlugins` comment). Re-enabling it, or letting Authula sessions bypass `RequireCapability`, forks the authz model.
- **Token-in-URL (F-050):** long-lived tokens accepted/advertised in query strings leak via history/logs. The setup bootstrap token must stay short-lived and setup-only.

**Why it happens:**
Auth cutovers are treated as a config flip, but they cross session format, CSRF, authz, and bootstrap simultaneously. The "default flip" hides a migration.

**How to avoid:**
- Ship an explicit **operator-provisioning path** before flipping default (CLI enroll or first-run wizard), and a **break-glass** (documented passphrase fallback or local recovery) so a bad flip isn't a permanent lockout.
- Treat existing sessions as invalidated on cutover *by design* — communicate "re-login required," don't try to translate cookie formats. Both paths already converge on the same principal/`withPrincipal` contract, so only the issuer changes.
- Add the **double-submit CSRF token** to any cross-origin or multi-user web write path (the code's own re-evaluation trigger), and replace permissive CORS wildcard with an explicit origin allowlist gated by runtime profile (F-022 fix). Set `Vary: Origin`.
- Keep `RequireCapability` as the single authz seam for both providers; do **not** enable Authula's access-control/RBAC plugin (stays out of scope — PROJECT.md NO RBAC).
- Reserve query-string tokens for short-lived setup bootstrap only; long-lived access via secure cookie/header; never log token query values (F-050 fix).

**Warning signs:**
- The flip lands with `DisableSignUp: true` and no provisioning command.
- A successful Authula login that reaches a mutating route without passing `RequireCapability`.
- Permissive CORS still wildcard in a non-dev profile; a cross-origin POST to `/agent/run` preflights OK with no auth.
- Any URL in install output / logs containing a long-lived token in the query string.

**Phase to address:** Multi-user + Authula cutover phase. **Finding: F-022 / R-020, F-050; cross-ref capability_grants boundary (CORE-03).**

---

### Pitfall 10: Runtime profiles that LIE — validation passes but unsafe defaults still apply

**What goes wrong:**
Profiles are supposed to make "production mode" an enforceable contract (F-026/F-002/F-007/F-008/F-016/F-017/F-022/F-041). The trap is profiles that *validate green but don't actually change behavior*:
- **Empty-override trap (F-002/R-002):** `.env.example` sets `AURA_SHELL_DESTRUCTIVE_PATTERNS=` and the parser treats *empty* as "disable the gate" (vs *unset* = "use defaults"). Copying the sample silently disables the destructive-shell approval. A profile validator that only checks "is the var present" passes while the gate is off.
- **Silent env fallback (F-016/R-016):** invalid int/bool env values silently use defaults (`envIntDefault`/`envBoolDefault`). An operator sets a security knob, typos it, and the runtime ignores it with no error — the profile reports healthy.
- **Relative run-dir (F-041/R-034):** `AURA_RUN_DIR=run` resolves differently per cwd; sidecars become unreadable after restart, but config validation passes.
- **Healthcheck that lies (F-008/F-017):** the container healthcheck runs `aura version`, so the container is "healthy" while the HTTP API/listener is down; readiness isn't wired to listener+dependency state.
- **Default secrets accepted (F-007/R-007):** static object-store/Garage credentials pass validation; single-replica Garage (F-018/R-017) passes as "durable."

**Why it happens:**
Validation is written to check *presence/shape*, not *effective behavior*. "Empty means disable" is a subtle parser asymmetry. Health = "process alive" is the easy check.

**How to avoid:**
- Validate **effective behavior, not presence**: the production profile asserts the destructive-shell gate is *active*, fail-policies are fail-closed, no default secrets, run-dir is absolute, listener+deps feed readiness. Add a `production-readiness` command that fails on any unmet requirement (F-026 fix).
- Fix the empty-override asymmetry: empty = "use defaults"; require explicit `off` to disable (F-002 fix). Add a config smoke test proving defaults survive copying `.env.example`.
- Production profile: invalid env for security/reliability knobs is a **fatal error**, not a silent default; dev profile warns (F-016 fix). Emit a config diagnostics report.
- Normalize `RunDir` to absolute at load or reject relative in validation (F-041 fix).
- Healthcheck → HTTP `/readyz` that reflects listener + dependency state; listener failure is fatal or flips readiness (F-008/F-017 fix).
- Reject default object-store/Garage secrets and replication-factor-1 outside dev (F-007/F-018 fix).

**Avoid the opposite failure — profile sprawl / painful local dev:** four profiles (`dev`/`local_trusted`/`single_user_hardened`/`server_production`) is the cap. Don't add per-knob profiles. `dev` must stay frictionless (loopback no-auth pass-through is *already* the design — `SecretConfigured==false` ⇒ no-op gate, confined to loopback by the boot guard). Don't make developers set 12 env vars to run locally.

**Warning signs:**
- A validator that checks `os.Getenv("X") != ""` instead of the resolved effective value.
- Copying `.env.example` disables a gate.
- Container healthy + API unreachable simultaneously.
- A typo'd security env produces no error in production.

**Phase to address:** Runtime-profiles phase. **Finding: F-002/R-002, F-007/R-007, F-008/R-008, F-016/R-016, F-017, F-018/R-017, F-022/R-020, F-026, F-041/R-034.**

---

### Pitfall 11: Over-abstracting the ToolGateway into an "atomic bomb" — OR under-building it into a bottleneck

**What goes wrong:**
Two opposite failures around the central `ToolGateway` (F-001 recommended fix: "all tool calls pass through one ToolGateway before execution and one ToolResultNormalizer after"):
- **Over-engineering:** building an ABAC/policy-DSL engine, a pluggable rule language, tenant/workspace policy trees, an external authorization-fabric API call before every tool — the enterprise "agent gateway" shape (Google/AWS/Microsoft). The operator explicitly rejects this (`feedback_no_atomic_bombs_minimal_industrial_shape`). It adds latency to *every* tool call and becomes a maintenance sink for a single-host, identity-isolation (not RBAC) system.
- **Single point of failure / bottleneck:** routing every tool through one synchronous chokepoint that does a DB ledger write + policy eval + approval lookup *inline* on the hot path. If the ledger DB hiccups, every tool stalls; if the gateway panics, the loop dies. The existing ledger is already best-effort (F-011) precisely to avoid this — but best-effort drops the forensic guarantee.

**Why it happens:**
"Central policy point" reads as "build a policy platform." And making it durable reads as "block on the DB."

**THE LINE (minimal industrial form):**
- The gateway is a **single in-process function/middleware** every tool dispatch passes through — not a service, not a DSL, not an external call. Input: `(principal, tool descriptor incl. Mutating + risk class, args, runtime profile)`. Output: `allow | deny | needs-approval`, plus a ledger reservation handle.
- Policy is **table-driven Go**, not a rule language: reuse the existing `internal/scoring/` risk tiers (SAFE/NORMAL/RISKY/DESTRUCTIVE) + the command capability classes the audit recommends. Hard-coded mapping, ~100 LOC, like the existing risk scorer.
- **Ledger: pre-execution reservation for mutating tools in production profile only** (F-011 fix). Read-only tools degrade per policy (don't block on ledger). Use the natural idempotency key already in the data model — `ConversationID + RequestID + ToolCallID` (confirmed in `toolinvocations.Event`) — so a reservation is idempotent and a retry/recovery can't double-execute (F-020 state machine: planned→authorized→started→side-effect-committed→result-persisted).
- **Not a SPOF:** the gateway is in-process (dies with the loop, no separate availability domain). The *ledger write* is the only external dependency; make it a fast single INSERT with a bounded timeout, and in non-production profiles keep it best-effort. Don't put policy eval behind a network call.
- One `ToolResultNormalizer` after execution (redaction, sidecar, untrusted-provenance marking) — also in-process, also a single function.

**What's explicitly OUT (the atomic-bomb side):** no policy DSL/Rego/OPA, no per-tenant policy store, no external auth-fabric API, no RBAC, no plugin policy modules. If a phase proposes any of those, it's over the line.

**Warning signs:**
- A "policy engine" with its own config language or external service.
- Every read-only tool call now does a synchronous DB write.
- Gateway code that imports OPA/Rego or defines a rule grammar.
- A gateway abstraction with >1 implementation or a plugin interface "for future policies."

**Phase to address:** ToolGateway phase. **Finding: F-001/R-001, F-011/R-010, F-020, F-031/R-025; minimal-form discipline per `feedback_no_atomic_bombs_minimal_industrial_shape`.**

---

### Pitfall 12: Production-ops surfaces that were "added" but never drilled / never run in CI

**What goes wrong:**
The ops findings cluster around things that *exist as code but were never exercised under real failure*:
- **Backup/restore never drilled (F-019/R-018):** a `pg_dump` handler exists but no DR restore drill with measured RPO/RTO. A backup you've never restored is a hope, not a backup. Single-replica Garage (F-018) means an artifact backup may have nothing to restore from.
- **Scheduler drain cancels in-flight work (F-035/R-035):** on SIGTERM, handler contexts are canceled immediately despite "graceful drain" comments — a long backup or agent_job fails mid-write. Stop-admission and job-work share one cancellation path.
- **systemd stop budget shorter than backup (F-043/R-036):** `TimeoutStopSec` < max backup duration ⇒ systemd SIGKILLs the scheduler mid-`pg_dump`, leaving partial output that might get promoted.
- **Load/chaos tests that don't run in CI (F-019):** adversarial prompt-injection, loop-liveness, cancellation, high-concurrency, pause/resume races have no mandatory gate. And the project's own **no-skip-as-green** rule (CLAUDE.md) is the exact trap: a chaos test that `t.Skip`s when its env is unset is a falsely-green job exercising nothing.
- **Healthcheck that lies (F-008/F-017):** covered in Pitfall 10 but it's an ops failure too — orchestrators trust a green container that can't serve.

**Why it happens:**
Ops code is written to the happy path and validated by "it compiled / it ran once." Failure behavior (kill mid-work, restore from scratch, 100 concurrent loops) is never triggered. Skip-on-missing-env makes the gate green without running.

**How to avoid:**
- **Actually drill restore:** a scripted DR drill that dumps, wipes a scratch instance, restores, and asserts data equality; record measured RPO/RTO as the contract (F-019 fix). Atomic backup promotion via temp-file + rename so a killed backup never promotes partial output (F-043 fix).
- **Separate stop-admission from job-work contexts** with an explicit drain deadline (F-035 fix); in-flight handlers keep a live context until the deadline, no new jobs admitted.
- **Align `TimeoutStopSec` ≥ longest handler duration + grace**, asserted by a static test (F-043 fix).
- **Load/chaos harness that runs in CI and fails-loud, never skips:** under `$CI`, missing env ⇒ `t.Fatal`, not `t.Skip` (project no-skip-as-green discipline). Golden tests for prompt-injection, terminal-sibling rejection, runaway-loop budgets, pause/resume races, MCP timeout, shell cancellation, background-job TTL (F-019 suggested coverage).
- **Readiness probe** reflecting listener + dependency state; listener failure fatal (F-008/F-017).

**Warning signs:**
- "Backup" handler with no corresponding restore test.
- SIGTERM during a long job marks it failed.
- A "chaos"/"load" CI job that finishes in under a second (it skipped).
- `TimeoutStopSec` numerically below the backup max duration.

**Phase to address:** Production-ops phase (backup/DR, scheduler drain, systemd budgets, load/chaos), Observability phase (readiness/health). **Finding: F-018/R-017, F-019/R-018, F-035/R-035, F-042, F-043/R-036, F-008/R-008, F-017.**

---

### Pitfall 13: Test coverage that doesn't actually prove two-identity isolation (and the dishonest 10/10)

**What goes wrong:**
The whole milestone's headline claim is "honest 10/10." The way teams fake it:
- **Single-identity "isolation" tests:** asserting one user can see their own data proves nothing about cross-user denial. You need *two authenticated identities with separate sessions* proving B cannot list/get/delete/archive/resolve A's data (exact F-028 suggested coverage). A green test with one identity is coverage theater.
- **Skip-as-green:** the integration/chaos/DR tiers `t.Skip` when their env (composed DSNs, sandbox stack, OpenRouter key) is unset, so CI is green while exercising nothing. The project already mandates `t.Fatal`-under-`$CI` skip-helpers; the new v2 tiers must adopt the same.
- **Coverage theater:** hitting an 85% line number with assertions that don't constrain behavior (`assert reply == "4"` passes if the model hallucinates without invoking the tool — the project's own anti-pattern list).
- **Probe verifies the reply, not the artifact:** an isolation/sandbox test that checks `r.Reply` instead of the filesystem/DB/container state (project rule: every Verify needs ≥1 assertion off the artifact, not the reply).

**Why it happens:**
Isolation is hard to test (needs two sessions + real stores). Skips make red go green. Line coverage is the easy metric.

**How to avoid:** see the dedicated honest-10/10 section below. In short: two-identity E2E proof, no-skip-as-green across every v2 tier, behavior-constraining assertions off artifacts, mutation-testing ≥70% on the new gateway/isolation/profile critical files, and prompt-injection regression that asserts *denial* under production profile.

**Warning signs:**
- An "isolation" test file that constructs one identity.
- A multi-user/chaos/DR CI job under ~1s runtime.
- Coverage ≥85% but the gateway's deny path has no test that proves a denied tool didn't execute.

**Phase to address:** Every v2 phase (each owns its proof), Capability-eval phase (the cross-cutting suite). **Finding: F-019/R-018, F-028/R-022; project test-discipline + no-skip-as-green + coverage-floor-85% rules.**

---

## Technical Debt Patterns

| Shortcut | Immediate Benefit | Long-term Cost | When Acceptable |
|----------|-------------------|----------------|-----------------|
| Mount host Docker socket into the sandbox for "build support" | Agent can build/run containers instantly | Root-on-host RCE; nullifies F-001 entirely | **Never** — use rootless nested builder |
| One shared workspace volume across identities | Simpler volume wiring | Cross-identity data leak (F-028 class) | **Never** in multi-user; OK only single-operator pre-v2 |
| Best-effort ledger on the tool hot path | No latency, no DB blocking | No forensic record of mutating side effects (F-011) | Read-only tools any profile; mutating tools **only** in dev profile |
| Fail-open security hook/gateway | A hook bug never blocks the operator | Transient failure silently disables the boundary (F-006) | Non-security enrichment hooks only |
| `t.Skip` when integration/chaos env unset | CI stays green locally | Falsely-green job exercises nothing (no-skip-as-green) | Local dev only; under `$CI` must `t.Fatal` |
| Sequential resource IDs (`sh_N`) | Trivial to generate/debug | IDOR enumeration once multi-user (F-032) | **Never** for cross-task-visible resources |
| Profile validator checks env presence, not effective behavior | Quick to write | Profiles that lie green (F-002/F-016) | **Never** — assert resolved behavior |
| Keep large warm pool to kill cold-start | Snappy UX | Idle RAM burn + container reuse weakens isolation (mini-PC OOM) | Pool = active identities, TTL-evicted |
| k3s/Firecracker "because industrial" | Feels enterprise-grade | ~600MB+ idle / +125% syscall overhead on a 32GB host | **Never** for v2.0.0; revisit on DGX Spark via ADR |

## Integration Gotchas

| Integration | Common Mistake | Correct Approach |
|-------------|----------------|------------------|
| rivetdev/sandbox-agent (per-user) | One shared container/volume across identities; bind-mount host paths | One warm container + named volume per identity; no host bind; user-ns remap; seccomp default-deny |
| Docker network allowlist | Pass `AURA_MCP_NETWORK_ALLOW` as env on a bridge network (advisory) | Enforce via egress proxy/firewall, or keep `--network none` (F-036) |
| Authula | Flip default with `DisableSignUp:true` and no provisioning; let sessions bypass `RequireCapability` | Ship enroll path + break-glass first; keep `RequireCapability` as sole authz seam; isolate to `authula` PG schema (already done) |
| identityctx propagation | Thread it into handlers only, miss store layer + background shells + sandbox + learning stores | Scope at the store layer for every per-user surface; enumerate the surface list as a deliverable |
| Postgres ledger reservation | Block every tool on a synchronous INSERT | Reserve only mutating tools in production; idempotency key = ConversationID+RequestID+ToolCallID; bounded timeout |
| systemd + scheduler | `TimeoutStopSec` < backup max duration; SIGTERM cancels in-flight | Stop-admission ≠ job-work context + drain deadline; `TimeoutStopSec` ≥ longest handler + grace; atomic backup rename |
| CORS + multi-user web | Permissive wildcard + no-auth loopback; SameSite-only CSRF on a cross-origin write path | Explicit origin allowlist gated by profile; add double-submit CSRF token for cross-origin/multi-user writes; `Vary: Origin` |

## Performance Traps

| Trap | Symptoms | Prevention | When It Breaks |
|------|----------|------------|----------------|
| gVisor/runsc for the agent shell | Builds/`git`/`npm` run 2x+ slower | Use plain containers (namespaces+cgroups+seccomp); reserve gVisor for DGX-Spark future | Any syscall/IO-heavy task (i.e. the core feature) |
| k3s control plane on the mini-PC | Idle RAM +~600MB, constant CPU heartbeat | Container-per-user over Docker, no orchestrator | Concurrent users + LLM sidecars push past ~7GB → OOM |
| Per-request cold-start | First-tool-call p95 in seconds | Warm container per *active identity*, lazy-start + idle-TTL evict | Idle gaps between turns; container per request |
| Unbounded warm pool | RAM grows with total-ever-seen identities | Cap via `AURA_SANDBOX_MAX_CONCURRENT_SESSIONS`; evict on logout/idle | More provisioned identities than RAM allows warm |
| Synchronous ledger on hot path | Every tool call waits on a DB write | Mutating-only reservation in production; bounded timeout; async/best-effort for read-only | DB latency spike stalls the whole loop |
| Unbounded learning stores | Heap + Neo4j rows grow forever | Max examples per label/tool, TTL/compaction, bounded `seen` map, metrics (F-049) | Long-running deployment, high-cardinality inputs |

## Security Mistakes

| Mistake | Risk | Prevention |
|---------|------|------------|
| Docker socket / `--privileged` / `--network host` in sandbox | Root-on-host RCE; F-001 nullified | Unrepresentable in the sandbox profile struct; user-ns remap |
| Advisory egress allowlist on a bridge network | "Sandboxed" MCP/sandbox reaches arbitrary hosts (F-036) | Proxy/firewall enforcement or `--network none` |
| Global conversation/approval stores under multi-user | Cross-identity read/mutate/delete (F-028) | Owner-scope every store at the store layer; 404/403 on cross-principal |
| Predictable `sh_N` IDs, no owner check | IDOR: poll/kill another user's job, read secrets (F-032) | Random IDs + session/actor binding for poll/kill |
| Fail-open gateway/hook | Transient failure disables the boundary (F-006) | Fail-closed for mutating/high-risk; profile-gated |
| Mutating tool panics → classified non-mutating | Completion gate skipped after side effect (F-031) | Preserve `Mutating` flag into panic recovery |
| DB-stored sidecar path read directly | Arbitrary local file read into history (F-005) | Reconstruct path from runDir+convID+seq; containment + reject symlinks |
| Default object-store/Garage secrets pass validation | Artifact store compromise if reused (F-007) | Reject defaults outside dev profile |
| Long-lived token in URL query | Leak via history/logs/proxy (F-050) | Query tokens setup-only/short-lived; cookies/headers for long-lived |
| Permissive CORS + no-auth loopback | Drive-by page drives local Aura (F-022) | Explicit origin allowlist; refuse permissive CORS when auth disabled outside dev |
| Mixed `url`+`command` MCP entry, empty trust | Appears remote-HTTP, launches local command (F-027) | One canonical transport classifier; reject ambiguous; empty trust = blocked (F-013) |
| Strict JSON not enforced on privileged routes | Trailing values / unknown fields silently accepted (F-052) | DisallowUnknownFields + single-decode EOF + size cap + content-type |

## UX Pitfalls

| Pitfall | User Impact | Better Approach |
|---------|-------------|-----------------|
| Cold-start stall on first tool call | Agent feels broken vs instant host shell today | Warm-per-identity + lazy-start; sub-second first call |
| Local dev needs many env vars / auth to run | Developers stop using the runtime locally | `dev` profile = loopback no-auth pass-through (already designed); keep frictionless |
| Auth cutover invalidates sessions with no notice | Operator/users 401'd mid-task, confused | Communicate "re-login required"; ship provisioning + break-glass first |
| Outside-workspace approval advertised but not wired | Agent loops / repeatedly asks (F-009) | Implement the send-file resume hook or remove the advertised route |
| Spilled conversation content not searchable | Important terms vanish from search (F-048) | Searchable preview column or document exclusion explicitly |

## "Looks Done But Isn't" Checklist

- [ ] **Per-user sandbox:** Often missing real egress enforcement and per-identity volumes — verify a disallowed host is unreachable and user B can't read user A's workspace (live container test, not a <1s skip).
- [ ] **Multi-user isolation:** Often missing the long-tail stores (approvals, background shells, sandbox, sidecars, learning stores) — verify a two-identity E2E denies B on *every* surface, and `NewConversation` owns by the authenticated principal not `local`.
- [ ] **Session eviction:** Often missing on one of the three delete paths — verify AG-UI delete, Telegram `/clear`, and CLI clear all invoke every `SessionEvictor` and handle running background jobs.
- [ ] **ToolGateway:** Often missing fail-closed default and panic-classification preservation — verify a denied mutating tool did NOT execute (artifact check) and a mutating-then-panic still arms the completion gate.
- [ ] **Ledger durability:** Often missing pre-execution reservation in production — verify a mutating tool is blocked when ledger reservation fails (production profile) while read-only degrades.
- [ ] **Runtime profiles:** Often missing effective-behavior validation — verify copying `.env.example` does NOT disable the destructive-shell gate, and a typo'd security env is fatal in production.
- [ ] **Auth cutover:** Often missing provisioning + break-glass — verify a fresh deploy can enroll the first operator and recover from a bad flip without a permanent lockout.
- [ ] **Healthcheck/readiness:** Often missing dependency awareness — verify `/readyz` goes red when the listener or a dependency fails (not just `aura version`).
- [ ] **Backup/DR:** Often missing an actual restore — verify a scripted dump→wipe→restore→data-equality drill with recorded RPO/RTO.
- [ ] **Scheduler drain:** Often missing the stop-admission/job-work split — verify SIGTERM during a long backup keeps the handler context live to the drain deadline.
- [ ] **Load/chaos/prompt-injection gates:** Often missing because they skip — verify they `t.Fatal` (not `t.Skip`) under `$CI` and assert *denial* under production profile.

## Recovery Strategies

| Pitfall | Recovery Cost | Recovery Steps |
|---------|---------------|----------------|
| Docker-socket/host-bind shipped in sandbox | HIGH | Treat as security incident: rotate host secrets, rebuild sandbox image without host access, add struct-level ban + test |
| Half-scoped isolation (some stores global) | MEDIUM | Audit every store for `WHERE identity_id`; add store-layer scoping; backfill two-identity tests; data-leak review of access logs |
| Chose k3s/gVisor, hit resource/perf wall | MEDIUM | Rip out orchestrator; revert to container-per-user over Docker; record ADR (F-025) on why |
| Auth cutover lockout | HIGH (if no break-glass) / LOW (if planned) | Break-glass recovery path; if absent, manual DB session/credential repair — hence ship break-glass *before* flip |
| Fail-open gateway in production | MEDIUM | Flip defaults fail-closed; audit which mutating actions ran during the open window via ledger (if durable) |
| Backup that can't restore | HIGH | Restore drill reveals it; fix dump format/atomic-promote; never count a backup until one restore succeeds |
| Profile that lied (gate was off) | MEDIUM | Add effective-behavior validation; audit production deployments that used the lying profile |

## Pitfall-to-Phase Mapping

| Pitfall | Prevention Phase (logical owner) | Verification | Findings |
|---------|----------------------------------|--------------|----------|
| 1. Host-leak via mount/socket/network | Per-user sandbox | Sandbox can't reach host daemon/services; no host bind in spec | F-001/R-001, F-036/R-030 |
| 2. Heavyweight isolation runtime | Per-user sandbox (design fork) | Idle RAM ≤~7GB; builds not 2x slower; no k3s/microVM | F-001/R-001 |
| 3. Advisory egress / shared volumes | Per-user sandbox | Disallowed host unreachable; per-identity volume isolation test | F-036/R-030, F-028 |
| 4. Cold-start / warm-pool trap | Per-user sandbox | First-tool-call sub-second; pool = active identities; no cross-identity reuse | F-001/R-001, F-039 |
| 5. Half-done identity isolation | Multi-user isolation | Two-identity E2E denies B on every surface; conversations owned by principal | F-028/R-022 + long tail |
| 6. Session eviction misses on delete | Multi-user isolation + agent-loop | All 3 delete paths evict; background jobs handled | F-039/R-033, F-032 |
| 7. IDOR via predictable IDs | Multi-user isolation | Session B can't poll/kill A's shell | F-032/R-026 |
| 8. Fail-open gateway/hook + panic classification | ToolGateway + Profiles | Denied mutating tool didn't run; panic arms completion gate | F-006/R-006, F-031/R-025 |
| 9. Auth cutover sharp edges | Multi-user + Authula cutover | Provisioning + break-glass; CSRF on cross-origin; capability seam intact | F-022/R-020, F-050 |
| 10. Profiles that lie | Runtime profiles | `.env.example` doesn't disable gate; typo'd security env fatal in prod; `/readyz` honest | F-002/F-007/F-008/F-016/F-017/F-018/F-041/F-026 |
| 11. Gateway over/under-build | ToolGateway | In-process, table-driven, mutating-only reservation; no DSL/external service | F-001/F-011/R-010/F-020 |
| 12. Ops never drilled / CI skips | Production-ops + Observability | Restore drill w/ RPO/RTO; drain deadline; chaos `t.Fatal` not skip | F-018/F-019/F-035/F-042/F-043/F-008/F-017 |
| 13. Isolation tests don't prove isolation | All phases + Capability-eval | Two-identity proof; mutation ≥70% on gateway/isolation files; injection denial | F-019/R-018, F-028/R-022 |

---

## What an HONEST 10/10 Requires (evidence bar — so the score isn't gamed)

"10/10 production readiness" (up from the audit's 4.6/10) is only honest if it is **evidenced by artifacts and live execution**, not by ticking findings closed. The bar:

1. **All 51 findings closed with a reproducing test that fails before the fix, passes after** (project TDD-reverse rule). Closing a finding without a regression test is reopening it later. Each P1 needs the specific suggested coverage from `bug-report.md` (e.g. F-028 → two authenticated identities, separate sessions, B denied on list/get/delete/archive/resolve).

2. **Two-identity isolation proven end-to-end, live.** A test harness with two real authenticated sessions over the real stores proving cross-identity denial on *every* owner-scoped surface (conversations, approvals, background shells, sandbox volumes, sidecars, learning stores, memory). A single-identity "isolation" test is theater (Pitfall 13).

3. **No-skip-as-green across every v2 tier.** Integration, sandbox, multi-user, load, chaos, DR, and prompt-injection tiers `t.Fatal` under `$CI` when their env is unset — a skipped tier *fails* the gate, never passes it. A sub-second "integration"/"chaos" runtime is a skip tell; verify execution, not just PASS (project no-skip-as-green discipline). Integration/owned-surface coverage stays ≥85% measured across the full tag matrix (project floor, overrides PRD's 75/60).

4. **Mutation testing ≥70% killed on the new critical files** (ToolGateway policy, identity-scoping store layer, runtime-profile validator, sandbox launcher, auth cutover seam) — documented in each phase's VALIDATION.md Manual-Only table, run live on WSL (the only go1.26 fork). Behavior-constraining assertions, not line-count coverage; reject the project's named anti-patterns (`assert reply == "4"`, no-syscall-verification, etc.).

5. **Prompt-injection regression suite that asserts DENIAL under production profile.** A golden corpus of injected "run destructive shell / read absolute path / reach disallowed host" prompts; under `server_production` the ToolGateway must *deny* (artifact check that the side effect did NOT happen), not merely log (F-019 suggested coverage).

6. **Drilled DR, not hoped DR.** A scripted dump→wipe-scratch→restore→assert-data-equality run, with measured RPO/RTO recorded as the deployment contract (F-019). A backup counts only once a restore has succeeded.

7. **Honest health/readiness.** `/readyz` proven to go red on listener or dependency failure (F-008/F-017); the Compose/systemd probe uses it, not `aura version`.

8. **Profiles validate effective behavior.** A `production-readiness` command (F-026) that fails on any unmet requirement, plus a config smoke test proving copying `.env.example` does not disable the destructive-shell gate (F-002) and that a typo'd security env is fatal in production (F-016).

9. **Artifact-grounded verification.** Every Verify asserts off the artifact (filesystem/DB/container/API state), never off `r.Reply` (project rule). Inspect bodies visually for mojibake/structure, not just PASS status.

10. **ADRs + release-readiness checklist exist and are referenced** (F-025/F-026/F-045/F-049): the isolation-runtime decision (container-per-user over k3s/gVisor), the gateway minimal-form line, the multi-user-without-RBAC scope, the DR contract — each an ADR, so the 10/10 is *traceable*, not asserted.

**The anti-gaming rule:** if a finding is "closed" but the only evidence is a code change with no failing-then-passing test, a skipped tier, a single-identity test, a healthcheck that still runs `aura version`, or a backup never restored — it is **not closed**. The honest score is bounded by the weakest evidence, not the count of green checkmarks.

---

## Sources

- `d:\Aura\docs\audit\bug-report.md` — 51 findings F-001..F-052 with reproduction + suggested coverage (HIGH)
- `d:\Aura\docs\audit\risk-register.md` — R-001..R-036 probability/impact (HIGH)
- `d:\Aura\docs\audit\security-audit.md` — trust boundaries, prompt-injection surfaces, ToolGateway/Normalizer recommendation (HIGH)
- `d:\Aura\.planning\PROJECT.md` — locked v2.0.0 scope, decisions (NO RBAC, container-per-user fork, Authula cutover), mini-PC budget (HIGH)
- `d:\Aura\.planning\codebase\CONCERNS.md` — capability_grants scaffolding, seccomp/SSRF/risk-tier design, no-skip-as-green roots (HIGH)
- Actual code read: `internal/runner/runner_conversation.go` (F-028 `local` ownership), `internal/agent/tools/shell_bg.go` (F-032 `sh_%d` IDs, process-scoped Evict), `internal/identityctx/identityctx.go` (propagation seam), `internal/webauth/authula.go` + `internal/agui/auth.go` (cutover seams, CSRF re-eval note, capability gate), `internal/toolinvocations/store.go` (idempotency key in data model) (HIGH)
- golang-security / golang-concurrency / golang-testing SKILL.md — fail-closed, client-header anti-pattern, IDOR, goroutine-leak, two-identity test discipline, no-skip (HIGH)
- KubeBlocks containerization benchmark + gVisor performance docs — gVisor +125% on syscall/IO-heavy workloads, ~10-30% typical (MEDIUM, verified multiple sources): https://kubeblocks.io/blog/does-containerization-affect-the-performance-of-databases , https://gvisor.dev/docs/architecture_guide/performance/
- k3s resource profiling + idle-footprint discussions — ~565MB-768MB idle, ~1.6GB guidance, non-negligible idle CPU (MEDIUM): https://docs.k3s.io/reference/resource-profiling , https://github.com/k3s-io/k3s/discussions/3558
- AI-agent sandbox isolation surveys — warm-pool/cold-start tension, container-reuse weakens isolation, ~1GB/microVM (MEDIUM): https://northflank.com/blog/sandboxes-on-kubernetes , https://manveerc.substack.com/p/ai-agent-sandboxing-guide
- Enterprise agent-gateway references (Google/AWS/Microsoft) — the over-engineered shape to avoid for a single-host identity-isolation system (MEDIUM, used as the anti-pattern boundary): https://docs.cloud.google.com/gemini-enterprise-agent-platform/govern/gateways/agent-gateway-overview

---
*Pitfalls research for: Aura v2.0.0 Industrial Hardening & Multi-User Production*
*Researched: 2026-06-29*
