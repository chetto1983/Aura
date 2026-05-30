# CLAUDE.md

Project guidance for Claude Code (claude.ai/code) on this codebase.

## Project state

Tabula-rasa rewrite, 2026-05-27. Prior implementation at git tag `pre-rewrite-2026-05-27`.

> **Resume work**: vedi [NEXT.md](NEXT.md) per stato sessione precedente + 3-step ripresa workflow + decisione open per ripartire. PRD + codebase map + GSD tooling + 46 skills sono tutti committati su `tabula-rasa`. Zero codice scritto: PRD-first principle. Next action raccomandata: `/gsd-discuss-phase 0.5` (Postgres infra).

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

## Skills installate (`.claude/skills/`, 46 totali — 3.5 MB)

Skills modulari caricate on-demand quando il task le triggera (markdown SKILL.md con frontmatter description). Installate via `npx skills add`. Mappa skill → utilizzo nel workflow GSD + Slice Q&A:

### Security + Audit + Q&A (Trail of Bits, 14 skills)

| Skill | Slice/Gate mappato | Use case |
|---|---|---|
| `ask-questions-if-underspecified` | **Gate 1 DoR** | Clarify requirements before impl ("serious doubts" check) |
| `audit-prep-assistant` | **Gate 3 DoD setup** | Pre-audit checklist (set goals, run static analysis, increase coverage, remove dead code, ensure accessibility) |
| `audit-context-building` | **Gate 3 DoD audit** | Ultra-granular line-by-line code analysis |
| `code-maturity-assessor` | **Gate 3 DoD scoring** | 9-category framework (arithmetic safety, auditing, access controls, complexity, decentralization, documentation, MEV/risks, low-level, testing) |
| `differential-review` | **Gate 3 DoD pre-merge** | Security-focused diff review per PR + git history + blast radius |
| `fp-check` | **Gate 3 DoD triage** | TRUE/FALSE POSITIVE verification con evidence |
| `sharp-edges` | Slice 7 + tool design | Footgun API detection (skill_create + sandbox config) |
| `codeql` | Slice 2 sandbox + 7 skills + 11 ingest | Data flow + taint tracking + SARIF |
| `semgrep-rule-creator` | Slice 7c | Custom rules per `AURA_SKILL_INJECTION_BLOCKLIST` |
| `mutation-testing` | **Gate 3 DoD** | mewt/muton config (PRD richiede ≥70% killed) |
| `property-based-testing` | Slice 3/4/8 (PRD esplicito) | gopter/rapid patterns |
| `agentic-actions-auditor` | Future CI/CD | GitHub Actions security per AI agents |
| `gh-cli` | **Gate 3 DoD PR** | Authenticated gh CLI workflow |
| `devcontainer-setup` | Slice 0.5 infra | Devcontainer Go + Postgres + Neo4j |

### Go programming (samber/cc-skills-golang, 16 skills)

| Skill | Slice/Gate mappato | Use case |
|---|---|---|
| `golang-testing` | **Tutte slice — Gate 2/3** | Table-driven, fuzzing, goleak, snapshot, race, coverage, parallel tests |
| `golang-stretchr-testify` | Tutte slice | testify assert/require/mock/suite (se Aura adopta) |
| `golang-benchmark` | **Slice 9c/11b/13b pre-merge benchmarks** | Methodology + measurement (PRD richiede benchmark pre-merge) |
| `golang-error-handling` | **Tutte slice** | Error wrapping `%w`, sentinel patterns (`ErrAwaitingUserInput`, `HTTPError`) |
| `golang-concurrency` | **Slice 0.9/1/3/11e** | Goroutines, channels, iter.Seq2 streaming, background workers |
| `golang-context` | **Slice 1/3 cancellation** | Ctx-cancel propagation end-to-end |
| `golang-safety` | Tutte slice | Memory safety patterns |
| `golang-security` | **Slice 2 sandbox + 7 skills** | Security best practices Go |
| `golang-observability` | Tutte slice | Structured logging (slog), OpenTelemetry |
| `golang-database` | **Slice 0.5/1.5/1.7/1.8/6/7c/10/11/13** | Patterns Postgres+pgx, transactions, migrations |
| `golang-troubleshooting` | Debug | pprof, Delve, race detector, GODEBUG |
| `golang-project-layout` | **Slice 0.5 bootstrap** | cmd/ + internal/ standard layout |
| `golang-structs-interfaces` | **Slice 0.9 Agent interface** | Composition, embedding, interface segregation |
| `golang-modernize` | Tutte slice (Go 1.25+) | t.Context, b.Loop, synctest, iter.Seq2 modernizations |
| `golang-spf13-cobra` | CLI subcommands | `aura chat`/`serve`/`exec`/`ingest`/`telegram` etc. |
| `golang-lint` | **Gate 2 Impl** | golangci-lint setup + rules |

