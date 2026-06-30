# Phase 33: Runtime Profiles + Config Validation - Context

**Gathered:** 2026-06-30
**Status:** Ready for planning

<domain>
## Phase Boundary

Add **4 named runtime profiles** (`dev` / `local_trusted` / `single_user_hardened` / `server_production`) to `internal/config`, plus an `aura config validate --profile <p>` CLI that fails fast and lists **every** unmet requirement, a **Go registry catalogue** of hot-path `AURA_*` knobs, and 6 config-correctness fixes: F-002 (destructive-shell gate), F-007 (sample object-store creds), F-016 (invalid-env behavior), F-018 (Garage replication=1), F-026 (profile selection), F-041 (RunDir absolute — partly landed).

**This phase is the config-contract + validation layer of the audit's "policy-first runtime" vision.** It delivers: (1) a runtime-readable profile, (2) profile-aware config validation that refuses unsafe production deploys, (3) the invalid-env fail-fast/warn split, and (4) flipping the *already-wired* safety gates under hardened/production.

**It does NOT deliver** per-profile *runtime enforcement* of tool capabilities, file-path fences, sandbox selection, or network egress — that is the **Tool Gateway (Phase 34+)**. See Deferred Ideas. Keeping these separate is the load-bearing scope decision (Q2).

Requirements PROF-01..06 + QUAL-04 are tightly locked (see `.planning/REQUIREMENTS.md`); this discussion captured only HOW, not WHAT.

</domain>

<decisions>
## Implementation Decisions

### Profile selection & default (PROF-01 / F-026)
- **D-01:** Active profile is a **new `AURA_PROFILE` env read at config load** — a real runtime field on `Config`, NOT a CLI-only flag. PROF-04 (fail-fast vs warn by profile at boot) requires the profile be readable at runtime, so a validation-only flag is insufficient.
- **D-02:** `aura config validate --profile <p>` accepts an explicit `--profile` that **overrides** `AURA_PROFILE` for the validation run (so an operator can lint any profile without changing their env).
- **D-03:** **Default when `AURA_PROFILE` is unset = `dev`** — loudest diagnostics, most permissive, preserves today's full-host behavior exactly (success criterion #4). Tightening is always an explicit opt-in; an unset profile never breaks the current daily CLI/REPL/serve flow.

### Posture scope — what profiles do in THIS phase (Q2)
- **D-04:** Profiles **validate config AND flip the cheap already-wired gates**. NOT validate-only, NOT the net-new gateway. The net-new tool/capability enforcement is deferred to the Tool Gateway phase.
- **D-05:** Enforcement mechanism = **fail validation; operator fixes the env.** When a flipped gate is violated under a tightened profile, `config validate` / boot **refuses and names the offending knob**. ONE consistent mechanism (identical posture to the existing missing-secret fail-fast). **No silent coercion, no coerce+warn** — the operator's env is the source of truth; we refuse unsafe values, we never override them to a different value.

### Invalid-env behavior (PROF-04 / F-016)
- **D-06:** Mechanism = **separate validation pass**. `internal/envutil` stays a **dumb silent-fallback leaf** for runtime reads (unchanged). A separate validation pass — **driven by the knob catalogue (D-08)** — re-parses each cataloged knob and collects diagnostics. Do NOT make envutil profile-aware (rejected: invasive, couples the leaf to profile state across every call site).
- **D-07:** Strictness = **all-strict in prod, no security-subset taxonomy.** Under `single_user_hardened` / `server_production`, **ANY** invalid cataloged value is a **fatal error**. Under `dev` / `local_trusted` it **warns** (with diagnostics) and falls back. No curated "security/reliability subset" to maintain — a typo in production should never silently degrade.

### Knob catalogue (QUAL-04)
- **D-08:** Catalogue = **Go registry as single source of truth.** A `[]KnobSpec{name, kind (int/bool/string/enum), default, profileConstraints}` slice that drives (a) the invalid-env validation pass (D-06), (b) `config validate` output, and (c) optionally `.env.example` / doc generation. One source kills validate-vs-docs drift. Rejected: doc-only markdown (two sources of truth that drift). The registry IS the validation engine — upfront code, but not duplicated.

