# ADR 0038 — Graph-store license posture: Neo4j Community (GPLv3) now, ArcadeDB (Apache-2.0) as the appliance-distribution fallback

- **Status:** Accepted
- **Date:** 2026-07-08
- **Supersedes / relates to:** spike 068 (`ArcadeDB-vs-Neo4j` COMPARISON.md), spike 069
  (real-data vector parity), ADR 0037 (mini-PC appliance vs DGX-Spark tiering), the persistence
  section of `prd.md`, and `THIRD_PARTY_NOTICES.md`.
- **Not legal advice:** this is an engineering decision record. The conveyance path in the
  "shipped appliance" scenario (§Decision D-2) **requires legal sign-off before the first unit
  ships**; the ADR records the engineering posture and the due-diligence items, not a legal
  opinion.

---

## Context

Aura's graph layer is **Neo4j Community Edition (GPLv3)** plus two plugins: **GDS Community**
(free tier of Graph Data Science) and **APOC Core** (Apache-2.0). Postgres (`aura.*`) is the
primary store; Neo4j holds the knowledge/agent-memory graph and the 1024d HNSW vector indexes, and
`mcp-neo4j-cypher` is the LLM↔graph interface. Aura's own code (Go) talks to Neo4j **only over
Bolt** — a separate server process, never embedded, never linked.

Two prior spikes (068/069) evaluated **ArcadeDB** (Apache-2.0, multi-model) as an alternative and
concluded **stay on Neo4j** on maturity/isolation grounds, with ArcadeDB as a pre-vetted fallback.
Those spikes carried **one factual error** — they claimed Leiden community detection "needs Neo4j
Enterprise." It does not: Leiden/PageRank run in **GDS Community (free)**, capped only at 4-core
concurrency (corrected in the 068 docs, 2026-07-08). Removing that error narrows the licensing
question to what this ADR settles: **not algorithm paywalls, but the copyleft/distribution
character of the stack** as Aura moves from a single dev node toward the **shipped appliance**
(the mini-PC / DGX-Spark SMB vision — ADR 0037).

The license question, precisely stated: **Neo4j Community is GPLv3; ArcadeDB is Apache-2.0. Does
GPLv3 create an obligation or a blocker for Aura — (a) today, and (b) when the graph engine is
distributed inside a product sold to customers?**

---

## What the licenses actually require

| | Neo4j Community | GDS Community (free tier) | APOC Core | ArcadeDB |
|---|---|---|---|---|
| License | **GPLv3** | GPLv3 (OSS build) — **but the packaged free jar's terms need verification, see Residual A** | Apache-2.0 | **Apache-2.0** |
| Copyleft reaches Aura's Go code? | **No** — separate process over Bolt = arm's-length aggregation, not a derivative work | No | No | No |
| Obligation when **run** (not distributed) | None beyond notices | None | None | None |
| Obligation when **distributed inside an appliance** | **Convey Corresponding Source** (offer or include), preserve license + notices, add no further restrictions | Same class of obligation → verify jar terms first | Notice retention | **None** (permissive; notice retention only) |

Key engineering facts that decide the copyleft question:

1. **Mere aggregation applies.** Aura invokes Neo4j across a process boundary using the Bolt wire
   protocol. GPLv3 copyleft does **not** propagate across that boundary — Aura's own source is not
   a derivative work of Neo4j and carries **no obligation to be GPL'd**. This is true today and
   remains true in the appliance.
2. **Conveyance is the only trigger.** The GPLv3 obligations (source offer, notices) fire **only
   when the Neo4j binary is *conveyed*** — i.e. shipped inside a distributed appliance. Running it
   on a dev box or a self-hosted server the operator installs themselves conveys nothing.
3. **Neo4j Community source is public**, so the source-offer obligation is *satisfiable* (point at
   the upstream tag + any local modifications — Aura makes none; it runs the stock image).

