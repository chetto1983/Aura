# Post-DRIFT Phase Plans — 2026-05-21 (updated 2026-05-24)

**Provenance:** Multiple parallel research scouts read `D:/tmp/{codex, elysia, nanobot, picobot, cli-printing-press, hermes-agent, openhuman, recursive-llm, graphify}` + 2026 online state-of-the-art, plus exhaustive Aura per-module audit. Research outputs in `docs/research-2026-05-21/` and `docs/research-2026-05-22/`. PRD entry: `PRD.md` section 7.5.

**Mandate (user 2026-05-21..22, verbatim, in order):**

1. "Andiamo con calma analizzando tutto" — careful analysis before action.
2. "Consolidate 1 inbound + 1 outbound for all web and telegram no duplication and must have same level" — Phase-CONS.
3. "Ogni modulo toccato dovrà subire un refactor profondo togliendo tutta la legacy e le parti morte" — every touch = deep refactor in same commit.
4. "Tutti i prompt in EN, output IT via direttiva esplicita" — overlays restructured EN-only.
5. "Togli il rag dei tool è una cagata; alla fine basta il search tool come hai te" — Phase-TOOL.
6. "Attaccare tutti i moduli uno per volta e renderli testati efficenti e bloat free al 100%" → option (B): Phase-MODERNIZE = INFRA + Wave-1 god-splits.
7. "(a) poi (b) poi (c)" — locked sequence post-MODERNIZE: Phase-OUT → Phase-CTX → Phase-CONS.
8. "Fold Phase-AGENT into Phase-OUT + cerca pattern affidabili" — budget enforcement stories US-OUT-07/08/09 backed by 2-scout convergent research.

---

## Phase index — current order of operations

| # | Phase | Plan | Sessions | LOC delta | Status |
| --- | --- | --- | ---: | ---: | --- |
| 1 | [Phase-WIKI-FIX](Phase-WIKI-FIX/plan.md) | Substrate bug sweep — FTS5 sync, dim ergonomics, dedup, system-page filter, admin reindex | ~1-2 | +450 | ✅ **shipped 2026-05-22** — 8/8 Ralph commits, FTS hit 0→20/20 |
| 2 | [Phase-TOOL](Phase-TOOL/plan.md) | KILL tool RAG entirely + 18 orphan deletes + description audit + 3 kitchen-sink collapses + MCP supervisor + compact dim-fix | ~3-4 | net **-4000** | ✅ **closed 2026-05-22** — 10/10 stories (US-TOOL-01..10), commits `976260fa..227fd2be` + `9f6e5d57` |
| 3 | [Phase-BUG](Phase-BUG/plan.md) | 3 verified-still-present critical bugs — `/api/chat` overlay loading (web users see slim prompt), `logging→api` boundary (590 transitive deps), errcheck-hidden silent failures (health JSON + backup gzip) | ~1 | +40 + 3 real bugs | ✅ **closed 2026-05-22** — 3/3 stories (US-BUG-01..03), commits `13b6926d`, `498893f8`, `d405e00b` |
| 4 | [Phase-MODERNIZE](Phase-MODERNIZE/plan.md) | INFRA gates + god-file splits | ~3 | +540 | ✅ **closed 2026-05-23** — commits `3dbef06e..3821bef1` |
| 5 | [Phase-CACHE](Phase-CACHE/plan.md) | Provider prompt cache, `end_turn`, empty-reply fallback, untrusted snippet upgrade | ~1 | ~+50 | ✅ **closed 2026-05-23** — commits `5d875ada..dd833d68` |
| 6 | Phase-DEFER | Deferred tool manifest protocol and always-on/deferred split | ~1 | +65 | ✅ **closed 2026-05-23** — commits `50f8126e`, `1128aa81`, `e60541f6` |
| 7 | [Phase-OUT](Phase-OUT/plan.md) | Output discipline (truncate/spill/throttle/tasks_completed/length-recovery/orphan-backfill) + budget enforcement | ~3 | ~+720 | ✅ **closed 2026-05-23** — commits `c8a60b85..6aabfbda` |
| 8 | Phase-GRAPH-FULL | Wiki aliases, alias-aware injection, embedding dedup, typed edges | ~1-2 | ~+2000 | ✅ **closed 2026-05-23** — commits `a410a440`, `6bb76d1a`, `875a50e8`, `5556319b`; graph rollback fix `6d96fd7b` |
| 9 | [Phase-CTX](Phase-CTX/plan.md) | Context engineering substrate (ContextEngine + payload summarizer + auto-compaction at 70%) | ~3 | ~+900 | 🔴 **NEXT** — repair source/benchmark before code |
| 10 | [Phase-CONS](Phase-CONS/plan.md) | Web↔Telegram 1+1 consolidation (CONS-02..08) — substantive web feature additions (streaming, voice, ask_user, archive) | ~3 | net -90 (dedup -810 + parity +720) | 🟣 staged — after CTX |
| 11 | [Phase-WIKI-SUBNODES](Phase-WIKI-SUBNODES/plan.md) | Heading-level subnodes (H2/H3 → parent_slug + byte ranges); re-scoped from Phase-WIKI-B Wave A US-WIKI-B04 | ~1 | ~+250 | ⚪ superseded by Phase-GRAPH-FULL unless a fresh benchmark reopens it |

