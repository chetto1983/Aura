---
phase: "1"
slug: "two-identities-live-and-separated"
# status lifecycle: draft (seeded by plan-phase) → validated (set by validate-phase §6)
# audit-milestone §5.5 distinguishes NOT-VALIDATED (draft) from PARTIAL (validated + nyquist_compliant: false) (#2117)
status: draft
nyquist_compliant: false
wave_0_complete: false
created: "2026-09-07"
---

# Phase 1 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Seeded from `01-RESEARCH.md` § Validation Architecture. The Per-Task Verification Map
> is filled by the planner; every value below it was read from source, not assumed.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go standard `testing` + build tags; no external test framework |
| **Config file** | none — the `//go:build` tag lines on each file are the config |
| **Quick run command** | `go vet -tags 'db_integration garage_integration authula_integration musr_e2e arcadedb_integration' ./cmd/aura/` (compile-only floor, mirrors `ci.yml:465`) |
| **Full suite command** | `make musr-e2e` (new target this phase — disposable Postgres + Garage + ArcadeDB bring-up, seed, tagged run, teardown) |
| **Estimated runtime** | `TestTwoIdentityCrossDeny` measured at 2.98s on 2026-09-07 (six subtests, four tags, stack already up). Full `make musr-e2e` including bring-up: **not yet measured — plan `01-04` Task 2 records it.** A sub-second reading for either is a skip tell, not a speed-up. |

---

## Sampling Rate

- **After every task commit:** `go vet ./... && go build ./... && go test ./internal/<touched>/ && go test -race ./internal/<touched>/` (CLAUDE.md § Post-edit validation)
- **After every plan wave:** the tagged tier for that wave — the `go vet -tags` compile floor at minimum; `make musr-e2e` once the ArcadeDB/Garage bring-up leg exists
- **Before `/gsd-verify-work`:** `make musr-e2e` green in CI
- **Max feedback latency:** ~60s for the per-commit loop; the tagged tier is bring-up bound and not yet measured

---

## Build Tags and Owning Gates

