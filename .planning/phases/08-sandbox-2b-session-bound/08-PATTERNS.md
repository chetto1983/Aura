# Phase 8: Sandbox 2b Session-Bound - Pattern Map

**Mapped:** 2026-06-03
**Files analyzed:** 13 (8 new, 5 modified)
**Analogs found:** 13 / 13 (every new/modified file has a concrete in-repo analog)

> All line numbers are as-read on 2026-06-03. Excerpts are verbatim from the shipped tree (`internal/sandbox/`, `internal/web/`, `internal/conversations/`, `internal/config/`, `internal/agent/tools/`, `internal/db/`, `cmd/aura/`, `sandbox/sidecar.py`). The planner copies these patterns; it does NOT re-invent. Cross-cutting concerns (sentinel errors, no-skip-as-green, goleak, deferred-tool, sqlc one-file-one-query, `os.Root` symlink guard, SSRF reuse) are in **Shared Patterns** — apply once, reference everywhere.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/sandbox/sessions.go` (NEW ~150) | service / control-plane | event-driven (reaper) + request-response (Acquire/Release) | `internal/sandbox/docker.go` (lifecycle/exec client) + `internal/web/dnspin.go` (sync.Map-style guarded map + injectable clock) | role-match (no existing control-plane-with-reaper; composed from two analogs) |
| `internal/sandbox/workspace.go` (NEW ~80) | service / utility | file-I/O | `internal/conversations/orphan_scan.go` (`O_NOFOLLOW`/Lstat symlink-guarded walk) + `internal/conversations/store.go` Delete cascade | exact (orphan_scan is the same no-follow-walk problem; upgrade `O_NOFOLLOW`→`os.Root`) |
| `internal/sandbox/network.go` (NEW ~80) | service / middleware | request-response (CONNECT proxy) | `internal/web/transport.go` (`hardenedTransport`/`dialContext`) + `internal/web/ssrf.go` (`classify`/`validateAndPin`) + `internal/web/dnspin.go` | role-match (proxy is new shape; SSRF core is verbatim-reused but UNEXPORTED — see landmine) |
| `internal/scoring/scoring.go` (NEW ~100) | utility / pure-module | transform (pure functions, no IO) | `internal/llm/prices.go` (pure lookup module) + the `RiskTier` spec in prd.md §Risk-Based Governance | role-match (pure module idiom) |
| `internal/scoring/scoring_test.go` (NEW ~80) | test | transform | `internal/web/ssrf_test.go` `TestBlocked_Classification` (exhaustive `t.Parallel` table) + `internal/llm/prices_test.go` | exact |
| `internal/db/queries/sandbox_sessions.sql` (NEW, 4 queries) | data-access | CRUD | `internal/db/queries/conversations.sql` (sqlc `:one`/`:exec`/`:many` naming + RETURNING shape) | exact |
| `internal/db/migrations/0008_sandbox_sessions.{up,down}.sql` (NEW) | migration | schema | `internal/db/migrations/0005_conversations.up.sql` (uuid PK + FK + GRANT + index) + `0007_cache_metrics.down.sql` (down idiom) | exact |
| `internal/sandbox/sandbox.go` (MODIFIED) | model / interface | — | itself (extend `Runner` + `Result` additively) | exact |
| `internal/sandbox/errors.go` (MODIFIED) | model / errors | — | itself (`ErrSandboxUnreachable`/`ErrSandboxProtocol` sentinel block) | exact |
| `internal/agent/tools/execute.go` (MODIFIED) | controller / tool | request-response | itself (`executeArgs.SessionID` already present, inert) + `internal/agent/tools/result.go` `WithToolCallContext` | exact |
| `internal/config/config.go` (MODIFIED) | config | — | itself, lines 154-156 (`Sandbox*` knobs) + `envDefault`/`envIntDefault` (187-208) | exact |
| `cmd/aura/main.go` + new `cmd/aura/sandbox.go` (MODIFIED/NEW) | controller / CLI | request-response | `cmd/aura/db.go` (`runDB` nested switch dispatcher) + `cmd/aura/exec.go` (`parseExecArgs --session`) | exact |
| `sandbox/sidecar.py` (MODIFIED) | service / sidecar | request-response + stateful | `sandbox/sidecar.py` itself (`INTERPRETERS` dispatch + `run_code` + `Handler.do_POST`) | exact (extend, keep stdlib-only) |

---

## Pattern Assignments

### `internal/sandbox/sessions.go` (service / control-plane, NEW ~150)

**Primary analogs:** `internal/sandbox/docker.go` (lifecycle/exec + docker-CLI-gated shellout) and `internal/web/dnspin.go` (mutex-guarded per-key map + injectable `now func() time.Time` for deterministic time tests).

**Lifecycle shellout pattern — copy the LookPath-gated, NEVER-socket idiom from `docker.go:162-174`:**
```go
// autoStart is the D-09 one-shot best-effort sidecar start. It is GATED on the
// docker CLI being on PATH ... It NEVER references or mounts the docker socket
// (escape vector #1); it only shells `docker compose up -d aura-sandbox` ...
func (r *DockerRunner) autoStart(ctx context.Context) {
	if _, err := exec.LookPath("docker"); err != nil {
		return // docker CLI absent — nothing to start; caller wraps ErrSandboxUnreachable
	}
	upCmd := exec.CommandContext(ctx, "docker", "compose", "up", "-d", "aura-sandbox") //nolint:gosec // fixed argv, no socket
	_ = upCmd.Run()
	r.probeHealth(ctx)
}
```
Adaptation (D-05): SessionManager shells `docker run --runtime=<cfg.SandboxRuntime> --user 65532:65532 --read-only --tmpfs /tmp --cap-drop ALL --security-opt no-new-privileges --security-opt seccomp=<session-profile> --pids-limit 64 --memory 512m --cpus 1.0 --ulimit nofile=64 ...` (Pitfall 6: the compose hardening is NOT inherited by an ad-hoc `docker run` — replicate every flag in argv) then `docker stop`/`docker rm`. Same `exec.LookPath("docker")` gate, same `//nolint:gosec // fixed argv, no socket` comment, same NEVER-mount-socket invariant.

