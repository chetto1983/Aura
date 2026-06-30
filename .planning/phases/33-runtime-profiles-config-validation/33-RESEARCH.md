# Phase 33: Runtime Profiles + Config Validation - Research

**Researched:** 2026-06-30
**Domain:** Go runtime configuration contracts, profile-aware fail-fast validation, env-knob cataloguing (`internal/config`, `cmd/aura`, `internal/agent/tools`)
**Confidence:** HIGH (entirely source-grounded against the live tree; zero external-package risk — all libs already in `go.mod`)

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

**Profile selection & default (PROF-01 / F-026)**
- **D-01:** Active profile is a **new `AURA_PROFILE` env read at config load** — a real runtime field on `Config`, NOT a CLI-only flag. PROF-04 (fail-fast vs warn by profile at boot) requires the profile be readable at runtime.
- **D-02:** `aura config validate --profile <p>` accepts an explicit `--profile` that **overrides** `AURA_PROFILE` for the validation run.
- **D-03:** **Default when `AURA_PROFILE` is unset = `dev`** — loudest diagnostics, most permissive, preserves today's full-host behavior exactly (success criterion #4). Tightening is always an explicit opt-in.

**Posture scope — what profiles do in THIS phase (Q2)**
- **D-04:** Profiles **validate config AND flip the cheap already-wired gates**. NOT validate-only, NOT the net-new gateway.
- **D-05:** Enforcement = **fail validation; operator fixes the env.** ONE consistent mechanism (identical posture to existing missing-secret fail-fast). **No silent coercion, no coerce+warn** — refuse unsafe values, never override them.

**Invalid-env behavior (PROF-04 / F-016)**
- **D-06:** Mechanism = **separate validation pass**. `internal/envutil` stays a **dumb silent-fallback leaf** for runtime reads (unchanged). A separate pass — **driven by the knob catalogue (D-08)** — re-parses each cataloged knob and collects diagnostics. Do NOT make envutil profile-aware.
- **D-07:** Strictness = **all-strict in prod, no security-subset taxonomy.** Under `single_user_hardened`/`server_production`, **ANY** invalid cataloged value is **fatal**. Under `dev`/`local_trusted` it **warns** and falls back.

**Knob catalogue (QUAL-04)**
- **D-08:** Catalogue = **Go registry as single source of truth.** A `[]KnobSpec{name, kind (int/bool/string/enum), default, profileConstraints}` slice that drives (a) the invalid-env validation pass, (b) `config validate` output, (c) optionally `.env.example` / doc generation. Rejected: doc-only markdown.

**Profile rule matrix (Q2 / Q3 follow-up)**
- **D-09:** `dev` (default) and `local_trusted` **preserve today's full-host behavior unchanged**: invalid env → warn only, **no gate flips**, full-host shell/tools intact. The dev↔local_trusted delta is minor (diagnostic verbosity / intent labeling).
- **D-10:** `single_user_hardened` (single-operator appliance tier, e.g. DGX Spark bundle) **keeps secret/auth hardening, relaxes redundancy**: requires real secrets + web-auth + rejects sample creds + invalid-env fail-fast (like prod), **BUT allows single-replica object store and single-node/loopback topology**.
- **D-11:** `server_production` = hardened + **additionally requires replication ≥ 2** and the **non-loopback-with-auth** posture. REJECTS: sample object-store creds + RPC secret (F-007/PROF-03); Garage `replication_factor = 1` (F-018/PROF-06); permissive CORS (`AURA_AGUI_CORS_PERMISSIVE=true`); destructive-shell `off` (`AURA_SHELL_DESTRUCTIVE_PATTERNS=off`, F-002); plus locked PROF requirements: required secrets unset, RunDir not absolute (F-041/PROF-05), non-loopback bind without auth (reuse `GuardWebBind`).

**F-002 destructive-shell semantics fix (PROF-02) — applies to ALL profiles**
- **D-12:** Flip `destructiveShellPatterns()`: **empty `AURA_SHELL_DESTRUCTIVE_PATTERNS` → use built-in defaults** (gate stays ACTIVE), **only explicit `off` disables.** Today empty=disabled. Test matrix covers unset / empty / `off` / custom / copied-sample. Correctness fix independent of profile (prod additionally forbids `off` per D-11).

### Claude's Discretion
- `go-playground/validator/v10` (v10.30.3) struct tags vs. existing hand-rolled multi-error pattern. Lib *available* (zero new direct dep). **Default to the hand-rolled idiom unless tags clearly reduce LOC.**
- Exact `KnobSpec` field shape, profile-constraint representation, and how `config validate` renders the violation list (human table; consider `--json` for CI, not required by acceptance).

### Deferred Ideas (OUT OF SCOPE — do NOT pull into Phase 33)
- **Per-profile RUNTIME enforcement** of tool capabilities, file-tool path fences, sandbox selection, network egress → **Tool Gateway (Phase 35+)**.
- **Durable mutating-tool ledger** → Phase 34/35.
- **Central capability policy engine** (actor × path × command × network × profile) → Tool Gateway.
- **F-026 contract items not already wired as knobs** — TLS termination, health-check hard gate, observability-required validation → future hardening. Phase 33 gates only *already-wired* knobs.
- **QUAL-04 double-`Validate` + pool-leak fix and `askuser/store.go:231` int32 guard** → **Phase 34** (only the env-catalog slice of QUAL-04 is Phase 33). Confirmed below (§QUAL-04 split).
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description (verbatim from REQUIREMENTS.md) | Research Support |
|----|---------------------------------------------|------------------|
| **PROF-01** | Operator selects a runtime profile and `aura config validate --profile <p>` reports every unmet requirement and fails non-zero. *(F-026)* | New `Config.Profile` field (D-01) + `config validate` CLI sibling to `show\|get\|set` (§Architecture P4) + aggregating `ValidateProfile()` mirroring `Validate()`'s `missing []string` (§Code Examples). |
| **PROF-02** | Copying `.env.example`→`.env` preserves destructive-shell gate (empty=defaults, only `off` disables); tests cover unset/empty/`off`/custom/copied-sample. *(F-002)* | Exact one-spot diff for `destructiveShellPatterns()` (§Code Examples) + 5-case truth table (§Validation Architecture). `.env.example:61` already commented. |
| **PROF-03** | `server_production` validation fails on sample/default object-store/Garage creds, RPC secret, bucket, endpoint; passes with supplied secrets. *(F-007)* | `defaultObjectStoreAccessKey`/`defaultObjectStoreSecretKey` are the reject sentinels (config.go:36-37); `GARAGE_RPC_SECRET` needs a read path (§Open Q1). Reject-set table in §Profile Rule Matrix. |
| **PROF-04** | Invalid int/bool env fails-fast under hardened/prod, warns under dev. *(F-016)* | Catalogue-driven re-parse pass keyed on `KnobSpec.Kind` (D-06/D-07); generic, zero per-knob code. PBT target (§Validation Architecture). NOTE: audit's `config_env.go/envIntDefault` ref is STALE — logic now in `internal/envutil`. |
| **PROF-05** | `AURA_RUN_DIR` normalized absolute at load, or rejected. *(F-041)* | **Already landed** — `absRunDir()` + `RunDirErr` surfaced by `Validate()` (config.go:519-532, 276-278). Phase 33 only confirms it stays wired into the profile validator. |
| **PROF-06** | `server_production` rejects single-replica Garage (`replication_factor = 1`), documents dev-only. *(F-018)* | `replication_factor=1` lives ONLY in `docker/garage/garage.toml:5` — NOT an env var. Needs a new knob OR toml parse (§Open Q1 — the one genuinely net-new read). |
| **QUAL-04** (env-catalog slice ONLY) | Catalogue hot-path `AURA_*` knobs in config. | The `[]KnobSpec` registry (D-08) IS this deliverable. Full grounded knob universe in §Knob Registry Catalogue. **int32-guard + double-Validate/pool-leak are Phase 34.** |
</phase_requirements>