| Tag | What it gates | Owning gate |
|-----|---------------|-------------|
| `db_integration` | Postgres-backed assertions (conversations, approvals, documents/RLS) | `musr-e2e` CI job (existing) |
| `garage_integration` | Garage object-store cross-deny | `musr-e2e` CI job (existing) |
| `authula_integration` | Real Authula-configured stack precondition | `musr-e2e` CI job (existing) |
| `musr_e2e` | The two-identity acceptance file itself | `musr-e2e` CI job (existing) |
| `arcadedb_integration` | ArcadeDB long-term-memory cross-deny — the plane the existing gate does not cover | `musr-e2e` CI job, extended (new this phase) |
| unit (untagged) | Config gate logic, CLI flag parsing | `make quality` (existing) |
| `-race` + `goleak` | The concurrent-runner separation test | `musr-e2e` CI job; `-race` already present at `ci.yml:543` |

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 01-07-T1 | 01-07 | 1 | E2E-02 | T-07-01, T-07-02 | The Authula TOTP *enrollment* contract is measured from the installed module at a pinned version and recorded before any login step is designed on it; a not-automatable verdict stops the phase instead of producing a workaround | source measurement (zero-dependency) | `go list -m github.com/Authula/authula && go doc github.com/Authula/authula/plugins/totp && test -s .planning/phases/01-two-identities-live-and-separated/01-AUTHULA-TOTP-CONTRACT.md` | ❌ new | ⬜ pending |
| 01-07-T2 | 01-07 | 1 | E2E-02 | T-07-04 | Q2 — the only question that needed a measurement — carries a disposition quoting the contract document's verdict, and the heading is marked closed; Q1's and Q3's plan-time dispositions are untouched and none was deleted to get there | docs | `grep -c 'RESOLVED' .planning/phases/01-two-identities-live-and-separated/01-RESEARCH.md` (2 before the task, at least 4 after) and `git diff -U0` on that file showing no removal beyond the heading and its plan-time status note | ❌ new | ⬜ pending |
| 01-01-T1 | 01-01 | 1 | ISO-01, E2E-02 | T-01-01, T-01-02 | `EnsureBox` refuses a blank identity instead of falling back to seeded `local`; one CLI run lands all four E2E-02 resources — the skills root asserted BY NAME, with the provisioned root set asserted non-empty first — through the unmodified saga | integration (4 tags) | `go test -race -count=1 -p 1 -tags 'db_integration garage_integration authula_integration musr_e2e' -run 'TestIdentityCreateProvisionsEveryPlane' ./cmd/aura/` | ❌ new | ⬜ pending |
| 01-01-T2 | 01-01 | 1 | E2E-02 | T-01-04 | Compensation destroys the box FIRST, on a cancel-immune context, idempotently; a nil port skips both leg and compensation | unit (`-race`) | `go test -race -count=1 ./internal/agui/ ./internal/sandbox/usersandbox/` | ❌ new | ⬜ pending |
| 01-01-T3 | 01-01 | 1 | E2E-02 | T-01-03 | Password and security answer never reach argv, stdout or a log; isolation-refusal, duplicate and backend failure stay distinguishable | unit | `go test -race -count=1 ./cmd/aura/ -run 'TestParseIdentityCreateFlags\|TestIdentityCreateErrors' -v` | ❌ new | ⬜ pending |
| 01-02-T1 | 01-02 | 2 | ISO-01 | T-02-03, T-02-05 | Strict + isolation-on refuses an unreachable box image naming a command that works; non-strict and isolation-off are untouched; no secret is echoed. Also: the DELEGATED Docker coverage authority is run once here, covering the `usersandbox` production code both 01-01 and 01-02 add | unit + delegated coverage authority | `go test -race -count=1 ./cmd/aura/ ./internal/sandbox/usersandbox/ ./internal/config/ -run 'TestSandboxPreflight\|TestBootInfoLine\|TestEnsureImage\|TestGateMultiUserRequiresStrictProfile' -v` and `bash scripts/docker_coverage_gate.sh` | ⚠️ partial — `TestGateMultiUserRequiresStrictProfile` exists (`internal/config/config_validate_test.go:233-247`), the rest new | ⬜ pending |
| 01-02-T2 | 01-02 | 2 | ISO-01 | T-02-01, T-02-02 | The shipped default hardens rather than relaxes; the in-place upgrade path is provably untouched | config / shell | `bash -n scripts/install.sh && git diff --exit-code -- compose.yaml && bash scripts/build_installer_test.sh` | ❌ new | ⬜ pending |
| 01-02-T3 | 01-02 | 2 | ISO-01 | T-02-01 | The exact key set a fresh install writes yields zero Fatal violations under `ProfileSingleUserHardened` | unit | `go test -race -count=1 ./cmd/aura/ -run 'TestInstallerFreshEnv' -v` | ❌ new | ⬜ pending |
| 01-03-T1 | 01-03 | 2 | ISO-02 | T-03-01, T-03-04, T-03-07 | Memory cross-deny through the `identityctx` chain and through the server's own SecurityException, each with a positive control, holding while both identities read concurrently — and the fifth tag lands in ONE commit with every command that selects on it, so no commit leaves the CI acceptance job green while selecting zero tests | integration (5 tags) + workflow assertion | `go test -race -count=1 -p 1 -tags 'db_integration garage_integration authula_integration musr_e2e arcadedb_integration' -run 'TestTwoIdentityCrossDeny' -v ./cmd/aura/` and the `ci.yml` five-tag YAML assertion in plan 01-03 Task 1 | ⚠️ partial — file exists at 4 tags, the 5th tag and the three memory subtests are new | ⬜ pending |
| 01-03-T2 | 01-03 | 2 | ISO-02 | T-03-02, T-03-06 | The MCP tenant selector honours the verified token's subject and not a caller-supplied header; no compose sidecar, so no daemon side effect | integration (`arcadedb_integration`) | `go test -race -count=1 -p 1 -tags 'arcadedb_integration' -run 'TestMemoryCrossDeny' -v ./cmd/arcadedb-mcp/` | ❌ new | ⬜ pending |
| 01-03-T3 | 01-03 | 2 | ISO-02 | T-03-03 | `DatabaseFor` fails closed on empty and on UUID lookalikes; the mapping is total and injective; the derivation is byte-stable | unit (daemon-free) | `go test -race -count=1 ./internal/arcadedb/ -run 'TestDatabaseFor\|TestTenantUserFor\|TestPasswordFor' -v` | ❌ new | ⬜ pending |
| 01-05-T1 | 01-05 | 2 | ISO-05 | T-05-01, T-05-04 | Two agents raced under two identities on the same tool with identical arguments neither replay nor cross; `Budget`, steer inbox and registry stay disjoint | unit (`-race` + goleak) | `go test -race -count=1 ./internal/agent/ -run TestTwoIdentityConcurrentRunsDoNotCross -v` | ❌ new | ⬜ pending |
| 01-05-T2 | 01-05 | 2 | ISO-05 | T-05-02, T-05-03 | Sidecar paths disjoint and mutually unreachable; reservation keys do not merge in the ledger; `messages[0]` byte-identical with no identity value in it | unit (`-race`) | `go test -race -count=1 ./internal/agent/ ./internal/gateway/ -run 'TestTwoIdentityConcurrent\|TestReservationKeyCrossIdentity' -v` | ❌ new | ⬜ pending |
| 01-05-T3 | 01-05 | 2 | ISO-05 | T-05-05 | Every assertion shown to go red when its property is deliberately broken; the reservation-key branches are killed, not merely covered | unit + mutation | `go test -race -count=1 ./internal/agent/... ./internal/gateway/ ./internal/steer/...` (mutation half is Manual-Only, below) | ❌ new | ⬜ pending |
| 01-04-T1 | 01-04 | 3 | ISO-02a | T-04-01, T-04-02 | The exit-4 `aura`-name refusal survives the extraction; the release-blocking coverage gate produces the same result it did before | shell test + gate re-run | `bash scripts/lib/disposable_stack_test.sh && bash scripts/coverage_docker.sh` | ❌ new | ⬜ pending |
| 01-04-T2 | 01-04 | 3 | ISO-02a | T-04-03, T-04-04 | One command from a clean checkout, no socat container, no hand-made database, and no `aura` daemon started as a compose side effect | integration / CI-shape | `make musr-e2e` | ❌ new | ⬜ pending |
| 01-04-T3 | 01-04 | 3 | ISO-02a | T-04-03 | CI runs the identical target; the always-compile floor gains the second package (`./cmd/arcadedb-mcp/`) on top of the five-tag list plan 01-03 already landed; the job still exports the composed DSNs the skip-helpers fail on | CI-shape | `bash scripts/check_ci_go_packages.sh` plus the workflow YAML assertion in plan 01-04 Task 3 | ❌ new | ⬜ pending |
| 01-06-T1 | 01-06 | 4 | E2E-01, E2E-02 | T-06-02, T-06-04, T-06-07 | Identity B's forced first login (password change + TOTP enrollment) driven end to end against the contract `01-07` measured at wave 1, with the module pin re-confirmed; each identity works only on data the run seeded; the deployment is left as found | live | `go list -m github.com/Authula/authula` (must match the recorded pin) then `bash scripts/musr_live_run.sh` | ❌ new | ⬜ pending |
| 01-06-T2 | 01-06 | 4 | E2E-01 | T-06-01, T-06-05 | A planted cross-read fails the run non-zero naming the assertion; an empty transcript or a non-overlapping timing fails rather than scoring low; the harness and the assert script name ONE run-output directory | live-assert + committed fixtures | `go run ./scripts/musr_live_run_assert.go --fixture clean`, `go run ./scripts/musr_live_run_assert.go --fixture leaking` (expected non-zero), and `grep -l 'artifacts/musr-live-run' scripts/musr_live_run.sh scripts/musr_live_run_assert.go` (must name both) | ❌ new | ⬜ pending |
| 01-06-T3 | 01-06 | 4 | E2E-01 | T-06-03, T-06-05 | The machine-checkable half blocks and the rubric only records; `internal/agenteval/case.go`'s no-rubric-gates position stands unamended; no credential in any captured artifact | live + human-check | `go run ./scripts/musr_live_run_assert.go --transcripts artifacts/musr-live-run` (the same directory Task 1 writes) and `git diff --exit-code -- internal/agenteval/case.go` | ❌ new | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