**Injectable-clock + guarded-map pattern — copy the `dnsPin` shape from `dnspin.go:28-44` for the reaper's determinism (synctest, PRD line 1720):**
```go
type dnsPin struct {
	mu  sync.Mutex
	m   map[pinKey]pinEntry
	ttl time.Duration
	now func() time.Time
}
func newDNSPin(ttlSec int) *dnsPin {
	return &dnsPin{
		m:   make(map[pinKey]pinEntry),
		ttl: time.Duration(ttlSec) * time.Second,
		now: time.Now,
	}
}
```
Adaptation: RESEARCH Pattern 1 gives the target struct (`SessionManager{ sessions sync.Map; capMu sync.Mutex; count int; maxN int; ttl time.Duration; now func() time.Time }` + per-`*session` `sync.Mutex` for D-07). The hard cap (D-12: 5) is checked under `capMu` before creating an entry → `ErrSessionCapReached` (NO silent LRU). Reaper is a `time.Ticker(60s)` goroutine bound to a cancelable ctx that `Close()` waits on via a done channel (goleak-clean — see Shared Pattern: goleak).

**600-LOC ceiling:** if `sessions.go` + reaper + boot-recovery exceed ~400 LOC, split per CLAUDE.md → `sessions_reaper.go` / `sessions_recovery.go`.

---

### `internal/sandbox/workspace.go` (service / utility, NEW ~80)

