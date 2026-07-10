# Spike Wrap-Up Summary

**Latest wrap-up:** 2026-07-10 (Sessions 24–25, spikes 071 + 090–096 — append mode).
**Cumulative:** 98 spikes wrapped into `./.claude/skills/spike-findings-Aura/` across sessions 1–25. The full per-spike record (all verdicts, tags, session narrative) lives in `.planning/spikes/MANIFEST.md` and the skill's `<metadata>` `processed_spikes` list; per-area implementation blueprints are in `references/*.md`.

## This run (Sessions 24–25, 071 + 090–096) — Phase 37E reasoning-effort selector + graph-DB contingency

| # | Name | Type | Verdict |
|---|------|------|---------|
| 095 | llama-cpp-reasoning-effort-wire-contract | standard | VALIDATED ✓ (OFF=`enable_thinking:false`; gradation=`thinking_budget_tokens`; Aura's OpenRouter `reasoning:{effort}` object IGNORED by llama-server) |
| 096 | openrouter-reasoning-effort-wire-contract | standard | VALIDATED ✓ (OFF reliable; DeepSeek on/off — ignores `reasoning.max_tokens`; **corrects** the token-budget-unifies claim) |
| 071 | arcadedb-adopt-strategy | standard | PLANNED (contingency, ADR-0038 gated — adopt pieces, don't rewrite) |
| 090 | turingdb-runtime-durability-fit | standard | VALIDATED (durable; needs explicit `load_graph` on boot) |
| 091 | turingdb-aura-cypher-compat | standard | INVALIDATED_AS_DROP_IN (no APOC/GDS/temporal/elementId) |
| 092 | turingdb-vector-graphrag-parity | standard | VALIDATED (overlap@5=1.00, p50 45ms) |
| 093 | turingdb-llm-graph-access-path | standard | PARTIAL (no native MCP/Bolt → custom REST bridge) |
| 094 | turingdb-memory-doc-e2e | standard | VALIDATED 10/10 (cli-printing-press bridge) |

**Phase 37E (95/096) — new blueprint `references/reasoning-effort-selector.md`.** The per-turn composer effort selector wire contract, settled live on both backends. OFF reliable everywhere; **gradation is backend-dependent** — real on llama.cpp (`thinking_budget_tokens`, needs `--jinja` + no `--reasoning-budget`), effectively on/off on DeepSeek-V4 (ignores `reasoning.max_tokens`, effort labels don't track). Net-new build item = a llama.cpp branch in `openai_compat` `buildWireReasoning` (Aura's OpenRouter path already does OFF). Do NOT promise uniform low/mid/high. Feeds `.planning/phases/37E-*/37E-CONTEXT.md` D-03/D-08/D-09/D-10.

**Graph-DB (071 + 090–094) — `references/graph-db-eval.md` extended.** Verdict unchanged: **STAY with Neo4j.** TuringDB is stronger than the June desk report on vectors + basic Cypher but still not a drop-in. ArcadeDB remains the strongest Apache-2.0 contingency, pre-scoped (071) behind ADR-0038 triggers.

Updated skill artifacts: `references/reasoning-effort-selector.md` (new), `references/graph-db-eval.md` (extended), `sources/071 + 090..096-*/`, feature-area index row, `processed_spikes` += 8, wrapped-session line in `SKILL.md`.

## Prior run (Session-23, 086–089) — Casbin authz engine + org-roles (the deferred forward bet)

| # | Name | Type | Verdict |
|---|------|------|---------|
| 086 | casbin-hascapability-backing | standard | VALIDATED ✓ (casbin/v2 backs HasCapability byte-identically, 15/15 vs SQL oracle; zero-rework swap) |
| 087a | casbin-adapter-pgx-native (pckhoi) | comparison | WINNER ✓ (native pgx, shares Aura's pool, migrate owns table, per-tenant filtered load; live Postgres) |
| 087b | casbin-adapter-sql-stdlib (Blank-Xu) | comparison | RUNNER-UP ✓ (works over database/sql but needs a bridge to share the pool; no per-tenant domains load) |
| 088 | casbin-rbac-domains-orgroles | standard | VALIDATED ✓ (per-dept manager/employee/viewer, domain-scoped hierarchy, cross-domain isolation) |
| 089 | casbin-nethttp-management-api | standard | VALIDATED ✓ (drops into RequireCapability; runtime grant/revoke live in-process; 086 guard closed) |

Grounds the Phase-36-deferred Casbin phase end-to-end. Engine = **casbin/v2** + **pckhoi/casbin-pgx-adapter/v3**; ONE RBAC-with-domains superset model; management API reuses `identity.ValidateCapabilityName`. Blueprint: `references/casbin-authz.md`. **PRD-amendment required before implementation.**

## Prior run (Session-22, 082–085) — Multi-user per-identity isolation, all four planes proven live

| # | Name | Type | Verdict |
|---|------|------|---------|
| 082 | agent-sandbox-realsource-contract | standard | VALIDATED ✓ (real source + live kind run; corrects 079) |
| 083 | two-identity-e2e-tenancy | integration | VALIDATED ✓ (box+Garage+memory together; closes 080/081 tiers) |
| 084 | per-identity-pim-sidecar | standard | VALIDATED ✓ (2-instance live; the 3rd MCP class) |
| 085 | document-ingest-tenancy | standard | VALIDATED ✓ (leak→fix live; the 4th plane) |

Skill artifacts from that run: `references/multiuser-per-identity-isolation.md` (extended), `sources/082..085-*/`, feature-area index row, `processed_spikes` 082–085, the multi-user Requirements bullet in `SKILL.md`.
