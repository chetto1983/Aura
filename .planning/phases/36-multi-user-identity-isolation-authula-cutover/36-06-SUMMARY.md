---
phase: 36-multi-user-identity-isolation-authula-cutover
plan: 06
subsystem: objectstore
tags: [garage, admin-api, object-store, identity-isolation, aes-gcm, encrypt-at-rest, config, net-http, tdd]

# Dependency graph
requires:
  - phase: 36-02
    provides: "migration 0030 aura.identity_object_store (bucket/access_key/secret_key_enc bytea) + AURA_AUTHULA_SECRET KEK source"
provides:
  - "Garage Admin API v2 enabled internal-only (:3903, bearer token via GARAGE_ADMIN_TOKEN, no host publish)"
  - "internal/objectstore/garageadmin: self-built stdlib net/http Admin v2 client (CreateBucket/CreateKey/AllowBucketKey + DenyBucketKey/DeleteBucket/DeleteKey, idempotent)"
  - "internal/objectstore.IdentityStore: per-identity S3 credential resolver keyed on identityctx; secret AES-256-GCM encrypted at rest (HKDF KEK from AURA_AUTHULA_SECRET); Put/Delete saga legs"
  - "config.Config.GarageAdminEndpoint + GarageAdminToken (AURA_GARAGE_ADMIN_ENDPOINT/AURA_GARAGE_ADMIN_TOKEN)"
  - "profile.ValidateIdentity — the single traversal/charset guard, now reused for object-store bucket naming"
affects: [36-08 provisioning saga consumes the create/delete legs, 36-10 audit UI, 36-12 rollout flip]

# Tech tracking
tech-stack:
  added: []   # zero new external packages — client is stdlib net/http, crypto is stdlib (T-36-06-SC)
  patterns:
    - "Self-built net/http admin client with bearer auth over the internal compose network (no SDK, no new dep)"
    - "Idempotent provisioning legs: create resolves the already-exists id by alias; delete/deny treat 404 as success"
    - "AES-256-GCM encrypt-at-rest with a domain-separated HKDF-SHA256 KEK from an existing app secret"
    - "Fail-closed credential resolution: an unprovisioned identity resolves to pgx.ErrNoRows, never the shared/foreign bucket"
    - "Config-file bind + env-injected secret (garage.toml [admin] bind, GARAGE_ADMIN_TOKEN env) mirroring the rpc_secret split"

key-files:
  created:
    - "internal/objectstore/garageadmin/types.go (Admin v2 request/response structs + Permissions)"
    - "internal/objectstore/garageadmin/client.go (net/http Admin v2 client + BucketForIdentity)"
    - "internal/objectstore/garageadmin/client_test.go (httptest behavioral units — Windows-runnable)"
    - "internal/objectstore/garageadmin/client_integration_test.go (//go:build garage_integration — live A1 gate)"
    - "internal/objectstore/identity_store.go (IdentityStore resolver + AES-GCM encrypt/decrypt)"
    - "internal/objectstore/identity_store_test.go (crypto round-trip + shared/fail-closed units)"
    - "internal/objectstore/identity_store_integration_test.go (//go:build db_integration — live round-trip)"
  modified:
    - "docker/garage/garage.toml ([admin] block binding :3903 internal-only)"
    - "compose.yaml (GARAGE_ADMIN_TOKEN to garage; AURA_GARAGE_ADMIN_ENDPOINT/TOKEN to aura; NO 3903 host publish)"
    - "internal/config/config.go (GarageAdminEndpoint + GarageAdminToken fields + loads)"
    - "internal/config/config_knobs.go (two Garage admin knobs; token flagged Secret)"
    - "internal/config/config_knobs_test.go (secret-set assertion four -> five)"
    - "internal/profile/store.go (extract ValidateIdentity — single guard, behavior-preserving)"
    - "scripts/install.sh (generate AURA_GARAGE_ADMIN_TOKEN so the required-token stack boots)"

