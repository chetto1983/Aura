# Verifica del prototipo — 22 ottobre 2026

Registro di ciò che è stato misurato su dati reali (Yahoo Finance, 22/10/2026) e di ciò che le
misure **non** dimostrano.

## 1. Formule

| Verifica | Metodo | Esito |
| --- | --- | --- |
| Sharpe, Sortino, Omega, volatilità, CAGR, max drawdown, Calmar, beta, VaR storico | Confronto con `empyrical-reloaded` su 1.500 rendimenti t-Student | Uguali a 1e-9 |
| RSI di Wilder | Esempio di StockCharts; primo valore a mano (3,34/14 ÷ 1,40/14) | 70,464 esatto sui prezzi arrotondati; scarto < 0,1 dalla tabella pubblicata, calcolata su prezzi non arrotondati |
| MACD, Bollinger, ATR | Confronto con la libreria `ta` | Uguali a 1e-9 (RSI converge entro 1e-4 dopo 200 sedute) |
| Parkinson, Garman-Klass, Rogers-Satchell (arXiv:2312.01426) | Monte Carlo su moto browniano geometrico con σ = 25% noto | Entro il 4% |
| Efficienza di Garman-Klass | Varianza dello stimatore contro close-to-close su 60 campioni | Oltre 3 volte (il paper riporta 7,4) |
| Probabilistic Sharpe Ratio | Riduzione all'errore standard di Lo con normalità; valore 0,5 al benchmark; frequenza Monte Carlo | Coerente |
| Cornish-Fisher | Assenza di asimmetria e curtosi riporta al quantile gaussiano | Coerente. Il test iniziale era sbagliato: al 95% la curtosi *riduce* il VaR, il segno si inverte a \|z\| = √3 |
| DuPont (dispensa USMx AF6010) | ROE = margine netto × rotazione × moltiplicatore, a mano e su ENI.MI reale | Identità esatta |
| Ciclo del circolante, liquidità, WACC, EVA, beta di Hamada | Valori calcolati a mano | Uguali |
| Espressioni | Test ostili: `__import__`, attributi dunder, lambda, indici, comprensioni, kwargs, potenze, input lunghi | Tutte rifiutate |
| Alfa di Jensen, capture ratio, information ratio | Confronto con `empyrical-reloaded` (alfa riportato alla sua capitalizzazione composta) | Uguali a 1e-9 |
| Treynor, M², beta corretto | Identità chiuse: Treynor = Sharpe × σ / β; il benchmark contro sé stesso ha alfa 0 e M² uguale al suo rendimento; 0,67 × grezzo + 0,33 | Esatte |
| Piotroski F-score | Tre esercizi costruiti a mano, segnale per segnale, più varianti con perdite, emissione di azioni e dato mancante | Uguali |
| Altman Z e Z'' | Calcolo a mano e soglie di zona | Uguali |
| Beneish M-score | Variabili calcolate a mano su due esercizi; i pesi applicati ai profili medi della Tabella 2 del paper danno −1,196 per i manipolatori e −2,246 per i controlli, ai due lati della soglia −1,78 | Uguali |
| Beta settimanale e upside capture su dati live | Ricalcolo indipendente da yfinance di ENI contro FTSE MIB | Entro il 2% |

## 2. Errori trovati dai test e corretti