## Summary

Phase 33 is a **pure-Go, internal-config + CLI phase** with zero external dependencies, zero network calls, and zero new packages. Everything it touches already exists in the tree: `internal/config/config.go` (`Config`, `Validate()`, `GuardWebBind()`, `absRunDir`/`RunDirErr`, the `defaultObjectStore*Key` sentinels), `internal/envutil` (the silent-fallback leaf, stays untouched), `internal/agent/tools/shell_exec_env.go` (`destructiveShellPatterns()`, the D-12 one-spot flip), and `cmd/aura/config.go` (the `show|get|set` dispatcher gaining a `validate` sibling). The work is: add a `Config.Profile` runtime field, build a `[]KnobSpec` registry that doubles as the F-016 re-parse engine, write a profile-aware `ValidateProfile()` that aggregates a `[]violation` (mirroring the existing `Validate()` / `GuardWebBind()` idiom), wire a `config validate` subcommand, and flip the destructive-shell empty-semantics.

The single load-bearing design decision is the **two-tier validator split**: (1) a **generic, kind-driven re-parse pass** over the registry satisfies PROF-04/F-016 for *every* cataloged int/bool/enum knob with no per-knob code (severity = fatal under hardened/prod, warn under dev/local_trusted); (2) a **small set of bespoke pure-function gates** (≤10, each mirroring `GuardWebBind`) satisfies the specific D-11 reject targets (sample creds, replication, CORS, destructive-off, required secrets, RunDir, bind-auth). Both feed one aggregator → "lists every unmet requirement" (criterion #1). This keeps the registry as clean, doc-genable data and avoids an over-engineered per-knob-closure framework — the "minimal industrial shape."

Two non-obvious hazards dominate the risk surface: a **four-level naming collision** (`internal/profile`, `Config.ProfileDir`, `AURA_PROFILE_DIR`, `config_profile_test.go` all already belong to the unrelated **Agent.md per-identity profile** — the new *runtime deployment* profile must use distinct names), and the fact that **`replication_factor` and `GARAGE_RPC_SECRET` do not currently flow into the `Config` struct at all** (the former is a static TOML value; the latter an env var read only by compose), so PROF-03/PROF-06 require a deliberate read-path decision (§Open Q1).

**Primary recommendation:** Hand-rolled `ValidateProfile()` mirroring `Validate()`/`GuardWebBind()` (reject `validator/v10` — see §State of the Art), a data-driven `[]KnobSpec` registry doubling as the F-016 engine, split into `config_runtimeprofile.go` + `config_knobs.go` + `config_validate.go` (config.go is 557/600 LOC — splitting is mandatory, not optional), and a `config validate` CLI with human table output + a cheap `--json` mode for CI.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Runtime-profile selection | API/Backend (`internal/config` load) | CLI (`cmd/aura`) | D-01: profile must be readable at runtime (boot fail-fast), not CLI-only. `loadBase()` populates `Config.Profile`. |
| Knob catalogue / registry | API/Backend (`internal/config`) | — | D-08: single source of truth in Go; drives validate + docs. |
| Profile-aware validation | API/Backend (`internal/config`) | CLI (renders + exit code) | Pure functions in config; CLI is a thin presenter. Mirrors `Validate()`/`GuardWebBind()`. |
| Invalid-env re-parse (F-016) | API/Backend (`internal/config` validation pass) | — | D-06: separate pass, NOT the `envutil` leaf, NOT call sites. |
| Destructive-shell semantics (D-12) | API/Backend (`internal/agent/tools` runtime leaf) | API/Backend (validator reads raw env for prod off-forbid) | The runtime leaf stays **profile-agnostic**; only the validator knows about prod. Clean separation. |
| `config validate` UX (table/json, exit code) | CLI (`cmd/aura`) | — | Presentation only; all logic lives in `internal/config`. |
| Garage replication / RPC-secret durability | Deployment/Storage (garage.toml, compose) | API/Backend (validator reads declared intent) | Phase 33 validates *declared* intent; actual Garage topology orchestration is deployment/ops (out of scope). |

## Standard Stack

This phase adds **no new dependencies**. Everything is Go stdlib + already-present modules.

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| Go stdlib `strconv` | go 1.26.4 | int/bool re-parse in the F-016 pass | Already the mechanism behind `envutil`; the validation pass reuses it for diagnostics `[VERIFIED: go.mod go 1.26.4]`. |
| Go stdlib `errors` (`Join`) | go 1.26.4 | optional multi-error aggregation | `errors.Join` (Go 1.20+) is the modern alternative to `missing []string`; see §State of the Art `[CITED: pkg.go.dev/errors]`. |
| Go stdlib `net` (`ParseIP`/`SplitHostPort`) | go 1.26.4 | loopback detection (reuse via `GuardWebBind`) | Already used by `GuardWebBind` (config.go:292-308) — do not regex IPs `[VERIFIED: internal/config/config.go]`. |
| Go stdlib `flag` | go 1.26.4 | `--profile`/`--json` parsing for `config validate` | Cleaner than the current manual `args[0]` switch for a flagged subcommand. |

### Supporting (test-only)
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `pgregory.net/rapid` | v1.3.0 **(direct dep)** | property-based testing of the registry env re-parse | Validator/parser pattern — ideal PBT target (§Validation Architecture) `[VERIFIED: go.mod:40]`. |
| `go.uber.org/goleak` | (project std) | goroutine-leak guard | Low value here (validators are pure, no goroutines) but cheap if `TestMain` already exists in the package. |
| `go-mutesting` (WSL toolchain) | per CLAUDE.md | mutation spot-check ≥70% on validator files | Project standard (NOT mewt). Run in WSL: `GOFLAGS` + DSN env only if container-gated (not needed here). |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Hand-rolled `ValidateProfile()` | `go-playground/validator/v10 v10.30.3` (indirect dep, available) | **REJECTED** — rules are profile-conditional + cross-field; validator/v10 tags express static field-level rules, so you'd write `StructLevel`/`RegisterValidation` custom funcs anyway = more ceremony, no LOC win. Breaks the codebase's clean `GuardWebBind` idiom. See §State of the Art. |
| `[]KnobSpec` registry | doc-only markdown table | REJECTED by D-08 — two sources of truth that drift (validate-vs-docs). |
| Per-knob `Validate func` closures in registry | data-only registry + generic kind pass + bespoke gates | Closures make the registry non-data (harder to table-test/doc-gen). The hybrid (§Architecture P1) is more minimal. |

**Installation:** None. Verify the two relevant libs are present (they are):
```bash
grep -E 'go-playground/validator|pgregory.net/rapid' go.mod
# go-playground/validator/v10 v10.30.3 // indirect
# pgregory.net/rapid v1.3.0
```

**Version verification:** `go 1.26.4` confirmed at `go.mod:3` — all modern test features apply (`synctest.Test`, `t.Context`, `b.Loop`, `errors.AsType`, `os.Root`, `t.ArtifactDir`).

## Package Legitimacy Audit

> **No external packages are installed by this phase.** The only two libraries referenced (`go-playground/validator/v10`, `pgregory.net/rapid`) are pre-existing entries in `go.mod` — no `go get`, no registry fetch, no slopcheck surface.

| Package | Registry | Age | Source Repo | Disposition |
|---------|----------|-----|-------------|-------------|
| `go-playground/validator/v10` | go modules (already indirect dep) | mature (10.x) | github.com/go-playground/validator | Pre-existing; **recommended NOT to promote to direct** (rejected — see Alternatives). |
| `pgregory.net/rapid` | go modules (already **direct** dep) | mature (1.3.0) | github.com/flyingmutant/rapid | Pre-existing; test-only, already vendored. |

**Packages removed due to slopcheck [SLOP] verdict:** none (no installs).
**Packages flagged as suspicious [SUS]:** none.

## Architecture Patterns

### System Architecture Diagram

```
ENV (os.Environ + best-effort .env via godotenv)
   │
   ▼
loadBase()  ──────────────────────────────────────────────┐
   │  reads AURA_PROFILE  ──► ParseProfile() ──► Config.Profile (default=dev, D-03)
   │  reads all other knobs (UNCHANGED silent-fallback via envutil/envDefault)
   ▼
Config{ Profile, DB, Neo4j, ObjectStore*, AGUI*, RunDir/RunDirErr, ... }
   │
   ├──────────────► BOOT PATH (daemon/REPL):  Validate() → ValidateProfile(cfg.Profile)
   │                     │                                      │
   │                     │ (existing: DB/Neo4j/RunDirErr)        │ fatal under hardened/prod
   │                     └──────────────────────────────────────┘  → fail-fast, name the knob
   │
   └──────────────► CLI PATH:  aura config validate [--profile P] [--json]
                          │
                          ▼
                   ValidateProfile(P or cfg.Profile)
                          │
              ┌───────────┴────────────────────────────────────┐
              ▼                                                 ▼
   (1) GENERIC re-parse pass                        (2) BESPOKE gates (≤10, pure)
       over []KnobSpec (D-08):                           mirror GuardWebBind:
       for each knob → re-read raw env →                  • sample object-store creds (F-007)
       parse per Kind (int/bool/enum) →                   • GARAGE_RPC_SECRET present (F-007)
       invalid ⇒ Violation{severity by profile}           • replication ≥2 (F-018)  [Open Q1]
       (PROF-04 / F-016, D-07)                            • CORS permissive=true (F-022 gate)
                                                          • destructive=off (F-002, prod only)
                                                          • required DB/Neo4j secrets (existing)
                                                          • RunDir absolute (F-041, existing)
                                                          • non-loopback bind w/o auth (GuardWebBind)
                                                          • web-auth required (hardened/prod)
              └───────────┬────────────────────────────────────┘
                          ▼
                   []Violation  (aggregated — "every unmet requirement")
                          │
                          ▼
            render: human table (default) | --json (CI)   +   exit 1 if any FATAL
                          │
                          └─ secret knobs REDACTED in output (Secret flag, §Security)

SEPARATE RUNTIME LEAF (profile-agnostic, D-12):
   internal/agent/tools/shell_exec_env.go::destructiveShellPatterns()
      unset|empty → defaults (gate ACTIVE)   only "off" → disabled   custom → parse
   (the validator only READS its raw env for the prod off-forbid check; never couples)
```

### Recommended Project Structure (file split — config.go is 557/600 LOC, split is MANDATORY)

The `internal/config` package already splits by concern (`config_mcp.go`, `config_routes.go`, `config_env.go`). Follow that precedent. **Avoid the Agent.md-profile name collision** (see Anti-Patterns):

```
internal/config/
├── config.go                      # +Profile field, +ParseProfile call in loadBase; MOVE Validate() out → frees LOC
├── config_runtimeprofile.go       # NEW: RuntimeProfile type (enum), ParseProfile(); ~60-90 LOC
├── config_knobs.go                # NEW: KnobSpec type + knobRegistry() []KnobSpec; ~150-300 LOC
├── config_validate.go             # NEW: Validate() (moved) + ValidateProfile() + Violation type + gates; ~150-250 LOC
├── config_runtimeprofile_test.go  # NEW (NOT config_profile_test.go — that's Agent.md!)
├── config_knobs_test.go           # NEW
└── config_validate_test.go        # EXTEND existing (currently 36 LOC, tests Validate())

cmd/aura/
├── config.go                      # +case "validate" in runConfig switch (286 LOC, room ok)
├── config_validate.go             # NEW: flag parse (--profile/--json) + render + exit; ~80-120 LOC
└── config_validate_test.go        # NEW

internal/agent/tools/
├── shell_exec_env.go              # D-12 one-spot flip + comment update (150 LOC)
└── shell_exec_env_test.go         # EXTEND: 5-case destructive truth table

.env.example                        # update lines 57-61 comment to match D-12 semantics
```

### Pattern 1: Two-Tier Validator (generic kind pass + bespoke gates)
**What:** A data-driven registry handles the broad F-016 re-parse; a handful of pure gate functions handle the specific D-11 reject targets. Both append to one `[]Violation`.
**When to use:** This phase. It is the "registry IS the engine" realization of D-08 without per-knob closures.
**Why it wins:** Every int/bool knob gets fail-fast-in-prod for free from `Kind`; the ~8 security gates stay individually table-testable like `GuardWebBind`.

### Pattern 2: Mirror `GuardWebBind` for every bespoke gate
**What:** Each gate is a pure, total, table-testable function returning a `config:`-prefixed message that **names the offending knob**.
**Example:** (verified pattern, config.go:292-308 + its test config_webauth_test.go:12-60)
```go
// Source: internal/config/config.go GuardWebBind — the exact pattern to mirror.
func GuardWebBind(bind string, authConfigured, trustProxy bool) error { ... }
// Test asserts err.Error() contains "AURA_AUTHULA_SECRET" and "AURA_WEB_TRUST_PROXY".
```
The "error message names the knob" assertion is the established way to prove criterion #1 ("lists every unmet requirement").

### Pattern 3: Profile as a typed enum with a total parse
**What:** `type RuntimeProfile string` with constants + `ParseProfile(s string) RuntimeProfile` defaulting unknown/empty → `dev` (D-03). Pure, table-testable. A `Tier()` helper (`lenient` {dev,local_trusted} vs `strict` {hardened,prod}) collapses the F-016 severity decision.

### Anti-Patterns to Avoid
- **Reusing the Agent.md-profile names.** `internal/profile` (parser.go/render.go/store.go), `Config.ProfileDir`, `Config.ProfileCertaintyN`, `AURA_PROFILE_DIR`, and `config_profile_test.go` are the **Agent.md per-identity profile** — wholly unrelated to runtime deployment profiles. Put the new type in `internal/config` (e.g. `RuntimeProfile`), use env `AURA_PROFILE` (no `_DIR`), and name new files `config_runtimeprofile*.go`. Do NOT create `internal/profile`-anything and do NOT name the test file `config_profile_test.go`.
- **Making `envutil` profile-aware.** Explicitly rejected (D-06). The leaf stays dumb; the validation pass re-reads raw env independently.
- **Teaching `shell_exec_env.go` about profiles.** The runtime leaf stays profile-agnostic (D-12). The prod "forbid `off`" check lives in the *validator*, which reads the raw env value — it does not change `destructiveShellPatterns()` behavior per profile.
- **Silent coercion.** D-05: never override an operator value to a "safe" one. Refuse and name it.
- **Chasing the stale audit reference.** F-016 cites `internal/config/config_env.go::envIntDefault/envBoolDefault`; that logic moved to `internal/envutil` (QUAL-03). `config_env.go` now holds only `envDefault`/`envSliceDefault` (string helpers).

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| int/bool parsing | custom parsers | stdlib `strconv` (as `envutil` does) | Already the mechanism; the validation pass calls `strconv.Atoi`/`ParseBool` for diagnostics. |
| Multi-error "list everything" | custom error-collector type | existing `missing []string` + `strings.Join`, OR `errors.Join` | `Validate()` already does this; consistency > novelty. |
| Loopback / wildcard IP detection | regex on host strings | `net.ParseIP(host).IsLoopback()` (reuse `GuardWebBind`) | Wildcards (0.0.0.0, ::) correctly fall through as non-loopback already. |
| Absolute-path normalization (F-041) | new code | **already done** — `absRunDir()` + `RunDirErr` | Just keep it wired into `ValidateProfile`. |
| A whole validation framework | adopt `validator/v10` for cross-field/profile rules | hand-rolled gates mirroring `GuardWebBind` | Profile-conditional + cross-field rules need custom funcs anyway; tags add ceremony, not savings. |
| Secret redaction in output | ad-hoc string masking | mirror `redactedAPIKey` (cmd/aura/config.go:24) + `KnobSpec.Secret` flag | Established `REDACTED` convention; never print secret values. |

**Key insight:** This domain is almost entirely "wire up and mirror existing, already-tested primitives." The only genuinely new logic is the `KnobSpec` registry shape and the replication/RPC-secret read path (§Open Q1). Everything else has a proven in-repo template.

## Knob Registry Catalogue (QUAL-04 — the actual list, grounded)

D-08's registry is the single source of truth. Below is the **full grounded universe** of `AURA_*` (and upstream-named) env reads in the tree, with where each is read. The planner decides the catalogue cut; the **recommended catalogue** = all int/bool/enum knobs in `internal/config` (the F-016 silent-fallback surface) **plus** the security-gated string knobs from D-11.

**Where reads live (methodology note):** a literal grep for `Getenv("AURA_…"` MISSES const-keyed reads (e.g. `shell_exec_env.go` uses `os.Getenv(envShellMaxTimeoutMs)`). The list below was built from `"AURA_…"` string-literal occurrences (const defs + inline) across `internal/` — it is complete for the central knobs.

### Tier A — recommended core catalogue (security/reliability hot-path; drives F-016 pass + gates)
| Knob | Kind | Default | Read at | Gate role (per D-11) |
|------|------|---------|---------|----------------------|
| `AURA_PROFILE` | enum(dev/local_trusted/single_user_hardened/server_production) | `dev` | **NEW** config.go loadBase | selects tier |
| `AURA_OBJECTSTORE_ACCESS_KEY` | string(secret) | `defaultObjectStoreAccessKey` sentinel | config.go:422 | sample ⇒ reject (hardened+prod) F-007 |
| `AURA_OBJECTSTORE_SECRET_KEY` | string(secret) | `defaultObjectStoreSecretKey` sentinel | config.go:423 | sample ⇒ reject (hardened+prod) F-007 |
| `GARAGE_RPC_SECRET` | string(secret) | empty (NOT in Config — compose-only) | compose.yaml:237,291 / `.env.example:208` | empty/sample ⇒ reject (hardened+prod) F-007 — **needs read path, Open Q1** |
| `AURA_OBJECTSTORE_BUCKET` | string | `aura-assets` | config.go:421 | default ⇒ warn (prod) |
| `AURA_OBJECTSTORE_ENDPOINT` | string | `http://127.0.0.1:3900` | config.go:418 | loopback ⇒ warn (prod) |
| *(replication_factor)* | int | `1` (garage.toml:5, NOT env) | docker/garage/garage.toml | `=1` ⇒ reject (prod) F-018 — **needs new knob, Open Q1** |
| `AURA_AGUI_CORS_PERMISSIVE` | bool | `false` | config.go:414 | `true` ⇒ reject (prod; recommend hardened too — A2) F-022 |
| `AURA_AGUI_BIND` | string | `127.0.0.1:9080` | config.go:413 | non-loopback w/o auth ⇒ reject (all) via GuardWebBind |
| `AURA_SHELL_DESTRUCTIVE_PATTERNS` | string | unset→defaults (post-D-12) | shell_exec_env.go:19 | `off` ⇒ reject (prod only, D-11) F-002 |
| `AURA_AUTHULA_SECRET` | string(secret) | empty | config.go:442 | empty ⇒ reject (hardened+prod web-auth required) |
| `AURA_WEB_TRUST_PROXY` | bool | `false` | config.go:437 | cross-field input to GuardWebBind |
| `POSTGRES_PASSWORD` / `AURA_DB_URL` | string(secret) | empty→"" DSN | config.go:325,333 | empty ⇒ reject (ALL, existing Validate) |
| `NEO4J_PASSWORD` | string(secret) | empty | config.go:359 | empty ⇒ reject (ALL, existing Validate) |
| `AURA_RUN_DIR` | string | `$UserCacheDir/aura` | config.go:347 | non-absolute ⇒ RunDirErr (ALL, existing) F-041 |

### Tier B — int/bool reliability knobs (in `internal/config`; recommended into the F-016 re-parse pass)
`AURA_CONTEXT_PREVIEW_CAP_BYTES`, `AURA_CONVERSATION_TURN_CAP_BYTES`, `AURA_CONTEXT_TOOL_EVICT_AFTER_TURNS`, `AURA_HISTORY_HARD_CAP_TURNS`, `AURA_RUN_DIR_WARN_THRESHOLD_BYTES`, `AURA_RUN_DIR_SWEEP_INTERVAL_SEC`, `AURA_WEB_DNS_PIN_TTL_SEC`, `AURA_WEB_FETCH_MAX_BODY_BYTES`, `AURA_WEB_CACHE_PERSISTENT`, `AURA_WEB_SEARCH_TIMEOUT_SEC`, `AURA_WEB_FETCH_TIMEOUT_SEC`, `AURA_SWARM_MAX_GOALS`, `AURA_SWARM_CHILD_TIMEOUT_SEC`, `AURA_SWARM_MAX_CONCURRENT`, `AURA_AGENT_JOB_MAX_DURATION_SEC`, `AURA_SCHEDULER_PREFER_ORIGIN_CHANNEL`, `AURA_SKILL_BODY_CAP_BYTES`, `AURA_SKILL_MANIFEST_CAP_BYTES`, `AURA_SKILL_SNIPPET_TTL_DAYS`, `AURA_AGUI_BUFFER_CAP`, `AURA_ASSET_MAX_*_BYTES` (×3), `AURA_ASSET_PRESIGN_TTL_SEC`, `AURA_ASSET_PROCESSING_CONCURRENCY`, `AURA_OBJECTSTORE_PATH_STYLE`, `AURA_TELEGRAM_LOCAL_BOT_API`, `AURA_AUTHULA_RATE_LIMIT_MAX`, `AURA_SERVE_SHUTDOWN_GRACE_SEC`, `AURA_VISION_CLOUD`, `AURA_MCP_NEO4J_CONNECT_TIMEOUT_SEC`, `AURA_EMBED_DIMENSIONS`, `AURA_PROFILE_CERTAINTY_N`.

### Tier C — knobs read OUTSIDE `internal/config` (const-keyed; in scope ONLY if catalogued by name)
The registry re-reads by env-key name (D-06), so these CAN be catalogued without moving their reads: `AURA_SHELL_MAX_TIMEOUT_MS`, `AURA_SHELL_OUTPUT_BUF_CAP`, `AURA_SHELL_BG_MAX`, `AURA_SHELL_BG_BUF_CAP` (`internal/agent/tools`), `AURA_FS_MAX_READ_BYTES`, `AURA_FS_WALK_NODE_CAP`, `AURA_FS_WALK_TIMEOUT_MS` (`internal/agent/tools/fs.go`), `AURA_LOOP_MAX_STEPS`/`_MAX_WALLCLOCK_SEC`/`_DEDUP_WINDOW`/`_BRANCH_SOFT_FRACTION`/`_NODE_TIMEOUT_SEC`/`_DEDUP_RESULT_CAP`/`_MAX_PARALLEL_TOOLS` (`internal/agent/budget.go`, `llm_agent_parallel.go`), `AURA_SWARM_MAX_DEPTH` (`internal/swarm`), `AURA_MCP_CALL_TIMEOUT_SEC`, `AURA_SCHEDULER_TICK_SECONDS`/`_MAX_CONCURRENT_RUNS`/`_NOTIFY_RETRY_ATTEMPTS`, `AURA_REASONING_TRACE_MAX_BYTES`, `AURA_TELEGRAM_*_THROTTLE_MS`/`_CHAT_RATE_LIMIT_MS`, `AURA_REASONING_FIFO_RUNES`, the LLM knobs (`internal/llm/config.go`: `AURA_LLM_*`, `AURA_MODEL_*`, `AURA_COMPLETION_*`).

**Recommendation:** catalogue Tier A (mandatory — the gates) + Tier B (the `internal/config` F-016 surface) for a "minimal industrial" registry that fully satisfies QUAL-04's "hot-path" wording without sprawling into every package. Tier C is optional — note it as a documented follow-on so the registry stays maintainable. (Tier B+C are all currently *silent-fallback*, so prod-fail-fast on any of them is the F-016 promise; cataloguing all of them is defensible but larger — flag the cut to the planner/discuss.)

## Profile Rule Matrix (D-09..D-12 → encodable table)

Legend: **REQ** = must be set/valid (else FATAL); **FORBID** = value rejected (FATAL); **WARN** = diagnostic, falls back, non-fatal; **OK/ignore** = no constraint. Tier shorthand: dev & local_trusted = *lenient*; hardened & prod = *strict*.

| Dimension (knob) | dev (default) | local_trusted | single_user_hardened | server_production | Finding |
|------------------|---------------|---------------|----------------------|-------------------|---------|
| Invalid int/bool of cataloged knob | WARN+fallback | WARN+fallback | **FATAL** | **FATAL** | F-016/PROF-04 (D-07) |
| Required DB secret (`POSTGRES_PASSWORD`/`AURA_DB_URL`) | REQ* | REQ* | REQ | REQ | existing `Validate()` |
| Required `NEO4J_PASSWORD` | REQ* | REQ* | REQ | REQ | existing `Validate()` |
| `AURA_RUN_DIR` absolute | REQ (RunDirErr) | REQ | REQ | REQ | F-041/PROF-05 (already wired) |
| Non-loopback `AURA_AGUI_BIND` w/o auth | FORBID (GuardWebBind) | FORBID | FORBID | FORBID | existing WEB-02 gate |
| Web-auth configured (`AURA_AUTHULA_SECRET`) | OK (loopback no-auth) | OK | **REQ** | **REQ** | D-10/D-11 gate flip |
| Sample object-store creds (`==default*Key`) | OK | OK | **FORBID** | **FORBID** | F-007/PROF-03 (D-10/D-11) |
| `GARAGE_RPC_SECRET` empty/sample | OK (fail-soft) | OK | **REQ non-empty** | **REQ non-empty** | F-007/PROF-03 — Open Q1 |
| Garage `replication_factor` | OK (=1) | OK (=1) | **OK (=1 allowed)** | **REQ ≥2** | F-018/PROF-06 — Open Q1; **the clean hardened↔prod differentiator** |
| Default bucket/loopback endpoint | OK | OK | OK (single-node, D-10) | WARN (A6) | F-007 (softer) |
| `AURA_AGUI_CORS_PERMISSIVE=true` | OK | OK | **FORBID (rec — A2)** | **FORBID** | F-022 gate (D-11 locks prod) |
| `AURA_SHELL_DESTRUCTIVE_PATTERNS=off` | OK | OK | OK (A3 — confirm) | **FORBID** | F-002 (D-11 locks prod only) |
| Destructive-shell empty→defaults (D-12) | active | active | active | active | F-002/PROF-02 (ALL profiles) |

`*` DB/Neo4j secrets are required for *any* daemon/REPL boot today (the wired `Validate()` runs regardless of profile) — the profile validator does not relax them.

**Tier collapse:** dev and local_trusted share an identical constraint column → implement them as one *lenient* tier with a label/banner difference (confirms D-09 / Open Q2). hardened differs from prod in exactly three cells: **replication ≥2**, **CORS-permissive forbidden**, **destructive-off forbidden** (the last two pending A2/A3 confirmation).

## Common Pitfalls

### Pitfall 1: Agent.md-profile naming collision
**What goes wrong:** Creating `internal/profile`-anything, `config_profile.go`, `config_profile_test.go`, or reusing `ProfileDir`/`AURA_PROFILE_DIR` for the runtime profile.
**Why:** Those names are already taken by the unrelated Agent.md per-identity profile (parser/render/store + `Config.ProfileDir` + `config_profile_test.go::TestProfileConfigDefaultsAndOverrides`).
**Avoid:** `type RuntimeProfile`, env `AURA_PROFILE`, files `config_runtimeprofile*.go`. **Warning sign:** a test named `TestProfile*` already compiles in the package → you're shadowing the wrong thing.

### Pitfall 2: replication_factor / GARAGE_RPC_SECRET are not in `Config`
**What goes wrong:** Writing `cfg.ReplicationFactor` or `cfg.GarageRPCSecret` checks that read fields which don't exist; or assuming `replication_factor` is an env var (it's static TOML at `docker/garage/garage.toml:5`).
**Avoid:** Decide the read path first (§Open Q1). Recommended: add `AURA_OBJECTSTORE_REPLICATION_FACTOR` (convention-compliant) → `Config.ObjectStoreReplicationFactor int` (default 1) and `Config.GarageRPCSecret string` from `os.Getenv("GARAGE_RPC_SECRET")` (upstream name, CLAUDE.md sidecar exception). **Warning sign:** "validate replication" with nothing to read.