key-decisions:
  - "Admin token via GARAGE_ADMIN_TOKEN env (Garage-native), NOT a plaintext admin_token in garage.toml — mirrors the shipped GARAGE_RPC_SECRET split so no secret is committed"
  - "CreateBucket idempotency is status-agnostic: any non-2xx (or 2xx-without-id) resolves the bucket id by alias via GetBucketInfo; only if that also fails is the create error surfaced"
  - "CreateKey is deliberately NOT name-idempotent — the Garage secret is shown once and unrecoverable, key names are non-unique labels; DB-row idempotency (Task 3 Put ON CONFLICT DO NOTHING) is the saga's marker instead"
  - "IdentityStore secret KEK = HKDF-SHA256(AURA_AUTHULA_SECRET, info=aura-objectstore-identity-key-v1) for domain separation from Authula's own derivations of the same secret"
  - "local principal (empty/CLI, 'local' name, or configured local UUID) -> shared bucket (D-11); every other identity -> own bucket, missing row fails CLOSED (F-007)"
  - "Resolve returns Credentials (bucket+access+secret); s3.go was left unmodified — the per-identity S3Store is built by the plan-08 consumer from Credentials layered on the shared endpoint/region/path-style (S3Config already accepts creds)"

patterns-established:
  - "Zero-new-dep admin client on stdlib net/http with bearer auth + idempotent create/delete legs"
  - "Encrypt-at-rest resolver with HKDF domain separation and fail-closed misses"

requirements-completed: []   # MUSR-01 is phase-spanning (Postgres=04, documents=05, Garage=06, saga=08, audit UI=10, flip=12); this plane advances but does NOT close it — mirrors 36-01/02/04/05/07 discipline

metrics:
  duration_min: 22
  tasks: 3
  files_created: 7
  files_modified: 7
  completed: 2026-07-05
---

# Phase 36 Plan 06: Object-Store Isolation Plane (Garage bucket-per-identity) Summary

Built the D-08 object-store isolation plane: enabled the Garage Admin API v2 internal-only, added a self-built stdlib `net/http` `garageadmin` client that provisions/de-provisions a per-identity bucket + scoped key idempotently, and a per-identity S3 credential resolver that decrypts each identity's secret (AES-256-GCM, HKDF KEK from `AURA_AUTHULA_SECRET`) and fails closed on a missing row — zero new external packages.

## What Shipped

### Task 1 — Garage Admin API v2 enabled internal-only (commit `3bb5386b`)
- `docker/garage/garage.toml`: `[admin]` block binding `api_bind_addr = "[::]:3903"` on the internal compose network. The bearer token is injected via the Garage-native `GARAGE_ADMIN_TOKEN` env var (never committed to the file), exactly mirroring how the already-shipped stack supplies `rpc_secret` via `GARAGE_RPC_SECRET`.
- `compose.yaml`: `GARAGE_ADMIN_TOKEN` passed to the `garage` service; `AURA_GARAGE_ADMIN_ENDPOINT` (default `http://garage:3903`) + `AURA_GARAGE_ADMIN_TOKEN` passed to the `aura` service. **No `127.0.0.1:3903` host publish** (verified `grep -c` == 0) — the admin port is a privilege-escalation surface (T-36-06-E / Pitfall 3).
- `config.Config`: `GarageAdminEndpoint` + `GarageAdminToken` (empty token non-fatal at boot; the admin client fails closed at call time so DB/migration paths keep working).
- `config_knobs.go`: both knobs catalogued; the token flagged `Secret` (the 5th secret knob — `config_knobs_test.go` secret-set assertion updated four -> five).
- `scripts/install.sh`: generates `AURA_GARAGE_ADMIN_TOKEN` in both the re-run path (`ensure_objectstore_env_secrets`) and the fresh-`.env` heredoc so the required-token compose stays bootable.

