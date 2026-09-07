# Inventario degli algoritmi per la memoria

2026-09-07: esaminate tutte le **71 voci** del [catalogo ufficiale dei grafi](https://docs.arcadedb.com/arcadedb/reference/graph-algorithms/chapter), in 12 categorie. **39 procedure** eseguite sul motore installato; la forma Cypher di shortestPath è inoltre misurata nei test temporali. Le tre funzioni SQL sono voci distinte: non sono 71 algoritmi indipendenti.

«Eseguito» attesta una chiamata live e i controlli esplicitati nei test, non correttezza generale o beneficio di retrieval. Due metriche sono escluse dopo controesempi. Questo è il catalogo degli algoritmi sui grafi; le funzioni vettoriali e SQL generiche sono un'altra superficie e non sono incluse nel conteggio.

| Algoritmo | Categoria | Prova | Valutazione per Aura |
|---|---|---|---|
| `algo.pagerank` | centrality | Eseguito | Globale; anche Isolated riceve punteggio positivo. |
| `algo.betweenness` | centrality | Eseguito | Punteggio strutturale: nessuna equivalenza con rilevanza o affidabilità. |
| `algo.closeness` | centrality | Eseguito | Punteggio strutturale: nessuna equivalenza con rilevanza o affidabilità. |
| `algo.degree` | centrality | Eseguito | Punteggio strutturale: nessuna equivalenza con rilevanza o affidabilità. |
| `algo.harmonic` | centrality | Eseguito | Punteggio strutturale: nessuna equivalenza con rilevanza o affidabilità. |
| `algo.eigenvector` | centrality | Esaminato | Punteggio strutturale: nessuna equivalenza con rilevanza o affidabilità. |
| `algo.hits` | centrality | Eseguito | Punteggio strutturale: nessuna equivalenza con rilevanza o affidabilità. |
| `algo.katz` | centrality | Esaminato | Punteggio strutturale: nessuna equivalenza con rilevanza o affidabilità. |
| `algo.voteRank` | centrality | Eseguito | Diversifica D,A,Tail: mantiene anche periferia; da valutare sul contesto. |
| `algo.eccentricity` | centrality | Esaminato | Punteggio strutturale: nessuna equivalenza con rilevanza o affidabilità. |
| `algo.personalizedPageRank` | centrality | Eseguito | OUT; sink e fatti non validi impediscono uso diretto. |
| `algo.articlerank` | centrality | Esaminato | Punteggio strutturale: nessuna equivalenza con rilevanza o affidabilità. |
| `algo.wcc` | community-detection | Eseguito | Partizioni e isole: 7+1. |
| `algo.scc` | community-detection | Eseguito | Cicli diretti: 3+3+1+1. |
| `algo.louvain` | community-detection | Eseguito | Frammenta le terne nella fixture; non scelto. |
| `algo.leiden` | community-detection | Eseguito | Prioritario: ABC / DEFTail / Isolated; stabile in tre ripetizioni. |
| `algo.labelpropagation` | community-detection | Eseguito | Collassa tutti i sette nodi connessi nella fixture. |
| `algo.hierarchicalClustering` | community-detection | Esaminato | Memoria quadratica e numero gruppi imposto; priorità a Leiden. |
| `algo.slpa` | community-detection | Eseguito | Organizzazione di gruppi; da confrontare con Leiden. |
| `algo.maxKCut` | community-detection | Esaminato | Obiettivo di separazione diverso da contesti coerenti. |
| `algo.modularityScore` | community-quality | Eseguito | ESCLUSO: una comunità restituisce .75 anziché 0. |
| `algo.conductance` | community-quality | Eseguito | ESCLUSO: un arco di confine restituisce cut=0 anziché 1. |
| `algo.graphSummary` | graph-statistics | Eseguito | Diagnostica; isolatedNodes include i sink. |
| `algo.maxFlow` | network-flow | Esaminato | Nessun contratto di capacità per i fatti. |
| `algo.assortativity` | network-science | Esaminato | Salute aggregata della rete; priorità inferiore alle query. |
| `algo.richClub` | network-science | Esaminato | Salute aggregata della rete; priorità inferiore alle query. |
| `algo.influenceMaximization` | network-science | Esaminato | Propagazione probabilistica non corrisponde a verità dei fatti. |
| `algo.fastrp` | node-embedding | Eseguito | Baseline sperimentale: coseno A-B .970, A-D .764, A-isolato .138. |
| `algo.node2vec` | node-embedding | Esaminato | Più impegnativo; misurare prima l'utilità del baseline FastRP. |
| `algo.graphsage` | node-embedding | Eseguito | Non scelto: A-D .950 supera A-B .884; proiezioni casuali non addestrate. |
| `algo.hashgnn` | node-embedding | Esaminato | Alternativa strutturale; beneficio di retrieval non stabilito. |
| `algo.dijkstra` | path-finding | Eseguito | Costo 4 con BOTH; pesi ed evidenze da motivare. |
| `algo.astar` | path-finding | Esaminato | Manca un'euristica motivata; Dijkstra già misurato. |
| `algo.bellmanford` | path-finding | Esaminato | Non sono definiti costi negativi per la memoria. |
| `algo.allsimplepaths` | path-finding | Eseguito | Un percorso di quattro hop con relazioni; enumerazione da limitare. |
| `algo.kShortestPaths` | path-finding | Eseguito | OUT, costo 6; payload length=0 e relazioni vuote, richiede risoluzione provenance. |
| `algo.apsp` | path-finding | Esaminato | Memoria quadratica; preferire query limitate. |
| `algo.bfs` | path-finding | Eseguito | Prioritario: raggio 2 da A restituisce B,C,D ed esclude il seed. |
| `algo.dfs` | path-finding | Esaminato | Non ordina per distanza; BFS prima per un budget limitato. |
| `algo.dijkstra.singleSource` | path-finding | Esaminato | Priorità a raggio e target espliciti rispetto a tutti i costi. |
| `algo.longestPath` | path-finding | Esaminato | Richiede DAG; interessa meno dei percorsi minimi limitati. |
| `algo.msa` | path-finding | Esaminato | Arborescenza radicata non richiesta dalla selezione delle evidenze. |
| `algo.steinerTree` | path-finding | Eseguito | Prioritario per più entità: connette A,E,Tail con cinque archi. |
| `algo.jaccard` | similarity-link-prediction | Eseguito | Candidati simili: A-B=1/3, A-D=1/4; non equivalenza di identità. |
| `algo.adamicAdar` | similarity-link-prediction | Eseguito | Candidati con meno peso agli hub; include anche nodi adiacenti nel runtime. |
| `algo.commonNeighbors` | similarity-link-prediction | Eseguito | Candidati dal vicinato; non inferire fatti o fusioni. |
| `algo.preferentialAttachment` | similarity-link-prediction | Esaminato | Candidati dal vicinato; non inferire fatti o fusioni. |
| `algo.resourceAllocation` | similarity-link-prediction | Eseguito | Candidati con peso inverso al grado; non nuove affermazioni. |
| `algo.simRank` | similarity-link-prediction | Esaminato | Memoria quadratica; priorità a Jaccard/Adamic-Adar. |
| `algo.knn` | similarity-link-prediction | Eseguito | Candidati dal vicinato; non inferire fatti o fusioni. |
| `algo.sameCommunity` | similarity-link-prediction | Esaminato | Candidati dal vicinato; non inferire fatti o fusioni. |
| `algo.totalNeighbors` | similarity-link-prediction | Esaminato | Candidati dal vicinato; non inferire fatti o fusioni. |
| `dijkstra()` | sql-path-functions | Esaminato | La procedura Cypher copre la prova necessaria. |
| `bellmanFord()` | sql-path-functions | Esaminato | Forma SQL e costi negativi non richiesti. |
| `shortestPath()` | sql-path-functions | Esaminato | Tre errori SQL precedenti: non ripetuti. Forma Cypher già misurata. |
| `algo.kcore` | structural-analysis | Eseguito | Non eliminare fatti per basso core; dipende dalla proiezione temporale. |
| `algo.triangleCount` | structural-analysis | Eseguito | Diagnostica di coesione/connettività; preservare i supporti periferici. |
| `algo.articulationPoints` | structural-analysis | Eseguito | Prioritario: trova C,D,F, connettori fra gruppi. |
| `algo.bridges` | structural-analysis | Eseguito | Trova C-D e F-Tail; direzione OUT, da non equiparare sempre a cut non diretti. |
| `algo.mst` | structural-analysis | Esaminato | Riduzione globale degli archi; Steiner è più adatto a terminali espliciti. |
| `algo.topologicalSort` | structural-analysis | Esaminato | Richiede DAG; FACT e MENTIONS possono avere cicli. |
| `algo.bipartite` | structural-analysis | Esaminato | Solo diagnostica di bipartizione, non presunta per Entity-Entity. |
| `algo.graphColoring` | structural-analysis | Esaminato | Problemi di vincoli/risorse, non recupero di evidenze. |
| `algo.cycleDetection` | structural-analysis | Esaminato | SCC copre già il controllo dei cicli in questa prova. |
| `algo.densestSubgraph` | structural-analysis | Eseguito | Seleziona A-F, perde Tail; non basta come selettore. |
| `algo.clique` | structural-analysis | Eseguito | Diagnostica di coesione/connettività; preservare i supporti periferici. |
| `algo.kTruss` | structural-analysis | Eseguito | Diagnostica di coesione/connettività; preservare i supporti periferici. |
| `algo.biconnectedComponents` | structural-analysis | Eseguito | Prioritario: blocchi con connettori condivisi; l'isolato non produce righe. |
| `algo.localClusteringCoefficient` | structural-analysis | Eseguito | Diagnostica di coesione/connettività; preservare i supporti periferici. |
| `algo.bipartiteMatching` | structural-analysis | Esaminato | Memoria non garantita bipartita; assegnazione uno-a-uno non richiesta. |
| `algo.randomWalk` | traversal-sampling | Eseguito | Campionamento che può perdere supporti; payload senza relazioni. |

Query, output e controesempi: [results.txt](results.txt). Metodo e limiti: [README.md](README.md). Classi installate: [installed-procedures.txt](installed-procedures.txt). Tutte le nuove prove scrivono solo in database dedicati eliminati a fine test.

