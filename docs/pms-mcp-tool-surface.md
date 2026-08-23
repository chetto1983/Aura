# PMS MCP — superficie dei tool

**Stato:** bozza di progetto, **non misurata**. Derivata da `Le_Camille_PMS_Aura_Specifica_v1.md`
(§5 flusso, §7 import, §8 motore commerciale, §11 disponibilità, §12 margine) e dai vincoli letti
nel codice Aura il 2026-08-22.

**Cosa questo documento NON dimostra:** nessuno stack PMS è mai stato acceso, nessun file reale di
Le Camille è stato visto, nessun tool è stato eseguito. Per il principio PRD-first del progetto
questa è una **proposta da verificare all'incontro §14 n.3 (Dati e documenti)**, non un registro di
misure. Ogni riga qui dentro decade davanti alla prima misura contraria.

**Decisioni prese (2026-08-22):** SKU = recipe MCP Aura con seam `Backend`; schema modellato su
Le Camille per primo, generalizzazione per estrazione dopo il pilota.

---

## 1. Vincoli che vengono da Aura, non dalla specifica

Questi non sono negoziabili con il cliente: sono proprietà della piattaforma su cui il plugin gira.

| Vincolo | Dove | Conseguenza sul design |
|---|---|---|
| Aura è un **host MCP generico**: zero codice Aura-side, zero dichiarazione, no ceremony | `.planning/phases/46-mcp-trust-and-facade/46-CONTEXT.md` | Il server si monta senza toccare Aura. La riga nel catalogo recipe è comodità, non requisito. |
| **La curatela di una superficie troppo larga si fa NEL server**, perché ogni MCP Aura è un fork nostro | idem, Phase Boundary | I ~20 verbi qui sotto vanno filtrati dentro `cmd/pms-mcp`. Mai chiedere ad Aura di nascondere tool. |
| **Elicitation già cablata lato client** | `internal/agent/mcptools/elicitation.go`, `internal/mcp/sdkclient.go:48` | Il §8 "l'utente approva" si implementa con `ServerSession.Elicit`. Nessuna UI nuova, nessun `ask_user` custom. |
| Canali già coperti da recipe esistenti | `internal/mcp/manager/catalog.go:124` (PIM/mail), `:137` (WhatsApp) | Il PMS **produce la traccia** del §8; non invia. `pms_send_*` non esiste. |
| Memoria a lungo termine già esistente | `catalog.go:158` → `cmd/arcadedb-mcp` | Da decidere cosa vive in Postgres PMS (fatti transazionali) e cosa nel grafo (preferenze, sensibilità al prezzo). Non duplicare. |
| Template di server | `cmd/arcadedb-mcp` — un tool per file `tool_*.go`, tenant-scoped, StreamableHTTP | Stessa forma. Un file per verbo. |
| Porta del sidecar | `compose.yaml` | **Si legge al momento dell'atterraggio, non si deduce** — stessa disciplina del numero di migration. |

## 2. Regola di ammissione di un tool

Un tool entra in questa superficie solo se supera tutte e tre:

1. **È un verbo di dominio.** Nessun `execute_sql`, nessun CRUD generico su record. Questo è il modo in cui quasi tutti gli MCP ERP pubblici violano il §8: se il modello può scrivere un record arbitrario, può scrivere un prezzo.
2. **Il suo gate è deterministico.** La decisione di ammissibilità sta nel motore di regole del server, mai nel modello. Il modello propone; il server verifica; la persona approva.
3. **Ha una classe di scrittura dichiarata.** Lettura, fatto, o decisione (§3).

## 3. La superficie

### Classe A — lettura (nessuna elicitation, scope per identità)

