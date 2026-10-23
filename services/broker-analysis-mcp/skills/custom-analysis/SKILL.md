---
name: custom-analysis
description: Charts and analyses with indicators, parameters and conditions chosen by the analyst, and hooks that alert when a condition occurs, with the custom_analysis, list_indicator_catalog, create_hook, list_hooks, run_hooks and delete_hook tools of the broker-analysis server. Use it when the analyst asks for a chart, indicators with their own parameters (EMA 20, RSI 9, Bollinger 30/2.5), a rule such as "flag when RSI drops below 30 above the 200-day average", how many times it happened and what happened next, or an alert on a condition.
---

# Custom analysis, charts and hooks

## Rules
- Every number comes from the tools: indicator values, counts, forward returns. Do not compute anything, not even a percentage distance: if you need one, ask for it as a derived series (`series`).
- Quote the data window, currency and source. Report the conditions' warnings.
- This is analysis, not trading: hooks alert, they do not place orders. Do not present an event as a recommendation.
- Answer in the language the analyst writes in, with that language's number format.

## Flow
1. If you do not know names or parameters, call `list_indicator_catalog`.
2. Translate the request into `indicators`, `series`, `conditions` and `levels`, then call `custom_analysis` (chart included by default).
3. For a recurring alert use `create_hook` with the same condition and the same indicators.

## Translating the request
| The analyst says | Specification |
| --- | --- |
| "EMA 20 and 50" | `{"id": "ema20", "fn": "ema", "params": {"n": 20}}`, `{"id": "ema50", "fn": "ema", "params": {"n": 50}}` |
| "Bollinger 30 with 2.5 deviations" | `{"id": "bb", "fn": "bollinger", "params": {"n": 30, "k": 2.5}}`, read as `bb.upper`, `bb.middle`, `bb.lower` |
| "9-day RSI" | `{"id": "rsi9", "fn": "rsi", "params": {"n": 9}}` |
| "average of the RSI" | `{"id": "rsi_ma", "fn": "sma", "params": {"source": "rsi9", "n": 5}}` (the source is a series defined earlier) |
| "distance from the lower band in %" | series `{"id": "dist", "expr": "(close / bb.lower - 1) * 100"}` |
| "when RSI drops below 30 above the 200-day average" | condition `rsi9 < 30 and close > sma200` |
| "bullish moving-average crossover" | `crosses_above(ema20, ema50)` |
| "new 20-day high" | `close >= highest(high, 20)` |
| "volume twice its average" | `volume > 2 * mean(volume, 20)` |

- Ids are lowercase, without spaces, and name the parameter (`rsi9`, `sma200`).
- `mode: "edge"` (default) counts the event only when the condition *becomes* true; `"state"` counts every day on which it is true. To count "how many times it happened" use edge.
- `marker: "down"` for bearish conditions: the mark on the chart is a downward triangle.
- Long averages need a sufficient `period`: 2y or more for the SMA200.

## Reading a condition's result
- `active_on_last_bar` and `fired_on_last_bar`: the state today.
- `events`, `last_event`, `recent_events`: the history of events in the window.
- `forward_returns`: mean, median and % of positive cases 5 and 20 sessions after the events (`after_events`), compared with all sessions (`all_bars`). The condition is interesting only if it departs from the baseline. With `difference_t_stat` below 2 in absolute value the difference cannot be told apart from chance; with fewer than 20 events it is anecdotal. Always say so.

## Hooks
- `action`: `{"type": "file", "target": "name"}` writes JSON lines on the server; `{"type": "webhook", "target": "https://..."}` works only towards addresses the operator has allowed. If the tool refuses the webhook, propose the file action and explain that enabling it is up to the operator.
- A hook does not resend past events: it fires on new ones. It has to be run periodically (`run_hooks` or the `broker-analysis-hooks` command in cron). Tell the analyst the id created and the date of the last historical event.

## Answer format
1. One sentence with the main result (current state of the condition, number of events, what happened next, with the numbers).
2. Table of the indicators' final values.
3. For each condition: events, last event, forward returns against the baseline, warnings.
4. The chart produced and the final line with window, currency and source.
