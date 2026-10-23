---
name: risk-analysis
description: Risk and return analysis of a stock, index or ETF with the risk_metrics tool of the broker-analysis server. Use it when the analyst asks about volatility, drawdown, Sharpe, Sortino, VaR, expected shortfall, beta (including the Bloomberg-style adjusted beta), correlation, Jensen's alpha, Treynor, M², information ratio, upside/downside capture, "how risky is it", "how much can it lose", or a risk comparison between instruments or against a benchmark.
---

# Risk analysis

## Rules
- Every number comes from `risk_metrics`: do not compute anything yourself, not even an average.
- Always state the data window (`window.start`–`window.end`), the currency and the source (Yahoo Finance, prototype).
- Report the tool's `warnings`. If a value is `null`, say that it is not available and why.
- This is internal analysis, not a personalised recommendation to a client.
- Answer in the language the analyst writes in, with that language's number format.

## How to call the tool
- `period`: 3y by default; 5y for volatile instruments, 1y only if the analyst wants the recent regime.
- `benchmark`: the reference index of the stock's market (FTSEMIB.MI for Milan, ^STOXX50E for the euro area, ^GSPC for the US). It is needed for beta, correlation, alpha, Treynor, M², information ratio and capture; the tool converts it into the stock's currency.
- The Bloomberg-style beta needs a `period` of at least 2y: with fewer weeks the tool warns about it.
- Crypto assets (BTC-USD, ETH-USD) trade every day, so the tool annualises over 365 days instead of 252 sessions (`parameters.periods_per_year`); say so. Against an equity index the relative metrics use only the days on which both trade.
- `risk_free_annual`: risk-free rate as a decimal (€STR or the 12-month BOT for the euro, T-bills for the dollar). If the analyst does not give one, use 0 and say so.

## How to read the results
| Metric | Reading | Caution |
| --- | --- | --- |
| `volatility_close_to_close` | Annual volatility from closing prices | Includes opening gaps |
| `volatility_garman_klass` | More efficient estimate (about 7 times close-to-close, arXiv:2312.01426) | Ignores overnight jumps: if it is well below close-to-close, the risk is in the gaps |
| `sharpe`, `sharpe_standard_error`, `sharpe_t_stat` | Return per unit of risk and its distance from zero in standard errors | With `sharpe_t_stat` below 2 the Sharpe ratio cannot be told apart from zero |
| `probabilistic_sharpe_vs_0` | Probability that the true Sharpe ratio is positive, corrected for skewness and tails (Bailey & López de Prado) | Below 0.95 do not say that the instrument "beats" the risk-free rate |
| `sortino` | Like the Sharpe ratio, but penalises only the downside | Useful with asymmetric returns |
| `omega` | Gains over losses relative to the threshold | Above 1 gains outweigh losses |
| `max_drawdown` | Worst loss from a peak | Depends on the window: state it |
| `cdar` | Mean of the worst 5% of drawdowns (Chekhlov, Uryasev, Zabarankin) | More stable than the maximum alone |
| `var_historical_1d` / `expected_shortfall_1d` | Daily loss exceeded on 5% of days / mean loss on those days | ES is always larger than VaR: report them together |
| `var_cornish_fisher_1d` | VaR corrected for skewness and kurtosis | At 95% fat tails can *lower* it; the sign flips above 95.8% confidence |
| `beta`, `correlation` | Sensitivity to and link with the benchmark, on daily returns | A low beta with a low correlation means specific risk, not a defensive stock |
| `beta_weekly_2y`, `beta_adjusted` | Beta on two years of weekly returns, and the adjusted beta 0.67 × raw + 0.33, as on Bloomberg's BETA screen | This is the number to compare with the terminal. With a benchmark from a market that closes at another time, prefer it to the daily beta |
| `jensen_alpha` | Annual return beyond what beta explains (CAPM) | The tool does not test its significance: do not present it as manager skill |
| `treynor` | Excess return per unit of beta | `null` with a negative or zero beta; with a low beta it becomes huge and says little: say so in that case |
| `m_squared`, `m_squared_vs_benchmark` | Return the instrument would have had at the benchmark's volatility, and the difference from the benchmark | The like-for-like risk comparison, in percentage points |
| `information_ratio`, `tracking_error` | Active return per unit of deviation from the benchmark, on weekly returns | Meaningful for funds and active mandates more than for a single stock |
| `up_capture`, `down_capture` (with `up_months`, `down_months`) | In %: how much of the benchmark's positive and negative months the instrument captures | Upside above 100 amplifies rallies; downside below 100 loses less in declines. Always quote the number of months: with few months the estimate is fragile |

## Answer format
1. One sentence with the main judgement and its two key numbers.
2. A table of the requested metrics, with units.
3. Two or three observations: comparison of volatility estimators, significance of the Sharpe ratio, drawdown.
4. Final line: window, currency, source, warnings.