### Task 2 — `garageadmin` Admin API v2 client, TDD (RED `49eb157d` -> GREEN `24cbd72d`)
- New package `internal/objectstore/garageadmin` (`types.go` + `client.go`), stdlib `net/http` only, bearer-authenticated POSTs to `/v2/{CreateBucket,CreateKey,AllowBucketKey,DenyBucketKey,DeleteBucket,DeleteKey}`.
- **Idempotency**: `CreateBucket` resolves an already-existing alias to its id via `GetBucketInfo` (status-agnostic — robust to whatever conflict status Garage returns); `DeleteBucket`/`DeleteKey`/`DenyBucketKey` treat 404 as success so the saga re-runs.
- `AllowBucketKey` grants `read+write, owner=false` on ONLY the target bucket (per-bucket grants, F-007).
- `BucketForIdentity` maps `identity -> aura-<id>` through the shared `profile.ValidateIdentity` guard (no bucket-name injection / cross-identity resolution).
- RED: 9 httptest behavioral units against not-implemented stubs (build stayed green). GREEN: real bodies + the `garage_integration` live test.

### Task 3 — Per-identity S3 credential resolver (commit `a5fac5c9`)
- `internal/objectstore/identity_store.go`: `IdentityStore.Resolve(ctx)` reads `aura.identity_object_store` by `identityctx.IdentityID`, decrypts `secret_key_enc`, returns `Credentials{Bucket, AccessKey, SecretKey}`.
- **Encrypt-at-rest**: AES-256-GCM (random nonce prepended); the KEK is `HKDF-SHA256(AURA_AUTHULA_SECRET, info="aura-objectstore-identity-key-v1")` — domain-separated from Authula's own derivations of the same secret.
- **Backward compat (D-11)**: empty/CLI, the `"local"` name, and the configured local UUID all map to the shared bucket+static creds. **Fail-closed (F-007)**: every other identity resolves to its own bucket, and a missing row returns `pgx.ErrNoRows` — never the shared or a foreign bucket.
- `Put` encrypts on write and is idempotent (`ON CONFLICT DO NOTHING`; rotation is Delete-then-Put per the table's no-UPDATE grant); `Delete` is idempotent. Both validate the identity through the traversal guard AND enforce it is a real UUID before touching a query.

## Deviations from Plan

### Auto-fixed / auto-added (Rules 1-3)

**1. [Rule 1 - Security] Admin token via env, not a plaintext placeholder in garage.toml**
- **Found during:** Task 1. RESEARCH/plan sketch showed `admin_token = "<AURA_GARAGE_ADMIN_TOKEN>"` literally in garage.toml, but TOML has no interpolation — a literal placeholder would be used verbatim (insecure) and a real token would be committed.
- **Fix:** `[admin]` block carries only the non-secret `api_bind_addr = "[::]:3903"`; the bearer token is injected via the Garage-native `GARAGE_ADMIN_TOKEN` env var, mirroring the already-shipped `GARAGE_RPC_SECRET` split (garage.toml has no `rpc_secret` either). Acceptance still met: `[admin]` block present (grep == 1), binds :3903, token supplied.
- **Files:** docker/garage/garage.toml, compose.yaml. **Commit:** 3bb5386b

**2. [Rule 2 - Reuse/refactor, beyond files_modified] Extract `profile.ValidateIdentity`**
- **Found during:** Task 2. The plan's read_first pointed at profile's identityPattern guard "to apply to identity->bucket-name mapping". Reusing it without duplication required an exported validator.
- **Fix:** Extracted `ValidateIdentity` from `RootIdentityDir` (which now delegates) — one traversal/charset guard for both filesystem rooting and bucket naming. Behavior-preserving; profile + mcp + skills tests stay green.
- **Files:** internal/profile/store.go. **Commit:** 24cbd72d

**3. [Rule 2 - Critical functionality, beyond files_modified] install.sh + config_knobs env catalog**
- **Found during:** Task 1. A required `${AURA_GARAGE_ADMIN_TOKEN:?}` in compose with no generator is a broken deploy; the plan action says "add to the env catalog".
- **Fix:** `scripts/install.sh` generates the token (both secret paths); `config_knobs.go` catalogues both knobs (the registry is the ".env.example / doc-generation" source). `config_knobs_test.go` secret-set assertion updated (a legitimate spec change — new secret knob).
- **Files:** scripts/install.sh, internal/config/config_knobs.go, internal/config/config_knobs_test.go. **Commit:** 3bb5386b

### Design notes / departures

**4. [Rule 1 - Correctness] CreateKey is not name-idempotent (documented)**
- The plan said "make each idempotent (409->success)". This applies cleanly to CreateBucket (alias-idempotent) and the delete/deny inverses (404->success), but **not** to CreateKey: Garage shows the secret ONCE (unrecoverable) and key names are non-unique labels, so swallowing a 409 would return empty creds — a latent bug. Key idempotency lives in the DB row instead (Task 3 `Put` ON CONFLICT DO NOTHING), which the plan-08 saga consults before re-creating. `CreateKey` errors on empty creds and on non-2xx.

**5. [files_modified] `internal/objectstore/s3.go` listed but not modified**
- The plan listed s3.go (Task 3 "or configure the existing one"). The resolver returns `Credentials`; the per-identity `S3Store` is built by the plan-08 consumer from those creds layered on the shared endpoint/region/path-style (`S3Config` already accepts AccessKey/SecretKey). Modifying s3.go now would be speculative (no consumer wired this plan), so it was left byte-unchanged.

## Threat Model Coverage

| Threat ID | Disposition | Evidence |
|-----------|-------------|----------|
| T-36-06-E (admin API exposed to host) | mitigated | `[admin]` binds :3903 internal-only; token via env bearer; `grep -c '127.0.0.1:3903' compose.yaml` == 0 |
| T-36-06-I (shared-bucket prefix bypass) | mitigated | bucket-per-identity (`aura-<id>`) + `AllowBucketKey{read,write,owner:false}` per-bucket; BucketForIdentity traversal guard |
| T-36-06-I2 (per-identity secret at rest) | mitigated | AES-256-GCM ciphertext (`secret_key_enc`), HKDF-domain-separated KEK; db_integration asserts ciphertext != plaintext |
| T-36-06-SC (package installs) | mitigated | zero new external packages — stdlib net/http + stdlib crypto; go.mod/go.sum byte-unchanged |

No new threat surface beyond the plan's register (the admin egress is exactly T-36-06-E's intent). No threat flags.

