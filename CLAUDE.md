# CLAUDE.md

Project guidance for Claude Code (claude.ai/code) on this codebase.

- **Spike findings for Aura** (implementation patterns, constraints, gotchas — skills self-extension, sandbox runtime, MCP live servers, AG-UI gateway, Telegram channel) → `Skill("spike-findings-Aura")`

## PRD-first principle (absolute)

**Senza PRD completo non si scrive una riga di codice.** Il PRD ([prd.md](prd.md)) è la **truth-source**, non un suggerimento. Ogni decisione architettonica, ogni file target, ogni env var, ogni open question è documentata lì. Deviazioni dal PRD richiedono PRD-amendment commit prima dell'implementazione (vedi §Slice Q&A discipline → Q&A revision protocol nel PRD).

## Frontend_aesthetics

You tend to converge toward generic, "on distribution" outputs. In frontend design,this creates what users call the "AI slop" aesthetic. Avoid this: make creative,distinctive frontends that surprise and delight.

Focus on:

- Typography: Choose fonts that are beautiful, unique, and interesting. Avoid generic fonts like Arial and Inter; opt instead for distinctive choices that elevate the frontend's aesthetics.
- Color & Theme: Commit to a cohesive aesthetic. Use CSS variables for consistency. Dominant colors with sharp accents outperform timid, evenly-distributed palettes. Draw from IDE themes and cultural aesthetics for inspiration.
- Motion: Use animations for effects and micro-interactions. Prioritize CSS-only solutions for HTML. Use Motion library for React when available. Focus on high-impact moments: one well-orchestrated page load with staggered reveals (animation-delay) creates more delight than scattered micro-interactions.
- Backgrounds: Create atmosphere and depth rather than defaulting to solid colors. Layer CSS gradients, use geometric patterns, or add contextual effects that match the overall aesthetic.

Avoid generic AI-generated aesthetics:

- Overused font families (Inter, Roboto, Arial, system fonts)
- Clichéd color schemes (particularly purple gradients on white backgrounds)
- Predictable layouts and component patterns
- Cookie-cutter design that lacks context-specific character

Interpret creatively and make unexpected choices that feel genuinely designed for the context. Vary between light and dark themes, different fonts, different aesthetics. You still tend to converge on common choices (Space Grotesk, for example) across generations. Avoid this: it is critical that you think outside the box!



Persistence: Postgres `aura.*` schema + Neo4j Cypher migrations. **Il numero e il floor di una nuova migration NON si deducono mai né si copiano da questo file: `ls internal/db/migrations/ | tail -1` (o `ls internal/knowledge/migrations/ | tail -1` per il Cypher) è l'unica fonte del prossimo slot libero.** `mcp-neo4j-cypher` MCP server è l'interfaccia LLM al graph.

## Slice Q&A discipline (3 gate sequenziali, mandatory)

Ogni slice attraversa 3 gate (formalizzati nel PRD §Slice Q&A discipline). Mapping ai GSD commands:

| Gate | Cosa | GSD command equivalente |
|---|---|---|
| **Gate 1 — Definition of Ready** (PRE) | Pre-req completati, OQ chiuse, acceptance machine-checkable, smoke runnable, file targets ≤600 LOC, test plan, Risk tier, migration, env catalog, commit template | `/gsd-spec-phase` → `/gsd-discuss-phase` → `/gsd-plan-phase` |
| **Gate 2 — Implementation Q&A** (DURANTE) | `go vet + build + test + race` verdi, refactor-on-touch, no asilo nido, no TODO orphan, no hard-coded env, 3-strike rule | `/gsd-execute-phase` con `gsd-executor` agent |
| **Gate 3 — Definition of Done** (POST pre-merge) | Acceptance ticked, smoke green, integration + regression passing, coverage ≥75% unit / ≥60% integration, mutation testing ≥70% killed, no goroutine leak, no data race, PRD updated | `/gsd-verify-work` → `/gsd-code-review` → `/gsd-audit-fix` → `/gsd-complete-milestone` |

**Niente shortcut.** Niente "lo aggiusto dopo". Niente "il PRD si capisce dal codice".

## GSD tooling (workflow ufficiale)

Installazione: `.claude/` con 67 commands + 33 agents + 9 hooks attivi (13 script in `.claude/hooks/`, 9 wired — vedi `.claude/settings.json`). Versione: 1.1.0.

