---
name: analisi-parametrica
description: Grafici e analisi con indicatori, parametri e condizioni scelti dall'analista, e hook che avvisano quando una condizione si verifica, con i tool custom_analysis, list_indicator_catalog, create_hook, list_hooks, run_hooks e delete_hook del server broker-analysis. Usala quando l'analista chiede un grafico, indicatori con parametri propri (EMA 20, RSI 9, Bollinger 30/2,5), una regola come "segnami quando l'RSI scende sotto 30 sopra la media a 200", quante volte è successo e cosa è successo dopo, oppure un avviso o alert su una condizione.
---

# Analisi parametrica, grafici e hook

## Regole
- Ogni numero viene dai tool: valori degli indicatori, conteggi, rendimenti successivi. Non calcolare nulla, nemmeno una distanza percentuale: se serve, chiedila come serie derivata (`series`).
- Cita finestra dati, valuta e fonte. Riporta gli avvisi delle condizioni.
- È analisi, non trading: gli hook avvisano, non eseguono ordini. Non presentare un evento come raccomandazione.

## Flusso
1. Se non conosci nomi o parametri, chiama `list_indicator_catalog`.
2. Traduci la richiesta in `indicators`, `series`, `conditions` e `levels`, poi chiama `custom_analysis` (grafico incluso di default).
3. Per un avviso ricorrente usa `create_hook` con la stessa condizione e gli stessi indicatori.

## Tradurre la richiesta
| L'analista dice | Specifica |
| --- | --- |
| "EMA a 20 e 50" | `{"id": "ema20", "fn": "ema", "params": {"n": 20}}`, `{"id": "ema50", "fn": "ema", "params": {"n": 50}}` |
| "Bollinger a 30 con 2,5 deviazioni" | `{"id": "bb", "fn": "bollinger", "params": {"n": 30, "k": 2.5}}`, letta come `bb.upper`, `bb.middle`, `bb.lower` |
| "RSI a 9 giorni" | `{"id": "rsi9", "fn": "rsi", "params": {"n": 9}}` |
| "media dell'RSI" | `{"id": "rsi_ma", "fn": "sma", "params": {"source": "rsi9", "n": 5}}` (la sorgente è una serie definita prima) |
| "distanza dalla banda bassa in %" | serie `{"id": "dist", "expr": "(close / bb.lower - 1) * 100"}` |
| "quando l'RSI scende sotto 30 sopra la media a 200" | condizione `rsi9 < 30 and close > sma200` |
| "incrocio rialzista delle medie" | `crosses_above(ema20, ema50)` |
| "nuovo massimo a 20 giorni" | `close >= highest(high, 20)` |
| "volume doppio della media" | `volume > 2 * mean(volume, 20)` |

- Gli id sono minuscoli, senza spazi, e indicano il parametro (`rsi9`, `sma200`).
- `mode: "edge"` (default) conta l'evento solo quando la condizione *diventa* vera; `"state"` conta ogni giorno in cui è vera. Per contare "quante volte è successo" usa edge.
- `marker: "down"` per condizioni ribassiste: il segno sul grafico è un triangolo verso il basso.
- Per le medie lunghe serve un `period` sufficiente: 2y o più per la SMA200.

## Leggere il risultato di una condizione
- `active_on_last_bar` e `fired_on_last_bar`: stato oggi.
- `events`, `last_event`, `recent_events`: storia degli eventi nella finestra.
- `forward_returns`: rendimento medio, mediano e % di casi positivi dopo 5 e 20 sedute dagli eventi (`after_events`), confrontato con tutte le sedute (`all_bars`). La condizione è interessante solo se si discosta dalla baseline. Con `difference_t_stat` sotto 2 in valore assoluto la differenza non è distinguibile dal caso; con meno di 20 eventi è aneddotica. Dillo sempre.

## Hook
- `action`: `{"type": "file", "target": "nome"}` scrive righe JSON sul server; `{"type": "webhook", "target": "https://..."}` funziona solo verso indirizzi autorizzati dall'operatore. Se il tool rifiuta il webhook, proponi l'azione file e spiega che l'abilitazione spetta all'operatore.
- L'hook non rinvia gli eventi passati: scatta sui nuovi. Va eseguito periodicamente (`run_hooks` o il comando `broker-analysis-hooks` in cron). Riferisci all'analista l'id creato e la data dell'ultimo evento storico.

## Formato della risposta
1. Una frase con il risultato principale (stato attuale della condizione, numero di eventi, cosa è successo dopo, con i numeri).
2. Tabella dei valori finali degli indicatori.
3. Per ogni condizione: eventi, ultimo evento, rendimenti successivi contro la baseline, avvisi.
4. Il grafico prodotto e la riga finale con finestra, valuta e fonte.
