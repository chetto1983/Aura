# Aura — Registro decisionale (decision registry)

> **Scopo.** Mappa di TUTTE le forcelle architetturali del rewrite, ordinate per
> **irreversibilità × finestra-decisionale** — l'asse "costoso-dopo" emerso dalla
> valutazione Neo4j/Memgraph (2026-06-01). Serve a decidere *deliberatamente* le
> scelte costose-da-cambiare PRIMA di costruirci sopra, non a scoprirle a metà build.
>
> **Come si usa.** Una decisione 🔓 OPEN in §2/§3 va portata a 🔒 con il rigore
> "ricerca aggiornata + mapping sul codice" prima della phase che la cementa.
> Le ⏳ validate-by-building NON si pre-decidono: si misurano costruendo.
>
> Creato 2026-06-01. Fonte: prd.md + ROADMAP.md + sessione di review #24-#29.

## Legenda

**Status:** 🔒 locked-validated · 🔓 open (da valutare) · ⏳ validate-by-building · 📦 deferred-v2
**Reversibilità** (costo di cambiare *dopo* che è atterrata): 🟢 bassa (swap localizzato) · 🟡 media · 🔴 alta (rework a cascata / re-processing corpus) · ⚫ molto alta (riscrittura)

---

## §0 — Decisione fondazionale: deployment & portabilità (D00)

| # | Decisione | Status | Revers. | Cosa la cementa | Validazione |
|---|---|---|---|---|---|
| **D00** | **Binario portabile unico** su 3 target (Hetzner cloud / mini-PC 32GB / DGX Spark) — locale-vs-remoto e provider selezionati da **config per-deployment**, NON build separate | 🔒 | ⚫ | ogni phase con sidecar, LLM, embedder, sandbox | Scelta utente 2026-06-01. Governa i vincoli di D11/D12/D13/D18-19. |

**Invarianti di portabilità (guardrail — violarle rompe silenziosamente un target):**

1. **Multi-arch arm64 + amd64.** DGX Spark è ARM (Grace); cloud/mini-PC x86. Binario Go cross-compila; **ogni sidecar deve avere immagine arm64** → guardrail di CI (build multi-arch), non scoperta sul DGX.
2. **seccomp è arch-specifico** (numeri syscall differiscono x86↔ARM) → la sandbox (D12) usa `libseccomp` (risolve per-nome) o due allowlist. **Trappola affilata.**
3. **Routing LLM/embedder config-driven** (LLMRouter Slice 13 promosso da v2 a seam day-1): nessun path hardcoda un provider. Client OpenAI-compat già abilita remoto + locale (vLLM/llama.cpp).
4. **Un solo embedder/dim su tutti i deployment** (D11): no index 768d sul mini-PC e 1024d sul DGX — lo stesso Aura li legge entrambi. Embedder scelto sul target più piccolo.
5. **`AURA_PRIVACY_MODE=local-only`** fail-fast se una capability (LLM/embedder/web) è remota → rende "massima privacy DGX" una garanzia verificabile, non una speranza. **Nuovo requisito da D00.**
6. **Budget risorse deployment-aware**: il chat LLM è **remoto** su 32GB (non regge un modello forte + Neo4j + Postgres + embedder + OS) e **locale** su DGX 128GB. Il footprint locale deve stare nel target più piccolo.

---

## §1 — Substrato fondazionale (LOCKED, validato)

