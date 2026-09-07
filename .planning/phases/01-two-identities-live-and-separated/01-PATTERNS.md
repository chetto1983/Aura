# Phase 1: Two Identities, Live and Separated - Pattern Map

**Mapped:** 2026-09-07
**Files analyzed:** 15 target files/areas (from CONTEXT.md D-01..D-18 and RESEARCH.md)
**Analogs found:** 13 / 15 (2 genuinely-new-shape items flagged below)

All analogs below were confirmed tracked (`git ls-files`) and re-read this session; line
numbers cite the state as of 2026-09-07 and may drift slightly by execution time — re-grep
before quoting verbatim in a PLAN.md.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|---|---|---|---|---|
| `internal/config/*` (D-03 sandbox-image boot preflight) | config/boot-gate | request-response (Fatal/INFO at boot) | `internal/config/config_validate.go` `gateMultiUserRequiresStrictProfile` (lines ~108-129) — but as a **separate boot step**, not inside `ValidateProfile` | role-match, shape differs (needs Docker I/O) |
| `cmd/aura/serve_sandbox_preflight.go` (new file, D-03) | controller/boot-step | request-response | `cmd/aura/serve_sandbox_readiness.go:31-58` (`router.CheckRuntime`, an `ImageInspect`, already exists — closest kin) | role-match, near-exact shape |
| `internal/agui/onboarding_provision_resources.go` — add `SandboxProvisioner` leg (D-09) | service/saga-leg | CRUD (eager, idempotent, compensated) | Same file, `MemoryProvisioner`/`ProvisionMemory` leg (lines 44-106) | exact |
| `internal/agui/deprovision.go` (compensation mirror, already exists — confirm wiring only) | service | CRUD | `cmd/aura/serve_provisioning.go:296-313` `sandboxPurgeAdapter`/`sandboxPurgerFor` | exact (already built — no new code, just the provisioning-side adapter) |
| `cmd/aura/identity.go` (new, D-07 `aura identity create`) | CLI verb / controller | request-response | `cmd/aura/recover_operator.go` (138 LOC) — `identityRecoverOperator` switch-tree entry + `flag.NewFlagSet` parsing | exact (same package, same std-flag convention, same switch-tree wiring) |
| `cmd/aura/two_identity_e2e_test.go` (add memory plane, 5th tag) | test (integration, `musr_e2e`) | CRUD / cross-deny assertions | Same file's existing `TestTwoIdentityCrossDeny` subtests (Postgres/Garage planes) | exact — same file, same `t.Run` tree |
| `cmd/aura/two_identity_e2e_harness_test.go` (add arcadedb tenant helpers) | test helper | CRUD | Same file's `musrProvisionIdentity`/`musrGarageIdentity` helpers (lines 42-90+) | exact |
| new concurrent-runner test (D-14, ISO-05) — file TBD, likely `internal/agent/two_identity_concurrent_test.go` or `internal/runner/` | test (unit, `-race`+goleak) | event-driven / concurrent | `internal/arcadedb/concurrent_fact_write_test.go` (goleak + N real goroutines vs sequential loop) | role-match (best available concurrent-goroutine shape in repo) |
| `scripts/lib/disposable_stack.sh` (new, D-12, extracted) | shell utility | file-I/O / batch | `scripts/coverage_docker.sh:13-70+` (the exact block to extract) | exact — literal extraction target |
| `scripts/musr_e2e.sh` (new, D-13) | shell orchestration script | batch | `scripts/agui_smoke.sh` (build→background-start→poll `/healthz`→login→assert→teardown) for process lifecycle; `scripts/coverage_docker.sh` for disposable-DB bring-up | exact (two analogs, each for half the script) |
| `Makefile` `musr-e2e` target (new) | config/build | batch | `Makefile:275-279` `memory-up-core` (dodges the `arcadedb-mcp depends_on: aura` daemon race) + `Makefile:89-98` `coverage-docker` target wiring `coverage_docker.sh` | exact |
| `docs/runbooks/musr-rollout.md` (update Acceptance section) | docs | — | same file, existing Acceptance section | exact |
| `.env.example` / `scripts/install.sh` heredoc (D-01, D-04) | config | — | `scripts/install.sh:511-538` `ensure_edge_channel_env` (the existing conditional-default pattern for `AURA_SANDBOX_IMAGE`) | role-match |
| D-16 closing-run harness (new, likely `scripts/two_identity_live_run.sh` or similar + a small Go driver) | integration harness | request-response (SSE) | `scripts/agui_smoke.sh:129-330` (build/background-start/poll/CSRF-login/SSE-assert) — **ported, not extended in place** | role-match; genuinely new composition (see No Analog below) |
| D-11 item 3: `arcadedb-mcp` OAuth `sub`-keyed test call | test | request-response | `cmd/arcadedb-mcp/identity.go:15-24` `identityFromToken` (production code to exercise, not to copy) + repo's 34 `arcadedb_integration`-tagged test files for calling convention | partial — no existing test already does a two-identity cross-deny call through the MCP boundary (per RESEARCH Open Question 3); grep those 34 files before assuming greenfield |