So the "license issue" is **not** "GPLv3 infects Aura" (it doesn't) and **not** "Leiden is paywalled"
(it isn't). It is narrower and real: **shipping GPLv3 (Neo4j) inside a sold appliance is a compliance
task**, and ArcadeDB (Apache-2.0) removes that task entirely. A separate, orthogonal cost lever —
Neo4j **Enterprise** for physical multi-tenant isolation / HA / online backup — is a *pricing*
concern, not a distribution-license one, and is out of scope here (tracked in spike 068 §TCO).

---

## Decision

### D-1 — Stay on Neo4j Community (GPLv3) for the current single-node posture.

For dev, CI, and the current self-hosted deployment there is **no license issue**: Neo4j
Community + GDS Community + APOC Core cover every feature Aura uses for **$0**, the GPLv3 copyleft
does not reach Aura's code (mere aggregation), and nothing is conveyed. Leiden runs in-engine via
`CALL gds.leiden.*` (4-core cap; the external `leidenalg` job remains an optional optimization,
not a gap-filler). **No migration is warranted on license grounds.**

### D-2 — Treat GPLv3 conveyance as a solvable compliance task, not a blocker — *if and when* Aura ships a distributed appliance.

When (not before) the graph engine is bundled into a distributed product, satisfy GPLv3 by:
- recording Neo4j Community, GDS, and APOC with their licenses and upstream source pointers in
  **`THIRD_PARTY_NOTICES.md`** (already the home for this);
- including a **written offer for Corresponding Source** (upstream tag; Aura ships the stock image
  unmodified, so no local-patch source to publish);
- retaining all upstream license/notice files in the image;
- adding **no further restrictions** on the GPL'd components in the product's own EULA.

This path is **contingent on legal sign-off** and on clearing Residual A.

### D-3 — ArcadeDB (Apache-2.0) remains the pre-vetted fallback, with two explicit switch triggers.

Migrate to ArcadeDB **only** if one of these becomes a hard product requirement:
1. **Distribution-license blocker:** legal determines GPLv3 (Neo4j) and/or the GDS free-tier jar
   terms are **unacceptable to convey** in the sold product, and the compliance path (D-2) cannot
   satisfy them. Apache-2.0 makes the obligation vanish.
2. **Physical per-tenant isolation + unacceptable Enterprise cost:** a shipped multi-customer unit
   needs database-per-tenant / HA / online backup, which Neo4j gates behind **Enterprise
   (~$20k–40k/yr per deployment)** — economically fatal across many units. ArcadeDB ships these
   free (Apache-2.0).

Absent one of these triggers, D-1 holds: $0 + mature + already-wired + **zero migration cost**
beats ArcadeDB's $0 + migration + younger ecosystem. The migration cost is bounded but real (spike
068): rewrite ~8 vector queries `db.index.vector.queryNodes` → SQL `vectorNeighbors()`, rework the
GDS/Leiden invocation off `CALL gds.*`, pre-define schema, and — the load-bearing gate — harden
Bolt tenant isolation on a **pinned post-CVE release** (not the SNAPSHOT the spike tested).

---

## Accepted Residuals

### Residual A — The GDS *free-tier jar* license needs verification before conveyance.

The GDS **source** on GitHub is GPLv3, but the packaged, downloadable GDS plugin has historically
shipped under a **Neo4j-specific "Graph Data Science Software License"** rather than pure GPLv3.
Before bundling GDS in a distributed appliance, **confirm the exact license of the free-tier jar we
ship** and whether it permits redistribution inside a product. If it does not, the options are:
(i) drop in-engine GDS and run community detection **entirely** via the external `leidenalg` job
(already the recommended optimization — this makes it load-bearing for licensing too), or
(ii) treat it as a D-3 trigger #1 toward ArcadeDB. **This is the one genuine open item** and must
be closed before the first appliance ships. Running GDS locally / self-hosted is unaffected.

### Residual B — "Mere aggregation" depends on keeping the process boundary.

The no-copyleft-on-Aura conclusion holds **only** while Neo4j is a separate process reached over
Bolt. If a future change ever **embeds** Neo4j in-process or links its libraries, the derivative-work
analysis changes. Invariant: **Aura talks to Neo4j only over Bolt/`mcp-neo4j-cypher`, never
embedded.** (Aura runs out-of-process either way — ADR-0037-style — so this costs nothing to keep.)

---

## Consequences

**Positive**
- No action required now; the current stack is compliant and free, and the prior "Leiden is
  Enterprise" scare is retired.
- The appliance license risk is **de-risked to two named, checkable items** (D-2 compliance
  checklist + Residual A jar-license check) rather than an open-ended fear.
- ArcadeDB stays a *ready* fallback with concrete, spike-measured migration scope and switch
  triggers — a future decision won't restart from zero.

**Negative / costs (accepted)**
- Shipping GPLv3 in a product carries a **recurring compliance surface** (notices, source offer)
  ArcadeDB wouldn't. Accepted as cheap and standard while Aura isn't yet a distributed appliance.
- Residual A is a **hard gate** on the appliance path that must be closed with legal before ship.

---

## Alternatives Considered

| Alternative | Verdict | Why |
|---|---|---|
| **Migrate to ArcadeDB now (pre-empt the license question)** | Rejected | Pays a real migration cost + younger-ecosystem + isolation-hardening risk today to solve a problem that only exists in the *future* distributed-appliance scenario. D-3 keeps it available exactly when needed. |
| **Buy Neo4j Enterprise to "make licensing go away"** | Rejected | Enterprise solves multi-DB/HA/cap, **not** the GPLv3-conveyance question (Enterprise is a *commercial* license with its *own* redistribution terms), and the per-deployment price is the very thing that's fatal at appliance scale. |
| **Drop the graph store; do everything in Postgres/pgvector** | Rejected | Loses native graph traversal + GDS + the `mcp-neo4j-cypher` LLM interface the PRD is built on; a far larger rewrite than the ArcadeDB fallback, for no license gain over ArcadeDB. |
| **Stay on Neo4j Community, treat conveyance as a compliance task (this ADR)** | **Accepted** | $0, mature, already-wired, copyleft doesn't reach Aura's code, and the only distributed-appliance obligations are standard and satisfiable — pending Residual A + legal sign-off. |

---

## Forward path

1. **Now:** no change. Keep the Bolt-only process boundary (Residual B invariant).
2. **Before any distributed appliance ships:** close **Residual A** (GDS free-tier jar license),
   complete the **D-2** `THIRD_PARTY_NOTICES.md` + source-offer checklist, and get **legal
   sign-off** on conveying the GPLv3 stack.
3. **If Residual A fails or D-3 triggers fire:** execute the spike-068 ArcadeDB migration, whose
   #1 gate is Bolt tenant-isolation hardening on a **pinned post-CVE release** build, alongside the
   vector-query rewrite and the GDS/Leiden invocation path.