### Pitfall 3: config.go blows the 600-LOC cap mid-edit
**What goes wrong:** Adding the Profile field + validator inline pushes config.go (557 LOC) over 600 → the `file-size` pre-commit hook blocks **all** commits (whole-tree scan, per MEMORY).
**Avoid:** Split first (move `Validate()` to `config_validate.go`), then add. **Warning sign:** the commit wrapper times out / file-size hook fires.

### Pitfall 4: D-12 fix that still disables on empty
**What goes wrong:** Removing `raw == ""` from the off-branch but letting empty fall through to `strings.Split("",",")` → yields no patterns → still disabled.
**Avoid:** Empty must explicitly return `defaultDestructivePatterns` (see §Code Examples). **Warning sign:** the "empty" truth-table case returns `nil, nil`.

### Pitfall 5: Treating compose's `${VAR:?}` as the F-007 fix
**What goes wrong:** Assuming the `${AURA_OBJECTSTORE_ACCESS_KEY:?...}` / `${GARAGE_RPC_SECRET:?...}` guards in compose.yaml (lines 132,231,237,291) already solve F-007.
**Why it's wrong:** Those guard only the compose path (unset/empty), NOT (a) a bare `aura serve` run — which silently falls back to the Go `defaultObjectStore*Key` sentinels — nor (b) an operator who explicitly sets the *sample* value. The Go validator must detect `value == sentinel`, regardless of source.

