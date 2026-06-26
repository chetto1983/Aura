# Aura — Documento di decisione (v2): Hardware, Modello, Architettura, Deployment

> Brief tecnico completo. Obiettivo: portare Aura (assistant Telegram agentico,
> "secondo cervello") a girare **interamente in locale** sul PC nuovo (RTX 3060
> 12GB), con il cloud solo a training time, e definire cosa consegnare ai clienti.
>
> **Stato attuale (verificato dal codice):** Aura gira oggi su **DeepSeek V4 Flash
> via OpenRouter** (cloud). Il locale è la migrazione in corso.
>
> **Decisione modello:** **Qwen3 8B** come candidato primario per il locale.
>
> Metodologia di fondo: **misura prima di comprare, testa prima di addestrare.**
> Zero-shot prima della distillazione; dati reali prima dell'hardware.

---

## 1. Contesto e obiettivo

- **Aura**: bot Telegram in Go, agentico, con RAG ibrido, tool calling, memoria a
  grafo Neo4j, MCP. Provider LLM **OpenAI-compatibile**; runtime **llama.cpp /
  llama-server come sidecar** → cambiare modello = config, non codice.
- **Ruolo dell'LLM**: **orchestratore** — routing tool + sintesi. NON visione/
  audio/parsing/embedding/estrazione (tutti su specialisti, vedi §3).
- **Lingua**: tool e system prompt in **inglese**; solo la **risposta finale** in
  **italiano** (pilotata da system prompt). → il "problema italiano" è marginale.
- **Due macchine, due ruoli**:
  - **Box dev/training** → PC nuovo con RTX 3060 12GB (sostituisce il portatile
    con Quadro P2000 2GB, che imponeva modelli minuscoli).
  - **Appliance cliente** → da decidere sui dati (vedi §9).

---

## 2. Decisione modello

### Primario: Qwen3 8B

`unsloth/Qwen3-8B-GGUF` — **Apache 2.0**, dense 8,2B, GQA.
- **Thinking nativo a interruttore** (`/think` `/no_think`, `enable_thinking`)
  per-turno → calza sul reasoning adattivo di Aura (vedi §5). **Motivo principale
  della scelta.**
- **Agentic forte** (leader open-source su task agentici complessi dichiarato da
  Qwen) + ecosistema Qwen-Agent/MCP.
- **100+ lingue** (italiano coperto).
- **Q4 ~5GB** → margine per i co-residenti; **QLoRA sta nel 3060** (training locale).
- Stessa famiglia di Hermes 4 14B (coerenza se si sale di taglia).
- Contesto: 32K nativi, 128K via YaRN (per Aura 32K bastano, il RAG tiene stretto).
- Caveat: reasoning-by-default → gestire i `<think>` (mai in history), `/no_think`
  sui turni semplici. Sampling thinking `temp 0.6 top_p 0.95 top_k 20`;
  non-thinking `temp 0.7 top_p 0.8`. Su llama-server: `--jinja`.

### Alternative (se l'eval lo richiede)

| Modello | Quando preferirlo | Limite |
|---|---|---|
| **Granite 4.1 8B** (Apache) | BFCL 68,3 + IFEval 87,1 (loop discipline), licenza | reasoning-tier meno liscio (no toggle per-turno doc.) |
| **Gemma 4 12B QAT** (Gemma ToU) | italiano nativo migliore, 256K SWA, MTP | None-tier rotto sul 12B, thinking mangia max_tokens, ~6,7GB |
| **Granite 4.0 H Micro 3B** (Apache) | 128K reali su 12GB (Mamba2), max velocità | sintesi 3B più semplice |
| **Hermes 4 14B** (Apache) | massima robustezza FC (JSON-repair) | no MTP, QLoRA solo su Modal, tira i 12GB |

### Scartati
FunctionGemma 270M (era vincolo P2000 2GB, ora superato), Gemma E2B/E4B (Tau2 24/42,
troppo deboli; il "212 tps su vLLM" era throughput, non capacità), Hermes 3 8B
(superato), Qwen3.6-35B-A3B IQ2_XXS (2-bit distrugge la precisione FC; serve 24GB a
Q4), modelli da 230B (capacità che il RAG rende inutile).

