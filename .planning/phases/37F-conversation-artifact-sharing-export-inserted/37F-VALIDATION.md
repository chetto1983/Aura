---
phase: 37F
slug: conversation-artifact-sharing-export-inserted
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-07-15
---

# Phase 37F — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Derived from `37F-RESEARCH.md` §Validation Architecture (2026-07-15, HEAD `1a3252e64`).
> The **Per-Task Verification Map** is populated by `/gsd-plan-phase` once task IDs exist.

---

## ⚠️ The structural finding that drives this whole strategy

The existing cross-identity E2E (`cmd/aura/two_identity_e2e_test.go:1`) **cannot** be 37F's SC4
coverage vehicle, for two independent reasons:

1. **Tags.** It requires `db_integration && neo4j_integration && garage_integration &&
   authula_integration && musr_e2e`. The coverage gate runs **exactly** `db_integration
   neo4j_integration` (`coverage_gate.sh:25`) → the file **compiles + skips** in CI → **zero**
   coverage. This is the documented WR-01 failure mode.
2. **Package.** The gate measures `./internal/...` only (`coverage_gate.sh:52-53`). **`cmd/aura`
   is not measured at all, at any tag.**

**⇒ SC4 MUST live in `internal/agui` under `db_integration`**, using `objectstore.NewFake()`
(`fake.go:17`). A `cmd/aura` `musr_e2e` variant is a *supplement* for the live-stack run, never
the coverage vehicle.

**37F has ZERO container/daemon-gated code — and that is a design property to protect.** The only
external dependency is Garage (S3), covered in-process by `objectstore.FakeStore`. Therefore 100%
of 37F's Go surface is reachable under `db_integration`; there is no structural coverage hole.
**If any 37F test reaches for a `garage_integration` tag, that code silently drops out of the
85% floor and CI fails ~20 min after push.**

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework (Go)** | stdlib `testing` + `net/http/httptest`; `go.uber.org/goleak`; raw pgx |
| **Framework (Web)** | vitest + @testing-library/react; Stryker (mutation) |
| **Config file** | `.golangci.yml` (dupl 100, `_test.go` excluded); `web/vitest.config.ts` |
| **Quick run command** | `go test ./internal/share/ ./internal/agui/` |
| **Quick run (race)** | `go test -race ./internal/share/ ./internal/agui/` |
| **Full suite command** | `bash scripts/coverage_docker.sh` (**run locally BEFORE push** — stack up) |
| **Coverage gate** | `scripts/coverage_gate.sh` — `-tags "db_integration neo4j_integration" -p 1 ./internal/...`, floor **85%** |
| **Web gates** | `npx vitest run --coverage` (≥85%); `npx stryker run` (≥70%) — **Windows Git Bash, not WSL (no node)** |
| **Estimated runtime** | ~60-90s quick; full matrix ~15-20 min |

**Env the 37F integration tests read** — the **composed DSNs**, NOT the `POSTGRES_*` primitives:
- `AURA_DB_URL` (app role, `aura_app`)
- `AURA_DB_MIGRATE_URL` (DDL role, `aura_migrate`)

37F needs **no** Garage/Authula/Neo4j env (FakeStore + httptest + no graph).

---

## Sampling Rate

- **After every task commit:** `go test ./internal/share/ ./internal/agui/` (+ `-race` on touched pkgs)
- **After every plan wave:** `bash scripts/coverage_docker.sh`
- **Before `/gsd-verify-work`:** full matrix green + web gates green
- **Max feedback latency:** 90 seconds (quick), ~20 min (full)

> **A sub-second "integration" runtime is a skip tell — verify execution, not just PASS.**

---

## Coverage floor