### Pitfall 6: Printing secret values in `config validate`
**What goes wrong:** Rendering the violation list dumps `AURA_AUTHULA_SECRET`/object-store keys to stdout/CI logs.
**Avoid:** `KnobSpec.Secret bool` → redact (mirror `redactedAPIKey`, cmd/aura/config.go:24). Compare-to-sample is against a *public* constant, so `==` is fine (no timing concern — note in §Security).

## Code Examples

### KnobSpec shape (recommended hybrid — data registry + generic kind pass)
```go
// internal/config/config_knobs.go  (illustrative — planner finalizes)
type KnobKind int
const ( KindString KnobKind = iota; KindInt; KindBool; KindEnum )

type KnobSpec struct {
    Name    string     // env key, e.g. "AURA_AGUI_CORS_PERMISSIVE"
    Kind    KnobKind
    Default string     // documented default (drives .env.example / doc gen, D-08)
    Enum    []string   // valid set when Kind==KindEnum
    Secret  bool       // redact in `config validate` output
}

// knobRegistry is the single source of truth (D-08). One slice literal.
func knobRegistry() []KnobSpec { return []KnobSpec{
    {Name: "AURA_AGUI_BUFFER_CAP", Kind: KindInt, Default: "64"},
    {Name: "AURA_AGUI_CORS_PERMISSIVE", Kind: KindBool, Default: "false"},
    {Name: "AURA_AUTHULA_SECRET", Kind: KindString, Default: "", Secret: true},
    // ... Tier A + Tier B
}}
```