| Tool | Cosa restituisce | Spec |
|---|---|---|
| `pms_daily_agenda` | Lista prioritaria di oggi: chi contattare, perché, priorità, momento consigliato | §5.4, §10 |
| `pms_customer_card` | Scheda cliente: ultimi due mesi, confronto stagionale, prodotti abituali e abbandonati, proposte accettate/rifiutate, motivazioni, sensibilità al prezzo | §10 |
| `pms_customer_search` | Ricerca per filtri **strutturati** (categoria, area, giro, stato, scostamento dalla frequenza attesa) | §6 |
| `pms_product_catalog` | Prodotti, formati, UM, modello produttivo (interno / lavorazione esterna / conto terzi / rivendita / co-branding) | §6, §21.5 |
| `pms_price_for` | Listino applicabile a *questo* cliente per *questo* prodotto a *questa* data, con validità e condizioni | §6 |
| `pms_availability` | Disponibilità per prodotto/lotto: quantità, stato, scadenza, già prenotato | §6, §11 |
| `pms_route_window` | Giro applicabile: zona, giorno, mezzo, orario limite, capienza residua, alternativa corriere refrigerato | §11, §21.6 |
| `pms_order_history` | Ordini e righe, con stato e canale | §6 |
| `pms_margin_estimate` | Margine stimato — livello 1 (fasce esperienziali) o livello 2 (costo completo), **dichiarando quale dei due** | §12 |
| `pms_kpi` | KPI di direzione e di pilota | §2, §10, §17 |

### Classe B — scrittura di fatti (nessuna elicitation, ma audit log)

Registrano cosa è successo. Non decidono nulla di commerciale, quindi non fermano il flusso.