| Problema | Come è emerso | Correzione |
| --- | --- | --- |
| Per i titoli di Londra Yahoo dà il prezzo in pence e la capitalizzazione in sterline | Test live: EV/EBITDA di Shell pari a 0,86 | `normalise_quote` divide per 100 solo il prezzo; test di regressione sui valori misurati |
| Tracking error gonfiato da chiusure non sincrone | SWDA.MI contro URTH: 6,5% settimanale; contro IWDA.AS 0,2% | Tracking error su rendimenti settimanali e avviso quando gli scarti da UTC differiscono |
| CAGR dei ricavi con un anno mancante in mezzo | Test unitario | Conteggio degli anni per posizione |
| ATR senza il range del primo giorno | Confronto con `ta` | TR del primo giorno = H − L, come Wilder |
| Per ASML il flusso di cassa operativo si chiama "Cash Flow From Continuing Operating Activities" e i rapporti che lo usano erano `null` | Misura delle righe Yahoo su 10 emittenti prima di scrivere i punteggi | Nome alternativo nella tabella delle righe |
| I tre prospetti di Yahoo hanno un numero di anni diverso (ENEL, SAP, TTE, KO, ISP) e venivano abbinati per posizione | Stessa misura; nessun valore sbagliato osservato, perché i primi anni coincidono | Anni abbinati per data di chiusura dell'esercizio; output identico al precedente su 5 ticker, salvo il flusso di cassa di ASML |
| La fonte del tracking error parlava di rendimenti giornalieri, il codice usa quelli settimanali | Rilettura delle fonti | Testo corretto |
| Con i punteggi, `peer_comparison` su 6 case auto restituiva 62.381 caratteri: Claude Code ha salvato il risultato in un file, l'agente l'ha letto con Grep e lo scorer non ha trovato i numeri (7,92/10) | E2E con agente, scenario Stellantis | Fonti una volta sola e componenti dei punteggi solo in `fundamental_ratios`: 41.904 caratteri, meno di prima dei punteggi |
| Ogni misura annualizzata usava 252 sedute anche per le criptovalute, che quotano tutti i giorni | Test sul Bitcoin, 3 anni: CAGR 23,1% contro il 35,2% di calendario (1.095 rendimenti letti come 4,35 anni), volatilità 39,0% contro 46,9% | 365 giorni quando Yahoo dichiara `CRYPTOCURRENCY`, riportati in `parameters.periods_per_year`; anche l'indicatore `volatility` |
| Il massimo e il minimo "a 52 settimane" erano le ultime 252 barre: per il Bitcoin otto mesi | Stesso test: massimo 87.364 contro 116.273 dell'anno di calendario | Finestra di 52 settimane di calendario |
| I rendimenti contro il benchmark erano calcolati prima di allineare le date: il lunedì del Bitcoin copriva domenica-lunedì, quello dell'S&P 500 venerdì-lunedì; lo stesso accade fra borse con festività diverse | Test con una cripto identica all'indice nei giorni feriali: correlazione 0,37 invece di 1 | Prezzi allineati prima dei rendimenti; ora correlazione e beta valgono 1 |
| La skill tecnica diceva di usare l'ATR "per dimensionare stop e posizioni", e l'agente proponeva stop a 1,5-2 ATR calcolati a mente | E2E con agente, in tutte le run | La skill vieta stop, target e dimensioni di posizione, e distanze invertite a mano |
| L'agente calcolava a mano la distanza dell'ATR e dalle bande | Valutazione dell'E2E con agente | Il tool fornisce `atr_pct_of_close`, `price_vs_*`, `sharpe_t_stat` |
| Per ENI Yahoo mette in "Interest Expense" gli oneri finanziari lordi (8,17 mld su 35,5 mld di debito medio, con 7,2 mld di "Interest Income"): copertura interessi 1,7× e WACC 8,9%, sopra il costo dell'equity | Test live: WACC > costo dell'equity. Su 11 emittenti gli interessi valgono 1,4-7,1% del debito di fine anno (XOM-BP), ENI 23,9% | Avviso quando gli interessi superano il 15% del debito medio; la skill non presenta copertura, costo del debito, WACC ed EVA come affidabili. Il test live asseriva `WACC ≤ ke`, che non è un'invariante: ora `0 < WACC ≤ max(ke, kd)` |

## 3. End to end

