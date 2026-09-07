# Criteri prefissati per le risposte dell'agente

Agente: Codex in questa sessione, con il proprio MCP `aura-memory` montato e OAuth ordinario. I criteri sono fissati prima di redigere le risposte finali del test. Il contesto contiene già parte del lavoro: è una verifica guidata delle risposte fondate su fonti, non un benchmark cieco o indipendente. Le chiamate non scrivono fixture nella memoria dell'operatore.

Una risposta passa se soddisfa tutto il criterio, riporta i limiti pertinenti e non aggiunge affermazioni non sostenute dalle evidenze recuperate. Le domande ignote devono produrre astensione dell'agente anche se il motore restituisse candidati vagamente affini.

| ID | Domanda | Tipo | Criterio |
|---|---|---|---|
| Q01 | Quale database usa Aura per la memoria a lungo termine? | Diretta | Nominare ArcadeDB, senza inventare altri archivi per questa funzione. |
| Q02 | Perché il merge delle entità falliva nella validazione del 3 settembre? | Causale documentata | Collegare copia di sources LIST OF MAP e rifiuto Cypher; collocare il difetto nel passato. |
| Q03 | Perché i test non avevano intercettato quel difetto? | Sintesi | Indicare unit test con server finto e assenza di prove live, senza attribuire intenzioni. |
| Q04 | Posso ripuntare @out di un FACT indicizzato per fare la fusione? | Vincolo tecnico | Citare il limite misurato sugli indici e la copia degli archi; non promettere supporto generale. |
| Q05 | Per un'uguaglianza DATETIME basta una stringa RFC3339? | Disambiguazione | Distinguere uguaglianza e range; indicare date(valore,formato). |
| Q06 | Dove trovo il manuale ArcadeDB clonato? | Navigazione | D:/tmp/arcadedb-docs e src/main/asciidoc. |
| Q07 | In quale sorgente del manuale è documentato il limite sulle mappe? | Collegamento | reference/cypher/cypher-clauses.adoc; non confondere con una fonte generica. |
| Q08 | Il percorso memory_merge_entities–ArcadeDB del 4 settembre prova che oggi il merge è rotto? | Premessa falsa | Negare l'inferenza; descrivere MENTIONS e la fonte storica. |
| Q09 | Qual era la prossima indagine memorizzata alle 08:45 del 7 settembre? | Temporale | Traversata-temporale-memoria; rispettare l'intervallo 08:29:47–09:02:31 UTC. |
| Q10 | E quale risulta memorizzata alle 09:51:27 dello stesso giorno? | Confronto temporale | Percorsi-temporali-FACT-consistenti; distinguere proposta memorizzata e stato reale attuale. |
| Q11 | Alle 09:02:31 vale ancora il piano precedente? | Confine temporale | No: valid_to esclusivo; non usare la topologia senza as_of per affermare validità. |
| Q12 | Quali metriche di comunità hanno controesempi e quali numeri li mostrano? | Sintesi numerica | Conductance 0 invece di 1/7 con cut 1; modularityScore .75 invece di 0 per una comunità. |
| Q13 | Quale algoritmo ha ricostruito meglio i gruppi della nostra fixture? | Confronto | Leiden; limitare la conclusione alla fixture, distinguendolo da Louvain e label propagation. |
| Q14 | Tenere solo core >= 2 conserva le evidenze utili? | Premessa falsa | No: un percorso valido scende a core 1; preservare connettori ed evidenze. |
| Q15 | Abbiamo dimostrato un miglioramento generale usando PPR? | Calibrazione | No: il confronto sintetico a pari budget non mostra guadagno; non confondere con direct-first. |
| Q16 | Perché la paginazione di recall non terminava? | Nuovo dettaglio | Cursore inclusivo sull'ultimo turno, ripetizione del confine e punto fisso; descrivere il difetto storico. |
| Q17 | Quale database usa il progetto NeonSaffron Quartz77? | Senza evidenza | Astenersi; non trasferire ad esso i fatti di Aura. |
| Q18 | Chi ha approvato il budget di ProgettoAtlantide2024? | Senza evidenza | Astenersi; non inventare una persona o una data. |

