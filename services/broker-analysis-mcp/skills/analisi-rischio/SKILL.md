---
name: analisi-rischio
description: Analisi di rischio e rendimento di un titolo, indice, ETF o portafoglio con il tool risk_metrics del server broker-analysis. Usala quando l'analista chiede volatilità, drawdown, Sharpe, Sortino, VaR, expected shortfall, beta, correlazione, "quanto è rischioso", "quanto può perdere", o un confronto di rischio tra strumenti.
---

# Analisi di rischio

## Regole
- Ogni numero viene da `risk_metrics`: non calcolare nulla a mente, nemmeno una media.
- Cita sempre finestra dati (`window.start`–`window.end`), valuta e fonte (Yahoo Finance, prototipo).
- Riporta i `warnings` del tool. Se un valore è `null`, dì che non è disponibile e perché.
- È analisi interna, non una raccomandazione personalizzata a un cliente.

## Come chiamare il tool
- `period`: 3y di default; 5y per strumenti volatili, 1y solo se l'analista vuole il regime recente.
- `benchmark`: l'indice di riferimento del mercato del titolo (FTSEMIB.MI per Milano, ^STOXX50E per l'area euro, ^GSPC per gli USA). Serve per beta e correlazione; il tool lo converte nella valuta del titolo.
- `risk_free_annual`: tasso privo di rischio in decimali (€STR o BOT a 12 mesi per l'euro, T-bill per il dollaro). Se l'analista non lo indica usa 0 e dichiaralo.

## Come leggere i risultati
| Metrica | Lettura | Attenzione |
| --- | --- | --- |
| `volatility_close_to_close` | Volatilità annua dai prezzi di chiusura | Include i salti di apertura |
| `volatility_garman_klass` | Stima più efficiente (circa 7 volte la close-to-close, arXiv:2312.01426) | Ignora i salti notturni: se è molto sotto la close-to-close, il rischio sta nei gap |
| `sharpe`, `sharpe_standard_error`, `sharpe_t_stat` | Rendimento per unità di rischio e distanza da zero in errori standard | Con `sharpe_t_stat` sotto 2 lo Sharpe non è distinguibile da zero |
| `probabilistic_sharpe_vs_0` | Probabilità che lo Sharpe vero sia positivo, corretta per asimmetria e code (Bailey & López de Prado) | Sotto 0,95 non dire che lo strumento "batte" il risk-free |
| `sortino` | Come lo Sharpe, ma penalizza solo i ribassi | Utile con rendimenti asimmetrici |
| `omega` | Guadagni su perdite rispetto alla soglia | Sopra 1 i guadagni pesano più delle perdite |
| `max_drawdown` | Peggior perdita da un massimo | Dipende dalla finestra: dichiarala |
| `cdar` | Media del 5% peggiore dei drawdown (Chekhlov, Uryasev, Zabarankin) | Più stabile del solo massimo |
| `var_historical_1d` / `expected_shortfall_1d` | Perdita giornaliera superata nel 5% dei giorni / media in quei giorni | L'ES è sempre maggiore del VaR: riportali insieme |
| `var_cornish_fisher_1d` | VaR corretto per asimmetria e curtosi | Al 95% le code grasse possono *abbassarlo*; il segno si inverte oltre il 95,8% di confidenza |
| `beta`, `correlation` | Sensibilità e legame col benchmark | Beta basso con correlazione bassa significa rischio specifico, non un titolo difensivo |

## Formato della risposta
1. Una frase con il giudizio principale e i suoi due numeri chiave.
2. Una tabella delle metriche richieste, con unità.
3. Due o tre osservazioni: confronto fra stimatori di volatilità, significatività dello Sharpe, drawdown.
4. Riga finale: finestra, valuta, fonte, avvisi.
