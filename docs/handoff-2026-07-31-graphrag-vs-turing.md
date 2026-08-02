# Handoff — GraphRAG: fork Neo4j vs turing_AgentMemory_MCP

Sessione del 2026-07-31. Il prompt da incollare nella sessione nuova è la sezione
[Prompt](#prompt); il resto è il contesto che lo giustifica.

---

## Prompt

> Riprendo la valutazione di `D:/turing_AgentMemory_MCP` come possibile sostituto del
> fork `neo4j-agent-memory` che Aura usa oggi per la memoria. Leggi
> `docs/handoff-2026-07-31-graphrag-vs-turing.md` per il contesto completo, poi:
>
> 1. **Verifica per prima cosa** che i tool `mcp__turing-agentmemory__*` siano nativi in
>    questa sessione (il server è registrato in user config e gira su
>    `http://127.0.0.1:8095/mcp/`). Se ci sono, **non scrivere Python per parlargli** —
>    usali direttamente. Se ancora non ci sono, dimmelo subito invece di aggirarli.
> 2. **Non ripetere le misure su 7 fatti**: sono già state fatte e non decidono nulla.
>    Il test che decide è il corpus vero in `D:/turing_AgentMemory_MCP/test`
>    (`Clienti.xlsx` con ground truth, ~18k PDF Normattiva). Carica lo stesso corpus su
>    entrambi i sistemi, fai le stesse domande, confronta contro la ground truth.
> 3. **Prima di caricare, libera memoria.** Il box ha 16,6 GB e il provider GLiNER di
>    turing (2,68 GiB) ha già OOM-killato `aura-rerank` una volta. Vedi
>    §"Budget di memoria" qui sotto.
> 4. Riferiscimi numeri, non impressioni: latenza p50, posizione della risposta corretta,
>    e se esiste una soglia che separa una domanda con risposta da una senza.
>
> In sospeso, da decidere con me: il lavoro Aura non committato (§"Non committato") e se
> tenere su lo stack turing.

---

## Cosa è stato stabilito, con evidenza

### Il grafo di Aura non è un grafo (confermato sul deployment vivo)

| | |
|---|---:|
| `:Entity` (reali: `Davide`, `Caraglio`, `PmSync`) | 27 (**3**) |
| `:Fact` (reali: 4 su Davide + 2 codici cliente) | 26 (**6**) |
| `:Fact` con `ABOUT_SUBJECT` | 4 |
| `RELATED_TO` (tutti e quattro residuo di test) | 4 |
| `:User` | 30 (quasi tutti residuo di test) |

Due difetti trovati leggendo i writer, **non** nel design:

1. **Due writer, due nomi di proprietà per lo stesso arco.**
   `CREATE_ENTITY_RELATIONSHIP` scrive `r.type`; `CREATE_ENTITY_RELATION_BY_NAME/_BY_ID`
   scrivono `r.relation_type`. Live: 3 archi con `type`, 1 con `relation_type`. Una
   traversata che legge una sola proprietà vede metà grafo. Latente oggi (i 4 archi sono
   spazzatura), fatale appena si materializzano archi veri.
2. **Il residuo di test è di due classi, non una.** 15 entità appese a `:User` che nessuna
   identità Postgres rivendica (discriminatore pulito e ripetibile); **9 scritte sotto
   l'identità reale dell'operatore** `e343c45d…` da script di audit/E2E — stesso
   `deduplication_scope` di `Davide`, **nessun discriminatore di provenienza**. Quelle
   vanno per lista di nomi rivista a mano. Causa radice: gli E2E vivi si autenticano come
   l'operatore.

### turing_AgentMemory_MCP: cosa è, e cosa è stato misurato

MCP ArcadeDB-native, 26 tool, già costruito e funzionante. `memory_search` dichiara
*"fused dense, BM25, entity, graph, and rerank signals"* e c'è `memory_rebuild_communities`
(Leiden) — cioè §2.4 e §3 del design `2026-07-31-graphrag-vero-design.md` già esistenti.

Scritture: 100–180 ms a fatto. Letture, **con reranker vivo**: 0,6–1,5 s.

Sugli stessi 7 fatti di Aura, le quattro domande di controllo:

| domanda | esito |
|---|---|
| «dove lavora la persona che vive a Caraglio?» | `works_for PmSync` in **4ª** posizione — il salto non lo fa |
| «codice cliente ZOPPI» | `located_in Volvera` e `codice_cliente 424410` **pari merito** |
| «xilofono quantistico marmellata» | 5 risultati, banda 0.0325–0.0317 |
| «dove vive Davide» | `timezone Europe/Rome` sopra `located_in Caraglio` |

**Tutti i punteggi, su tutte le domande, stanno entro il 7%.** `1/61 + 1/62 ≈ 0.0325`:
sono somme RRF con k=60 su due gambe.

### La conclusione che vale oltre questo confronto

**RRF non può esprimere "non lo so", per costruzione.** Vede solo *ranghi*, e ogni gamba
restituisce sempre un ordinamento completo — quindi una query senza senso e una query
perfetta cadono nella stessa banda di punteggio. Il design attribuiva il mancato
"non lo so" alla topologia assente; è più profondo, ed è vero identico per Aura, che usa
lo stesso `rrfK` sui documenti (`internal/documents/seed_fusion.go`).

Corollario operativo: **su 7 fatti nessuno dei due sistemi discrimina**, e nessuna misura
fatta lì decide alcunché. Serve il corpus vero.

---

## Trappole già pagate (non ripagarle)

**Budget di memoria — ha già rotto qualcosa.** Host: 16,6 GB. Il provider GLiNER di turing
occupa **2,68 GiB** e alle 09:08:37Z ha fatto **OOM-killare `aura-rerank`** (exit 137,
`OOMKilled: true`). Aura è rimasta senza reranker e turing ritentava il DNS verso un
container morto: **è tutta lì l'origine dei "14 secondi" per query**, non nel retrieval.
`aura-rerank` è stato riavviato ed è `healthy`. Prima di un ingest grosso: alzare il
limite, o fermare `aura-tts` + `aura-stt` (~1,2 GB) che alla misura non servono.

**`.env` ha line ending CRLF.** Un `set -a; . .env` in WSL appiccica `\r` a ogni valore →
il segreto diventa lungo 65 byte invece di 64 → `401`. Passare da
`tr -d '\r' < .env`. Ha bruciato due giri.

**L'auth del memory MCP di Aura non è un bearer token.** `AURA_AGENT_MEMORY_MCP_AUTH_SECRET`
è una **chiave HMAC** per un JWT HS256 che vive **60 secondi**
(`internal/mcp/memory_auth.go`: iss `aura`, aud `agent-memory`, scope `memory:access`,
sub `aura-service`). Passare il segreto come bearer dà `invalid_token`. Per questo un
mount MCP con header statico non regge: il token scade.

**turing MCP è stateful.** FastMCP emette un `mcp-session-id` su `initialize` e rifiuta
ogni chiamata successiva che lo ometta (`Bad Request: Missing session ID`). I one-liner
curl falliscono sempre, e non è il server rotto.

**I tool MCP non entrano a sessione avviata.** Il container è partito dopo l'avvio della
sessione: `claude mcp list` diceva `✔ Connected` ma `ToolSearch` non trovava nulla, né per
keyword né per nome esatto, e `/mcp reconnect` non è bastato. Serve una sessione nuova —
motivo per cui esiste questo handoff.

**Mai eseguire `.exe` sull'host Windows** (l'antivirus li blocca): compilare ed eseguire in
WSL o in container.

