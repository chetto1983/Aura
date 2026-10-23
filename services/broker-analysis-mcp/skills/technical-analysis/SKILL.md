---
name: technical-analysis
description: Technical price analysis with the technical_indicators tool of the broker-analysis server. Use it when the analyst asks about trend, momentum, moving averages, RSI, MACD, Bollinger bands, ATR, support and resistance, 52-week highs and lows, overbought or oversold.
---

# Technical analysis

## Rules
- Every indicator comes from `technical_indicators` with a `period` of at least 1y (better 2y), so that the 200-session average is defined.
- Quote the date of the last close (`window.end`) and the currency.
- For crypto assets every bar is a calendar day, weekends included: the 200 average covers 200 days, not 200 sessions. 52-week highs and lows still cover a calendar year.
- Every number comes from the tool: do not compute distances, ATR multiples or levels. Report a distance as the tool gives it, from the price's point of view ("price +18.5% above the SMA200"), without inverting it.
- Do not propose stops, targets or position sizes: those are trading decisions, and this is an analysis tool.
- Technical analysis describes the price, not the value: do not present it as a certain forecast.
- This is internal analysis, not a personalised recommendation to a client.
- Reply in the language of the question: English for a question in English, Italian for one in Italian. Use that language's number format (22.1% in English, 22,1% in Italian).

## How to read the results
| Indicator | Reading | Caution |
| --- | --- | --- |
| `price_vs_sma200` | Positive = long-term uptrend | Slow signal, late on reversals |
| `sma50` against `sma200` | 50 above 200 = established uptrend | Crossovers give many false signals in a sideways market |
| `rsi14` (Wilder) | Above 70 overbought, below 30 oversold | In a strong trend it stays extreme for a long time |
| `macd_histogram` | Positive and rising = upward momentum | The sign and the direction matter, not the level |
| Bollinger bands | Price near the upper or lower band = 20-session statistical extreme | They do not indicate the direction |
| `atr14`, `atr14_pct_of_close` | Average daily range, in currency and in % of the price | It describes volatility: do not turn it into stops or position sizes |
| `high_52w` / `low_52w` and `price_vs_*_52w` | Recent levels and the price's distance from them in % | They are support and resistance only if the price has reacted to them |

## Combining signals
- Trend (averages) before momentum (RSI, MACD): a low RSI in a downtrend is not a buy signal.
- Signals that agree strengthen the reading; signals that disagree must be reported as such.

## Answer format
1. One sentence: trend (up, down, sideways) and momentum, with the two indicators that show it.
2. Table of the indicators with values and units.
3. Relevant levels (highs, lows, bands) with the % distances given by the tool, and volatility (`atr14_pct_of_close`).
4. Final line: date, currency, source, warnings.
