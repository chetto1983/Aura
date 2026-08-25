# CLAUDE.md

Project guidance for Claude Code (claude.ai/code) on this codebase.

- **Spike findings for Aura** (implementation patterns, constraints, gotchas — skills self-extension, sandbox runtime, MCP live servers, AG-UI gateway, Telegram channel) → `Skill("spike-findings-Aura")`

## PRD-first principle — misura, poi emenda, poi implementa

**Un PRD-amendment ha senso SOLO dopo un test reale.** Si misura sullo stack acceso, poi
si scrive l'emendamento per **registrare** la misura, poi si implementa. Mai l'inverso:
un emendamento scritto su una supposizione diventa un vincolo che nessuno ha verificato e
che il codice poi eredita come se fosse un fatto.

Il PRD ([prd.md](prd.md)) resta il posto dove ogni decisione architettonica, file target,
env var e open question è documentata — ma è un **registro di ciò che è stato misurato**,
non un oracolo. Quando una misura contraddice il PRD, **vince la misura** e il PRD si
corregge, citando data ed evidenza.

Ogni emendamento dichiara anche **cosa la misura NON dimostra**: un numero senza il suo
perimetro è un'altra supposizione travestita.

Prezzo misurato di non averlo fatto (2026-08-07, una sola sessione): il PRD pinnava un
artefatto modello (`Qwen3-8B-Q4_K_M`, size + SHA-256) mai scaricato, sostituito a metà
task; il catalogo env portava ancora `AURA_DOCUMENT_CHUNK_MAX_TOKENS=512` dell'era Docling
mentre il tetto vero è **2048 token**, dichiarato dal GGUF di EmbeddingGemma; e il
paragrafo provenance stava per congelare uno schema ArcadeDB invece di misurare cosa la
pipeline scrive davvero.

Corollario operativo: **una suite unitaria verde non chiude niente.** La prova è il test
E2E vero sullo stack acceso, guidato dall'agente reale (vedi §DEFINITION OF DONE).

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



Persistence: Postgres `aura.*` schema (control plane, documenti, catalogo) + ArcadeDB (memoria a lungo termine, un database per identità). **Il numero e il floor di una nuova migration NON si deducono mai né si copiano da questo file: `ls internal/db/migrations/ | tail -1` è l'unica fonte del prossimo slot libero.** L'interfaccia LLM alla memoria è `cmd/arcadedb-mcp`, l'MCP che Aura si scrive da sé.

## Slice Q&A discipline (3 gate sequenziali, mandatory)

Ogni slice attraversa 3 gate (formalizzati nel PRD §Slice Q&A discipline). Mapping ai GSD commands:

| Gate | Cosa | GSD command equivalente |
|---|---|---|
| **Gate 1 — Definition of Ready** (PRE) | Pre-req completati, OQ chiuse, acceptance machine-checkable, smoke runnable, file targets ≤600 LOC, test plan, Risk tier, migration, env catalog, commit template | `/gsd-spec-phase` → `/gsd-discuss-phase` → `/gsd-plan-phase` |
| **Gate 2 — Implementation Q&A** (DURANTE) | `go vet + build + test + race` verdi, refactor-on-touch, no asilo nido, no TODO orphan, no hard-coded env, 3-strike rule | `/gsd-execute-phase` con `gsd-executor` agent |
| **Gate 3 — Definition of Done** (POST pre-merge) | Acceptance ticked, smoke green, integration + regression passing, coverage ≥75% unit / ≥60% integration, mutation testing ≥70% killed, no goroutine leak, no data race, PRD updated | `/gsd-verify-work` → `/gsd-code-review` → `/gsd-audit-fix` → `/gsd-complete-milestone` |

**Niente shortcut.** Niente "lo aggiusto dopo". Niente "il PRD si capisce dal codice".

## GSD tooling (workflow ufficiale)

Installazione: **una sola copia, in HOME** (`~/.claude/`, **1.11.0**, layout skills-based):
72 skill `gsd-*` in `~/.claude/skills/`, tool in `~/.claude/gsd-core/bin/gsd-tools.cjs`,
17 hook wired in `~/.claude/settings.json`, agents in `~/.claude/agents/`.

La copia project-local 1.1.0 è stata **ritirata il 2026-08-25**: comandi rimossi, hook
sganciati da `.claude/settings.json`. Non era una ridondanza innocua -- gli hook dei diversi
scope si **sommano**, non si sovrascrivono, quindi ogni `Edit` faceva partire due
`prompt-guard` e due `read-guard` di versioni divergenti (3.489 B contro 10.445 B), e la
guard vecchia poteva bloccare ciò che la nuova consente. Verificato prima di rimuoverla:
tutti e 67 i comandi `/gsd:*` locali avevano la skill HOME equivalente (HOME ne ha 4 in più:
`gsd-mempalace-capture`, `gsd-mempalace-recall`, `gsd-next`, `gsd-onboard`), e il set di hook
HOME è un superset con matcher più larghi (`workflow-guard` locale su `Write|Edit`, HOME su
`Bash|Edit|Write|MultiEdit`).

