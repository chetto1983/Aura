# CLAUDE.md

Project guidance for Claude Code (claude.ai/code) on this codebase.

- **Spike findings for Aura** (implementation patterns, constraints, gotchas — skills self-extension, sandbox runtime, MCP live servers, AG-UI gateway, Telegram channel) → `Skill("spike-findings-Aura")`
- **Consolidamento 2026-10-09 — Aura è l'ospite del lavoro dell'anno.** PMSync, StickyFlow,
  wpt-iot, sacchi_agent, Market_MCP e le letture di OpenDots e SVAR cedono ad Aura
  **comportamenti, mai codice**: inventario ordinato, con la misura che gata ogni atterraggio,
  in `docs/superpowers/specs/2026-10-09-consolidation-best-of-design.md` (PRD §1).

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

Ogni slice attraversa 3 gate (formalizzati nel PRD §Slice Q&A discipline). Mapping alle skill
superpowers (dal 2026-10-09; prima il workflow era GSD, vedi sotto):

| Gate | Cosa | Skill superpowers |
|---|---|---|
| **Gate 1 — Definition of Ready** (PRE) | Pre-req completati, OQ chiuse, acceptance machine-checkable, smoke runnable, file targets ≤600 LOC, test plan, Risk tier, migration, env catalog, commit template | `brainstorming` → spec in `docs/superpowers/specs/` → `writing-plans` → piano in `docs/superpowers/plans/` |
| **Gate 2 — Implementation Q&A** (DURANTE) | `go vet + build + test + race` verdi, refactor-on-touch, no asilo nido, no TODO orphan, no hard-coded env, 3-strike rule | `executing-plans` o `subagent-driven-development`, task per task con `test-driven-development`; `systematic-debugging` quando qualcosa è rosso |
| **Gate 3 — Definition of Done** (POST pre-merge) | Acceptance ticked, smoke green, integration + regression passing, coverage ≥75% unit / ≥60% integration, mutation testing ≥70% killed, no goroutine leak, no data race, PRD updated | `verification-before-completion`, `requesting-code-review`, poi il run live sul lab VM con il suo file in `docs/superpowers/verification/` |

**Niente shortcut.** Niente "lo aggiusto dopo". Niente "il PRD si capisce dal codice".

## Superpowers (workflow ufficiale dal 2026-10-09)

Il workflow è quello del plugin superpowers (`obra/superpowers`): una spec per decisione in
`docs/superpowers/specs/YYYY-MM-DD-<tema>-design.md`, un piano per implementazione in
`docs/superpowers/plans/YYYY-MM-DD-<tema>.md` con task a checkbox, passi TDD e comandi di
verifica, l'esecuzione task per task con un commit atomico per task, e un file datato in
`docs/superpowers/verification/` per ogni run di accettazione. Il formato dei piani esistenti
(`Goal`, `Architecture`, `Tech Stack`, `Spec`, `Global Constraints`, `Review Focus`,
`Decisions taken while writing this plan`, `File structure`, i task) è il modello: si copia
quello, non si inventa un altro.

**GSD è ritirato dal 2026-10-09** per decisione dell'utente. Le note che seguono sulla
`.planning/` restano vere come storia: la directory esiste ancora ed è tracciata, ma i comandi
`/gsd-*` non sono più il workflow e nessun nuovo piano vi atterra. Il ritiro delle copie
project-local di GSD (2026-08-25 e 2026-08-27) è nella storia git di questo file.