- **Test offline:** 124 verdi.
- **Test live via MCP:** 20 verdi, compresi volatilità, CAGR e massimi a 52 settimane del Bitcoin ricalcolati da yfinance.
- **E2E via protocollo MCP su dati live:** server avviato come sottoprocesso stdio, ogni tool chiamato su ticker reali e confrontato con i numeri di Yahoo e con un ricalcolo indipendente.
- **Agente reale:** Claude Code headless, con solo questo server e le skill, riceve le domande di un analista ed è valutato in automatico. La valutazione considera:
  - tool giusto con argomenti giusti;
  - skill caricata;
  - numeri decimali della risposta presenti nei risultati dei tool, con l'arrotondamento mostrato e con
    il segno: un numero scritto col segno vale solo con quel segno, tranne le perdite che il tool dà
    positive (VaR, expected shortfall, drawdown); uno scritto senza segno può averlo nelle parole
    ("8,2 punti in meno");
  - metriche richieste presenti;
  - avvertenze dichiarate.

  Controlli negativi: un numero inventato (27,35%) e un beta, una correlazione o un up capture col segno
  invertito vengono segnalati.

  Undici scenari:
  - i sette iniziali;
  - il beta in stile Bloomberg con il capture di ENI;
  - il controllo di qualità del bilancio di Stellantis;
  - rischio e analisi tecnica del Bitcoin contro l'S&P 500.

  Due run consecutive a codice fermo, dopo le correzioni chieste da CodeRabbit (`1896227`): **10,00** e
  **9,99** su 10. L'unico punto perso è un rapporto calcolato dall'agente: drawdown "oltre 2,5 volte"
  quello dell'S&P 500 (53,1% contro 18,9%). Nessun livello di prezzo stimato dal grafico, nessuno stop,
  nessun consiglio di trading.

  Lo scorer di `1896227`, il primo col segno, dava 9,84 e 9,90: confrontava col segno anche dove il
  segno non c'era o era scritto altrimenti. Questi falsi positivi, insieme a tre che lo scorer aveva già,
  sono corretti:
  - il meno scritto come trattino lungo o come trattino unificatore ("–132,7%", "‑53,1%");
  - il meno nel testo dei tool, letto senza segno ("Above -1.78" nella nota su Beneish);
  - il segno detto a parole ("1,5 punti percentuali sotto il FTSE MIB");
  - migliaia col punto all'italiana ("81.792 USD") e miliardi ("15,8 miliardi" per 15.801.000.000);
  - etichette di parametri ("MACD (12,26,9)").

  Un decimale col punto all'inglese ("0.28") prima non veniva letto affatto; adesso sì.

  Tutte le run conservate, ripunteggiate con lo scorer corrente (`--rescore`); tra parentesi il punteggio
  dato allora:
  - undici scenari, sul commit che annualizza le criptovalute su 365 giorni: 9,98 e 9,98 (9,94 e 9,93).
    Errore vero ripetuto in entrambe: nello scenario del grafico l'agente stima a occhio un livello di
    prezzo dall'immagine ("circa 18,5 €", "20–20,5 EUR");
  - nove scenari, dopo le correzioni a `peer_comparison` e alla skill tecnica: 10,00 e 10,00 (9,99 e 9,98);
  - nove scenari, prima di quelle correzioni: 9,73 e 9,82 (9,72 e 9,80), con lo scenario Stellantis a
    7,97 per la risposta troppo grande e l'agente che proponeva stop calcolati dall'ATR (vedi la
    sezione 2);
  - i sette scenari iniziali: 9,93 e 9,98 (9,95 e 9,97). Lo scorer col segno trova le distanze dai
    supporti girate in negativo ("SMA50 a 328,6 USD (−5,9%)" dove il tool dà il prezzo +5,94% sopra),
    corrette poi nella skill tecnica.

## 4. Modelli di previsione: scartati

Regola: un componente che impara deve battere il suo baseline stupido fuori campione, o si scarta.
`e2e/tsfm_eval.py`, protocollo fissato prima dei risultati e registrato in `e2e/runs/tsfm.json`: 10 serie
giornaliere (ENI.MI, ISP.MI, UCG.MI, AAPL, MSFT, NVDA, ^GSPC, FTSEMIB.MI, ^STOXX50E, SWDA.MI), 73 origini
ogni 10 sedute sulle ultime 750, 1.024 sedute di contesto, quantili 0,1-0,9. Test di Diebold-Mariano
con errori standard di Newey-West; t negativo = modello migliore.

Il test aggregato media le perdite dei titoli origine per origine, quindi vale solo fra titoli con le
stesse date: Milano (ENI, ISP, UCG) e USA (AAPL, MSFT, NVDA, ^GSPC). FTSE MIB, Euro Stoxx 50 e SWDA hanno
ciascuno un calendario proprio e hanno solo il test per titolo. Run del 23 ottobre: prima origine fra il
24 ottobre e il 6 novembre 2023 secondo il calendario, ultimo obiettivo il 7 o l'8 ottobre 2026.

