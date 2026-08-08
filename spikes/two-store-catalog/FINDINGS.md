# Spike — can the document catalog be deleted?

**Date:** 2026-08-08 · **Stack:** live (Garage, ArcadeDB 26.7.3, aura-llama-embed), `aura-ingest:local`
built 2026-08-07 · **Disposable resources only:** bucket `spike-twostore`, ArcadeDB database
`spike_twostore`, volume `spike-twostore-state`. `aura-assets`, `aura_memory` and the per-identity
`mem_<uuid>` databases were never touched.

## The claim under test

The document plane can drop PostgreSQL entirely and keep **two stores**: Garage holds bytes and
names, ArcadeDB holds passages and vectors. If true, `document_open` resolves in one hop and the
3,563 LOC of catalog machinery has no reason to exist.

This was a *design assumption* in the handoff ("Passage carrying its own `source_key`"). It had
never been measured against the shipped writer.

## What was measured

Seven real files uploaded under **nested prefixes** to a disposable bucket, then ingested by the
CURRENT `services/ingest` sidecar in **catch-up mode** (`AURA_INGEST_LIVE=false`, the fail-closed
leg — in live mode `auto_refresh` swallows extraction errors into the next cycle).

| finding | result |
|---|---|
| `source_key` on every passage | **full nested bucket key** (`fatture/2026/q1/fattura-acme.pdf`), not a filename |
| `source_kind` | `s3` |
| `raw_sha256` vs the uploaded bytes | **7/7 exact match** |
| `search_document_id` vs Go's `SearchDocumentID` framing | **7/7 reproduce** from `(identity, "s3", source_key)` |
| multi-chunk splitting, live | 101,288-byte text → **14 chunks**; the other six → 1 each (20 passages total) |
| legacy `.doc` / `.xls` | extracted via the LibreOffice normalisation leg |

### The three questions the catalog used to answer

1. **"Where are this document's bytes?"** → `source_key` IS the object key. One hop, no lookup
   table. Previously five hops: `doc_<hex>` → resolver → catalog uuid → `GetDocument` → active
   version → asset id → object.
2. **"Are these the bytes that were indexed?"** → `raw_sha256` on the passage, verified against the
   object. Amendment #114's *provenance-bearing passages* is satisfied by the passage itself.
3. **"Which objects are indexed?"** → `SELECT DISTINCT source_key FROM Passage`. No Postgres.

### The UI data shape

`list_objects_v2(Delimiter="/")` returns the SVAR React FileManager model with no transformation:

| SVAR needs | S3 returns |
|---|---|
| `type: "folder"` | `CommonPrefixes` → `archivio/`, `contratti/`, `fatture/`, … |
| `id` / `size` / `date` | `Key` / `Size` / `LastModified` |
| `onRequestData(id)` lazy expand | `list_objects_v2(Prefix=id, Delimiter="/")` |

## What this does NOT prove

- **Retrieval quality is untouched by this spike.** No question was asked of the corpus. The blind
  test (≥6 documents, vocabulary-sharing distractors, questions naming neither file nor content,
  plus one unanswerable to check abstention) remains unrun.
- **`document_open` was not driven end to end by the agent**, because the Go path still resolves
  through the catalog — that is the thing the design would change, so it cannot be measured before
  it is built.
- **Incrementality was not re-measured here.** One catch-up pass only; the memoization and
  delete-reconciliation behaviour is the earlier spike's evidence, not this one's.
- **Nothing about multi-identity isolation was exercised.** One identity, one bucket. The live
  deployment uses a SHARED `aura-assets` bucket with `identity/<id>/` prefixes, NOT the
  bucket-per-identity the design assumed — that gap is unresolved and matters for the credential-is-
  the-boundary argument.

## Corrections to the record

- The handoff's "Passage carrying its own `source_key`" was **true of the shipped writer but not of
  any live database**. `aura_chain_probe` (12 passages) and `aura_probe` (6) carry an OLDER schema —
  `document` = bare filename, `document_id`, `text`, `embedding`, and **no `source_key` at all**.
  Reading those probe databases as current evidence falsifies a premise that is actually sound.
  Stale probe databases are worse than no evidence.