**Resta project-local solo `.claude/agents/`** (33 agent): scope diversi non collidono, il
progetto vince e basta, nessuna doppia esecuzione. **`gsd-tools.cjs` si invoca sempre da
HOME.**

> **`.planning/` ESISTE ed è tracciata in git.** Cancellata alla chiusura della milestone
> v2.0.0, è stata **rigenerata il 2026-08-05** (`b1a95faf8`, apertura di v2.1.0
> HERMES-CLAUDE_PARITY) dai comandi GSD `/gsd-ingest-docs`, `/gsd-map-codebase`,
> `/gsd-graphify`. Contiene oggi `PROJECT.md`, `ROADMAP.md`, `REQUIREMENTS.md`,
> `STATE.md`, `INGEST-CONFLICTS.md`, `config.json` e le directory `codebase/`, `intel/`,
> `phases/`, `handoffs/`, `research/`, `debug/`. Gli unici percorsi non versionati sono
> `.planning/tmp/` e `.planning/graphs/*` (vedi `.gitignore`). I riferimenti a
> `.planning/...` qui sotto sono quindi percorsi leggibili adesso, non una struttura
> promessa.
>
> **Ma il contenuto invecchia.** `STATE.md` è fermo al 2026-08-05 (Phase 45 di 52, `status:
> planning`, 0 fasi completate) e `phases/` ne conserva una sola, `32-quality-cleanup-dead-code-shared-helpers`,
> residuo della milestone precedente. Prima di trattare un file di `.planning/` come stato
> corrente, confronta il suo `last_updated` con `git log`: la data nel file è un'asserzione,
> non una misura.

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

## Skills installate

L'inventario vive in `.claude/skills/` e `~/.claude/skills/`: `ls` è la fonte, non questo file.
Il modello sceglie la skill dal `description` nel frontmatter — nessun elenco qui la rende più
raggiungibile. Per cercarne di nuove: `/find-skills`, o
`npx skills add <owner>/<repo> --skill <name> --agent claude-code -y`.

> Misurato 2026-08-25 su 69 avvii e 446 sessioni: 43 delle 49 skill di progetto non erano mai
> state invocate una volta. Sono disattivate via `skillOverrides` in `.claude/settings.local.json`;
> riattivarne una è rimuovere la sua riga.

## Behavioral rules (apply to every change)

- **NEVER SUPPOSE.** Read code before editing. If uncertain about API contract, stop and ask.
- **READ THE DOCUMENTATION FIRST — ALWAYS.** Before writing a line against any external system (ArcadeDB, Postgres, LibreOffice, an MCP server, a library), read its documentation. Not after a failure, not "if the probe is unclear": first. Probing a live system to discover an API is guessing with extra steps, and it produces code that works by accident. Measured cost on 2026-08-01 alone: ArcadeDB's `vector.fuse` needs an options object, backticked names, and a `@rid, $score` full-text leg — all documented, all discovered by trial; `INT8` quantization was copied from the manual's production recommendation without reading the sentence that exempts corpora under 10K vectors; `LIST OF FLOAT` vs `ARRAY_OF_FLOATS` cost a failed write; and half an hour went into probing user management before the page said plainly that a user's databases CANNOT be widened after creation. Each was one page away. Cite the page in the code comment when the behaviour is surprising.
- **INVENTORY BEFORE INVENTION. ALWAYS.** Before writing a component, enumerate what the
  engine, the library or the package you already depend on ALREADY DOES. Read its function
  list, not its landing page. `gh api "search/code?q=repo:<owner>/<repo>+<Symbol>"` and a
  scan of the source tree take two minutes; the component you were about to write takes a
  week and then rots because nobody calls it. Measured on 2026-08-03: ArcadeDB's engine
  exposes **78 vector functions**, among them `SQLFunctionVectorRerank` — a declarative
  prefetch+rerank that re-scores a coarse candidate set against full-precision vectors in
  ONE query. `internal/rerank` had been written, had never acquired a caller, and was
  deleted the same morning. Likewise `vector.multiscore` / `hybridscore` / `rrfscore` /
  `normalizescores` were sitting in the engine while a Go fusion layer was being planned,
  and `vector.neighbors` takes a `{filter: <RIDs>}` so a graph traversal can pick the
  candidate set before ANN runs. The rule cuts BOTH ways and the second half is the one
  that costs money: the same search proved ArcadeDB has NO text-to-vector function at all
  (`SQLFunctionVectorEmbed`/`VectorEncode`/`VectorEmbedding` = zero hits), so "bring your
  own embedding model" is literal and the embedding sweep genuinely must be ours. Answer
  BUILD vs REUSE with a grep over the dependency, never with an assumption in either
  direction.