| Modello | Prezzo a 20 sedute contro drift (t), Milano / USA | Quantili a 20 sedute contro simulazione storica (t), Milano / USA | Volatilità a 20 sedute contro EWMA, QLIKE (t), Milano / USA | Copertura dell'intervallo 80%, 5 / 20 sedute | Esito |
| --- | --- | --- | --- | --- | --- |
| Mistral Timestral-28M | −0,11 / −0,76 | −0,12 / −0,56 | +0,10 / −0,72 | 78,8% / 75,5% | Scartato |
| Amazon Chronos-Bolt-base | +1,03 / +2,02 (peggio) | +1,41 / +2,22 (peggio) | +0,25 / +0,36 | 81,9% / 74,8% | Scartato |
| Amazon Chronos-2 | +0,75 / −0,07 | +0,75 / +0,30 | +0,19 / −0,44 | 74,0% / 69,7% | Scartato |
| Simulazione storica (baseline) | — | — | — | 84,5% / 78,6% | — |

- Contro il drift nessun modello scende sotto t −0,8 in un gruppo, né sotto −1,7 su un singolo titolo.
- Timestral-28M batte la random walk a 20 sedute nel gruppo di Milano (t −3,02); Timestral e Chronos-2 la
  battono anche su singoli titoli (ISP, UCG, ^GSPC e SWDA: t fra −2,7 e −3,4). Il guadagno è la deriva, che il
  baseline di drift cattura senza modello.
- Sui quantili di MSFT Timestral e Chronos-2 hanno t −2,01 e −2,37: su 10 titoli e più test per modello
  due |t| poco sopra 2 sono compatibili col caso.
- A 20 sedute gli intervalli dei modelli sono troppo stretti: coprono dal 69,7% al 75,5% dei casi, contro
  il 78,6% della simulazione storica.
- La prima versione di questa tabella, run del 22 ottobre, mediava i dieci titoli insieme, con origini
  fino a 13 giorni di distanza fra Milano e New York. I verdetti non cambiano. Cambia la significatività:
  Chronos-Bolt è peggio del drift sui titoli USA (t +2,02), non su quelli di Milano (+1,03), mentre prima
  risultava peggio su tutti (+2,34).
- Una prima passata su Timestral-5M, con 21 quantili, ha dato lo stesso verdetto (t −0,74 contro drift a
  20 sedute); i suoi file non sono conservati perché lo script di allora è confluito in `tsfm_eval.py`.
- Licenze: Timestral ha un tetto di 20 milioni di dollari di ricavi mensili; Chronos è Apache-2.0.

### Criptovalute, 23 ottobre 2026

Stesso script e stesso protocollo su BTC-USD, ETH-USD, SOL-USD e XRP-USD, 73 origini per serie
(`e2e/runs/tsfm_crypto.json`). Per le criptovalute una seduta è un giorno di calendario.

| Modello | Prezzo a 20 giorni contro drift (t) | Quantili a 20 giorni contro simulazione storica (t) | Volatilità contro EWMA, QLIKE (t) | Volatilità contro EWMA, MSE della log-varianza (t) | Esito |
| --- | --- | --- | --- | --- | --- |
| Mistral Timestral-28M | −1,76 | −0,71 | −1,65 | −2,64 | Scartato |
| Amazon Chronos-Bolt-base | −0,13 | +0,94 | +0,56 | −0,49 | Scartato |
| Amazon Chronos-2 | −0,27 | +0,86 | −1,29 | −2,23 | Scartato |

- Prezzi e quantili: nessun modello batte drift o simulazione storica. A 5 giorni Chronos-2 e
  Chronos-Bolt fanno peggio della random walk (t +2,24 e +1,78), e Chronos-Bolt anche dei quantili
  storici (t +2,06).
- Volatilità: Timestral-28M e Chronos-2 hanno un MSE della log-varianza più basso dell'EWMA, ma non un
  QLIKE significativamente più basso. Il QLIKE è la perdita che resta coerente quando la varianza vera è
  sostituita da una proxy rumorosa come quella di Garman-Klass (Patton 2011); l'MSE della log-varianza no.
  Su 24 test, due |t| fra 2 e 3 sono compatibili col caso.
- Copertura dell'intervallo 80% a 20 giorni: Timestral 81,2%, la simulazione storica 77,4%. Sulle
  criptovalute gli intervalli di Timestral sono meglio calibrati che sulle azioni (75,8%).
