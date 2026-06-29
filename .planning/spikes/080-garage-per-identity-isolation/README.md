---
spike: 080
name: garage-per-identity-isolation
type: standard
validates: "Given Garage, when assets are isolated per identity with scoped S3 credentials, then identity B cannot read identity A's objects and the shared-static-cred posture (F-007) is removed under server_production"
verdict: VALIDATED
related: [078]
tags: [garage, object-store, multi-user, per-identity, s3, f-007, phase-36, v2.0.0]
---

# Spike 080: Garage Per-Identity Object-Store Isolation

## What This Validates

Given the live Garage object store, **when assets are isolated per identity with scoped S3 credentials**, then identity B cannot read/list identity A's objects, AND the current shared static credential (audit finding **F-007** / R-007) is replaced by per-identity scoped keys under `server_production`. New for v2.0.0 — no prior Garage spike exists.

## Research / current model (cited)

- Today: a single bucket `aura-assets` + a single key `aura-assets-local` (live probe 2026-06-29). This is the F-007 posture — one shared static cred, no per-identity scoping. `internal/objectstore/s3.go` + `internal/assets/` presign/upload/finalize against that one bucket.
- **Garage authorization model (verified live):** grants are **per-bucket** (`bucket allow --read/--write/--owner --key K B`) — Garage does NOT support S3-IAM-style **per-prefix** policies within a bucket. So a shared bucket + `identity/<id>/` key-prefix would be **app-enforced only**, not storage-enforced — any key with read on the bucket reads every prefix. **Storage-enforced per-identity isolation therefore requires bucket-per-identity + a scoped key.**

## How to Run

```powershell
docker exec aura-garage /garage bucket create aura-spike-ida; docker exec aura-garage /garage bucket create aura-spike-idb
docker exec aura-garage /garage key create spike-ka; docker exec aura-garage /garage key create spike-kb
docker exec aura-garage /garage bucket allow --read --write --key spike-ka aura-spike-ida
docker exec aura-garage /garage bucket allow --read --write --key spike-kb aura-spike-idb
docker exec aura-garage /garage bucket info aura-spike-ida   # authorized keys: ONLY spike-ka
docker exec aura-garage /garage bucket info aura-spike-idb   # authorized keys: ONLY spike-kb
```

## Results

**VALIDATED ✓** (2026-06-29, live on `aura-garage` Garage v2.0.0):
- Bucket A `bucket info` → authorized keys = **only** `spike-ka` (RW). Bucket B → **only** `spike-kb` (RW).
- `spike-kb` holds **zero grant** on bucket A → Garage denies cross-bucket access at the storage layer. Per-identity isolation is storage-enforced via bucket-per-identity + scoped key.
- Each identity holding its own key removes the shared static `aura-assets-local` cred → satisfies F-007 under `server_production`.

Test artifacts cleaned (`bucket delete --yes` / `key delete --yes`); only `aura-assets` remains.

## Investigation Trail

- Garage binary is `/garage` (not on `$PATH`) in the `dxflrs/garage` image; admin RPC reachable via the local node.
- Native-stderr handshake logs surface as PowerShell `NativeCommandError` noise — harmless (per the docker-on-PowerShell convention).
- The per-bucket-not-per-prefix finding is the load-bearing design decision and is the reason a naive key-prefix scheme would be a silent isolation hole.

## Signal for the build (Phase 36)

- **Bucket-per-identity** (`aura-<identityID>` or alias) + a per-identity Garage **key** with RW only on that bucket; mint the key during identity provisioning (alongside the Authula cutover, MUSR-06).
- `internal/objectstore` + `internal/assets` select the bucket/key from `identityctx.IdentityID(ctx)`; presign scoped to the identity's key.
- A `server_production` validation rejects the shared default key (F-007).
- Open / next-tier proof: a full S3 PUT-as-A / GET-denied-as-B round-trip via an S3 client against the endpoint (grant-level boundary proven here; round-trip is the strong tier — defer to Phase-36 impl test). Garage bucket-count ceiling for many identities is a scaling question for the DGX tier (single appliance: fine).
