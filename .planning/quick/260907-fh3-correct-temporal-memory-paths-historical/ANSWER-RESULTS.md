# Test delle risposte finali dell’agente

2026-09-07. Ho formulato queste risposte come agente Codex, usando il mio MCP `aura-memory` montato dopo l’aggiornamento dei container. Criteri: [ANSWER-CRITERIA.md](ANSWER-CRITERIA.md). Le risposte sono state controllate contro le evidenze recuperate, non contro il semplice successo delle chiamate.

**Esito: 18/18 risposte conformi ai criteri prefissati**, incluse due astensioni, tre letture temporali e il rifiuto di premesse non sostenute. È una verifica guidata e autovalutata nella sessione che conosce il progetto; non è una valutazione cieca o indipendente su tutti i domini.

| Caso | Risposta finale formulata dall’agente | Evidenza | Esito |
|---|---|---|---|
| Q01 | Aura usa ArcadeDB come memoria a lungo termine. | `32f4388b` | PASS |
| Q02 | Nel difetto registrato il 3 settembre, il merge copiava anche sources, una LIST OF MAP. L’assegnazione tramite Cypher rifiutava quei valori, facendo fallire le fusioni con provenienza. | `89982b2a` | PASS |
| Q03 | C’erano test unitari contro un server finto che poteva rispondere moved:1 a qualsiasi statement; mancava una verifica sul database reale. | `bc5e26bb` | PASS |
| Q04 | Sul motore 26.9.1 misurato, ripuntare @out falliva quando il tipo di arco aveva indici. Per FACT la soluzione adottata era copiare l’arco, preservandone i dati. | `cfa3f014` | PASS |
| Q05 | Nei confronti SQL misurati, la stringa RFC3339 funzionava per i range, ma non per l’uguaglianza DATETIME. L’uguaglianza richiedeva date(valore, formato). | `1c73c20c` | PASS |
| Q06 | Il clone del manuale è D:/tmp/arcadedb-docs; i sorgenti sono sotto src/main/asciidoc. | `8d95b4e7` | PASS |
| Q07 | Il riferimento specifico è reference/cypher/cypher-clauses.adoc, che descrive il rifiuto delle mappe come valori delle proprietà. | `f88b30dc` | PASS |
| Q08 | No. Il percorso del 4 settembre contiene una MENTIONS sostenuta dal fatto sul vecchio difetto LIST OF MAP. È una connessione documentata a quell’incidente, non la prova che il merge sia oggi guasto. | `89982b2a + graph_path 2026-09-04` | PASS |
| Q09 | Alle 08:45 UTC risulta la proposta Traversata-temporale-memoria. Il record è valido dalle 08:29:47 alle 09:02:31 UTC del 7 settembre. | `RID #5:14338, lettura as_of` | PASS |
| Q10 | Alle 09:51:27 UTC risulta Percorsi-temporali-FACT-consistenti: verificare la consistenza dei percorsi e provarli via SDK/MCP. Questo descrive il piano memorizzato a quell’ora, non lo stato attuale dell’implementazione. | `733caec0, lettura as_of` | PASS |
| Q11 | No. Alle 09:02:31 UTC il record precedente ha raggiunto il suo valid_to esclusivo; la lettura temporale restituisce già il piano successivo. | `RID #5:14338 + 733caec0` | PASS |
| Q12 | Conductance ha restituito cut=0 e valore 0 con un arco fra comunità, dove gli attesi erano cut=1 e conductance=1/7. ModularityScore ha restituito 0,75 per una sola comunità, dove l’atteso era zero. Sono controesempi che ne impediscono l’adozione in questa versione. | `91811827` | PASS |
| Q13 | Nella nostra fixture Leiden ha ricostruito ABC / DEFTail / Isolated ed è rimasto stabile in tre prove. Louvain ha frammentato le terne e label propagation ha unito i sette nodi connessi. La conclusione riguarda quella fixture. | `b1c74036` | PASS |
| Q14 | No. Nella prova temporale un percorso necessario è sceso da core 2 a core 1. Bisogna conservare le evidenze pertinenti e i connettori anche quando la coreness è bassa. | `b1c74036` | PASS |
| Q15 | No. Il confronto sintetico a pari budget non ha mostrato un guadagno da PPR. Il miglioramento osservato con la precedenza ai fatti diretti è un risultato distinto. | `733caec0 + mounted-budget-results.json` | PASS |
| Q16 | Nel difetto storico, il cursore indicava l’ultimo turno già restituito mentre la query era inclusiva. La pagina successiva ripeteva il confine e, a fine conversazione, il cursore restava identico. | `e583148e` | PASS |
| Q17 | Non ho trovato evidenze su NeonSaffron Quartz77 nella memoria consultata, quindi non posso indicarne il database. | `memory_recall: no_qualified_candidates` | PASS |
| Q18 | Non ho trovato evidenze sull’approvazione del budget di ProgettoAtlantide2024. Non posso attribuirla a una persona o a una data. | `memory_recall: no_qualified_candidates` | PASS |

## Evidenze e perimetro

- `memory_facts_about(ArcadeDB, depth=2, limit=20)` ha restituito i fatti di riferimento tecnici, le fonti del manuale e le diagnosi storiche. `memory_facts_about(Validazione-Algoritmi-Memoria-2026-09-07, depth=1)` ha fornito i risultati degli algoritmi.
- Le risposte Q09–Q11 usano chiamate distinte con istanti espliciti. Q08 usa il percorso montato con direzione originale e supporting_fact. Il fatto storico Q09 non ha più una chiave di correzione attiva, ma mantiene RID e provenienza.
- Q17–Q18 hanno chiamato `memory_recall` in modalità semantic con le domande complete; entrambe le risposte del motore hanno `abstained=true`, `reason=no_qualified_candidates`, nessun fatto e nessun turno.
- Gli identificatori di fatto in tabella sono prefissi dei `fact_key` restituiti dal MCP, usati qui per leggere il rapporto. Non passarli troncati a un tool di correzione.
- I risultati completi delle chiamate contenenti memoria dell’operatore erano in `.planning/tmp/`, esclusa da git e rimossa da una pulizia concorrente prima della pubblicazione. Questo rapporto conserva risposte sul progetto e risultati aggregati; i payload privati originali non sono più disponibili.
- Non sono state scritte fixture nella memoria dell’operatore. Non è stata misurata una nuova politica PPR, una superiorità generale rispetto ad altri sistemi, o qualità di risposta su ambiti privi di fonti.