- Esito: restano scartati. Il segnale sulla volatilità delle criptovalute è un'ipotesi da riprovare con un
  protocollo fissato prima, con più serie o una finestra più lunga; non è un risultato.

### Conferma su ETH e SOL con il solo Timestral-28M, 23 ottobre 2026

Timestral-28M è il modello migliore del test sulle criptovalute: t più forti sulla volatilità con
entrambe le perdite, il migliore sul prezzo a 20 giorni, intervalli meglio calibrati. Ripeterlo sulla
stessa finestra non è nuova evidenza, perché sono i dati che lo hanno scelto. L'ipotesi è stata scritta
nel report prima di eseguire (`e2e/runs/tsfm_eth_sol_holdout.json`, campo `hypothesis`):

- in una finestra disgiunta da quella del segnale, ottenuta togliendo prima le ultime 750 sedute,
  il QLIKE della volatilità a 20 giorni è più basso di quello dell'EWMA;
- confermata su un asset se il t di Diebold-Mariano è al massimo −2,24 (5% bilaterale con Bonferroni
  su due asset); confermata per le criptovalute solo se passano entrambi.

| Finestra | ETH: QLIKE (t) | SOL: QLIKE (t) | ETH / SOL: MSE della log-varianza (t) |
| --- | --- | --- | --- |
| Quella che ha scelto il modello, ott 2024 – ott 2026 (`tsfm_eth_sol.json`, in-sample) | −0,46 | −1,91 | −1,02 / −2,51 |
| Conferma, set 2022 – set 2024 (SOL da feb 2023, 59 origini) | −0,93 | −0,16 | −1,89 / −1,13 |

- Esito: ipotesi non confermata, Timestral-28M resta scartato anche sulle criptovalute. Il segnale stava
  quasi tutto su SOL nella finestra recente e non si ripete prima.
- La direzione è a favore del modello in tutti e quattro i casi, ma con un effetto che non si distingue
  dal caso. Prezzo e quantili restano non significativi (t fra −0,3 e −1,6).
- Le due finestre non si sovrappongono: l'ultimo obiettivo della conferma è del 22 settembre 2024, la
  prima origine della finestra recente del 3 ottobre 2024. I contesti dei modelli sì, ma i modelli non
  sono addestrati su questi dati.

### Kronos, 23 ottobre 2026

Kronos (arXiv:2508.02739, codice MIT) è preaddestrato su candele e legge barre OHLCV con le loro date.
Stesso script e stesso protocollo sulle quattro criptovalute, con le differenze fissate prima della
prima esecuzione e scritte in testa a `tsfm_eval.py`:
- campionamento a temperatura 0,9 e top-p 0,9, l'impostazione del paper per la volatilità, con 10
  traiettorie per origine;
- volatilità di Garman-Klass delle barre campionate e, come lettura secondaria, varianza delle chiusure
  campionate;
- solo origini dopo giugno 2024, la fine del suo preaddestramento. Sulle criptovalute la finestra
  comincia comunque a ottobre 2024, con 73 origini per serie.

Il campionamento passa da una cache KV, che riproduce i logit di Kronos a meno di 1e-5 ed è da 6,5 a
13,6 volte più veloce.

| Run | Prezzo a 20 giorni contro drift (t) | Quantili a 20 giorni contro simulazione storica (t) | Volatilità contro EWMA, QLIKE (t) | Copertura dell'intervallo 80%, 5 / 20 giorni |
| --- | --- | --- | --- | --- |
| Kronos-mini, 1.024 barre di contesto (`kronos_crypto.json`) | +7,56 (peggio) | +6,99 (peggio) | +0,57 | 65,4% / 43,8% |
| Kronos-mini, 40 barre, post hoc (`kronos_crypto_ctx40.json`) | +2,19 (peggio) | +4,42 (peggio) | −0,01 | 63,7% / 33,9% |
| Kronos-small, 40 barre, post hoc | +2,35 (peggio) | +3,61 (peggio) | +0,47 | 56,5% / 31,5% |
| Simulazione storica (baseline), prima finestra | — | — | — | 79,5% / 77,1% |

