# Phase 36: Multi-User Identity Isolation + Authula Cutover - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-05
**Phase:** 36-multi-user-identity-isolation-authula-cutover
**Areas discussed:** Scope split (36 vs 37), Admin model / Casbin, Provisioning & break-glass,
Job TTL & kill authority, Cutover & data migration, New-user provisioning bundle, Authula
first-login, Shared-sidecar vs per-identity MCP, Runner/session keying, Postgres RLS, Telegram
multi-user routing, Capability grant management, Two-identity E2E + CI, CLI/`local` surface,
User de-provisioning, Rollout/migration safety, Per-identity quotas, Admin audit visibility

---

## Admin model / authz engine (operator-raised: go-mizu, then Casbin)

| Option | Description | Selected |
|--------|-------------|----------|
| `settings.write` capability on existing seam | One flag on `capability_grants` | |
| Reuse `governance.write` | No new capability name | |
| Reconsider Casbin (PRD amendment) | Adopt a real authz engine now | (initial pick) |
| **Own spike + phase (final)** | Casbin deferred via swappable `HasCapability` interface | ✓ |

**User's choice:** Admin = capability grants on the existing seam for Phase 36; **Casbin
adopted as a forward bet on SMB org-roles but deferred to its own spike + phase** (PRD
amendment), reached via the `HasCapability` interface (zero rework).
**Notes:** Operator clarified the admin/user difference is narrow: "only admin can change model
settings + create users; **Telegram is per-user**." Driver for Casbin = the commercial
DGX-Spark SMB bundle eventually wanting real org-roles. Researched go-mizu (framework-coupled,
pre-v1 — rejected) and Apache Casbin (mature, right engine — deferred). Key insight:
`HasCapability` is already an interface, so Casbin is a later swap, not a now-or-never fork.

---

## Scope split (Phase 36 vs 37) + deny semantics

| Option | Description | Selected |
|--------|-------------|----------|
| Defer class-c to 37+ | 36 = enforced core only | ✓ |
| Include class-c in 36 | Full multi-user PIM now | |
| Machinery now, instances lazy | MountForIdentity resolves c, lazy spin-up | |

**User's choice:** Defer class-(c) PIM/WhatsApp instances to Phase 37+. Deny: **404 on read,
403 on mutate**.

---

## Provisioning, break-glass & first-login

| Option | Description | Selected |
|--------|-------------|----------|
| CLI-minted recovery (break-glass) | Host CLI mints short-lived reset | ✓ |
| Boot bootstrap code | One-time code at first boot | |
| Env-var admin bypass | Standing bypass credential | |
| One-time setup link (first-login) | Authula recovery-link, admin-delivered | |
| **Admin-set initial password + forced change (first-login)** | Temp password, change on first login + TOTP | ✓ |
| **Eager idempotent saga (bundle)** | Provision everything atomically at create | ✓ |
| Lazy per-resource (bundle) | Mint Garage/dirs on first use | |

**User's choice:** Break-glass = CLI-minted recovery. First-login = admin-set initial password
+ forced change + TOTP (Authula; no SMTP). Provisioning = eager idempotent in-process saga.
**Notes:** Ory Kratos referenced but rejected (Authula already embedded). No dedicated Go
provisioning library exists — lightweight in-process saga.

---

## Background jobs (TTL, kill authority)

| Option | Description | Selected |
|--------|-------------|----------|
| **1 hour TTL** | Tight resource bound, env-overridable | ✓ |
| 6 hours / 24 hours | Looser bounds | |
| **Owner session + admin (kill)** | Cross-session recovery via admin cap | ✓ |
| Owner session only | Tightest, TTL-only recovery | |

**User's choice:** 1h default TTL (env-overridable); poll/kill = owner session + admin capability.

---

## Cutover, documents-plane backfill & rollout

| Option | Description | Selected |
|--------|-------------|----------|
| **Operator stays `local`** | Existing data untouched; new users = fresh UUIDs | ✓ |
| Re-own local → new UUID | Migrate operator too | |
| **Backfill to operator, then flip** | Attach `:User` edges before fail-closed | ✓ |
| Clean slate (no backfill) | Existing docs invisible | |
| **Config flag, reversible (rollout)** | `AURA_MUSR_ISOLATION` gates the flip | ✓ |
| Migration-ordered only | No runtime flag | |