> **Decisione operativa:** `debug_llm` zero-shot di **Qwen3 8B** con la superficie
> tool reale (profilo `core`), misurando: routing tool (inglese), italiano di
> sintesi, **iterazioni-a-convergenza**, tok/s single-stream. Se regge → niente
> finetune. Granite/Gemma come fallback sui dati.

---

## 3. Architettura Aura (verificata dal codice)

Tesi centrale, confermata: **l'LLM è un orchestratore su evidenze già recuperate.
Tutto il lavoro pesante è su specialisti → un modello piccolo basta, il 230B è
inutile.**

### Specialisti come tool (sidecar)
- **GLM-OCR** (`aura-ocr-vl`, GPU) → immagini
- **Whisper** (`aura-stt`, CPU/GPU opz.) → ASR
- **Kokoro** (`aura-tts`, CPU) → TTS
- **markitdown** → parsing/chunking documenti
- **Granite-embedding 97M multilingue 384-dim** (`aura-llama-embed`, CPU) →
  embedding. **Triplo-uso**: RAG + classifier reasoning-tier + ranking tool_search.
  Load-bearing: se cade, perdi tre sottosistemi.

### Retrieval ibrido (Fase 30 — PIANIFICATA, non ancora nello stack)
- Documenti: markitdown → Neo4j (full-text + `:NEXT_CHUNK`) → embedding async.
- Pipeline a due stadi: seed vettore/BM25 → **rerank** → graph-expand.
- Reranker `aura-rerank` (Qwen3-Reranker-0.6B Q4, GPU-only, fail-soft) — **in
  `.planning/`, NON nel `compose.yaml` attuale.** Oggi il retrieval gira senza.
- Eval harness: nDCG@10 / Recall@5 / MRR. Invariante: `messages[0]` mai mutato.

### Memoria a grafo (pacchetto Neo4j Labs `agent-memory`, profilo `extended`)
- Schema **fisso POLE+O**. Estrazione via **spaCy/GLiNER/GLiREL** (specialisti), LLM
  solo uno stadio.
- L'LLM chiama **tool strutturati** (`memory_add_entity`, …), **non scrive Cypher**.
- **Profilo attuale = `extended` (16 tool)** (`--profile extended` nel compose).
  Core = 6 tool. **La memoria è hard-coded NON-deferibile** (`namespace != "memory"`):
  provata la deferral, rompe la doctrine (search/write-before-answer ogni turno).