**Sampling continuity:** no three consecutive tasks lack an automated verify — every one of the
twenty rows above carries a runnable command, and each command is paired in its PLAN.md with
a stated failing direction naming the observable signal that constitutes failure (including,
for every tag-gated command, the `[no test files]` / sub-second skip-as-green shape CLAUDE.md's
NO SKIP-AS-GREEN rule forbids).

**`human_verify_mode` is `end-of-phase`** in `.planning/config.json`, so no plan emits a
`checkpoint:human-verify`. The one human judgement this phase needs — scoring the two live
transcripts against the ≥9.8 rubric — is carried in plan `01-06` Task 3's
`<verify><human-check>` block and is harvested into the phase UAT batch.

---

## Wave 0 Requirements

Existing infrastructure covers ISO-01's config-gate assertion (`internal/config/config_validate_test.go:233-247`)
and ISO-02's five already-proven planes (`TestTwoIdentityCrossDeny`). Everything below is new:

Reconciled against the actual seven-plan decomposition. The seeded list was correct on every
item; four additions and one correction follow from the task breakdown.

- [ ] **Added, and it runs FIRST:** the Authula TOTP *enrollment* contract, measured from the
      installed module at a pinned version and recorded in `01-AUTHULA-TOTP-CONTRACT.md` —
      E2E-02 (plan `01-07`, T1). It is a zero-dependency, minutes-long read that needs no
      stack, no Docker and no prior plan, and its answer decides what E2E-02's "no manual step
      outside the documented path" can claim. A not-automatable verdict is a stop-and-report,
      not a workaround. Nothing in waves 1-4 should be built on the assumption until this row
      is green. Plan `01-07` T2 closes Q2, the last open question in RESEARCH.md, in the same
      wave; Q1 and Q3 were editorial and carry dispositions appended at plan time.
