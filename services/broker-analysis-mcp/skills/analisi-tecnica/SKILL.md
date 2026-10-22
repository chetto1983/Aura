---
name: analisi-tecnica
description: Analisi tecnica di prezzo con il tool technical_indicators del server broker-analysis. Usala quando l'analista chiede trend, momentum, medie mobili, RSI, MACD, bande di Bollinger, ATR, supporti e resistenze, massimi e minimi a 52 settimane, ipercomprato o ipervenduto.
---

# Analisi tecnica

## Regole
- Ogni indicatore viene da `technical_indicators` con `period` di almeno 1y (meglio 2y), così la media a 200 sedute è definita.
- Cita la data dell'ultima chiusura (`window.end`) e la valuta.
- Ogni numero viene dal tool: non calcolare distanze, multipli dell'ATR o livelli. Una distanza si riporta come la dà il tool, dal punto di vista del prezzo ("prezzo +18,5% sopra la SMA200"), senza invertirla.
- Non proporre stop, target o dimensioni di posizione: sono decisioni di trading, e questo è uno strumento di analisi.
- L'analisi tecnica descrive il prezzo, non il valore: non presentarla come previsione certa.
- È analisi interna, non una raccomandazione personalizzata a un cliente.

## Come leggere i risultati
| Indicatore | Lettura | Attenzione |
| --- | --- | --- |
| `price_vs_sma200` | Positivo = trend di lungo periodo rialzista | Segnale lento, in ritardo sulle inversioni |
| `sma50` contro `sma200` | 50 sopra 200 = trend rialzista consolidato | Gli incroci danno molti falsi segnali in laterale |
| `rsi14` (Wilder) | Sopra 70 ipercomprato, sotto 30 ipervenduto | In un trend forte resta estremo a lungo |
| `macd_histogram` | Positivo e in crescita = momentum rialzista | Conta il segno e la direzione, non il livello |
| Bande di Bollinger | Prezzo vicino alla banda alta o bassa = estremo statistico a 20 sedute | Non indicano la direzione |
| `atr14`, `atr14_pct_of_close` | Escursione media giornaliera, in valuta e in % del prezzo | Descrive la volatilità: non trasformarla in stop o in dimensioni di posizione |
| `high_52w` / `low_52w` e `price_vs_*_52w` | Livelli recenti e distanza del prezzo in % | Sono supporti e resistenze solo se il prezzo vi ha reagito |

## Combinare i segnali
- Trend (medie) prima del momentum (RSI, MACD): un RSI basso in un trend ribassista non è un segnale di acquisto.
- Segnali concordi rafforzano la lettura; segnali discordi vanno riportati come tali.

## Formato della risposta
1. Una frase: trend (rialzista, ribassista, laterale) e momentum, con i due indicatori che lo mostrano.
2. Tabella degli indicatori con valori e unità.
3. Livelli rilevanti (massimi, minimi, bande) con le distanze in % date dal tool, e volatilità (`atr14_pct_of_close`).
4. Riga finale: data, valuta, fonte, avvisi.