**Current next step:** repair Phase-CTX planning artifacts (`source.md`, `benchmark.md`, `progress.md`) against the closed Phase-GRAPH-FULL baseline before coding.

---

## Cancelled / absorbed phases (kept for archive)

| Phase | Status | Reason |
| --- | --- | --- |
| Phase-STREAM | ⚪ ABSORBED into Step 1.LAT US-LAT-06 (already shipped) | Stream-time parallel tool dispatch is live in production. Phase-STREAM plan superseded. |
| Phase-CLEAN3 | ⚪ ABSORBED into Phase-MODERNIZE Wave B | 10 god-file splits planned originally; folded into Phase-MODERNIZE. |
| Phase-WIKI-B Wave A US-WIKI-B02 | ⚪ DONE de facto by Phase-WIKI-FIX FIX-01 | Hybrid FTS+vec+exact fusion already works; FTS5 mirror sync was the missing piece. |
| Phase-WIKI-B Wave A US-WIKI-B01 | ⚪ CANCELLED | `wiki_subgraph` new tool conflicts with Phase-TOOL "no new tools" direction. |
| Phase-WIKI-B Wave A US-WIKI-B04 | ↪️ RE-SCOPED to Phase-WIKI-SUBNODES | Independent 1-session story, no longer needs Wave-A umbrella. |
| Phase-WIKI-B Wave B/C | 🟣 deferred indefinitely | Markitdown plugins + reranker + Leiden clustering — reconsider after Phase-CONS ships if bench reveals plateau. |

---

## Sequencing rules

1. **Phase-WIKI-FIX is the priority-0 blocker** — ✅ closed 2026-05-22; substrate retrieval healthy (FTS 20/20, p95 75ms).
2. **Phase-TOOL kills the tool RAG + cleans tool surface** — ✅ closed 2026-05-22 (10/10 stories); removed the live 30-call thrash root cause + dim-mismatch logs.
3. **Phase-BUG (locked 2026-05-22)** — ✅ closed 2026-05-22 (3/3 stories); 3 verified-still-present critical bugs fixed (web overlay loading, logging→api boundary, silent JSON/gzip failures).
4. **Phase-MODERNIZE is closed** — depguard, deadcode CI, LOC gates, lefthook, MODULE-HEALTH, and god-file splits are now enforcement substrate.
5. **Phase-CACHE is closed** — provider prompt cache and `end_turn` are no longer staged work.
6. **Phase-DEFER is closed** — deferred tool manifest protocol is live.
7. **Phase-OUT is closed** — output discipline + budget enforcement are live.
8. **Phase-GRAPH-FULL is closed** — alias and typed-edge graph substrate is the new baseline.
9. **Phase-CTX is NEXT** — context engineering substrate; payload_summarizer + auto-compaction at 70% are upstream of Phase-CONS.
10. **Phase-CONS follows CTX** — web/telegram 1+1; CONS-04 large single-commit collapse requires the ContextEngine ABC from CTX to be stable.
11. **Per-phase deep refactor** — every story includes `golangci-lint clean` + `dupl -t 60 clean` + LOC ≤600 + dead-code removed + comments updated on touched files (CLAUDE.md rule).
12. **One story = one commit** per `feedback_one_module_per_slice`. No batching except mechanical sed-style refactor with very low risk.
13. **Feature-flagged risky merges** — Phase-CONS CONS-04 (large -360 LOC single commit) ships behind `AURA_AGENTCORE_BUILDER=true` flag for 1 week of live traffic.

