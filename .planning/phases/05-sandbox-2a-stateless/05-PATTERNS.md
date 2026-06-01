# Phase 5: Sandbox 2a Stateless - Pattern Map

**Mapped:** 2026-06-01
**Files analyzed:** 11 (5 Go new/modified, 5 sidecar/infra new, 1 bench script)
**Analogs found:** 9 / 11 (2 infra files have weak-to-no in-repo analog)

All analogs read read-only. The Go integration surface is small and almost entirely
copy-from-existing; the sidecar/seccomp/gVisor artifacts are net-new to the repo
(planner falls back to RESEARCH.md published primitives for those).

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/sandbox/docker.go` (NEW ~220) | service / runner | request-response (HTTP) | `internal/llm/openai_compat/client.go` | role+flow exact (non-stream variant) |
| `internal/sandbox/errors.go` (NEW ~20) | utility | — | `internal/askuser/store.go` (sentinel vars) + `internal/llm/openai_compat/httperror.go` (struct error) | exact |
| `internal/sandbox/sandbox.go` (MODIFY) | model / interface | request-response | self (interface already defined; `Stub` removed) | self |
| `internal/sandbox/config.go` (NEW ~50, optional) | config | — | `internal/config/config.go` env helpers + `internal/llm/config.go` per-pkg config | role-match |
| `internal/agent/tools/execute.go` (NEW ~140) | tool / controller | request-response (delegates to Runner) | `internal/agent/tools/ask_user.go` + `current_time.go` (deferred + Execute shape) + `result.go` `NewResult` | role-match (no existing tool delegates to a sub-service yet) |
| `cmd/aura/main.go` (MODIFY ~+60) | route / CLI dispatcher | request-response | self (switch + `buildRegistry`) | self |
| `cmd/aura/exec_test.go` (NEW) | test | request-response | `cmd/aura/agent_test.go` (re-exec subprocess exit-code) | exact |
| `internal/config/config.go` (MODIFY) | config | — | self (`envDefault`/`envIntDefault` + `Config` struct) | self |
| `sandbox/Dockerfile` (NEW) | config / infra | — | none in repo (no Dockerfile exists) | NO ANALOG |
| `sandbox/sidecar.py` (NEW ~150) | service (non-Go) | request-response (HTTP server) | none in repo (no Python) | NO ANALOG |
| `sandbox/seccomp.json` (NEW ~80) | config / infra | — | none in repo | NO ANALOG (moby default.json baseline — RESEARCH) |
| `compose.yaml` (MODIFY) + `compose.gvisor.yaml` (NEW) | config / infra | — | `compose.yaml` existing services | role-match |
| `scripts/sandbox_escape_bench.sh` (NEW) | test / bench | batch | `scripts/llm_smoke.sh` (style/header/skip-discipline) | style-match |

## Pattern Assignments

### `internal/sandbox/docker.go` (service/runner, request-response)

**Analog:** `internal/llm/openai_compat/client.go` (read in full — the proven HTTP-client shape this file must copy)

**Imports pattern** (client.go lines 3-12) — stdlib `net/http`/`net`/`time` + `encoding/json` + `bytes`/`context`:
```go
import (
    "bytes"
    "context"
    "encoding/json"
    "net"
    "net/http"
    "time"
    // + "os/exec" (auto-start D-09), "errors", "fmt" for sentinels
)
```

**HTTP client construction — COPY EXACTLY** (client.go lines 36-48). The load-bearing
constraints (carried verbatim in the doc-comment): NO `http.Client.Timeout` (it counts
body-read and would abort mid-call — the total timeout rides the request ctx);
`DisableKeepAlives: true` keeps goleak order-independent. The connect timeout lives on
the dialer only:
```go
httpClient: &http.Client{
    Transport: &http.Transport{
        DialContext: (&net.Dialer{
            Timeout: time.Duration(cfg.ConnectTimeoutSec) * time.Second,
        }).DialContext,
        DisableKeepAlives: true,
    },
},
```

**Request build + Do + non-2xx handling** (client.go lines 79-106) — `http.NewRequestWithContext`
(POST), `Content-Type: application/json`, `c.httpClient.Do(httpReq)`, then on non-2xx
build a typed error and `resp.Body.Close()`. For docker.go this is the per-call shape:
```go
httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
    c.url+"/exec/"+lang, bytes.NewReader(body))
