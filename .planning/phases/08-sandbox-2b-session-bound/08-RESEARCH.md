# Phase 8: Sandbox 2b Session-Bound - Research

**Researched:** 2026-06-03
**Domain:** Session-bound container orchestration (control plane), persistent stdlib Python interpreter, host-side forward-proxy egress allowlist, openat2 symlink-escape-resistant FS walks (Go 1.26 `os.Root`), shared risk-tier scoring module, Postgres control-plane registry + boot recovery.
**Confidence:** HIGH on mechanics (verified against shipped 2a code + curated Codex/AIO sources + Go 1.26 stdlib); MEDIUM on the egress-bridge interaction (the 2a `aura-sandbox-egressless` non-masquerading bridge + `connect`-denied seccomp is a concrete landmine for 2b — flagged below).

## Summary

This phase extends the shipped, verified 2a stateless runner (`internal/sandbox/`, all read in full) into session-bound execution keyed by `conversation_id`. The CONTEXT (D-01..D-14) locks every WHAT and most HOW; nothing is re-decided here. This research surfaces the implementation mechanics the planner needs and flags four concrete landmines the planner MUST resolve in the plans.

The four discretion items resolve cleanly: (1) the persistent Python interpreter is best done as a **stdlib-only long-lived `exec()`-into-a-namespace-dict REPL server thread per session** (no subprocess-per-call, no IPython) — the simplest design that survives `x=42`, stays stdlib-only, and fits the RAM cap; (2) the host-side forward proxy is a **stdlib `net/http` `CONNECT`-handler that hijacks the conn and tunnels to a resolve-then-pin-validated IP**, reusing the 2a/Slice-5 SSRF classification, with deny-wins glob allowlisting modeled exactly on Codex's `allowed_domains`/`denied_domains` globset; (3) the symlink guard is **`os.Root`/`os.OpenInRoot` (openat2 `RESOLVE_BENEATH`) for `walkSize`, and a manual openat-anchored no-follow recursive delete for the cascade** because `os.Root` has no `RemoveAll` (golang/go#67002 still open) and even `os.RemoveAll` is TOCTOU-susceptible (golang/go#52745); (4) the `SessionManager` is a sync.Map-of-per-session-mutex control plane with a `synctest`-testable, goleak-clean reaper goroutine.

