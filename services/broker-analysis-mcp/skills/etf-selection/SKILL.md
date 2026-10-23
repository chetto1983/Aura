---
name: etf-selection
description: Evaluation and comparison of ETFs with the etf_analysis and risk_metrics tools of the broker-analysis server. Use it when the analyst asks about an ETF's cost, TER, tracking difference, tracking error, which ETF replicates an index best, or a comparison of ETFs on the same index.
---

# ETF selection

## Rules
- Costs and replication quality come from `etf_analysis`, risk from `risk_metrics`: do not compute anything.
- Sign convention: **tracking difference = ETF return − benchmark return**, annualised. Negative = the ETF returned less than the index. Always state it: some sites use the opposite sign.
- Report all `warnings`.
- This is internal analysis, not a personalised recommendation to a client.
- Answer in the language the analyst writes in, with that language's number format.

## Choosing the benchmark
1. **Same time zone** as the fund: with different closing times the tracking error is inflated. Measured: SWDA.MI against URTH (New York) 6.5% weekly, against IWDA.AS (Amsterdam) 0.2%. For an ETF listed in Milan use a European ETF on the same index or the local index.
2. **Total return** (dividends included): against a price index the tracking difference is inflated by the dividend yield.
3. The tool converts the benchmark's currency into the fund's with Yahoo's daily exchange rates.

## How to read the results
| Metric | Reading | Caution |
| --- | --- | --- |
| `ter` | Declared annual cost | Excludes spreads and trading costs |
| `tracking_difference` | Actual cost of the replication | It can exceed the TER (hidden costs) or be better (securities lending, optimised withholding taxes) |
| `tracking_error` | Fidelity of the replication, on weekly returns | Valid only with a benchmark in the same time zone |
| `aligned_days` | Days compared | Under one year the tracking difference is not reliable |

## Other criteria to flag (not covered by the tool)
- Domicile (Ireland or Luxembourg), replication method (physical or synthetic), income policy (accumulating or distributing), fund size, trading spread on Borsa Italiana.
- Italian taxation: gains on harmonised ETFs are capital income (26%) and do not offset earlier capital losses.

## Answer format
1. One sentence: which ETF really costs less (tracking difference) and replicates better (tracking error).
2. Table per ETF: TER, tracking difference, tracking error, window.
3. Warnings on the benchmark and the data, and criteria not covered that should be checked on the KID.
