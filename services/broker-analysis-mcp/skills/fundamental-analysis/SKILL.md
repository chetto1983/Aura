---
name: fundamental-analysis
description: Fundamental analysis and relative valuation of listed companies with the fundamental_ratios and peer_comparison tools of the broker-analysis server. Use it for multiples (P/E, EV/EBITDA, EV/EBIT, P/B, P/S), profitability and the DuPont breakdown (ROE, ROA, ROIC, margins), the working capital cycle (receivable, inventory and payable days), liquidity (current, quick, cash ratio), leverage and interest coverage, cost of capital (CAPM, WACC, EVA), dividends, revenue growth, balance-sheet scores (Piotroski F-score, Altman Z, Beneish M-score) and to compare a company with its peers.
---

# Fundamental analysis

## Rules
- Every ratio comes from the tools: do not recompute it and do not estimate it.
- The ratios are based on the **latest annual report** (`fiscal_year_end`); `yahoo_trailing_pe_ttm` is Yahoo's figure over the last 12 months. Always say which one you are quoting.
- A `null` value means "not meaningful" (losses, negative EBITDA, missing data): say so, do not replace it.
- Report the `warnings`, in particular the currency conversion between price and financial statements.
- This is internal analysis, not a personalised recommendation to a client.
- Answer in the language the analyst writes in, with that language's number format.

## Cost of capital
- WACC, cost of equity and EVA are computed only if the analyst gives `risk_free_annual` and `equity_risk_premium` (decimals). Do not choose them yourself: ask for them, or propose explicit values (for instance the 10-year BTP yield and a stated risk premium) and have them confirmed.
- Beta is Yahoo's unless the analyst gives `beta`; `parameters.beta_source` says which one was used.
- A positive EVA means the company earns more than its cost of capital. The WACC uses book debt as a proxy for market debt.
- If a warning says that interest is an implausible share of average debt, the reported figure includes gross finance costs (FX, derivatives): do not present interest coverage, cost of debt, WACC and EVA as reliable, and quote the warning.

## Balance-sheet scores
The tool computes them on the last two fiscal years and puts them in `metrics`, with their components in `scores`. Banks and insurers are excluded.

| Score | Reading | Caution |
| --- | --- | --- |
| `piotroski_f_score` (0-9) | Financial strength: nine signals on profitability, leverage and liquidity, efficiency. In the paper 8-9 is the strong group, 0-1 the weak one | If it is `null` a signal is missing: quote `scores.piotroski.partial_score`, `signals_available` and `missing`, and do not present it as a mark out of 9 |
| `altman_z` | Distress risk for listed industrial and manufacturing companies: below 1.81 distress zone, between 1.81 and 2.99 grey zone, above 2.99 safe zone (`scores.altman.z_zone`) | X4 uses today's market capitalisation, so it follows the price. For services it is outside the range it was estimated on |
| `altman_z_double_prime` | Variant for non-manufacturing companies and emerging markets, with book equity | The paper consulted gives no thresholds for this variant: compare it with peers, not with a threshold |
| `beneish_m` | Signal of possible earnings manipulation: above −1.78 the model flags it (`scores.beneish.above_cutoff`) | It is a warning bell, not an accusation: in the paper it misses 26% of manipulators and flags 13.8% of non-manipulators. Read the variables driving it (`scores.beneish.variables`) and the neutralised ones (`neutralised`) |

## How to choose peers
- Same sector and business model, comparable size, 3-6 companies. Examples: ENI.MI, TTE.PA, SHEL.L, BP.L, REP.MC for European integrated energy; ISP.MI, UCG.MI, BNP.PA, SAN.MC for banks.
- Report the tickers in `errors` instead of ignoring them.

## Which multiple for which sector
| Sector | Core multiples | To avoid |
| --- | --- | --- |
| Banks and insurers | P/B, ROE, P/E | EV/EBITDA, net debt/EBITDA: debt is their raw material |
| Industrials, energy, consumer | EV/EBITDA, P/E, FCF yield, net debt/EBITDA | — |
| Growth and technology | EV/EBITDA, revenue growth, margins, ROIC | P/E if earnings are negative or depressed |
| Utilities and real estate | Net debt/EBITDA, payout, interest coverage | P/B without reading the assets |

## How to read the results
- **DuPont**: ROE = net margin × asset turnover × equity multiplier (average balances over the year). It shows where ROE comes from: margins, efficiency or leverage.
- **Cash cycle**: receivable days + inventory days − payable days. A lengthening cycle absorbs cash. Payable days are computed on cost of goods sold.
- **Liquidity**: current ratio, quick ratio and cash ratio; for banks and insurers they are `null` by construction and must not be replaced.
- **Valuation**: compare with the peer median, not with absolute thresholds. A discount needs an explanation (growth, risk, leverage).
- **Quality**: ROIC above the cost of capital creates value; a high ROE with high leverage is worth less.
- **Solidity**: net debt/EBITDA above 3 and interest coverage below 4 should be flagged for non-financial sectors.
- **Cash**: FCF yield and payout show whether the dividend is covered; a payout above 100% does not last.
- The annual and the TTM P/E can differ a lot after an exceptional year: explain the difference.

## Answer format
1. One sentence of judgement: expensive, in line or at a discount to peers, with the two multiples that show it.
2. Table: one row per company, columns for the sector's multiples, units in the header.
3. Quality and solidity in two or three points.
4. Final line: reference fiscal year, reporting currency, source, warnings.
