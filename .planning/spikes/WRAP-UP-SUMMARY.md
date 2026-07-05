# Spike Wrap-Up Summary

**Latest wrap-up:** 2026-07-05 (Session-23, spikes 086–089 — append mode).
**Cumulative:** 90 spikes wrapped into `./.claude/skills/spike-findings-Aura/` across sessions 1–23. The full per-spike record (all verdicts, tags, session narrative) lives in `.planning/spikes/MANIFEST.md` and the skill's `<metadata>` `processed_spikes` list; per-area implementation blueprints are in `references/*.md`.

## This run (Session-23, 086–089) — Casbin authz engine + org-roles (the deferred forward bet)

| # | Name | Type | Verdict |
|---|------|------|---------|
| 086 | casbin-hascapability-backing | standard | VALIDATED ✓ (casbin/v2 backs HasCapability byte-identically, 15/15 vs SQL oracle; zero-rework swap) |
| 087a | casbin-adapter-pgx-native (pckhoi) | comparison | WINNER ✓ (native pgx, shares Aura's pool, migrate owns table, per-tenant filtered load; live Postgres) |
| 087b | casbin-adapter-sql-stdlib (Blank-Xu) | comparison | RUNNER-UP ✓ (works over database/sql but needs a bridge to share the pool; no per-tenant domains load) |
| 088 | casbin-rbac-domains-orgroles | standard | VALIDATED ✓ (per-dept manager/employee/viewer, domain-scoped hierarchy, cross-domain isolation) |
| 089 | casbin-nethttp-management-api | standard | VALIDATED ✓ (drops into RequireCapability; runtime grant/revoke live in-process; 086 guard closed) |

Grounds the Phase-36-deferred Casbin phase (CONTEXT §Deferred, D-04) end-to-end over Aura's real stack. Every kill-risk cleared — the "defer via the `HasCapability` interface, zero rework" premise is confirmed safe.

**Decisions locked:** engine = **casbin/v2** (v2.135.0, most-stable-fit — a "use all v3" detour was reverted because the v3 pgx adapter ecosystem is weaker for Aura); adapter = **pckhoi/casbin-pgx-adapter/v3** (native pgx, `WithConnectionPool`+`WithSchema("aura")`+`WithSkipTableCreate` → golang-migrate owns `aura.casbin_rule`); ONE RBAC-with-domains superset model serves both flat caps AND org-roles; management API reuses `identity.ValidateCapabilityName`. **PRD-amendment required before implementation.**

**Sharp edges:** trim trailing empty pckhoi `Filter` columns (trailing-`AND` builder bug); `*`+grammar guards live in the management API, not the model; Watcher only for multi-instance.

Updated skill artifacts: `references/casbin-authz.md` (new), `sources/086..089-*/`, feature-area index row, `processed_spikes` 086–089, the Casbin Requirements bullet + wrapped-session line in `SKILL.md`. Blueprint: `references/casbin-authz.md`.

## Prior run (Session-22, 082–085) — Multi-user per-identity isolation, all four planes proven live

| # | Name | Type | Verdict |
|---|------|------|---------|
| 082 | agent-sandbox-realsource-contract | standard | VALIDATED ✓ (real source + live kind run; corrects 079) |
| 083 | two-identity-e2e-tenancy | integration | VALIDATED ✓ (box+Garage+memory together; closes 080/081 tiers) |
| 084 | per-identity-pim-sidecar | standard | VALIDATED ✓ (2-instance live; the 3rd MCP class) |
| 085 | document-ingest-tenancy | standard | VALIDATED ✓ (leak→fix live; the 4th plane) |

Skill artifacts from that run: `references/multiuser-per-identity-isolation.md` (extended), `sources/082..085-*/`, feature-area index row, `processed_spikes` 082–085, the multi-user Requirements bullet in `SKILL.md`.