### Generic F-016 re-parse pass (zero per-knob code; D-06/D-07)
```go
// internal/config/config_validate.go
type Severity int
const ( Warn Severity = iota; Fatal )
type Violation struct{ Knob string; Sev Severity; Msg string }

func reparsePass(p RuntimeProfile) []Violation {
    sev := Warn
    if p.Tier() == tierStrict { sev = Fatal }     // hardened/prod ⇒ fatal (D-07)
    var vs []Violation
    for _, k := range knobRegistry() {
        raw, set := os.LookupEnv(k.Name)
        if !set || strings.TrimSpace(raw) == "" { continue } // unset = OK (uses default)
        switch k.Kind {
        case KindInt:
            if _, err := strconv.Atoi(strings.TrimSpace(raw)); err != nil {
                vs = append(vs, Violation{k.Name, sev, "not a valid integer"})
            }
        case KindBool:
            if _, err := strconv.ParseBool(strings.TrimSpace(raw)); err != nil {
                vs = append(vs, Violation{k.Name, sev, "not a valid boolean"})
            }
        case KindEnum:
            if !slices.Contains(k.Enum, raw) {
                vs = append(vs, Violation{k.Name, sev, "not one of " + strings.Join(k.Enum, ",")})
            }
        }
    }
    return vs
}
```

