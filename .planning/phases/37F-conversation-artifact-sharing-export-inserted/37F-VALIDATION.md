---
phase: 37F
slug: conversation-artifact-sharing-export-inserted
status: complete
nyquist_compliant: true
wave_0_complete: true
created: 2026-07-15
closed: 2026-07-18
closed_by: 37F-13
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

*Populated by 37F-13 from the executed plans' own SUMMARYs, closing out the placeholder
`/gsd-plan-phase` left. Every row below is derived from the Requirements → Test Map in
`37F-RESEARCH.md:675-716` (plus the 37F-20 `gap_closure` addendum, not present in the original
research). Task ID = the owning plan (this phase's plans are small enough that a plan-level
grain is the meaningful unit; the specific implementing task is named in the Secure Behavior
column where it adds signal). All commands below were re-run as part of 37F-13's own full-suite
regression pass (`go test -tags db_integration -race -p 1 -count=1 ./internal/agui/...` — 468
PASS, 1 pre-existing unrelated FAIL, see this plan's SUMMARY) or verified present via the
package's own `go test ./internal/share/` / `./internal/objectstore/` / etc. runs inside the
`bash scripts/coverage_docker.sh` full-matrix pass.*

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 37F-09 | 37F-09 | 4 | WEBSHARE-01 | D-06 | Export MD via `GET /api/conversations/{id}/export?format=md` → 200, `Content-Disposition: attachment` | integration | `go test -tags db_integration ./internal/agui/ -run TestShareExportMarkdown` | ✅ | ✅ |
| 37F-09 | 37F-09 | 4 | WEBSHARE-01 | D-06 | Export JSON → 200, valid `Snapshot`, round-trips | integration | `go test -tags db_integration ./internal/agui/ -run TestShareExportJSON` | ✅ | ✅ |
| 37F-09 | 37F-09 | 4 | WEBSHARE-01 | D-06 | Export of a **foreign** conversation → **404** (never 403) | integration | `go test -tags db_integration ./internal/agui/ -run TestShareExportForeignConversation404` | ✅ | ✅ |
| 37F-06 | 37F-06 | 3 | WEBSHARE-01 | D-07 | MD/JSON derive from one `Snapshot` (no divergence) | unit | `go test ./internal/share/ -run TestSnapshotFormatsAgree` | ✅ | ✅ |
| 37F-10 | 37F-10 | 5 | WEBSHARE-02 | D-10/T-37F-56 | Internal link: **no** capability needed; owner mints; any authed identity resolves | integration | `go test -tags db_integration ./internal/agui/ -run TestShareInternalBearerWithinAuth` | ✅ | ✅ |
| 37F-13 | 37F-13 | 7 | WEBSHARE-02 | T-37F-06 | Public mint **without** `share.public` → **403** (the capability-gap closure — see Deviations) | integration | `go test -tags db_integration ./internal/agui/ -run 'TestShareCrossIdentityDeny/row8'` | ✅ | ✅ |
| 37F-10 | 37F-10 | 5 | WEBSHARE-02 | D-02 | Public mint **with** `share.public` → 201 + plaintext token once | integration | `go test -tags db_integration ./internal/agui/ -run TestSharePublicMintWithCapability` | ✅ | ✅ |
| 37F-10 | 37F-10 | 5 | WEBSHARE-02 | R-08 | Org kill-switch off → 403 **even with** the capability **and** on loopback (`!SecretConfigured`) | integration | `go test -tags db_integration ./internal/agui/ -run TestSharePublicOrgKillSwitch` | ✅ | ✅ |
| 37F-08 | 37F-08 | 4 | WEBSHARE-02 | D-01 | Public tier is **never** the default (absent tier ⇒ internal) | unit | `go test ./internal/share/ -run TestDefaultTierIsInternal` | ✅ | ✅ |
| 37F-10 | 37F-10 | 5 | WEBSHARE-02 | D-06 | Revoke → subsequent resolve **404** | integration | `go test -tags db_integration ./internal/agui/ -run TestShareRevokeThen404` | ✅ | ✅ |
| 37F-07 | 37F-07 | 3 | WEBSHARE-02 | D-15/OQ3 | Expired token → **404** with the sweep **never run** (lazy gate) | integration | `go test -tags db_integration ./internal/share/ -run TestShareExpiredLazy404` | ✅ | ✅ |
| 37F-07 | 37F-07 | 3 | WEBSHARE-02 | D-04 | Public mint **without** `expires_at` → rejected (CHECK + Go) | integration | `go test -tags db_integration ./internal/share/ -run TestSharePublicRequiresExpiry` | ✅ | ✅ |
| 37F-20 | 37F-20 | 6 | WEBSHARE-02 | gap_closure/T-37F-06 | RLS backstop: identity B cannot `SELECT` A's `shared_links` row at the DB layer, zero `owner_identity_id` predicate in the proving query | integration | `go test -tags db_integration ./internal/share/ -run TestShareStoreRLSDeniesCrossIdentityRead` | ✅ | ✅ |
| 37F-20 | 37F-20 | 6 | WEBSHARE-02 | gap_closure | RLS: the owner still sees her own row (no over-broad DENY) | integration | `go test -tags db_integration ./internal/share/ -run TestShareStoreRLSOwnerStillSeesOwnRow` | ✅ | ✅ |
| 37F-20 | 37F-20 | 6 | WEBSHARE-02 | gap_closure | RLS: the public token-resolve lane is unaffected by the new policy | integration | `go test -tags db_integration ./internal/share/ -run TestShareStoreRLSPublicLaneUnaffected` | ✅ | ✅ |
| 37F-03 | 37F-03 | 2 | WEBSHARE-03 | SC3 | **SC3: no host path in MD/JSON/page** (hostile fixture) | unit | `go test ./internal/share/ -run TestSnapshotRedactsHostPaths` | ✅ | ✅ |
| 37F-03 | 37F-03 | 2 | WEBSHARE-03 | SC3 | `send_file` `{path}` never in any surface | unit | `go test ./internal/share/ -run TestSnapshotStripsSendFilePath` | ✅ | ✅ |
| 37F-03 | 37F-03 | 2 | WEBSHARE-03 | SC3 | Tool **names** survive; **args** never do | unit | `go test ./internal/share/ -run TestSnapshotKeepsToolNamesDropsArgs` | ✅ | ✅ |
| 37F-03 | 37F-03 | 2 | WEBSHARE-03 | SC3 | `role=tool` turns never reach the snapshot | unit | `go test ./internal/share/ -run TestSnapshotDropsToolRoleTurns` | ✅ | ✅ |
| 37F-03 | 37F-03 | 2 | WEBSHARE-03 | SC3 | Spilled turn (`ContentSidecarPath`) leaks no path | unit | `go test ./internal/share/ -run TestSnapshotSpilledTurnNoSidecarPath` | ✅ | ✅ |
| 37F-03 | 37F-03 | 2 | WEBSHARE-03 | SC3 | Owner identity id absent from every surface | unit | `go test ./internal/share/ -run TestSnapshotOmitsIdentity` | ✅ | ✅ |
| 37F-07 | 37F-07 | 3 | WEBSHARE-03 | D-14 | Every action (create/update/revoke/expire/open) writes `share_audit` | integration | `go test -tags db_integration ./internal/agui/ -run TestShareAuditLedger` | ✅ | ✅ |
| 37F-07 | 37F-07 | 3 | WEBSHARE-03 | D-14 | `share_audit` surfaces in the admin union with `source="share"` | integration | `go test -tags db_integration ./internal/agui/ -run TestAuditUnionIncludesShare` | ✅ | ✅ |
| 37F-10 | 37F-10 | 5 | WEBSHARE-03 | D-14 | Public open audits **no recipient PII** (no IP/UA persisted) | integration | `go test -tags db_integration ./internal/agui/ -run TestSharePublicOpenAuditNoPII` | ✅ | ✅ |
| **37F-13** | **37F-13** | **7** | **WEBSHARE-04** | **T-37F-05/52/05b/54/56/06/01/62/63/64** | **SC4 cross-identity deny, all 10 rows** (see this plan's own `<threat_model>` for the full per-row STRIDE mapping) | integration | `go test -tags db_integration -race -p 1 -count=1 -run TestShareCrossIdentityDeny -v ./internal/agui/` | ✅ | ✅ |
| 37F-09 | 37F-09 | 4 | D-06 | D-06 | Turns appended after mint do NOT appear on the existing link | integration | `go test -tags db_integration ./internal/share/ -run TestShareSnapshotFrozen` | ✅ | ✅ |
| 37F-08 | 37F-08 | 4 | D-06 | D-06 | Update re-snapshots, keeps the token, bumps `updated_at` | integration | `go test -tags db_integration ./internal/share/ -run TestShareUpdateResnapshot` | ✅ | ✅ |
| 37F-08 | 37F-08 | 4 | D-09 | R-11/D-09 | Bundled artifact downloads via token; **not** via `/api/assets/{id}/download` | integration | `go test -tags db_integration ./internal/share/ -run TestShareBundledArtifactTokenScoped` | ✅ | ✅ |
| 37F-08 | 37F-08 | 4 | D-09 | R-12 | Only `source_kind='agent'`, non-deleted/canceled artifacts bundle | unit | `go test ./internal/share/ -run TestBundleFiltersAgentArtifacts` | ✅ | ✅ |
| 37F-08 | 37F-08 | 4 | D-09 | R-10 | Revoke drops the Garage bytes (FakeStore `List` → empty) | integration | `go test -tags db_integration ./internal/share/ -run TestShareRevokeDropsBlobs` | ✅ | ✅ |
| 37F-04 | 37F-04 | 2 | D-12 | D-12 | Share keys never collide with / escape the `identity/` namespace | unit+property | `go test ./internal/objectstore/ -run TestShareKeyNamespaceDisjoint` | ✅ | ✅ |
| 37F-11 | 37F-11 | 5 | D-15 | D-15 | `DeleteConversationLifecycle` revokes shares + drops blobs **before** the row delete | integration | `go test -tags db_integration ./internal/runner/ -run TestDeleteLifecycleRevokesShares` | ✅ | ✅ |
| 37F-07 | 37F-07 | 3 | D-15 | D-15 | FK `ON DELETE CASCADE` backstops a raw conversation delete | integration | `go test -tags db_integration ./internal/share/ -run TestSharedLinksCascade` | ✅ | ✅ |
| 37F-11 | 37F-11 | 5 | OQ3 | OQ3 | Sweep expires due links + drops blobs; idempotent on re-run | integration | `go test -tags db_integration ./internal/share/ -run TestShareExpirySweep` | ✅ | ✅ |
| 37F-11 | 37F-11 | 5 | OQ3 | OQ3 | Nil expirer ⇒ disabled no-op, not a panic | unit | `go test ./internal/cron/handlers/ -run TestShareExpiryDisabled` | ✅ | ✅ |
| 37F-12 | 37F-12 | 6 | D-03 | T-37F-57 | `/s/{token}` reachable **without** a session; every other route still gated | integration/unit | `go test ./cmd/aura/ ./internal/agui/ -run TestPublicShareRouteAllowlist` (+ `TestSharePublicCapabilityNameValid`) | ✅ | ✅ |
| 37F-16 | 37F-16 | 3 | D-03 | D-08 | Public page HTML artifact ⇒ `sandbox="allow-scripts"`, no `allow-same-origin`, `srcDoc` | web unit | `npx vitest run web/src/routes/SharePage.test.tsx` | ✅ | ✅ (manual browser confirmation deferred to 37F-19) |
| 37F-14 | 37F-14 | 3 | D-05/UI | UI | ShareToggle renders in the cluster; `data-shared` reflects state | web unit | `npx vitest run web/src/shell/ShareShell.test.tsx` | ✅ | ✅ |
| 37F-15, 37F-17 | 37F-15, 37F-17 | 4, 5 | UI | UI | Modal states; public never preselected; warning only when public | web unit | `npx vitest run web/src/chat/share` | ✅ | ✅ |
| 37F-05 | 37F-05 | 2 | i18n | i18n | Every share key present in **both** en and it | web unit | `npx vitest run web/src/i18n` | ✅ | ✅ |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky. The Go rows above were re-verified by 37F-13
via the full `bash scripts/coverage_docker.sh` matrix run (worked around a pre-existing,
human-reserved `internal/db` migration-test blocker — see this plan's SUMMARY and
`deferred-items.md`'s two 37F-13 entries); the web rows were verified present at Wave 0 time by
their owning plans and are not independently re-run by 37F-13 (Go-only scope).*

---

## Wave 0 Requirements

Every 37F test file is net-new — `grep` for `shared_links|share_audit|ShareLink|/s/{token}` across
`internal/`, `cmd/`, `web/src/` returns **zero** matches.

- [x] `internal/share/snapshot_test.go` — SC3 redaction core, hostile fixtures (WEBSHARE-03) — 37F-03
- [x] `internal/share/format_test.go` — MD/JSON agree, round-trip (WEBSHARE-01, D-07) — 37F-06
- [x] `internal/share/token_test.go` — mint/hash, entropy, hash-stability (D-13) — 37F-04
- [x] `internal/share/bundle_test.go` — agent-artifacts-only filter (D-09, amended) — 37F-08
- [x] `internal/share/expiry_test.go` — expiry math, cap clamp (D-04) — 37F-04
- [x] `internal/agui/share_api_test.go` (+ `share_api_internal_test.go` + `share_api_public_test.go`,
      split by 37F-13's own refactor-on-touch after the share.public capability check pushed the
      single file past the CLAUDE.md 600-LOC cap; mirrors the share_api.go/share_api_internal.go/
      share_api_public.go production split) — routes, capability gate, allowlist (WEBSHARE-02,
      D-03) — 37F-10, split 37F-13
- [x] `internal/agui/share_cross_identity_test.go` — **SC4**, `//go:build db_integration`
      (WEBSHARE-04), all 10 rows green, 1.25s (non-sub-second) — 37F-13
- [x] `internal/objectstore/share_key_test.go` — namespace disjointness (D-12) — 37F-04
- [x] `internal/runner/runner_delete_share_test.go` — revoke-on-delete cascade (D-15) — 37F-11
- [x] `internal/cron/handlers/share_expiry_test.go` — sweep + nil-expirer no-op (OQ3) — 37F-11
      (actual filename; this checklist's original `share_expiry_sweep_test.go` was a
      planning-time guess, corrected here to the file 37F-11 actually shipped)
- [x] `web/src/shell/ShareShell.test.tsx` — ShareToggle + `data-shared` state (D-05, amended) — 37F-14
- [x] `web/src/chat/share/*.test.tsx` — modal states; public never preselected (D-01) —
      `ShareModal.test.tsx` (37F-15), `SharedSection.test.tsx` (37F-17)
- [x] `web/src/i18n/*.test.ts` — every share key in **both** en and it — `resources.share.test.ts`
      (37F-05) + `__tests__/resources.parity.test.ts`
- [x] Skip-helper mirroring `musrEnvOrSkip` (`two_identity_e2e_harness_test.go:38-41`) —
      **`t.Fatal` when env unset AND `$CI` set** — the shared `envOrSkip`
      (`internal/agui/server_integration_test.go:43`), reused by every 37F `db_integration` test
      via `migratedPool(t)`, never a bespoke per-file skip helper

*Framework: already installed. No new test framework needed.*
*Also shipped beyond Wave 0's original list (a 37F-20 `gap_closure: true` addendum, requirement
WEBSHARE-02): `internal/share/store_rls_integration_test.go` — proves migration 0041's RLS
owner-isolation policy on `aura.shared_links` bites at the database layer (`TestShareStoreRLSDeniesCrossIdentityRead`,
`TestShareStoreRLSOwnerStillSeesOwnRow`, `TestShareStoreRLSPublicLaneUnaffected`).*

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
| 3 | A minted an **internal** link | B (authenticated, **non-owner**) `GET /api/shares/{id}/data` | **200** — D-10 bearer-within-auth is *intended*; resolving as A would pass vacuously |
| 4 | A minted an **internal** link | **Anonymous** `GET /api/shares/{id}/data` (through the real `RequireAuth` chain) | **401/302** — internal is NOT on the public allowlist |
| 5 | A minted a **public** link | Anonymous resolves | **200** + zero B data + zero paths |
| 6 | A minted a link | B `DELETE /api/shares/{id}` (the actual revoke route — this row's original draft said `POST .../revoke`, which was never implemented; corrected by 37F-13) | **404** |
| 7 | A minted a **public** link, then revoked | Anonymous resolves | **404** (never a stale render — D-15) |
| 8 | B holds `share.public`; A does **not** | A mints public | **403** |
| 9 | A's public snapshot | Anonymous `GET /s/{token}/asset/{B's assetID}` | **404** — a token scopes to **its** snapshot only |
| 10 | A's public link | Anonymous `GET /api/assets/{A's assetID}/download` | **401/302** — token grants **no** identity-lane access |

**Rows 9 and 10 are the ones a naive implementation fails:** 9 catches "token authenticates, then
any asset id is fetched"; 10 catches "the public session leaks into the authenticated lane."

**Rows 3 and 4 are the D-10 pair, and they are why `GET /api/shares/{id}/data` exists.** An internal
share cannot be served over `/s/{token}`: migration 0040's `shared_links_tier_shape` CHECK forces
`token_hash IS NULL` for `tier='internal'`, so there is no token to address it with, and
`ResolveByToken`'s `WHERE token_hash = $1` can never match NULL. It also must not be — `isPublicShareRoute`
admits every `GET /s/...` **unauthenticated**, which row 4 forbids. The internal tier is therefore an
id-addressed authenticated route (`RequireAuth`, no capability, no owner predicate), resolved by
`share.Service.ResolveInternal` via `Store.ResolveLiveByID`. See PRD item (17) / RESEARCH OQ#4 for the
recorded rationale.

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
| Mutation spot-check ≥70% killed | Gate 3 DoD | `go-mutesting` is not wired into CI; WSL-only (only fork supporting go1.26) | On WSL: `GOFLAGS=-tags=db_integration go-mutesting ./internal/share/snapshot.go ./internal/share/token.go`. `PASS`=killed, `FAIL`=survived. Record score in this file. **37F-03 result (redact.go + snapshot.go, `token.go` does not exist yet — deferred to its own plan): `go-mutesting ./internal/share/redact.go ./internal/share/snapshot.go` → 0.875000 (7 passed, 1 failed, 0 duplicated, 0 skipped, total 8) = 87.5% killed, well above the 70% floor. The 1 survivor (`redact.go`, checksum `7e5eb8a880afae2ab8259f0e22681300`) was confirmed via `md5sum` to be byte-identical to `redact.go.original` — a true no-op mutation: the tool's `branch/case`/`branch/else` mutator targeted the empty `default:` arm of `projectTurns`'s role switch (whose entire job is to drop the turn, i.e. do nothing), so the "mutated" source is textually indistinguishable from the original and cannot be behaviorally detected by any test. Classified **equivalent (advisory-accepted)** — confirmed NOT leak-class: it does not touch `projectArtifact`, `toolNames`, or any Arguments/path-bearing field, so nothing that reaches the Snapshot changes. Zero leak-class survivors. Also fixed on the way: a genuine (non-equivalent) `toolNames` early-return was dead code masked by a second `len(names)==0` guard — removed as a Rule-1 dead-code cleanup, and a new `TestSnapshotToolNamesNilWhenAllBlank` assertion (pinning `!= nil`, not just `len==0`) now kills the remaining nil-normalization mutant that `omitempty`-based JSON assertions alone could not catch. Coverage after: 100.0% (`go test ./internal/share/ -cover -count=1`).** |
| Stryker web mutation ≥70% | Gate 3 DoD | Long runtime; Windows-only (WSL has no node) | Windows Git Bash: `npx stryker run` |
| Live public-link render | D-03 | Requires the real stack + a browser to confirm no console errors and correct sandboxing | `docker compose build aura && up -d`, mint a public link, open `/s/{token}` in a private window (no session), inspect the iframe attrs in devtools |
| Visual inspection of the share modal | D-01/UI | "Inspect artifact visually, not just PASS status" (project rule) | Open the modal in all 4 states (not-shared / internal / public / revoked); confirm public is never preselected and the warning renders only for public |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references (see the corrected checklist above — every file
      exists, one filename corrected from planning-time guess to actual)
- [x] No watch-mode flags
- [x] Feedback latency < 90s (quick) — `TestShareCrossIdentityDeny` itself: 1.25s
- [x] **No 37F test carries a tag outside `db_integration neo4j_integration`** — verified by
      37F-13's phase-wide grep audit; `internal/objectstore/garageadmin/client_integration_test.go`
      is the ONLY `garage_integration`-tagged match under `internal/objectstore/`, and it predates
      37F entirely (37F-20's own siblings and this phase's `share_key_test.go` are untagged/
      `db_integration`-only) — see 37F-13-SUMMARY.md for the full audit trail
- [x] SC4 lives in `internal/agui`, NOT `cmd/aura`
- [x] Aggregate coverage ≥85% — **85.7%**, computed against a fresh, real
      `db_integration neo4j_integration` coverprofile (37F-13, `bash scripts/coverage_docker.sh`
      run + hand-applied `coverage_gate.sh` filter after a pre-existing, unrelated,
      human-reserved `internal/db` test failure blocked the script's own final aggregation step
      — see Deviations in 37F-13-SUMMARY.md and `deferred-items.md`'s two 37F-13 entries for the
      full accounting).
- [ ] **Every owned package individually ≥85%** — 5 of 6 clear it (`internal/share` 85.3%,
      `internal/agui` 85.8%, `internal/runner` 92.4%, `internal/cron/handlers` 87.5%,
      `internal/config` 92.0%); `internal/objectstore` measures **69.6%**, below the floor. This
      is a pre-existing, structural, already-once-deferred (37F-04) shortfall concentrated
      entirely in `s3.go`'s live-Garage-endpoint methods (0% each, need a live S3-compatible
      endpoint outside this gate's tag set) — nothing 37F authored in this package (37F-04's own
      3 key-derivation functions are 100% each). Left UNCHECKED rather than falsely marked
      done — see `deferred-items.md`'s second 37F-13 entry for the full disposition and the
      recommended closure path for whoever picks it up next.
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** 37F-13 (automated technical validation — SC4 all 10 rows green, capability gap
closed, tag audit clean, aggregate coverage floor cleared). Two pre-existing, out-of-scope
findings are fully documented in `deferred-items.md` and left for their proper owners: the
`internal/db` migration round-trip test drift (human-reserved, `fix/ci-red-37f-drift`) and the
`internal/objectstore` package-level coverage shortfall (structural, Garage-live-endpoint-only,
first deferred by 37F-04). Final human UAT + git push to master is 37F-19's own declared scope
(wave 9, depends on 37F-18) — this sign-off covers the automated/Nyquist layer only.