- `MemorySettings.llm` separato dal chat-LLM (oggi punta anch'esso a V4 Flash).

### tool_search (discovery dei tool deferred)
- Non-deferred, sempre visibile. Path `select:Name` (esatto) e free-text.
- **Ranking embedding-primary** (Granite cosine); **BM25 solo tiebreak guardato**
  — perché su query italiane BM25 ha segnale zero (IT vs descrizione tool EN). Più
  re-embed incrementale al mount e **learned boost** (active learning).

### Agent loop (guardrail production-grade, verificati)
- **Cap step atomico condiviso = 25** (bound sull'intero albero swarm, TOCTOU-safe),
  **wallclock = 300s**, **dedup ring** (window 3).
- **Cycle detection multi-periodo**: period-1 (A-A-A), period-2 (A-B-A-B), period-N.
- **Progress veto fail-safe**: fingerprint = `sha256(name+args)` senza il result →
  i tool con output volatile falliscono SAFE, non fail-open.
- → il rischio loop del modello piccolo è **coperto per costruzione**.

### Superficie tool always-on
- **13 builtin non-deferred** + **16 memory (extended)** = **29 tool** in ogni
  richiesta. ≈ 8K token. **Leva: profilo `core`** (6 invece di 16) → 19 tool.
- Deferiti (7): web_search/fetch, document_search, shell_exec/poll, swarm_spawn,
  send_file. Memoria NON deferibile.
- Il prefisso è byte-stabile → **prefix-cacheable** (ordine canonico, no `map` Go).

---

## 4. Runtime e serving — llama.cpp / llama-server

- **Tutto via `llama-server` sidecar OpenAI-compatibile**, GGUF Q4. NON Ollama
  (manca controllo fine), NON vLLM (vedi sotto).
- **Perché non vLLM sul 3060**: BF16 non entra; FP8 richiede Ada/Hopper (3060 è
  Ampere); servono quant AWQ/GPTQ (Qwen3 ha GGUF); KV pre-allocata; batching
  inutile mono-utente. vLLM **ha più parametri** (structured outputs maturi,
  multi-LoRA) ma **non gira bene sul tuo hardware** → rilevante solo per l'ATOM
  multi-cliente. L'unico vantaggio utile (tool-JSON garantito) **lo hai in
  llama.cpp via grammatiche GBNF / `json_schema`**.
- **Flag base**: `-fa --cache-type-k q8_0 --cache-type-v q8_0 -c 16384 --jinja`.
- **Velocità**: 8B Q4 ~**25-40 tok/s** single-stream (memory-bandwidth-bound, 360
  GB/s). Per un bot mono-utente conta il single-stream, NON il throughput batchato.
- **Più VRAM ≠ più velocità**: i 12GB comprano capacità, non tok/s.

---

## 5. Reasoning adattivo (verificato) e porting in locale

- **Lo decide il codice, non il modello.** Un **classifier a embedding**
  (`adaptiveReasoningTier`, riusa il sidecar Granite) classifica il prompt in
  **None / Low / High** PRIMA del router LLM — deterministico, ~gratis. Fallback
  LLM-oracle + learner.
  - None: "ciao" → niente reasoning. Low: "che tempo fa" → tool semplice. High:
    "debugga questo segfault" → reasoning profondo.
- **Implicazione:** al modello non serve il meta-giudizio "quando pensare" — serve
  **obbedire al tier** e ragionare bene a High. Qwen3 (toggle per-turno) è il più
  liscio.
- **GAP da chiudere per il locale:** oggi `ApplyAdaptiveReasoning` proietta **solo
  su OpenRouter** (`reasoning.effort`, tarato su V4 Flash). Per Qwen3 locale serve
  un ramo che mappi:
  - **None** → `enable_thinking=false` / `/no_think` (toggle, NON budget=0).
  - **Low** → `thinking_budget_tokens: ~256-512` + `--reasoning-budget-message`.
  - **High** → `thinking_budget_tokens: ~2048-4096` o uncapped.
- **`reasoning_effort` su llama.cpp**: va in `chat_template_kwargs`, NON top-level
  (altrimenti ignorato in silenzio), ed è **semantico solo per gpt-oss** (low/med/
  high nativi). Per Qwen3/ibridi si usa il **budget di token** (meccanico).
- **Costo locale**: i `<think>` mangiano il TUO contesto e decode (su OpenRouter
  erano server-side). High a 4096 token = un quarto di 16K → dimensiona `num_ctx`.
- **Cicatrice da rispettare** (dal codice, "203-turn disaster"): **mai cappare
  `max_tokens` per tier** (troncava le tool-call). Il budget di thinking va tenuto
  **separato** dal max_tokens del visibile.

---

## 6. Budget VRAM (ricalcolato sui dati veri)

GPU-resident reali: solo **GLM-OCR** (obbligatorio) + il **chat-LLM** (quando
locale). STT GPU opzionale; TTS ed embedding su CPU. Reranker NON ancora nello stack.

| Componente | VRAM | Note |
|---|---|---|
| Chat-LLM (Qwen3 8B Q4) | ~5,5GB | residente |
| GLM-OCR (vision) | ~2-4GB | **bursty** (solo su immagini) |
| Embedding Granite 97M | ~0 (CPU) | — |
| Reranker (Fase 30, futuro) | ~0,5GB | per-query |
| KV cache (16-32K, Q8) | ~1-3GB | gli ~8K di tool ≈ <1GB, cacheable |

- **Qwen3 8B (5,5) + OCR-spike (4) + KV (2) ≈ 11,5GB** → ci sta nei 12.
- Un **14B (9) + OCR (4) = 13GB** → sfora sullo spike immagine. **L'8B è ciò che
  fa stare la co-residenza chat+OCR.**
- **La vera pressione VRAM = chat-LLM + GLM-OCR bursty**, non il reranker (assente)
  né l'embedding (CPU). Mitigazione: 8B, o OCR on-demand. **NON serve comprare VRAM.**

### Il "mi serve più VRAM" — diagnosi
Il blocco non è la scheda: è la somma di tre scelte software. (1) modello troppo
grande → **Qwen3 8B**; (2) superficie tool larga (29) → **profilo `core`** (19);
(3) training creduto locale del 14B → con l'8B il **QLoRA sta nel 3060**. Sistemate
le tre, i 12GB sono comodi.

---

## 7. Hardware (box dev) e decisioni "non comprare"

- **Scelto**: PC usato con **RTX 3060 12GB** (~500€), banda 360 GB/s. Per il carico
  di Aura conta la GPU, non la piattaforma.
- Contesto "crisi memorie 2026": 3090 usata ~1.100-1.340€, RAM 32GB ~130-150€.
- **NON comprare**: 3090 ora (fuori budget, non sblocca nulla che Modal+8B non
  facciano); scheda 32GB (~4.000€, prima vendere trazione); seconda 3060 (~250€,
  24GB splittati = capacità non velocità) **solo se** l'eval lo richiede.
- **Modal (30€/mese)**: per training pesante/14B se mai servisse. Ma l'**8B QLoRA
  gira in locale** → per ora non serve.

---

## 8. Training / distillation

### Pipeline esistente (`finetune/`) — già costruita bene
- Student attuale: **FunctionGemma 270M** (era vincolo P2000) → **da ri-puntare a
  Qwen3 8B**.
- Teacher: **V4 Flash** via OpenRouter. Harvest **trace reali** (Postgres, limit
  2000) **+ sintetico** (6 sample/seed, 8 turni). **`train_on_responses_only`**
  (loss-on-completion). LoRA r=32/α=64, lr=2e-4, 3 epoche.

### Re-point a Qwen3 8B (diff `pipeline.yaml`)
```yaml
base_model: unsloth/Qwen3-8B          # era functiongemma-270m-it
max_seq_len: 8192                      # Qwen3 nativo 32K
# Marker ChatML (NON Gemma) — CRITICO per train_on_responses_only:
instruction_part: "<|im_start|>user\n"
response_part: "<|im_start|>assistant\n"
# verificare i token esatti nel tokenizer Qwen3 prima del run
# rivalutare lora_r / lr / batch_size: scala 270M → 8B
```
> **Trappola #1**: lasciare i marker Gemma con Qwen3 → la loss maschera il pezzo
> sbagliato, training corrotto senza errori. Cambiali in ChatML.

### Vincolo e stack
- Teacher via API → **solo SFT black-box** (no GKD/logit KD: richiedono teacher
  caricato; V4 Flash 284B ingestibile).
- **Niente thinking nel dataset** (parti così): distilli routing+risposta dei *tuoi*
  tool, lasci il thinking nativo di Qwen3 al classifier. Strippa i `<think>` dagli
  esempi multi-turn.
- Judge filter = step più importante (scarta call malformate).
- Dataset italiano NON serve (tool/prompt in inglese).

> **Ma prima: zero-shot.** Con RAG + tool, Qwen3 8B potrebbe bastare senza
> finetune. Addestra solo se l'eval mostra routing dei tool concatenati o persona
> italiana deboli. Il QLoRA dell'8B **gira sul 3060**.

---

## 9. Deployment cliente

- **Appliance valutato: Gigabyte AI TOP ATOM** (GB10, 128GB unified, CUDA, ~273
  GB/s, ~€3.500-4.500). Pro: tutto locale/zero data exposure, CUDA, headroom per
  modelli grossi + 128K + fine-tuning on-prem per cliente. Riserva: **compri
  capacità, non velocità** (273 GB/s < 3060; un 14B gira ~come il 3060). È **Arm**
  → compila Aura `arm64`.
- **Regola di dimensionamento**: 8B + RAG + specialisti basta → **box GPU 16GB**
  (es. RTX 5060 Ti) a metà prezzo. Serve 30-70B / 128K / fine-tuning on-prem per
  cliente → **ATOM**.
- **L'appliance DEVE avere GPU** (GLM-OCR e il futuro reranker sono GPU-only).
- Traduzione test: qualità identica ovunque; velocità ATOM ≈ **0,75 × tok/s 3060**.
- **vLLM diventa sensato qui** (multi-cliente): structured outputs + multi-LoRA
  (un adapter fine-tunato per cliente da una base).

---

## 10. Modello di business / pricing

- **Prezza sul valore e sull'on-prem, non sul costo.** Non competere con ChatGPT
  (20-25€/mese): il tuo premium è **privacy / dati che non escono / GDPR / AI Act**.
- **Clienti target**: chi NON può mettere dati nel cloud — studi legali,
  commercialisti, medici, consulenti (dati sensibili → solo self-hosted).
- **Range (da validare col primo pilota)**:
  - Turnkey su ATOM: ~**7-12k€ one-time + canone** (manutenzione/aggiornamenti/
    fine-tuning).
  - Box GPU 16GB: ~**3-6k€**.
  - Ricorrente (chiave per un solo dev): qualche centinaio €/mese o contratto annuo.
- **Leva incentivi IT 2026**: iperammortamento 180%, Voucher MIMIT, IPCEI AI → il
  cliente finanzia l'investimento, tu prezzi più alto.
- **Strategia**: 1-3 pilota a prezzo ridotto per case study, verticale unica,
  scope stretto e ROI misurabile. Solo 7,7% delle PMI IT usa AI → mercato aperto.

---

## 11. Piano e prossimi passi

**Sequenza, in ordine (tutto sul PC nuovo, niente acquisti):**

1. **Profilo MCP `core`** (compose: `--profile extended` → `core`) → superficie
   tool 29 → 19.
2. **Zero-shot Qwen3 8B** via llama-server:
   `llama-server -hf unsloth/Qwen3-8B-GGUF:UD-Q4_K_XL --jinja -fa --cache-type-k q8_0 --cache-type-v q8_0 -c 16384`
3. **`debug_llm`** con la superficie tool reale, misurando 4 metri:
   - routing tool (inglese) tra ~19 tool — *il vero stress test del piccolo*
   - italiano di sintesi (forzato da system prompt)
   - **iterazioni-a-convergenza** + comportamento-al-cap (loop)
   - tok/s single-stream — con `nvidia-smi` a tutti i sidecar accesi
4. **Esito**:
   - regge zero-shot → Aura locale **senza addestrare**.
   - fallisce su routing-tool-concatenati / persona → **QLoRA Qwen3 8B in locale**
     (pipeline ri-puntata, marker ChatML).
   - separa **retrieval vs generazione**: se la risposta è errata, verifica prima
     che il chunk giusto fosse in contesto (un gap di retrieval non si aggiusta col
     modello).
5. **Porting reasoning-tier in locale**: ramo `tier → {enable_thinking,
   thinking_budget_tokens, reasoning-budget-message}` per Qwen3.
6. **Dimensiona l'appliance** sui dati: 8B basta → box 16GB; serve di più → ATOM.

**Domande aperte da chiudere coi test:**
- Qwen3 8B instrada bene tra ~19 tool reali, o serve scendere il profilo / salire di taglia?
- Italiano di sintesi zero-shot: sufficiente o serve finetune?
- tok/s single-stream reali sul 3060 (→ ×0,75 per l'ATOM)?
- Iterazioni-a-convergenza: chiude in 2-3 step o sfiora il cap 25?
- Co-residenza chat+OCR sotto carico: `nvidia-smi` conferma i ~11,5GB?

---

*Principio guida: misura prima di comprare, testa prima di addestrare. Il valore
di Aura è nel retrieval ibrido + memoria a grafo + specialisti + agent loop
blindato — non nella taglia del cervello. Un 8B che orchestra bene batte un 14B
più lento e affamato di VRAM.*