## Pattern Assignments

### `internal/agui/onboarding_provision_resources.go` — new `SandboxProvisioner` leg (D-09)

**Analog:** same file, `MemoryProvisioner` interface + its leg inside `provisionResourceLegs` (140 LOC file currently — comfortably under 600, no split needed for one more leg + one more interface).

**Interface pattern** (lines 13-20):
```go
type MemoryProvisioner interface {
	MemoryPurger
	ProvisionMemory(ctx context.Context, identityID string) error
}
```
New `SandboxProvisioner` should mirror this: embed a `SandboxPurger` (destroy side, already
exists per `agui.SandboxPurger` referenced by `cmd/aura/serve_provisioning.go:308`) plus a
`ProvisionSandbox(ctx, identityID) error`.

**Core eager/idempotent/compensated leg pattern** (lines 72-81):
```go
if s.memory != nil {
    if err := run.step(ctx, sagaStepMemory, func(ctx context.Context) error {
        return s.memory.ProvisionMemory(ctx, identityID)
    }); err != nil {
        if derr := s.memory.PurgeMemory(context.WithoutCancel(ctx), identityID); derr != nil {
            slog.Error("onboarding: COMP memory after provision failure failed", "step", "compensate")
        }
        return compResources, provisionFail("memory provision", err)
    }
}
```
The new sandbox leg wraps `SandboxRouter.Resolve`/`Route` (create) the same way, adds
itself to `compResources`'s closure (reverse order, `context.WithoutCancel`), and needs a
new `sagaStepSandbox` constant alongside `sagaStepMemory`/`sagaStepGarage`/`sagaStepFilesystem`.

**Compensation-closure pattern** (lines 50-64): each optional port gets a `if s.X != nil`
block inside `compResources`, called in reverse provisioning order (filesystem → objectStore
→ memory today; sandbox should slot in wherever its provisioning position lands).

---

### `cmd/aura/serve_provisioning.go` — the already-built compensation half (D-09, confirm/wire only)

**Analog:** the file itself, `sandboxPurgeAdapter` (lines 296-313):
```go
type sandboxPurgeAdapter struct{ router *usersandbox.SandboxRouter }

func (a sandboxPurgeAdapter) DestroySandbox(ctx context.Context, identityID string) error {
	return a.router.Destroy(ctx, identityID)
}

func sandboxPurgerFor(router *usersandbox.SandboxRouter) agui.SandboxPurger {
	if router == nil {
		return nil
	}
	return sandboxPurgeAdapter{router: router}
}
```
The new **provisioning-side** adapter (`sandboxProvisionAdapter` or similar, wrapping
`router.Route`/`Resolve`) is the missing symmetric half — same file, same shape, same
composition-root wiring point where `sandboxPurgerFor` is currently called into
`buildDeprovisioner`.