### Profile rule matrix (Q2 / Q3 follow-up)
- **D-09:** `dev` (default) and `local_trusted` **preserve today's full-host behavior unchanged** (success criterion #4): invalid env → warn only, **no gate flips**, full-host shell/tools intact. The dev↔local_trusted delta is minor (diagnostic verbosity / intent labeling) — see Open Questions for the planner.
- **D-10:** `single_user_hardened` (the single-operator appliance tier, e.g. the DGX Spark bundle) **keeps secret/auth hardening, relaxes redundancy**: requires real secrets + web-auth + rejects sample creds + invalid-env fail-fast (like prod), **BUT allows single-replica object store and single-node/loopback topology**.
- **D-11:** `server_production` = hardened + **additionally requires replication ≥ 2** and the **non-loopback-with-auth** posture. The validation set it **REJECTS**:
  - Sample object-store creds + RPC secret (F-007 / PROF-03): `defaultObjectStoreAccessKey`/`defaultObjectStoreSecretKey`, empty/sample `GARAGE_RPC_SECRET`, default bucket/endpoint.
  - Garage `replication_factor = 1` (F-018 / PROF-06) — non-durable, documented dev-only.
  - Permissive CORS — `AURA_AGUI_CORS_PERMISSIVE=true` (audit bug-report finding).
  - Destructive-shell gate set to `off` — `AURA_SHELL_DESTRUCTIVE_PATTERNS=off` (F-002; the empty=defaults fix below applies to ALL profiles; this *additionally* forbids explicit-off in prod).
  - Plus the locked PROF requirements: required secrets unset, RunDir not absolute (F-041 / PROF-05), non-loopback bind without auth (reuse `GuardWebBind`).

### F-002 destructive-shell semantics fix (PROF-02) — applies to ALL profiles
- **D-12:** Flip `destructiveShellPatterns()` in `internal/agent/tools/shell_exec_env.go`: **empty `AURA_SHELL_DESTRUCTIVE_PATTERNS` → use built-in defaults** (gate stays ACTIVE), **only explicit `off` disables.** Today empty=disabled, which means copying `.env.example`→`.env` could silently kill the gate. `.env.example` line 61 is already commented out; the test matrix must cover unset / empty / `off` / custom / copied-sample. This is a correctness fix independent of profile (then prod additionally forbids `off` per D-11).

### Resolved post-research (2026-06-30 — Open Q1–Q4 confirmed with user after RESEARCH.md)
- **D-13 (PROF-03/PROF-06 read path — was Open Q1/A1, highest leverage):** Validate Garage durability via a **new env knob**, NOT by parsing `garage.toml`. Add `AURA_OBJECTSTORE_REPLICATION_FACTOR` (default `1`) → `Config.ObjectStoreReplicationFactor int`, and `Config.GarageRPCSecret string` from `os.Getenv("GARAGE_RPC_SECRET")` (upstream name, CLAUDE.md sidecar exception). This is the **one genuinely net-new config read** the phase needs; it stays inside the config-contract scope fence (declared intent, NOT runtime enforcement). Drift caveat: keeping `docker/garage/garage.toml:5` in sync with the knob is a **deployment follow-on, NOT required for this phase's acceptance** (defer unless trivially templated in install).
- **D-14 (dev↔local_trusted delta — was Open Q2/A4):** **Collapse to one lenient tier.** `dev` and `local_trusted` share an **identical constraint set** (warn-on-invalid, no gate flips, full-host intact); they differ ONLY by an intent label/banner (e.g. `local_trusted` prints "trusted local mode — full host capability active"). Do NOT maintain two identical rule sets. (Confirms D-09 hypothesis.)
- **D-15 (hardened gate strictness — was Open Q3/A2/A3):** `single_user_hardened` **additionally forbids permissive-CORS** (`AURA_AGUI_CORS_PERMISSIVE=true`) for consistency with prod (cheap), but **ALLOWS destructive-shell `off`** (appliance-operator flexibility). Only `server_production` forbids destructive `off` (per D-11). [A2 = forbid-CORS under hardened; A3 = allow-off under hardened.]
- **D-16 (catalogue cut — was Open Q4):** Knob catalogue = **Tier A+B only** — the `internal/config` surface + the gated knobs (CORS, web-auth, sample object-store creds, `GARAGE_RPC_SECRET`, replication factor, destructive-shell, required DB/Neo4j secrets, RunDir, invalid-env int/bool knobs). **Tier C (agent-tools / loop / llm `AURA_*` knobs) is OUT** of this phase's registry.
- **A5/A6 (reject-set baselines) → planner adopts the researcher's recommended defaults:** `GARAGE_RPC_SECRET` reject baseline = **non-empty required under hardened/prod** (grep `scripts/garage_bootstrap.sh` for any literal sample secret to extend the reject set); default bucket (`aura-assets`) / loopback object-store endpoint under prod = **WARN, not FATAL** (legitimate behind compose DNS).

