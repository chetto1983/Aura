---
phase: 37-per-user-full-capability-sandbox
reviewed: 2026-07-07T07:56:47Z
depth: deep
files_reviewed: 10
files_reviewed_list:
  - internal/config/config_sandbox.go
  - internal/config/config_knobs.go
  - internal/config/config_sandbox_test.go
  - cmd/aura/serve_dispatch.go
  - internal/sandbox/usersandbox/docker_backend.go
  - cmd/aura/serve_dispatch_egress_test.go
  - cmd/aura/serve_dispatch_egress_integration_test.go
  - internal/sandbox/usersandbox/egress_integration_test.go
  - compose.yaml
  - docs/adr/0037-per-identity-docker-sandbox.md
findings:
  critical: 0
  warning: 2
  info: 2
  total: 4
status: issues_found
---

# Phase 37: Code Review Report — 37-10 Gap-Closure Diff

**Scope note:** this review covers ONLY commit `bdebc5c9` ("fix(37-10): wire the always-on egress floor into buildSandboxRouter (SBX-04)"), the sole gap-closure diff landed since Phase 37 was last verified (which flagged SBX-04 egress wiring as a BLOCKER: gaps_found 4/5). Plans 37-01..37-09 are explicitly OUT of scope — they were already verified and are treated as ground truth here; any pre-existing code they own is read only for call-chain context, never re-litigated.

**Reviewed:** 2026-07-07T07:56:47Z
**Depth:** deep (full read of all 10 files, cross-file call-chain trace from config env var → `SandboxConfig.EgressImage` → `buildSandboxRouter`/`newSandboxBackend` → `usersandbox.WithEgress`/`DockerBackend.launchEgress` → `Resolve` → `Route` → tool call sites; independently ran the build/vet/test/lint/gofmt gates rather than trusting the commit message)
**Files Reviewed:** 10
**Status:** issues_found (0 Critical / 2 Warning / 2 Info — no Critical or High finding; the security-critical wiring fix itself is correct)

## Summary

This diff does exactly what it claims: it is a surgical, composition-root-only wiring fix. It adds `config.SandboxConfig.EgressImage` (sourced from `AURA_SANDBOX_EGRESS_IMAGE`, non-empty default `aura-egress:latest`, cataloged in the `KnobSpec` registry), threads it through a new `newSandboxBackend(cli, cfg)` helper into `usersandbox.WithEgress`, and exposes a read-only `DockerBackend.EgressImage()` accessor so the wiring is regression-testable without a Docker daemon. It touches none of the sidecar's security-relevant internals (`egress.go`, `translate.go`, `docker_backend_lifecycle.go`, `router.go` are byte-unchanged by this commit).

Independent verification performed (not just reading the diff):

