# Phase 13: Channels + Telegram + Multimodal - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-07
**Phase:** 13-channels-telegram-multimodal
**Areas discussed:** PRD amendments + scope spike, Superficie artifact delivery, Refactor CLI-as-channel, Scope multimodale 9c

---

## PRD amendments + scope spike

| Option | Description | Selected |
|--------|-------------|----------|
| Primo plan della fase | 13-01 = doc amendment + design spec, stile 16-01 | |
| Commit amendment ora, pre-planning | Amendment al prd.md committato prima di /gsd-plan-phase 13 | ✓ |
| You decide | — | |

**User's choice:** Commit amendment ora, pre-planning.

| Option | Description | Selected |
|--------|-------------|----------|
| Dentro 9b | Tabelle = renderer, artifact = send path; 9b ~1150 LOC | ✓ |
| Sub-slice 9b-bis dedicata | "Rich output" come quarto sub-slice atomico | |
| You decide | — | |

**User's choice:** Dentro 9b.

| Option | Description | Selected |
|--------|-------------|----------|
| Solo Telegram token | Scope PRD 9a esatto, niente API-key field | |
| Seam estensibile ora | Form multi-campo generico per Phase 17 | |
| (freeform) | "solo api backend al momento la prossima milestone farà il frontend" | ✓ |

**User's choice:** Wizard = SOLO API backend in Phase 13; frontend nella prossima milestone.
**Notes:** Flusso operatore confermato esplicitamente: AURA_SETUP_TOKEN su stdout; bot token via curl POST /setup/token o env TELEGRAM_BOT_TOKEN; onboarding via QR ASCII in terminale (qrterminal) + deep-link; qr_svg JSON resta per il frontend futuro. ROADMAP success criterion 1 da emendare.

---

## Superficie artifact delivery

| Option | Description | Selected |
|--------|-------------|----------|
| Tool send_file esplicito | L'agente decide quando consegnare | ✓ |
| Auto-detect del renderer | Scan dei tool result / workspace a fine run | |
| Entrambi | Esplicito + fallback orfani | |

**User's choice:** Tool send_file esplicito.

| Option | Description | Selected |
|--------|-------------|----------|
| Channel-agnostic via evento | Evento artifact generico nello stream AG-UI | ✓ |
| Telegram-only per ora | Consegna solo su Telegram, altrove path testuale | |
| You decide | — | |

**User's choice:** Channel-agnostic via evento.

| Option | Description | Selected |
|--------|-------------|----------|
| Qualsiasi path leggibile | Coerente con postura #50 full host terminal | ✓ |
| Scoped ai root artifact | Solo $AURA_RUN_DIR + workspace | |
| You decide | — | |

**User's choice:** Qualsiasi path leggibile.

---

## Refactor CLI-as-channel

| Option | Description | Selected |
|--------|-------------|----------|
| CLI resta dov'è | Channel framework solo per daemon channel; CLI = debug-only | ✓ |
| Refactor pieno da PRD | chat.go/chat_render.go/chat_repl.go → internal/channels/cli/ | |
| Adapter sottile | Wrapper Channel senza muovere file | |

**User's choice:** CLI resta dov'è (deviazione PRD registrata nell'amendment).

| Option | Description | Selected |
|--------|-------------|----------|
| Interface + registry da PRD | ~170 LOC, deliverable UX-02 | ✓ |
| Solo interface, registry dopo | Registry estratta col secondo canale | |
| You decide | — | |

**User's choice:** Interface + registry da PRD.

---

## Scope multimodale 9c

| Option | Description | Selected |
|--------|-------------|----------|
| Light-check su E4B default | Validazione snella + numeri a snapshot | |
| Benchmark completo da PRD | Matrice E2B/E4B/26B piena | |
| Accetta E4B, zero benchmark | Solo success criteria come gate | |
| (freeform) | "qui serve un gsd spike molto accurato con vLLM" | ✓ |

**User's choice:** Spike GSD dedicato e accurato con vLLM.
**Notes:** Follow-up freeform: "valutiamo altri modelli multimodali online con gpu da 4Gb del 2026" → lo spike è una survey di mercato 2026 (modelli multimodali STT+vision ≤4 GB VRAM) serviti con vLLM, baseline = llama.cpp + Gemma 4 E4B. "guarda questo PC ha 32Gb di ram e gpu da 4Gb" → l'hardware di misura è questo stesso PC (numeri di produzione reali).

| Option | Description | Selected |
|--------|-------------|----------|
| Prima del plan, 9c-blocking | Spike prima di /gsd-plan-phase 13 | ✓ |
| In parallelo a 9a/9b | Spike sblocca solo 9c | |
| 9c esce dalla fase | Multimodale → fase dedicata | |

**User's choice:** Prima del plan, 9c-blocking.

---

## Claude's Discretion

- Forma esatta dell'evento artifact AG-UI (custom event vs estensione tool_call_result).
- Wiring per-run del fanout (Subscribe-before-Run).
- Cap 50 MB su send_file, caption handling, flag Deferred.
- Dettagli registry lifecycle/error-aggregation in serve.go.

## Deferred Ideas

- Setup wizard frontend (page.html, browser flow) — prossima milestone.
- Campo OPENROUTER_API_KEY nel wizard (Phase 17 D-22) — con il frontend.
- vLLM come serving engine unificato (Slice 13 / DGX Spark) — fuori da Phase 13.