- **STOP BEFORE BESPOKE. ASK, DO NOT ASSUME.** The moment you are about to write a custom
  component, adapter, wrapper or protocol implementation against a dependency: STOP. Do not
  write it. Post the inventory you actually ran and the gap you believe it leaves, and
  confirm with the human BEFORE a line is written. "The docs do not mention it" is NOT
  evidence of absence, and neither is "the connector I looked at does not expose it": the
  capability is often orthogonal to the place you looked. Enumerate the whole public
  surface (`__all__`, the exported symbol list, the module tree of the INSTALLED version --
  not the landing page, not the docs site, not one source file), because a package will
  ship a public API its own documentation never demonstrates. Measured on 2026-08-06:
  cocoindex's S3 connector genuinely has no live mode (`list_objects` rejects `live=` with
  TypeError, `items()` is a bare async_generator with no `watch()`), and from that a custom
  LiveComponent of about 50 lines was proposed and nearly written. It was not needed.
  `coco.auto_refresh(fn, interval=...)` is in `coco.__all__`, wraps ANY process function as
  a LiveComponent, and is orthogonal to the source -- it does not appear in the connector
  docs or the connector table at all. One grep of the installed package's `__all__` would
  have found it before the design went the wrong way; the docs alone did not.
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
- **COVERAGE GATE TAG SET = `db_integration` ONLY.** `scripts/coverage_gate.sh` defaults to it (`AURA_COVERAGE_TAGS`) and runs in TWO CI jobs — `ci.yml` and `skills.yml` — with the SAME tag set since the graph store was retired, so the two numbers no longer diverge. **Two tiers run in CI but feed NO coverage**: `docker_integration` (job `sandbox-docker-integration` — `internal/sandbox/usersandbox` DockerBackend lifecycle/exec/egress, the routed branches in `internal/agent/tools`) proves the box works and counts zero toward the floor; that split is why the CAP_NET_ADMIN cap-assertion bug stayed latent (WR-01). **And `arcadedb_integration` genuinely runs, but not for coverage: it executes in CI job `arcadedb-integration-test` (`.github/workflows/ci.yml:713`) via `make agent-memory-eval` (`Makefile:176-178`, `.github/workflows/ci.yml:811`), which runs `go test -race -tags=arcadedb_integration -coverprofile=...` against `./internal/arcadedb/` (`scripts/agent_memory_eval.py:52`) — compiled, race-checked, and coverage-profiled. What it does NOT do is feed the 85% floor: `scripts/coverage_gate.sh:29` defaults `AURA_COVERAGE_TAGS` to `db_integration` only, so this tier's coverage profile is produced and then never aggregated into the gate. That runs-but-feeds-no-coverage split is the real remaining concern — folding it into `AURA_COVERAGE_TAGS` is deliberately deferred (would require a live ArcadeDB + embed sidecar + MCP in every coverage run, including local `scripts/coverage_docker.sh`, and would re-base the last full-matrix baseline for reasons unrelated to whatever change prompted it).** **When you add daemon/container-gated runtime code you MUST also write daemon-free unit tests for its pure logic** — spec/tar builders, path-traversal + symlink guards, nil/disabled early-return paths, structural-capability "not supported" errors — or the aggregate silently drops below 85% and CI fails ~20 min after push. **Verify locally BEFORE pushing with `bash scripts/coverage_docker.sh`** (needs the stack up) — the gate provisions a DISPOSABLE coverage DB (`aura_cov`, owned by `aura_migrate`) and drops it on exit, NEVER the live `aura`; it also refuses `db_integration` against a DB named `aura` when run locally (unset `GITHUB_ACTIONS`). This closed a 2026-07-10 footgun where the gate truncated the live deployment's auth tables (operator identity + `authula` wiped, no backup). A green local full-matrix run is worth more than a push-and-wait CI cycle.
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

**WSL is the full primary dev environment** — it runs everything: `gcc` 15 + GNU `make` (build-essential), `CGO_ENABLED=1` so native `go test -race` works, and the Go quality toolchain in `~/go/bin` (`make tools` or `go install`): `golangci-lint` (v2.12.2, CI-pinned), `staticcheck`, `govulncheck`, `dupl`, `gotestsum`, `deadcode`, `goimports`, `go-mutesting`. The login shell does **not** put `~/go/bin` or `~/.local/bin` on PATH — prepend both (`export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"`) or invoke by full path. The whole `make quality-full` gate (incl. db integration coverage) passes natively in WSL.

**Where to run what:**

