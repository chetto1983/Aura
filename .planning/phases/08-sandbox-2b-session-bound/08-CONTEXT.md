# Phase 8: Sandbox 2b Session-Bound - Context

**Gathered:** 2026-06-03
**Status:** Ready for planning
**Method:** Research-grounded discussion across all 4 selected gray areas, per user directive "deep research on D:/tmp + industrial 2026 pattern". Four parallel research agents triangulated curated D:/tmp sources (`agent-infra-sandbox`/AIO, OpenAI `codex` Rust sandbox, `adk-go-study/session`, nanobot/picobot/hermes) against 2026 industrial patterns (E2B, Modal, Daytona, Anthropic Code Execution + Claude Code on the Web, OpenAI Code Interpreter, gVisor networking, Go 1.24+ `os.Root`). The PRD locks most of the WHAT (Slice 2b: session-bound, workspace mount, network allowlist, `sandbox_sessions`, migration 0010); this CONTEXT captures the HOW + **five architectural decisions that require PRD-amendment commits before planning** (see `### Required PRD Amendments`). Four user decisions were ratified explicitly (see each Area). This phase extends the shipped Phase 5 2a runner — read `05-CONTEXT.md` first.

<domain>
## Phase Boundary

Extend the shipped **2a stateless runner** (`internal/sandbox/`) into **session-bound execution keyed by `conversation_id`**. Adds: per-conversation **persistent execution state**, a per-conversation **RW workspace mount** (`$AURA_RUN_DIR/conversations/<id>/workspace/` → `/workspace`, `nosuid,nodev,noexec`, owner `65532`, 100 MiB quota), an **opt-in network egress allowlist** (default deny), an **idle-TTL reaper** (`AURA_SANDBOX_SESSION_TTL_SEC=1800`, 60s sweep, hard cap `AURA_SANDBOX_MAX_CONCURRENT_SESSIONS=5`), the **`aura.sandbox_sessions`** table (migration 0010), a **host-side symlink-escape guard** on cleanup walkers, and admin CLI `aura sandbox sessions {list|terminate|prune}` + `aura exec --session <conv_id>`. The `execute` tool gains an optional `session_id` arg (default = `conversation_id`, already reaching tools via `WithToolCallContext`). Needed by **P11 Skills 7e** snippet execution.

**Inherits, does not re-decide (locked by Phase 5 / D12):** gVisor `runsc` on x86 + hardened-container/seccomp-allowlist/userns-remap floor, `cap_drop: ALL` + `no-new-privileges`, the `Runner` interface, lean ToolResult format (D-17), typed `ErrSandboxUnreachable`/`ErrSandboxProtocol`, deferred `execute` tool, hand-rolled `aura exec` dispatcher.

**Out of scope:** arbitrary on-demand `pip install` UX beyond the allowlist plumbing; LLM-driven SandboxEscapeBench red-team (Phase 5 D-03, scheduled/manual); the Scheduler/Skills *application* pipelines that will consume the scoring module (P10/P11). microVM tier stays rejected (D-05a).

</domain>

<decisions>
## Implementation Decisions

> All decisions are HOW (implementation) unless flagged as overriding locked PRD content. Each is the planner's default unless research surfaces a concrete reason to deviate. Research grounding cited where triangulated. **Five decisions deviate from locked PRD/ROADMAP wording and are gated behind PRD-amendment commits — see the dedicated section.**

### Area 1 — Session state model: BOTH tiers, persistent interpreter (user-ratified)

> **Grounding:** AIO/agent-infra runs both tiers split by runtime — Python = persistent Jupyter/IPython kernel keyed by `session_id` so `x=42` survives (`CodeExecuteRequest.ts:21-24`, `code-execution.mdx:76-93`), bash = tmux but only API-managed `cwd` survives (`BashExecRequest.ts:10`). 2026 consensus (OpenAI Code Interpreter, E2B `run_code`): a stateful kernel tier + a stateless exec tier coexist; idle-kernel RAM is the cost driver (evict 5-20min). User chose **"Both tiers (persistent interp + files)"**.