### Claude's Discretion
- Whether to lean on the already-indirect `go-playground/validator/v10` (v10.30.3) struct tags vs. the existing hand-rolled multi-error pattern — see Code Insights. RESEARCH.md recommends **hand-rolled** (rules are profile-conditional + cross-field, so tags add ceremony, not LOC savings). Default to the hand-rolled `Validate()`/`GuardWebBind()` idiom unless tags clearly reduce LOC.
- Exact `KnobSpec` field shape, profile-constraint representation, and how `config validate` renders the violation list (human table; consider a `--json` mode for CI, but not required by acceptance).

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Requirements & roadmap (authoritative WHAT)
- `.planning/REQUIREMENTS.md` — PROF-01..06 + QUAL-04 (exact requirement text + linked findings).
- `.planning/ROADMAP.md` §"Phase 33: Runtime Profiles + Config Validation" — goal + 4 success criteria.

### Audit findings (the F-026 contract — MUST read)
- `docs/audit/bug-report.md` §F-026 (lines ~476-493) — "Industrial deployment profile is not documented as a contract"; the full contract spans auth, secrets, tool caps, sandboxing, object storage, health checks, TLS, observability. Also: F-002 destructive-shell (~lines around shell gate), F-007 (~136-138), F-016 invalid int/bool (~309), F-018 replication=1 (~339-345), permissive-CORS finding (~414-420).
- `docs/audit/action-plan.md` §"Add production runtime profiles" (~77-82) and §"Add profile validation for object-store keys" (~49) — acceptance: `aura config validate --profile server_production` reports all unmet requirements.
- `docs/audit/architecture-review.md` §"Move toward a policy-first runtime" (~60-70) — establishes the SCOPE BOUNDARY: profiles (#3) are distinct from the Tool gateway (#2) and Sandbox layer (#6). Phase 33 = profiles-as-config-contract only.

### Code to extend / mirror (read before writing)
- `internal/config/config.go` — `Config` struct, `Validate()` (extend, don't replace — currently DB/Neo4j secrets + `RunDirErr`), `GuardWebBind()` (the pure/total/table-testable `config:`-prefixed pattern to mirror AND the non-loopback-auth gate to reuse), `absRunDir()`/`RunDirErr` (F-041 partly landed), `defaultObjectStoreAccessKey`/`defaultObjectStoreSecretKey` constants (F-007 reject targets).
- `internal/envutil/envutil.go` — `IntDefault`/`BoolDefault` silent-fallback leaf (keep dumb; the new pass re-parses for diagnostics).
- `internal/agent/tools/shell_exec_env.go` — `destructiveShellPatterns()` (F-002 semantics flip, D-12).
- `cmd/aura/config.go` — `show|get|set` dispatcher; add `validate` as a sibling subcommand.
- `.env.example` — line ~61 destructive-shell (already commented), lines ~200-211 object-store + `GARAGE_RPC_SECRET` (F-007 targets).

### Available dependency (don't reinvent)
- `go-playground/validator/v10 v10.30.3` — already an **indirect** dep; struct-tag validation usable with zero new direct dep if the planner wants it (Claude's discretion, D above).

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `config.Validate()`: extend with profile-aware checks; today only checks `DB.URL` / `Neo4j.Password` / `RunDirErr` — the new profile validation aggregates onto the same `missing []string`-style multi-error so it "lists every unmet requirement" (success criterion #1).
- `GuardWebBind(bind, authConfigured, trustProxy)`: pure, total, table-testable, `config:`-prefixed error — the exact pattern to mirror for new profile validators, AND directly reusable as the `server_production` non-loopback-auth requirement.
- `envutil.IntDefault/BoolDefault`: stays the runtime silent-fallback leaf; the catalogue-driven validation pass does the strict re-parse separately (D-06).
- `destructiveShellPatterns()`: one-spot semantics flip for F-002 (D-12).

### Established Patterns
- Fail-fast posture = aggregate into `missing []string`, return `fmt.Errorf("config: ...")`. No panics. Table-test-friendly pure functions.
- `cmd/aura` subcommand dispatch (see `runConfig` switch) — `validate` slots in beside `show|get|set`; usage to stderr + `os.Exit(1)` on bad usage.
- Env convention `AURA_<DOMAIN>_<UNIT>`; third-party sidecars keep upstream naming (so `AURA_PROFILE` is correct).
- "No-skip-as-green" / coverage floor ≥85% / table tests with realistic fixtures (CLAUDE.md gates apply).

### Integration Points
- `Config` gains a `Profile` field populated in `loadBase()` from `AURA_PROFILE` (default `dev`).
- The daemon/REPL boot path that already calls `Validate()` (note QUAL-04's double-`Validate` + pool-leak concern lives in Phase 34, but be aware of the call site) now routes through the profile-aware validation.
- `config validate` CLI reads the same `Config` + runs the profile validator; exit code non-zero on any violation.

</code_context>

<specifics>
## Specific Ideas

- User explicitly asked to **build on proven patterns, not reinvent** — hence the go-playground/validator availability note and the "extend Validate / mirror GuardWebBind" guidance over a new framework.
- "No atomic bombs / minimal industrial shape": the registry (D-08) is justified because it removes duplication (it IS the engine), not as gold-plating. Resist expanding profiles into the runtime enforcement gateway in this phase.
- `single_user_hardened` is explicitly the **DGX Spark / SMB appliance** bridge tier (one box, real secrets, but single-node OK).

</specifics>

<deferred>
## Deferred Ideas

These came up via the audit's full "policy-first runtime" vision but belong to LATER phases — do NOT pull into Phase 33:

- **Per-profile RUNTIME enforcement** of tool capabilities, file-tool path fences, sandbox selection, network egress policy → **Tool Gateway (Phase 34+)** (`architecture-review.md` #2/#6).
- **Durable mutating-tool ledger** (started/succeeded/failed; block mutating tools in prod if reservation fails) → Phase 34 LOOP (`action-plan.md` "Add durable mutating tool ledger").
- **Central capability policy engine** (actor × path × command × network × profile → deny/approve/execute) → Tool Gateway.
- **F-026 contract items not already wired as knobs** — TLS termination requirements, health-check endpoints as a hard gate, observability-required validation → future hardening phase. Phase 33 only gates the *already-wired* knobs (CORS, auth, creds, replication, destructive-off, secrets, RunDir).
- **QUAL-04 double-`Validate` + pool-leak fix and `askuser/store.go:231` int32 guard** are catalogued under QUAL-04 but mapped to **Phase 34** in REQUIREMENTS.md (the env-catalog slice of QUAL-04 is Phase 33; the correctness fixes are Phase 34). Confirm split during planning.

### Open question for the planner — RESOLVED
- ~~Define the precise `dev` ↔ `local_trusted` validation delta.~~ **Resolved by D-13..D-16 above** after RESEARCH.md: dev↔local_trusted collapse to one lenient tier differing only by label (D-14).

[No scope-creep ideas surfaced — discussion stayed within phase scope.]

</deferred>

---

*Phase: 33-runtime-profiles-config-validation*
*Context gathered: 2026-06-30*