> **`.planning/` ESISTE ed è tracciata in git.** Cancellata alla chiusura della milestone
> v2.0.0, è stata **rigenerata il 2026-08-05** (`b1a95faf8`, apertura di v2.1.0
> HERMES-CLAUDE_PARITY) dai comandi GSD `/gsd-ingest-docs`, `/gsd-map-codebase`,
> `/gsd-graphify`. L'inventario è `git ls-files .planning`, non un elenco in questo file.
> Gli unici percorsi non versionati sono `.planning/tmp/` e `.planning/graphs/*` (vedi
> `.gitignore`).
>
> **Le directory di fase vecchie non ci sono più.** `5bff1faa4` (2026-09-07) ha cancellato
> tutte le fasi della milestone precedente, `32-quality-cleanup-dead-code-shared-helpers`
> compresa: 193 file, raggiungibili solo dalla storia git (`git show 5bff1faa4^:<path>`).
> `.planning/milestones/` e `MILESTONES.md` erano già stati rimossi in `9f9f2d974`
> (2026-08-02). Misurato il 2026-10-03, `phases/` contiene solo le fasi della milestone
> v1.1.0 *Production Launch — Multi-Tenant* (`01-…`, `02-…`, `03-…`, numerazione ripartita
> da 01), e `STATE.md` dichiara la fase 02 `complete` con `last_updated` 2026-09-12. Un test
> che scrive dentro `.planning/phases/<fase cancellata>/` ricrea una directory non tracciata:
> le evidenze di test vanno allegate al risultato (`testInfo.attach`), non scritte nel tree.
>
> **Il contenuto invecchia.** Prima di trattare un file di `.planning/` come stato corrente,
> confronta il suo `last_updated` con `git log`: la data nel file è un'asserzione, non una
> misura. Misurato 2026-10-09: `STATE.md` dava la fase 02 chiusa al 2026-09-12 mentre la
> v1.1.0 è uscita l'8 ottobre (`CHANGELOG.md`); la roadmap non aveva seguito il rilascio. E su un clone shallow (il default delle sessioni cloud) `git log` su un percorso
> mente per omissione: `git rev-parse --is-shallow-repository` prima di concludere che un
> percorso "non è mai stato tracciato".

> **Slice → Phase (Rosetta).** Il PRD numera per **Slice** (0.5, 0.7, 1, 3, 11a-e, 13 — vocabolario storico, tuttora in `prd.md`); `.planning/ROADMAP.md`, `.planning/phases/` e gli scope dei commit numerano per **Phase** (0-43). Le due sequenze NON coincidono: una Slice può atterrare in una Phase con numero diverso. Per qualunque decisione di ordine/atterraggio (migrations su tutte) vale l'**ordine-fase**, mai l'ordine-slice.