- [ ] `aura identity create` CLI verb + the eager sandbox saga leg, exercised by a tagged
      provisioning test — E2E-02, ISO-01 (plan `01-01`, tasks T1–T3). **Correction to the seed:**
      the verb is not exercised "inside the seed step" — it is the tracer's own subject, and the
      seed step (`scripts/authula_seed_e2e.go`) is unchanged.
- [ ] Sandbox-image boot preflight test — ISO-01's second half; production code does not exist yet
      (plan `01-02`, T1)
- [ ] **Added:** installer fresh-`.env` contract test proving the shipped key set yields zero
      Fatal violations under the strict profile — ISO-01 (plan `01-02`, T3)
- [ ] `arcadedb_integration`-tagged memory cross-deny subtests inside the existing
      `TestTwoIdentityCrossDeny` tree — ISO-02 (plan `01-03`, T1)
- [ ] **Added:** MCP-boundary cross-deny in `cmd/arcadedb-mcp`, over the existing
      `newAgentMemoryLiveMCPWithOptions` harness — ISO-02, D-11 item 3 (plan `01-03`, T2)
- [ ] **Added:** daemon-free tenant edge battery (empty, malformed, adjacent, repeated) in
      `internal/arcadedb` — ISO-02 (plan `01-03`, T3)