**≥85% across the full tag matrix** (CLAUDE.md — overrides the PRD's ≥75% unit / ≥60% integration).
Owned surface = `./internal/...` minus `internal/db/sqlc/`, `internal/agent/agenttest/`,
`internal/llm/client.go` (`coverage_gate.sh:64-67`). Current aggregate **90.3%** — 37F must not
drag it under 85, and **every owned package must itself clear 85**.

**Gate-safety:** run `bash scripts/coverage_docker.sh` (disposable `aura_cov` DB). Never run the
gate against the live `aura` DB — `coverage_gate.sh:35` refuses it locally (this closed the
2026-07-10 footgun that wiped the live deployment's auth tables). Unset `AURA_WEB_AUTH_SECRET`
from `.env` before `make coverage` (known leak → breaks config tests).

---

## Per-Task Verification Map

*Populated by `/gsd-plan-phase` once task IDs exist. Every row below is derived from the
Requirements → Test Map in `37F-RESEARCH.md:675-716` and MUST be represented.*

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| {TBD-planner} | — | — | WEBSHARE-01..04 | — | see Requirements → Test Map | — | — | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

Every 37F test file is net-new — `grep` for `shared_links|share_audit|ShareLink|/s/{token}` across
`internal/`, `cmd/`, `web/src/` returns **zero** matches.

- [ ] `internal/share/snapshot_test.go` — SC3 redaction core, hostile fixtures (WEBSHARE-03)
- [ ] `internal/share/format_test.go` — MD/JSON agree, round-trip (WEBSHARE-01, D-07)
- [ ] `internal/share/token_test.go` — mint/hash, entropy, hash-stability (D-13)
- [ ] `internal/share/bundle_test.go` — agent-artifacts-only filter (D-09, amended)
- [ ] `internal/share/expiry_test.go` — expiry math, cap clamp (D-04)
- [ ] `internal/agui/share_api_test.go` — routes, capability gate, allowlist (WEBSHARE-02, D-03)
- [ ] `internal/agui/share_cross_identity_test.go` — **SC4**, `//go:build db_integration` (WEBSHARE-04)
- [ ] `internal/objectstore/share_key_test.go` — namespace disjointness (D-12)
- [ ] `internal/runner/runner_delete_share_test.go` — revoke-on-delete cascade (D-15)
- [ ] `internal/cron/handlers/share_expiry_sweep_test.go` — sweep + nil-expirer no-op (OQ3)
- [ ] `web/src/shell/ShareShell.test.tsx` — ShareToggle + `data-shared` state (D-05, amended)
- [ ] `web/src/chat/share/*.test.tsx` — modal states; public never preselected (D-01)
- [ ] `web/src/i18n/*.test.ts` — every share key in **both** en and it
- [ ] Skip-helper mirroring `musrEnvOrSkip` (`two_identity_e2e_harness_test.go:38-41`) — **`t.Fatal` when env unset AND `$CI` set**

*Framework: already installed. No new test framework needed.*

---

## The SC4 cross-identity deny E2E — exact wiring

**Location:** `internal/agui/share_cross_identity_test.go`, `//go:build db_integration`
**Identities:** two throwaway per-run UUIDs seeded into `aura.identities` (harness pattern:
`two_identity_e2e_harness_test.go:95-102`; 37F seeds `share.public` into `capability_grants` the same way).
**Object store:** `objectstore.NewFake()` — **no Garage, no `garage_integration` tag.**

| # | Setup | Act | Assert |
|---|---|---|---|
| 1 | A owns conv-A | B `GET /api/conversations/{conv-A}/export` | **404** (not 403 — reads hide foreign existence, 36 D-06) |
| 2 | A owns conv-A | B `POST /api/shares` for conv-A | **404** |
| 3 | A minted an **internal** link | B (authenticated) resolves it | **200** — D-10 bearer-within-auth is *intended* |
| 4 | A minted an **internal** link | **Anonymous** resolves it | **401/302** — internal is NOT on the public allowlist |
| 5 | A minted a **public** link | Anonymous resolves | **200** + zero B data + zero paths |
| 6 | A minted a **public** link | B `POST /api/shares/{id}/revoke` | **404** |
| 7 | A minted a **public** link, then revoked | Anonymous resolves | **404** (never a stale render — D-15) |
| 8 | B holds `share.public`; A does **not** | A mints public | **403** |
| 9 | A's public snapshot | Anonymous `GET /s/{token}/asset/{B's assetID}` | **404** — a token scopes to **its** snapshot only |
| 10 | A's public link | Anonymous `GET /api/assets/{A's assetID}/download` | **401/302** — token grants **no** identity-lane access |

**Rows 9 and 10 are the ones a naive implementation fails:** 9 catches "token authenticates, then
any asset id is fetched"; 10 catches "the public session leaks into the authenticated lane."

> **R-13 caution:** `local` holds the `*` wildcard (`capability_grants.sql:22`; seeded in `0004`),
> so `share.public` auto-passes for the operator. **Cross-identity tests MUST use provisioned
> non-wildcard identities** or every capability assertion passes vacuously.

---

## Property-based testing (gopter/rapid — PRD-mandated where indicated)

| Property | Statement |
|---|---|
| **Token opacity/uniqueness** | ∀ n mints: all plaintexts distinct; each decodes to exactly 32 bytes; no two hashes collide; no plaintext is a prefix/substring of another. |
| **Redaction idempotence** | ∀ histories h: `BuildSnapshot(BuildSnapshot(h))` ≡ `BuildSnapshot(h)`. |
| **Redaction totality (the SC3 property)** | ∀ histories h, ∀ secrets s ∈ args ∪ results ∪ sidecar paths: `s ∉ Markdown(BuildSnapshot(h))` ∧ `s ∉ JSON(BuildSnapshot(h))`. **SC3 as a machine-checkable universal.** |
| **Serializer round-trip** | ∀ snapshots s: `JSON⁻¹(JSON(s)) ≡ s` (D-07 lossless). |
| **Key-namespace disjointness** | ∀ uuids a,b,c: `!HasPrefix(ShareSnapshotKey(a,b), "identity/")` ∧ `!HasPrefix(AssetKey(…), "share/")` ∧ `ShareKeyPrefix(a) ≠ ShareKeyPrefix(b)` for a≠b. |
| **Expiry monotonicity** | ∀ t: once `resolve(l,t)`=404 for expiry, `resolve(l,t')`=404 ∀ t'>t (an expired link never resurrects — guards clock-skew). |

---

## Security test angles (the one unauthenticated surface)

| Angle | Assertion |
|---|---|
| XSS on `/s/{token}` | HTML artifact ⇒ `sandbox="allow-scripts"` **without** `allow-same-origin`, via `srcDoc` (37B D-07) |
| SVG | download-only, never inline-rendered (D-03) |
| Token enumeration | 256-bit opaque; no sequential/enumerable IDs; revoked+expired both ⇒ **404**, indistinguishable from never-existed |
| Timing | hash-indexed equality on `token_hash` (amended D-13) — no secret compared in Go memory |
| Org kill-switch | enforced **inside the handler**, not only at mount — `RequireCapability` is a **pass-through** when `!SecretConfigured` (`auth.go:282`), so the mount-level gate does not exist on loopback (R-08) |
| Orphaned blobs | revoke/delete drops Garage bytes; FK CASCADE removes the row but **not** the blob (R-10) — lifecycle hook is mandatory |

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Mutation spot-check ≥70% killed | Gate 3 DoD | `go-mutesting` is not wired into CI; WSL-only (only fork supporting go1.26) | On WSL: `GOFLAGS=-tags=db_integration go-mutesting ./internal/share/snapshot.go ./internal/share/token.go`. `PASS`=killed, `FAIL`=survived. Record score in this file. |
| Stryker web mutation ≥70% | Gate 3 DoD | Long runtime; Windows-only (WSL has no node) | Windows Git Bash: `npx stryker run` |
| Live public-link render | D-03 | Requires the real stack + a browser to confirm no console errors and correct sandboxing | `docker compose build aura && up -d`, mint a public link, open `/s/{token}` in a private window (no session), inspect the iframe attrs in devtools |
| Visual inspection of the share modal | D-01/UI | "Inspect artifact visually, not just PASS status" (project rule) | Open the modal in all 4 states (not-shared / internal / public / revoked); confirm public is never preselected and the warning renders only for public |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 90s (quick)
- [ ] **No 37F test carries a tag outside `db_integration neo4j_integration`**
- [ ] SC4 lives in `internal/agui`, NOT `cmd/aura`
- [ ] Coverage ≥85% aggregate AND every owned package ≥85%
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
