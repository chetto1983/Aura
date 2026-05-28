# CLAUDE.md

Project guidance for Claude Code (claude.ai/code) on this codebase.

## Project state

Tabula-rasa rewrite, 2026-05-27. Prior implementation at git tag `pre-rewrite-2026-05-27`.

## PRD-first principle (absolute)

**Senza PRD completo non si scrive una riga di codice.** Il PRD ([prd.md](prd.md)) è la **truth-source**, non un suggerimento. Ogni decisione architettonica, ogni file target, ogni env var, ogni open question è documentata lì. Deviazioni dal PRD richiedono PRD-amendment commit prima dell'implementazione (vedi §Slice Q&A discipline → Q&A revision protocol nel PRD).

## Project scope (13 slice — vedi prd.md per dettaglio completo)

Infrastruttura:
1. **0.5** — Postgres + sqlc + pgx + golang-migrate
2. **0.7** — Neo4j Community + APOC + GDS + HNSW + embedding sidecar
3. **0.9** — Agent runtime abstraction (`Agent` interface + workflow agents, pattern da google/adk-go non importato)

Core agent:
4. **1** — LLM client OpenAI-compat (DeepSeek-V4 via OpenRouter) + ToolResult pattern
5. **1.5** — `ask_user` pause/resume + multi-pause FIFO
6. **1.7** — Identity minimal + capability_grants (scaffolding multi-user)
7. **1.8** — Conversation persistence (multi-thread Claude.ai-style) + microcompact (1.8b)

Capabilities:
8. **2** — Sandbox runner (2a stateless + 2b session-bound + workspace + network allowlist)
9. **3** — Swarm coordinator (riusa ParallelAgent Slice 0.9)
10. **4** — KV cache builder (stable-prefix + provider-aware)
11. **5** — Web tools (web_search SearXNG + web_fetch)
12. **6** — Scheduler (cron + agent jobs persistente)
13. **7** — Skills (7a/b/c/d instruction-based + **7e** executable code snippets multi-lang con pattern analysis + TTL archived)

Transport + UX:
14. **8** — AG-UI gateway (SSE event protocol transport)
15. **9** — Channels framework + Telegram main user-facing + Setup wizard + multimodal Gemma 4 (9a/b/c)
16. **10** — User onboarding + `Agent.md` profile per identity
17. **11** — Memory ingestion + taxonomy (Documents + Entities + Graph + Agent journal, 11a/b/c/d/e)
18. **13** — Local LLM fallback (vLLM + LMCache disk-tier, doppio sidecar)

Persistence: Postgres `aura.*` schema (15 migrations 0001-0014 + Neo4j Cypher 0001-0002). `mcp-neo4j-cypher` MCP server è l'interfaccia LLM al graph.

## Slice Q&A discipline (3 gate sequenziali, mandatory)

Ogni slice attraversa 3 gate (formalizzati nel PRD §Slice Q&A discipline). Mapping ai GSD commands:

| Gate | Cosa | GSD command equivalente |
|---|---|---|
| **Gate 1 — Definition of Ready** (PRE) | Pre-req completati, OQ chiuse, acceptance machine-checkable, smoke runnable, file targets ≤600 LOC, test plan, Risk tier, migration, env catalog, commit template | `/gsd-spec-phase` → `/gsd-discuss-phase` → `/gsd-plan-phase` |
| **Gate 2 — Implementation Q&A** (DURANTE) | `go vet + build + test + race` verdi, refactor-on-touch, no asilo nido, no TODO orphan, no hard-coded env, 3-strike rule | `/gsd-execute-phase` con `gsd-executor` agent |
| **Gate 3 — Definition of Done** (POST pre-merge) | Acceptance ticked, smoke green, integration + regression passing, coverage ≥75% unit / ≥60% integration, mutation testing ≥70% killed, no goroutine leak, no data race, PRD updated | `/gsd-verify-work` → `/gsd-code-review` → `/gsd-audit-fix` → `/gsd-complete-milestone` |

**Niente shortcut.** Niente "lo aggiusto dopo". Niente "il PRD si capisce dal codice".

## GSD tooling (workflow ufficiale)

Installazione: `.claude/` con 67 commands + 33 agents + 12 hooks attivi (vedi `.claude/settings.json`). Versione: 1.1.0.

Core workflow per nuova slice:
```
/gsd-discuss-phase  → adaptive questioning su contesto (Gate 1 DoR check)
/gsd-plan-phase     → PLAN.md dettagliato con verification loop
/gsd-execute-phase  → wave-based parallel execution
/gsd-verify-work    → conversational UAT validation
/gsd-code-review    → bug/security/quality review
/gsd-audit-fix      → autonomous audit-to-fix pipeline (Gate 3 DoD)
/gsd-complete-milestone → archive + prepare next
```