> **Slice → Phase (Rosetta).** Il PRD numera per **Slice** (0.5, 0.7, 1, 3, 11a-e, 13 — vocabolario storico, tuttora in `prd.md`); `.planning/ROADMAP.md`, `.planning/phases/` e gli scope dei commit numerano per **Phase** (0-43). Le due sequenze NON coincidono: una Slice può atterrare in una Phase con numero diverso. Per qualunque decisione di ordine/atterraggio (migrations su tutte) vale l'**ordine-fase**, mai l'ordine-slice.

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
- `/gsd-validate-phase` — Nyquist validation gaps (test discipline rigorosa); **spawna l'agent `gsd-nyquist-auditor`, che NON è un comando invocabile**
- `/gsd-add-tests` — test generation da UAT criteria
- `/gsd-graphify` — knowledge graph del progetto in `.planning/graphs/`

Bootstrap inziale (one-shot):
- `/gsd-ingest-docs` — importa prd.md esistente in `.planning/` setup (PRD → ADR/SPEC structured)
- `/gsd-map-codebase` — analizza il codebase esistente in `.planning/codebase/` (v0.0.0 Phase 0-21 + v1.0.0 Phase 22-30 shippate, v2.0.0 Phase 31-42 in corso — **~98k LOC non-test su 68 package**, di cui ~7k sqlc-generated; ~143k LOC di test)

## Skills installate (`.claude/skills/`, 69 totali — 7.6 MB)

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
| `neo4j-vector-index-skill` | **Slice 0.7 + 11a** | HNSW vector index (CREATE VECTOR INDEX, cosine, dimensions 1024d) |
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

### Frontend/web + tooling (23 skills, installate post-v1.0.0)

| Skill | Slice/Phase mappato | Use case |
|---|---|---|
| `frontend-design`, `accessibility`, `shadcn`, `vercel-react-best-practices`, `vite` | **v1.0.0 Cockpit** (Phase 22-30) | Design distintivo (vedi §Frontend_aesthetics), WCAG 2.2, componenti, build |
| `assistant-ui`, `primitives`, `runtime`, `streaming`, `tools` | **v1.0.0 Cockpit chat/SSE** | assistant-ui: runtime selection, primitives, wire protocol AG-UI/SSE |
| `printing-press` + 9 varianti (`-amend/-catalog/-import/-output-review/-polish/-publish/-reprint/-retro/-score`) | CLI per API esterne | Genera CLI agent-ready per API di terze parti (NON per il codice Aura) |
| `claude-api` | Slice 1/3 LLM | Anthropic SDK + prompt caching + tool use |
| `xlsx` | Slice 11b ingest | Spreadsheet come input/output di un task |
| `spike-findings-Aura` | **meta — auto-caricata** | Blueprint dagli spike (vedi riga 5 di questo file) |

### Trigger automatic

Il modello detecta skill rilevante dal frontmatter `description` (es. "Use when writing or reviewing Go tests" → triggera `golang-testing`). Skill caricate solo quando richieste — no token bloat in default manifest.

### Espandere il set

- `/find-skills` (skill meta) per cercare nuove
- `npx skills add <owner>/<repo> --skill <name> --agent claude-code -y` per install
- Repo noti: `samber/cc-skills-golang` (40 Go skills), `neo4j-contrib/neo4j-skills` (25 graph skills), `trailofbits/skills` (security), `anthropics/skills` (utility), `vercel-labs/skills` (find-skills + agent-skills)

## Behavioral rules (apply to every change)