### Neo4j + graph + memory (neo4j-contrib/neo4j-skills, 13 skills)

| Skill | Slice mappato | Use case |
|---|---|---|
| `neo4j-cypher-skill` | **Slice 0.7 + 11** | Cypher writing/optimization (4.x/5.x/2025.x/2026.x), deprecations |
| `neo4j-driver-go-skill` | **Slice 0.7** | Go driver native (fallback se mcp-neo4j-cypher non sufficiente) |
| `neo4j-mcp-skill` | **Slice 0.7** | mcp-neo4j-cypher server (get-schema, read-cypher, write-cypher, list-gds-procedures) |
| `neo4j-vector-index-skill` | **Slice 0.7 + 11a** | HNSW vector index (CREATE VECTOR INDEX, cosine, dimensions 768d) |
| `neo4j-graphrag-skill` | **Slice 11d** | GraphRAG retrieval pattern (Microsoft, hybrid BM25+vector+graph) |
| `neo4j-gds-skill` | **Slice 11c** | Graph algorithms (Leiden community detection, PageRank, ecc.) |
| `neo4j-modeling-skill` | **Slice 11a** | Schema design (labels vs relationships vs properties) |
| `neo4j-agent-memory-skill` | **Slice 11e** | Agent memory subgraph pattern (parity con :AgentEpisode/Insight) |
| `neo4j-query-tuning-skill` | Slice 11 optimization | EXPLAIN/PROFILE slow queries, dbHits, runtime selection |
| `neo4j-getting-started-skill` | **Slice 0.7 bootstrap** | Zero-to-success agentic Neo4j |
| `neo4j-import-skill` | Slice 11b ingest | Bulk CSV/file import |
| `neo4j-document-import-skill` | **Slice 11b ingest** | Document → chunks → embeddings → graph pipeline |
| `neo4j-genai-plugin-skill` | Slice 0.7 embedding | GenAI plugin (embedding sidecar integration) |

### Meta + MCP + Anthropics (3 skills)

| Skill | Slice mappato | Use case |
|---|---|---|
| `find-skills` (vercel-labs) | meta | Discover/install new skills on demand |
| `mcp-builder` (anthropics) | **Slice 0.7** | MCP server design pattern (per mcp-neo4j-cypher integration) |
| `skill-creator` (anthropics) | **Slice 7 Skills** | Meta-pattern per creating + evaluating Aura skills |

### Trigger automatic

Il modello detecta skill rilevante dal frontmatter `description` (es. "Use when writing or reviewing Go tests" → triggera `golang-testing`). Skill caricate solo quando richieste — no token bloat in default manifest.

### Espandere il set