---

## Anti-patterns reaffirmed across 4+ scouts (DO NOT LIFT)

- ❌ **Vector embedding for tool routing** — 0/7 production systems use it; killed in Phase-TOOL.
- ❌ **Fast-path classifier bypassing agent loop** — picobot has it; codex/elysia/nanobot/openhuman reject.
- ❌ **LLM-as-reranker for retrieval** — use cross-encoder.
- ❌ **Compaction at 100% context** — compact at 70-80% (Anthropic 2026).
- ❌ **Silent truncation without marker** — always emit a marker the LLM recognizes.
- ❌ **Defaults-on for expensive tools in autonomous contexts** — Hermes $4.63 incident.
- ❌ **Wrapping every API endpoint as an MCP tool** — Anthropic 2026 "Writing tools for agents".
- ❌ **Prompt-pressure budget warnings** — Hermes removed in #7915 ("models give up prematurely"). Code-level enforcement is signal; prompt-level pressure is noise. This is the lesson behind Phase-OUT US-OUT-07/08/09.
- ❌ **Big-bang horizontal cleanup sweep** — IBM/McKinsey 2026 evidence: 40-50% slower + 30-40% costlier than incremental. Phase-MODERNIZE Wave B is bounded to top-10 gods exactly to avoid this.

---

## Live evidence motivating each phase (so we can show our work)

| Phase | Live evidence |
| --- | --- |
| Phase-WIKI-FIX | Direct bench 2026-05-22: FTS5 mirror had 17 rows vs 150+ pages on disk; score 0.013 uniform; recurring `Vector dimension error` every 10 min in logs |
| Phase-TOOL | Log 2026-05-22 05:11: `elapsed_ms=179668 llm_calls=28 tool_calls=33 delivered=false`. Agent thrashed 30+ `web_search`/`web_fetch` with 4 ignored 404s. Tool RAG dim-mismatch error every 10 min |
| Phase-MODERNIZE | Per-module audit 2026-05-22: 28 god files >500 LOC (8 violate the 600 cap), 103 dupl clusters, 0 unused symbols, 6 TODO total — real debt is god files + dupl |
| Phase-OUT (budget stories) | Same live evidence as Phase-TOOL; budget enforcement is what prevents the thrash from recurring after Phase-TOOL kills the immediate trigger |
| Phase-CTX | Long-conversation context window saturation observed during multi-step debug sessions (memory `feedback_aura_as_product` 70%-threshold requirement) |
| Phase-CONS | Web-channel feature parity gap: 7 features missing (streaming, voice, markdown, soft-budget, compaction, tools_allowed, tools_used) per `feedback_web_telegram_parity_full_fix` |

---

## Cross-references

- **Phase 8 substrate** (DE-SCOPED in prd.md §7.4) — openhuman scout produced concrete code references; cost drops 6-12 → 2-3 sessions when workload trigger arrives.
- **MCP Roundup** (deferred from §7.4) — survey/score/swap best community MCPs; gated on Phase-MCP-UI which is gated on Phase-TOOL closure.
- **Phase-U plugin layout** (deferred from §7.4) — domain plugins; gated on Phase-MM Wave 2/3 audio + Phase-CTX substrate.

---

*Updated 2026-05-24. Closed-phase plan files are historical evidence; the phase index plus `PRD.md` section 7.5 name the current order of operations.*
