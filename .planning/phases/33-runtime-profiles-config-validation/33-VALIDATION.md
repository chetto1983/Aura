---
phase: 33
slug: runtime-profiles-config-validation
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-30
---

# Phase 33 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Derived from RESEARCH.md `## Validation Architecture` (commit 4e8341ef).

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go stdlib `testing` (table-driven) + `pgregory.net/rapid` v1.3.0 (property-based) |
| **Config file** | none (Go convention); `.golangci.yml` for lint |
| **Quick run command** | `go test ./internal/config/ ./cmd/aura/ ./internal/agent/tools/` |
| **Full suite command** | `make quality` (vet+build+file-size+lint+test-race+vuln); `make quality-full` adds the coverage gate |
| **Estimated runtime** | ~10–20 seconds (pure-Go, no service dialing) |

> **No runtime services required.** Validation reads env vars + Go constants; it does not dial Postgres/Neo4j/Garage. The integration stack is irrelevant to this phase.

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/config/ ./cmd/aura/ ./internal/agent/tools/` (+ `-race` on touched packages per CLAUDE.md Gate 2)
- **After every plan wave:** Run `make quality` (whole-tree vet/build/lint/test-race/vuln)
- **Before `/gsd-verify-work`:** `make quality-full` green (coverage ≥85% owned surface) + mutation spot-check ≥70% on the validator files
- **Max feedback latency:** ~20 seconds (quick run)

---

## Per-Task Verification Map

> Task IDs are assigned at planning; this map keys by requirement → test (from RESEARCH.md). The planner/`gsd-add-tests` refine to per-task `<automated>` verify blocks.

| Requirement | Behavior | Test Type | Automated Command | File Exists |
|-------------|----------|-----------|-------------------|-------------|
| PROF-01 | `validate --profile server_production` exits non-zero, lists every unmet req | unit + e2e | `go test ./cmd/aura/ -run TestConfigValidate_ServerProduction -x` | ❌ W0 (`cmd/aura/config_validate_test.go`) |
| PROF-01 | `ValidateProfile` aggregates ALL violations (not first-fail) | unit (table) | `go test ./internal/config/ -run TestValidateProfile -x` | ❌ W0 (extend `config_validate_test.go`) |
| PROF-02 | destructive truth table: unset/empty/`off`/`OFF`/custom/copied-sample | unit (table) | `go test ./internal/agent/tools/ -run TestDestructiveShellPatterns -x` | ❌ W0 (extend `shell_exec_env_test.go`) |
| PROF-03 | sample creds + empty RPC secret rejected under hardened/prod, pass when supplied | unit (table) | `go test ./internal/config/ -run TestGateObjectStore -x` | ❌ W0 (`config_validate_test.go`) |
| PROF-04 | invalid int/bool ⇒ FATAL under hardened/prod, WARN under dev/local_trusted | unit (table) + **PBT** | `go test ./internal/config/ -run 'TestReparse|TestRapidEnv' -x` | ❌ W0 (`config_knobs_test.go`) |
| PROF-05 | non-absolute `AURA_RUN_DIR` ⇒ RunDirErr surfaced by validator | unit | `go test ./internal/config/ -run TestRunDir -x` | ✓ partial (`config_rundir_test.go`) — extend |
| PROF-06 | `replication_factor` (new `AURA_OBJECTSTORE_REPLICATION_FACTOR` knob) =1 rejected under prod, ≥2 passes | unit (table) | `go test ./internal/config/ -run TestGateReplication -x` | ❌ W0 |
| QUAL-04 | every cataloged knob has a registry row; registry round-trips defaults | unit | `go test ./internal/config/ -run TestKnobRegistry -x` | ❌ W0 (`config_knobs_test.go`) |
| D-01/D-03 | `AURA_PROFILE` unset → dev; override via `--profile` | unit (table) | `go test ./internal/config/ -run TestParseProfile -x` | ❌ W0 (`config_runtimeprofile_test.go`) |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

### Property-based invariants (rapid) — PROF-04 / criterion #1

1. **Strictness invariant:** for any cataloged int/bool knob and any garbage value failing `strconv`, `ValidateProfile(strict)` yields a **Fatal** naming that knob; `ValidateProfile(lenient)` yields at most a **Warn** (never Fatal).
2. **No-false-positive invariant:** for any cataloged knob and any *valid* value of its kind, the re-parse pass yields **no** violation for that knob.
3. **Aggregation invariant:** violation count is monotonic — adding a second bad knob never removes the first's violation (proves "lists every unmet requirement").

### Destructive-shell truth table (D-12 / PROF-02)

unset → ACTIVE (defaults) · `""` empty → ACTIVE (**the fix**) · whitespace → ACTIVE (TrimSpace) · `off`/`OFF`/`Off` → DISABLED (case-insensitive) · custom → ACTIVE (custom) · copied `.env.example` (commented) → ACTIVE (criterion #2).

---

## Wave 0 Requirements

- [ ] `internal/config/config_runtimeprofile_test.go` — `TestParseProfile` (PROF-01 / D-01 / D-03)
- [ ] `internal/config/config_knobs_test.go` — `TestKnobRegistry`, `TestReparsePass`, rapid invariants (PROF-04 / QUAL-04)
- [ ] `internal/config/config_validate_test.go` — EXTEND with `TestValidateProfile`, `TestGateObjectStore`, `TestGateReplication` (PROF-01 / 03 / 06)
- [ ] `internal/agent/tools/shell_exec_env_test.go` — EXTEND with `TestDestructiveShellPatterns` truth table (PROF-02)
- [ ] `cmd/aura/config_validate_test.go` — `TestConfigValidate_ServerProduction` exit-code + knob-name output (PROF-01 e2e)
- [ ] Framework install: none (`rapid` already a direct dep)

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Mutation spot-check ≥70% killed on `config_validate.go` / `config_knobs.go` | QUAL-04 / CLAUDE.md gate | `go-mutesting` runs on WSL only, not in per-task sampling | In WSL: `go-mutesting ./internal/config/...` on the validator files; record killed/total in phase VALIDATION sign-off |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 20s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