**User's choice:** Operator stays `local`; backfill existing docs to `local` then flip;
config-flag-gated reversible rollout. **Notes:** Spike 085 reviewed live — leak reproduced
through the production `Searcher`, fix mirrors memory `9a4ca594`. GO Feature Flag service
rejected as overkill.

---

## Postgres RLS (kernel-enforced isolation)

| Option | Description | Selected |
|--------|-------------|----------|
| **RLS + app-level scoping (defense-in-depth)** | Kernel backstop + `*ForIdentity` | ✓ |
| App-level only | Forgotten filter = silent leak | |
| RLS only | Drop app filters | |

**User's choice:** RLS + app-level scoping. **Notes:** go-saas/Go-Multitenancy rejected
(single-DB SaaS mismatch vs Aura's 3 substrates). RLS satisfies the spike's "storage/kernel-
enforced" non-negotiable; three planes = Postgres RLS / Neo4j `EXISTS{}` / Garage bucket.

---

## Shared-sidecar vs per-identity MCP / skills / runner keying

| Option | Description | Selected |
|--------|-------------|----------|
| **Global-managed + per-call scope key** | agent-memory always-on, admin-governed | ✓ |
| Per-identity toggleable | Each identity toggles shared infra | |

**User's choice:** agent-memory = one globally-managed always-on sidecar + mandatory
`user_identifier` scope key; per-identity enable/trust applies to class-(a) stdio only. Runner/
session state keyed by `(identity, session)` (locked implementation directive).

---

## Telegram routing, capability management, E2E CI, CLI/`local`

| Option | Description | Selected |
|--------|-------------|----------|
| **Reject + point to web linking (TG unknown)** | No agent for unlinked users | ✓ |
| Bot-initiated onboarding / silent ignore | Alternatives | |
| **CLI + settings-page UI (grant mgmt)** | Both surfaces | ✓ |
| CLI-only / DB-only | Alternatives | |
| **Full live stack gates in CI (E2E)** | Add Garage+Authula, no-skip-as-green | ✓ |
| Tiered / documented-deferred | Partial gate | |
| **`local` = operator/admin, CLI-only** | Seeded admin caps, no impersonation | ✓ |
| `--as-identity` impersonation | Deferred | |

**User's choice:** As marked. **Notes:** Reuse `telebot.v4` + existing `IdentityLinker`
(rejected go-telegram/bot). Reuse the build-tag live-stack harness (rejected testcontainers-go).

---

## User deletion, quotas, admin audit

| Option | Description | Selected |
|--------|-------------|----------|
| **Soft-delete → purge after grace** | Deactivate now, saga-purge later | ✓ |
| Hard cascade / defer delete | Alternatives | |
| **Defer quotas to Phase 37/OPS** | Not a MUSR requirement | ✓ |
| Minimal caps in 36 | Small scope bump | |
| **Full admin audit UI now** | Web view for per-user activity | ✓ |
| Identity-key + CLI read / identity-key only | Alternatives | |

**User's choice:** Soft-delete → grace → purge (de-provisioning saga, symmetric to
provisioning); quotas deferred to 37/OPS (`golang.org/x/time/rate` + counters noted); full admin
audit UI now. **Notes:** Temporal + Kafka/CDC saga libs rejected — lightweight in-process saga.

---

## Claude's Discretion

- Whether `GET /api/settings` is admin-gated or read-for-all.
- Capability name (`settings.model.write` vs reusing `governance.write`).
- Saga journal storage shape (new `aura.*` table vs outbox).

## Deferred Ideas

- Casbin authz engine + org-roles — own spike + phase, PRD-amendment required.
- Class-(c) per-user PIM/WhatsApp sidecar instances — Phase 37+.
- Per-identity quotas/limits — Phase 37/OPS.
- CLI `--as-identity` impersonation — later.
- Deeper governance/audit web surface — grows with the Casbin phase.