// ... headers ...
resp, err := c.httpClient.Do(httpReq)   // transport err → wrap ErrSandboxUnreachable after auto-start
if resp.StatusCode/100 != 2 { /* ErrSandboxProtocol */ }
```

**Per-call timeout (D-19/D-16)** — derive `ctx` with `context.WithTimeout(ctx, timeout)`
where `timeout = min(timeout_sec, 600s)` (cap enforced runner-side per
`config.go` comment line 349). NOT a `Client.Timeout`.

**Interface to satisfy** (`sandbox.go` lines 13-23) — `DockerRunner` implements:
```go
RunPython(ctx context.Context, code string) (Result, error)
RunShell(ctx context.Context, cmd string) (Result, error)
```
Add `var _ Runner = (*DockerRunner)(nil)` (mirrors client.go line 28 `var _ llm.Client = (*Client)(nil)`).

**Wire→Result mapping (D-16):** decode `{stdout,stderr,exit_code,elapsed_ms,truncated,limit_hit}`
into `Result{Stdout,Stderr,ExitCode,ElapsedMs}` 1:1 (existing struct, sandbox.go lines 18-23);
carry `truncated`/`limit_hit` through to the preview formatter (these have no `Result` field —
return them alongside, or extend `Result`; planner's call). A non-zero `exit_code` is a normal
`Result`, NEVER a Go error (D-18).

**Auto-start (D-09), runner-private helper:** on transport failure, shell `exec.Command("docker","compose","up","-d","aura-sandbox")` ONCE, health-check, retry the POST once; still failing → `ErrSandboxUnreachable`. Gate on `docker` being on PATH (RESEARCH OQ3 — never mount the socket).

---

### `internal/sandbox/errors.go` (utility, sentinels)

**Analog:** `internal/askuser/store.go` lines 49-50 (bare `errors.New` sentinels) — this is the
shape RESEARCH already templated (RESEARCH §Code Examples lines 354-360):
```go
var (
    ErrPauseNotFound = errors.New("paused state not found or already resumed")
    ErrInvalidAnswer = errors.New("invalid resume answer")
)
```
**New file replicates that exactly (D-18):**
```go
var (
    ErrSandboxUnreachable = errors.New("sandbox sidecar unreachable (auto-start failed)")
    ErrSandboxProtocol    = errors.New("sandbox sidecar returned a malformed response")
)
```
These propagate as a tool-execution error to the loop AND surface on `aura exec` as exit 70.
A struct-error variant (if a payload is needed) follows `internal/llm/openai_compat/httperror.go`
(`type HTTPError struct` + `Error()` + a `newHTTPError(resp)` constructor) and `ask_user.go`
lines 67-77 (`ErrAwaitingUserInput` struct + `errors.As`-friendly). For 2a the bare-sentinel
form is sufficient; wrap with `fmt.Errorf("...: %w", ErrSandboxUnreachable)` to keep `errors.Is`.

---

### `internal/sandbox/sandbox.go` (model/interface, MODIFY)

**Analog:** self. The `Runner` interface (lines 13-16) and `Result` struct (lines 18-23) are
ALREADY defined and forward-stable — DO NOT change their signatures (the `execute` tool and
the agent loop bind to them). The ONLY change is **removing `Stub`** (lines 25-36) once the real
runner is wired. Before deleting, grep `sandbox.Stub` usages (RESEARCH Runtime State Inventory:
the agent-loop smoke test referenced it) — currently `grep` shows zero live `.go` usages outside
sandbox.go itself, so removal is clean, but re-verify at plan time. Update the package doc-comment
(lines 1-3) which still says "First version is a stub".

---

### `internal/agent/tools/execute.go` (deferred tool, request-response → Runner)

**Analog (deferred + Execute + JSON-schema):** `internal/agent/tools/ask_user.go` (struct tool,
inline JSON-schema `Parameters`, enum validation, `Spec()`+`Execute()`); `current_time.go` is the
minimal template. **Analog (result shaping):** `result.go` `NewResult`.

**Tool struct shape** (mirror `ToolSearch` in search.go lines 18-20 — a tool that holds an injected
dependency):
```go
type Execute struct {
    Runner sandbox.Runner   // injected in buildRegistry; like ToolSearch{Registry}
}
```

**Spec() — MUST set `Deferred: true`** (CLAUDE.md mandatory deferred-tool partition; `execute`
has a long description + enum schema + safety examples). Copy the inline `json.RawMessage` schema
shape from `ask_user.go` lines 88-97 (enum on `kind` → here enum on `lang ∈ {python,shell}`,
plus `code` string + optional `timeout_sec` integer + reserved-inert `session_id`):
```go
return Spec{
    Name:        "execute",
    Summary:     "Run a Python or shell snippet in an isolated sandbox.",
    Description: "...long, with safety notes + examples...",
    Parameters:  params,   // {lang enum, code, timeout_sec?, session_id? reserved}
    Deferred:    true,      // ← unlike every existing builtin which is Deferred:false
}
```
Note: every EXISTING tool sets `Deferred:false`; `execute` is the **first `Deferred:true`** tool
shipped, so it is the reference template for the partition — get this right.

**Execute() body** — validate `lang` enum (copy the `switch a.Kind { ... default: error }` pattern
from ask_user.go lines 122-126), clamp `timeout_sec` ≤600, reject inert `session_id` with a
"Phase 8 / Slice 2b" message (D-19), delegate to `e.Runner.RunPython`/`RunShell`, then build the
**lean preview (D-17)** and route the WHOLE string through `NewResult`:
```go
res, err := e.Runner.RunPython(ctx, a.Code)   // or RunShell
if err != nil { return ToolResult{}, err }     // typed sandbox err → loop (D-18)
preview := formatLean(res)                      // D-17: stdout verbatim; stderr: only if non-empty;
                                                //       exit_code: N only if non-zero; [limit: …] only if hit;
                                                //       "(no output, exit 0)" when empty