---

### `internal/sandbox/usersandbox/router.go` — `Route`/`Destroy` (the create/destroy pair the new leg wraps)

**Analog:** the file itself. Route (get-or-create seam every tool call passes through,
lines ~79-94):
```go
func (r *SandboxRouter) Route(ctx context.Context) (BoxHandle, error) {
	if r == nil || r.backend == nil {
		return BoxHandle{}, errBackendUnavailable
	}
	id := r.identityID(ctx)
	h, err := r.backend.Resolve(ctx, r.specFor(id))
	if err != nil {
		return BoxHandle{}, err // fail-CLOSED (D-09/GATE-01)
	}
	r.mu.Lock()
	r.lastUsed[id] = r.clock()
	r.handles[id] = h
	r.mu.Unlock()
	return h, nil
}
```
`Destroy(ctx, identityID)` is at line 152. **Note:** RESEARCH.md cited `Resolve`/`Destroy` at
lines 79-99/152-167; as read this session `Route` (not a bare `Resolve` call) is the public
seam at line ~79 — `Route` internally calls `r.backend.Resolve`. The new provisioning leg
should call the same seam a tool call would use (`Route`, keyed by `identityID` via context,
or a lower-level call if eager provisioning needs to force a specific identity not yet in
`identityctx`) — verify which is right against the saga's call context before writing the
adapter.

**identityID resolution pattern** (lines 96-103): falls back to seeded `local` identity
(`00000000-0000-0000-0000-000000000001`) when `identityctx.IdentityID(ctx)` is empty — the
provisioning leg will have an explicit `identityID string` parameter already, so it likely
does NOT go through this fallback path; confirm which entry point the saga leg should call
directly rather than through the context-derived `Route`.

---

### `cmd/aura/identity.go` (new, D-07 `aura identity create`)

**Analog:** `cmd/aura/recover_operator.go` (138 LOC), for CLI verb shape, flag parsing, and
switch-tree wiring convention (also see D-07's own citation of
`internal/agui/onboarding_api.go:199`'s `StartSession`→`Provision` pair as the exact
service calls to make — that is the "what to call," this is the "how to wire a CLI verb").