- Prezzo e quantili: peggio di tutti i baseline, con t da +2,2 a +7,6. Con 1.024 barre Kronos-mini
  estrapola la tendenza lunga: in una prova su nove origini di BTC la sua mediana a 20 giorni stava in
  media il 13,3% sopra l'ultima chiusura, contro un movimento effettivo medio del 5,7% in valore
  assoluto.
- Con le 40 barre che il paper usa per i dati giornalieri (Tabella 8), scelte dopo aver visto il
  risultato e dichiarate post hoc, l'errore sul prezzo scende ma resta peggiore del drift. Gli
  intervalli si stringono ancora: a 20 giorni coprono un terzo dei casi.
- Volatilità: con QLIKE non si distingue dall'EWMA (t da −0,01 a +0,57). Letta sulle chiusure campionate
  è peggio (t da +1,55 a +4,49).
- Esito: scartato. I run si sono fermati prima di Kronos-base e delle azioni, su richiesta: il primo
  passaggio bastava a vedere che Kronos non aggiunge nulla ai baseline.
- Durante i run sono emersi due problemi, corretti in `0f1ee88` prima di registrare risultati:
  - Kronos-base, con 256 righe per batch, chiedeva 10,5 GB di cache e il kernel ha chiuso il processo;
  - Kronos-small ha campionato barre con prezzi ≤ 0 su SOL e XRP, e la loro volatilità risultava NaN.
- Il run a 40 barre è precedente alla correzione, ma non contiene barre di quel tipo, quindi la
  correzione non lo cambia. Le due finestre differiscono di un giorno: dal 3 o dal 4 ottobre 2024.
- Non misurati: Kronos-base, le azioni, il fine-tuning.

## Cosa queste misure non dimostrano

- Che i dati Yahoo siano corretti o stabili: yfinance usa endpoint non ufficiali.
- Che le statistiche degli eventi abbiano valore previsivo: sono event study con finestre sovrapposte e senza costi.
- Che l'agente risponda bene a domande diverse dalle undici valutate.
- Che le risposte dell'agente siano fondate oltre ciò che lo scorer controlla. Lo scorer cerca ogni
  decimale della risposta fra i numeri dei tool e le metriche richieste fra quelli citati; non verifica
  che un numero sia attribuito alla grandezza giusta, che un calcolo dell'agente sia corretto, né che
  la risposta non contenga affermazioni senza numeri e senza fondamento. Il 10,00 e il 9,99 misurano
  solo questo.
- Che i punteggi coincidano con quelli dei terminali. Le differenze note sono queste:
  - Piotroski usa il debito corrente di Yahoo, che include i prestiti a breve, al posto della sola quota
    corrente del debito a lungo; senza la riga di emissione di azioni usa la variazione del numero di azioni.
  - Beneish usa lo stesso debito corrente nelle accruals, e DEPI vale quasi sempre 1, perché Yahoo di solito
    dà solo gli ammortamenti totali, senza separare quelli delle immobilizzazioni materiali: il paper fissa 1
    quando il dato manca.
  - Altman usa la capitalizzazione di oggi; la soglia alta 2,99 viene dal paper del 1968 come lo citano le
    fonti secondarie, perché il testo del 2000 letto dà solo 1,81 e 2,675; per Z'' non ci sono soglie.
  - Il capture è annualizzato come in empyrical; Morningstar dichiara la media geometrica ma non
    l'annualizzazione.
- Che `peer_comparison` stia sempre nel limite del client: 6 società valgono 41.904 caratteri, quindi 10
  possono superare il limite predefinito di Claude Code per i risultati MCP.
- Che i mesi del capture siano sempre completi: se l'ultimo giorno lavorativo del mese è festivo per la Borsa
  (il 31 dicembre a Milano), un'analisi lanciata il 30 scarta un mese chiuso.
- Che un beta basso indichi un titolo difensivo: per ENI contro il FTSE MIB la correlazione giornaliera è
  0,39-0,56 nel 2023-2025 e −0,24 nel 2026, e il Treynor, diviso per un beta di 0,28, sale all'85%.
- Che la soglia del 15% sugli interessi separi sempre il dato buono da quello lordo: è tarata su 11
  emittenti, e un emittente in difficoltà può pagare davvero più del 15%.
- Che i modelli scartati non servano su altri orizzonti, altre serie o con fine-tuning: è stato misurato
  solo l'uso zero-shot descritto sopra.