- The plan's Task 2 skeleton framed the digest as null-**separated**. The shipped
  `services/ingest/identity.py` is null-**prefixed**, matching `ids.go:92-96`, and says so in a
  comment. The skeleton was wrong; the code is right.

## Round 2 — the postgres target, and a correction to this document's own conclusion

**2026-08-08, later the same day.** The design above concluded "delete the catalog, two stores" and
rejected a Postgres projection as a third copy that would drift. That rejection was wrong, and it
was wrong for the reason this project keeps paying for: the dependency was inventoried AFTER the
design instead of before it.

**CocoIndex ships a `postgres` connector.** `cocoindex/connectors/postgres/_target.py` exports
`mount_table_target`, `declare_table_target`, `table_target`, `TableSchema`, `ColumnDef`, `PgType`
and `create_pool` — the same target API the `neo4j` connector already uses for the passages, with
pgvector support included. `declare_table_target` takes `managed_by`: `ManagedBy.SYSTEM` creates and
drops the table, `ManagedBy.USER` requires it to exist and manages only its rows.

A projection written by the same engine, in the same pass, from the same source **cannot drift by
construction**. The drift objection applies only to a copy Aura synchronises itself.

### Measured: does it survive fail-closed RLS?

The document plane is under RLS (migration 0087) and the connector owns its own pool, so a target
that cannot set `app.current_identity` per connection would write rows owned by nobody. Measured on
a disposable database reproducing 0087's policy, writing as a **non-owner** role:

| pool | write | rows visible |
|---|---|---|
| `asyncpg.create_pool(init=…)` sets the GUC | **OK** | 1 |
| no GUC | **REFUSED** — `InsufficientPrivilegeError: new row violates row-level security policy` | 0 |
| blind reader, no GUC | — | **0 — fail-closed holds** |

`create_pool` in the connector is deprecated and delegates to `asyncpg.create_pool(dsn, **kwargs)`,
so the pool is ours to construct and the GUC goes in `init=`. **The whole mechanism is pool
configuration — no Go, no sync code, no bespoke component.**

**Two constraints this puts on the design.** The sidecar must connect as `aura_app`, NOT
`aura_migrate`: 0087 deliberately uses `ENABLE`, not `FORCE` ("aura_migrate OWNS these tables …
aura_app is a non-owner, non-superuser, non-BYPASSRLS role"), so the owner bypasses RLS and the
database-level guarantee would be lost. And the image needs **`cocoindex[amazon_s3,postgres]`**:
`postgres = ["asyncpg>=0.31.0"]` in cocoindex's pyproject, and `asyncpg` appeared in the plan's
draft requirements but never shipped — the image carries only `cocoindex[amazon_s3]`, `iscc-tika`
and `neo4j`. Exactly the failure mode the `amazon_s3` comment in that same file already warns about.

**What this does NOT prove:** no `TableSchema` was declared and no target was actually mounted — the
INSERT path was exercised directly against the same pool configuration the target would use. That
isolates the RLS question, which was the risk; it does not prove the target's own DDL, upsert or
delete-reconciliation behaviour against this schema.

## Operational gotchas found

- **ArcadeDB locks out after repeated failed auth** ("Too many failed authentication attempts").
  Five bad attempts from a quoting bug cost a couple of minutes. Nested `sh -lc "…\$PW…"` mangles
  the credential — pass the database name as a positional arg and keep the script single-quoted.
- `text.left()` is not an ArcadeDB function.
- The `aura-ingest` image's ENTRYPOINT is `python -m ingest.app`; ad-hoc scripts need
  `--entrypoint python`.
- `docker exec aura-garage /garage …` needs `MSYS_NO_PATHCONV=1` or MSYS rewrites `/garage` to
  `C:/Program Files/Git/garage`.
- `aura-ingest` sits behind `profiles: [ingest]` and does not start with a plain `docker compose up`.