### Bespoke gate, mirroring GuardWebBind (F-007 sample-cred reject)
```go
// internal/config/config_validate.go — pure, total, table-testable, names the knob.
func (c *Config) gateObjectStoreCreds(p RuntimeProfile) []Violation {
    if p.Tier() != tierStrict { return nil }      // dev/local_trusted: OK (D-10/D-11)
    var vs []Violation
    if c.ObjectStoreAccessKey == defaultObjectStoreAccessKey {
        vs = append(vs, Violation{"AURA_OBJECTSTORE_ACCESS_KEY", Fatal,
            "sample object-store access key rejected under " + string(p)})
    }
    if c.ObjectStoreSecretKey == defaultObjectStoreSecretKey {
        vs = append(vs, Violation{"AURA_OBJECTSTORE_SECRET_KEY", Fatal,
            "sample object-store secret key rejected under " + string(p)})
    }
    return vs
}
```

### D-12 destructive-shell fix (the EXACT one-spot diff)
```go
// internal/agent/tools/shell_exec_env.go destructiveShellPatterns()
// BEFORE:  if !set { return defaultDestructivePatterns, nil }
//          if raw == "" || strings.EqualFold(raw, "off") { return nil, nil }
// AFTER (D-12):
raw, set := os.LookupEnv(envShellDestructivePatterns)
raw = strings.TrimSpace(raw)
if !set || raw == "" {                       // unset OR empty → built-in defaults (gate ACTIVE)
    return defaultDestructivePatterns, nil
}
if strings.EqualFold(raw, "off") {           // ONLY explicit "off" disables
    return nil, nil
}
// ... parse comma-separated custom patterns (UNCHANGED)
```
Also update the doc comments (shell_exec_env.go:16-18 and 86-88) and `.env.example:57-61` to read "empty = use defaults; only `off` disables."