return NewResult(ctx, preview)                  // D-25 cap + sidecar spillover, UNCHANGED
```

**Critical:** route through `tools.NewResult(ctx, s)` (result.go lines 94-133) — DO NOT
re-implement truncation/spillover (search.go lines 58/68 show the exact reuse: a tool builds
a string then hands it to `NewResult`). `NewResult` reads the tool-call ctx the agent injected
via `WithToolCallContext`; `execute` writes ZERO spillover logic.

---

### `cmd/aura/main.go` (CLI dispatcher, MODIFY)

**Analog:** self. Two edits, both copy-existing-shape:

1. **New `case "exec":`** in the `switch os.Args[1]` (main.go lines 32-56), hand-rolled — NOT
   cobra (D-19, repo convention). Mirror the existing `case "agent": runAgent(os.Args[2:])`
   delegation. Parse `--session` manually (reserved-inert), `lang` positional ∈ {python,shell},
   `code` positional or `-`→stdin. Exit with the sandbox `exit_code`; `ErrSandboxUnreachable` → exit 70.
   Add `exec` to `usage()` (line 60) and the package doc-comment subcommand list (lines 1-17).

2. **`buildRegistry()` registration** (lines 63-71) — append exactly like the existing
   `reg.Register(...)` lines:
   ```go
   reg.Register(&tools.Execute{Runner: dockerRunner})   // dockerRunner from sandbox.NewDockerRunner(cfg)
   ```
   The runner is constructed from `config.Load()` sandbox fields (CONTEXT Integration Points).
   `Execute` is a pointer-receiver tool (holds the Runner) like `&tools.ToolSearch{...}` and
   `&tools.ReadToolOutput{}` already registered here.

---

### `cmd/aura/exec_test.go` (test, exit-code)

**Analog:** `cmd/aura/agent_test.go` lines 212-236 (`TestRunAgent_ExitPaths`) — the **re-exec
subprocess pattern** for asserting `os.Exit(N)` without killing the test process. Copy it
verbatim for the exit-70 path (D-19):
```go
if os.Getenv("AURA_TEST_RUNEXEC_EXIT") != "" {
    runExec(strings.Split(os.Getenv("AURA_TEST_RUNEXEC_ARGS"), "|"))
    return
}
cmd := exec.Command(os.Args[0], "-test.run", "TestRunExec_Exit70") //nolint:gosec // re-exec self
cmd.Env = append(os.Environ(), "AURA_TEST_RUNEXEC_EXIT=1", "AURA_TEST_RUNEXEC_ARGS="+args)
err := cmd.Run()   // assert a non-zero / exit-70 ProcessState
```
For the precise exit code, inspect `exec.ExitError.ExitCode()`. The arg-parse unit tests
mirror `TestParseDryRunArgs_*` (agent_test.go lines 116-143).

---

### `internal/config/config.go` (config, MODIFY)

**Analog:** self. Add three `AURA_SANDBOX_*` fields to the `Config` struct (lines 32-47) and
populate them in `loadBase()`'s returned literal (lines 105-131) using the EXISTING
`envDefault`/`envIntDefault` helpers (lines 151-172). RESEARCH §Code Examples lines 346-351
already templated this against the convention:
```go
SandboxURL:        envDefault("AURA_SANDBOX_URL", "http://127.0.0.1:18901"),
SandboxTimeoutSec: envIntDefault("AURA_SANDBOX_TIMEOUT_SEC", 30), // 600 cap enforced runner-side
SandboxRuntime:    envDefault("AURA_SANDBOX_RUNTIME", defaultRuntimeForArch()), // runsc x86 / runc arm64
```
`defaultRuntimeForArch()` is a new helper alongside `defaultRunDir()` (lines 176-181) — reads
`runtime.GOARCH` (amd64→runsc, arm64→runc per D-07). `envIntDefault` silently absorbs parse
failures (comment lines 158-161) — the 600 cap is NOT enforced here; it is clamped in the runner.

> Alternatively the sandbox config can live in `internal/sandbox/config.go` (per-package config,
> mirroring `internal/llm/config.go` which owns its own `Load()` — RESEARCH project structure
> lines 205). Either placement reuses the same `envDefault`/`envIntDefault` idiom. Planner picks;
> the root-`config.go` placement matches the doc-comment rule "no new fields without an owning
> slice plan" (lines 8) so the sandbox plan owns these.

---

### `sandbox/sidecar.py` + `sandbox/Dockerfile` + `sandbox/seccomp.json` (NO in-repo Go analog)

These are **net-new artifact types** (no Python, no Dockerfile, no seccomp profile exist in the
repo). Planner uses RESEARCH directly:
- `sidecar.py` — stdlib `http.server` BaseHTTPRequestHandler + `subprocess.run([interp,"-c",code],
  timeout=…, capture_output=True)`; RESEARCH Pattern 2 (lines 244-262) gives the exact shape incl.
  `TimeoutExpired`→`limit_hit:"timeout"`, `rc==137`→`"oom"`, 1 MiB per-stream truncation.
- `seccomp.json` — fetch moby `profiles/seccomp/default.json` at plan time and SUBTRACT the
  hard-exclude set (`ptrace,unshare,process_vm_readv,bpf,kexec_load,userfaultfd,mount` + net sockets);
  `defaultAction: SCMP_ACT_ERRNO`, `architectures: ["SCMP_ARCH_X86_64","SCMP_ARCH_AARCH64"]`
  (RESEARCH lines 362-374, D-10/D-11). NEVER hand-author from scratch; NEVER hardcode syscall numbers.
- `Dockerfile` — `python:3.12-slim` (pin digest) + apt `bash`/`coreutils`, non-root uid 65532,
  no pip (RESEARCH Standard Stack).

### `compose.yaml` (MODIFY) + `compose.gvisor.yaml` (NEW)

**Analog:** existing `compose.yaml` services (postgres/neo4j/aura-llama-embed). Replicate the
conventions: `name: aura` already set; `container_name: aura-sandbox`; loopback-only port
`"127.0.0.1:18901:18901"` (matches the existing `"127.0.0.1:7474:7474"` / `8081` convention,
compose.yaml lines 46-48, 76-77); a `healthcheck:` block (copy the CMD-SHELL+interval+retries
shape from the embed service lines 80-85). The hardening fields (`cap_drop`, `read_only`, `tmpfs`,
`network_mode: none`, `security_opt`, `pids_limit`, `mem_limit`, `cpus`, `ulimits`, `user: "65532:65532"`)
are net-new — RESEARCH §compose example (lines 376-398) is authoritative. `userns-remap` is a
`daemon.json` setting, NOT a compose field (D-15). `compose.gvisor.yaml` overlay flips
`runtime: runsc` (x86-only, D-04).

### `scripts/sandbox_escape_bench.sh` (NEW, bench)

**Analog (style only):** `scripts/llm_smoke.sh` — copy the header discipline: `#!/usr/bin/env bash`,
`set -euo pipefail`, `cd "$(git rev-parse --show-toplevel)"`, a WHY-block comment explaining the
no-skip-as-green stance, and an explicit skip notice when a prerequisite is absent. Unlike
`llm_smoke.sh` (manual-only), this bench is **CI-GATING** (D-13) — so it must emit a deterministic
escape-rate, run the 18-scenario port with an auditable per-scenario table (N/A lines for the
inapplicable K8s scenarios — RESEARCH Pitfall 6 / OQ1), and write the escape-rate to
`docs/aura-quality-snapshot.md`. Content is net-new (no escape-probe script exists); only the
bash scaffolding/discipline copies from `llm_smoke.sh`.

