# Phase 37A: Web Artifact Delivery Lane - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-08
**Phase:** 37A-Web Artifact Delivery Lane
**Areas discussed:** Persistence/ingest scope, Ingest-failure behavior, `agent` source_kind, Download UI, Thread-ctx plumbing, Download mechanism, Serve-time MIME/XSS, Filename encoding, Ownership, Agent-asset retention, Asset processing pipeline, Size-cap double-gate, Empty thread_id

> Two research passes were run mid-discussion at the user's request: (1) general "how senior devs do it" + cloned LibreChat, open-webui, assistant-ui, ag-ui into `D:\tmp`; (2) Weaviate Elysia + elysia-frontend into `D:\tmp`. All recommendations below are grounded in those clones.

---

## Ingest scope (persistence)

| Option | Description | Selected |
|--------|-------------|----------|
| Always ingest | Any authenticated send_file → Garage + asset; event carries `{path}` + asset fields | ✓ |
| Web-only lane | Ingest only for web-destined deliveries; breaks channel-agnostic invariant | |
| Ingest, path stays internal | Also refactor Telegram to fetch bytes by asset_id (bigger blast radius) | |

**User's choice:** Always ingest (recommended).
**Notes:** Keeps `send_file`'s delivery-mechanism-unaware invariant; Telegram unregressed because it ignores the extra fields.

---

## Ingest-failure behavior

| Option | Description | Selected |
|--------|-------------|----------|
| Degrade to path-only | Emit `{path}` only; Telegram delivers, web shows render-only card; never wedge the turn | ✓ |
| Fail the delivery | Inline error, nothing delivered — regresses Telegram on a Garage outage | |
| Retry then degrade | One bounded Put retry, then degrade | |

**User's choice:** Degrade to path-only (recommended).

---

## `agent` source_kind

| Option | Description | Selected |
|--------|-------------|----------|
| New `agent` value + migration | Migration ~0035 relaxes CHECK; `SourceAgent` constant; first-class + auditable | ✓ |
| Reuse `cli` | No migration but conflates agent output with operator CLI upload | |
| Reuse `web` | Even more misleading (web = user browser upload) | |

**User's choice:** New `agent` value + migration (recommended).
**Notes:** Confirmed next migration slot is 0035 (latest on disk: 0034_scheduler_sandbox_reap_kind).

---

## Download UI

| Option | Description | Selected |
|--------|-------------|----------|
| Extend local_artifact card | Download button when asset_id present; path chip replaced; smallest web diff | ✓ |
| New dedicated attachment part | New display type + frames + i18n + tests | |
| Decide after research | Hold for assistant-ui/LibreChat findings | |

**User's choice:** Extend local_artifact card (recommended). Later reinforced by assistant-ui's `File.*` compound (override the download href) and Elysia's `RenderDisplay` switch registry.

---

## Thread-ctx plumbing

| Option | Description | Selected |
|--------|-------------|----------|
| Reuse SwarmContext.ConvID | Zero new plumbing; log the swarm-leak smell as deferred | ✓ |
| Dedicated threadctx key | Honest but touches every run entrypoint for no behavior change | |
| Planner discretion | — | |

**User's choice:** Reuse SwarmContext.ConvID this phase (recommended).

---

## Download mechanism

| Option | Description | Selected |
|--------|-------------|----------|
| Stream-through Go, no Range | io.Copy behind RequireAuth, req-ctx-scoped; strategy iface for future presign | ✓ |
| Stream-through + Range | http.ServeContent with ReadSeeker; buffering cost | |
| Presigned Garage redirect | Leaks direct store URL; needs client→store reachability | |

**User's choice:** Stream-through Go, no Range (recommended).
**Notes:** Research verdict against presign for a private per-identity store; LibreChat ships both behind one strategy interface — mirror it so presign is a future drop-in.

---

## Serve-time MIME / XSS

| Option | Description | Selected |
|--------|-------------|----------|
| Force attachment + octet-stream | Kill in-origin HTML/SVG render; sniffed mime rides SSE event for icon only | ✓ |
| Replay sniffed type + attachment | Nicer hints, reintroduces the avoided risk surface | |
| Inline allowlist (pdf/image) else attachment | open-webui's fuller policy; more surface | |

**User's choice:** Force attachment + octet-stream (recommended). LibreChat `files.js:543` + open-webui converge on this.

---

## Filename encoding

| Option | Description | Selected |
|--------|-------------|----------|
| Dual RFC 6266 form | `filename="ascii"; filename*=UTF-8''enc`; preserves unicode, closes injection | ✓ |
| ASCII-folded only | Reuse existing transliteration; loses filename fidelity | |

**User's choice:** Dual RFC 6266 form (recommended). Port LibreChat's helper (`api/server/utils/files.js:67-88`) or use `mime.FormatMediaType`.

---

## Ownership check

| Option | Description | Selected |
|--------|-------------|----------|
| Scoped query, 404 on miss | `GetForIdentity`; not-found OR not-owned → 404; non-owner regression test | ✓ (fixed by WEBART-03 + research convergence) |

**User's choice:** Effectively locked by WEBART-03 + OWASP/LibreChat/open-webui convergence (404 existence-hiding).

---

## Agent-asset retention

| Option | Description | Selected |
|--------|-------------|----------|
| Tag now, defer policy to Phase 39 | Persist like uploads; `agent` source_kind makes them selectable later | ✓ |
| Prune with the thread | Cascade on thread delete; pre-empts Phase 39 design | |
| TTL on agent assets | Scheduler/purge job — Phase 39/scheduler territory | |

**User's choice:** Tag now, defer policy to Phase 39 (recommended).

---

## Asset processing pipeline

| Option | Description | Selected |
|--------|-------------|----------|
| Skip processing — delivery-only | No embeddings/extraction/indexing; deliverable ≠ knowledge memory | ✓ |
| Run full processing | Index like an upload; pollutes memory, heavier hot path | |
| Process only if flagged | Skip by default, opt-in param — deferred instead | |

**User's choice:** Skip processing — delivery-only (recommended).
**Notes:** Elysia precedent unambiguous — agent results → ephemeral `Environment`, never the vector store; source-ingestion and agent-output are separate pipelines.

---

## Size-cap double-gate

| Option | Description | Selected |
|--------|-------------|----------|
| Flat 50 MiB only, bypass per-modality Limits | send_file's gate is authoritative; avoid late half-delivery reject | ✓ |
| Honor assets.Limits (double-gate) | Consistent but can reject an already-delivered file | |

**User's choice:** Flat 50 MiB only, bypass per-modality Limits (recommended).

---

## Empty thread_id

| Option | Description | Selected |
|--------|-------------|----------|
| Degrade to path-only | No thread → no thread-scoped asset; consistent fallback | ✓ |
| Identity-scoped asset fallback | Second asset scope; diverges from thread-scoped Telegram parity | |

**User's choice:** Degrade to path-only (recommended).

---

## Claude's Discretion

- Exact `0035` migration filename/suffix.
- `mime.FormatMediaType` vs ported LibreChat helper for RFC 6266.
- Internal signature for injecting `assets.Service` into `SendFile`.

## Deferred Ideas

- Dedicated `threadctx` package (retire the `SwarmContext.ConvID` leak).
- Presigned-redirect delivery strategy for very large artifacts.
- HTTP Range / resumable downloads (`http.ServeContent`).
- Inline in-browser preview allowlist (pdf/images) at serve time.
- Opt-in knowledge indexing of a deliverable (`send_file index:true`).
- Agent-asset retention/TTL policy → Phase 39 (Idempotency + Observability Pack).