### CLI subcommand wiring (slots beside show|get|set)
```go
// cmd/aura/config.go runConfig switch — add:
case "validate":
    configValidate(args[1:])   // implemented in cmd/aura/config_validate.go
// configValidate: flag.NewFlagSet, --profile (override AURA_PROFILE, D-02), --json;
//   load full config (config.LoadServe), call ValidateProfile, render, os.Exit(1) if any Fatal.
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `missing []string` + `strings.Join` (current `Validate()`) | `errors.Join(errs...)` (Go 1.20+) for independent errors | Go 1.20 | Either is fine; **recommend keeping `missing []string`/`[]Violation`** for parity with `Validate()` and because the CLI needs structured per-knob items (severity, name) for table/json rendering, which a flat joined error can't give cleanly. |
| `synctest.Run` (Go 1.24 experimental) | `synctest.Test` (Go 1.25+/1.26) | Go 1.25 | N/A here (no goroutines/timers in validators). |
| Manual `b.N` benchmarks | `b.Loop()` (Go 1.24+) | Go 1.24 | N/A (no benchmarks needed). |

**`validator/v10` verdict (Claude's Discretion, D-row):** **REJECT for this phase.** Rationale: (1) the rules are *profile-conditional and cross-field* (`CORS=true forbidden only under prod`; `non-loopback bind requires auth OR trustProxy`) — `validator/v10` struct tags express *static field-level* rules, so cross-field/conditional logic is written as `StructLevel`/`RegisterValidation` Go funcs anyway = **more** ceremony, not less; (2) it would break the codebase's clean, table-tested `GuardWebBind`/`Validate` idiom; (3) its strengths (declarative field rules, i18n) aren't needed — we need a `profile → []Violation` aggregator that names each knob. CONTEXT default ("hand-rolled unless tags clearly reduce LOC") resolves to hand-rolled. The only "engine" is the trivial kind-driven re-parse loop, which is ~20 LOC of stdlib, not a framework.

**`--json` rendering recommendation:** Implement it. It's ~15 LOC (`json.NewEncoder(os.Stdout).Encode(violations)`), the milestone is CI-heavy (REL-01/OBS), and it makes `config validate` usable as a CI gate. Not required by acceptance, but high value/low cost. Human table is the default; `--json` opt-in.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `replication_factor` is validated via a **new** `AURA_OBJECTSTORE_REPLICATION_FACTOR` env knob (default 1), not by parsing garage.toml | §Open Q1, Matrix | If the planner instead parses garage.toml, the file path inside the container must be known/shipped; an env knob may drift from the toml Garage actually reads. **Needs user/planner confirm.** |
| A2 | `single_user_hardened` should ALSO forbid permissive CORS (CONTEXT locks only prod) | Matrix row CORS | If hardened must allow permissive CORS, the matrix cell changes to OK/WARN. Researcher recommendation, not a locked decision. |
| A3 | `single_user_hardened` ALLOWS destructive-shell `off` (D-11 forbids it only under prod) | Matrix row destructive-off | An appliance tier allowing the destructive gate to be disabled may be surprising; confirm this is intended for the DGX bundle. |
| A4 | dev↔local_trusted differ only by label/diagnostic verbosity (no functional gate delta) | Matrix tier-collapse, Open Q2 | If local_trusted must flip a gate, it can't be collapsed with dev. CONTEXT D-09 supports A4; confirming closes Open Q2. |
| A5 | `GARAGE_RPC_SECRET` reject baseline = "non-empty under hardened/prod" (no Go sample constant exists) | §Knob Catalogue, Matrix | A literal sample RPC secret may live in `scripts/garage_bootstrap.sh`; planner should grep it to extend the reject set. |
| A6 | Default bucket (`aura-assets`) / loopback endpoint under prod = WARN (not FATAL) | Matrix | They may be legitimate behind compose DNS; D-11 lists them but treating as hard-fail could block valid prod deploys. |

**These are the items `/gsd-discuss-phase` or the planner must confirm before they become locked.** A1/A4 are the highest-leverage.

## Open Questions

1. **How does `config validate` read `replication_factor` (PROF-06) and `GARAGE_RPC_SECRET` (PROF-03)?** Neither is in `Config` today. `replication_factor = 1` is hardcoded in `docker/garage/garage.toml:5`; `GARAGE_RPC_SECRET` is an env var read only by compose (`${...:?}`), never into Go.
   - What we know: this is the **one genuinely net-new read** the phase needs (everything else gates already-wired knobs).
   - Recommendation: introduce `Config.ObjectStoreReplicationFactor int` from `AURA_OBJECTSTORE_REPLICATION_FACTOR` (convention-compliant, default 1) and `Config.GarageRPCSecret string` from `os.Getenv("GARAGE_RPC_SECRET")` (upstream name per CLAUDE.md sidecar exception). This stays inside the scope fence (config-contract validation, NOT runtime enforcement). Note the drift risk: the env knob is *declared intent*; keeping `garage.toml` in sync (e.g. install.sh templating it) is a deployment follow-on, optionally a small Phase 33 task or deferred. **Confirm with planner/discuss.**

2. **Exact dev ↔ local_trusted delta.** CONTEXT hypothesis (D-09): diagnostic verbosity / intent labeling only; both warn-on-invalid, neither flips gates.
   - Recommendation: **confirm the hypothesis** — implement both as one *lenient* tier (identical constraint column) differing only by a label/banner (e.g. local_trusted prints "trusted local mode — full host capability active"). Do not maintain two identical rule sets.

3. **Should hardened forbid permissive-CORS and destructive-`off`?** (A2/A3.) D-11 locks these only for prod. Recommend forbidding permissive-CORS under hardened too (consistency, cheap); leave destructive-`off` as A3 to confirm.

4. **Catalogue cut (Tier A+B vs +C).** Recommended Tier A+B (the `internal/config` surface + gates). Tier C (agent-tools/loop/llm knobs) is catalog-able by name without moving reads but enlarges the registry — confirm the cut.

## Environment Availability

> Pure-Go config/CLI phase. **No runtime services required** — validation reads env vars + Go constants; it does not dial Postgres/Neo4j/Garage. The integration test stack is irrelevant to this phase's unit/PBT tests.

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | build/test | ✓ | 1.26.4 (go.mod:3) | — |
| `pgregory.net/rapid` | PBT (env re-parse) | ✓ direct dep | 1.3.0 (go.mod:40) | example-based tests |
| `go-playground/validator/v10` | (rejected; available) | ✓ indirect | 10.30.3 | — |
| golangci-lint | lint gate | ✓ | v2.12.2 (CI-pinned, CLAUDE.md) | — |
| go-mutesting | mutation ≥70% | ✓ (WSL only) | per CLAUDE.md | manual mutation reasoning |
| Garage/Postgres/Neo4j running | — | n/a | — | not needed (no dialing) |

**Missing dependencies with no fallback:** none.
**Missing dependencies with fallback:** none.

## Validation Architecture

> Nyquist validation is ENABLED (`config.json workflow.nyquist_validation: true`). This section is consumed verbatim by VALIDATION.md generation and Dimension-8 plan-checking.

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` (table-driven) + `pgregory.net/rapid` v1.3.0 (PBT) |
| Config file | none (Go convention); `.golangci.yml` for lint |
| Quick run command | `go test ./internal/config/ ./cmd/aura/ ./internal/agent/tools/` |
| Full suite command | `make quality` (vet+build+file-size+lint+test-race+vuln); `make quality-full` adds the coverage gate |
| Mutation | `go-mutesting` on `config_validate.go`, `config_knobs.go` in WSL (≥70% killed) |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| PROF-01 | `validate --profile server_production` exits non-zero, lists every unmet req | unit + e2e | `go test ./cmd/aura/ -run TestConfigValidate_ServerProduction -x` | ❌ Wave 0 (`cmd/aura/config_validate_test.go`) |
| PROF-01 | `ValidateProfile` aggregates ALL violations (not first-fail) | unit (table) | `go test ./internal/config/ -run TestValidateProfile -x` | ❌ Wave 0 (extend `config_validate_test.go`) |
| PROF-02 | destructive truth table: unset/empty/`off`/`OFF`/custom/copied-sample | unit (table) | `go test ./internal/agent/tools/ -run TestDestructiveShellPatterns -x` | ❌ Wave 0 (extend `shell_exec_env_test.go`) |
| PROF-03 | sample creds + empty RPC secret rejected under hardened/prod, pass when supplied | unit (table) | `go test ./internal/config/ -run TestGateObjectStore -x` | ❌ Wave 0 (`config_validate_test.go`) |
| PROF-04 | invalid int/bool ⇒ FATAL under hardened/prod, WARN under dev/local_trusted | unit (table) + **PBT** | `go test ./internal/config/ -run 'TestReparse|TestRapidEnv' -x` | ❌ Wave 0 (`config_knobs_test.go`) |
| PROF-05 | non-absolute `AURA_RUN_DIR` ⇒ RunDirErr surfaced by validator | unit | `go test ./internal/config/ -run TestRunDir -x` | ✓ partial (`config_rundir_test.go`) — extend |
| PROF-06 | `replication_factor` (or new knob) =1 rejected under prod, ≥2 passes | unit (table) | `go test ./internal/config/ -run TestGateReplication -x` | ❌ Wave 0 (depends on Open Q1) |
| QUAL-04 | every cataloged knob has a registry row; registry round-trips defaults | unit | `go test ./internal/config/ -run TestKnobRegistry -x` | ❌ Wave 0 (`config_knobs_test.go`) |
| (D-01/D-03) | `AURA_PROFILE` unset → dev; override via `--profile` | unit (table) | `go test ./internal/config/ -run TestParseProfile -x` | ❌ Wave 0 (`config_runtimeprofile_test.go`) |

### Sampling Rate
- **Per task commit:** `go test ./internal/config/ ./cmd/aura/ ./internal/agent/tools/` (+ `-race` on touched pkgs per CLAUDE.md Gate 2).
- **Per wave merge:** `make quality` (whole-tree vet/build/lint/test-race/vuln).
- **Phase gate:** `make quality-full` green (coverage ≥85% owned surface) + mutation spot-check ≥70% on the validator files, before `/gsd-verify-work`.

### Property-Based Testing target (rapid)
The registry-driven re-parse is the textbook PBT case (validator/parser/normalization per the property-based-testing skill). Invariants to encode with `rapid`:
1. **Strictness invariant:** for any cataloged int/bool knob and any string that fails `strconv` parse, `ValidateProfile(strict)` yields a **Fatal** violation naming that knob, and `ValidateProfile(lenient)` yields at most a **Warn** (never Fatal) — over randomly generated profile×knob×garbage-value triples.
2. **No-false-positive invariant:** for any cataloged knob and any *valid* value of its kind, the re-parse pass yields **no** violation for that knob.
3. **Aggregation invariant:** `len(ValidateProfile)` violations is monotonic — adding a second bad knob never *removes* the first's violation (proves "lists every unmet requirement", criterion #1).
Generate via `rapid.Check(t, func(t *rapid.T){...})` with `rapid.SampledFrom(profiles)` + `rapid.String()`.