**Imports pattern** (lines 20-30):
```go
import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/term"

	"github.com/chetto1983/aura/internal/breakglass"
	"github.com/chetto1983/aura/internal/config"
)
```
`aura identity create` will instead import `internal/agui` for `onboardingService` — same
`flag`/`os`/`fmt` std-lib shape, no cobra (repo has no `spf13/cobra` dependency; the file's
own header comment states this explicitly: "Flags are parsed with a std flag.FlagSet, NOT
cobra: go.mod has no spf13/cobra and CLAUDE.md mandates following the existing
runIdentity/runDB switch-tree pattern").

**Flag-parsing pattern** (lines 118-132):
```go
func parseRecoverOperatorFlags(args []string) (generate, noRecovery bool, err error) {
	fs := flag.NewFlagSet("recover-operator", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&generate, "generate", false, "generate one strong random secret and print it once to stdout")
	fs.BoolVar(&noRecovery, "no-recovery", false, "skip the identity_recovery re-seed")
	if err = fs.Parse(args); err != nil {
		return false, false, err
	}
	if fs.NArg() > 0 {
		return false, false, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	return generate, noRecovery, nil
}
```

**Entry-point + error handling pattern** (lines 43-101): every failure path prints a plain
message to stderr via `fmt.Fprintln(os.Stderr, err)` and `os.Exit(1)`; success prints one
`ok: ...` line to stdout naming the identity id and never a secret. The existing verb is
found via `identityRecoverOperator` — search the caller switch (likely `cmd/aura/main.go` or
`cmd/aura/identity_*.go`) for how `recover-operator` is dispatched from `aura identity
<verb>` to find the exact registration point for `create`.

**IMPORTANT — must call the saga, not raw SQL:** unlike `two_identity_e2e_harness_test.go`'s
`musrProvisionIdentity` (raw `INSERT INTO aura.identities`), D-07 explicitly requires the
CLI to call `onboardingService.StartSession(ctx, operatorID)` then
`Provision(operatorID, token, req)` — the exact pair at `internal/agui/onboarding_api.go:199`
— so the analog for the *call sequence* is `onboarding_api.go`, not the test harness.

---

### `cmd/aura/serve_sandbox_preflight.go` (new, D-03)

**Analog:** `cmd/aura/serve_sandbox_readiness.go:31-58` — `router.CheckRuntime`, already an
`ImageInspect`-based check (not a pull), the nearest existing "ask the sandbox router about
image availability" call. The new preflight needs `ImagePull` too (per D-03: "refuse to
start when `AURA_SANDBOX_IMAGE` is neither present nor pullable"), which
`docker_backend_lifecycle.go:228-241`'s `ensureImage` already implements as the lazy,
first-tool-call path — the preflight should call the same underlying pull logic eagerly at
boot, not duplicate it.

**Anti-pattern already documented in RESEARCH:** do NOT put this inside
`Config.ValidateProfile` — its own doc comment (`config_validate.go:79-85`) states it
"performs no other I/O" beyond two named exceptions, and `serve_settings.go:201`'s
live-reload path depends on that being side-effect-free. This must be a separate boot step
that runs after `cfg.Validate()` succeeds, structured like a gate (Fatal message naming the
exact remedy command) but living outside that function.

**Gate-message shape to mirror** (from `gateMultiUserRequiresStrictProfile`,
`config_validate.go:108-129`):
```go
func (c *Config) gateMultiUserRequiresStrictProfile(p RuntimeProfile) []Violation {
	if c == nil || !c.MUSRIsolation || p.Strict() {
		return nil
	}
	return []Violation{{
		Knob: "AURA_MUSR_ISOLATION",
		Sev:  Fatal,
		Msg: "a second identity needs a hardened profile: set AURA_PROFILE to " +
			"single_user_hardened or server_production, or leave AURA_MUSR_ISOLATION off — " +
			"...",
	}}
}
```
D-03's preflight message must, per CLAUDE.md's own cited lesson in this same function's
comment ("a fail-closed gate that tells the operator to set a variable with no effect leaves
them stuck at a boot loop following its own instructions"), name a command that actually
works — e.g. the literal `docker build`/`docker pull` invocation, not a vague pointer.

---

### `cmd/aura/two_identity_e2e_test.go` + `two_identity_e2e_harness_test.go` (D-10, D-11 — memory plane, 5th tag)

**Analog:** the files themselves. Build-tag header to extend (currently, verbatim):
```go
//go:build db_integration && garage_integration && authula_integration && musr_e2e
```
becomes (adding the 5th tag, D-10):
```go
//go:build db_integration && garage_integration && authula_integration && musr_e2e && arcadedb_integration
```

**musrEnvOrSkip pattern** (harness file, lines ~40-51) — every new ArcadeDB env read must go
through the same shape:
```go
func musrEnvOrSkip(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("musr_e2e requires %s, but it is unset under CI — a skipped acceptance "+
				"tier must not pass as green; wire it in ci.yml", key)
		}
		t.Skipf("musr_e2e requires %s; bring the full stack up and set it", key)
	}
	return v
}
```

**musrProvisionIdentity pattern** (harness, lines ~85-108) — the shape for a new
`musrProvisionArcadeDBTenant`-style helper (D-11 item 2's HMAC-derived credential):
```go
func musrProvisionIdentity(t *testing.T, pool *pgxpool.Pool, label string) string {
	t.Helper()
	id := uuid.NewString()
	name := fmt.Sprintf("musr-%s-%s@example.test", label, id[:8])
	...
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM aura.identities WHERE id = $1::uuid`, id)
	})
	return id
}
```
The new memory-plane helper should call `arcadedb.NewTenantCredentials()` +
`arcadedb.DatabaseFor`/`PasswordFor` directly (per RESEARCH's own "Don't Hand-Roll" table:
"This *is* the production mechanism; testing a stand-in would prove nothing about it") —
not a mock.

**Positive-control convention (Claude's Discretion item):** every cross-deny subtest in the
existing file asserts B is denied AND A still reads its own row in the same `t.Run` — follow
that structure for the memory plane's three D-11 surfaces.

---

### `internal/arcadedb/tenant.go` — the production mechanism D-11 items 1-2 exercise directly

**Analog:** itself (production code to call, not to copy). Header rationale (lines 12-24,
verbatim) — the "why" comment the planner should cite when justifying the design of the
cross-deny test rather than re-deriving it:
```
// One database per identity, and the SERVER enforces it.
//
// The alternative was an owner_id column and a filter on every read. Measured on
// the live server, the difference is not stylistic:
//
//	User 'aura_memory' is not allowed to access database 'tenant_probe'
//
// That is a SecurityException from ArcadeDB.
```
`DatabaseFor(identityID string) (string, error)` (line ~34) fail-closes on empty input; the
D-11 item 2 test constructs identity B's credential via this exact function and points it at
identity A's database name, expecting the server-level `SecurityException`.

---

### `cmd/arcadedb-mcp/identity.go` — D-11 item 3's target mechanism

**Analog:** itself (production code). `identityFromToken` (lines 15-24) reads
`req.Extra.TokenInfo.UserID` from the verified bearer token — never a caller-supplied
header. Confirmed present as RESEARCH claims. No existing test in the 34
`arcadedb_integration`-tagged files was found this session to already construct two
identities and assert cross-deny through this specific MCP boundary — grep those files
before writing this subtest (RESEARCH Open Question 3 stands; treat as likely-new, verify
before estimating).

---

### `internal/agent/*` concurrent-runner test (D-14, ISO-05)

**Analog:** `internal/arcadedb/concurrent_fact_write_test.go` (build tag
`arcadedb_integration`) — the only file in the repo demonstrating "real goroutines +
goleak against a shared/singleton-backed resource, deliberately racing," which is exactly
D-14's shape (adapted to no build tag / in-process, since D-14 needs two `LlmAgent` runs in
one `go test` process, not a live sidecar).

**Build-tag + goleak header pattern** (lines 1-21):
```go
//go:build arcadedb_integration

// ... N workers writing into ONE identity's graph at the same time. A sequential
// `for` loop ... passes green while leaving the actual race ... completely
// unexercised ...
package arcadedb

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"go.uber.org/goleak"
)
```
D-14's new test has **no build tag** (it is an in-process white-box test, not a live-sidecar
integration test) but should still call `goleak.VerifyNone(t)` explicitly (or rely on
`internal/agent/main_test.go:14`'s existing `goleak.VerifyTestMain(m)`, which already covers
every test in that package — confirm whether the new test's package already has this
`TestMain`, in which case an explicit second `VerifyNone(t)` call is redundant, not
additive).

**Per-turn construction to assert stays per-turn** — `internal/agent/llm_agent.go:42-120`
(596 LOC file, near the 600-LOC ceiling — a new large helper type may force a split into
`llm_agent_<concern>.go`) and `internal/runner/runner.go:381-387`'s fresh-`Budget`-per-turn
call (`agent.NewBudget(...)`) are the production code the test constrains, not an analog to
copy.

**Four D-15 surfaces to prove disjoint** (targets of assertion, not files to pattern-copy):
1. `internal/agent/tools/result.go:198-206` `sidecarPath` — session-keyed, not identity-keyed
2. `internal/gateway/approve.go:213-227` `ReservationKey{ConversationID, RequestID, ToolCallID}` — no identity field, verbatim:
```go
func gatewayApprovalContext(spec tools.Spec, tier scoring.RiskTier, key ReservationKey, fp string) json.RawMessage {
	b, err := json.Marshal(map[string]any{
		"type":            gatewayApprovalType,
		"tool":            spec.Name,
		"tier":            string(tier),
		"conversation_id": key.ConversationID,
		"request_id":      key.RequestID,
		"tool_call_id":    key.ToolCallID,
		"args_sha256":     fp,
	})
	...
}
```
3. `tools.Registry`, `SteerInbox` — process-wide singletons per `runner.go`'s own comments
4. `llm.Client`, `PromptBuilder`, KV prefix — one of each serves every identity

---

### `scripts/lib/disposable_stack.sh` (new, D-12 — extracted from `scripts/coverage_docker.sh`)

**Analog:** `scripts/coverage_docker.sh` lines 13-70+ (the exact block to lift). Key
excerpts to extract as functions:

`read_secret` (lines 15-25):
```bash
read_secret() {
  local key="$1" val="${!1:-}"
  if [ -z "$val" ] && [ -f .env ]; then
    val="$(grep -E "^${key}=" .env | head -1 | cut -d= -f2-)"
  fi
	printf '%s' "$val" | tr -d '\r'
}
```

The `aura`-name refusal guard (lines 51-59, the D-12-cited anti-footgun):
```bash
COV_DB="${AURA_COVERAGE_DB:-aura_cov}"
...
if [ "$COV_DB" = "aura" ]; then
  echo "FATAL: AURA_COVERAGE_DB must not be 'aura' — the db_integration tier TRUNCATEs it (data loss). Pick a throwaway name." >&2
  exit 4
fi
```
This must be parameterized (the new script's disposable-DB var name will differ) but the
exit-4 refusal shape and message tone should carry over verbatim per D-12.

The disposable-Postgres-container provisioning (lines 60-70+, `docker run -d --rm --name
"$COV_POSTGRES" ...` on `127.0.0.1:5433`) is the block `scripts/musr_e2e.sh` needs via the
extracted lib.

---

### `scripts/musr_e2e.sh` (new, D-13)

**Analog A (process/stack lifecycle half):** `scripts/agui_smoke.sh` — build, background
start, poll pattern (lines ~593-604 area of the file as read):
```bash
go build -o "${BIN}" ./cmd/aura
"${BIN}" serve >"${SERVE_LOG}" 2>&1 &
SERVE_PID=$!
for _ in $(seq 1 60); do
  if ! kill -0 "${SERVE_PID}" 2>/dev/null; then
    echo "FAIL: aura serve exited during boot"; cat "${SERVE_LOG}" >&2; exit 1
  fi
  if curl -fsS -o "${NULL_OUT}" "${BASE}/healthz" 2>/dev/null; then READY=1; break; fi
  sleep 0.5
done
```
D-13's script does not need this exact shape (it runs a tagged `go test`, not a live daemon
+ curl loop) but should still follow `agui_smoke.sh`'s no-fixed-sleep polling discipline for
its own Postgres/Garage/ArcadeDB readiness waits.

**Analog B (disposable stack half):** `scripts/coverage_docker.sh` via the newly-extracted
`scripts/lib/disposable_stack.sh` (D-12) — `source`d by both scripts per D-12's decision.

---

### `Makefile` `musr-e2e` target (new)

**Analog:** `Makefile:275-279` `memory-up-core` — the exact pattern that avoids the
`arcadedb-mcp depends_on: aura` daemon-race hazard:
```makefile
memory-up-core:
	docker compose up -d arcadedb aura-llama-embed
	$(call wait_compose_healthy,arcadedb)
	$(call wait_compose_healthy,aura-llama-embed)
	@echo "ok"
```
D-13 literally specifies `docker compose up -d garage arcadedb` (no `arcadedb-mcp`), which
matches this target's shape (bring up the substrate, skip the daemon-coupled sidecar). The
comment directly above `memory-up-core` (lines 261-274) documents the measured CI #1809
incident this avoids — cite it, do not re-derive it, in the new target's own comment if one
is warranted.

**Analog for wiring a shell script as a Make target:** `Makefile:89-98` area, the existing
`coverage-docker` target that calls `bash scripts/coverage_docker.sh`.

---

## Shared Patterns

### Fail-closed gate with actionable remedy message
**Source:** `internal/config/config_validate.go:108-129` (`gateMultiUserRequiresStrictProfile`)
**Apply to:** D-03's sandbox-image boot preflight — same "name the exact knob and the exact
remedy" shape, informed by the same function's own scar-tissue comment about a gate that
named a variable nothing read.

### Eager, idempotent, symmetric-compensation saga leg
**Source:** `internal/agui/onboarding_provision_resources.go:44-106`
**Apply to:** D-09's new sandbox leg — journaled through `run.step`, compensated on
`context.WithoutCancel(ctx)`, added to the reverse-order `compResources` closure.

### No-skip-as-green env read
**Source:** `cmd/aura/two_identity_e2e_harness_test.go` `musrEnvOrSkip` (lines ~40-51)
**Apply to:** every new env var the D-10/D-11 memory-plane subtests and D-13's script read.

### Server-enforced tenancy, called directly (never mocked)
**Source:** `internal/arcadedb/tenant.go` (`DatabaseFor`, `TenantCredentials`)
**Apply to:** D-11 items 1-2 — the test must call the production credential-derivation
function, per RESEARCH's explicit "Don't Hand-Roll" guidance ("testing a stand-in would
prove nothing about it").

### Disposable-resource anti-footgun (exit-4 name refusal)
**Source:** `scripts/coverage_docker.sh:51-59`
**Apply to:** `scripts/lib/disposable_stack.sh` (D-12) — same refusal shape for whatever
name variable the new lib introduces.

## No Analog Found

| File | Role | Data Flow | Reason |
|---|---|---|---|
| D-16 closing-run harness (drives two REAL authenticated concurrent `/agent/run` conversations with the D-17 raced symmetric tasks, scored against a rubric) | integration harness | streaming (SSE) + concurrent | `scripts/agui_smoke.sh` covers the single-identity process-lifecycle/login half but nothing in the repo drives **two** identities concurrently through the live gateway with **captured, comparable transcripts** for scoring — this is closer to new composition than an extension. RESEARCH's own "Primary recommendation" and Pitfall 2 (no existing driver for the forced-first-login-of-a-provisioned-identity flow, since `agui_smoke.sh` only exercises TOTP *verify* against the pre-seeded `local` identity, never TOTP *enrollment*) confirm this. |
| The written ≥9.8 scoring rubric itself (D-18) | docs/spec artifact | — | No existing rubric-shaped file in the repo; `internal/agenteval/case.go:14-17`'s explicit no-rubric-gates philosophy is the constraint to honor, not a template to copy — this is a new prose artifact recording dimensions/weights, not code. |

## Metadata

**Analog search scope:** `internal/agui/`, `internal/sandbox/usersandbox/`, `internal/config/`,
`internal/arcadedb/`, `cmd/arcadedb-mcp/`, `internal/agent/`, `internal/runner/`,
`internal/gateway/`, `cmd/aura/` (all files matching `serve*`, `recover*`, `two_identity*`,
`identity*`), `scripts/`, `Makefile`, `docs/runbooks/`.
**Files scanned:** ~30 read directly this session (see command history); RESEARCH.md's own
~40-file "Primary (HIGH confidence)" source list was cross-checked, not re-read where this
session's reads already covered the same file.
**Pattern extraction date:** 2026-09-07
**Migration numbering fact (not a decision):** `ls internal/db/migrations/ | tail -1` at
research time returned `0119_drop_orphan_content_parts.up.sql` — confirmed unchanged this
session. None of D-01..D-18 requires a new migration per CONTEXT.md; RESEARCH Open Question 1
asks the planner to grep `aura.provisioning_saga`'s schema before assuming the sandbox leg
needs no new column. If a migration turns out to be needed, re-run `ls` at landing time —
`0120` is a point-in-time fact, not a reservation.
