# Post-DRIFT Phase Plans — 2026-05-21

**Provenance:** Seven parallel scouts read `D:/tmp/{codex, elysia, nanobot, picobot, cli-printing-press, hermes-agent, openhuman, recursive-llm}`, 2026 online state-of-the-art, and audited Aura's own codebase. Outputs in `docs/research-2026-05-21/`. Cross-analysis in `ANALYSIS-DEEP.md` (conflicts, hidden dependencies, gaps). Executive summary in `MASTER-SYNTHESIS.md`. PRD entry: `prd.md` §7.5.

**Mandate (user 2026-05-21):**
1. "Andiamo con calma analizzando tutto" — careful analysis before action.
2. "Consolidate 1 inbound + 1 outbound for all web and telegram no duplication and must have same level" — Phase-CONS.
3. "Ogni modulo toccato dovrà subire un refactor profondo togliendo tutta la legacy e le parti morte" — every touch = deep refactor in same commit (CLAUDE.md DEEP REFACTOR ON TOUCH rule).
4. "Tutti i prompt in EN, output IT via direttiva esplicita" — overlays already restructured EN-only 2026-05-21.

---

## Phase index

| Phase | Plan | Sessions | LOC delta | Status |
| --- | --- | ---: | ---: | --- |
| [Phase-WIKI-FIX](Phase-WIKI-FIX/plan.md) | Substrate bug sweep — FTS5 sync, dim-change ergonomics, dedup, system-page filter, admin reindex | ~1-2 | +450 | 🔴 **priority-0 blocker** for Phase-WIKI-B Wave A (2026-05-22 bench diagnosis) |
| [Phase-BUG](Phase-BUG/plan.md) | Critical bug fixes — overlay loading, logging boundary, errcheck-hidden bugs | ~1 | -30 + 2 bugs | 🔴 ship immediately, concurrent with Phase-WIKI-FIX |
| [Phase-CACHE](Phase-CACHE/plan.md) | Provider prompt caching + small wins | ~1 | ~+100 / -50 | 🟡 after Phase-WIKI-B Wave A |
| [Phase-OUT](Phase-OUT/plan.md) | Output discipline stack (truncate, spill, throttle, tasks_completed, length-recovery) | ~2 | ~+520 | 🟡 after Phase-CACHE |
| [Phase-CONS](Phase-CONS/plan.md) | Web↔Telegram 1+1 consolidation (CONS-02..08) | ~3 | net -90 | 🟡 after Phase-OUT |
| [Phase-TOOL](Phase-TOOL/plan.md) | Tool surface reduction (`os.Root` + kitchen-sink collapse) | ~2 | -1250 | 🟡 after Phase-CONS |
| [Phase-CTX](Phase-CTX/plan.md) | Context engineering substrate (ContextEngine + payload summarizer + auto-compaction) | ~3 | ~+900 | 🟡 after Phase-TOOL |
| [Phase-STREAM](Phase-STREAM/plan.md) | Stream-time parallel tool dispatch | ~1 | ~+200 | 🟡 after Phase-CTX |
| [Phase-CLEAN3](Phase-CLEAN3/plan.md) | Codebase audit follow-through (god-file splits + dupl folds + errcheck) | ~2 | -700 | 🟡 after Phase-STREAM |

**Total**: ~15 sessions, **net ~ -750 LOC** with significant feature additions on web (streaming, voice, ask_user, archive).

**Expected end-state if all phases land:**
- p95 latency 10s → 3-5s (cache hit + parallel dispatch + lookup throttle + compaction)
- Bench strict-pass 3/20 → 12-15/20
- Tools 22 → ~10
- 0 god-files >600 LOC
- 0 production dupl clusters
- 1+1 transport shape (web/telegram parity)
- golangci-lint clean across touched files

---

## Sequencing rules

1. **Phase-WIKI-FIX is the priority-0 blocker** — surfaced by the 2026-05-22 direct retrieval bench. FTS5 mirror has 17 rows vs 150+ pages on disk → hybrid fusion silently runs on 1/3 channels. Without it, Phase-WIKI-B Wave A measures nothing useful (RRF over empty FTS = RRF over 1 channel). See [Phase-WIKI-FIX/plan.md](Phase-WIKI-FIX/plan.md) and memory `project_2026-05-22_substrate_bench_diagnosis`.
2. **Phase-WIKI-B Wave A** ships AFTER Phase-WIKI-FIX, not before.
3. **Phase-BUG runs concurrent** — it fixes a live bug invalidating web bench data; not waiting.
4. **Per-phase deep refactor** — each Ralph story MUST include `golangci-lint clean` + `dupl -t 60 clean` + LOC ≤600 + dead-code removed + comments updated, on every file touched (CLAUDE.md rule).
5. **One story = one commit** — granularity preserved per `feedback_one_module_per_slice`. No batching except for mechanical sed-style refactor with very low risk.
6. **Feature-flagged risky merges** — Phase-CONS CONS-04 (large single-commit collapse, -360 LOC) ships behind `AURA_AGENTCORE_BUILDER=true` flag for 1 week of live traffic before deleting the legacy code path.

---

## Anti-patterns reaffirmed across 4+ scouts (DO NOT LIFT)

- ❌ Fast-path classifier bypassing agent loop (picobot has, codex/elysia/nanobot/openhuman reject)
- ❌ LLM-as-reranker for retrieval (use cross-encoder)
- ❌ Compaction at 100% context (compact at 70-80%)
- ❌ Silent truncation without marker
- ❌ Defaults-on for expensive tools in autonomous contexts
- ❌ Wrapping every API endpoint as an MCP tool

---

## Cross-references

- **Existing PRD §7.4 roadmap** — Phase-KV partially absorbed by Phase-CACHE; remainder revisited after Phase-CACHE measures cache-hit improvement.
- **Phase 8 substrate** (DE-SCOPED in §7.4) — openhuman scout produced concrete code references; implementation cost drops from 6-12 sessions to 2-3 when workload trigger arrives. No new phase needed; augment existing slot.
- **Phase-WIKI-B Wave B/C** — Online research confirmed `bge-reranker-v2-m3` reranker pick; sidecar deploy is Wave B/C add (~3-5 days), document in that plan when fleshed out.

---

*Updated: 2026-05-21. Each phase's `plan.md` is the source of truth for that phase's scope and stories. Update progress inline as stories ship.*