---

## Stato dei container

Su e healthy: `arcadedb` (2480), `agentmemory-gliner`, `turing-agentmemory-mcp` (8095),
più l'intero stack Aura. Il file
`D:/turing_AgentMemory_MCP/compose.aura-sidecars.yaml` (scritto in questa sessione) aggancia
turing all'embed e al rerank **già in uso da Aura** invece di duplicarli — entrambi gli
stack vogliono lo stesso Qwen3-0.6B a 1024d, e caricarlo due volte su una A2000 da 4 GB è
un OOM annunciato. ArcadeDB e GLiNER restano di turing: Aura non ha equivalenti.

```
docker compose up -d arcadedb agentmemory-gliner
docker compose -f compose.yaml -f compose.aura-sidecars.yaml up -d --no-deps turing-agentmemory-mcp
```

Modelli, presi da `/v1/models` dei sidecar Aura e non dai default GGUF del repo turing:
embed `Qwen/Qwen3-Embedding-0.6B-GGUF` (1024d), rerank
`Voodisss/Qwen3-Reranker-0.6B-GGUF-llama_cpp`.

---

## Non committato

**In `d:/Aura`** — Task 1 del piano, `go vet` + `go build` + `go test` + `-race` tutti verdi:

- `internal/agent/mcptools/bridge_memory.go` — soglia di recall di default (0.8) iniettata
  al bridge, gemella di `withMemoryUserIdentifier`; `acceptsUserIdentifier` generalizzata
  in `acceptsParameter(schema, name, whenUnknown)`.
  **L'asimmetria è il punto:** l'identità fallisce **aperta** (non scopare una lettura fa
  trapelare ogni tenant), la soglia fallisce **chiusa** (passare un argomento che il tool
  non dichiara è una chiamata rifiutata). Schema illeggibile, risposta opposta.
- `internal/agent/mcptools/bridge.go` — una riga in `Execute`.
- `internal/agent/mcptools/bridge_memory_threshold_test.go` — nuovo, 5 casi via
  `Bridge` + `fakeServer`.
- `internal/agent/mcptools/bridge_user_identifier_test.go` — call site rinominati,
  asserzioni invariate, più un caso sul nuovo asse `whenUnknown`.

Vale a prescindere da chi vince il confronto: `memory_search` documenta `threshold` e
nessuno lo passava mai.

- `docs/superpowers/plans/2026-07-31-graphrag-write-path-and-hygiene.md` — piano in 5 task
  per la strada "fork Neo4j". **Congelato in attesa dell'esito del confronto.** Se vince
  turing, i Task 2/4/5 (unificazione `relation_type`, endpoint tipizzati, backfill) muoiono
  con il fork; Task 1 e Task 3 (bonifica del residuo) restano validi comunque, perché il
  residuo va tolto da Neo4j in ogni caso.

**In `D:/turing_AgentMemory_MCP`** — `compose.aura-sidecars.yaml` (nuovo) più modifiche
preesistenti non mie a `compose.yaml`, `gliner_provider*.py`, `server.py`,
`tests/test_docker_hardening.py`.