| # | Decisione | Status | Revers. | Cosa la cementa | Validazione |
|---|---|---|---|---|---|
| D01 | **Graph DB = Neo4j 5.26** (vs Memgraph) | 🔒 | 🔴 | tutto il Cypher di Slice 11 | Valutato 2026-06-01: cheap-now/expensive-after-P15; Memgraph scartato per modello in-memory vs corpus che cresce all'infinito. **Vindicato da D00**: è l'unico graph DB che gira identico su tutti e 3 i target (disk-native scala giù a 32GB E su a DGX); Memgraph in-memory sarebbe stato ok solo su DGX. Ha immagine arm64. |
| D02 | **Relational = Postgres 17** + pgx + sqlc + golang-migrate | 🔒 | 🔴 | 6 migration shippate | Phase 1 done |
| D03 | **3-store split** (PG=app-state, Neo4j=knowledge+vector, FS=artifact) | 🔒 | 🔴 | ogni slice con persistenza | non-negoziabile (PRD §Persistence) |
| D04 | **Neo4j data via MCP** (mcp-neo4j-cypher) + native driver SOLO per DDL | 🔒 | 🟡 | client.go chokepoint | Phase 1 done; è ciò che rende D01 reversibile |
| D05 | **Runtime = Go 1.26** | 🔒 | ⚫ | tutto | go.mod 1.26.3 |
| D06 | **Agent runtime custom** (interface "stolen-not-imported" da adk-go) + workflow agents | 🔒 | ⚫ | Phase 2 done | costruito; reopening non vale (revers. ora ⚫) |
| D07 | **KV-cache = prefisso 3-segmenti** `[0]`system+tools `[1]`Agent.md `[2]`Insight `[3..N]`tail | 🔒 | 🟡 | Phase 6 + ogni iniezione memory | pinnato amendment #29 |
| D08 | **LLM client handrolled OpenAI-compat** (no SDK) | 🔒 | 🟢 | Phase 3 done | swappable per design |
| D09 | **Tool design = deferred-tool pattern** + `tool_search` | 🔒 | 🟡 | ogni tool | Phase 3 done |
| D10 | **Memory design** = mem0+Letta+GraphRAG+Cognee blend, valid-time, WRRF, soft-archive, POLE+O | 🔒 | 🔴 | Phase 15 | indurito amendments #24-29 + OQ chiuse |

---

## §2 — TIER-1: forcelle APERTE irreversibili (valutare ADESSO)

> Condividono con D01 la proprietà "costoso-dopo + sotto-esaminato". Sono le uniche
> due che meritano il trattamento completo prima di costruire.