## Shared Patterns

### HTTP client lifecycle (goleak-safe)
**Source:** `internal/llm/openai_compat/client.go` lines 36-48 (`New`) + the doc-comment lines 30-35.
**Apply to:** `internal/sandbox/docker.go`.
```go
&http.Client{Transport: &http.Transport{
    DialContext: (&net.Dialer{Timeout: connectTimeout}).DialContext,
    DisableKeepAlives: true,   // goleak order-independent
}}
// NO Client.Timeout — total timeout rides the request ctx (context.WithTimeout per call).
```

### Typed sentinel errors (errors.Is-friendly)
**Source:** `internal/askuser/store.go` lines 49-50 (bare `errors.New`); `internal/llm/openai_compat/httperror.go` (struct-error variant + constructor); `internal/agent/tools/ask_user.go` lines 67-77 (`errors.As` payload struct).
**Apply to:** `internal/sandbox/errors.go` (+ `aura exec` exit-70 mapping in `cmd/aura/main.go`).
Rule (D-18): environment fault → typed Go error; code fault (non-zero exit, EPERM, timeout, OOM) → `ToolResult`, never an error.

### Deferred-tool registration + on-demand spec load
**Source:** `internal/agent/tools/spec.go` lines 1-25 (the `Deferred` contract) + `search.go` (`tool_search` loads deferred specs) + `cmd/aura/main.go` `buildRegistry` lines 63-71.
**Apply to:** `internal/agent/tools/execute.go` (`Deferred: true`) + its `buildRegistry` registration.
`execute` is the first `Deferred:true` tool in the repo — the manifest shows only Name+Summary until `tool_search` fetches the full schema.