Specializzati per Aura:
- `/gsd-ai-integration-phase` — design contract AI-SPEC.md per Slice 1/3/11/13 (agent runtime, swarm, memory, vLLM)
- `/gsd-secure-phase` — threat mitigations retro-verification (Risk-Based governance audit)
- `/gsd-nyquist-auditor` — Nyquist validation gaps (test discipline rigorosa)
- `/gsd-add-tests` — test generation da UAT criteria
- `/gsd-graphify` — knowledge graph del progetto in `.planning/graphs/`

Bootstrap inziale (one-shot):
- `/gsd-ingest-docs` — importa prd.md esistente in `.planning/` setup (PRD → ADR/SPEC structured)
- `/gsd-map-codebase` — analizza skeleton 633 LOC esistente in `.planning/codebase/`

## Behavioral rules (apply to every change)

- **NEVER SUPPOSE.** Read code before editing. If uncertain about API contract, stop and ask.
- **READ BEFORE EDIT.** Re-read a file you haven't touched in the last 5 messages.
- **3-STRIKE RULE.** Same failing approach max 3 times. On strike 3, stop and ask (or escalate via PRD-amendment, vedi PRD §Q&A escalation).
- **NEVER MODIFY TESTS TO MAKE THEM PASS** unless the test itself is broken. Fix the code or rewrite the test with explicit justification in commit message.
- **SCOPE CONTROL.** Do exactly what was asked. No unrequested features, refactors, or improvements.
- **FOLLOW EXISTING PATTERNS.** Never invent new approaches when codebase patterns exist.
- **NO GOD CLASS.** Never create a file >600 LOC. Refactor on touch (split into `<name>_<concern>.go`).
- **REUSABLE CODE.** Never duplicate; extract a helper.
- **DEEP REFACTOR ON TOUCH.** Every file you edit gets dead-code removal + dupl-folding + LOC ≤600 + comments-updated in the SAME commit.
- **GIT PUSH DISCIPLINE.** Never `git push` (or any remote-mutating command) unless explicitly requested in the current turn. A previous approval does not carry over.
- **NO COMMENTS UNLESS WHY IS NON-OBVIOUS.** Identifier names already explain what. Comments only for hidden constraints, workarounds, or surprising behavior.
- **NO TEST ASILO NIDO.** Tests must follow PRD §Test discipline rigorosa: realistic fixtures, goleak, race detector, property-based dove indicato, build tags integration, coverage threshold, mutation testing spot-check. Cita la tabella esempi per slice.

## Tool design — deferred-tool pattern (mandatory)

Big tools (long descriptions, complex JSON schema, examples) live in **dedicated files** with a `Deferred = true` flag on the `ToolSpec`. They do NOT appear in the LLM-visible default manifest — only their name + 1-line summary. The model uses the built-in `tool_search` (a hook tool) to fetch the full spec on demand. This protects the cache (no manifest bloat per turn) and scales to N tools without context cost.

Convention:
- Tool implementation: `internal/agent/tools/<name>.go`
- Tool spec metadata constant in the file
- Big tools: `Deferred: true`
- Small tools (e.g. `text_response`, `ask_user`): `Deferred: false`

## Post-edit validation (Gate 2 Implementation Q&A)

After every Go file edit:
- `go vet ./...`
- `go build ./...`
- `go test ./internal/<package>/` if tests exist
- `go test -race ./internal/<package>/` per package toccati
Fix issues before moving on.

## Commit discipline

- **One slice = one commit** (o N per sub-slice con atomicity nota nel PRD).
- Atomic. Commit message: imperative subject + body explaining *why*.
- Co-Authored-By trailer per project convention.
- PRD-amendment commit prima del code commit se la slice ha rivelato un buco architettonico (vedi PRD §Q&A revision protocol).

## Persistence

- **Postgres** primary (port `5432`): schema `aura.*`, sqlc-generated client, golang-migrate. 15 migrations 0001-0014.
- **Neo4j** Community + APOC + GDS (`compose.yaml`): port `7687` bolt, `7474` browser. HNSW vector index 768d cosine. `mcp-neo4j-cypher` MCP server è l'interfaccia LLM al graph (no native Go adapter).
- **Filesystem** per artifact: `$AURA_RUN_DIR/` (sidecar tool results + spillover content) + `~/.aura/agents/<id>/` (Agent.md profile) + `~/.aura/pyscripts/<id>/` (Slice 7e snippets) + `$AURA_SKILLS_DIR/` (skills instruction).
- **Backup**: Postgres `pg_dump` + Neo4j `neo4j-admin database dump` (vedi PRD §Backup strategy).

## Env vars

Tutti gli env vars usano convenzione `AURA_<DOMAIN>_<UNIT>` (es. `AURA_SWARM_MAX_DEPTH`). Eccezioni: env per librerie/sidecar di terze parti (`TELEGRAM_BOT_TOKEN`, `OPENROUTER_API_KEY`, `MULTIMODAL_*`, `LLAMA_*`, `LMCACHE_*`) mantengono naming canonico upstream.

Indice completo: vedi PRD §Caps & Limits → Indice completo env vars (~60 voci catalogate).