- **NEVER SUPPOSE.** Read code before editing. If uncertain about API contract, stop and ask.
- **READ THE DOCUMENTATION FIRST — ALWAYS.** Before writing a line against any external system (ArcadeDB, Postgres, LibreOffice, an MCP server, a library), read its documentation. Not after a failure, not "if the probe is unclear": first. Probing a live system to discover an API is guessing with extra steps, and it produces code that works by accident. Measured cost on 2026-08-01 alone: ArcadeDB's `vector.fuse` needs an options object, backticked names, and a `@rid, $score` full-text leg — all documented, all discovered by trial; `INT8` quantization was copied from the manual's production recommendation without reading the sentence that exempts corpora under 10K vectors; `LIST OF FLOAT` vs `ARRAY_OF_FLOATS` cost a failed write; and half an hour went into probing user management before the page said plainly that a user's databases CANNOT be widened after creation. Each was one page away. Cite the page in the code comment when the behaviour is surprising.
- **NOT MY WORK.** If Bug or gap found fix on touch. Never Skip.
- **READ BEFORE EDIT.** Re-read a file you haven't touched in the last 5 messages.
- **3-STRIKE RULE.** Same failing approach max 3 times. On strike 3, stop and ask (or escalate via PRD-amendment, vedi PRD §Q&A escalation).
- **NEVER MODIFY TESTS TO MAKE THEM PASS** unless the test itself is broken. Fix the code or rewrite the test with explicit justification in commit message.
- **SCOPE CONTROL.** Do exactly what was asked. No unrequested features, refactors, or improvements.
- **FOLLOW EXISTING PATTERNS.** Never invent new approaches when codebase patterns exist.
- **NO GOD CLASS.** Never create a file >600 LOC. Refactor on touch (split into `<name>_<concern>.go`). Legacy if forbidden too remove olde code and unused param. DARK CODE IS FORBIDDEN TOO.
- **CODE BASE RULES.** Code base must be clean and readable. Remove unnecessary complex part. Code must be work on same way in simpler implementation. in case of doubt read:
"""
Beautiful is better than ugly.
Explicit is better than implicit.
Simple is better than complex.
Complex is better than complicated.
Flat is better than nested.
Sparse is better than dense.
Readability counts.
Special cases aren't special enough to break the rules.
Although practicality beats purity.
Errors should never pass silently.
Unless explicitly silenced.
In the face of ambiguity, refuse the temptation to guess.
There should be one-- and preferably only one --obvious way to do it.
Although that way may not be obvious at first.
Now is better than never.
Although never is often better than *right* now.
If the implementation is hard to explain, it's a bad idea.
If the implementation is easy to explain, it may be a good idea.
"""
- **REUSABLE CODE.** Never duplicate; extract a helper.
- **DEEP REFACTOR ON TOUCH.** Every file you edit gets dead-code removal + dupl-folding + LOC ≤600 + comments-updated in the SAME commit.
- **GIT PUSH DISCIPLINE.**  `git push` (or any remote-mutating command) at the end of a phase or a competed job and check all CI are green.
- **MERGE ON EVERY TASK CLOSE.** After implementation, required reviews, plan tracking, and the task's atomic commits are complete, merge the task branch/worktree into `master` and run fresh post-merge verification before starting the next task. A task is not closed while it exists only in a feature branch or linked worktree. Preserve unrelated dirty files during the merge; do not push unless the phase/job push rule or the user explicitly requires it.
- **QUALITY SNAPSHOT AT PHASE CLOSE (last phase/plan, before the phase-close push).** Update `docs/aura-quality-snapshot.md`: for EVERY row whose CI-gate-path glob matches a file changed in the phase, bump `Last measured` to today and PREPEND a re-attestation note — a fresh measurement if the metric moved, else a metric-neutral justification naming exactly what changed and why the number cannot move (keep the prior notes as `Prior …`). The CI job `scripts/quality_snapshot_gate.sh` fails the push otherwise (PRD amendment #20). Verify locally FIRST (must print `ok: … checked N row(s)`): `AURA_QUALITY_CHANGED_FILES="$(git diff --name-only origin/master..HEAD)" AURA_QUALITY_BASE_DATE="$(git log -1 --format=%cs origin/master)" bash scripts/quality_snapshot_gate.sh`.
- **NO COMMENTS UNLESS WHY IS NON-OBVIOUS.** Identifier names already explain what. Comments only for hidden constraints, workarounds, or surprising behavior.
- **NO TEST BASY SITTING.** Tests must follow PRD §Test discipline rigorosa: realistic fixtures, goleak, race detector, property-based dove indicato, build tags integration, coverage threshold, mutation testing spot-check. Cita la tabella esempi per slice.
- **NO SKIP-AS-GREEN IN CI.** Integration/smoke tests must actually run in the pipeline — a `t.Skip` that fires under `$CI` is a falsely-green job exercising nothing. (1) CI jobs export the exact env the tests read (composed DSNs `AURA_DB_URL`/`AURA_DB_MIGRATE_URL`, not just the `POSTGRES_*` primitives `config.Load` composes for the CLI). (2) Skip-helpers (`envOrSkip` and inline `t.Skip`) call `t.Fatal` when a required var is unset and `$CI` is set; locally they still skip. A sub-second "integration" runtime is a skip tell — verify execution, not just PASS.
- **COVERAGE FLOOR 85%.** No phase/slice closes below 85% measured coverage across the full tag matrix (unit + integration + smoke). This overrides the PRD's ≥75% unit / ≥60% integration. A bare unit-only number under 85% is not an acceptable closing metric — report the combined figure.
- **COVERAGE GATE TAG SET = `db_integration neo4j_integration` ONLY (the `docker_integration` tier DOES run in CI since fix plan 2.6 — job `sandbox-docker-integration` in `ci.yml` — but it feeds NO coverage).** The gate (`scripts/coverage_gate.sh`) runs in **TWO** CI jobs: the **Knowledge** job (`ci.yml`, tags `db_integration neo4j_integration`) and the **Skills gate** (`skills.yml`, **`db_integration` ONLY** — hence the stricter of the two: a package can clear Knowledge and still fail Skills, so **at phase close verify THAT number**). Any package whose runtime is exercised ONLY under another tag — above all `docker_integration` (e.g. `internal/sandbox/usersandbox` DockerBackend lifecycle/exec/egress, the routed branches in `internal/agent/tools`) — counts as UNCOVERED in the owned-surface floor: those `//go:build docker_integration` tests now **execute in `sandbox-docker-integration` but are absent from the coverage matrix, so they still contribute ZERO coverage** (before that job they compile+skipped everywhere, which is why the CAP_NET_ADMIN cap-assertion bug stayed latent — WR-01). Behavioural signal and coverage credit are separate things here: the tier proves the box lifecycle/egress/routing works, and the floor still counts none of it. **When you add daemon/container-gated runtime code you MUST also write daemon-free unit tests for its pure logic** — spec/tar builders, path-traversal + symlink guards, nil/disabled early-return paths, structural-capability "not supported" errors — or the aggregate silently drops below 85% and CI fails ~20 min after push. **Verify locally BEFORE pushing with `bash scripts/coverage_docker.sh`** (containerized mcp-neo4j-cypher, needs the stack up) — the gate now provisions a DISPOSABLE coverage DB (`aura_cov`, owned by `aura_migrate`) and drops it on exit, NEVER the live `aura`; `scripts/coverage_gate.sh` also refuses `db_integration` against a DB named `aura` when run locally (unset `GITHUB_ACTIONS`). This closed a 2026-07-10 footgun where the gate truncated the live deployment's auth tables (operator identity + `authula` wiped, no backup). A green local full-matrix run is worth more than a push-and-wait CI cycle. See test-class rules in `docs/` and the coverage campaign history below.
- **DEFINITION OF DONE** Phase/Job are complete when is fully validate E2E at score >9.8 on real scenario.
- **AUDIT** refer to \docs\audit for audit finding and improvement on codebase test and observability

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

**WSL is the full primary dev environment** — it runs everything: `gcc` 15 + GNU `make` (build-essential), `CGO_ENABLED=1` so native `go test -race` works, `mcp-neo4j-cypher` 0.6.0 (pipx, `~/.local/bin`), and the Go quality toolchain in `~/go/bin` (`make tools` or `go install`): `golangci-lint` (v2.12.2, CI-pinned), `staticcheck`, `govulncheck`, `dupl`, `gotestsum`, `deadcode`, `goimports`, `go-mutesting`. The login shell does **not** put `~/go/bin` or `~/.local/bin` on PATH — prepend both (`export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"`) or invoke by full path. The whole `make quality-full` gate (incl. db+neo4j integration coverage) passes natively in WSL.

**Where to run what:**

| Task | Where | Notes |
|------|-------|-----|
| everything (`make quality-full`, race, integration, coverage, mutation, lint, vuln) | **WSL** (primary) or **CI Linux** | WSL reaches the Windows Docker stack via `127.0.0.1`; prepend `~/.local/bin:~/go/bin` to PATH and export the composed DSNs (see integration env below) |
| `go test -race` on **Windows** (w64devkit) | alt only | prefix with `BASH_ENV=~/.aura-toolchain.sh` (binutils-shadow fix); WSL native race is simpler |
| mutation (`go-mutesting`) | **WSL** | only fork supporting go1.26; for container-gated code add `GOFLAGS=-tags=db_integration` + the DSN env; `PASS`=killed, `FAIL`=survived, score=killed/total |
| integration env | any, **stack up** | derive `AURA_DB_URL`/`AURA_DB_MIGRATE_URL` from `POSTGRES_PASSWORD`; `mcp-neo4j-cypher` on PATH; `.env` carries the Neo4j/embed vars |

> WSL apt installs (build-essential, pipx) were done as root via `wsl -u root` (passwordless from the Windows host — interactive `sudo` is not available).

**Gates (enforced in CI, runnable locally):**
- `make quality` — pre-push, no containers: vet + file-size + lint(+dupl) + deadcode + test-race + vuln, then `go build $(GO_PACKAGES)` as the recipe body. (There is no standalone `build:` target — `build` is a recipe line here and a separate lefthook pre-push command; `Makefile:88-90` is the source of truth.)
- `make quality-full` — `quality` + coverage gate (needs stack up via `make neo4j-migrate`).
- `make coverage` → `scripts/coverage_gate.sh` — **owned-surface floor ≥85%** (`internal/*` minus generated `sqlc` + pre-rewrite skeletons + test-support `internal/agent/agenttest`; `cmd/aura` glue excluded as it's behaviourally covered). Tunable via `AURA_COVERAGE_MIN`. **Last full-matrix measurement: 90.3%** (2026-06-13 @ HEAD `882df109`, full `db_integration neo4j_integration` matrix on the live stack) — **NOT re-measured since: phases 37A-37F, 42 and 43 have landed after it.** Treat 90.3% as the last known figure, not as current state. The ≥85% per-package floor is **enforced by the gate on every run**, not a standing attestation: for current per-row numbers see `docs/aura-quality-snapshot.md`. The 2026-06-13 campaign raised 16 sub-floor packages (e.g. `skilladapters` 0→100%, `reasoningtrace` 71.8→95.8%, `cron/handlers` 71.1→96.9%, `runner` 72.4→96.2%, `db` 76.5→90.2% via `WithTx`/`MigrateSteps`, `onboarding` 79→96.8%); the earlier "86.0%"/"≈91.7%" figures are superseded.
- `make vuln` → `govulncheck ./...` — supply-chain CVE scan (CI `vulncheck` job).
- `dupl` is enabled in `.golangci.yml` (threshold 100, `_test.go` excluded — table tests are intentionally repetitive).
- Mutation spot-check ≥70% on each phase's critical file(s); documented in the phase `VALIDATION.md` Manual-Only table (recent: 37F-03 SC3 core 87.5% killed; the v0.0.0 examples db.go 82.8% + budget.go/budget_dedup.go 89.4% are now archived under `.planning/milestones/v0.0.0-phases/`).

**No-skip-as-green** still governs: the coverage gate runs the tagged tiers, which `t.Fatal` under `$CI` when their env is unset — a skipped tier fails the gate, never passes it. Phase validation (deep) executes every tier live, never compile-checks — bring the stack up and run the real integration + smoke + mutation, do not trust a compile-check.

## Commit discipline

- **One slice = one commit** (o N per sub-slice con atomicity nota nel PRD).
- Atomic. Commit message: imperative subject + body explaining *why*.
- Co-Authored-By trailer per project convention.
- PRD-amendment commit prima del code commit se la slice ha rivelato un buco architettonico (vedi PRD §Q&A revision protocol).

## Persistence

- **Postgres** primary (port `5432`): schema `aura.*`, sqlc-generated client, golang-migrate. Il conteggio/floor corrente si legge dalla directory, mai da una cifra hardcoded in questo documento.
  - **Migration numbering — regola imperativa.** Il numero si assegna **all'atterraggio = prossimo intero libero quando la PHASE esegue** (ordine-fase, NON ordine-slice). **Prima di creare una migration esegui `ls internal/db/migrations/ | tail -1` e usa il successivo: il numero non si deduce, non si calcola dalla slice, non si copia da questo file** — questo file invecchia, la directory no. I numeri hardcodati nelle sezioni slice del PRD sono **indicativi** e superseduti da questa regola. Fonte di verità: prd.md §Persistence "Migration numbering — fonte di verità".
- **Neo4j** Community + APOC + GDS (`compose.yaml`): port `7687` bolt, `7474` browser. HNSW vector indexes use the native 1024d cosine contract. Cypher migrations live in `internal/knowledge/migrations/` — sequenza separata da Postgres, **stessa regola imperativa**: `ls internal/knowledge/migrations/ | tail -1` prima di aggiungerne una. `mcp-neo4j-cypher` MCP server è l'interfaccia LLM al graph (no native Go adapter).
- **Filesystem** per artifact: `$AURA_RUN_DIR/` (sidecar tool results + spillover content) + `~/.aura/agents/<id>/` (Agent.md profile) + `~/.aura/pyscripts/<id>/` (Slice 7e snippets) + `$AURA_SKILLS_DIR/` (skills instruction).
- **Backup**: Postgres `pg_dump` + Neo4j `neo4j-admin database dump` (vedi PRD §Backup strategy).

## Env vars

Tutti gli env vars usano convenzione `AURA_<DOMAIN>_<UNIT>` (es. `AURA_SWARM_MAX_DEPTH`). Eccezioni: env per librerie/sidecar di terze parti (`TELEGRAM_BOT_TOKEN`, `OPENROUTER_API_KEY`, `MULTIMODAL_*`, `LLAMA_*`, `LMCACHE_*`) mantengono naming canonico upstream.

Indice completo: vedi PRD §Caps & Limits → Indice completo env vars (~60 voci catalogate).