### Shared output cap → preview → sidecar spillover
**Source:** `internal/agent/tools/result.go` `NewResult` lines 94-133; reuse site in `search.go` lines 58/68.
**Apply to:** `internal/agent/tools/execute.go` — build the lean preview string, hand the WHOLE string to `NewResult(ctx, s)`. Zero new truncation/spillover code (D-17/D-25).

### Hand-rolled CLI subcommand + re-exec exit-code test
**Source:** `cmd/aura/main.go` switch (lines 32-57) + `cmd/aura/agent_test.go` `TestRunAgent_ExitPaths` (lines 212-236) + `TestParseDryRunArgs_*` (lines 116-143).
**Apply to:** the new `exec` case + `cmd/aura/exec_test.go`. No cobra (D-19, CLAUDE.md).

### Env-var config via envDefault/envIntDefault
**Source:** `internal/config/config.go` lines 105-172 (`loadBase` literal + the two helpers + `defaultRunDir`).
**Apply to:** the new `AURA_SANDBOX_URL` / `AURA_SANDBOX_TIMEOUT_SEC` / `AURA_SANDBOX_RUNTIME` fields (+ a `defaultRuntimeForArch()` helper alongside `defaultRunDir`).

### Loopback-only compose service + healthcheck
**Source:** `compose.yaml` lines 9-85 (port `"127.0.0.1:PORT:PORT"` convention; CMD-SHELL healthcheck block).
**Apply to:** the new `aura-sandbox` service + `compose.gvisor.yaml` overlay.

