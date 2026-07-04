---
spike: 083
name: two-identity-e2e-tenancy
type: integration
validates: "Given identities A and B each with a box (078) + Garage bucket (080) + scoped memory (032/081), when A ingests/writes and B attempts to read, then A gets it and B is denied at storage AND memory AND box simultaneously — closing 080's PUT/GET-deny tier and 081's 2-identity memory recall"
verdict: VALIDATED
related: [078, 080, 081, 032, 034]
tags: [integration, multi-user, per-identity, garage, memory, sandbox, phase-36, phase-37, v2.0.0]
---

# Spike 083: 2-identity E2E tenancy (box + Garage + memory together)

## What This Validates

Session-21 proved per-identity isolation **one plane at a time**: box (078, live), Garage (080, live but only the layout — "PUT/GET-deny = next tier"), MCP/skills scoping (081, design-only — "2-identity memory-recall test [open]"). The integration risk this spike closes is whether the **three storage planes isolate the SAME A/B pair at once**, and whether a single identity key drives box + bucket + memory-scope consistently. It closes the two open next-tiers 080 and 081 left dangling, live.

Two identities used everywhere: **A = alice**, **B = bob** (`spike083-a`/`spike083-b` buckets & boxes, `spike083-alice`/`spike083-bob` memory scope).

## How to Build It

Prereq stack (all healthy): `neo4j`, `aura-llama-embed`, then `docker compose up -d garage garage-bootstrap aura-agent-memory-mcp`. Then `bash .planning/spikes/083-two-identity-e2e-tenancy/run.sh`.

- **Box plane** (`run.sh`, reproduces 078): two `alpine:3.20` containers, `--network none`, no host bind, each mounting only its own named volume. A writes a secret into `vol-a`; B's `/idbox` is empty and cannot read A's file.
- **Garage plane** (`main.go` via `internal/objectstore.S3Store` — Aura's REAL client): `bucket-per-identity + scoped key` (per 080's key finding — grants are per-bucket, NOT per-prefix). `key-a` is allowed RW on `bucket-a` only, `key-b` on `bucket-b` only.
- **Memory plane** (`main.go` via `internal/mcp.OpenServer` — the same bridge as `memory_integration_test.go`): the live `aura-agent-memory-mcp` at `:8091` with per-call `user_identifier` scope on the provenance-safe fork.

## What to Expect

Every cross-identity read/write is denied at all three planes; every same-identity operation succeeds.

## Investigation Trail

1. Confirmed the stack: `dxflrs/garage:v2.0.0` + `aura-agent-memory-mcp:local` images cached, neo4j running. Brought garage + memory up via compose (attaches to `aura_default`, memory reaches `neo4j:7687`, MCP on `:8091`).
2. **Garage gotcha (carried from 080-era):** the distroless garage image has no shell and the binary is `/garage`; MSYS rewrote `/garage`→`C:/Program Files/Git/garage` until `MSYS_NO_PATHCONV=1`. Same fix as the container-isolation probes (059/060) and spike 025.
3. Built the Garage plane on Aura's production `objectstore.NewS3` (path-style, static creds) rather than a raw SDK call — so the isolation is proven through the exact seam Phase 36 ships.
4. Memory plane reused the shipped scoped tool contract (`memory_add_fact`/`memory_get_facts` + `user_identifier`) verified by `internal/agent/mcptools/memory_integration_test.go`; the fork enforces the scope server-side.

### Live-run evidence (2026-07-04)

```
BOX     A reads own secret: AURA-SPIKE-083-ALICE-SECRET
        B /idbox listing: (empty)      B read A's secret: cat: can't open '/idbox/secret.txt': No such file or directory
        B network egress: wget: can't connect to remote host (--network none)
GARAGE  [via internal/objectstore.S3Store, path-style]
        PASS A PUT own bucket
        PASS A GET own bucket round-trips — got="AURA-SPIKE-083-ALICE-OBJECT"     <- closes 080's open PUT/GET tier
        PASS B PUT own bucket
        PASS B GET A's object DENIED — 403 AccessDenied "Operation is not allowed for this key"
        PASS A GET B's object DENIED — 403 AccessDenied
        PASS B PUT into A's bucket DENIED — 403 AccessDenied
MEMORY  [via internal/mcp.OpenServer, live :8091 fork]
        PASS A add_fact stored — {"stored": true, "type": "fact", "id": "0d818045-…"}
        PASS A recalls own fact — {"fact_count": 1, …}
        PASS B does NOT see A's fact — {"fact_count": 0, "facts": []}             <- closes 081's open 2-identity recall
SUMMARY VALIDATED — all Garage + Memory per-identity isolation checks passed (go run exit 0)
```

## What to Avoid

- **Garage prefix-in-shared-bucket is NOT isolation** (080's finding, re-confirmed by construction here): grants are per-bucket. The scoped `key-a` returns 403 on `bucket-a`'s neighbour, but a shared bucket with per-identity prefixes would only be app-enforced. Phase 36 must provision one bucket + one scoped key per identity.
- **A shared static object-store cred (today's `aura-assets` + one key) is F-007.** The per-identity scoped key removes it; do not carry the single-cred posture into multi-user.
- The three planes must key on **one** identity principal (`identityctx`). Here box name, bucket name, and memory `user_identifier` were all derived from the same A/B — Phase 36 must not let them drift apart, or an identity could have a box under one key and a bucket under another.

## Constraints

- Garage S3 requires **path-style** addressing (`AURA_OBJECTSTORE_PATH_STYLE=true`); `objectstore.NewS3` already honours it.
- The memory scope is enforced by the `aura/provenance-safe-dedup` fork (`c1c2d65`) — upstream semantic dedup is NOT provenance-safe (033/034). The `user_identifier` scope is the boundary; do not rely on entity/preference semantic matching for isolation.
- Box `--network none` is the dev-simple egress cut; the production per-identity box uses the 059/009 egress posture, not necessarily full network-none.

## Results

**VALIDATED ✓** — per-identity isolation holds across box + Garage + memory **simultaneously**, for the same A/B pair, through Aura's real `objectstore` and `mcp` seams. This is the combined guarantee Phase 36 ships on, and it closes the two open next-tiers from session 21:

- **080's open "PUT/GET-deny round-trip"** → CLOSED: live PUT+GET round-trip in the owning bucket, plus 403 on every cross-identity GET/PUT.
- **081's open "2-identity memory recall"** → CLOSED: A's scoped fact recalls for A (`fact_count 1`) and is invisible to B (`fact_count 0`) through the real bridge.

**Signal for the build (Phases 36-37):** one `identityctx` principal fans out to (box name + named volume) + (bucket + scoped key) + (memory `user_identifier`); provision all three atomically at identity-create and tear down together. The `internal/objectstore` seam already supports per-identity clients (just different creds) — no new store abstraction needed; the per-identity provisioning is a Garage-admin call (`bucket create` + `key create` + `bucket allow`) plus the box/volume from 078. Memory needs only the `user_identifier` threaded from `identityctx` into every `memory__*` call.

**Open (small):** this proves storage-layer isolation. The end-to-end *document ingest* path (upload → assets bucket → documents pipeline → memory chunks, all under one identity) chains 075/077 through these planes — worth one follow-up that ingests a doc as A and confirms B's `document_search` misses it, once the Phase-36 identity plumbing exists to thread the principal through the pipeline.