**Primary recommendation:** Build five new files (`sessions.go`, `workspace.go`, `network.go`, `scoring/scoring.go`, `sandbox_sessions.sql`/migration) + extend `docker.go`, `errors.go`, `execute.go`, `sidecar.py`, `compose.yaml`, `config.go`, `conversations/store.go`, `cmd/aura/main.go`. Resolve the four landmines (FK type, migration number, egress-bridge model, sidecar-dir vs workspace-dir co-tenancy) BEFORE writing task actions. PRD-first: five PRD/DECISIONS amendments (per CONTEXT) land before any code commit.

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions (D-01 .. D-14 — verbatim intent, see 08-CONTEXT.md for full text)
- **D-01** — Session sidecar holds a long-lived Python interpreter per `session_id` (in-memory `x=42` survives call→call, prd.md:1252) PLUS the RW workspace mount. Two tiers, not contradictory. Extends 2a's `subprocess.run`-per-call → PRD-amendment item 1.
- **D-02** — Persistence is ASYMMETRIC and must be documented to the model: Python in-memory vars persist; shell `cd`/`export` do NOT persist across calls (only the API-managed working dir is re-applied). Workspace files persist for both langs.
- **D-03** — Idle interpreter is the RAM cost driver; bounded by hard cap 5 + 1800s idle-TTL eviction. Interpreter mechanism = Claude's discretion within "in-memory `x=42` survives + sidecar server stays stdlib-only + fits RAM cap".
- **D-04** — One gVisor `runsc` container per `conversation_id`, lifecycle owned by `SessionManager` (`internal/sandbox/sessions.go`).
- **D-05** — Contained carve-out: SessionManager DOES shell `docker run`/`stop`/`rm` for LIFECYCLE; EXECUTION stays HTTP. MUST NEVER mount the docker socket. → PRD-amendment item 2.
- **D-06** — `aura.sandbox_sessions` is the control-plane registry. Boot recovery: `active` rows → `terminated`, container recreated lazily on next call (parity with `agent_job_runs`). Workspace files survive Aura restart; in-memory interpreter state does NOT.
- **D-07** — Container lock per `session_id` (sync.Map + mutex) serializes concurrent `execute` within a session. Swarm session-reuse serializes on this lock (amendment #34-B).
- **D-08** — Egress enforced OUTSIDE the sandbox by a Go host-side forward proxy (`internal/sandbox/network.go` reshaped). Default-deny; allowlist from `AURA_SANDBOX_NETWORK_ALLOW_HOSTS` (CSV), per-session keyed by `conversation_id`. → PRD-amendment item 3 (replaces iptables).
- **D-09** — Reuse Slice 5 `internal/web` SSRF machinery verbatim: `safeDialContext` IP-classification + DNS-rebinding pin + redirect re-validation. Deny-wins precedence, glob domains (`pypi.org`, `*.pythonhosted.org`). Sidecar gets `HTTP_PROXY`/`HTTPS_PROXY`/`PIP_PROXY`/`NPM_CONFIG_PROXY` env.
- **D-10** — Hostname/SNI granularity only; NO MITM. Validate at CONNECT; guard DNS-rebinding; watch request-smuggling. Honor `AURA_PRIVACY_MODE=local-only`: a non-empty allowlist under local-only must fail-fast or be inert. Caveat → 08-SECURITY.
- **D-11** — Phase 8 ships the FULL shared `internal/scoring/` module (`scoring.go` + exhaustive `scoring_test.go`, no DB) — pulled forward from Slice 6. → PRD-amendment item 5.
- **D-12** — SCOPE GUARD: build the MODULE, not the per-slice application pipelines. Phase 8 wires ONLY the sandbox advisory path: `ComputeSandboxTier(args)` → `network_allow` empty=SAFE, `pypi.org`-only=SAFE-bump, arbitrary domains=RISKY/IRREVERSIBLE → tool result carries `{risk_tier, gate_recommended}` (advisory, no pending-state persistence for sandbox in v1).
- **D-13** — Host-side workspace walkers use `os.Root`/`os.OpenInRoot` (`workspace.go` `walkSize` + `Conversations.Delete` cascade at `store.go:430`). → minor PRD-amendment item 4 (`O_NOFOLLOW` → `os.Root`).
- **D-14** — Cascade delete is a MANUAL no-follow openat walk, NOT `os.RemoveAll` (`os.Root` has no `RemoveAll` yet — golang/go#67002). Acceptance test (ROADMAP #2) gates this.

### Claude's Discretion
- Persistent-interpreter mechanism (D-03): REPL-over-stdin vs IPython kernel — invariant "`x=42` survives + sidecar server stays stdlib-only + fits RAM cap". → **Recommendation in this doc: stdlib in-process `exec()` namespace-dict REPL, NOT a subprocess, NOT IPython.**
- Per-session port allocation / container naming scheme (D-04/D-05).
- Proxy shape (D-08): stdlib `net/http` `CONNECT` handler vs small SOCKS5 — invariant "default-deny hostname allowlist + resolve-then-pin + reuses Slice 5 IP classification". → **Recommendation: `CONNECT` handler.**
- DNS resolution cache TTL (PRD proposes 5 min; validate against `pypi.org` rotating A-records — OQ6).
- `aura sandbox sessions` subcommand output formatting.

### Deferred Ideas (OUT OF SCOPE)
- Memory-snapshot resume across restart (E2B-style pause/resume of full interpreter memory) — record only.
- Firecracker/microVM per-session (D-05a, KVM-less infra).
- Per-identity (not per-conversation) sandbox sessions — v2.
- Method-level / MITM egress filtering (Codex `NetworkMode::Limited`) — skip (D-10).
- Scheduler/Skills application pipelines that consume `scoring` (pending_approval persistence, audit columns, skills pending dir) — P10/P11, NOT Phase 8 (D-12).
- Swarm parallel-sandbox (dedicated RISKY-tier container per child) — Phase 9.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| CAP-02 | Sandbox 2b session-bound con workspace per-conversation + network allowlist via iptables, TTL configurable, symlink escape guard su host walkers. | This phase. NOTE: "via iptables" is superseded by D-08 host-side forward proxy (PRD-amendment item 3). "symlink escape guard su host walkers" → D-13/D-14 `os.Root` + manual no-follow walk. All four ROADMAP success criteria mapped in Validation Architecture below. |
</phase_requirements>

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Session lifecycle (create/reuse/reap container) | Aura host process — `SessionManager` control plane (`sessions.go`) | Postgres `sandbox_sessions` (durable registry) | D-04/D-06: control plane owns lifecycle; DB is the recovery state, not the driver. NEVER the sidecar. |
| Persistent Python interpreter state (`x=42`) | Sidecar container — long-lived in-process interpreter per session | — | D-01/D-03: state lives where the process lives; dies on container reap. |
| Code execution | Sidecar container (HTTP `/session/{id}/exec/{lang}`) | — | D-05: Go drives lifecycle via `docker` CLI but NEVER execution — execution is HTTP-only (inherited 2a invariant, narrowed). |
| Workspace file persistence | Host filesystem (`$AURA_RUN_DIR/conversations/<id>/workspace/`) bind-mounted RW into container | — | D-06: host dir survives Aura restart; container is ephemeral. |
| Egress allowlist enforcement | Aura host process — forward proxy (`network.go`) | Slice 5 `internal/web` SSRF guard (IP classification) | D-08/D-09: enforced OUTSIDE the sandbox; untrusted code never owns its own firewall. |
| Symlink-escape-safe FS walk (quota + cascade) | Aura host process — `os.Root` openat2 walks | — | D-13/D-14: the host walker is the unsandboxed actor that must refuse attacker-planted symlinks. |
| Risk-tier computation | Pure `internal/scoring/` module (no DB, no IO) | — | D-11/D-12: self-contained pure functions; consumers wire it. |

## Standard Stack

### Core (all stdlib — zero new dependencies, matching 2a's "no new module" discipline)
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| Go stdlib `os` (`os.Root`, `os.OpenInRoot`, `os.OpenRoot`) | go1.26.4 (verified `go.mod`) | openat2 `RESOLVE_BENEATH` traversal-resistant FS walks (D-13) | The canonical Go answer to CVE-2026-39861-class symlink escapes; supersedes `O_NOFOLLOW`. [CITED: go.dev/blog/osroot] |
| Go stdlib `net/http` (`http.Hijacker`, `http.Server`) | go1.26.4 | Host-side `CONNECT` forward proxy (D-08) | Already the 2a/Slice-5 HTTP idiom; `Hijacker` is the standard CONNECT-tunnel primitive. [CITED: pkg.go.dev/net/http] |
| Go stdlib `net`/`net/netip` | go1.26.4 | resolve-then-pin + IP classification reuse (D-09) | Slice 5 `internal/web/ssrf.go` `classify()` already implements the full SSRF class table. [VERIFIED: internal/web/ssrf.go read in full] |
| Go stdlib `sync` (`sync.Map`, `sync.Mutex`) | go1.26.4 | Per-session lock control plane (D-07) | PRD-locked shape; standard Go concurrency. |
| Go stdlib `testing/synctest` | go1.26.4 (GA since 1.25) | Deterministic TTL-reap test (PRD line 1720) | The 2026 stdlib answer to time-based goroutine tests; no fake-clock dependency. [ASSUMED — verify `synctest` GA-stable in 1.26 at plan time via `go doc testing/synctest`] |
| Python stdlib (`code.InteractiveInterpreter` or bare `exec()` + namespace dict, `http.server`, `threading`) | Python 3.12 (sidecar image) | Long-lived per-session interpreter (D-01/D-03) | Sidecar MUST stay stdlib-only (PRD D-20a + D-03 invariant). `exec(code, ns)` against a persisted `dict` is the minimal `x=42`-survives design. [VERIFIED: sandbox/sidecar.py is stdlib-only `http.server`] |

### Supporting (test-only, already in repo)
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `go.uber.org/goleak` | existing test dep (2a uses it) | reaper goroutine leak assertion (D-03) | `goleak.VerifyNone` in `sessions_test.go` TestMain. [VERIFIED: 2a docker.go comment + CLAUDE.md amendment #15] |
| sqlc + pgx/v5 | existing | `sandbox_sessions` generated queries | `internal/db/queries/sandbox_sessions.sql` (4 queries). [VERIFIED: internal/db/queries/conversations.sql pattern] |
| golang-migrate | existing | migration up/down | next free number is **0008**, NOT 0010 (landmine below). |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| stdlib in-process `exec()` interpreter | IPython/Jupyter kernel (ipykernel + ZMQ) | Violates "sidecar stays stdlib-only" (D-03); adds heavyweight deps + ZMQ ports + RAM. REJECTED. |
| stdlib in-process `exec()` interpreter | `python -i` REPL-over-stdin subprocess kept alive | Works for `x=42` but reintroduces stdin/stdout framing fragility, prompt-detection heuristics, and a separate long-lived child process per session. The in-process `exec()`-into-dict approach is simpler and avoids stdin parsing. PREFERRED. |
| stdlib `net/http` CONNECT proxy | SOCKS5 proxy | SOCKS5 needs the sidecar to speak SOCKS (pip/curl support it via `ALL_PROXY=socks5h://`), but `HTTP_PROXY`/`HTTPS_PROXY` CONNECT is universally honored by pip/urllib/curl/npm and is what Claude Code + Codex ship. CONNECT PREFERRED (D-10 hostname-only fits CONNECT's `Host:port` exactly). |
| Per-session `docker run --rm` | Reuse the single compose `aura-sandbox` service | The compose service is the 2a stateless singleton; 2b needs N independent containers keyed by conv → SessionManager `docker run` per session (D-05). |

**Installation:** No new Go modules. No new Python packages (sidecar stays stdlib). Sidecar image may add nothing for the interpreter; pip-install-at-runtime (the network test, criterion 4) uses the BAKED pip + the new egress allowlist (2a already bakes pip per D-20, it is just net-blocked at runtime in 2a).

**Version verification:** `go1.26.4` confirmed in `go.mod` and `go version`. `os.Root`/`os.OpenInRoot` confirmed available (introduced 1.24, openat2-backed). `testing/synctest` confirmed GA in 1.25+; re-verify exact 1.26 stability at plan time.

## Package Legitimacy Audit

> No external packages are installed by this phase. All Go code is stdlib + existing repo deps (pgx, sqlc-generated, goleak). The sidecar stays Python-stdlib-only. **slopcheck N/A — zero new packages.**

| Package | Registry | Disposition |
|---------|----------|-------------|
| (none) | — | No new dependencies — phase uses Go stdlib + already-vendored modules only. |

**Packages removed due to slopcheck [SLOP] verdict:** none (no installs).
**Packages flagged as suspicious [SUS]:** none.

## Architecture Patterns

### System Architecture Diagram

```
                         ┌─────────────────────────────────────────────┐
  aura chat / aura exec  │              Aura host process                │
  --session <conv_id> ──▶│                                               │
                         │  execute tool (tools/execute.go)              │
                         │     │ session_id (= conversation_id via       │
                         │     │   WithToolCallContext, already wired)    │
                         │     ▼                                          │
                         │  SessionManager.Acquire(conv_id) ──────┐      │
                         │   (sessions.go: sync.Map[conv]→*session)│      │
                         │     │  per-session mutex (D-07)         │      │
                         │     │  hard cap 5 → ErrSessionCapReached│      │
                         │     ▼                                   │      │
                         │  ┌── lifecycle (shell `docker`, D-05) ──┘      │
                         │  │   docker run/stop/rm  (NEVER socket)        │
                         │  │   record in aura.sandbox_sessions (PG)      │
                         │  ▼                                             │
   ┌─────────────────────┼──────────────┐    ┌──────────────────────────┤
   │ per-conv container   │ HTTP exec    │    │ forward proxy (network.go)│
   │ (gVisor runsc)       │ POST /session│    │  CONNECT handler          │
   │  sidecar.py          │ /{id}/exec/  │    │  deny-wins glob allowlist │
   │   persistent interp  │◀────{lang}───┘    │  resolve-then-pin (Slice5)│
   │   ns dict {x:42}     │                   │  AURA_SANDBOX_NETWORK_    │
   │   /workspace (RW) ───┼──┐                │   ALLOW_HOSTS per conv    │
   │   HTTP_PROXY=──────┐ │  │ pip→pypi.org   │◀──CONNECT pypi.org:443────┤
   └────────────────────┼─┼──┼───────────────┘    └─▶ pinned public IP    │
                        │ │  │                          (SSRF-classified)  │
   ┌────────────────────┼─┼──┼──────────────────────────────────────────┐│
   │ host filesystem     ▼ │  ▼                                          ││
   │  $AURA_RUN_DIR/conversations/<conv_id>/                             ││
   │     ├── <tool_call_id>.result   ← tools.NewResult spillover (2a)    ││
   │     └── workspace/              ← bind-mounted /workspace (2b, RW)  ││
   │   walkSize quota + cascade delete via os.Root (D-13/D-14)           ││
   └────────────────────────────────────────────────────────────────────┘│
            │ idle-TTL reaper goroutine (60s sweep, synctest-testable)     │
            │ last_used_at < now()-TTL → docker stop/rm + MarkTerminated   │
            └──────────────────────────────────────────────────────────────
```

The reader traces criterion 1 (persistence): `execute --session conv` → Acquire (reuse) → HTTP exec → `exec("x=42", ns)` → second call reuses same container → `exec("print(x)", ns)` reads `ns["x"]`. Criterion 4 (network): pip CONNECT → forward proxy → glob match `pypi.org` → resolve-then-pin → tunnel; `curl 1.1.1.1` → no glob match → deny.

### Recommended Project Structure
```
internal/sandbox/
├── sandbox.go        # Runner iface + Result (extend, do not break stateless path)
├── docker.go         # 2a HTTP client + auto-start (extend: per-session exec path)
├── errors.go         # add ErrSessionCapReached, ErrWorkspaceQuotaExceeded
├── sessions.go       # NEW ~150 — SessionManager: Acquire/Release, lifecycle, lock, reaper, cap
├── workspace.go      # NEW ~80  — WorkspaceManager: EnsureDir, walkSize (os.Root), cascade walk
├── network.go        # NEW ~80  — forward proxy: CONNECT handler, glob allowlist, resolve-then-pin
└── *_test.go         # unit + //go:build sandbox_integration tiers
internal/scoring/
├── scoring.go        # NEW ~100 — RiskTier, ComputeTaskTier/Skill/Sandbox, GateRecommended, RequiresImmediateAlert
└── scoring_test.go   # NEW ~80  — exhaustive table tests + property-based (modifier monotonicity)
internal/db/
├── queries/sandbox_sessions.sql           # NEW ~30 — 4 sqlc queries
├── migrations/0008_sandbox_sessions.up.sql  # NEW (NOT 0010 — landmine)
└── migrations/0008_sandbox_sessions.down.sql
sandbox/sidecar.py    # extend: per-session interpreter + /session/{id}/exec/{lang}
compose.yaml          # adjust network model for the proxy route (landmine #3)
cmd/aura/main.go      # aura exec --session + aura sandbox sessions {list|terminate|prune}
```

### Pattern 1: Per-session control plane with sync.Map + per-key mutex (D-04/D-07)
**What:** `SessionManager` holds `sync.Map[conv_id]*session` where each `*session` carries its own `sync.Mutex`, container id, port, last-used time. `Acquire` loads-or-creates the session entry, then locks its mutex (serializing intra-session execs, D-07). The hard cap is checked under a manager-level lock before creating a new entry.
**When to use:** Every `execute` call with a session.
**Example:**
```go
// Source: pattern from Go stdlib sync.Map + per-key mutex (standard control-plane idiom)
type session struct {
    mu          sync.Mutex // D-07: serialize execs within this session
    containerID string
    port        int
    lastUsed    atomic.Int64 // unix nanos; reaper reads without holding mu
}
type SessionManager struct {
    sessions sync.Map // conv_id → *session
    capMu    sync.Mutex
    count    int      // live session count, guarded by capMu (D-12 hard cap 5)
    maxN     int
    ttl      time.Duration
    now      func() time.Time // injectable for synctest determinism
}
```
Reaper goroutine: `time.Ticker(60s)` (or `synctest`-driven), iterate `sessions.Range`, if `now-lastUsed > ttl` → `docker stop/rm` + `MarkTerminated` + delete from map + decrement count. goleak-clean: the reaper exits on a `context.Context` cancel passed to `NewSessionManager`/`Close`.

### Pattern 2: Persistent stdlib Python interpreter (D-01/D-02/D-03)
**What:** The sidecar keeps one `dict` namespace per `session_id`. `/session/{id}/exec/python` runs `exec(compile(code, "<session>", "exec"), ns)` where `ns` is the persisted dict — so `x=42` set in call 1 lives in `ns["x"]` for call 2. stdout/stderr are captured by temporarily redirecting `sys.stdout`/`sys.stderr` to `io.StringIO` around the `exec` (since we are no longer in a subprocess). Shell (`/session/{id}/exec/shell`) stays `subprocess.run` per call BUT re-applies a per-session API-managed cwd (D-02 asymmetric: shell `cd`/`export` do NOT persist; only the tracked cwd is reapplied as `subprocess.run(cwd=session_cwd)`).
**When to use:** Session-bound python/shell exec.
**Critical detail (D-02 contract):** Document to the model in the `execute` tool description that python vars persist but shell env does not.
**Concurrency:** The Go per-session lock (D-07) already serializes execs, so the sidecar's per-session namespace dict needs no internal locking for the single-Aura case; the sidecar SHOULD still guard the namespace map with a `threading.Lock` (ThreadingHTTPServer) for defense against a second concurrent Aura process (PRD criterion 1 names "2 process Aura concurrent → serializzati via container lock" — but the container lock is Aura-local, so two Aura processes can race; the sidecar lock closes that).
**RAM bound:** Each idle interpreter holds its namespace dict + imported modules resident. The hard cap of 5 + 1800s TTL is the bound. Keep the namespace dict per session, evict the whole container (not just the dict) on reap.

### Pattern 3: Host-side CONNECT forward proxy with deny-wins glob allowlist (D-08/D-09/D-10)
**What:** A small `http.Server` on a loopback host port (reachable from the container via the bridge gateway). On `CONNECT host:port`: (1) split host, (2) deny-wins glob match against the per-session policy (`denied` globs beat `allowed` globs — modeled on Codex), (3) if allowed, resolve-then-pin via the Slice-5 classification (reject if any resolved IP is private/metadata/loopback), (4) `Hijack()` the client conn, dial the pinned IP, `io.Copy` bidirectionally. No MITM (D-10): the proxy never decrypts TLS — it only validates the CONNECT target hostname.
**When to use:** Every container egress when `AURA_SANDBOX_NETWORK_ALLOW_HOSTS` is non-empty.
**Example (deny-wins + glob, modeled on Codex network_policy.rs):**
```go
// Source: D:/tmp/codex/codex-rs/network-proxy/src/{network_policy.rs,state.rs} —
// deny-wins precedence + globset allow/deny; baseline-deny for not-allowed.
func (p *policy) allow(host string) bool {
    if p.denySet.Match(host) { return false } // deny ALWAYS wins (D-09)
    return p.allowSet.Match(host)             // baseline deny if not allowed
}
// glob: "*.pythonhosted.org" matches files.pythonhosted.org but NOT pythonhosted.org's parent.
// Codex rejects a GLOBAL wildcard ("*") in the deny list — replicate that validation.
```
**Env wiring (D-09):** The sidecar container gets `HTTP_PROXY`/`HTTPS_PROXY`/`http_proxy`/`https_proxy` = `http://<bridge-gateway>:<proxy-port>`, plus `PIP_PROXY`/`NPM_CONFIG_PROXY` for tool-specific honoring. pip honors `HTTPS_PROXY` for the CONNECT to pypi.org:443; urllib honors `https_proxy`. `NO_PROXY` should exclude nothing (we want ALL egress proxied).
**`AURA_PRIVACY_MODE=local-only` (D-10):** If set AND `AURA_SANDBOX_NETWORK_ALLOW_HOSTS` non-empty → fail-fast at session-create OR render the allowlist inert (deny all). Pick fail-fast for an explicit operator error; document in 08-SECURITY.

### Pattern 4: openat2 traversal-resistant walks (D-13/D-14)
**What:** `walkSize` (quota): open the workspace root with `os.OpenRoot(workspacePath)` → a `*os.Root` whose every `Open`/`Stat`/`ReadDir` is confined beneath the root (openat2 `RESOLVE_BENEATH`, rejects `..` and escaping symlinks). Recurse using `root.Open(name)` + `Lstat` to size regular files only (never follow symlinks). Cascade delete: because `os.Root` has **no `RemoveAll`** (golang/go#67002 open) AND `os.RemoveAll` is itself TOCTOU-symlink-susceptible (golang/go#52745), implement a manual post-order walk: open each dir via the `*os.Root` (no-follow), `Lstat` entries, `root.Remove(name)` files+symlinks (removes the link, never the target), recurse into real subdirs, then remove the now-empty dir.
**When to use:** `WorkspaceManager.walkSize` quota check + `Conversations.Delete` cascade.
**Acceptance gate (criterion 2):** `ln -s /etc /workspace/escape` then host cascade → the walker sees `escape` as a symlink (`Lstat` → `ModeSymlink`), `root.Remove("escape")` unlinks the symlink itself, host `/etc` untouched.

### Anti-Patterns to Avoid
- **Mounting the docker socket** into Aura or the sidecar (D-05, re-introduces SandboxEscapeBench escape vector #1 — keep config-regression 0). Lifecycle uses the `docker` CLI shelled out, gated on `exec.LookPath("docker")` (2a `autoStart` precedent).
- **In-container iptables/nftables** (D-08 — needs `CAP_NET_ADMIN`, contradicts `cap_drop: ALL`; under gVisor only touches the virtual netstack). Egress is host-side proxy ONLY.
- **`os.RemoveAll` on the workspace** (D-14 — TOCTOU symlink race; the whole point of the cascade is attacker-controlled trees).
- **TLS MITM in the proxy** (D-10 — CA injection complexity not needed for hostname-only allowlisting).
- **Cherry-picking a public IP from a mixed DNS result** (Slice 5 `validateAndPin` fails closed on ANY blocked record — reuse that exact behavior).
- **Letting the reaper leak** (goleak — bind it to a cancelable ctx; `Close()` waits via a done channel).

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Symlink-escape-safe file walk | Manual `O_NOFOLLOW` + `..` string checks | `os.Root`/`os.OpenInRoot` (openat2 `RESOLVE_BENEATH`) | Atomic kernel-level confinement; closes the TOCTOU window `O_NOFOLLOW` leaves open. [CITED: go.dev/blog/osroot] |
| SSRF / DNS-rebinding IP classification | New IP-class table in `network.go` | `internal/web` `classify()` + `dnsPin` (Slice 5) | Already implements the full class table (loopback/link-local/private/multicast/cgnat/this-network/metadata-v6, IPv4-mapped unmap). [VERIFIED: internal/web/ssrf.go] — BUT it is unexported (landmine #5). |
| Deterministic time-based reaper test | `time.Sleep` + flaky polling | `testing/synctest` (fake clock, virtual time) | PRD line 1720 mandates synctest; eliminates flake + speeds CI. |
| Glob domain matching | Hand-rolled `strings.HasSuffix` wildcard | A globset (Codex uses `globset`) OR a careful suffix-match helper | Edge cases: `*.x.org` must NOT match `x.org`; global `*` in deny list is a footgun Codex explicitly rejects. |
| Persistent interpreter | IPython kernel / Jupyter | stdlib `exec()` into a per-session namespace dict | Keeps sidecar stdlib-only (D-03 invariant); no ZMQ, no extra RAM, no deps. |
| Boot recovery of orphaned sessions | Ad-hoc scan | Mirror `agent_job_runs` parity: `ListActive` → mark `terminated` at boot, lazy recreate | PRD-locked pattern; consistency with the rest of the codebase. |

**Key insight:** Every "hard" part of 2b already has a battle-tested answer in either the Go 1.26 stdlib (`os.Root`, `synctest`) or the shipped Slice-5 SSRF code. The phase's risk is in WIRING (the four landmines), not in novel algorithms.

## Runtime State Inventory

> Phase 8 is greenfield-extending (new tables/files), not a rename. This section is included because the phase introduces durable runtime state that boot recovery and cleanup must reconcile.

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | `aura.sandbox_sessions` rows (NEW). Workspace files at `$AURA_RUN_DIR/conversations/<id>/workspace/`. | Migration 0008 creates the table; boot recovery marks orphaned `active` rows `terminated`. Workspace files persist across restart (D-06). |
| Live service config | Per-session gVisor containers (created via `docker run`, named e.g. `aura-sandbox-sess-<conv_id>`). NOT in compose/git — created at runtime by SessionManager. | Boot recovery: containers from a prior Aura process are gone (or orphaned if Aura crashed mid-run). `ListActive` rows → `terminated`; a `docker ps`-based orphan sweep of `aura-sandbox-sess-*` containers SHOULD run at boot to `docker rm` strays (defense-in-depth; PRD says lazy-recreate so existing strays must be cleaned). |
| OS-registered state | None — no Task Scheduler / systemd / launchd registration. Verified: phase adds no OS-level scheduling. | None. |
| Secrets/env vars | New `AURA_SANDBOX_SESSION_TTL_SEC` (1800), `AURA_SANDBOX_MAX_CONCURRENT_SESSIONS` (5), `AURA_SANDBOX_WORKSPACE_MAX_BYTES` (104857600), `AURA_SANDBOX_NETWORK_ALLOW_HOSTS` (empty), `AURA_RISK_ALERT_THRESHOLD` (risky). All follow `envIntDefault`/`envDefault`. Interacts with existing `AURA_PRIVACY_MODE` (D-10). | Add to `config.go` `loadBase()` + the PRD env index. No secret VALUES — config knobs only. |
| Build artifacts | Sidecar image rebuild (sidecar.py gains session endpoints). | `docker compose build aura-sandbox` after sidecar.py change; the per-session containers `docker run` the SAME image. |

**Boot-recovery orphan landmine:** The 2a workspace dir is co-tenant with the tool-result spillover dir (both under `<run_dir>/conversations/<id>/`). The existing `conversations/orphan_scan.go` reconciles spillover dirs; 2b's workspace subdir is INSIDE that, so the existing orphan scan + the new `Conversations.Delete` cascade (D-13/D-14) must agree on ownership. See landmine #4.

## Common Pitfalls

### Pitfall 1: The 2a egress bridge blocks the proxy route (LANDMINE #3 — HIGHEST RISK)
**What goes wrong:** 2a shipped egress containment NOT as `network_mode: none` but as (a) `connect(2)` REMOVED from the seccomp allowlist + (b) a non-masquerading bridge `aura-sandbox-egressless` (`compose.yaml:124-127,166-169`, AR-05-01). For 2b's forward proxy to work, the sidecar's pip/urllib MUST be able to `connect(2)` to the host proxy. That directly conflicts with the 2a `connect`-denied seccomp profile.
**Why it happens:** The 2a security audit (AR-05-01) chose connect-denial as the egress control; 2b's proxy model REQUIRES connect to the proxy.
**How to avoid:** 2b session containers need a DIFFERENT seccomp profile (or a session-mode profile variant) that ALLOWS `connect(2)` — egress containment shifts entirely to the host proxy (D-08). The session container must join a bridge that CAN route to the host proxy port (the egressless non-masquerading bridge reaches the host gateway for loopback-published ports, but verify the proxy is reachable at the bridge gateway IP, not `127.0.0.1` which is container-local). When `AURA_SANDBOX_NETWORK_ALLOW_HOSTS` is empty, the session container keeps the 2a egressless posture (no proxy env, connect still effectively dead-ends). **The planner MUST add an explicit task: design the session-container network+seccomp posture and document the deviation from 2a in 08-SECURITY (extends AR-05-01).**
**Warning signs:** A passing unit test but `pip install` hangs/refuses in the live integration test (criterion 4) because connect is seccomp-denied or the proxy is unreachable at the address the container uses.

### Pitfall 2: FK type mismatch — `conversation_id text` vs `conversations.id uuid` (LANDMINE #1)
**What goes wrong:** PRD migration 0010 spec literally says `conversation_id text NOT NULL REFERENCES aura.conversations(id)`. But `aura.conversations.id` is `uuid` (verified `0005_conversations.up.sql:9`). A `text` FK to a `uuid` PK fails (`foreign key constraint ... are of incompatible types: text and uuid`).
**Why it happens:** PRD authored before the conversations schema locked id as uuid; `paused_states` had the same text→uuid promotion (0005:52-53).
**How to avoid:** Migration 0008 must declare `conversation_id uuid NOT NULL REFERENCES aura.conversations(id) ON DELETE CASCADE`. The Go/sqlc side carries it as `uuid.UUID`/`pgtype.UUID` (conversations store pattern). Note: the `execute` tool's `session_id` arrives as a STRING (the conversation id string) — parse to uuid at the SessionManager boundary, exactly like `conversations.Store.sidecarDir`/`parseUUID`.
**Warning signs:** `aura db migrate` fails on 0008; or sqlc generates a `string` field that won't bind.

### Pitfall 3: Migration number 0010 does not exist — next free is 0008 (LANDMINE #2)
**What goes wrong:** PRD + CONTEXT both say "migration 0010". The repo only has migrations through **0007** (`0007_cache_metrics`). golang-migrate requires contiguous-ish ascending numbers; naming the file 0010 leaves gaps 0008/0009 that future slices expect.
**Why it happens:** PRD pre-assigned 0010 assuming Slices 6 (scheduler) + others would land first; the actual roadmap order put Phase 8 earlier.
**How to avoid:** Name it **0008_sandbox_sessions**. Cross-check `prd.md §Persistence "Migration numbering — fonte di verità"` (CLAUDE.md cites it) and amend the PRD's "0010" reference. The planner MUST verify the next free number at plan time (`ls internal/db/migrations/`).
**Warning signs:** A 0010 file with no 0008/0009 — migrate may still run but the numbering contract is broken and the next slice collides.

### Pitfall 4: Workspace dir co-tenancy with tool-result spillover (LANDMINE #4)
**What goes wrong:** `tools.NewResult` writes spillover to `<run_dir>/conversations/<session_id>/<tool_call_id>.result` (verified `result.go:70`). 2b's workspace is `<run_dir>/conversations/<id>/workspace/`. They share the parent. The cascade delete (D-14) must remove BOTH the `.result` files (already handled by `Conversations.Delete` `os.RemoveAll`, `store.go:449`) AND the workspace — but `os.RemoveAll` is exactly what D-14 forbids for the attacker-controlled `workspace/` subtree.
**Why it happens:** Two subsystems (tool-result spillover, sandbox workspace) write under the same per-conversation dir.
**How to avoid:** Replace `store.go:449` `os.RemoveAll(dir)` with: (1) a `WorkspaceManager`-provided os.Root no-follow cascade for the `workspace/` subtree (attacker-controlled), then (2) ordinary removal of the remaining `.result` files + the parent dir (Aura-controlled, not attacker-writable, so plain removal is acceptable — but using the os.Root walk for the whole `<id>/` dir is cleanest). Decide ownership: the cleanest design is `WorkspaceManager.PurgeConversationDir(conv_id)` doing the full os.Root walk of `<id>/`, called from `Conversations.Delete`. This is a cross-package edit (`conversations` → `sandbox`) — watch the import direction (sandbox should not import conversations; conversations may import sandbox, OR define a small cleanup interface in conversations that main.go wires to a sandbox impl).
**Warning signs:** Cascade test (criterion 2) deletes `/etc`, or `.result` files orphaned after delete.

### Pitfall 5: Slice-5 SSRF guard is unexported (LANDMINE #6, lower risk)
**What goes wrong:** D-09 says "reuse `internal/web` `safeDialContext`/`classify` verbatim", but `classify`, `guard`, `validateAndPin`, `dnsPin` are all UNEXPORTED in `internal/web` (verified ssrf.go/dnspin.go/transport.go). `internal/sandbox` cannot call them.
**Why it happens:** Slice 5 encapsulated the SSRF guard as package-private.
**How to avoid:** Two options — (a) export a minimal reusable surface from `internal/web` (e.g. `web.ClassifyIP(netip.Addr) (string,bool)` + a `web.NewDialGuard(...)` the proxy uses), OR (b) extract the IP-classification + DNS-pin into a shared `internal/netguard` package both `web` and `sandbox` import. Option (b) is cleaner (no `sandbox`→`web` coupling) but is a larger refactor touching shipped Slice-5 code (deep-refactor-on-touch applies, CLAUDE.md). Option (a) is smaller. The planner should pick (a) for scope control unless the refactor is trivial. **Either way this is NOT "reuse verbatim" — it requires an export/extract task.** Re-test the Slice-5 web tier after any export to confirm no regression.
**Warning signs:** A copy-pasted `classify` in `network.go` (duplication — CLAUDE.md NO-DUPLICATE; the audit will flag it).

### Pitfall 6: gVisor `runsc` per-session container parity
**What goes wrong:** 2a runs `runsc` via a compose OVERLAY (`compose.gvisor.yaml`, applied through `make sandbox-up`). 2b's `docker run` (SessionManager, D-05) must pass `--runtime=runsc` on x86 explicitly — there is no compose overlay for an ad-hoc `docker run`.
**How to avoid:** SessionManager reads `cfg.SandboxRuntime` (already resolved per-arch, `config.go:239`) and passes `--runtime=<runtime>` to `docker run`, plus ALL the 2a hardening flags (`--user 65532:65532 --read-only --tmpfs /tmp --cap-drop ALL --security-opt no-new-privileges --security-opt seccomp=<session-profile> --pids-limit 64 --memory 512m --cpus 1.0 --ulimit nofile=64`) — the compose service's hardening must be replicated in the `docker run` argv because it is no longer compose-managed.
**Warning signs:** Session containers run under `runc` (weaker boundary) or without seccomp because the flags were assumed inherited from compose.

### Pitfall 7: DNS A-record rotation for pypi.org (OQ6)
**What goes wrong:** PRD proposes a 5-min DNS cache; `pypi.org`/`files.pythonhosted.org` rotate A-records (Fastly CDN). Pinning one IP for the whole CONNECT session is correct for anti-rebinding, but caching across CONNECTs for 5 min could pin a now-dead edge.
**How to avoid:** The Slice-5 `dnsPin` already has a config TTL (`AURA_WEB_DNS_PIN_TTL_SEC`, default 60s) and re-resolves+re-classifies on expiry — reuse its semantics. Per-CONNECT resolution (pin only for the life of one tunnel) is simplest and avoids the dead-edge problem. Validate with a live `pip install` against pypi in the integration test (criterion 4).

## Code Examples

### sandbox_sessions migration (0008) — corrected FK type
```sql
-- Source: PRD §Slice 2b file targets + 0005_conversations.up.sql (FK is uuid, not text)
CREATE TABLE aura.sandbox_sessions (
    id              uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id uuid        NOT NULL REFERENCES aura.conversations(id) ON DELETE CASCADE,
    container_id    text        NOT NULL,
    image_digest    text        NOT NULL,
    started_at      timestamptz NOT NULL DEFAULT now(),
    last_used_at    timestamptz NOT NULL DEFAULT now(),
    status          text        NOT NULL DEFAULT 'active'
                    CHECK (status IN ('active','idle','terminated','evicted'))
);
CREATE INDEX sandbox_sessions_status_last_used_idx
    ON aura.sandbox_sessions (status, last_used_at);   -- reaper + boot recovery
GRANT SELECT, INSERT, UPDATE, DELETE ON aura.sandbox_sessions TO aura_app;
GRANT ALL                            ON aura.sandbox_sessions TO aura_migrate;
```

### sqlc queries (4, parity with conversations.sql naming)
```sql
-- name: InsertSession :one
INSERT INTO aura.sandbox_sessions (conversation_id, container_id, image_digest)
VALUES ($1, $2, $3) RETURNING id, conversation_id, container_id, image_digest, started_at, last_used_at, status;

-- name: TouchLastUsed :exec
UPDATE aura.sandbox_sessions SET last_used_at = now() WHERE id = $1;

-- name: MarkTerminated :exec
UPDATE aura.sandbox_sessions SET status = 'terminated' WHERE id = $1;

-- name: ListActive :many
SELECT id, conversation_id, container_id, image_digest, started_at, last_used_at, status
FROM aura.sandbox_sessions WHERE status = 'active' ORDER BY last_used_at;
```

### scoring module sandbox advisory path (D-12 scope guard)
```go
// Source: prd.md §Risk-Based Governance ~4534-4567 (full module) + D-12 (sandbox-only wiring)
package scoring

type RiskTier string
const ( Safe RiskTier="safe"; Normal RiskTier="normal"; Risky RiskTier="risky"; Destructive RiskTier="destructive" )

// SandboxArgs is the sandbox-tier input (D-12: ONLY this path is WIRED in Phase 8).
type SandboxArgs struct{ NetworkAllow []string } // from AURA_SANDBOX_NETWORK_ALLOW_HOSTS / tool arg
func ComputeSandboxTier(a SandboxArgs) RiskTier {
    if len(a.NetworkAllow) == 0 { return Safe }            // empty = no egress = SAFE (D-12)
    if onlyPyPI(a.NetworkAllow) { return Safe }            // pypi.org-only = legit install = SAFE-bump→stays Safe (D-12)
    return Risky                                           // arbitrary domains = RISKY (D-12; IRREVERSIBLE if broader)
}
func GateRecommended(t RiskTier) bool { return t == Risky || t == Destructive }
// ComputeTaskTier / ComputeSkillTier / RequiresImmediateAlert: BUILT + UNIT-TESTED now (D-11),
// NO runtime consumers in Phase 8 (D-12 — Scheduler P10 / Skills P11 wire them later).
```
Tool-result wiring (advisory only, D-12): the `execute` result for a session call with a non-empty allowlist appends `{risk_tier, gate_recommended}` to the lean preview; NO pending-state persistence for sandbox in v1.

### os.Root no-follow cascade delete (D-14)
```go
// Source: go.dev/blog/osroot + golang/go#67002 (no Root.RemoveAll) + #52745 (RemoveAll TOCTOU)
func purgeBeneath(root *os.Root, dir string) error {
    entries, err := root.ReadDir(dir) // openat2 RESOLVE_BENEATH — never follows escaping symlinks
    if err != nil { return err }
    for _, e := range entries {
        p := path.Join(dir, e.Name())
        if e.IsDir() {                 // a real dir (Lstat-based; symlink-to-dir reports as symlink)
            if err := purgeBeneath(root, p); err != nil { return err }
        }
        if err := root.Remove(p); err != nil { return err } // unlinks the link itself, never the target
    }
    return nil
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| in-container iptables OUTPUT rules (PRD original) | host-side forward-proxy allowlist (Claude Code, Codex, Modal) | 2026 consensus | D-08: untrusted code never owns its firewall; works under `cap_drop: ALL` + gVisor. |
| `O_NOFOLLOW` symlink guard (PRD original) | `os.Root`/`os.OpenInRoot` openat2 `RESOLVE_BENEATH` | Go 1.24+ (stable 1.26) | D-13: atomic kernel confinement, closes TOCTOU. |
| `time.Sleep`-based reaper tests | `testing/synctest` virtual clock | Go 1.25 GA | Deterministic, fast, flake-free TTL tests. |
| IPython/Jupyter kernel for persistence | stdlib `exec()` namespace dict (per-session) | this phase (D-03 constraint) | Keeps sidecar stdlib-only; minimal RAM. |
| `os.RemoveAll` for cleanup | manual openat-anchored no-follow walk | golang/go#67002 (no Root.RemoveAll) + #52745 (RemoveAll TOCTOU) | D-14: attacker-controlled tree demands no-follow. |

**Deprecated/outdated:**
- PRD "network_mode: bridge + iptables" → superseded by D-08 proxy (amendment item 3).
- PRD "migration 0010" → actual next free is 0008.
- PRD "conversation_id text" FK → must be uuid.
- CAP-02 "via iptables" wording → host proxy.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `testing/synctest` is GA-stable in go1.26.4 (was GA in 1.25). | Standard Stack | LOW — re-verify `go doc testing/synctest` at plan time; if not, use an injectable clock (`now func()` already in the SessionManager design). |
| A2 | The `aura-sandbox-egressless` non-masquerading bridge gateway is reachable from a session container for a loopback-published host proxy port. | Pitfall 1 | HIGH — if not, the proxy is unreachable and criterion 4 fails. Plan MUST include a live network-reachability spike for the session container → host proxy. |
| A3 | pip/urllib in the baked sidecar honor `HTTPS_PROXY` CONNECT for pypi.org:443 without extra config. | Pattern 3 | MEDIUM — pip honors `HTTPS_PROXY`; verify with the live criterion-4 test. |
| A4 | Exporting a minimal SSRF surface from `internal/web` (Pitfall 5 option a) is lower-risk than extracting `internal/netguard`. | Pitfall 5 | MEDIUM — if the export entangles too much, extract; either way Slice-5 web tier must be re-tested. |
| A5 | A non-empty `AURA_SANDBOX_NETWORK_ALLOW_HOSTS` under `AURA_PRIVACY_MODE=local-only` should FAIL-FAST (not silently inert). | Pattern 3 / D-10 | LOW — user/discuss-phase decision; document either way in 08-SECURITY. Verify `AURA_PRIVACY_MODE` is actually read somewhere in the codebase (it was referenced as D00.5; confirm it exists). |
| A6 | The sidecar's per-session namespace needs a `threading.Lock` to defend against a SECOND concurrent Aura process (the Aura-local container lock does not cover cross-process). | Pattern 2 | LOW — defense-in-depth; criterion-1 "2 process Aura concurrent" is named in PRD line 1252. |
| A7 | Boot recovery should also `docker rm` stray `aura-sandbox-sess-*` containers (not just mark DB rows). | Runtime State Inventory | MEDIUM — PRD says lazy-recreate; strays from a crash would leak. Confirm with the user/plan whether a boot `docker ps` sweep is in-scope or deferred. |

## Open Questions

1. **Session-container network + seccomp posture (the egress-bridge landmine).**
   - What we know: 2a denies `connect(2)` in seccomp + uses a non-masquerading bridge; 2b's proxy NEEDS connect-to-proxy.
   - What's unclear: exact bridge/seccomp variant for session containers; whether the host proxy is reachable at the bridge gateway.
   - Recommendation: dedicated plan task — design + live-spike the session network posture; extend 08-SECURITY/AR-05-01.
2. **`internal/web` SSRF reuse mechanism (export vs extract).**
   - What we know: the guard is unexported.
   - Recommendation: prefer a minimal export (`web.ClassifyIP`, a dial-guard constructor) for scope control; re-test Slice 5.
3. **Cross-package cleanup ownership (`conversations.Delete` → workspace cascade).**
   - What we know: `store.go:449` uses `os.RemoveAll`; D-14 forbids it for the workspace subtree.
   - Recommendation: `WorkspaceManager.PurgeConversationDir`; wire via an interface to avoid `sandbox`→`conversations` import cycle.
4. **`AURA_PRIVACY_MODE` existence + semantics.** Confirm the env is read in the current codebase (referenced as D00.5) before relying on it for D-10.
5. **DNS pin TTL for pypi (OQ6).** Per-CONNECT resolution vs the 60s `dnsPin` TTL — validate against pypi rotation in the live test.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain (os.Root, synctest) | All Go code | ✓ | go1.26.4 (verified) | — |
| Docker CLI + daemon | SessionManager `docker run` (D-05), integration tier | ✓ (dev: Docker Desktop/WSL; CI: DinD) | — | Integration tests `t.Fatal` under `$CI` if absent (no-skip-as-green); unit tests need no docker. |
| gVisor `runsc` | x86 session-container boundary (D-04) | ✓ on x86 (2a wired) | — | arm64 falls back to runc + seccomp floor (inherited D-07). |
| Postgres 17 | `sandbox_sessions` registry | ✓ | 17 (compose) | DB integration tier `t.Fatal` under `$CI` if DSN unset. |
| pypi.org reachability | criterion-4 live network test | host-dependent | — | Test is `sandbox_integration`-tagged + live-only; skips locally, hard-runs in CI with egress. |

**Missing dependencies with no fallback:** none — all present in the WSL/CI quality-gate environment (CLAUDE.md confirms WSL runs the full `make quality-full` incl. docker stack via 127.0.0.1).

## Validation Architecture

> Nyquist enabled (not disabled in config). This section feeds VALIDATION.md.

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` + `testing/synctest` (virtual clock) + `go.uber.org/goleak` (existing) |
| Config file | none (Go convention); build tags `//go:build sandbox_integration` (+ `db_integration` where the registry is exercised) |
| Quick run command | `go test ./internal/sandbox/ ./internal/scoring/` (unit, no docker/db) |
| Full suite command | WSL: `export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"` + composed DSNs + `go test -race -tags 'sandbox_integration db_integration' ./internal/sandbox/ ./internal/scoring/ ./internal/conversations/` then `make quality-full` |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| CAP-02 (crit 1a) | Session persistence: `x=42` call 1 readable call 2 (in-memory interp) | integration | `go test -tags sandbox_integration -run TestSessions_PythonStatePersists ./internal/sandbox/` | ❌ Wave 0 |
| CAP-02 (crit 1b) | Session persistence: workspace file written call 1 visible call 2 | integration | `go test -tags sandbox_integration -run TestSessions_WorkspacePersists ./internal/sandbox/` | ❌ Wave 0 |
| CAP-02 (crit 1c) | Two Aura processes, same session → serialized via lock (no race) | integration | `go test -race -tags sandbox_integration -run TestSessions_ConcurrentSerialized ./internal/sandbox/` | ❌ Wave 0 |
| CAP-02 (crit 2) | Symlink escape: `ln -s /etc /workspace/escape` + host cascade → `/etc` untouched, symlink removed | integration | `go test -tags sandbox_integration -run TestWorkspace_SymlinkEscapeCascade ./internal/sandbox/` | ❌ Wave 0 |
| CAP-02 (crit 3) | Idle TTL reap: after TTL, session reaped, container removed, workspace cleaned, `sessions list` empty | unit (synctest) + integration | `go test -run TestReaper_EvictsAfterTTL ./internal/sandbox/` (synctest) + `-tags sandbox_integration -run TestReaper_LiveContainerRemoved` | ❌ Wave 0 |
| CAP-02 (crit 4a) | `network_allow:[pypi.org]` → `pip install` to pypi succeeds | integration (live egress) | `go test -tags sandbox_integration -run TestNetwork_PyPIAllowed ./internal/sandbox/` | ❌ Wave 0 |
| CAP-02 (crit 4b) | same call `curl 1.1.1.1`/`urlopen(example.com)` → refused | integration | `go test -tags sandbox_integration -run TestNetwork_NonAllowlistRefused ./internal/sandbox/` | ❌ Wave 0 |
| CAP-02 (cap) | 6th concurrent session → `ErrSessionCapReached` (no silent LRU) | unit | `go test -run TestSessions_HardCapEnforced ./internal/sandbox/` | ❌ Wave 0 |
| CAP-02 (boot) | boot recovery: `active` rows → `terminated`, lazy recreate | integration (db) | `go test -tags db_integration -run TestSessions_BootRecovery ./internal/sandbox/` | ❌ Wave 0 |
| CAP-02 (proxy) | deny-wins glob: `*.pythonhosted.org` allowed, parent denied; private-IP resolve rejected | unit | `go test -run TestProxy_AllowlistGlobAndSSRF ./internal/sandbox/` | ❌ Wave 0 |
| CAP-02 (scoring) | `ComputeSandboxTier`: empty=Safe, pypi-only=Safe, arbitrary=Risky; modifiers monotone (never down) | unit + property | `go test -run TestScoring ./internal/scoring/` | ❌ Wave 0 |
| CAP-02 (reaper-leak) | reaper goroutine leaves no leak | unit | `goleak.VerifyNone` in `sandbox` TestMain | ❌ Wave 0 |

### Sampling Rate
- **Per task commit:** `go vet ./... && go build ./... && go test ./internal/sandbox/ ./internal/scoring/` (+ `-race` on touched packages).
- **Per wave merge:** full tagged tiers in WSL with stack up (`go test -race -tags 'sandbox_integration db_integration' ...`).
- **Phase gate:** `make quality-full` green (coverage ≥85% owned-surface floor, CLAUDE.md overrides PRD) + the 4 ROADMAP criteria live + mutation spot-check ≥70% on `network.go` + `scoring.go` (security/decision-critical files) via WSL go-mutesting.

### Wave 0 Gaps
- [ ] `internal/sandbox/sessions_test.go` — session persistence, hard cap, concurrent-serialize, boot recovery (covers crit 1, cap, boot)
- [ ] `internal/sandbox/workspace_test.go` — quota walkSize, symlink-escape cascade (covers crit 2)
- [ ] `internal/sandbox/network_test.go` — glob allow/deny-wins, SSRF reject, proxy CONNECT (covers crit 4, proxy)
- [ ] `internal/sandbox/reaper_test.go` (or within sessions_test) — synctest TTL reap (covers crit 3)
- [ ] `internal/scoring/scoring_test.go` — exhaustive tier table + property-based modifier monotonicity (covers scoring)
- [ ] `internal/sandbox` TestMain with `goleak.VerifyNone` (reaper leak gate)
- [ ] `t.Fatal`-under-`$CI` skip helpers for `sandbox_integration` + `db_integration` (no-skip-as-green — copy the 2a `docker_integration_test.go:35-42` pattern)
- [ ] Migration test: `aura db migrate` up/down round-trip for 0008 (db tier)

## Security Domain

> `security_enforcement` enabled (absent = enabled). Phase 8 extends the Phase 5 threat register (05-SECURITY.md) — author a 08-SECURITY.md.

### Applicable ASVS Categories
| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | single-user substrate; no auth surface added. |
| V3 Session Management | partial | sandbox "session" ≠ auth session, but lifecycle/eviction/cap hardening applies (hard cap 5, TTL reap, boot recovery). |
| V4 Access Control | yes | per-conversation isolation (one container per conv, D-04); cascade-on-delete; workspace owner 65532. |
| V5 Input Validation | yes | `conversation_id`/`session_id` traversal guard (existing `validateID`), CSV allowlist parse, glob validation (reject global `*` in deny per Codex). |
| V6 Cryptography | no | no MITM (D-10), no new crypto. |
| V10/V12 (egress / SSRF) | yes | host-side proxy + resolve-then-pin SSRF reuse (Slice 5), deny-wins allowlist. |
| V14 Configuration | yes | session-container hardening flags replicated in `docker run` argv (Pitfall 6); no docker socket (D-05). |

### Known Threat Patterns for {Go control plane + gVisor container + host proxy + host FS walker}
| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Workspace symlink escape (CVE-2026-39861 shape) | Elevation/Tampering | `os.Root` openat2 walks + manual no-follow cascade (D-13/D-14); acceptance crit 2. |
| Docker socket mount → host takeover | Elevation | NEVER mount socket; `docker` CLI shelled, LookPath-gated (D-05, inherited T-05-03-EOP-SOCKET). |
| Egress data-exfil via allowlist bypass / DNS rebinding | Information Disclosure | CONNECT-time hostname validation + resolve-then-pin + classify-every (Slice 5); deny-wins; document Claude-Code-bypass caveat (D-10) in 08-SECURITY. |
| Request smuggling through the proxy | Tampering | proxy validates CONNECT target only, no body parsing/forwarding (CONNECT is opaque tunnel); reject malformed CONNECT. |
| Resource exhaustion (N sessions, RAM) | DoS | hard cap 5 (`ErrSessionCapReached`, no silent LRU) + per-container 512m/1cpu/64pids (replicated in `docker run`) + TTL reap. |
| Cross-conversation state leak | Information Disclosure | one container + one workspace dir per conv; per-(conv,host) DNS pin (Slice 5 already scopes by conv). |
| connect(2) re-enabled weakens 2a posture | Elevation | session profile allows connect ONLY because egress is host-proxy-contained; document deviation extending AR-05-01; empty-allowlist sessions keep the 2a egressless posture. |
| Privacy-mode bypass (`local-only` + allowlist) | Information Disclosure | fail-fast or inert under `AURA_PRIVACY_MODE=local-only` (D-10/A5). |

## Project Constraints (from CLAUDE.md)
- PRD-first absolute: 5 PRD/DECISIONS amendments (D-01/05/08/11 major + D-13 minor) land as commits BEFORE any code commit.
- 600-LOC file ceiling → split on touch (`sessions.go`, `workspace.go`, `network.go` each ~80-150; if a file grows, split `<name>_<concern>.go`).
- Coverage floor **85%** owned-surface (overrides PRD 75/60). Report combined unit+integration figure.
- No-skip-as-green: integration tiers `t.Fatal` under `$CI` when env unset (copy 2a pattern).
- Deferred-tool pattern: `execute` stays `Deferred:true`; `session_id` arg becomes ACTIVE (was inert in 2a); admin lives in CLI (`aura sandbox sessions`), not a new tool.
- Env convention `AURA_<DOMAIN>_<UNIT>` (the 5 new sandbox/risk vars comply).
- One slice = one commit (sub-commits allowed per sub-area with atomicity noted).
- Typed sentinel errors: add `ErrSessionCapReached`, `ErrWorkspaceQuotaExceeded` following `ErrSandboxUnreachable`.
- goleak.VerifyNone mandatory (reaper goroutine).
- No god class / no duplicate (do NOT copy-paste Slice-5 `classify` — export/extract instead).
- WSL is the full quality-gate env; mutation spot-check on critical files; deep-refactor-on-touch when editing shipped 2a/Slice-5 files.

## Sources

### Primary (HIGH confidence)
- `internal/sandbox/{sandbox,docker,errors}.go`, `internal/agent/tools/{execute,result}.go`, `internal/web/{ssrf,dnspin,transport}.go`, `internal/config/config.go`, `internal/conversations/store.go`, `compose.yaml`, `sandbox/sidecar.py`, `internal/db/{migrations/0005,queries/conversations}.sql` — all READ IN FULL this session. [VERIFIED]
- `prd.md` §Slice 2 (1200-1376) + §Risk-Based Governance (4459-4567) — READ. [VERIFIED]
- `08-CONTEXT.md` (D-01..D-14) + `05-CONTEXT.md` + `05-SECURITY.md` (AR-05-01 egress deviation) — READ IN FULL. [VERIFIED]
- `D:/tmp/codex/codex-rs/network-proxy/src/{network_policy,connect_policy,state}.rs` — READ (deny-wins globset, baseline-deny, is_non_public_ip connect-time reject, global-wildcard-in-deny rejection). [VERIFIED]
- go.dev/blog/osroot — os.Root openat2 RESOLVE_BENEATH. [CITED]
- golang/go#67002 (no Root.RemoveAll, open) + #52745 (RemoveAll TOCTOU symlink race). [CITED]
- `go.mod` / `go version` — go1.26.4 confirmed. [VERIFIED]

### Secondary (MEDIUM confidence)
- pkg.go.dev/net/http (Hijacker CONNECT tunnel pattern). [CITED]
- michaellivs.com / oneuptime gVisor egress-proxy + JWT-allowlist articles — corroborate host-proxy-for-pip consensus (D-08). [CITED]
- AIO agent-infra-sandbox (`D:/tmp/agent-infra-sandbox` present) — persistent-kernel session API grounding (D-01/D-02 asymmetric persistence). [CITED]

### Tertiary (LOW confidence)
- `testing/synctest` exact 1.26 stability — re-verify at plan time (A1). [ASSUMED]

## Metadata
**Confidence breakdown:**
- Standard stack: HIGH — all stdlib, verified against go.mod + shipped code.
- Architecture: HIGH — control-plane + proxy + os.Root patterns verified against Codex source + Go stdlib + shipped Slice-5.
- Pitfalls: HIGH — the four landmines (FK type, migration number, egress bridge, dir co-tenancy) are derived directly from reading the actual schema/compose/store code, not assumed.
- Egress-bridge interaction (A2): MEDIUM — requires a live reachability spike in planning.

**Research date:** 2026-06-03
**Valid until:** 2026-07-03 (stable stdlib + locked CONTEXT; re-verify synctest 1.26 status and pypi DNS behavior at plan time).