### Destructive-shell truth table (D-12, the literal cases)
| `AURA_SHELL_DESTRUCTIVE_PATTERNS` | LookupEnv set | result | gate |
|-----------------------------------|---------------|--------|------|
| (unset) | false | `defaultDestructivePatterns` | ACTIVE |
| `""` (empty) | true | `defaultDestructivePatterns` | ACTIVE ← **the fix** |
| `"  "` (whitespace) | true | `defaultDestructivePatterns` | ACTIVE (TrimSpace) |
| `"off"` / `"OFF"` / `"Off"` | true | `nil` | DISABLED (case-insensitive) |
| `"rm -rf /tmp/x,mkfs"` (custom) | true | compiled custom | ACTIVE (custom) |
| copied `.env.example` (`#AURA_SHELL_...` commented) | false | `defaultDestructivePatterns` | ACTIVE (criterion #2) |

### End-to-end assertion (criterion #1)
Construct a `Config` with sample object-store creds + empty Authula secret + permissive CORS + replication 1 + destructive `off`, call `ValidateProfile(server_production)`, assert: returns ≥6 Fatal violations, the rendered output **contains each offending knob name** (mirror `TestGuardWebBind`'s `strings.Contains(msg, "AURA_AUTHULA_SECRET")` assertion), and the CLI path exits non-zero.

### race / goleak
Validators and `ParseProfile`/registry are **pure, no goroutines** → goleak adds little (apply only if the package's `TestMain` already wires it). Run `-race` per CLAUDE.md on every touched package regardless (cheap, catches accidental shared state in test helpers).

### Wave 0 Gaps
- [ ] `internal/config/config_runtimeprofile_test.go` — `TestParseProfile` (PROF-01/D-01/D-03)
- [ ] `internal/config/config_knobs_test.go` — `TestKnobRegistry`, `TestReparsePass`, rapid invariants (PROF-04/QUAL-04)
- [ ] `internal/config/config_validate_test.go` — EXTEND with `TestValidateProfile`, `TestGateObjectStore`, `TestGateReplication` (PROF-01/03/06)
- [ ] `internal/agent/tools/shell_exec_env_test.go` — EXTEND with `TestDestructiveShellPatterns` truth table (PROF-02)
- [ ] `cmd/aura/config_validate_test.go` — `TestConfigValidate_ServerProduction` exit-code + knob-name output (PROF-01 e2e)
- [ ] Framework install: none (rapid already a dep)

## Security Domain

> `security_enforcement` is absent from `.planning/config.json` ⇒ treated as ENABLED. This phase IS a security control (it is the config-contract that refuses unsafe production deploys).

### Applicable ASVS Categories
| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V14 Configuration | **yes (core)** | Secure-by-default + fail-fast on unsafe config — the entire phase. Profiles refuse sample creds, permissive CORS, single-replica, disabled destructive gate. |
| V5 Validation, Sanitization & Encoding | yes | The registry-driven env re-parse (PROF-04) — strict parse under strict tiers; stdlib `strconv` (no hand-rolled parser). |
| V6 Stored Cryptography | partial | Secret *presence/sample* checks only — **no crypto is performed**; do NOT hand-roll any hashing/derivation. Comparison to the *public* sample constant uses `==` (no secret involved → constant-time NOT required; note inline). |
| V7 Error Handling & Logging | yes | `config:`-prefixed, lowercase, knob-naming messages (golang-error-handling skill). **Redact secret knobs** in output (V8) — never log `AURA_AUTHULA_SECRET`/object-store keys. |
| V8 Data Protection | yes | `KnobSpec.Secret` flag → redact in table/json (mirror `redactedAPIKey`). |

### Known Threat Patterns for this stack
| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Sample/default object-store creds reused in prod (F-007) | Elevation of Privilege | Reject `value == defaultObjectStore*Key` under hardened/prod (PROF-03). |
| Permissive CORS + no-auth loopback → drive-by browser RCE-by-proxy (F-022) | Spoofing / EoP | Forbid `AURA_AGUI_CORS_PERMISSIVE=true` under prod (full allowlist is Phase 40/SEC-02). |
| Destructive-shell gate silently disabled by copying `.env.example` (F-002) | Tampering | D-12 empty→defaults; prod forbids explicit `off` (PROF-02). |
| Unauthenticated non-loopback cockpit bind | Spoofing | Reuse `GuardWebBind` (existing); web-auth required under hardened/prod. |
| Secret value leaked into validate output / CI logs | Information Disclosure | `Secret` flag → redact. **Fail-closed:** any unmet requirement ⇒ non-zero exit, never fail-open. |
| Silent int/bool fallback hiding a misconfigured security knob (F-016) | Tampering / repudiation | Strict re-parse under strict tiers (PROF-04). |

**Operator trust boundary note:** the env is operator-controlled and the operator is trusted (single-tenant appliance / identity-isolation model, no RBAC). The threat model here is **accidental misconfiguration**, not a malicious operator — hence "refuse + name the knob" (D-05) rather than sandboxing the config source.

## Sources

### Primary (HIGH confidence — direct source reads, this session)
- `internal/config/config.go` (557 LOC) — `Config`, `Validate()` (L265-280), `GuardWebBind()` (L292-308), `absRunDir`/`RunDirErr` (L519-532), `defaultObjectStore*Key` (L36-37), `loadBase()` (L321-479).
- `internal/config/config_env.go` (41) — `envDefault`/`envSliceDefault` (int/bool moved to envutil; F-016 ref is stale here).
- `internal/config/config_validate_test.go` (36), `config_profile_test.go` (37, Agent.md collision), `config_webauth_test.go` (108, `TestGuardWebBind` pattern + `clearPostgresEnv`).
- `internal/envutil/envutil.go` (48) — `IntDefault`/`BoolDefault` leaf (stays unchanged; comment defers agent-tool knob pull).
- `internal/agent/tools/shell_exec_env.go` (150) — `destructiveShellPatterns()` (L108-131), default patterns.
- `cmd/aura/config.go` (286) — `runConfig` switch, `redactedAPIKey`, usage/exit idiom.
- `.env.example` (lines 57-61 destructive, 199-211 object-store/`GARAGE_RPC_SECRET`/Garage knobs).
- `docker/garage/garage.toml` (`replication_factor = 1` at L5), `compose.yaml` (L132/231/237/291 `${VAR:?}` guards).
- `docs/audit/bug-report.md` F-002 (L20), F-007 (L119), F-016 (L294), F-018 (L330), F-022 (L403), F-026 (L476), F-041 (L777).
- `docs/audit/action-plan.md` (L47-52 object-store creds acceptance; L77-82 profiles acceptance — criterion #1 quote).
- `docs/audit/architecture-review.md` (L43/47/61-68 — policy-first scope fence: profiles #3 ≠ gateway #2 ≠ sandbox #6).
- `.planning/REQUIREMENTS.md` (PROF-01..06 L16-21, QUAL-04 L116, traceability L157-158), `.planning/ROADMAP.md` (Phase 33 detail L178-189; QUAL-04 phasing note L111), `.planning/config.json` (nyquist on, security default).
- `go.mod` (go 1.26.4 L3; `pgregory.net/rapid v1.3.0` L40; `validator/v10 v10.30.3` indirect L104).
- Knowledge graph (`graphify query`): confirmed `GuardWebBind`@config.go:292 + test, `destructiveShellPatterns`@shell_exec_env.go:108 — no new cross-doc relationships for this internal-config phase.
- User skills: golang-testing, property-based-testing, mutation-testing, golang-error-handling, golang-security, golang-lint, golang-structs-interfaces.

### Secondary (MEDIUM)
- CLAUDE.md (≤600 LOC cap, coverage ≥85% floor, env convention `AURA_<DOMAIN>_<UNIT>`, go-mutesting, golangci-lint v2.12.2, minimal-industrial directive).
- MEMORY.md (file-size hook scans whole tree; gsd commit wrapper file-size timeout — use direct `git commit`; never run `.exe` on host).

### Tertiary (LOW — none)
- No web sources needed; this phase is entirely internal Go.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — no new deps; both relevant libs verified in `go.mod`.
- Architecture / file split: HIGH — config.go LOC measured (557/600), existing split precedent (`config_mcp.go`/`config_routes.go`), patterns mirror live tested code (`GuardWebBind`).
- Profile rule matrix: HIGH for locked cells (D-09..D-12); MEDIUM for A2/A3/A6 (researcher recommendations flagged in Assumptions Log).
- replication/RPC-secret read path: MEDIUM — confirmed the gap (not in `Config`), recommendation flagged as Open Q1 / A1 for confirmation.
- Pitfalls: HIGH — every pitfall grounded in a specific file/line.

**Research date:** 2026-06-30
**Valid until:** 2026-07-30 (stable internal domain; only invalidated by edits to config.go/shell_exec_env.go/garage.toml or a profile-design change).
