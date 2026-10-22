---
name: analisi-rischio
description: Analisi di rischio e rendimento di un titolo, indice, ETF o portafoglio con il tool risk_metrics del server broker-analysis. Usala quando l'analista chiede volatilità, drawdown, Sharpe, Sortino, VaR, expected shortfall, beta (anche corretto come su Bloomberg), correlazione, alfa di Jensen, Treynor, M², information ratio, upside/downside capture, "quanto è rischioso", "quanto può perdere", o un confronto di rischio tra strumenti o contro un benchmark.
---

# Analisi di rischio

## Regole
- Ogni numero viene da `risk_metrics`: non calcolare nulla a mente, nemmeno una media.
- Cita sempre finestra dati (`window.start`–`window.end`), valuta e fonte (Yahoo Finance, prototipo).
- Riporta i `warnings` del tool. Se un valore è `null`, dì che non è disponibile e perché.
- È analisi interna, non una raccomandazione personalizzata a un cliente.

## Come chiamare il tool
- `period`: 3y di default; 5y per strumenti volatili, 1y solo se l'analista vuole il regime recente.
- `benchmark`: l'indice di riferimento del mercato del titolo (FTSEMIB.MI per Milano, ^STOXX50E per l'area euro, ^GSPC per gli USA). Serve per beta, correlazione, alfa, Treynor, M², information ratio e capture; il tool lo converte nella valuta del titolo.
- Per il beta in stile Bloomberg serve `period` di almeno 2y: con meno settimane il tool lo avvisa.
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
| `beta`, `correlation` | Sensibilità e legame col benchmark su rendimenti giornalieri | Beta basso con correlazione bassa significa rischio specifico, non un titolo difensivo |
| `beta_weekly_2y`, `beta_adjusted` | Beta su due anni di rendimenti settimanali e beta corretto 0,67 × grezzo + 0,33, come la schermata BETA di Bloomberg | È il numero da confrontare col terminale. Con benchmark di un mercato che chiude a un'altra ora preferiscilo al beta giornaliero |
| `jensen_alpha` | Rendimento annuo oltre quello spiegato dal beta (CAPM) | Il tool non ne testa la significatività: non presentarlo come abilità del gestore |
| `treynor` | Rendimento in eccesso per unità di beta | `null` con beta negativo o nullo; con un beta basso diventa enorme e dice poco: in quel caso dillo |
| `m_squared`, `m_squared_vs_benchmark` | Rendimento che lo strumento avrebbe avuto con la volatilità del benchmark, e la differenza col benchmark | È il confronto a parità di rischio, in punti percentuali |
| `information_ratio`, `tracking_error` | Rendimento attivo per unità di scarto dal benchmark, su rendimenti settimanali | Ha senso per fondi e gestioni attive più che per un singolo titolo |
| `up_capture`, `down_capture` (con `up_months`, `down_months`) | In %: quanto lo strumento cattura dei mesi positivi e negativi del benchmark | Upside sopra 100 amplifica i rialzi; downside sotto 100 perde meno nei ribassi. Cita sempre il numero di mesi: con pochi mesi la stima è fragile |

## Formato della risposta
1. Una frase con il giudizio principale e i suoi due numeri chiave.
2. Una tabella delle metriche richieste, con unità.
3. Due o tre osservazioni: confronto fra stimatori di volatilità, significatività dello Sharpe, drawdown.
4. Riga finale: finestra, valuta, fonte, avvisi.
