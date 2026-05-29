# Phase 1: Infra DB + Knowledge - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-05-29
**Phase:** 01-infra-db-knowledge
**Areas discussed:** Commit atomicity

---

## Gray areas presented (multiSelect)

| Option | Description | User selection |
|--------|-------------|----------------|
| Commit atomicity | Phase 1 = 1 mega commit OR 2 atomic slice commits (0.5 → 0.7) inside the phase. PRD says "1 slice = 1 commit"; ROADMAP bundles slices into Phase 1. | ✓ |
| Compose file path | `./compose.yaml` at repo root (CLAUDE.md + codebase target) OR `sandbox/compose.yaml` (PRD vestigial). | — (Claude's discretion) |
| Embed sidecar scope | Ship `aura-llama-embed` container in Phase 1 OR defer to Phase 15, leaving only env contract here. | — (Claude's discretion) |
| Neo4j smoke fixture | Port spike corpus, embed tiny Italian fixture in script, or generate from seed at runtime. | — (Claude's discretion) |

---

## Commit Atomicity

| Option | Description | Selected |
|--------|-------------|----------|
| 2 atomic slice commits (Recommended) | Wave 1 ships `slice 0.5: postgres + sqlc + pgx + golang-migrate infrastructure` (Postgres up + role separation + 0001/0002 migrations + db_integration tests green). Wave 2 stacks `slice 0.7: neo4j community + mcp-neo4j-cypher + cypher migrations` (compose neo4j + embed sidecar + Cypher 0001 + neo4j_integration tests + smoke recall@5). Two stop-and-verify gates. Matches PRD slice commit templates verbatim. | ✓ |
| 1 phase commit (parallel waves) | Both slices proceed in parallel waves under one big `phase 1: postgres + neo4j infra + mcp-neo4j-cypher` commit. Mirrors Phase 0 phase-level commit shape. Faster wall-clock with Codex parallel sessions. Loses fine-grained revert granularity. | |
| 2 commits, neo4j first | Reverse order: Slice 0.7 first, Slice 0.5 second. Generally inferior — Slice 0.7 writes to `aura.knowledge_migrations` (Postgres) so it has a soft dep on 0.5's schema being there. Listed for completeness. | |

**User's choice:** 2 atomic slice commits in PRD-natural order (0.5 → 0.7)
**Notes:** Aligned with PRD §Slice Q&A discipline ("1 slice = 1 commit") and user's prior `feedback_one_module_per_slice` memory ("Un modulo per slice, andiamo calmi"). Confirms the soft Postgres-→-Neo4j dependency direction makes 0.5 → 0.7 the right serial order (not parallel, not reversed). Each slice gets its own Gate 2 verify (`go vet + build + test + race` green for the touched packages) before the next slice starts.

---

## Claude's Discretion

User selected one area; the remaining three plus two latent decisions were defaulted by Claude with explicit rationale captured in CONTEXT.md `<decisions> → Claude's Discretion`:

- **D-02 Compose file path** — defaulted to `./compose.yaml` at repo root (CLAUDE.md §Persistence + codebase target layout authoritative; PRD `sandbox/compose.yaml` is vestigial pre-rewrite scaffolding). Triggers a one-line PRD amendment in the Slice 0.5 commit.
- **D-03 Embed sidecar scope** — defaulted to **ship in Slice 0.7** (PRD acceptance row 182 + ROADMAP SC#4 both require `aura knowledge ping` validating sidecar `/health` `{dim:768}` — without the sidecar the contract is theoretical). RAM impact already budgeted in PRD §Slice 0.5 RAM table.
- **D-04 Neo4j smoke fixture corpus** — defaulted to tiny ~5-doc Italian fixture committed under `scripts/fixtures/neo4j-smoke/*.md` + seed Cypher. Spike corpus at `D:/tmp/aura-neo4j-spike-2026-05-27/` is volatile (host temp); committed fixture is reviewable, replayable, regression-friendly.
- **D-05 Subcommand naming** — defaulted to `aura neo4j {migrate|ping|status|reset}` literal per PRD §Slice 0.7. ROADMAP Phase 1 SC#4 mention of `aura knowledge ping` is a ROADMAP typo; fix in Slice 0.7 commit.
- **D-06 MCP mid-runtime crash policy** — defaulted to **fail the Aura process** (PRD: "lifecycle coupled to main process"). Restart-once would mask infra rot; graceful-degrade is meaningless when knowledge-touching slices silently no-op. Phase 10 scheduler can later add an orchestrator-level watchdog if real-world demand emerges.

Each default is annotated as overridable by the planner if research surfaces a concrete reason.

---

## Deferred Ideas

Captured in CONTEXT.md `<deferred>` — preserved here as audit trail summary:

- Domain tables beyond `0001_init` + `0002_knowledge_migrations` → owning slices (Phases 2, 4, 10, 11)
- Cron backup handlers (`backup_postgres`, `backup_neo4j`) → Phase 10 (Slice 6b)
- Full Neo4j schema (`:Document`, `:Entity`, `:Community`, `:AgentEpisode`, `:AgentInsight`, `:UserConversation`, `:UserSnippet`) → owning slices (Phase 11 + Phase 11/7e)
- `aura knowledge ingest` / GraphRAG retrieval → Phase 15
- MCP server subprocess watchdog (auto-restart wrapper) → Phase 10 candidate
- Multi-database Neo4j (`CREATE DATABASE aura`) → out of scope (requires Enterprise license)
- `aura init-models` bundle distribution of `mcp-neo4j-cypher` → out of scope (PRD OQ 1 explicit, scope creep)
- Per-conversation Postgres pool tuning → Phase 10 candidate if scheduler workload surfaces contention

---

## Drifts to flag during planning (carried into CONTEXT.md canonical_refs)

- `prd.md` §Slice 0.5 row `sandbox/compose.yaml (diff)` → vestigial; Slice 0.5 commit ships PRD-amendment line updating to `compose.yaml (root)`
- `prd.md` §Slice 0.7 row `sandbox/compose.yaml (diff)` → same vestigial pattern; same amendment scope
- `.planning/ROADMAP.md` Phase 1 SC#4 `aura knowledge ping` → typo; Slice 0.7 commit ships one-line ROADMAP fix to `aura neo4j ping`
- `go.mod` Go 1.23 → Go 1.25 bump should already be Phase 0 Amendment #1; planner verifies before Slice 0.5 begins