| Task | Where | Notes |
|------|-------|-----|
| everything (`make quality-full`, race, integration, coverage, mutation, lint, vuln) | **WSL** (primary) or **CI Linux** | WSL reaches the Windows Docker stack via `127.0.0.1`; prepend `~/.local/bin:~/go/bin` to PATH and export the composed DSNs (see integration env below) |
| `go test -race` on **Windows** (w64devkit) | alt only | prefix with `BASH_ENV=~/.aura-toolchain.sh` (binutils-shadow fix); WSL native race is simpler |
| mutation (`go-mutesting`) | **WSL** | only fork supporting go1.26; for container-gated code add `GOFLAGS=-tags=db_integration` + the DSN env; `PASS`=killed, `FAIL`=survived, score=killed/total |
| integration env | any, **stack up** | derive `AURA_DB_URL`/`AURA_DB_MIGRATE_URL` from `POSTGRES_PASSWORD`; `.env` carries the ArcadeDB/embed vars |

> WSL apt installs (build-essential, pipx) were done as root via `wsl -u root` (passwordless from the Windows host — interactive `sudo` is not available).

**Gates (enforced in CI, runnable locally):**
- `make quality` — pre-push, no containers: vet + file-size + lint(+dupl) + deadcode + test-race + vuln, then `go build $(GO_PACKAGES)` as the recipe body. (There is no standalone `build:` target — `build` is a recipe line here and a separate lefthook pre-push command; `Makefile:88-90` is the source of truth.)
- `make quality-full` — `quality` + coverage gate (needs the stack up via `make db-migrate memory-up`).
- `make coverage` → `scripts/coverage_gate.sh` — **owned-surface floor ≥85%** (`internal/*` minus generated `sqlc` + pre-rewrite skeletons + test-support `internal/agent/agenttest`; `cmd/aura` glue excluded as it's behaviourally covered). Tunable via `AURA_COVERAGE_MIN`. **Last full-matrix measurement: 90.3%** (2026-06-13 @ HEAD `882df109`) — measured on a `db_integration neo4j_integration` matrix that NO LONGER EXISTS, and **not re-measured since: phases 37A-37F, 42, 43 and the graph-store removal have all landed after it.** Treat 90.3% as the last known figure, not as current state. The floor is **un solo aggregato, non per package**: `scripts/coverage_gate.sh:86-95` somma `covered`/`total` su tutto il profilo filtrato e `:97-107` confronta quell'unico numero con `MIN` — non c'è nessun ciclo per package, quindi un package debole viaggia sulle spalle dei forti. È deliberato finché il gate gira un solo tag set: sotto `db_integration` da solo un floor per-package boccerebbe i package la cui coverage vera vive in un altro tier — `internal/arcadedb` misura **92,81% (943/1016)** sotto `arcadedb_integration` (2026-08-03, `docs/aura-quality-snapshot.md:16`) e molto meno sotto il tier del gate. I numeri per package restano attestati riga per riga in `docs/aura-quality-snapshot.md`, per misura, non per gate. The 2026-06-13 campaign raised 16 sub-floor packages (e.g. `skilladapters` 0→100%, `reasoningtrace` 71.8→95.8%, `cron/handlers` 71.1→96.9%, `runner` 72.4→96.2%, `db` 76.5→90.2% via `WithTx`/`MigrateSteps`, `onboarding` 79→96.8%); the earlier "86.0%"/"≈91.7%" figures are superseded.
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
- **ArcadeDB** (`compose.yaml`): memoria a lungo termine, **un database per identità** creato al provisioning con credenziale derivata per tenant (HMAC su `AURA_ARCADEDB_TENANT_SECRET`) — l'isolamento lo impone il server, non una WHERE che ci si può dimenticare. Fatti bitemporali (`valid_from`/`valid_to` + supersede), indice vettoriale nativo e full-text nello stesso motore. L'interfaccia LLM è `cmd/arcadedb-mcp`. Richiede ≥ 26.4.2 (CVE-2026-44221): `VerifySecureVersion` rifiuta di partire sotto quella soglia.
- **Filesystem** per artifact: `$AURA_RUN_DIR/` (sidecar tool results + spillover content) + `~/.aura/agents/<id>/` (Agent.md profile) + `~/.aura/pyscripts/<id>/` (Slice 7e snippets) + `$AURA_SKILLS_DIR/` (skills instruction).
- **Backup**: Postgres `pg_dump` (vedi PRD §Backup strategy).

## Env vars

Tutti gli env vars usano convenzione `AURA_<DOMAIN>_<UNIT>` (es. `AURA_SWARM_MAX_DEPTH`). Eccezioni: env per librerie/sidecar di terze parti (`TELEGRAM_BOT_TOKEN`, `OPENROUTER_API_KEY`, `MULTIMODAL_*`, `LLAMA_*`, `LMCACHE_*`) mantengono naming canonico upstream.

Indice completo: vedi PRD §Caps & Limits → Indice completo env vars (~60 voci catalogate).