| # | Decisione | Status | Revers. | Finestra (cosa la cementa) | Nota |
|---|---|---|---|---|---|
| **D11** | **Modello embedding + dimensione** | 🔒 *contratto* / ⏳ *pick finale* | 🟢 (se 768-native-GGUF) / 🔴 (se cambia dim) | benchmark prima di P15-ingest-a-scala | **Risolto 2026-06-01** (rigore Neo4j/Memgraph, 2 agenti). **Contratto locked**: index 768d, serving llama.cpp-GGUF `/v1/embeddings`, modello emette 768 (nativo o **MRL-truncato-a-768**) → qualsiasi 768-native-GGUF = drop-in (2 arg compose, zero migrazione); dim-diversa = wipe+re-embed forward-only. **Default locked**: EmbeddingGemma-300m@768d (wired, sub-200MB, lowest-risk). **Niente hardcoding del nome modello**. **Pick finale = benchmark P15 su corpus IT reale**, shortlist per costo-di-adozione: ① **Granite-r2 311m** (768 nativo→cheap, Apache, MMTEB ~65 batte Gemma ~61, ModernBERT/llama.cpp — *front-runner, da verificare serving GGUF/arm64*); ② Nomic-v2-moe (768, Apache, mid-quality); ③ EmbeddingGemma (incumbent); ④ Qwen3-0.6B (1024→migrazione, solo se delta qualità lo giustifica). jina-v5 escluso (CC-BY-NC). EmbeddingGemma battuto su qualità (~4 MMTEB) ma vince su RAM; Granite-r2 dissolve il trade-off (cheap+migliore+Apache). |
| **D12** | **Primitiva isolamento sandbox** (Slice 2) | 🔒 | 🟡 | Phase 5 | **RE-DECISO 2026-06-01** (amendment #36, supersede #32; driver = direttiva utente "sandbox come il tuo, non un giocattolo" su infra KVM-less; D-05/D-06/D-07). **Primario x86 = gVisor `runsc`** (D-05): user-space kernel syscall-intercepting, il guest non raggiunge mai il kernel host — l'isolamento più forte **senza `/dev/kvm`** (infra target KVM-less), modello di produzione Modal. gVisor è **default-on x86**, NON più escalation solo-sopra-5% (era #32). **Floor portabile = container indurito + seccomp allowlist positiva + userns-remap** (D-06): defense-in-depth *dentro* gVisor su x86 + boundary standalone di fallback su **arm64/DGX** finché gVisor-arm64 non è GA (240/294 syscall, 4KB-page — preoccupazione #32 ancora valida per il floor). 5 flag (`cap_drop ALL`, `no-new-privileges`, `pids_limit`, userns-remap) + profilo by-name con `SCMP_ARCH_AARCH64` + `SandboxEscapeBench` <5% Gate-3 sul **profilo gVisor-primary x86**. **Runner runtime-agnostico** (D-07): puro client HTTP → switch `runc`↔`runsc` è compose/daemon-concern, zero Go; `AURA_SANDBOX_RUNTIME` risolve `runsc`@amd64 / `runc`@arm64. **microVM RESTA RIGETTATO**: KVM bloccato su Hetzner-cloud + non confermato DGX → fallisce D00; gVisor è il massimo achievable. **wazero = fast-path futuro** (in-process, no-Docker, honors local-only) solo pure-compute; non regge py/shell/js 7e. **Obbligo tracciato**: QEMU-arm64-emulation (CI) può divergere dal seccomp di un kernel arm64 reale → conferma DGX reale resta pre-produzione arm64. |

---

## §3 — TIER-2: bet architetturali (moderatamente reversibili)

| # | Decisione | Status | Revers. | Finestra | Nota |
|---|---|---|---|---|---|
| D13 | **Strategia provider LLM** — multi-provider OpenAI-compat + routing per-deployment | 🔒 | 🟢 | continuo | **Risolto 2026-06-01 (amendment #33), ADJUST**: seam OpenAI-compat ratificato. 2 gap cablati: (a) **`AURA_PRIVACY_MODE=local-only` era solo prosa** → boot fail-fast (check loopback standalone, NON dipende da Slice 5) + `LLMRouter.Route()` trigger priorità-0; (b) **reasoning dual-field** (`reasoning`‖`reasoning_content`) — vLLM ha rimosso `reasoning_content`, il path DGX-local perdeva gli eventi reasoning. `:exacto` resta (Auto-Exacto default OpenRouter da mar-2026, no-op aggiuntivo). |
| D14 | **Coordinamento Swarm** (Slice 3/9) — ParallelAgent reuse + bus custom, cap 2-deep | 🔒 | 🟡 | Phase 9 | **Risolto 2026-06-01 (amendment #34), RATIFY+**: modello corretto (fan-out+supervisor 2026; budget condiviso D-10/D-11 già shippato bounda il costo dell'albero). 3 fix D00/D12: (A) `AURA_SWARM_MAX_CONCURRENT` per-target (manca cap *larghezza* → 32GB OOM, D00.6); (B) nota session-reuse serializza sul lock container (D12); (C) stub `MaxSpawnDepth=3`→2. Privacy-mode ereditato by-construction. Full N-deep = SWARM-V2-01 (deferred). |
| D15 | **Transport = AG-UI SSE gateway** (Slice 8) | 🔒 | 🟡 | Phase 12 | **Risolto 2026-06-01 (amendment #35), ADJUST**: AG-UI ratificato (standard 2026, blast-radius confinato). 3 fix: (1) **split 8a** (translator+fanout = critical-path Telegram) **/ 8b** (HTTP server+Dojo, deferito finché client HTTP reale + auth); (2) footgun `--bind 0.0.0.0` senza auth su Hetzner-cloud → richiede auth + fail-fast sotto `local-only`; (3) reasoning dual-field (= D13). |
| D16 | **Channels framework + Telegram primary** (Slice 9) | 🔒 | 🟡 | Phase 13 | Telegram È la porta d'ingresso del prodotto (README). Locked di fatto. |

---

## §4 — Decisioni pre-merge benchmark (non pre-decidibili, flag-ate)

| # | Decisione | Status | Finestra | Nota |
|---|---|---|---|---|
| D17 | Variante multimodale **Gemma 4** (E2B/E4B/26B/31B) | ⏳ | Slice 9c | benchmark accuracy+latenza+RAM su corpus reale; baseline E4B |
| D18 | **GPU vs CPU** per vLLM | ⏳ | Slice 13 | **Risolto per-target da D00**: DGX=GPU locale, mini-PC/cloud=remoto (nessun vLLM locale). Non più una scelta globale. |
| D19 | Modello **LLM locale** (per il path full-local DGX) | 🔓 | Slice 13 | **Non più "pura v2"**: su DGX "massima privacy = tutto locale" rende il path locale **centrale** (D00). Su DGX 128GB regge 70B-class quantizzato. Modello da benchmark IT+EN. Il *seam* (D13) è day-1; l'*implementazione* locale segue il target DGX. |
| D20 | CLI default **in-process vs via-agui** | ⏳ | Slice 8 | misurare latency roundtrip HTTP loopback (~50-150ms attesi) |

---

## §5 — Validate-by-building (TIER-3, si misurano costruendo)

| # | Decisione | Status | Nota |
|---|---|---|---|
| D21 | Task Canvas **deterministico vs LLM-refine** | ⏳ | OQ8 chiusa: skeleton det ship; LLM-refine on solo se offload-recovery-rate >0.15 su carico reale |
| D22 | **Tuning RRF / chunk-size / re-ranker** | ⏳ | OQ1/3 chiuse con default; affinare su benchmark recall corpus reale |
| D23 | Soglie Memify (180gg/<3), insight top-K (3/0.7) | ⏳ | default conservativi configurabili; misurare drift |

---

## §6 — Deferred v2 (fuori milestone, condizioni di rientro note)

| # | Item | Rientra se |
|---|---|---|
| D24 | vLLM + LMCache (LLM-V2-01) | GPU disponibile (DGX Spark path) |
| D25 | Skill cross-conv auto-suggest (Slice 7f, SKILL-V2-01) | dopo 0.7 HNSW + 7e + 11 |
| D26 | Full N-deep swarm (SWARM-V2-01) | use-case reale multi-livello |
| D27 | Bi-temporale memory (OQ7) | caso d'uso audit/retroattivo |
| D28 | Multi-user isolation (OQ6) | oltre single-user; hedge `identity_id` già stampato |

---

## §7 — Phase 8 / Slice 2b: decisioni implementative (D-01..D-14, ratificate 2026-06-03)

> Decisioni HOW della Phase 8 (sandbox session-bound), distinte dalla mappa architetturale
> D00-D28 di §0-§6. Cinque deviano dalla wording locked del PRD/ROADMAP e sono gated dietro
> **amendment-commit #38..#42** (landed in 08-01, doc-only gate, prima di ogni code wave).
> Fonte: `08-CONTEXT.md` §Implementation Decisions + `08-RESEARCH.md`.

| # | Decisione | Amendment | Deviazione PRD? |
|---|---|---|---|
| **D-01** | Sidecar session-bound tiene un **interprete Python long-lived per `session_id`** (in-memory `x=42` survives, prd.md:1252) come tier-1 + workspace mount RW come tier-2. I due claim di persistenza del PRD sono i due tier, non una contraddizione. Estende il sidecar 2a `subprocess.run`-per-call. | **#38** | ✅ (risolve la tensione interna PRD) |
| **D-02** | Persistenza asimmetrica documentata al modello: Python in-memory vars persistono; shell `cd`/`export` NON persistono cross-call (solo working dir API-managed re-applicato); file workspace persistono per entrambi. | (parte #38) | — |
| **D-03** | Idle interpreter = RAM cost driver (mini-PC 32GB); boundato da hard cap 5 sessioni concorrenti + 1800s idle-TTL eviction. Meccanismo interprete (REPL-over-stdin vs IPython kernel) = discrezione implementatore, invariante "x=42 survives + sidecar stdlib-only". | — | — |
| **D-04** | Un container gVisor `runsc` per `conversation_id`, lifecycle posseduto dal `SessionManager` control-plane. Threat model = uno/pochi utenti **trusted**, non tenant mutuamente-distrusting → gVisor (non Firecracker) sufficiente. | — | — |
| **D-05** | Carve-out docker-lifecycle contenuto: il `SessionManager` shella `docker run`/`stop`/`rm` per il *lifecycle*, ma l'*esecuzione* resta HTTP. L'invariante 2a "Go non guida il runtime Docker" ristretta a "non guida l'*esecuzione* via docker". **MUST NEVER mount the docker socket** (escape vector #1). | **#39** | ✅ |
| **D-06** | `aura.sandbox_sessions` = control-plane registry (migration **0008**). Boot recovery: row `status='active'` al boot → `'terminated'`, container ricreato lazy. File workspace sopravvivono al restart; stato in-memory interprete NO (container reaped). | — | — |
| **D-07** | Container lock per `session_id` (sync.Map + mutex) serializza `execute` concorrenti intra-session. Swarm worker che riusano il `SessionID` parent condividono il container e serializzano su questo lock. | — | — |
| **D-08** | Egress enforcato **fuori dalla sandbox** da un **forward proxy Go host-side** (hostname-CONNECT allowlist, default-deny, resolve-then-pin). **Supersede** "iptables OUTPUT rules" / "`network_mode: bridge` + iptables" — iptables richiede `CAP_NET_ADMIN`, incompatibile col floor `cap_drop: ALL`/D12. CAP-02 "via iptables" superseduta. | **#40** | ✅ (pivot di rete) |
| **D-09** | Riusa la SSRF machinery di Slice 5 `internal/web` (IP-classification + DNS-rebinding pin + redirect re-validation). Il sidecar ottiene `HTTP_PROXY`/`HTTPS_PROXY`/`PIP_PROXY` env verso il proxy. Deny-wins, glob domains. | (parte #40) | — |
| **D-10** | Solo granularità hostname/SNI, **NO MITM**. CONNECT-validation contro l'allowlist sufficiente per `pip install`. Honor `AURA_PRIVACY_MODE=local-only` (D00.5): allowlist non-vuota sotto local-only → fail-fast. Caveat data-exfil documentato in 08-SECURITY. | (parte #40) | — |
| **D-11** | Phase 8 shippa il **modulo condiviso `internal/scoring/` per intero** (pulled forward da Slice 6). Scheduler (P10) + Skills (P11) lo consumano. | **#41** | ✅ (home-slice migrata) |
| **D-12** | SCOPE GUARD: costruire il MODULO, non le pipeline applicative per-slice. Phase 8 wira SOLO il path advisory sandbox (`ComputeSandboxTier`); il `pending_approval` scheduler / audit columns / skills pending dir restano P10/P11. | (parte #41) | — |
| **D-13** | Walker host-side workspace usano `os.Root`/`os.OpenInRoot` (openat2), **supersede** `O_NOFOLLOW` letterale (CVE-2026-39861 è la shape esatta). | **#42** | ✅ (minore) |
| **D-14** | Cascade delete = walk openat no-follow manuale, **NON `os.RemoveAll`** (`os.Root` non ha ancora `RemoveAll`, golang/go#67002). Test acceptance: `ln -s /etc /workspace/escape` poi host cascade → no host `/etc` deletion. | (parte #42) | — |
| **D-15** | **PIVOT (supersede l'intera Slice 2 bespoke).** Sandbox = **code-sandbox-mcp** (MCP server Go stdio) montato via un **bridge MCP→agent-tool generico** (Shape B). Cancella sidecar/seccomp/SessionManager/host-proxy/`sandbox_sessions`/DinD/`cmd/aura` exec+sandbox+proxy. Delta: persistenza filesystem-only (no Python in-memory cross-call), networking docker default (no egress allowlist). Provato E2E su Docker Desktop 2026-06-03. Supersede D-01/D-08/D-09/D-10 + amendment #38/#40. `internal/scoring/` (D-11) resta. | **#43** | 🔄 (in build) |

**Schema landmine fix (correzioni, non amendment nuovi):** migration **0010 → 0008** (floor shippato è 0007, regola ordine-fase §Persistence) + `conversation_id` **text → uuid** (PK di `conversations` è uuid, verificato 0005). Applicati in prd.md §Slice 2b insieme agli amendment.

---

## §8 — Phase 9 / Slice 3: decisioni implementative (D-01..D-25, ratificate 2026-06-04)

> Decisioni HOW della Phase 9 (swarm minimale), distinte dalla mappa architetturale D00-D28
> di §0-§6 e dal D14 (swarm coordinamento, §3). La forma industriale MINIMA ("no atomic bombs"
> — lezione Phase 8) è congelata da 3 researcher pass + 2 spike live (001 mail / 002 whatsapp)
> + ruflo study. Gate dietro **amendment-commit #44** (landed in 09-01, doc-only gate, prima di
> ogni code wave — precedente 05-01/08-01). Fonte: `09-CONTEXT.md` §Implementation Decisions +
> `09-RESEARCH.md`.

| # | Decisione | Amendment | Deviazione PRD? |
|---|---|---|---|
| **D-01** | UN solo tool deferred `swarm_spawn {goals:[...]}` — blocking; fan-out interno wrappa il `ParallelAgent` shippato (Slice 0.9); ritorna array ordinato di per-child report. Core sequential dispatcher (`llm_agent.go`) untouched. NO `swarm_talk`/`swarm_join`/bus. | **#44** | ✅ (supersede acceptance Talk/broadcast Slice 3) |
| **D-02** | Per-child failure isolation: un errore child = entry `{id, status:"failed", error}` accanto ai sibling, NESSUNA cancellazione sibling (Anthropic partial-results; ruflo#1872). Il wrap bypassa il first-error-cancels-siblings di `ParallelAgent` riusandone i leak-safety idiom. | (parte #44) | — |
| **D-03** | Nessun param `tier` in v1: schema `{goals}` only. `AURA_SWARM_MODEL_*` restano no-op documentate; re-add in v2 SWARM-V2-01. | (parte #44) | ✅ (tier.go cut) |
| **D-04** | Pause-as-report: un child il cui `ask_user` scatta **termina** → entry `{child_id, tool_call_id, status:"needs_user_input", question, options}` (da `Event{Actions.AwaitingInput}` shippato). Il parent LLM rilancia via il proprio `ask_user` + re-spawn. NESSUN child parcheggiato / ResumeChild / Responder / pending volatile. | **#44** | ✅ (supersede OQ5 Responder design) |
| **D-05** | `ask_user` Spec guadagna optional `proxied_from_child_id` + `proxied_tool_call_id` (model-discretionary). Runner li stampa in `aura.paused_states` (colonne shippate 0003). Plumb 3-layer (Spec → InsertParams/Insert → persistPause). | (parte #44) | — |
| **D-06** | Worker `messages[0]` = parent `systemMessage()` byte-stable; il framing worker viaggia in `messages[1]`/UserTurns (NON muta `messages[0]` → cache CAP-04). | (parte #44) | — |
| **D-07** | Goal come primo USER message: brief Anthropic 4-part (objective, output format, tool guidance, boundaries). MAI in `messages[0]`. **Supersede OQ1** ("system prompt parametrizzato dal goal"). | **#44** | ✅ (OQ1) |
| **D-08** | Tool inheritance: full inherit MINUS `swarm_spawn` (D-10). `ask_user` resta (D-04 lo converte). `text_response` = terminale worker. | (parte #44) | — |
| **D-09** | Pre-spawn budget guard: reject quando `Budget.Remaining() < len(goals) + ~3` (reserve parent synthesis, Codex `reserve_spawn_slot`). Child ereditano via `Budget.Child()` shared `*atomic.Int32`. Snapshot `Remaining()` once prima del fan-out. | (parte #44) | — |
| **D-10** | Flat v1: i worker NON ricevono `swarm_spawn` (tool-exclusion). `AURA_SWARM_MAX_DEPTH=2` env + code-guard retained forward-compat. **ROADMAP SC#2 re-specced** (tool-not-available + code-guard unit-test). | **#44** | ✅ (supersede acceptance depth-runtime) |
| **D-11** | Per-child timeout `AURA_SWARM_CHILD_TIMEOUT_SEC` (default 120) = per-worker ctx deadline; timeout → `{status:"failed", error:"timeout"}`, siblings unaffected. Budget wallclock = ceiling globale. | **#44** | ✅ (nuova env) |
| **D-12** | Internal waves: goals oltre `AURA_SWARM_MAX_CONCURRENT` (default 4, 2 sul mini-PC) eseguiti in wave sequenziali nella stessa call (bounds RAM/FD, D00.6). | (parte #44) | — |
| **D-13** | Goals cap: `len(goals) > AURA_SWARM_MAX_GOALS` (default 8, nuova env) → tool error model-readable (Anthropic over-spawn failure #1). | **#44** | ✅ (nuova env) |
| **D-14** | Burst accepted: NO per-tool semaphores in v1, `MAX_CONCURRENT` = unico cap. sandbox-agent process-based (no per-session lock — **#34(B) wording superseduta**). | (parte #44) | ✅ (supersede #34(B)) |
| **D-15** | `ChildReport` array ordinato per goal_index: `{goal_index, child_id, status, summary (per-child cap ~2-4KB), error?, question?/options?/tool_call_id?}`. Overflow via lo shippato `tools.NewResult` (preview+sidecar+`read_tool_output`), NO secondo meccanismo. | (parte #44) | ✅ (supersede payload summarizer custom) |
| **D-16** | Ephemeral per-call runner dentro `swarm_spawn.Execute`: build N LlmAgent + wrap + drain + collect + return → GC. ZERO cross-call state (no children map/RWMutex/registry). Child IDs deterministici `w1..wN`. File: `internal/swarm/{swarm,report,brief}.go`. | **#44** | ✅ (cancella bus.go/tier.go/swarm_talk.go/swarm_join.go) |
| **D-17** | Silent-until-done + 3 slog line per child (`child.spawned`/`child.completed`/`child.failed`). No polling tool / event bus / forwarding seam in v1. | (parte #44) | — |
| **D-18** | Transcript dump always-on per child → `$AURA_RUN_DIR/<conv>/swarm/<w_i>.jsonl` via `Event.MarshalJSON`. GC via run-dir TTL sweep esistente. Best-effort (fail mai blocca lo swarm). | (parte #44) | — |
| **D-19** | Server (spike-validated): `mail` = martinzarfl/mail-mcp (spike 001), `whatsapp` = lharries/whatsapp-mcp fork `chetto1983/whatsapp-mcp@6de1dcd` (spike 002, bridge patch). Self only. Calendar DROPPED → Phase 16. Recipes in `mcpRecipes` puntano al fork. | (parte #44) | — |
| **D-20** | `mcptools.Mount` guadagna allowlist per-server. Mail v1: `send_email,fetch_emails,search_emails,get_thread`. WhatsApp v1: `send_message,list_messages,list_chats,search_contacts`. PLUS flip bridged tools `Deferred:false→true` (`bridge.go:88`) — protegge la soglia 30-50-tool di 8.1. | (parte #44) | — |
| **D-21** | Boot mount ALREADY EXISTS (`buildRegistryWithMCP`). **NESSUNA env `AURA_MCP_{MAIL,WHATSAPP}_SERVER`** — managed config `~/.aura/mcp/servers.json` via `aura mcp` è il path; `AURA_MCP_*_SERVER_JSON` restano override test-tier. Phase 9 rende il boot **fail-soft** (per-server WARN-and-drop, oggi abortisce). Non-goal: ping ticker / restart supervisor / lazy mount / OpenClaw plugin-host. | **#44** | ✅ (supersede env list del D-23) |
| **D-22** | Dual scoring gate: live E2E (`cot_eval`, OPENROUTER-gated, operator-run, NOT CI) = ground-truth hard floor (N worker via tool_use; fatti attesi; mail/whatsapp read-back; timing < 1.5× single) + judge rubric ≥90% (no "swarm" nel prompt; no over-spawn su control prompt). Numeri in `docs/aura-quality-snapshot.md`. | (parte #44) | — |
| **D-23** | Amendment Wave-0 (questo): doc-only PRD-amendment-gate (precedente 05-01/08-01) committato PRIMA di ogni code. **[NOTA: la env-add list del D-23 conteneva `AURA_MCP_*_SERVER` — superseduta da D-21, vedi Resolved OQ sotto.]** | **#44** | — |
| **D-24** | Anti-over-spawn load-bearing literal nella Description di `swarm_spawn`: usa SOLO per ≥2 subtask indipendenti self-contained; task semplice = rispondi diretto; ogni goal = brief completo (objective+output+boundaries; il worker non vede la conversazione). Test asserisce le frasi (pattern `finalizeNudge`). | (parte #44) | — |
| **D-25** | Property-based (Slice-3 requisito esplicito PRD): rapid properties — per ogni goals array (1..8) e mix di outcome: report.len/order == goals; total tree steps ≤ parent remaining at spawn; goleak-clean al return; per-child isolation (un fail/timeout non tocca i sibling). | (parte #44) | — |

**Resolved Open Questions (RESEARCH §Open Questions):**
- **OQ1 — env-add contradiction (D-23 vs D-21):** D-23 (pre-spike) elencava `AURA_MCP_{MAIL,WHATSAPP}_SERVER` come env da aggiungere; D-21 (spike-corrected, later truth) dice che NON esistono — il path di registrazione è la managed config `~/.aura/mcp/servers.json` via `aura mcp`. **Risoluzione: D-21 supersede; le 2 env `AURA_MCP_*_SERVER` sono DROPPED, solo `AURA_SWARM_MAX_GOALS` + `AURA_SWARM_CHILD_TIMEOUT_SEC` vengono aggiunte.** Applicato in prd.md §Caps & Limits.
- **OQ2 — worker registry clone helper:** `tools.Registry` non ha `Clone`/`Without`. In scope: un piccolo `Registry.Without(names ...string)` (o build in `internal/swarm`) per "parent registry minus swarm_spawn" (D-08/D-10).
- **OQ3 — D-06 system-overlay mechanism:** `NewLlmAgent` non ha hook per appendere un overlay a `messages[0]`. **Pick: (b)** il worker framing viaggia in `messages[1]`/UserTurns (NESSUN `SystemPrompt` mutation), `messages[0]` resta byte-stable cross-worker (cache CAP-04) — più semplice e soddisfa il goal di byte-stability del D-06.

---

## Phase 16 MCP Manager + Third-Party Trust Decisions

| ID | Decision | Requirement | Status |
|----|----------|-------------|--------|
| **D-26** | **CAP-09 / MCP-V2-01 promoted to v1.** Phase 16 is the Aura MCP Manager control plane, not only richer mail/WhatsApp recipes. It includes profiles, catalog metadata, trust approvals, sandboxed third-party local runtime, Streamable HTTP, status/doctor/logs, and risk-policy enforcement. | **CAP-09 / MCP-V2-01** | Planned |
| **D-27** | **Trust classes are canonical:** `trusted_recipe`, `trusted_local`, `sandboxed_local`, `remote_http`, and `blocked`. New third-party local commands default to `blocked`; chat boot filters blocked/untrusted servers before launch. | **CAP-09 / MCP-V2-01** | Planned |
| **D-28** | **OpenClaw remains separate.** Do not import the OpenClaw plugin-host runtime, typed plugin RPC, module loader, mTLS/audit surface, or public marketplace semantics into Phase 16. MCP servers are process/HTTP endpoints; OpenClaw plugins are arbitrary module code. | **CAP-09 / MCP-V2-01** | Planned |
| **D-29** | **No restart supervisor.** Preserve the Phase 9 lifecycle decision: fail-soft boot and on-demand doctor/status checks, not a background ping ticker or auto-restart loop for stdio MCP servers. Docker/container health is external runtime evidence. | **CAP-09 / MCP-V2-01** | Planned |
| **D-30** | **No marketplace auto-install.** Phase 16 supports explicit user commands, built-in trusted recipes, local catalog files, and manual third-party entries. Natural-language public registry browsing and auto-install are deferred. | **CAP-09 / MCP-V2-01** | Planned |

---

## Prossime azioni raccomandate

0. **D00 locked (2026-06-01)**: binario portabile unico. Le 6 invarianti di portabilità sono guardrail per ogni phase → da verificare in CI (multi-arch) e nel design (routing config-driven, privacy-mode, seccomp per-arch).
1. ~~Valutare D11 (embedding)~~ **RISOLTO 2026-06-01** (amendment #31): contratto 768/GGUF locked, EmbeddingGemma default, pick finale = benchmark P15 (front-runner Granite-r2). Vedi riga D11.
2. ~~Valutare D12 (sandbox)~~ **RISOLTO 2026-06-01, RE-DECISO** (amendment #36 supersede #32): **gVisor `runsc` primario e default-on su x86** (D-05), container+seccomp+userns-remap = floor portabile / fallback arm64 (D-06), runner runtime-agnostico via `AURA_SANDBOX_RUNTIME` (D-07), microVM resta rigettato, wazero fast-path futuro. Vedi riga D12. **→ Entrambe le forcelle TIER-1 (D11+D12) chiuse.**
3. ~~Le TIER-2 (D13-D15)~~ **RISOLTE 2026-06-01** (amendment #33/#34/#35): D13 routing (privacy-mode cablato + reasoning dual-field), D14 swarm (cap larghezza per-target), D15 transport (split 8a/8b + cloud-auth). Tutte ratificate; gli adjustment hanno cablato gli invarianti D00 #5/#6 che erano promesse non-implementate.
4. Non spendere pre-analisi su §5 (validate-by-building).
5. **Build-ready**: D00 lockato + D11/D12 + D13/D14/D15 risolte → **l'intera mappa decisionale (TIER-1 + TIER-2) è chiusa**. Phase 5 (sandbox) può partire dal piano indurito. Filo conduttore degli adjustment TIER-2: gli invarianti D00 (#5 privacy-mode, #6 footprint per-target) erano dichiarati ma non cablati nelle slice — ora specificati come acceptance.

### Amendment PRD richiesto (follow-up)

D00 implica edit al PRD prima di Phase 5: (a) `AURA_PRIVACY_MODE=local-only` nel catalogo env + guard fail-fast; (b) requisito multi-arch (arm64) su Stack + ogni sidecar; (c) promozione del seam LLMRouter da Slice 13-v2 a contratto day-1; (d) nota seccomp-per-arch in Slice 2. Tracciato qui, da formalizzare in un amendment.