Misura del codebase (`/gsd-map-codebase` non si usa più; il numero si rimisura con
`git ls-files '*.go'`): v1.1.0 *Production Launch — Multi-Tenant* shippata 2026-10-08,
**~212k LOC non-test su 99 package**, escluse le ~14k sqlc-generated, ~313k LOC di test
(misurato 2026-10-09; la cifra precedente di ~98k era ferma a un'altra era).

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
- **GIT PUSH DISCIPLINE.**  `git push` (or any remote-mutating command) at the end of a phase or a competed job and check all CI are green. Push from WSL: `LEFTHOOK_BIN=$HOME/go/bin/lefthook git -c core.hooksPath=.git/hooks push origin master`. From Git Bash the pre-push gates run `*.test.exe` and cannot run the web gates; from WSL without the recipe no gate runs at all. Every pre-push job has a glob, so a documentation-only push skips the hook (measured 2026-10-09).
- **MERGE ON EVERY TASK CLOSE.** After implementation, required reviews, plan tracking, and the task's atomic commits are complete, merge the task branch/worktree into `master` and run fresh post-merge verification before starting the next task. A task is not closed while it exists only in a feature branch or linked worktree. Preserve unrelated dirty files during the merge; do not push unless the phase/job push rule or the user explicitly requires it.
- **QUALITY SNAPSHOT IS MEASUREMENT-DRIVEN, NOT A DIFF GATE.** `docs/aura-quality-snapshot.md` remains the historical measurement ledger. Update a row only when its metric is actually measured, invalidated, superseded, or retired; never bump its date or prepend a metric-neutral re-attestation merely because a path glob matched. CI and pre-push do not gate on prose-ledger freshness (PRD amendment #177); the executable coverage, race, mutation, integration, and E2E gates remain authoritative.
- **NO COMMENTS UNLESS WHY IS NON-OBVIOUS.** Identifier names already explain what. Comments only for hidden constraints, workarounds, or surprising behavior.
- **NO TEST BASY SITTING.** Tests must follow PRD §Test discipline rigorosa: realistic fixtures, goleak, race detector, property-based dove indicato, build tags integration, coverage threshold, mutation testing spot-check. Cita la tabella esempi per slice.
- **NO SKIP-AS-GREEN IN CI.** Integration/smoke tests must actually run in the pipeline — a `t.Skip` that fires under `$CI` is a falsely-green job exercising nothing. (1) CI jobs export the exact env the tests read (composed DSNs `AURA_DB_URL`/`AURA_DB_MIGRATE_URL`, not just the `POSTGRES_*` primitives `config.Load` composes for the CLI). (2) Skip-helpers (`envOrSkip` and inline `t.Skip`) call `t.Fatal` when a required var is unset and `$CI` is set; locally they still skip. A sub-second "integration" runtime is a skip tell — verify execution, not just PASS.
- **COVERAGE FLOOR 85%.** No phase/slice closes below 85% measured coverage across the full tag matrix (unit + integration + smoke). This overrides the PRD's ≥75% unit / ≥60% integration. A bare unit-only number under 85% is not an acceptable closing metric — report the combined figure.
- **COVERAGE EVIDENCE IS TIERED AND PACKAGE-LOCAL.** `scripts/coverage_gate.sh` defaults to `db_integration`, runs in both `ci.yml` and `skills.yml`, and collects native covdata with `-coverpkg=./internal/...` from tests in `internal` plus `cmd/aura`. The aggregate owned-source floor remains exactly 85%. `scripts/coverage_package_policy.json` additionally classifies every filtered package: packages already compliant keep an exact 85% floor; named low packages keep an exact covered/total non-regression baseline and visible 85% target; inventory or denominator drift fails closed. Two packages delegate, each to a separately release-blocking report measuring the same 85% floor on a denominator this tier can execute: `internal/sandbox/usersandbox` to the native `docker_integration` coverage report, and `internal/arcadedb` to the live `arcadedb_integration` profile, whose `arcadedb_package_coverage` scenario is an Agent Memory hard gate re-checked by release readiness (amendment #203). Never concatenate or average these denominators. **When you add daemon/container-gated runtime code you MUST also write daemon-free unit tests for its pure logic** — spec/tar builders, path-traversal + symlink guards, nil/disabled early-return paths, structural-capability "not supported" errors. **Verify locally BEFORE pushing with `bash scripts/coverage_docker.sh`** — it provisions and drops only the disposable `aura_cov` database and refuses a local `db_integration` run against the live `aura` database unless the explicit danger override is set.
- **DEFINITION OF DONE** Phase/Job are complete when is fully validate E2E at score >9.8 on real scenario.
- **BASELINE OR DROP.** Un componente che impara (seed bank, recall, triage, briefing, stile) deve battere il suo baseline stupido su dati fuori campione, o si scarta. Nessuna eccezione. Regola misurata su Market_MCP (principio guida 3) e già praticata dal gate del turn recall: qui diventa obbligatoria per ogni pezzo appreso.
- **SPIKE VERDICTS GATE SPECS.** Una spec usa liberamente un finding `VALIDATED`, un `PARTIAL` solo con i suoi vincoli dichiarati, e mai un `INVALIDATED` finché uno spike successivo non lo supera. Il vocabolario è quello di `Skill("spike-findings-Aura")`; prima del 2026-10-09 il gate non era scritto.
- **VALIDATIONS ARE A DATED LEDGER.** Ogni run di accettazione sul lab VM lascia un file in `docs/superpowers/verification/`, nominato per data e scenario, che dice cosa ha misurato e cosa non dimostra. Una spec chiusa senza il suo file non è chiusa (pattern di sacchi_agent `docs/superpowers/validazioni/`).
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

**WSL is the full primary dev environment** — it runs everything: `gcc` 15 + GNU `make` (build-essential), `CGO_ENABLED=1` so native `go test -race` works, and the Go quality toolchain in `~/go/bin` (`make tools` or `go install`): `golangci-lint` (v2.14.0, CI-pinned), `staticcheck`, `govulncheck`, `dupl`, `gotestsum`, `deadcode`, `goimports`, `go-mutesting`. The login shell does **not** put `~/go/bin` or `~/.local/bin` on PATH — prepend both (`export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"`) or invoke by full path. The whole `make quality-full` gate (incl. db integration coverage) passes natively in WSL.

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
- `make coverage` → `scripts/coverage_gate.sh` — **owned-surface aggregate floor ≥85% plus package-local policy** (`internal/*` minus generated `sqlc`, `internal/agent/agenttest`, `internal/dbtest`, and the pre-rewrite `internal/llm/client.go`; `cmd/aura` tests contribute execution but its glue stays outside the denominator). Go native covdata supplies truthful cross-package attribution. `scripts/coverage_package_policy.json` is an explicit fail-closed inventory: target packages must remain ≥85%; below-target packages cannot regress below their pinned exact ratio or change denominator silently; `usersandbox` is checked by the independent Docker authority. **Current disposable full-matrix measurement: 27,671/31,839 = 86.9091% (2026-08-26)**; 58/71 packages are at target, 12 have named non-regression debt, and one is delegated. `internal/mcpregistry` is 71/75 = 94.6667%. The JSON release artifact carries all package results and release readiness recomputes their contract rather than trusting a self-attested boolean.
- `make vuln` → `govulncheck ./...` — supply-chain CVE scan (CI `vulncheck` job).
- `dupl` is enabled in `.golangci.yml` (threshold 100, `_test.go` excluded — table tests are intentionally repetitive).
- Mutation spot-check ≥70% on each phase's critical file(s); documented in the phase `VALIDATION.md` Manual-Only table (recent: 37F-03 SC3 core 87.5% killed; the v0.0.0 examples db.go 82.8% + budget.go/budget_dedup.go 89.4% lived under `.planning/milestones/v0.0.0-phases/`, removed in `9f9f2d974` and readable only from git history).

**No-skip-as-green** still governs: the coverage gate runs the tagged tiers, which `t.Fatal` under `$CI` when their env is unset — a skipped tier fails the gate, never passes it. Phase validation (deep) executes every tier live, never compile-checks — bring the stack up and run the real integration + smoke + mutation, do not trust a compile-check.

## Commit discipline

- **One slice = one commit** (o N per sub-slice con atomicity nota nel PRD).
- Atomic. Commit message: imperative subject + body explaining *why*.
- Co-Authored-By trailer per project convention.
- PRD-amendment commit prima del code commit se la slice ha rivelato un buco architettonico (vedi PRD §Q&A revision protocol).

## Persistence

- **Postgres** primary (port `5432`): schema `aura.*`, sqlc-generated client, golang-migrate. Il conteggio/floor corrente si legge dalla directory, mai da una cifra hardcoded in questo documento.
  - **Migration numbering — regola imperativa.** Il numero si assegna **all'atterraggio = prossimo intero libero quando la PHASE esegue** (ordine-fase, NON ordine-slice). **Prima di creare una migration esegui `ls internal/db/migrations/ | tail -1` e usa il successivo: il numero non si deduce, non si calcola dalla slice, non si copia da questo file** — questo file invecchia, la directory no. I numeri hardcodati nelle sezioni slice del PRD sono **indicativi** e superseduti da questa regola. Fonte di verità: prd.md §Persistence "Migration numbering — fonte di verità".
- **ArcadeDB** (`compose.yaml`): memoria a lungo termine, **un database per identità** creato al provisioning con credenziale derivata per tenant (HMAC su `AURA_ARCADEDB_TENANT_SECRET`) — l'isolamento lo impone il server, non una WHERE che ci si può dimenticare. Fatti bitemporali (`valid_from`/`valid_to` + supersede), indice vettoriale nativo e full-text nello stesso motore. L'interfaccia LLM è `cmd/arcadedb-mcp`. Richiede ≥ 26.10.1: sotto 26.4.2 un database creato a runtime resta senza autorizzazione (CVE-2026-44221), e sotto 26.10.1 un filtro di `vector.neighbors` che non trova niente vale come nessun filtro (#8959). `TenantClients.For` lo verifica una volta per resolver con `VerifySecureVersion`, prima di consegnare il primo client tenant.
- **Filesystem** per artifact: `$AURA_RUN_DIR/` (sidecar tool results + spillover content) + `$AURA_SKILLS_DIR/` (skill condivise) + `$AURA_SKILLS_IDENTITY_DIR/<id>/` (skill di ogni identità, #214). `~/.aura/agents/<id>/` (Agent.md), `~/.aura/mcp/<id>/` e `~/.aura/pyscripts/<id>/` sono ritirati (amendment #206/#207, `cmd/aura/serve_provisioning.go`): il registry MCP è una tabella Postgres e i pyscripts non avevano lettori.
- **Backup**: Postgres `pg_dump` (vedi PRD §Backup strategy).

## Env vars

Tutti gli env vars usano convenzione `AURA_<DOMAIN>_<UNIT>` (es. `AURA_SWARM_MAX_DEPTH`). Eccezioni: env per librerie/sidecar di terze parti (`TELEGRAM_BOT_TOKEN`, `OPENROUTER_API_KEY`, `MULTIMODAL_*`, `LLAMA_*`, `LMCACHE_*`) mantengono naming canonico upstream.

Indice completo: vedi PRD §Caps & Limits → Indice completo env vars (~60 voci catalogate).