## Verification / No-skip-as-green

Run live on this Windows host (CGO disabled — no `-race`, no live Garage/Postgres):
- `go build ./...` + `go vet ./...` (repo-wide) — clean.
- `go test ./internal/objectstore/...` — green (garageadmin 9 units + objectstore crypto round-trip / shared / fail-closed units).
- `go test ./internal/config/ ./internal/profile/ ./internal/mcp/ ./internal/skills/` — green (profile refactor confirmed non-regressive across consumers).
- `go vet -tags garage_integration` and `go vet -tags db_integration` — compile-clean; both integration tiers **SKIP cleanly** locally and `t.Fatal` under `$CI`.

**NOT run here (honestly `unknown` — MUST run green in WSL/CI before phase close):**
- `go test -tags garage_integration -run TestGarageAdmin ./internal/objectstore/garageadmin/` on the live Garage stack — this is the **RESEARCH-A1 confirmation gate** for the Admin v2 wire shapes (`globalAlias`/`accessKeyId`/`secretAccessKey`/`bucketId`, and the DeleteBucket/DeleteKey `?id=` query + GetBucketInfo `?globalAlias=` lookup). A wrong field name is a struct-tag/endpoint fix caught by this test.
- `go test -tags db_integration -run TestIdentityObjectStore ./internal/objectstore/` on the live Postgres stack (persist/decrypt round-trip, ciphertext-not-plaintext, fail-closed miss, idempotent Put/Delete).
- `-race` across the touched packages.

## Requirements Advanced (not closed)

MUSR-01 stays `[ ]` — it is phase-spanning (Postgres plane=04, documents=05, **Garage/object-store=06 (this plan)**, provisioning saga=08, audit UI=10, rollout flip=12). This plan delivers + unit-proves the object-store isolation mechanism; the two-identity live E2E lands at 36-12. `requirements mark-complete` intentionally NOT run (matches 36-01/02/04/05/07 discipline).

## Self-Check: PASSED

All 7 created files + the SUMMARY exist on disk; all 4 task commits (`3bb5386b`, `49eb157d`, `24cbd72d`, `a5fac5c9`) exist in git history.
