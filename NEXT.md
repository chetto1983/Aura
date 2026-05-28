# NEXT — riprendere domani

> Stato sessione 2026-05-28 (web). Da continuare su PC.

## Cosa è stato fatto in questa sessione

- **PRD completo**: `prd.md` 4400 righe, 14 slice (0.5/0.7/0.9/1/1.5/1.7/1.8/2a/2b/3/4/5/6/7+7e/8/9a/9b/9c/10/11a-e/13a-b)
- **CLAUDE.md** aggiornato: PRD-first principle, 3-gate Slice Q&A discipline, GSD workflow mapping
- **Codebase map**: `.planning/codebase/` 7 doc (2721 LOC) mappa skeleton attuale vs target PRD
- **GSD tooling** installato: `.claude/` con 67 commands + 33 agents + 12 hooks attivi
- **46 skills** installate in `.claude/skills/` (3.5 MB):
  - 14 Trail of Bits (security + audit + Q&A)
  - 16 Go (samber/cc-skills-golang)
  - 13 Neo4j (neo4j-contrib/neo4j-skills)
  - 3 meta (find-skills, mcp-builder, skill-creator)

## Cosa NON è stato fatto

- **Zero codice scritto**. Per design: PRD-first principle (`senza PRD completo non si scrive una riga`). Skeleton attuale = 633 LOC pre-rewrite invariato.
- **`.planning/PROJECT.md` non esiste ancora**: il `/gsd-new-project` è stato avviato ma interrotto per fare `/gsd-map-codebase` prima.

## Riprendere domani — 3 step

### Step 1 — Verifica setup

```bash
cd Aura
git checkout tabula-rasa
git pull origin tabula-rasa
ls .planning/codebase/   # 7 file: STACK, INTEGRATIONS, ARCHITECTURE, STRUCTURE, CONVENTIONS, TESTING, CONCERNS
ls .claude/skills/        # 46 skill (3.5 MB)
```

### Step 2 — Apri Claude Code + fresh context

```
/clear
```

### Step 3 — Riprendi GSD workflow

**Opzione A (consigliata)** — Completa init project (era stato interrotto a Step 2):

```
/gsd-new-project
```

Il workflow detecterà `has_codebase_map=true` e salterà direttamente a Step 3 Deep Questioning, sfruttando i 2721 LOC di context map già scritti. Output: `.planning/PROJECT.md` + `.planning/config.json` + `.planning/REQUIREMENTS.md` + `.planning/ROADMAP.md` + `.planning/STATE.md`.

**Opzione B (skip project init)** — Vai direttamente alla prima slice:

```
/gsd-discuss-phase 0.5      # Postgres infra (Gate 1 DoR)
```

Adatta meglio se PROJECT.md è ridondante con il prd.md già completo. Però rompe il pattern GSD standard.

## Decisione open per domani

Quale Slice iniziare per prima? Sequencing rationale del PRD:

```
0.5 Postgres → 0.7 Neo4j → 0.9 Agent runtime → 1 LLM client → 1.5 ask_user
  → 1.7 Identity → 1.8 Conversations → 2a Sandbox → 3 Swarm → 4 KV cache
  → 5 Web tools → 6 Scheduler → 7 Skills (a/b/c/d/e) → 2b Sandbox session
  → 8 AG-UI → 9 Telegram (a/b/c) → 10 Onboarding → 11 Memory (a/b/c/d/e)
  → 13 Local LLM fallback
```

Naturale partire da **0.5 Postgres**. Comando: `/gsd-discuss-phase 0.5`.

## Open questions pre-merge da chiudere (28 totali)

Lista parziale flagged nel PRD:
- **Slice 9c CRITICA**: Gemma 4 variant E2B vs E4B vs 26B (pre-merge benchmark)
- **Slice 13b CRITICA**: vLLM CPU vs GPU (potrebbe attivare path 13-bis llama.cpp fallback)
- **Slice 11b**: chunk size 512 vs 1024 tokens benchmark
- Altre 25 con default proposto OK fino a benchmark slice-specific

Tutte sono flagged nel PRD per slice. Si chiudono durante Gate 1 DoR di ognuna.

## Stato git (al momento del pause)

```
Branch: tabula-rasa
Last commit: 70c8cb2 chore: +29 skills (16 Go samber + 13 Neo4j) per Aura impl stack
Working tree: clean
Remote: synced
Total commits sessione: 33+
```

---

*Bunon coding domani. PRD-first, Slice Q&A 3-gate, niente asilo nido.*