- `/find-skills` (skill meta) per cercare nuove
- `npx skills add <owner>/<repo> --skill <name> --agent claude-code -y` per install
- Repo noti: `samber/cc-skills-golang` (40 Go skills), `neo4j-contrib/neo4j-skills` (25 graph skills), `trailofbits/skills` (security), `anthropics/skills` (utility), `vercel-labs/skills` (find-skills + agent-skills)

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
- **NO SKIP-AS-GREEN IN CI.** Integration/smoke tests must actually run in the pipeline — a `t.Skip` that fires under `$CI` is a falsely-green job exercising nothing. (1) CI jobs export the exact env the tests read (composed DSNs `AURA_DB_URL`/`AURA_DB_MIGRATE_URL`, not just the `POSTGRES_*` primitives `config.Load` composes for the CLI). (2) Skip-helpers (`envOrSkip` and inline `t.Skip`) call `t.Fatal` when a required var is unset and `$CI` is set; locally they still skip. A sub-second "integration" runtime is a skip tell — verify execution, not just PASS.
- **COVERAGE FLOOR 85%.** No phase/slice closes below 85% measured coverage across the full tag matrix (unit + integration + smoke). This overrides the PRD's ≥75% unit / ≥60% integration. A bare unit-only number under 85% is not an acceptable closing metric — report the combined figure.

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

## Quality tooling & gates (industrial)

The quality toolchain lives in **WSL** at `~/go/bin` (installed via `make tools` or `go install`): `golangci-lint` (v2.12.2, CI-pinned), `staticcheck`, `govulncheck`, `dupl`, `gotestsum`, `deadcode`, `goimports`, `go-mutesting`. WSL's login shell does **not** put `~/go/bin` on PATH — invoke with the full path (`/home/davide/go/bin/<tool>`) or `$(go env GOPATH)/bin/<tool>`.

**Cross-environment matrix** (WSL has no `gcc` and no `make` — build-essential is intentionally not installed):

| Task | Where | Why |
|------|-------|-----|
| `go test -race`, CGO | **Windows (w64devkit)** or **CI Linux** | WSL `-race` fails `requires cgo`; prefix Windows race cmds with `BASH_ENV=~/.aura-toolchain.sh` (w64devkit binutils shadow fix) |
| lint / vuln / dupl / staticcheck / deadcode | **WSL** (direct binary) or **CI** | pure-Go analyzers, no CGO |
| mutation (`go-mutesting`) | **WSL** | only fork that supports go1.26; for container-gated code run with `GOFLAGS=-tags=db_integration` + composed DSN env (WSL reaches Windows Docker via `127.0.0.1`); `PASS`=killed, `FAIL`=survived, score=killed/total |
| `make ...` | **Windows (w64devkit)** or **CI Linux** | no `make` in WSL |
| integration tests + coverage | any, **stack must be up** | derive `AURA_DB_URL`/`AURA_DB_MIGRATE_URL` from `POSTGRES_PASSWORD`, add mcp-neo4j-cypher to PATH |

**Gates (enforced in CI, runnable locally):**
- `make quality` — pre-push, no containers: vet + build + file-size + lint(+dupl) + test-race + vuln.
- `make quality-full` — `quality` + coverage gate (needs stack up via `make neo4j-migrate`).
- `make coverage` → `scripts/coverage_gate.sh` — **owned-surface floor ≥85%** (`internal/*` minus generated `sqlc` + pre-rewrite skeletons; `cmd/aura` glue excluded as it's behaviourally covered). Tunable via `AURA_COVERAGE_MIN`. Current: **91.6%**.
- `make vuln` → `govulncheck ./...` — supply-chain CVE scan (CI `vulncheck` job).
- `dupl` is enabled in `.golangci.yml` (threshold 100, `_test.go` excluded — table tests are intentionally repetitive).
- Mutation spot-check ≥70% on each phase's critical file(s); documented in the phase `VALIDATION.md` Manual-Only table (db.go 82.8%, budget.go/budget_dedup.go 89.4%).

**No-skip-as-green** still governs: the coverage gate runs the tagged tiers, which `t.Fatal` under `$CI` when their env is unset — a skipped tier fails the gate, never passes it. Phase validation (deep) executes every tier live, never compile-checks — bring the stack up and run the real integration + smoke + mutation, do not trust a compile-check.

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
