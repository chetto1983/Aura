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

## 2. Errori trovati dai test e corretti

| Problema | Come è emerso | Correzione |
| --- | --- | --- |
| Per i titoli di Londra Yahoo dà il prezzo in pence e la capitalizzazione in sterline | Test live: EV/EBITDA di Shell pari a 0,86 | `normalise_quote` divide per 100 solo il prezzo; test di regressione sui valori misurati |
| Tracking error gonfiato da chiusure non sincrone | SWDA.MI contro URTH: 6,5% settimanale; contro IWDA.AS 0,2% | Tracking error su rendimenti settimanali e avviso quando gli scarti da UTC differiscono |
| CAGR dei ricavi con un anno mancante in mezzo | Test unitario | Conteggio degli anni per posizione |
| ATR senza il range del primo giorno | Confronto con `ta` | TR del primo giorno = H − L, come Wilder |
| L'agente calcolava a mano la distanza dell'ATR e dalle bande | Valutazione dell'E2E con agente | Il tool fornisce `atr_pct_of_close`, `price_vs_*`, `sharpe_t_stat` |
| Per ENI Yahoo mette in "Interest Expense" gli oneri finanziari lordi (8,17 mld su 35,5 mld di debito medio, con 7,2 mld di "Interest Income"): copertura interessi 1,7× e WACC 8,9%, sopra il costo dell'equity | Test live: WACC > costo dell'equity. Su 11 emittenti gli interessi valgono 1,4-7,1% del debito di fine anno (XOM-BP), ENI 23,9% | Avviso quando gli interessi superano il 15% del debito medio; la skill non presenta copertura, costo del debito, WACC ed EVA come affidabili. Il test live asseriva `WACC ≤ ke`, che non è un'invariante: ora `0 < WACC ≤ max(ke, kd)` |

## 3. End to end

- **Test offline:** 73 verdi.
- **Test live via MCP:** 15 verdi.
- **E2E via protocollo MCP su dati live:** server avviato come sottoprocesso stdio, ogni tool chiamato su ticker reali e confrontato con i numeri di Yahoo e con un ricalcolo indipendente.
- **Agente reale:** Claude Code headless, con solo questo server e le skill, riceve 7 domande di un analista ed è valutato in automatico. La valutazione considera:
  - tool giusto con argomenti giusti;
  - skill caricata;
  - numeri decimali della risposta presenti nei risultati dei tool, con l'arrotondamento mostrato;
  - metriche richieste presenti;
  - avvertenze dichiarate.

  Un numero inventato inserito come controllo negativo (27,35%) viene segnalato.

  Due run consecutive, a codice fermo, dalla posizione in Aura: **9,95** e **9,97** su 10. Sulle 14 risposte
  i punti persi vengono da due cause:
  - due falsi positivi dello scorer: etichette di parametri scritte con la virgola ("Bollinger 20,2",
    "MACD 12,26,9") lette come decimali;
  - due calcoli fatti a mente dall'agente, aritmeticamente giusti e segnalati con "circa", ma contro la
    regola della skill: la distanza della SMA200 invertita rispetto a `price_vs_sma200` (−15,6% invece di
    +18,54%) e il rapporto fra expected shortfall e VaR (1,6).

## 4. Modelli di previsione: scartati

Regola: un componente che impara deve battere il suo baseline stupido fuori campione, o si scarta.
`e2e/tsfm_eval.py`, protocollo fissato prima dei risultati e registrato in `e2e/runs/tsfm.json`: 10 serie
giornaliere (ENI.MI, ISP.MI, UCG.MI, AAPL, MSFT, NVDA, ^GSPC, FTSEMIB.MI, ^STOXX50E, SWDA.MI), 73 origini
ogni 10 sedute sulle ultime 750, 1.024 sedute di contesto, quantili 0,1-0,9. Test di Diebold-Mariano
con errori standard di Newey-West; t negativo = modello migliore.

| Modello | Prezzo a 20 sedute contro drift (t) | Quantili a 20 sedute contro simulazione storica (t) | Volatilità a 20 sedute contro EWMA, QLIKE (t) | Copertura dell'intervallo 80%, 5 / 20 sedute | Esito |
| --- | --- | --- | --- | --- | --- |
| Mistral Timestral-28M | −0,79 | −0,38 | −0,31 | 76,6% / 75,8% | Scartato |
| Amazon Chronos-Bolt-base | +2,34 (peggio) | +2,80 (peggio) | +0,29 | 81,5% / 75,2% | Scartato |
| Amazon Chronos-2 | −0,07 | +0,22 | −0,05 | 72,7% / 70,0% | Scartato |
| Simulazione storica (baseline) | — | — | — | 84,4% / 80,0% | — |

- Timestral-28M e Chronos-2 battono la random walk a 20 sedute (t −3,55 e −2,69), ma il guadagno è la
  deriva, che il baseline di drift cattura senza modello.
- A 20 sedute gli intervalli dei modelli sono troppo stretti: coprono il 70-76% dei casi invece dell'80%.
- Una prima passata su Timestral-5M, con 21 quantili, ha dato lo stesso verdetto (t −0,74 contro drift a
  20 sedute); i suoi file non sono conservati perché lo script di allora è confluito in `tsfm_eval.py`.
- Licenze: Timestral ha un tetto di 20 milioni di dollari di ricavi mensili; Chronos è Apache-2.0.

## Cosa queste misure non dimostrano

- Che i dati Yahoo siano corretti o stabili: yfinance usa endpoint non ufficiali.
- Che le statistiche degli eventi abbiano valore previsivo: sono event study con finestre sovrapposte e senza costi.
- Che l'agente risponda bene a domande diverse dalle sette valutate.
- Che la soglia del 15% sugli interessi separi sempre il dato buono da quello lordo: è tarata su 11
  emittenti, e un emittente in difficoltà può pagare davvero più del 15%.
- Che i modelli scartati non servano su altri orizzonti, altre serie o con fine-tuning: è stato misurato
  solo l'uso zero-shot descritto sopra.
