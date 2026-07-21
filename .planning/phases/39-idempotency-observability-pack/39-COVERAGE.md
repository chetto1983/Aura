# Phase 39 External API Coverage

This matrix is the deterministic API-coverage checkpoint for Phase 39. Existing clients remain the implementation source of truth; the phase extends only the named seams. All integration tests use local fakes or disposable Compose services and must be safe to rerun.

| capability | decision | reason |
|---|---|---|
| MCP tools-call operation metadata | INTEGRATE | Aura-namespaced stable operation metadata is carried by stdio and HTTP envelopes. |
| MCP read-only reconnect replay | INTEGRATE | Preserve the existing classified read-only reconnect behavior and contract tests. |
| MCP mutating reconnect replay | OPT-OUT | Ambiguous mutations become terminal indeterminate and are never automatically reissued. |
| MCP remote identity authority | OPT-OUT | Identity and operation scope remain trusted local context, never model or server metadata. |
| OTLP metric export | INTEGRATE | One bounded provider exports the finite Aura metric catalog to configured collectors. |
| OTLP trace export | INTEGRATE | Preserve trace export in the shared provider lifecycle and add Tempo correlation smoke tests. |
| Telemetry content capture | OPT-OUT | Phase 39 exports content-free metadata; secret-like content enforcement belongs to Phase 40. |
| Prometheus private scrape | INTEGRATE | A dedicated registry and internal listener expose canonical bounded Aura series. |
| Prometheus public scrape | OPT-OUT | Metrics are not mounted on public AG-UI routes or host-published by default. |
| Grafana file provisioning | INTEGRATE | Datasources and four stable-UID dashboards are immutable checked-in assets. |
| Grafana API smoke verification | INTEGRATE | Bounded local health and UID lookups prove provisioning in Compose and CI. |
| Grafana production UI edits | OPT-OUT | Production-only mutable UI state would bypass review and CI validation. |
| Tempo OTLP ingest | INTEGRATE | Internal OTLP ingest receives metadata-only traces under bounded retention. |
| Tempo trace query correlation | INTEGRATE | Grafana trace drilldown is validated without identifiers becoming metric labels. |
| Aura deletion of Tempo blocks | OPT-OUT | Tempo exclusively owns and compacts its block storage. |
| Garage retention artifact removal | INTEGRATE | Existing owner-scoped client removes durable claimed objects before metadata finalize. |
| Garage owner export streaming | INTEGRATE | Bounded streams feed a versioned manifest with sizes and SHA-256 checksums. |
| Hidden backup on plain delete | OPT-OUT | Plain owner delete invokes canonical teardown and creates no undisclosed copy. |
| Neo4j bounded learned save and load | INTEGRATE | Server-side TTL, ordering, and limits enforce 512-bucket and 10k-store caps. |
| Neo4j learned-example compaction | INTEGRATE | Bounded deterministic candidate/delete batches converge after partial failure. |
| Neo4j pinned seed deletion | OPT-OUT | Manual and evaluation seeds are isolated and never consumed by learned retention caps. |

## Integration details

### MCP stdio and Streamable HTTP

- Auth/trust: existing trusted managed-server configuration; identity/scope never comes from model or server metadata.
- Bounds/retries/errors: one bounded envelope; reads retain reconnect policy; mutation ambiguity becomes `indeterminate`; completed replay opens no transport.
- Evidence: captured stdio/HTTP envelopes, hostile `_meta` override fixture, read/mutate reconnect matrix, and existing protocol tests in 39-02-03.

### OTLP, Prometheus, Grafana, and Tempo

- Auth/trust: configured internal endpoints, a dedicated Prometheus registry, and no checked-in credentials or default public ingestion ports.
- Bounds/retries/errors: SDK queue/batch/export deadlines and bounded health polling; telemetry failure never retries domain work or blocks shutdown past deadline.
- Evidence: fake exporters/collector, private handler/cardinality tests, promtool fixtures, invalid JSON/UID/link fixtures, provisioning health/UID smoke, and a synthetic trace in 39-04 and 39-05.

### Garage object storage

- Auth/trust: existing per-owner credentials and identity-scoped keys; only adapter-reconstructed keys are accepted.
- Bounds/retries/errors: bounded candidate/object batches and streaming export; already-absent is idempotent; transient/permanent failures resume from durable cleanup state.
- Evidence: fake object store plus disposable Garage, two-identity denial, partial removal/rerun, replaced-object, and checksum-manifest fixtures in 39-06-02/03.

### Neo4j learning graph

- Auth/trust: existing configured GraphClient/MCP seam with Aura-owned bucket/source enums.
- Bounds/retries/errors: server-side age/source/order/LIMIT, 512 bucket and 10k store caps, hash-idempotent writes, and non-overlapping bounded compaction.
- Evidence: exact Cypher tests, fake rows, and disposable Neo4j cap/expiry/partial-failure compaction in 39-07-02/03.

## Cross-surface invariants

- No external API accepts identity, ownership, operation scope, or authorization from model-controlled content.
- Domain mutations are owned by the local registry before any remote call; exporter/scrape/provisioning retries never repeat domain effects.
- All enumerations, queues, payloads, polling loops, and shutdowns have explicit caps or deadlines.
- Errors exposed to users/models/unauthenticated probes are typed and sanitized; detailed diagnostics remain internal and redact secrets.
- Contract tests pin request/envelope/query/config shapes, while disposable integration fixtures prove the actual service behavior.