### Bash script header/skip discipline
**Source:** `scripts/llm_smoke.sh` (shebang, `set -euo pipefail`, git-root cd, WHY/no-skip-as-green comment, explicit skip notice).
**Apply to:** `scripts/sandbox_escape_bench.sh` (but CI-gating, not manual — emits a deterministic escape-rate).

## No Analog Found

| File | Role | Data Flow | Reason |
|------|------|-----------|--------|
| `sandbox/sidecar.py` | service (Python) | request-response | No Python in the repo; stdlib `http.server`+`subprocess` — use RESEARCH Pattern 2 (lines 244-262). |
| `sandbox/Dockerfile` | infra | — | No Dockerfile exists in the repo; use RESEARCH Standard Stack (`python:3.12-slim`, non-root, no pip). |
| `sandbox/seccomp.json` | infra/config | — | No seccomp profile exists; baseline = moby `default.json` minus the dangerous set (RESEARCH lines 362-374, D-10/D-11). |

(The hardening compose fields + the gVisor overlay are partial-analog: the service skeleton copies
`compose.yaml`, but every security field is net-new from RESEARCH lines 376-398.)

## Metadata

**Analog search scope:** `internal/sandbox/`, `internal/llm/openai_compat/`, `internal/agent/tools/`, `internal/config/`, `internal/askuser/`, `cmd/aura/`, `scripts/`, repo-root `compose.yaml`.
**Files scanned (read):** `client.go`, `sandbox.go`, `result.go`, `spec.go`, `read_tool_output.go`, `ask_user.go`, `search.go`, `current_time.go`, `config.go`, `main.go`, `agent_test.go`, `compose.yaml`, `scripts/llm_smoke.sh` (head) + grep of `httperror.go`/`store.go`/`llm/config.go` sentinel & config-field locations.
**Pattern extraction date:** 2026-06-01

## PATTERN MAPPING COMPLETE

**Phase:** 5 - Sandbox 2a Stateless
**Files classified:** 11 (+2 split: errors.go, exec_test.go, compose.gvisor.yaml)
**Analogs found:** 9 / 11

### Coverage
- Files with exact / self analog: 6 (`docker.go`←client.go, `errors.go`←askuser/store.go, `sandbox.go`←self, `exec_test.go`←agent_test.go, `main.go`←self, `config.go`←self)
- Files with role-match analog: 2 (`execute.go`←ask_user.go+result.go, `compose.yaml`/`compose.gvisor.yaml`←compose.yaml)
- Files with style-only analog: 1 (`sandbox_escape_bench.sh`←llm_smoke.sh)
- Files with no analog: 3 (`sidecar.py`, `Dockerfile`, `seccomp.json` — RESEARCH primitives)

### Key Patterns Identified
- **`docker.go` is a near-verbatim copy of the `openai_compat` HTTP-client shape** (dialer connect-timeout + `DisableKeepAlives`, NO `Client.Timeout`, ctx-rides-timeout, `var _ Runner` assertion) — the single highest-leverage analog; non-zero exit is a `Result`, never a Go error (D-18).
- **`execute` is the repo's FIRST `Deferred:true` tool** — registers like the existing `reg.Register` lines in `buildRegistry`, validates a `lang` enum the way `ask_user` validates `kind`, and routes its lean preview through the already-shipped `tools.NewResult` (zero new spillover code).
- **CLI + sentinel + config all copy self/sibling idioms** — hand-rolled `switch` case (no cobra), bare `errors.New` sentinels mapped to exit 70, `envDefault`/`envIntDefault` for the three `AURA_SANDBOX_*` vars; the re-exec subprocess test from `agent_test.go` covers the exit-code path.
- **Sidecar/seccomp/gVisor artifacts have no in-repo precedent** — planner pulls them straight from RESEARCH published primitives (moby default.json minus dangerous set; stdlib `http.server` subprocess sidecar; `python:3.12-slim`); the compose service skeleton + loopback port copy `compose.yaml`.

### File Created
`/home/user/Aura/.planning/phases/05-sandbox-2a-stateless/05-PATTERNS.md`

### Ready for Planning
Pattern mapping complete. Planner can reference each analog (file + line range + concrete excerpt) directly in PLAN.md action sections. Note: the two PRD-amendment commits (D-05, D-09) remain hard prerequisites before any Phase-5 code commit (CONTEXT § Required PRD Amendments).
