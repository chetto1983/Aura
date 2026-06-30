# Phase 33: Runtime Profiles + Config Validation - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-30
**Phase:** 33-runtime-profiles-config-validation
**Areas discussed:** Selection & default, Profile semantics, Invalid-env strictness, Knob catalogue form (+ user-requested research: online + D:/tmp curated repos to avoid reinventing the wheel)

---

## Research preface (user-requested)

User added a free-text item to the area selection: "search online and on D:/tmp curated repo how to build efficiently without reinventing the wheel." Findings that shaped the options below:
- `go-playground/validator/v10 v10.30.3` is already an **indirect** dependency — struct-tag validation available with zero new direct dep.
- The audit (`docs/audit/bug-report.md` F-026, `action-plan.md`, `architecture-review.md`) already specifies the deployment-profile contract and the policy-first-runtime scope boundary.
- Web (go.dev security best-practices, Viper guide, LabEx env-validation): validate config in CI pre-deploy; "always-valid-values" via validating constructor; profile read at load.

---

## Selection & default

| Option | Description | Selected |
|--------|-------------|----------|
| dev | Unset → dev: loudest diagnostics, most permissive, preserves today's full-host behavior exactly. | ✓ |
| local_trusted | Unset → local_trusted: every run gets a named posture; dev becomes explicit opt-in. | |
| Refuse boot if unset | Force operators to always name a profile; safest but high friction. | |

**User's choice:** dev (Recommended)
**Notes:** Profile is a runtime-readable `AURA_PROFILE` env (required by PROF-04), with `--profile` overriding for `config validate`.

---

## Profile semantics — posture scope

| Option | Description | Selected |
|--------|-------------|----------|
| Validate-only contract | Readiness linter + invalid-env fail/warn; NO runtime behavior change; all gating deferred to Tool Gateway. | |
| Validate + flip already-wired gates | Also enforce already-present gates (CORS, web-auth, sample creds); net-new gateway still deferred. | ✓ |

**User's choice:** Validate + flip already-wired gates
**Notes:** Follow-up locked the enforcement mechanism = fail validation (operator fixes env), no silent coercion. Production reject-set confirmed: sample creds + RPC secret (F-007), replication=1 (F-018), permissive CORS, destructive-shell `off` (F-002). `single_user_hardened` keeps secret/auth hardening but relaxes redundancy (single-replica + single-node OK); `server_production` adds replication≥2 + non-loopback-with-auth.

---

## Invalid-env strictness (F-016 / PROF-04)

| Option | Description | Selected |
|--------|-------------|----------|
| Separate pass, all-strict in prod | envutil stays dumb leaf; catalogue-driven pass; ANY invalid value fails in prod, warns in dev. | ✓ |
| Separate pass, security-subset only | Same pass but only a curated subset hard-fails; maintain the subset forever. | |
| Make envutil profile-aware | Thread profile into the leaf itself; invasive, couples leaf to profile. | |

**User's choice:** Separate validation pass, all-strict in prod (Recommended)

---

## Knob catalogue form (QUAL-04)

| Option | Description | Selected |
|--------|-------------|----------|
| Go registry as source of truth | []KnobSpec drives validation + `config validate` output + optional docs/.env.example gen. | ✓ |
| Doc-only markdown catalogue | docs/env-catalog.md; validation stays ad-hoc; two sources drift. | |

**User's choice:** Go registry as source of truth (Recommended)

---

## Claude's Discretion

- Whether to adopt `go-playground/validator/v10` struct tags vs. the existing hand-rolled `Validate()`/`GuardWebBind()` multi-error idiom (default to hand-rolled unless tags clearly reduce LOC).
- Exact `KnobSpec` field shape, profile-constraint representation, and `config validate` rendering (human table; optional `--json` for CI, not required).
- Precise `dev` ↔ `local_trusted` validation delta (likely diagnostic verbosity only; both preserve today's behavior).

## Deferred Ideas

- Per-profile RUNTIME enforcement of tool capabilities / path fences / sandbox / network egress → Tool Gateway (Phase 34+).
- Durable mutating-tool ledger → Phase 34 LOOP.
- Central capability policy engine (actor × path × command × profile).
- F-026 contract items not already wired as knobs (TLS, health-check hard gate, observability-required) → future hardening phase.
- QUAL-04 correctness fixes (double-Validate/pool-leak, askuser/store.go int32 guard) are mapped to Phase 34 in REQUIREMENTS.md; only the env-catalog slice of QUAL-04 is Phase 33 — confirm split at planning.