| Tool | Cosa scrive | Spec |
|---|---|---|
| `pms_log_interaction` | Telefonata / messaggio / email: contenuto, esito, operatore | §5.8, §6 |
| `pms_record_rejection` | Motivo **strutturato** del rifiuto, nota, data utile per la riproposta | §6, §8 |
| `pms_defer_suggestion` | Rinvio di un suggerimento (l'azione "rinvia" del §8) | §8 |
| `pms_flag_data_issue` | Anomalia, dato mancante, attendibilità da rivedere | §6 |

### Classe C — decisioni commerciali (**elicitation obbligatoria**)

Qui vive la regola di sicurezza del §8. Ogni tool di questa classe fa `Elicit` **prima** di scrivere,
presentando esattamente l'output obbligatorio del §8 e le quattro azioni: approva, modifica, rinvia, scarta.

| Tool | Gate deterministico prima dell'elicitation | Spec |
|---|---|---|
| `pms_propose_order` | Nessuna scrittura: costruisce la proposta e la sottopone. È il punto di validazione del §5.6 | §8 |
| `pms_create_order` | Listino valido alla data, ordine minimo del canale, margine minimo, cliente non bloccato | §5.9, §6 |
| `pms_reserve_quantity` | Disponibilità reale, lotto, scadenza, quota già prenotata, capacità di preparazione | §11 |
| `pms_apply_price_override` | Soglia di autorizzazione **per ruolo** — solo Direzione | §4, §8 |
| `pms_accept_late_order` | Orario limite, tempo residuo, lavorazione richiesta, capacità, stato del giro, valore e margine. Restituisce uno dei sei esiti del §11 | §11 |
| `pms_assign_to_route` | Zona, giorno, mezzo, limite orario, capienza; distingue giro programmato / fuori giro / corriere refrigerato | §11, §21.6 |
| `pms_amend_order` | Modifica o annullamento: rilascia le quantità impegnate in modo atomico | §3 |
| `pms_record_return` | Reso o non conformità: prodotto, quantità, motivo, **responsabilità** (rilevante per le lavorazioni esterne), costo | §6, §21.5 |
| `pms_merge_records` | Deduplicazione: "mai fusione irreversibile senza verifica" | §6 |

### Classe D — import (§7, due fasi, mai one-shot)

| Tool | Ruolo |
|---|---|
| `pms_import_preview` | Riceve il file, accetta un mapping **proposto dal modello**, restituisce anteprima, errori, dati mancanti, duplicati candidati. Read-only. |
| `pms_import_commit` | Elicitation obbligatoria (§7 punto 7). Produce il registro delle righe importate, scartate e corrette; conserva il file di origine. |

Il §7 chiude con il vincolo che governa entrambi: *l'AI può suggerire il mapping e normalizzare il
testo, ma le validazioni fiscali, i codici e i vincoli sono regole software deterministiche.* Il
mapping è un **input** al server, mai una decisione del server.

## 4. Cosa NON è un tool di questo server

| Tentazione | Perché no |
|---|---|
| `pms_send_whatsapp` / `pms_send_email` | Le recipe `whatsapp` e `calendar` esistono già. Il PMS produce la traccia. |
| Nove server, uno per agente del §9 | I nove agenti sono ruoli dell'orchestratore Aura, non processi. Sono prompt e tool, non deployment. |
| `pms_generate_ddt` / fatturazione | §20 decisione #1 (gestionale e integrazione) è aperta. Costruire ora significa indovinare. |
| Ottimizzazione dei percorsi | §3 la mette esplicitamente fuori dal primo MVP. |
| `pms_query` / `execute_sql` | Viola il §8 alla radice. |

## 5. Il seam `Backend`

Un'interfaccia sola, così che il secondo cliente con un ERP già in casa non richieda un secondo prodotto:

- **Implementazione 1 — nativa Postgres.** Per Le Camille, che oggi ha Excel e non un ERP. Deve essere il **minimo che regge il pilota**: non reimplementare lotti, UM a peso variabile, listini e giri da zero — il modello si copia concettualmente da Odoo (`sale.order`, `product.pricelist`, `stock.lot`) e dagli *order cycle* di Open Food Network, che è l'unico progetto ad aver già risolto orario limite + giro + aggregazione ordini.
- **Implementazione 2 — adapter ERP via API HTTP.** Un sidecar che parla a Odoo o ERPNext via REST **non è opera derivata**: nessun linking, nessun import. Si prende il mercato di Odoo senza la sua licenza, e si resta puliti anche verso la GPL di ERPNext e Dolibarr.

La superficie dei tool del §3 è la stessa nei due casi. È questo che rende il plugin rivendibile.

## 6. Cosa è bloccato dalle decisioni aperte del §20

| Decisione aperta | Tool che non si può specificare finché non è chiusa |
|---|---|
| #1 Gestionale e modalità di integrazione | DDT, fatture, export amministrativo |
| #4 Modello di disponibilità e momento del blocco | `pms_reserve_quantity` — la semantica di "prenotato" vs "impegnato" è il tool |
| #5 Ruolo del sistema di tracciabilità esistente | semantica di lotto in `pms_availability` e `pms_record_return` |
| #7 Soglia e autorizzazioni per prezzi, sconti ed eccezioni | `pms_apply_price_override` — senza la soglia il gate non esiste |

## 7. Cosa si può costruire prima dell'incontro 3

Il guscio e la classe A. Il guscio (server, trasporto, scope per identità, elicitation, audit log) non
dipende dallo schema. La classe A dipende dallo schema ma è reversibile: una lettura sbagliata si
riscrive, una scrittura sbagliata lascia dati sbagliati.

Le classi B, C e D aspettano di aver visto un Excel vero, un DDT vero e un listino vero.

## 8. Licenze verificate (2026-08-22, leggendo i file `LICENSE`, non i badge)

Rilevante perché lo SKU è rivendibile.

| Progetto | Licenza reale | Plugin closed-source rivendibile? |
|---|---|---|
| Odoo | LGPLv3 | Sì — è il motivo per cui apps.odoo.com vende moduli proprietari |
| Medusa | MIT (salvo materiali Enterprise Edition) | Sì |
| ERPNext | GPL-3.0 | No — l'app è derivata |
| Dolibarr | GPL-3.0 | No |
| Twenty | AGPLv3 + file marcati `@license Enterprise` | No, ed è il caso peggiore: l'AGPL scatta anche con l'uso in rete |
| MCP go-sdk | MIT → Apache-2.0 (transizione in corso) | Sì |

Il seam del §5 rende la domanda quasi irrilevante: un adapter che parla via HTTP non tocca nessuna
di queste licenze.