- [ ] `make musr-e2e` target + its bring-up script + the extracted
      `scripts/lib/disposable_stack.sh` — ISO-02a, the unattended/clean-checkout/CI leg
      (plan `01-04`, T1–T3)
- [ ] Concurrent-runner white-box test (`-race` + `goleak`, goroutines inside one `go test`
      process) covering all four D-15 surfaces — ISO-05 (plan `01-05`, T1–T3)
- [ ] Live closing harness driving two concurrent authenticated `/agent/run` conversations,
      plus the blocking assert script and the recorded rubric — E2E-01, E2E-02
      (plan `01-06`, T1–T3)
- [ ] **Added:** the delegated `docker_coverage` authority run once for the phase, after both
      plans' `internal/sandbox/usersandbox` additions are in the tree — `bash scripts/docker_coverage_gate.sh`
      at the same 85% floor (plan `01-02`, T1). `scripts/coverage_docker.sh` is the
      `db_integration` tier and explicitly does not own that package, so it cannot answer for
      `router_provision.go` (01-01) or `router_image.go` (01-02).

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Conversation quality of the two concurrent live identities, scored ≥9.8 | E2E-01 | The score is a judgement against a written rubric; the machine-checkable half of the same run (both conversations complete, tools fire, no cross-read) is what blocks | Run the closing harness against a live `aura serve` with two provisioned identities; score both transcripts against the phase rubric and record the evidence |
| Mutation spot-check ≥70% killed on `internal/gateway` — the reservation-key machinery a prior replay defect lived in, and the surface this phase's ISO-05 evidence rests on | CLAUDE.md gate | `go-mutesting` runs only under WSL and is not wired into CI | `PATH="$HOME/.local/bin:$HOME/go/bin:$PATH" go-mutesting ./internal/gateway/` under WSL. `PASS` means killed, `FAIL` means survived; record killed/total and the ratio in the plan `01-05` SUMMARY. Result: _(pending, plan 01-05 T3)_ |
| Each ISO-05 assertion shown to go red when its property is deliberately broken | ISO-05 | The break is a temporary source edit reverted afterwards; it cannot live in CI as a permanently-failing test | Per plan `01-05` T3: give both agents one `SessionID`, then one `*Budget`, then drain with the other run's conversation id, then inject an identity value into the system message — confirming a named assertion fails each time. Record the break-to-assertion mapping in the SUMMARY. Result: _(pending)_ |

---

## Sampling Argument — what these checks would miss

- The concurrent-runner test samples **one deliberate collision shape** (same tool, same arguments,
  overlapping in time). A leak that manifests only under a different shape — different tools racing,
  or a three-way race — is not covered. Randomized interleaving is explicitly deferred, not hidden.
- The `musr-e2e` CI job runs `-p 1` (`ci.yml:543`) to serialize shared Postgres writes. The
  concurrency this phase must prove therefore has to be **goroutines within one `go test` process**;
  separate `go test` invocations would be serialized away and the test would pass vacuously.
- The shared-surface list is a **floor, not a ceiling**. Proving the enumerated surfaces disjoint does
  not prove no other process-wide singleton leaks.
- D-11 item 3's MCP surface is asserted through an **in-process `httptest` server** over the production
  tenant resolver and a live ArcadeDB, not through the compose `arcadedb-mcp` container. It covers
  `identityFromToken` → `tenants.For` → the per-identity database — the mechanism — and does **not**
  cover the deployed sidecar's own composition root. The narrowing is deliberate (`depends_on: aura`
  starts the whole daemon against the same Postgres a tagged tier writes to; measured CI #1809) and it
  is stated as a `must_haves` truth in plan `01-03` rather than left in rationale prose.

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency measured and recorded
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