**Primary analog:** `internal/conversations/orphan_scan.go:32-43` — this is the SAME problem (a host walker over an attacker-influenceable per-conversation dir that must refuse symlink redirection). Today it uses `O_NOFOLLOW`/`Lstat`; D-13/D-14 upgrade it to `os.Root`/openat2.
```go
//   - removes $AURA_RUN_DIR/conversations/<id> dirs with NO matching conversations
//     row (session_id == conversation_id, D-26), under an O_NOFOLLOW/Lstat symlink
//     guard so a malicious symlink cannot redirect RemoveAll outside runDir;
```
**Cascade target — `internal/conversations/store.go:435-454` (`Store.Delete`), the exact line to change:**
```go
func (s *Store) Delete(ctx context.Context, conversationID string) error {
	...
	dir, err := s.sidecarDir(conversationID)
	...
	if rmErr := os.RemoveAll(dir); rmErr != nil {   // <-- store.go:449 — D-14 FORBIDS os.RemoveAll for the workspace subtree
		return fmt.Errorf("delete conversation %s: remove sidecar dir (orphan-scan will reconcile): %w",
			conversationID, rmErr)
	}
	return nil
}
```
Adaptation (D-13/D-14, RESEARCH Pattern 4 + Code Example "os.Root no-follow cascade delete"): implement `WorkspaceManager.EnsureDir`, `walkSize` (quota via `os.OpenRoot` + recurse + `Lstat` regular files only), and `PurgeConversationDir(convID)` (manual post-order openat walk; `os.Root` has NO `RemoveAll` — golang/go#67002). Replace the `store.go:449` `os.RemoveAll(dir)` with a call into this. **Co-tenancy landmine #4:** the dir holds BOTH `<tool_call_id>.result` spillover (Aura-controlled, `result.go:70`) AND `workspace/` (attacker-controlled). Cleanest: `WorkspaceManager.PurgeConversationDir` does the full `os.Root` walk of `<id>/`. **Import direction:** `sandbox` must NOT import `conversations`; define a small cleanup interface in `conversations` that `main.go` wires to a `sandbox` impl (Open Question 3).

**Acceptance gate (criterion 2):** `ln -s /etc /workspace/escape` → host cascade → `Lstat` reports `ModeSymlink` → `root.Remove("escape")` unlinks the link, `/etc` untouched.

---

### `internal/sandbox/network.go` (service / middleware, NEW reshaped ~80)

**Primary analog:** `internal/web/transport.go` `hardenedTransport.dialContext` (transport.go:80-90) + `internal/web/ssrf.go` `classify`/`validateAndPin` (ssrf.go:35-107) + `internal/web/dnspin.go`.

**Resolve-then-pin gate — `transport.go:80-90`, the exact dial-time SSRF gate to mirror in the proxy's CONNECT handler:**
```go
func (h *hardenedTransport) dialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	pinned, reason := h.guard.validateAndPin(ctx, convIDFrom(ctx), host)
	if reason != "" {
		return nil, &internalError{code: CodeBlockedURL, reason: reason, message: "blocked", host: host}
	}
	return h.dial(ctx, network, net.JoinHostPort(pinned.String(), port))
}
```
**Classify-every-fail-closed — `ssrf.go:96-106` (NEVER cherry-pick a public IP from a mixed result):**
```go
for _, ip := range ips {
	if _, blocked := classify(ip); blocked {
		return netip.Addr{}, ReasonPrivateOrMetadata // fail closed on ANY blocked record
	}
	if !first.IsValid() {
		first = ip.Unmap()
	}
}
```
Adaptation (D-08/D-09/D-10, RESEARCH Pattern 3): a stdlib `http.Server` on a loopback host port; on `CONNECT host:port` → (1) deny-wins glob match against the per-session allowlist (RESEARCH Code Example, modeled on Codex `network_policy.rs`: `if p.denySet.Match(host) { return false }; return p.allowSet.Match(host)`; reject a global `*` in the deny list), (2) `validateAndPin` resolve-then-pin, (3) `Hijack()` the conn, dial the pinned IP, `io.Copy` bidirectionally. NO MITM (D-10).

**⚠️ LANDMINE #5 (Pitfall 5) — the SSRF surface is UNEXPORTED.** `classify`, `guard`, `validateAndPin`, `dnsPin` are all package-private to `internal/web` (verified ssrf.go/dnspin.go/transport.go). This is NOT "reuse verbatim" — the planner MUST add an export-or-extract task BEFORE network.go:
- **Option (a), preferred for scope:** export a minimal surface from `internal/web` (e.g. `web.ClassifyIP(netip.Addr) (string,bool)` + a dial-guard constructor) — smaller, but couples `sandbox`→`web`.
- **Option (b), cleaner:** extract IP-classification + DNS-pin into a new `internal/netguard` package that both `web` and `sandbox` import — larger refactor touching shipped Slice-5 code (deep-refactor-on-touch applies). 
Either way **DO NOT copy-paste `classify` into network.go** (CLAUDE.md NO-DUPLICATE; `dupl` + audit will flag it). Re-test the Slice-5 web tier after any export (no regression).

---

### `internal/scoring/scoring.go` (utility / pure-module, NEW ~100)

**Primary analog:** `internal/llm/prices.go` (a self-contained pure lookup module, no DB/IO) for the package shape; the spec is prd.md §Risk-Based Governance (~4459-4548) + RESEARCH Code Example "scoring module sandbox advisory path":
```go
package scoring

type RiskTier string
const ( Safe RiskTier="safe"; Normal RiskTier="normal"; Risky RiskTier="risky"; Destructive RiskTier="destructive" )

type SandboxArgs struct{ NetworkAllow []string }
func ComputeSandboxTier(a SandboxArgs) RiskTier {
	if len(a.NetworkAllow) == 0 { return Safe }    // empty = no egress = SAFE (D-12)
	if onlyPyPI(a.NetworkAllow) { return Safe }    // pypi.org-only = legit install = SAFE (D-12)
	return Risky                                   // arbitrary domains = RISKY (D-12)
}
func GateRecommended(t RiskTier) bool { return t == Risky || t == Destructive }
```
Adaptation (D-11/D-12): build the FULL module now — `RiskTier` enum, `ComputeTaskTier`, `ComputeSkillTier`, `ComputeSandboxTier`, `GateRecommended`, `RequiresImmediateAlert`, the UP-only modifier table, and `AURA_RISK_ALERT_THRESHOLD` (read via `config.go`, NOT inside `scoring` — mirror how `dnspin.go:36-37` takes the TTL as a constructor arg rather than reading env). **SCOPE GUARD (D-12):** `ComputeTaskTier`/`ComputeSkillTier`/`RequiresImmediateAlert` are BUILT + UNIT-TESTED but have NO runtime consumers in Phase 8 (Scheduler P10 / Skills P11 wire them). Only the sandbox advisory path is wired: the `execute` result for a session call with a non-empty allowlist appends `{risk_tier, gate_recommended}` to the lean preview; NO pending-state persistence.

---

### `internal/scoring/scoring_test.go` (test, NEW ~80)

**Primary analog:** `internal/web/ssrf_test.go` `TestBlocked_Classification` (ssrf_test.go:18-55) — exhaustive `t.Parallel()` table with `name/input/expected` rows:
```go
func TestBlocked_Classification(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		ip      string
		blocked bool
		reason  string
	}{
		{"loopback v4", "127.0.0.1", true, "loopback"},
		...
	}
```
Adaptation: one exhaustive table per `Compute*Tier` (empty→Safe, pypi-only→Safe, arbitrary→Risky; modifier-table rows) + a property-based test for modifier monotonicity (modifiers only ever raise a tier, never lower it — RESEARCH Validation map "modifiers monotone (never down)"). No build tag (pure unit; `//go:build !web_integration`-style tag is unnecessary — scoring has no integration tier).

---

### `internal/db/queries/sandbox_sessions.sql` (data-access / CRUD, NEW)

**Primary analog:** `internal/db/queries/conversations.sql:1-39` — sqlc `:one`/`:exec`/`:many` annotation + explicit-column `RETURNING`:
```sql
-- name: CreateConversation :one
INSERT INTO aura.conversations (id, identity_id, model, status, metadata)
VALUES ($1, $2, $3, 'active', $4)
RETURNING id, title, identity_id, created_at, ...;

-- name: UpdateConversationStatus :exec
UPDATE aura.conversations SET status = $2, last_active_at = now() WHERE id = $1;
```
Adaptation (RESEARCH Code Example "sqlc queries"): exactly 4 queries (sqlc one-file-one-query anti-god-class) — `InsertSession :one` (RETURNING the full row), `TouchLastUsed :exec`, `MarkTerminated :exec`, `ListActive :many ... WHERE status='active' ORDER BY last_used_at`. The string `session_id`/`conversation_id` arriving from the tool is parsed to `uuid` at the SessionManager boundary, exactly like `store.go` `parseUUID` (landmine #1).

---

### `internal/db/migrations/0008_sandbox_sessions.{up,down}.sql` (migration / schema, NEW)

**Primary analog:** `internal/db/migrations/0005_conversations.up.sql:8-21,60-68` (uuid PK, FK with `ON DELETE CASCADE`, dual GRANT, status CHECK, index) + `0007_cache_metrics.down.sql` (the `DROP TABLE IF EXISTS` down idiom).
```sql
CREATE TABLE aura.conversations (
    id                  uuid          PRIMARY KEY,
    ...
    identity_id         uuid          NOT NULL REFERENCES aura.identities (id) ON DELETE CASCADE,
    status              text          NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived', 'deleted')),
    ...
);
GRANT SELECT, INSERT, UPDATE, DELETE ON aura.conversations TO aura_app;
GRANT ALL                            ON aura.conversations TO aura_migrate;
CREATE INDEX conversations_identity_status_idx ON aura.conversations (identity_id, status, last_active_at DESC);
```
Adaptation (RESEARCH Code Example "sandbox_sessions migration"):
- **⚠️ LANDMINE #2:** file number is **0008**, NOT 0010 (verified `ls internal/db/migrations/` → highest is 0007; next free is 0008). Amend the PRD's "0010" reference.
- **⚠️ LANDMINE #1:** `conversation_id uuid NOT NULL REFERENCES aura.conversations(id) ON DELETE CASCADE` — uuid, NOT `text` (the PRD's literal `text` FK fails against the uuid PK; verified 0005:9). Precedent: 0005:52-53 promoted `paused_states.conversation_id` text→uuid for the same reason.
- Columns: `id uuid PK DEFAULT gen_random_uuid()`, `container_id text`, `image_digest text`, `started_at`/`last_used_at timestamptz DEFAULT now()`, `status text CHECK (status IN ('active','idle','terminated','evicted'))`; index on `(status, last_used_at)` for the reaper + boot recovery; dual GRANT. Down = `DROP TABLE IF EXISTS aura.sandbox_sessions;`.

---

### `internal/sandbox/sandbox.go` (model / interface, MODIFIED)

**Analog: itself (sandbox.go:15-30).** Extend the `Runner` interface + `Result` ADDITIVELY — do not break the stateless 2a path.
```go
type Runner interface {
	RunPython(ctx context.Context, code string, timeoutSec int) (Result, error)
	RunShell(ctx context.Context, cmd string, timeoutSec int) (Result, error)
}
```
Adaptation: add session-aware methods (or a session-scoped variant) without altering the existing two signatures (2a callers — `execute.go`, `exec.go` — must keep compiling). `DockerRunner` (`docker.go:42`) gains a per-session exec path (`POST /session/{id}/exec/{lang}`) alongside the existing `/exec/{lang}`. `Result` may gain advisory fields (e.g. for `{risk_tier, gate_recommended}`) but the four original fields stay (sandbox.go:23-30 comment: "keep the agent-loop binding intact").

---

### `internal/sandbox/errors.go` (model / errors, MODIFIED)

**Analog: itself (errors.go:13-16).** Add new sentinels to the existing block:
```go
var (
	ErrSandboxUnreachable = errors.New("sandbox sidecar unreachable (auto-start failed)")
	ErrSandboxProtocol    = errors.New("sandbox sidecar returned a malformed response")
)
```
Adaptation: append `ErrSessionCapReached` and `ErrWorkspaceQuotaExceeded` following the exact `errors.New(...)` + `errors.Is`-friendly doc-comment convention. The CLI (`exec.go:118-123`) maps these to exit codes via `errors.Is`.

---

### `internal/agent/tools/execute.go` (controller / tool, MODIFIED)

**Analog: itself.** The `session_id` arg ALREADY exists and is inert (execute.go:32,42,70-73). Phase 8 makes it ACTIVE.
```go
type executeArgs struct {
	Lang       string `json:"lang"`
	Code       string `json:"code"`
	TimeoutSec int    `json:"timeout_sec"`
	SessionID  string `json:"session_id"`
}
...
if a.SessionID != "" {
	// Reserved-but-inert in 2a: an error ToolResult so the model self-corrects.
	return NewResult(ctx, "error: session_id is reserved for Phase 8 / Slice 2b ...")
}
```
**Default session_id = conversation_id — already wired via `result.go:26-33` `WithToolCallContext` (NO `InvocationContext` change, D-25):**
```go
func WithToolCallContext(ctx context.Context, sessionID, toolCallID, runDir string, previewCap int) context.Context {
	return context.WithValue(ctx, toolCallContextKey{}, toolCallContext{ sessionID: sessionID, ... })
}
```
Adaptation: remove the inert-reject branch; when `session_id` is empty, default it to `toolCallCtx(ctx).sessionID` (= conversation id). Update the `Spec()` description (execute.go:54: delete "Do NOT set session_id ...") and document the D-02 ASYMMETRIC persistence contract (python vars persist; shell `cd`/`export` do NOT). `execute` stays `Deferred: true`; NO new deferred tool for sessions (admin lives in CLI). Validate `session_id` with the existing `validateID` traversal guard (`result.go:45-58`).

---

### `internal/config/config.go` (config, MODIFIED)

**Analog: itself, lines 49-55 (struct fields) + 154-156 (loader) + 187-208 (`envDefault`/`envIntDefault`).**
```go
SandboxURL:        envDefault("AURA_SANDBOX_URL", "http://127.0.0.1:18901"),
SandboxTimeoutSec: envIntDefault("AURA_SANDBOX_TIMEOUT_SEC", 30), // 600s cap clamped runner-side
SandboxRuntime:    envDefault("AURA_SANDBOX_RUNTIME", defaultRuntimeForArch()),
```
Adaptation: add 5 fields + loader lines (AURA_<DOMAIN>_<UNIT> convention):
- `SandboxSessionTTLSec int` ← `envIntDefault("AURA_SANDBOX_SESSION_TTL_SEC", 1800)`
- `SandboxMaxConcurrentSessions int` ← `envIntDefault("AURA_SANDBOX_MAX_CONCURRENT_SESSIONS", 5)`
- `SandboxWorkspaceMaxBytes int` ← `envIntDefault("AURA_SANDBOX_WORKSPACE_MAX_BYTES", 104857600)`
- `SandboxNetworkAllowHosts string` ← `envDefault("AURA_SANDBOX_NETWORK_ALLOW_HOSTS", "")` (CSV; empty default; parse to `[]string` at the SessionManager/proxy boundary)
- `RiskAlertThreshold string` ← `envDefault("AURA_RISK_ALERT_THRESHOLD", "risky")`
All non-fatal `envIntDefault`/`envDefault` (typo falls back, not boot-fatal — the established 2b/web knob discipline). **D-10:** under `AURA_PRIVACY_MODE=local-only` a non-empty allowlist must fail-fast or be inert (confirm `AURA_PRIVACY_MODE` is actually read — RESEARCH Open Question 4).

---

### `cmd/aura/main.go` + new `cmd/aura/sandbox.go` (controller / CLI, MODIFIED/NEW)

**Analog: `cmd/aura/db.go:19-42` (`runDB` nested switch) for the new `aura sandbox sessions {list|terminate|prune}` subcommand group:**
```go
func runDB(args []string) {
	if len(args) < 1 { fmt.Fprintln(os.Stderr, "usage: aura db {migrate|ping|status|reset}"); os.Exit(1) }
	cfg := config.LoadDB()
	ctx := context.Background()
	switch args[0] {
	case "migrate": dbMigrate(ctx, cfg)
	...
	default: fmt.Fprintln(os.Stderr, "usage: ..."); os.Exit(1)
	}
}
```
**Analog: `cmd/aura/exec.go:43-79` (`parseExecArgs`) for `aura exec --session <conv_id>` — the `--session` flag latch is ALREADY parsed (exec.go:31,49-60), just inert (exec.go:90-93):**
```go
case arg == "--session":
	expectSession = true
...
if ea.session != "" {
	fmt.Fprintln(os.Stderr, "aura exec: --session is reserved for Phase 8 / Slice 2b ...")
	os.Exit(exitUsage)
}
```
Adaptation: (1) add `case "sandbox":` to `main.go:37` switch → `runSandbox(os.Args[2:])` in a new `cmd/aura/sandbox.go` (mirror `db.go`/`agent.go`/`identity.go` — "Lives in package main alongside main.go's switch case"). `runSandbox` dispatches `sessions {list|terminate|prune}` over `LoadDB()` + a SessionManager/`ListActive` query. (2) In `exec.go:90-93`, replace the inert-reject with the real session path (route `RunPython`/`RunShell` through the session-bound runner). Reuse `tools.FormatLean` (exec.go:126 — no drift). Output formatting for `sessions list` is Claude's discretion (consider `text/tabwriter`, as `db.go:13` imports).

---

### `sandbox/sidecar.py` (service / sidecar, MODIFIED — stays Python-stdlib-only)

**Analog: itself — `INTERPRETERS` dispatch (sidecar.py:48-51) + `run_code` (88-136) + `Handler.do_POST` (158+).**
```python
INTERPRETERS = {
    "/exec/python": ["python3", "-c"],
    "/exec/shell": ["bash", "-c"],
}
...
proc = subprocess.run([*argv_prefix, code], capture_output=True, text=True, timeout=timeout_sec, check=False)
```
Adaptation (D-01/D-02/D-03, RESEARCH Pattern 2): add `POST /session/{id}/exec/{lang}`. Python = a long-lived per-session namespace `dict`; `exec(compile(code, "<session>", "exec"), ns)` so `x=42` (call 1) lives in `ns["x"]` (call 2); capture stdout/stderr by redirecting `sys.stdout`/`sys.stderr` to `io.StringIO` around the `exec` (no longer a subprocess). Shell stays `subprocess.run` per call BUT re-applies a per-session API-managed `cwd` (D-02 asymmetric: `cd`/`export` do NOT persist; only the tracked cwd is `subprocess.run(cwd=session_cwd)`). Guard the per-session namespace map with a `threading.Lock` (defense against a 2nd concurrent Aura process; the Go container lock is Aura-local). Keep the existing `_send_json`/`do_GET /healthz`/`MAX_BODY_BYTES`/`MAX_STREAM`/`limit_hit` machinery (sidecar.py:139-169). Map the path with a small router (the current static `INTERPRETERS.get(self.path)` becomes a prefix parse for `/session/{id}/exec/{lang}`). **MUST stay stdlib-only** (NO IPython/Jupyter — D-03 invariant).

---

## Shared Patterns

### Typed sentinel errors (errors.Is taxonomy)
**Source:** `internal/sandbox/errors.go:13-16`
**Apply to:** `sessions.go`, `workspace.go` (new sentinels declared in `errors.go`); consumed by `execute.go` + `cmd/aura/exec.go` + `cmd/aura/sandbox.go` via `errors.Is`.
```go
var (
	ErrSandboxUnreachable = errors.New("sandbox sidecar unreachable (auto-start failed)")
	ErrSandboxProtocol    = errors.New("sandbox sidecar returned a malformed response")
)
```
Add `ErrSessionCapReached`, `ErrWorkspaceQuotaExceeded` to this block (NO silent LRU on cap — return the sentinel). CLI maps to exit codes the way `exec.go:118-123` maps `ErrSandboxUnreachable`→70.

### No-skip-as-green integration tier
**Source:** `internal/sandbox/docker_integration_test.go:1-43` (`//go:build sandbox_integration` + `sidecarURL` helper)
**Apply to:** every `*_integration_test.go` in `internal/sandbox/` (sessions, workspace, network) + the migration round-trip (`db_integration`).
```go
func sidecarURL(t *testing.T) string {
	t.Helper()
	v := os.Getenv("AURA_SANDBOX_URL")
	if v == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("integration test requires AURA_SANDBOX_URL, but it is unset under CI — a skipped integration test must not pass as green ...")
		}
		t.Skip("integration test requires AURA_SANDBOX_URL + a live sidecar; ...")
	}
	return v
}
```
A skipped tier must `t.Fatal` under `$CI`. Copy this helper shape for `AURA_DB_URL` (db tier) and any new env the session/proxy tiers read.

### goleak.VerifyTestMain (mandatory — the 2b reaper goroutine)
**Source:** `internal/sandbox/docker_integration_test.go:25-29`
**Apply to:** the `internal/sandbox` TestMain (untagged + tagged).
```go
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
```
The reaper goroutine MUST exit on a cancelable ctx; `SessionManager.Close()` waits via a done channel so goleak sees no leak (RESEARCH Anti-Patterns: "Letting the reaper leak").

### Deferred-tool partition (CLAUDE.md)
**Source:** `internal/agent/tools/execute.go:46-57` (`Deferred: true`)
**Apply to:** `execute.go` — `session_id` becomes active but `execute` STAYS `Deferred: true`; admin (`aura sandbox sessions`) lives in the CLI, NOT a new deferred tool.

### sqlc one-file-one-query naming
**Source:** `internal/db/queries/conversations.sql` (`:one`/`:exec`/`:many` + explicit-column RETURNING)
**Apply to:** `sandbox_sessions.sql` (exactly 4 queries — anti-god-class).

### os.Root / openat2 symlink guard (supersedes O_NOFOLLOW)
**Source:** the existing `O_NOFOLLOW`/`Lstat` guard in `internal/conversations/orphan_scan.go:36-38` (the pattern to UPGRADE) + RESEARCH Code Example "os.Root no-follow cascade delete".
**Apply to:** `workspace.go` `walkSize` + `PurgeConversationDir`, and the `store.go:449` cascade. `os.Root` has NO `RemoveAll` (golang/go#67002) → manual post-order openat walk; never `os.RemoveAll` on the attacker-controlled `workspace/` subtree (D-14).

### SSRF resolve-then-pin reuse (do NOT duplicate)
**Source:** `internal/web/transport.go:80-90` + `internal/web/ssrf.go:35-107` + `internal/web/dnspin.go`
**Apply to:** `network.go` proxy. **These are UNEXPORTED (landmine #5)** — export a minimal surface (preferred) or extract `internal/netguard`; never copy-paste `classify` (CLAUDE.md NO-DUPLICATE; `dupl` flags it). Re-test the Slice-5 web tier after any export.

### Injectable clock for deterministic time tests
**Source:** `internal/web/dnspin.go:32,42` (`now func() time.Time`)
**Apply to:** `SessionManager` (reaper TTL via `testing/synctest`, PRD line 1720) and `scoring` (none — scoring is timeless). The `now` seam doubles as the synctest hook (RESEARCH A1 fallback if synctest is not GA-stable in 1.26).

### Config knob convention (non-fatal env)
**Source:** `internal/config/config.go:154-156,187-224`
**Apply to:** the 5 new `AURA_SANDBOX_*` / `AURA_RISK_*` vars — `envDefault`/`envIntDefault`, AURA_<DOMAIN>_<UNIT>, fall back on typo (never boot-fatal). `scoring`/`dnspin` take the threshold/TTL as a constructor arg; config owns the env read.

### Hand-rolled CLI switch dispatcher (no cobra)
**Source:** `cmd/aura/db.go:19-42` (nested switch) + `cmd/aura/exec.go:43-79` (`parseExecArgs` flag latch)
**Apply to:** new `cmd/aura/sandbox.go` `runSandbox` + the `aura exec --session` activation.

---

## No Analog Found

| File | Role | Data Flow | Reason |
|------|------|-----------|--------|
| (none) | — | — | Every new/modified file has at least a role-match analog in the shipped tree. The two genuinely novel mechanisms — the SessionManager control plane (reaper + hard cap) and the CONNECT forward proxy — are COMPOSED from existing analogs (`docker.go` shellout + `dnspin.go` guarded-map/clock; `transport.go` dial-gate + `ssrf.go` classify), not invented. RESEARCH confirms: "Every 'hard' part of 2b already has a battle-tested answer in either the Go 1.26 stdlib (`os.Root`, `synctest`) or the shipped Slice-5 SSRF code. The phase's risk is in WIRING (the landmines), not in novel algorithms." |

## Landmines the Planner MUST Resolve (from RESEARCH, cross-referenced to files)

| # | Landmine | Affected file(s) | Resolution |
|---|----------|------------------|------------|
| 1 | FK type: PRD says `conversation_id text`; PK is `uuid` | `0008_*.up.sql`, `sandbox_sessions.sql`, `sessions.go` | declare `conversation_id uuid ... REFERENCES aura.conversations(id) ON DELETE CASCADE`; parse string→uuid at SessionManager boundary (parseUUID idiom) |
| 2 | Migration number: PRD says 0010; next free is 0008 | `0008_*.{up,down}.sql` | name it 0008; amend PRD |
| 3 (HIGHEST) | 2a seccomp DENIES `connect(2)` + non-masquerading egressless bridge — breaks the proxy route | `compose.yaml`, session-container `docker run` argv, `network.go`, `08-SECURITY.md` | session containers need a profile that ALLOWS connect (egress contained host-side); verify proxy reachable at bridge gateway (NOT 127.0.0.1); empty allowlist keeps 2a egressless posture; document deviation extending AR-05-01 |
| 4 | Workspace dir co-tenant with `.result` spillover; `os.RemoveAll` forbidden for workspace | `workspace.go`, `conversations/store.go:449` | `WorkspaceManager.PurgeConversationDir(convID)` os.Root walk of `<id>/`; wire via interface to avoid sandbox→conversations import cycle |
| 5 | Slice-5 SSRF guard is UNEXPORTED | `internal/web/*`, `network.go` | export minimal surface (preferred) or extract `internal/netguard`; re-test web tier; NO copy-paste |
| 6 | gVisor `runsc` not inherited by ad-hoc `docker run` | `sessions.go` | pass `--runtime=<cfg.SandboxRuntime>` + ALL 2a hardening flags in argv |

## Metadata

**Analog search scope:** `internal/sandbox/`, `internal/web/`, `internal/conversations/`, `internal/config/`, `internal/agent/tools/`, `internal/db/{queries,migrations}/`, `internal/llm/`, `cmd/aura/`, `sandbox/sidecar.py`
**Files scanned (read in full or targeted):** docker.go, sandbox.go, errors.go, ssrf.go, dnspin.go, transport.go, result.go, execute.go, store.go (Delete + Store), orphan_scan.go (head), config.go (struct + loader + helpers), conversations.sql (head), 0005_conversations.up.sql, 0007_cache_metrics.down.sql, exec.go, db.go (head), sidecar.py (run_code + handler), ssrf_test.go (head), docker_integration_test.go (head); migrations dir listing (confirms next free = 0008)
**Pattern extraction date:** 2026-06-03