- **D-01 — The session sidecar holds a long-lived Python interpreter per `session_id`** so in-memory state (`x=42` set in call 1) is readable in call 2 — honoring `prd.md:1252` verbatim — PLUS the RW workspace mount for file persistence (honoring ROADMAP success-criterion #1). The two PRD persistence claims are NOT contradictory; they are the two tiers. **Resolves the PRD internal tension.** ⚠️ Extends the 2a `subprocess.run`-per-call sidecar → PRD-amendment item 1.
- **D-02 — Persistence model is asymmetric and must be documented to the model:** Python in-memory vars persist (interpreter pinned to session); shell `cd`/`export` do NOT persist across calls (steal AIO's explicit contract — only the API-managed working dir is re-applied) — avoids the silent-env-drift footgun of a long-lived shell. Workspace files persist for both langs.
- **D-03 — Idle interpreter is the RAM cost driver** (mini-PC 32GB budget, [[feedback_minipc_cpu_budget]]). Bounded by the hard cap of 5 concurrent sessions + 1800s idle-TTL eviction (reaper kills the container, frees the interpreter). Planner: interpreter mechanism (persistent `python -i`/REPL-over-stdin vs. an IPython kernel) is Claude's discretion within "in-memory `x=42` survives, sidecar server stays stdlib-only".

### Area 2 — Container isolation model: per-conversation container via control plane (user-ratified)

> **Grounding:** AIO = single shared container, logical sessions = explicit anti-model for isolation (sudo user, full net, lost on restart). 2026 consensus is **unanimous**: one dedicated instance per session, provisioned by a **control plane**; the SDK never drives the runtime — it asks the orchestrator, then speaks HTTP to the instance (Modal = gVisor-per-sandbox, E2B = Firecracker-per-sandbox). Maps onto Aura 1:1: `sandbox_sessions` (Postgres) IS the control-plane state table. User chose **"Per-conversation container (control plane)"**.

- **D-04 — One gVisor `runsc` container per `conversation_id`**, lifecycle owned by the `SessionManager` "control plane" (`internal/sandbox/sessions.go`). Strong cross-conversation isolation = the 2026 consensus; defensible with gVisor (not Firecracker) because Aura's threat model is one/few **trusted** users, NOT mutually-distrusting tenants (coupling-risk note: "sandbox session per-conversation, per-identity is v2" — research SUMMARY.md:52).
- **D-05 — Contained D-08 carve-out: the SessionManager DOES shell out `docker`** (`docker run`/`stop`/`rm` per session) for *lifecycle* — but *execution* stays HTTP to the per-session container's port. Precedent: D-09 already shells `docker compose up -d` for auto-start. The 2a "Go never drives the docker runtime" invariant is narrowed to "Go never drives *execution* via docker; lifecycle orchestration is the control-plane's job". ⚠️ PRD-amendment item 2. **MUST NEVER mount the docker socket into Aura or any sidecar** (re-introduces SandboxEscapeBench escape vector #1 — keep config-regression at 0, per Phase 5 D-08).
- **D-06 — `aura.sandbox_sessions` is the control-plane registry** (migration 0010 as locked). Boot recovery: rows `status='active'` at boot → `'terminated'`, container recreated lazily on next call (PRD-locked, parity with `agent_job_runs`). **Workspace files survive an Aura restart** (host dir persists); **in-memory interpreter state does NOT** (container was reaped) — the durability promise the planner must encode in tests and surface to the model.
- **D-07 — Container lock per `session_id`** (sync.Map + mutex) serializes concurrent `execute` calls within a session (PRD-locked). Forward-compat: swarm workers reusing the parent `SessionID` (OQ7 / amendment #34-B) share the container and serialize on this lock — sandbox parallelism does NOT extend to the fan-out unless a RISKY-tier dedicated container is forced. Capture as a timing-aware note for the swarm slice.

### Area 3 — Network egress: host-side proxy allowlist, NOT in-container iptables (user-ratified — PRD pivot)

> **Grounding — decisive, three independent sources converged:** (a) iptables/nftables **require `CAP_NET_ADMIN`** → directly contradicts the locked `cap_drop: ALL`/D12 floor; (b) under gVisor, iptables touches only the *virtual* netstack (partially implemented), not host netfilter, and letting untrusted code own its own firewall is the anti-pattern; (c) what everyone actually ships (Anthropic Claude Code, OpenAI Codex `network-proxy`) = strip egress, force all traffic through a **host-side forward proxy** doing hostname-CONNECT allowlisting, default-deny, **resolve-then-pin** (DNS-rebinding guard). User chose **"Host-side proxy allowlist (amend PRD)"**. This is exactly what `05-CONTEXT.md` deferred-note already flagged as the reference pattern.

- **D-08 — Egress is enforced OUTSIDE the sandbox by a Go host-side forward proxy** (`internal/sandbox/network.go` reshaped). Default-deny; allowlist from `AURA_SANDBOX_NETWORK_ALLOW_HOSTS` (CSV). Per-session policy keyed by `conversation_id`. The session container otherwise stays egress-isolated; opt-in traffic routes through the proxy. ⚠️ PRD-amendment item 3 (replaces "iptables OUTPUT rules" / "`network_mode: bridge` + iptables").
- **D-09 — Reuse Slice 5 `internal/web` SSRF machinery verbatim** ([[reference_web_tools_e2e_and_agent_run]]): `safeDialContext` IP-classification + DNS-rebinding pin + redirect re-validation. Codex's policy primitives map directly to Go: deny-wins precedence, glob domains (`pypi.org`, `*.pythonhosted.org`), block an allowlisted host that resolves to a private IP. The sidecar gets `HTTP_PROXY`/`HTTPS_PROXY`/`PIP_PROXY`/`NPM_CONFIG_PROXY` env pointing at the proxy.
- **D-10 — Hostname/SNI granularity only; NO MITM.** CONNECT-validation against the hostname allowlist is sufficient for `pip install` use cases and avoids Codex's MITM CA-injection complexity. **Security caveat (must land in 08-SECURITY):** documented Claude Code allowlist *bypasses* enabling data-exfil exist — validate at CONNECT, guard DNS-rebinding, watch request-smuggling. Honor `AURA_PRIVACY_MODE=local-only` (D00.5): a non-empty allowlist under local-only must fail-fast or be inert.

### Area 4 — Risk-tier governance: build the FULL shared `internal/scoring/` module now (user-ratified — pulled forward)

> **Grounding:** The PRD already fully specs `internal/scoring/scoring.go` (~100 LOC, prd.md:4534+) under §Risk-Based Governance — a self-contained pure module (no DB): `RiskTier` enum {safe,normal,risky,destructive}, `ComputeTaskTier`/`ComputeSkillTier`/`ComputeSandboxTier`, `GateRecommended`, `RequiresImmediateAlert`, UP-only modifiers, `AURA_RISK_ALERT_THRESHOLD`. It is currently scheduled to land with Slice 6 (Scheduler). User chose **"Full governance framework now"**.

- **D-11 — Phase 8 ships the full shared `internal/scoring/` module** (`scoring.go` + exhaustive `scoring_test.go`, no DB) — pulled forward from Slice 6 to Phase 8. Includes all three `Compute*Tier` functions + `GateRecommended` + `RequiresImmediateAlert` + the modifier table + the env threshold. Scheduler (P10) and Skills (P11) then *consume* it (no re-build). ⚠️ PRD-amendment item 5 (migration of the file target's home slice).
- **D-12 — SCOPE GUARD: build the MODULE, not the per-slice APPLICATION pipelines.** `ComputeTaskTier`/`ComputeSkillTier` are built + unit-tested now but have **no runtime consumers until their slices** — Phase 8 must NOT build the scheduler `pending_approval` flow, the `agent_job_runs` audit columns, or the skills pending dir (those are P10/P11 and would be scope creep). Phase 8 wires ONLY the **sandbox advisory path**: `ComputeSandboxTier(args)` → `network_allow` empty=SAFE, `pypi.org`-only=SAFE-bump, arbitrary domains=RISKY/IRREVERSIBLE → tool result carries `{risk_tier, gate_recommended}` (the sandbox gate is **advisory** per PRD "gate consigliato"; no pending-state persistence for sandbox in v1).

### Area 5 (bonus, surfaced by research) — Symlink-escape guard: `os.Root`/openat2, not literal `O_NOFOLLOW`

> **Grounding:** Pitfall #2 (workspace symlink escape) is the P8 security focus (research SUMMARY.md:107). Research found **CVE-2026-39861 is Aura's exact shape** (sandbox writes a symlink → unsandboxed host parent follows it out of root). Go 1.26 (Aura's toolchain) ships `os.Root`/`os.OpenInRoot` (openat2-based, rejects `..` + escaping symlinks) — strictly stronger than `O_NOFOLLOW`. User did NOT pick the "literal O_NOFOLLOW" option.

- **D-13 — Host-side workspace walkers use `os.Root`/`os.OpenInRoot`** (`internal/sandbox/workspace.go` `walkSize` quota check + the `Conversations.Delete` cascade at `store.go:430`). ⚠️ Minor PRD-amendment item 4 (`O_NOFOLLOW` → `os.Root`).
- **D-14 — Cascade delete is a manual no-follow openat walk, NOT `os.RemoveAll`.** `os.Root` has no `RemoveAll` yet (golang/go#67002): recursive cleanup of an attacker-controlled tree must be an openat-anchored walk that refuses to follow symlinks out of the workspace root. The acceptance test (ROADMAP #2: `ln -s /etc /workspace/escape` then host cascade) gates this directly.

### Claude's Discretion
- Persistent-interpreter mechanism (D-03): REPL-over-stdin vs. IPython kernel — invariant is "`x=42` survives + sidecar server stays stdlib-only + fits the RAM cap".
- Exact per-session port allocation / container naming scheme for D-04/D-05.
- Proxy implementation shape (D-08): stdlib `net/http` `CONNECT` handler vs. a small SOCKS5 — invariant is "default-deny hostname allowlist + resolve-then-pin + reuses Slice 5 IP classification".
- DNS resolution cache TTL (PRD proposes 5 min; validate against `pypi.org` rotating A-records — PRD open question #6).
- `aura sandbox sessions` subcommand output formatting.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### PRD + roadmap + locked decisions
- `prd.md` §Slice 2 (lines ~1200–1376) — Sandbox runner: 2b acceptance, file targets (2a + 2b tables), open questions, commit templates. **Truth-source for WHAT.** Note the 5 amendments below override parts of it.
- `prd.md` §Risk-Based Governance (lines ~4459–4548+) — the full `internal/scoring/` module spec (tier enum, Compute* funcs, modifiers, pipeline, `AURA_RISK_ALERT_THRESHOLD`) pulled forward per D-11.
- `prd.md:1252` — the "var Python `x=42` leggibile nella seconda call" test (honored by D-01).
- `.planning/ROADMAP.md` §"Phase 8: Sandbox 2b Session-Bound" (lines ~263–276) — goal + 4 success criteria. Goal wording (iptables, O_NOFOLLOW) amended per items 3+4.
- `.planning/REQUIREMENTS.md` CAP-02 (line ~33) — the requirement this phase satisfies.
- `.planning/DECISIONS.md` D12 (amendment #36) — inherited isolation primitive (gVisor + `cap_drop: ALL` floor) — the basis for the D-08 network pivot. D14 amendment #34-B — swarm session-reuse serializes on the container lock (D-07).
- `.planning/phases/05-sandbox-2a-stateless/05-CONTEXT.md` — **READ FIRST.** All inherited 2a decisions (D-05 gVisor, D-08 HTTP-only, D-16/D-17 wire+result, D-18 error taxonomy, D-19 CLI). The deferred-note already names the host-proxy egress model as the 2b reference.
- `.planning/phases/05-sandbox-2a-stateless/05-SECURITY.md` — 2a threat register + trust boundaries to extend (T-05-02 EOP-ROOT, userns-remap assertion).
- `.planning/research/SUMMARY.md` lines 52, 56, 62, 107 — coupling risks + Pitfall #1 (escape) / #2 (workspace symlink) for the P8 re-audit.

### Existing code (the integration surface)
- `internal/sandbox/sandbox.go` — `Runner` interface + `Result` struct (extend for session-bound, do not break the stateless path).
- `internal/sandbox/docker.go` (201 LOC) — 2a HTTP client + auto-start (D-09). The lifecycle carve-out (D-05) lands alongside.
- `internal/sandbox/errors.go` — `ErrSandboxUnreachable`/`ErrSandboxProtocol` sentinels to extend.
- `internal/agent/tools/result.go` — `WithToolCallContext(ctx, sessionID, …)` is HOW `execute` already reads `conversation_id` (no `InvocationContext` change needed) + `toolCallCtx` + `validateID` traversal guard.
- `internal/conversations/store.go:430` — `Store.Delete(ctx, conversationID)` — the cascade hook for D-13/D-14 workspace cleanup; `store.go:63` `RunDir` field.
- `internal/config/config.go:53-55,144` — existing `SandboxURL/TimeoutSec/Runtime` + `RunDir`; add the new `AURA_SANDBOX_*` 2b vars following `envDefault`/`envIntDefault`.
- `internal/web/` (`fetcher.go` `safeDialContext`, SSRF/DNS-rebinding) — reused verbatim by the egress proxy (D-09).
- `cmd/aura/main.go` — hand-rolled switch dispatcher: `aura exec --session` + `aura sandbox sessions {list|terminate|prune}` land as new cases.
- `sandbox/sidecar.py` — gains per-session interpreter + `/session/{id}/exec/{lang}` (D-01); server stays stdlib-only.

### External research (grounding, not requirements)
- agent-infra-sandbox (AIO): persistent-kernel session API + the anti-isolation lesson — `D:/tmp/agent-infra-sandbox` (`code-execution.mdx`, `bash/.../BashExecRequest.ts`).
- OpenAI Codex `network-proxy` + `linux-sandbox`: the proxy-as-policy-engine, deny-wins, DNS-rebinding-by-IP-classification, glob domains — `D:/tmp/codex/codex-rs/network-proxy/src/{config.rs,runtime.rs,policy.rs}`.
- Go `os.Root`: https://go.dev/blog/osroot ; no-RemoveAll caveat: https://github.com/golang/go/issues/67002
- gVisor security/networking (iptables-in-gVisor is virtual + partial): https://gvisor.dev/docs/architecture_guide/security/ , /networking/
- Docker `CAP_NET_ADMIN` required for iptables (incompatible with `cap_drop: ALL`): https://oneuptime.com/blog/post/2026-01-25-docker-container-capabilities/view
- CVE-2026-39861 (Claude Code sandbox symlink-escape, Aura's exact shape): https://www.sentinelone.com/vulnerability-database/cve-2026-39861/
- Claude Code allowlist bypass / data-exfil (D-10 caveat): https://oddguan.com/blog/second-time-same-sandbox-anthropic-claude-code-network-allowlist-bypass-data-exfiltration/
- E2B persistence (auto-pause, memory snapshot): https://e2b.dev/docs/sandbox/persistence ; Modal sandbox networking (CIDR allowlist, idle TTL): https://modal.com/docs/guide/sandbox-networking

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `WithToolCallContext`/`toolCallCtx` (`tools/result.go`): `execute` already receives `sessionID` (= conversation id) — the PRD's "session_id default = conversation_id dal InvocationContext" is satisfied by the existing context-value path; no `InvocationContext` field add.
- `internal/web` `safeDialContext` + SSRF/DNS-rebinding pin + `CheckRedirect` re-validation — the egress proxy's IP-classification core (D-09), reused not reinvented.
- `Conversations.Store.Delete` (`store.go:430`) — extend with the workspace cascade (D-13/D-14); `Store.RunDir` gives the host root.
- `config.envDefault`/`envIntDefault` — add `AURA_SANDBOX_SESSION_TTL_SEC` (1800), `AURA_SANDBOX_MAX_CONCURRENT_SESSIONS` (5), `AURA_SANDBOX_WORKSPACE_MAX_BYTES` (104857600), `AURA_SANDBOX_NETWORK_ALLOW_HOSTS` (empty), `AURA_RISK_ALERT_THRESHOLD` (risky).
- 2a `docker.go` auto-start helper (D-09) — the precedent for the SessionManager's contained `docker` lifecycle carve-out (D-05).

### Established Patterns
- **Deferred-tool partition** (CLAUDE.md): `execute` stays `Deferred:true`; the new `session_id` arg extends the existing schema, no new deferred tool for sessions (admin lives in CLI).
- **Hand-rolled CLI dispatcher** (`cmd/aura/main.go` switch) — `aura sandbox sessions …` is new cases, not cobra.
- **Typed sentinel errors** — add session-specific sentinels (e.g. `ErrSessionCapReached`) following `ErrSandboxUnreachable`.
- **Build-tag integration tier** `//go:build sandbox_integration` + no-skip-as-green (`t.Fatal` under `$CI`) + ≥85% coverage floor (CLAUDE.md). TTL reap tested deterministically via `synctest` (PRD line 1720).
- **goleak.VerifyNone** mandatory — 2b adds the reaper goroutine (amendment #15 list includes 2b).
- **sqlc one-file-one-query** anti-god-class — `sandbox_sessions.sql` = 4 queries (`InsertSession`, `TouchLastUsed`, `MarkTerminated`, `ListActive`).

### Integration Points
- `internal/sandbox/sessions.go` (NEW ~150) — `SessionManager.Acquire/Release`, container lifecycle (D-05), per-session lock (D-07), reaper goroutine (D-03), hard cap (D-12 enforce).
- `internal/sandbox/workspace.go` (NEW ~80) — `WorkspaceManager.EnsureDir` + `walkSize` quota via `os.Root` (D-13), cascade integrated into `Conversations.Delete`.
- `internal/sandbox/network.go` (NEW, reshaped from PRD ~80) — host-side forward proxy + allowlist (D-08/D-09/D-10), NOT iptables.
- `internal/scoring/scoring.go` + `scoring_test.go` (NEW ~100+80) — full shared module (D-11), sandbox advisory wiring only (D-12).
- `internal/db/queries/sandbox_sessions.sql` + `migrations/0010_sandbox_sessions.{up,down}.sql` — as PRD-locked.
- `sandbox/sidecar.py` + `compose.yaml` — per-session interpreter + `/session/{id}/exec/{lang}`; compose `network_mode` adjusted for the proxy route.

</code_context>

<specifics>
## Specific Ideas

- **"A sandbox like yours, not a toy" (carried from Phase 5).** The user's isolation-first intent drove the per-conversation-container choice (D-04) over the cheaper shared-sidecar model, and the full-tier session-state choice (D-01) over files-only — Aura's 2b should match the capability of E2B/OpenAI Code Interpreter while keeping the gVisor+`cap_drop:ALL` containment stronger than AIO's.
- **Empirical introspection of Claude Code's own sandbox (user directive 2026-06-03, "look how your sandbox works and we do same for aura" — repeats the Phase-5 move).** Probing the live runtime: a *local* Claude Code install is **unsandboxed** (Bash runs as the host user via Git Bash/MSYS — no container, no seccomp, no proxy env), so the reference is the *cloud/`--sandbox`* runtime captured in `05-CONTEXT.md` + Anthropic's published `anthropic-experimental/sandbox-runtime`. Its model = strong boundary + permissive inside + egress via a **host-resolving proxy allowlist** (`CLAUDE_CODE_PROXY_RESOLVES_HOSTS=true`, default-deny) + ephemeral per-session env + the CVE-2026-39861 symlink-escape lesson. This **validates the four decisions as-chosen** (D-04 boundary, D-08 proxy-not-iptables, D-06 ephemeral session, D-13/D-14 symlink guard) rather than changing them. Deliberate divergence: Aura keeps the gVisor + `cap_drop:ALL` boundary **always on** (vs Claude Code's permissive-on-host local mode) — "a sandbox like yours, stronger."
- **Research-grounded, brainstorm-best (user directive 2026-06-03).** "deep research on D:/tmp + industrial 2026 pattern" — every Area decision is backed by curated-source + web evidence, not assertion. The network-egress pivot (D-08) is the clearest case: the PRD's literal iptables design is technically impossible under the inherited floor, and the evidence for the host-proxy model is unanimous.
- **Build the framework, not the playground** — the user's "full governance framework now" (D-11) reflects [[feedback_aura_as_product]]: ship the reusable `scoring` seam once, properly, rather than three inline copies. The scope guard (D-12) keeps it from ballooning into P10/P11 work.

</specifics>

<deferred>
## Deferred Ideas

- **Memory-snapshot resume across restart** (E2B-style pause/resume of full interpreter memory) — out of scope; Aura 2b is ephemeral-by-default (in-memory state dies with the reaped container; only workspace files + the `sandbox_sessions` row survive). Record only.
- **Firecracker/microVM per-session** — stronger cross-tenant isolation, but rejected (D-05a, KVM-less infra); revisit only if a deployment confirms `/dev/kvm` AND multi-tenant-untrusted becomes the threat model.
- **Per-identity (not per-conversation) sandbox sessions** — v2 (research SUMMARY coupling note). 2b keys strictly by `conversation_id`.
- **Method-level / MITM egress filtering** (Codex `NetworkMode::Limited` GET/HEAD-only) — more than the hostname allowlist needs; skip (D-10).
- **Scheduler/Skills application pipelines** that consume `scoring` (pending_approval persistence, audit columns, skills pending dir) — explicitly P10/P11, NOT Phase 8 (D-12).
- **Swarm parallel-sandbox** (dedicated RISKY-tier container per child instead of shared parent session) — surfaced by D-07; belongs to the Swarm slice (Phase 9).

</deferred>

---

*Phase: 8-Sandbox 2b Session-Bound*
*Context gathered: 2026-06-03*
