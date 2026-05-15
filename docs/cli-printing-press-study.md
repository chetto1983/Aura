# cli-printing-press — Studio per Aura

Data: 2026-05-15
Source: https://github.com/mvanhorn/cli-printing-press (clone in `D:/tmp/cli-printing-press`)
Stack: Go, 67k LOC prod, license Apache-2.0, autore solo (Hiten Shah + mvanhorn)
Versione: v4.x

## TL;DR — cosa è davvero

**Un generatore di CLI agent-native + MCP server da una OpenAPI spec (o sniff browser, o HAR, o URL nudo).**

Input: nome API o spec → Output: `<api>-pp-cli` (Go binary) + `<api>-pp-mcp` (MCP server stdio) + research docs + verifica + scorecard.

L'eseguibile è doppio uso:
1. **Generator binario standalone**: `printing-press` con sub-comandi `pipeline`, `run`, `dogfood`, `verify`, `scorecard`, `publish`, `lock promote`, `mcp-sync`, `tools-audit`, ecc.
2. **Skill Claude Code**: `/printing-press <api>` che orchestra il binario in 5 fasi con feedback agent-driven sui gap

Il "Library" repo (https://github.com/mvanhorn/printing-press-library) è solo il catalog di output: 87 CLI già stampati con questo generatore.

## Architettura — 3 layer

```
┌──────────────────────────────────────────────────────────────┐
│  LAYER 3 — Skill (Claude Code)                                │
│  skills/printing-press/SKILL.md drives the binary             │
│  context: fork + references/*.md pattern for token economy    │
├──────────────────────────────────────────────────────────────┤
│  LAYER 2 — Pipeline (managed, resumable, 9 phases)            │
│  pipeline/state.json + per-phase plan files                   │
│  preflight→research→scaffold→enrich→regenerate→review→        │
│  agent-readiness→comparative→ship                             │
├──────────────────────────────────────────────────────────────┤
│  LAYER 1 — Generator binario (deterministic)                  │
│  82 Go templates → produce CLI struttura completa             │
│  internal/generator/, internal/spec/, internal/mcpdesc/,      │
│  internal/openapi/, internal/browsersniff/, internal/llm/     │
└──────────────────────────────────────────────────────────────┘
```

LOC per package (top 10):

| Package | LOC | Ruolo |
|---|---|---|
| internal/pipeline | 25,572 | State machine 9-fase, fast-run, seeds, transitions |
| internal/cli | 9,020 | Cobra root + 30+ sub-comandi |
| internal/generator | 7,324 | Template renderer + 82 .tmpl files |
| internal/openapi | 5,564 | Parser/normalizer OpenAPI 3.0+ |
| internal/browsersniff | 4,762 | Cattura traffico browser (Playwright/CDP) per API senza spec |
| internal/spec | 2,999 | Modello dati interno della spec |
| internal/crowdsniff | 2,699 | Scopre community CLI esistenti (npm + GitHub) |
| internal/profiler | 1,701 | Quality profile (auth, pagination, rate-limit signals) |
| internal/artifacts | 1,139 | Runstate + manuscripts on-disk |
| internal/patch | 968 | Re-render template subset su CLI già publicato |

## I 5 pattern davvero replicabili in Aura

### 1. `mcpdesc.Compose()` — descrizione MCP strutturata da endpoint

**Cosa fa**: prende un endpoint OpenAPI (verbo + parametri + response shape + auth) e produce una **descrizione MCP standardizzata** del formato:

```
<verb-led action>. Required: <param1> (<type>), <param2> (<type>). Optional: <param3> (<type>, default: X) [plus N more]. Returns: <shape>. (requires auth)
```

Detect override strutturali (`Required:` / `Optional:` colon-terminated) per saltare il composer quando l'autore ha già scritto a mano. Auth annotation single-sourced via `naming.MCPDescription`.

**Perché conta per Aura**: il registry tool di Aura ha oggi `Description` free-form scritti a mano (vedi `internal/agent/tools/registry/*.go`). Inconsistenti, alcuni verbosi, alcuni telegrafici. L'agente fa errori di tool-choice quando le description sono fuzzy.

**Cosa portare**: copia `internal/mcpdesc/compose.go` (~250 LOC, dipendenze minime). Adatta il `spec.Endpoint` al tuo `tools.ToolDef` struct. Risultato: ogni tool registry-side ottiene una description del **formato fissato** (verb + Required: + Optional: + Returns:). L'LLM impara la grammatica al primo turno e poi fa pattern-match.

**Effort**: 1 slice MVP (~150 LOC + applicazione retroattiva ai 30 tool esistenti).

---

### 2. `references/` pattern per gli skill

**Cosa fa**: SKILL.md resta sotto ~500 righe con solo:
- Cardinal rules
- Decision matrices
- Phase structure
- "Read [references/foo.md](...) when X"

Tutto il resto va in `references/*.md` caricati on-demand quando l'agente raggiunge il gate.

Claim del repo: **30-40% riduzione baseline context** vs SKILL.md monolitica.

**Perché conta per Aura**: i tuoi skill in `internal/skills/` già usano progressive disclosure tramite `read_skill` tool, ma OGNI SKILL.md viene inviato nel system prompt (manifesto). Se gli skill bodies sono pesanti, il manifest cresce. Adottando references/ all'interno del singolo SKILL.md sposti la metà del contenuto fuori dal payload iniziale.

**Cosa portare**: convenzione + check di lint. Tipo: skill linter che fa fail se SKILL.md > 500 righe e `references/` è assente. Implementazione concreta: aggiungi a `internal/skills/loader.go` un warning quando manifest entry supera N caratteri.

**Effort**: 1 slice (~100 LOC + retro-fit di 2-3 skill grossi).

---

### 3. Catalog YAML per servizi esterni

**Cosa fa**: ogni API supportata ha un file `catalog/<api>.yaml`:

```yaml
name: stripe
display_name: Stripe
category: payments
spec_url: https://raw.githubusercontent.com/stripe/openapi/master/openapi/spec3.json
spec_format: json
auth_key_url: https://dashboard.stripe.com/apikeys
auth_instructions: "Use a test mode key (sk_test_*)..."
notes: "Very large spec (~500 endpoints). Generator truncates significantly."
known_alternatives:
  - name: stripe-cli
    url: https://github.com/stripe/stripe-cli
sandbox_endpoint: https://api.stripe.com
```

**Perché conta per Aura**: oggi `mcp.json` è un dizionario `{name: command}` piatto. Quando aggiungi un nuovo MCP server non c'è posto per: dove prendere la key, qual è il sandbox, quali sono le alternative, cosa l'agente deve sapere prima di chiamarlo. Le user instructions sono fuori-banda nella docs umana, non in un manifest leggibile dall'agente.

**Cosa portare**: estendi `mcp.json` (o aggiungi `mcp-catalog.yaml`) con campi tipo `auth_instructions`, `sandbox_endpoint`, `cost_warning`. Aura lo legge al boot, lo espone nel system prompt come "external service catalog" — l'agente sa quando un tool ha cost o richiede setup utente prima di usarlo.

**Effort**: 1 slice (schema + loader + retro-fit dei MCP esistenti).

---

### 4. Pipeline state machine con plan files per-fase

**Cosa fa**: ogni workflow lungo (la generazione qui) è uno stato persistente in `pipeline/state.json` con:

```go
type Phase struct {
    Status     string  // pending|planned|executing|completed|failed
    PlanStatus string  // seed|expanded|completed
}
```

Ogni fase ha un plan file markdown:
- **seed** = template iniziale (templated da `seeds.go`)
- **expanded** = l'operatore/agente ha espanso il piano
- **completed** = fase chiusa, gates passati

Phase numbering con gaps (0, 10, 20, ...) per inserzioni future senza renaming.

**Perché conta per Aura**: pattern molto vicino a quello che usi già con `.planning/HANDOFF.json` + `progress.txt` + GSD ticket flow. Ma:
- `Status`/`PlanStatus` ortogonali separano "esecuzione" da "documentazione"
- Phase numbering con gap è meglio del rinominare US-A13b.3
- Seed templates = ogni fase nasce con un piano coerente, non da zero

**Cosa portare**: già lo fai in spirito. Vale la pena leggere `internal/pipeline/state.go` e `seeds.go` per i 2-3 dettagli (gap numbering, defensive nil-init delle mappe) e applicarli al ciclo GSD/Ralph.

**Effort**: 0 — è una lezione di design, non da copiare. 20 min di lettura.

---

### 5. Scorecard formalizzata con dimensioni numeriche

**Cosa fa**: `comparative-analysis.md` punteggia il CLI generato su 6 dimensioni vs gli alternativi (100pt totali):

| Dimensione | Pts | Come misurato |
|---|---|---|
| Breadth | 20 | Command count ratio vs best alternative |
| Install friction | 20 | Single binary=20, clone+build=15, runtime=10 |
| Auth UX | 15 | env+config=15, env only=10, manual=5 |
| Output formats | 15 | 5 per format (JSON, table, plain) |
| Agent friendliness | 15 | `--json`(5) + `--dry-run`(5) + non-interactive(5) |
| Freshness | 15 | <30d=15, <90d=10, <1y=5, older=0 |

Plus tier-1 MCP-shape dimensions: `mcp_remote_transport`, `mcp_tool_design`, `mcp_surface_strategy`.

**Perché conta per Aura**: oggi "Aura è meglio di X?" è soggettivo. Una scorecard quantitativa applicata su (a) tuoi tool vs MCP esterni, (b) wiki vs alternativi (Obsidian, Logseq), (c) intero stack vs Mem.ai/Reflect/Notion, mette numeri al posto della sensazione. E ti dà un trigger oggettivo per "questo va riscritto".

**Cosa portare**: prendi 4-5 dimensioni rilevanti per il tuo dominio (Memory recall accuracy, Source ingest latency, Tool call efficiency, Wiki write determinism, ecc.) e fai una `docs/aura-scorecard.md` aggiornato per ogni milestone.

**Effort**: 1 slice (definire dimensioni + script che misura).

## Patterns NON portabili (esplicitamente)

1. **L'intero generator template tree (82 .tmpl)** — solo utile se vuoi STAMPARE CLI. Aura non è in quel business.
2. **`browsersniff` (4762 LOC) + `crowdsniff` (2699 LOC)** — Playwright per sniffare API non documentate. Out of scope per second-brain.
3. **Compound-engineering skill integration** — l'`agent-readiness` phase delega a una skill esterna `compound-engineering:cli-agent-readiness-reviewer`. Vendor-locked.
4. **Publishing flow** — npm + il public library repo. Aura non distribuisce nulla.
5. **OpenAPI-first mindset** — Aura non parte da spec, parte da fonti destrutturate (PDF, conversazioni, web).

## Pattern del SKILL.md di /printing-press — copy questo

Leggi `D:/tmp/cli-printing-press/skills/printing-press/SKILL.md` (1900+ righe). I primi 120 sono il template di skill agent-native che vale la pena studiare:

1. **frontmatter ricco**: `name`, `description`, `version`, `min-binary-version`, `allowed-tools` (lista esplicita)
2. **una "Modes" section** che spiega quali variant esistono (Default, Codex Mode, Polish Mode) — l'agente sa se può delegare
3. **Cardinal rules** numerate, non-negotiable
4. **Secret/PII protection** section esplicita per cosa NON deve mai finire in output
5. **`<!-- PRESS_SETUP_CONTRACT_START -->` markers** per sezioni machine-edited
6. **Preflight section** prima di qualsiasi user-facing prompt

Per Aura: i suoi skill correnti (`internal/skills/`) seguono il formato Anthropic ma sono meno strutturati. Adottare il pattern Modes/Cardinal rules/Preflight farebbe gli skill di Aura più resistenti al context-rot.

## Verdetto operativo (per Aura)

**Adottare come sistema?** No. È un generatore, non un sistema di memoria.

**Cherry-pick pattern (in ordine di ROI)**:

1. **`mcpdesc.Compose()` per i tool description** — alto impatto, basso effort, attacca direttamente il context-rot della tool-choice
2. **`references/` pattern + lint per gli skill** — medio impatto, basso effort
3. **Catalog YAML per MCP esterni** — medio impatto, medio effort, ma high-leverage se aggiungi tanti MCP futuri
4. **Scorecard quantitativa** — medio impatto, basso effort, dà metrica per il debt
5. **Studiare lo state-machine pattern** — 0 effort, sblocca disciplina per il GSD ciclo

**Cherry-pick di SINGOLI MCP server già stampati**: dal library repo (https://github.com/mvanhorn/printing-press-library) prendere arxiv, archive-is, hackernews, cal-com come singoli MCP da aggiungere a `mcp.json`. NO npm install starter-pack — manuale.

## Footnote sui contro

- **Single-author + vendor coupling**: la pipeline dipende da `compound-engineering` skill e ha bias forti dall'autore. Non è una community spec.
- **OpenAPI-centric**: tutto parte da una spec o da un sniff. APIs senza spec e senza UI scrappabile non sono nel target.
- **Tool surface esposto**: 82 templates Go + 30+ sub-comandi del binario. Manutenibilità futura = dipende dal singolo autore.
- **Skill format = Claude Code, non Anthropic puro**: `allowed-tools`, `min-binary-version`, `version` non sono nel formato Anthropic SKILL.md di base. Aura's `internal/skills/` legge SKILL.md Anthropic — non scambiabile senza shim.

---

## File interessanti da leggere (in ordine)

1. `README.md` (37k chars) — top of repo
2. `docs/PIPELINE.md` (276 righe) — contratto delle 9 fasi
3. `docs/PATTERNS.md` (43 righe!) — "Deterministic Inventory + Agent-Marked Ledger" pattern, sintesi geniale
4. `docs/SKILLS.md` (64 righe) — pattern frontmatter + references/
5. `internal/mcpdesc/compose.go` — il file che porterei in Aura per primo
6. `internal/pipeline/state.go` + `seeds.go` — state machine + plan templating
7. `skills/printing-press/SKILL.md` — il SKILL canonico
8. `catalog/stripe.yaml` (e simili) — formato catalog YAML
