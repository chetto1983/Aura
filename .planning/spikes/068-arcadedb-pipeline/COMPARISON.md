# ArcadeDB vs Neo4j — comprehensive measured comparison (for Aura)

**Date:** 2026-06-20 · **Scope:** every dimension where ArcadeDB could beat Neo4j, measured.
**Companion to:** spike 067 (Apache AGE), spike 068 README (ArcadeDB pipeline), `docs/research/2026-06-20-neo4j-alternatives-...md`.

**Evidence legend:** `[ver]` = personally run & verified this session (ArcadeDB 26.7.1-SNAPSHOT over Bolt/HTTP, Granite-97m sidecar) · `[068]` = parallel-session spike result in the 068 README (corroborated by my re-run where noted) · `[doc]` = official docs · `[vendor]` = ArcadeDB marketing (validate) · `[CVE]` = security advisory · `[PoC]` = needs a real-dataset PoC.

> **⚠ CORRECTION (2026-07-08).** Rows 8 & 13, upside-item 2, and the cost table below originally stated *"Leiden in Neo4j needs Enterprise."* **That is false** — Leiden/PageRank run in **GDS *Community* (free)**, capped only at **4-core concurrency**; Enterprise lifts the cap, it does not gate the algorithm (Neo4j GDS docs). Corrected inline. Net effect: dimension 8 (Graph algorithms) flips to **Neo4j** (free *and* Cypher-native, vs ArcadeDB's Java-only invocation); ArcadeDB's licensing edge narrows to **multi-DB / HA / online-backup / uncapped-GDS + Apache-2.0-vs-GPLv3 distribution** — not Leiden. Overall verdict (**stay on Neo4j**) unchanged.

> **One-line answer to "is ArcadeDB better?":** On *breadth* (multi-model, query languages, protocols, free multi-database, free Leiden, native vectors) ArcadeDB is genuinely richer than Neo4j **Community**. On *depth where Aura actually leans* (battle-tested isolation, mature Bolt, `CALL gds.*` graph-algorithm ergonomics, ecosystem) Neo4j still wins. Net: ArcadeDB is the strongest *alternative*, not a clear *upgrade* — **stay on Neo4j now**, keep ArcadeDB as the pre-vetted fallback.

---

## Scorecard — who wins each dimension

| # | Dimension | Winner | Why (evidence) |
|---|-----------|--------|----------------|
| 1 | Data model breadth | **ArcadeDB** | One engine = graph + document + key-value + time-series + full-text + vector, shared storage/tx `[doc]`. Neo4j = graph (+native vector). |
| 2 | Query languages | **ArcadeDB** | SQL, **Cypher**, Gremlin, GraphQL, MongoDB, Redis `[doc/ver]`. Neo4j = Cypher (+Gremlin via plugin). |
| 3 | Network protocols | **ArcadeDB** | **Bolt** + Postgres-wire + HTTP/JSON + Redis + Mongo `[doc]`. Neo4j = Bolt + HTTP. |
| 4 | Cypher compatibility | **≈ tie** | ArcadeDB native Cypher 25, vendor-claimed 97.8% TCK; my run cleared 7/9 AGE-blockers + multi-label/elementId/`db.labels()`/`EXISTS{}` `[ver]`. But Neo4j *is* Cypher (100%, incl. its own vector/GDS procedures). |
| 5 | **Multi-database / multi-tenancy** | **ArcadeDB** (capability) | N databases per server, **free**; Bolt selects DB per connection `[ver]`. **Neo4j Community = single database**; multi-DB needs **Enterprise (paid)** `[doc]`. *This is the dimension you flagged.* |
| 6 | Tenant *isolation* quality | **Neo4j** | Record-level isolation held in my test (alice blocked from bob's data) `[ver]`, **but** DB-*attach* over Bolt was permissive (alice opened a session on bob_db) `[ver]` and the area had **CRITICAL CVE-2026-44221 (CVSS 9.0)** cross-DB authz bypass `[CVE]`. Neo4j's RBAC is mature/battle-tested. |
| 7 | Vector search | **≈ tie / context** | ArcadeDB native **HNSW + Vamana**, ACID, correct ranking on real Granite 384d `[068]` — but reachable via **SQL `vectorNeighbors()`, not Cypher** (the Neo4j vector procedure/DDL fail `[ver]`). Neo4j vector index is in **Cypher**, in **Community**, 384d cosine `[doc]` — matches Aura's current usage directly. |
| 8 | Graph algorithms / GDS | **Neo4j** *(corrected)* | Leiden/PageRank run in **GDS *Community* (free)**, capped only at **4-core concurrency** — Enterprise lifts the cap, it does NOT gate the algorithm `[doc]`. Neo4j exposes them as clean `CALL gds.*` Cypher procedures over Bolt. ArcadeDB ships 70+ algos incl. Leiden but they're Java-API/Graph-OLAP-bound, **not callable over Bolt/SQL** `[068]` → invocation rework. Neo4j wins on **both** licensing *and* ergonomics here. |
| 9 | ACID / durability | **≈ tie** | Both fully ACID, persistent, transactional `[doc]`. |
| 10 | Backup / restore | **ArcadeDB** | `BACKUP DATABASE` → one zip, one command `[068]`. Neo4j: `neo4j-admin database dump` (online backup = Enterprise). |
| 11 | Performance | **? unknown** | ArcadeDB claims Graph-OLAP PageRank ~462× its own OLTP `[vendor]`; my latency numbers were on a 23-node graph = not a valid signal. **Needs a real-dataset PoC** `[PoC]`. Neo4j is a known, tuned quantity at Aura's scale. |
| 12 | Embeddability | **ArcadeDB** | Embeddable in-process on the JVM (OrientDB lineage) `[doc]`. Neo4j embedded exists but is heavier/Community-gated. (Aura runs out-of-process either way → low weight.) |
| 13 | Licensing / cost | **ArcadeDB** | **Apache-2.0, everything free** incl. multi-DB, HA, online backup, uncapped GDS. Neo4j Community = **GPLv3** with single-DB / no-clustering / no-hot-backup / **4-core-capped GDS** limits (Leiden/PageRank themselves are free — the *cap*, not the algorithm, is the gate); lifting them = **Enterprise commercial** `[doc]`. GPLv3 also carries a **distribution obligation** if the Neo4j binary is shipped inside an appliance (→ ADR 0038). |
| 14 | Maturity / ecosystem / ops | **Neo4j** (decisively) | Neo4j = years of production, huge driver/tooling/community, mature Bolt+TLS, Aura already wired (`mcp-neo4j-cypher`, agent-memory). ArcadeDB = small team (OrientDB lineage), young, the image I tested was a **`-SNAPSHOT`**, **Bolt has no TLS yet** `[doc]`, recent **critical CVE** `[CVE]`. |
| 15 | Aura drop-in fit | **Neo4j** (today) / **ArcadeDB** (closest challenger) | Neo4j: zero change. ArcadeDB: graphview + 7/9 agent-memory features run over Bolt unmodified `[ver]`, but needs vector-query rewrite (→`vectorNeighbors`), Leiden-invocation rework, schema pre-definition, and isolation/ops hardening. |

---

## Where ArcadeDB is genuinely *better* than Neo4j (the honest upside)

1. **Free multi-database** — the one you raised. For tenant-per-database isolation, Neo4j makes you buy Enterprise; ArcadeDB gives it in Apache-2.0. `[ver/doc]`
2. **Free, uncapped 70+ graph algorithms** — ArcadeDB's algos are free with no core cap, vs Neo4j GDS Community's **4-core concurrency cap**. *(Correction: Leiden/PageRank are free on Neo4j too — not Enterprise-gated, only core-capped.)* `[vendor/doc]`
3. **One engine, many models + many protocols** — graph+doc+vector+KV+timeseries under SQL/Cypher/Gremlin/Bolt/Postgres/HTTP. Less polyglot sprawl. `[doc]`
4. **Everything Apache-2.0** — no feature is paywalled (online backup, clustering, multi-DB, advanced algos). `[doc]`
5. **One-command backup**, no AGE-style silent-drop footgun. `[068]`

## Where Neo4j stays better (why we don't switch)

1. **Tenant isolation is mature & trusted** — ArcadeDB's exact multi-tenant boundary had a **CVSS-9.0 CVE** and my test showed the Bolt DB-attach boundary is still loose (record data held, but attach wasn't denied). For multi-user privacy that's the *last* place you want surprises. `[CVE/ver]`
2. **Vectors + graph algorithms are Cypher-native in Neo4j** — Aura's agent-memory calls `db.index.vector.queryNodes` and (would call) `CALL gds.*`; on ArcadeDB both move off Cypher (SQL `vectorNeighbors`, Java/OLAP algos) = real rework. `[ver/068]`
3. **Maturity/ops** — production track record, TLS-on-Bolt, huge ecosystem, and Aura is already wired. ArcadeDB is young + the tested build was a SNAPSHOT. `[doc]`
4. **Known performance at Aura's scale** — ArcadeDB's edge is unproven on a real Aura dataset. `[PoC]`

---

## Cost / TCO (added 2026-06-20)

| | Neo4j | ArcadeDB |
|---|---|---|
| Free tier | Community $0 (GPLv3) — single DB, **GDS Leiden/PageRank free but 4-core-capped**, no online backup, no clustering, basic security | **Everything $0** (Apache-2.0) — multi-DB, GDS uncapped, HA, vectors, all features, no paywall |
| Managed cloud | AuraDB Pro $65/mo, Business-Critical $146/mo+ | — (self-host) |
| Enterprise self-hosted | **$3k–6k per core/yr** → ~$20k–40k/yr (4–8 cores) up to $80k–200k+/yr (16+ cores, HA); contact-sales | n/a (features already free) |
| Optional support | bundled w/ Enterprise | Silver $299 / Gold $599 / Platinum $1,499 per server/mo (optional; $0 community) |

**The cost lever is not "ArcadeDB is cheaper" in the abstract — it's "ArcadeDB removes Neo4j's per-deployment Enterprise tax."** Two scenarios decide it:

- **① Aura today (single self-hosted node, current features): Neo4j Community wins.** Both are $0; Aura already fills the Community gaps for free (external `leidenalg` to sidestep the GDS 4-core cap — Leiden itself is free in-engine, logical user-scoping instead of multi-DB, offline `neo4j-admin dump`). $0 + mature + already-wired + **zero migration cost** beats ArcadeDB's $0 + migration + younger ecosystem.
- **② Aura as a shipped multi-customer appliance (DGX-Spark SMB vision) needing Enterprise-grade features (physical per-tenant isolation / HA / online backup): ArcadeDB wins decisively.** Neo4j would need an **Enterprise license per deployment** (~$20k–40k/yr *each*) — economically fatal across many customers. ArcadeDB ships those features **free with every appliance** (Apache-2.0). This is the only path to Enterprise-grade graph features in a many-units product without a per-unit license tax.

Caveat: even in the appliance, if logical scoping + external Leiden + offline backup suffice, **Neo4j Community ($0) still wins** — the ArcadeDB cost advantage only materializes once a *hard* Enterprise-only requirement appears per deployment.

## Verdict

ArcadeDB **can** be better than Neo4j on **breadth, licensing, and multi-tenant *capability*** — and your multi-user instinct is sound: free database-per-tenant is a real ArcadeDB advantage Neo4j Community can't match. But on the dimensions Aura's pipeline actually leans on — **trusted tenant isolation, Cypher-native vectors/GDS, and maturity** — Neo4j is still ahead, and the multi-tenancy area is precisely where ArcadeDB carries a recent critical CVE.

**Recommendation unchanged: stay with Neo4j now; ArcadeDB is the pre-vetted fallback.** If multi-tenant database-per-user isolation becomes a hard product requirement (and Neo4j Enterprise's price is unacceptable), that's the trigger to run the deeper ArcadeDB PoC — and that PoC's #1 gate must be **tenant-isolation hardening over Bolt on a pinned, post-CVE *release* (not SNAPSHOT) build**, alongside the vector-rewrite, the Leiden-invocation path, and real-dataset latency/recall.

### The 3 things that would flip the decision toward ArcadeDB
1. Bolt tenant isolation proven airtight on a pinned release build (deny cross-DB attach, not just cross-DB read).
2. A working, performant Leiden/PageRank invocation reachable from Aura's runtime.
3. Real-dataset p95 + GraphRAG Recall@5/nDCG@10 within target vs the live Neo4j baseline.
