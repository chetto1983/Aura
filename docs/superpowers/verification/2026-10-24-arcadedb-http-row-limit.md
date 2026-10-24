# ArcadeDB's HTTP row limit, and the ingest audit

Date: 2026-10-24. The operator pointed at ArcadeData/arcadedb-drivers#151 ("controlla anche
questo"). The issue says the drivers could not send `limit` on `/command`, so a result past the
server default came back cut. #153 fixed it in the drivers, and gRPC was out of its scope. Aura
uses none of these drivers: `internal/arcadedb` and `services/ingest/arcade.py` call the HTTP
API themselves. The question was whether either has the same gap.

**Verdict:**
- **The server honours `limit` on both endpoints.** On 26.10.2, `/query` and `/command` both
  take it. The gap #151 describes was only in the drivers.
- **The Go client was already safe.** It sends `"limit": -1` on every statement.
- **The ingest audit was not.** `indexed_source_keys` sent no limit, so an identity with more than
  20,000 indexed documents would have had every key past the cut reported `MISSING`. It now sends
  `-1`.

## Measurements (ArcadeDB 26.10.2, compose's pin, throwaway container)

30,000 `IndexedDocument` rows, `SELECT source_key FROM IndexedDocument`:

| Endpoint | `limit` | HTTP | Rows | Response fields |
|---|---|---|---|---|
| `/query` | absent | 200 | 20,000 | `limit: 20000, returned: 20000, truncated: true` |
| `/query` | -1 | 200 | 30,000 | `limit: -1, returned: 30000, truncated: false` |
| `/query` | 25000 | 200 | 25,000 | |
| `/command` | absent | 200 | 20,000 | `truncated: true` |
| `/command` | -1 | 200 | 30,000 | |
| `/command` | 25000 | 200 | 25,000 | |

- **The cut is marked.** Each cut response also left a `WARNI` line in the server log:
  "returned more rows than the default HTTP limit of 20000".
- **The two settings.** The server reports `arcadedb.server.httpQueryDefaultLimit` = 20000 and
  `arcadedb.server.httpQueryMaxResultRows` = 1000000. The second setting's description says a
  result past it fails with HTTP 413 even with `limit: -1`. That was read from the server, not
  measured.
- **The test.** The new `test_indexed_source_keys_reads_past_the_http_default_limit` writes 20,001
  rows. Before the fix it failed with `assert 20000 == 20001`; after it, it passes.
  `test_arcade_integration.py`, `test_audit.py` and `test_audit_cycle.py` pass in full, 33 tests,
  against the same server with Bolt enabled.

## What this does not establish

- **No real corpus.** No identity holding more than 20,000 documents was audited. The rows were
  synthetic, with only `search_document_id` and `source_key`.
- **The 413 ceiling.** It was not reached.
- **The other callers.** The E2E scripts that read ArcadeDB over HTTP were not changed. They
  query fixtures of a few documents.