- **Fail-closed chain traced end-to-end and confirmed live.** `Resolve` (`docker_backend_lifecycle.go:69-71`, unchanged) calls `launchEgress` *after* the box is live; `launchEgress` (`docker_backend.go`, unchanged) no-ops only when `egressImage == ""`, otherwise `ensureImage` pull-failure propagates a wrapped error up through `Resolve` → `Route` (`router.go:84-87`, unchanged) returns `(BoxHandle{}, true, err)` → every tool call site (`shell_exec.go:131-134`, `fs_read.go`, `fs_write.go`, `send_file.go`) checks `if routed && routeErr != nil { return sandboxUnavailableResult(...) }` and denies. No path lets a `routed=true` box run without a successfully-launched sidecar.
- **Non-empty default independently confirmed stronger than "just a default."** `envDefault(key, fallback)` (`config_env.go:17-22`) falls back to `fallback` whenever the env var is **unset OR empty** — not only unset. Combined with the non-empty `defaultSandboxEgressImage = "aura-egress:latest"`, this means there is no environment-variable-driven way to make `cfg.Sandbox.EgressImage == ""` in production; the only way to construct a `SandboxConfig` with an empty `EgressImage` is to build one directly in Go bypassing `loadSandboxConfig()`, which no production call site does (`buildSandboxRouter`'s only caller path is `config.Load`/`config.LoadServe` via `chat_boot.go`).
- **No contract weakening.** `translate.go`'s `toHostConfig` (unchanged) never sets `CapAdd` for the box; `launchEgress`'s `ContainerCreate` (unchanged) grants `CapAdd: ["NET_ADMIN"]` to the sidecar only. `compose.yaml`'s diff is comment-only plus the (pre-existing, previously-unconsumed) `AURA_SANDBOX_EGRESS_IMAGE` line — no new capability, mount, or port is added to the `aura` service.
- **Live test run, not just reading.** Ran the actual test suite rather than trusting the SUMMARY: `go build ./...`, `go vet ./...`, `go build -tags docker_integration ./...`, `go vet -tags docker_integration ./...` all clean; `go test -count=1 ./cmd/aura/... ./internal/sandbox/usersandbox/... ./internal/config/...` all green, including `TestBuildSandboxRouterWiresEgress` and `TestLoad_SandboxConfig` individually (`-run` + `-v`). `gofmt -l` clean on all 8 touched Go files. `golangci-lint run` clean on the touched packages (the two pre-existing findings it surfaces — a `gosec G304` in `materialize.go` and a `govet inline` hint on the *pre-existing, unchanged* `client.NewClientWithOpts` line in `serve_dispatch.go` — both predate this commit and are not attributable to it).
- **Docker-free wiring guard verified to actually prove the wiring.** `TestBuildSandboxRouterWiresEgress` (`serve_dispatch_egress_test.go`) asserts (1) a distinct digest-pinned `EgressImage` reaches `DockerBackend.EgressImage()` through `newSandboxBackend` (not a hardcoded literal), (2) a default-loaded `config.Load()` yields the non-empty `aura-egress:latest` and that reaches `EgressImage()` too, and (3) a non-strict profile returns `nil` from `buildSandboxRouter` before ever touching `WithEgress`/Docker. All three assertions are real (not tautological) and pass.

No Critical or High issue was found. Two Warnings and two Info items below are about verification depth and test-code hygiene, not the correctness of the shipped fix.

## Critical Issues

None found.

## Warnings

### WR-01: The composition-root live-DROP proof (and the pre-existing backend-level proof it mirrors) has no CI wiring at all — the strongest evidence for SBX-04 currently only runs on a developer's manual WSL invocation

**File:** `cmd/aura/serve_dispatch_egress_integration_test.go:1` (new `//go:build docker_integration` tag), also affects the pre-existing `internal/sandbox/usersandbox/egress_integration_test.go`

**Issue:** `egressITDockerdOrGate`/`egressITEnforcingBridgeOrGate` (and the pre-existing `skipUnlessDockerd`/`skipUnlessEnforcingBridge` they mirror) correctly implement the no-skip-as-green contract *internally* — under `$CI` they `t.Fatal` rather than `t.Skip` when the daemon is unreachable or the runner isn't native-Linux. That part of the review focus checks out.

However, `.github/workflows/ci.yml` has **zero** references to `docker_integration`, `AURA_SANDBOX_EGRESS_IMAGE`, or `aura-egress` (confirmed by grep across every workflow file). Since GitHub Actions only compiles/runs a build-tagged file when a job explicitly passes `-tags docker_integration`, this means:
- `TestBuildSandboxRouter_LaunchesEgressFloor` (the new composition-root live-DROP proof this plan adds) and
- `TestEgress_FloorDropsInternal` (the pre-existing backend-level proof, 37-04/37-06)

never execute in CI at all — not even as a skip. They are invisible to every automated pipeline run and only execute if a developer manually runs `go test -tags docker_integration ./...` on a native-Linux Docker host. `37-10-SUMMARY.md` is honest about this ("Carried Forward... NOT passed... Must run green in WSL/CI before Phase 37 Gate-3 close"), so this is a disclosed, tracked gap rather than a hidden one — and the actual regression this plan closes (the missing `WithEgress` call) *is* protected by the always-on, tag-free `TestBuildSandboxRouterWiresEgress`, which really does run on every CI push. This is why the finding is a Warning, not a Critical: the shipped fix is proven; only the deepest, most convincing proof (a real nftables DROP against a live kernel) is not yet continuously verified.

Given the project already has the exact scaffolding needed for this (e.g. `memory-integration-test` in `ci.yml:770-884` builds a sidecar image via `docker/build-push-action@v6` then runs a tagged `go test -race -tags memory_integration ...` against `ubuntu-latest`, which is already native Linux with Docker preinstalled), wiring an equivalent `sandbox-integration-test` job is a small, well-precedented addition — not a novel undertaking.

**Fix:** Add a CI job modeled on `memory-integration-test`/`calendar-integration-test`:
```yaml
sandbox-integration-test:
  name: Sandbox egress floor (docker_integration)
  runs-on: ubuntu-latest
  steps:
    - uses: actions/checkout@v6
    - uses: actions/setup-go@v6
      with: { go-version-file: go.mod }
    - name: Build aura-egress image
      run: docker build -t aura-egress:latest docker/aura-egress
    - name: Build aura-sandbox image
      run: docker build -t aura-sandbox:latest docker/aura-sandbox
    - name: Run SBX-04 live egress-floor proof
      env: { CI: "true", AURA_EGRESS_ENFORCE: "1" }
      run: go test -race -count=1 -tags docker_integration ./cmd/aura/... ./internal/sandbox/usersandbox/...
```

### WR-02: New `cmd/aura` docker_integration test helpers duplicate ~60-70 lines of pre-existing `usersandbox` test helpers instead of sharing them

**File:** `cmd/aura/serve_dispatch_egress_integration_test.go:127-254` vs `internal/sandbox/usersandbox/egress_integration_test.go:31-97` and `internal/sandbox/usersandbox/dockertest_support.go:23-72`

**Issue:** `egressITDockerdOrGate`, `egressITEnforcingBridgeOrGate`, `egressITRawExec`, `egressITBoxWgetOK`, and `egressITCapAdd` are near-verbatim reimplementations of `skipUnlessDockerd`/`dockerEndpoint`/`dockerdReachable`, `skipUnlessEnforcingBridge`, `rawExec`, `boxWgetOK`, and `inspectCapAdd`. This is a deliberate, plan-documented trade-off (`37-10-PLAN.md:194`: "its helpers are package-private to usersandbox, so the cmd/aura test carries its own minimal gate + moby inspect") rather than an oversight, and `.golangci.yml`'s `dupl` linter already excludes `_test.go` files project-wide — so this doesn't fail any existing gate. It is flagged anyway because CLAUDE.md's "REUSABLE CODE. Never duplicate; extract a helper" is stated as an absolute project rule, and the duplication has a real (if small) maintenance cost: a future bugfix to, say, the enforcing-bridge gate's CI-mandatory logic now has to be applied in two places to stay in sync, and nothing enforces that they stay identical.

**Fix:** Not blocking; consider on a future touch of this area — extract a small exported, `docker_integration`-tagged support package (e.g. `internal/sandbox/usersandbox/sandboxtest`) exposing `DockerdOrFatal(t)`, `EnforcingBridgeOrFatal(t)`, `RawExec(t, cli, id, cmd)`, `BoxWgetOK(...)`, `InspectCapAdd(...)` that both `usersandbox`'s and `cmd/aura`'s integration tests import, collapsing both copies to one.

## Info

### IN-01: Test cleanup is registered only after the fatal-error checks, so a `Resolve`/`Route` failure mid-creation leaks the box container + volume on a live daemon

**File:** `cmd/aura/serve_dispatch_egress_integration_test.go:293-305` (new), mirrors the pre-existing `internal/sandbox/usersandbox/egress_integration_test.go:113-117`

**Issue:** In `TestBuildSandboxRouter_LaunchesEgressFloor`:
```go
h, routed, err := router.Route(ctx)
if err != nil {
    t.Fatalf("router.Route: composition-root box creation failed ...: %v", err)
}
if !routed {
    t.Fatal("router.Route: routed=false ...")
}
cleanup := usersandbox.NewDockerBackend(cli, boxImage, ..., usersandbox.WithEgress(egressImage))
t.Cleanup(func() { _ = cleanup.Stop(context.Background(), h) })
```
`t.Cleanup` is registered *after* both fatal checks. If `Resolve` fails partway — precisely the fail-closed scenario this plan hardens (box created successfully, then `launchEgress`'s `ensureImage` pull fails) — `t.Fatalf` unwinds the goroutine before the cleanup is ever registered, leaking the already-created `aura-box-<id>` container and its workspace volume on the live daemon. This exactly mirrors the pre-existing `TestEgress_FloorDropsInternal` (`egress_integration_test.go:113-117`), so it is not a new regression, only a new instance of an existing pattern; it only matters on an already-failing run, and only on a manual/CI `docker_integration` invocation (never in the production binary).

**Fix:** Register cleanup before the fatal checks, using the deterministic name (Docker accepts a container name anywhere it accepts an ID for remove/inspect calls):
```go
cleanup := usersandbox.NewDockerBackend(cli, boxImage, ..., usersandbox.WithEgress(egressImage))
t.Cleanup(func() {
    _ = cleanup.Stop(context.Background(), usersandbox.BoxHandle{ContainerID: "aura-box-" + id, IdentityID: id})
})
h, routed, err := router.Route(ctx)
...
```

### IN-02: Doc-comment attributes the empty-egress-image guard to the wrong function

**File:** `cmd/aura/serve_dispatch.go:167-169`

**Issue:** `newSandboxBackend`'s doc comment states: "WithEgress(\"\") is a guarded no-op." `WithEgress` itself (`docker_backend.go:76-78`) unconditionally sets `b.egressImage = image`, including when `image == ""` — it does not guard anything. The actual guard is downstream, in `launchEgress`/`findEgress` (`if b.egressImage == "" { return nil }`, `docker_backend.go:112,129`). The end-to-end claim ("wiring an empty string makes the sidecar inert") is correct; only the attribution of *which function* guards it is imprecise. Purely cosmetic — no functional impact.

**Fix:** Reword to "an empty `EgressImage` makes `launchEgress` a no-op (guarded there, not in `WithEgress` itself)" or similar.

---

## Verification performed

- `go build ./...` — clean
- `go vet ./...` — clean
- `go build -tags docker_integration ./...` — clean (new integration test type-checks)
- `go vet -tags docker_integration ./...` — clean
- `go test -count=1 ./cmd/aura/... ./internal/sandbox/usersandbox/... ./internal/config/...` — all green
- `go test ./internal/config/... -run TestLoad_SandboxConfig -v` — PASS (all subtests)
- `go test ./cmd/aura/... -run TestBuildSandboxRouterWiresEgress -v` — PASS
- `gofmt -l` on all 8 touched `.go` files — clean
- `golangci-lint run` on the touched packages (with and without `--build-tags docker_integration`) — 2 findings, both pre-existing and unrelated to this diff (gosec G304 in untouched `materialize.go`; govet `inline` hint on an unchanged line in `serve_dispatch.go`, also present pre-commit)
- `compose.yaml` parsed with a YAML loader — valid
- Cross-checked every production call site of `router.Route(ctx)` (`shell_exec.go`, `fs_read.go`, `fs_write.go`, `send_file.go`) for the `routed && routeErr != nil` deny contract
- Grepped the entire repo for other `usersandbox.NewDockerBackend(` call sites to confirm `newSandboxBackend` is the only production constructor
- Grepped all GitHub Actions workflows for `docker_integration`/`aura-egress` — zero matches (substantiates WR-01)

---

_Reviewed: 2026-07-07T07:56:47Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep_
