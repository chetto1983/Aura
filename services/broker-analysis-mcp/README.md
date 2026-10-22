# broker-analysis-mcp

Prototype of a **read-only MCP server for financial analysis** for a broker's analysts, with
Claude skills that teach the agent how to read every number. Data from Yahoo Finance through
`yfinance`.

It lives in Aura as a standalone uv project. It is not registered as an Aura MCP recipe: an operator
who wants it adds it as a custom server.

> **Prototype only.** yfinance reads Yahoo's unofficial endpoints, is meant for personal and
> research use, and can break without notice. Production needs a licensed data source
> (Bloomberg or LSEG through their MCP servers, or a vendor with redistribution rights). Nothing
> here places orders or gives personalised advice.

## Tools

| Tool | What it returns |
| --- | --- |
| `risk_metrics` | CAGR, close-to-close and range volatility (Garman-Klass, Parkinson, Rogers-Satchell), Sharpe with standard error and t statistic, Probabilistic Sharpe Ratio, Sortino, Omega, max drawdown, CDaR, Calmar, historical and Cornish-Fisher VaR, expected shortfall, beta and correlation |
| `technical_indicators` | Snapshot of SMAs, RSI, MACD, Bollinger, ATR, 52-week range, all with adjustable parameters |
| `fundamental_ratios` / `peer_comparison` | Multiples, DuPont (ROE, ROA, margins, turnover, equity multiplier), ROIC, working-capital cycle, liquidity, leverage and coverage, payout, revenue CAGR; CAPM cost of equity, WACC, EVA and unlevered beta when the analyst gives the risk-free rate and the equity risk premium; currency conversion between quote and statements |
| `etf_analysis` | TER, tracking difference and tracking error against a benchmark converted to the fund's currency |
| `list_indicator_catalog` | The parametric indicators (typed parameters with defaults and bounds), expression functions and syntax |
| `custom_analysis` | User-defined indicators, derived series, conditions and levels on one ticker; condition events with forward returns against all bars; PNG chart plus a QuantDinger-style JSON spec (plots, signals, layers) |
| `create_hook` / `list_hooks` / `run_hooks` / `delete_hook` | Saved conditions that deliver a JSON event (file or allow-listed webhook) when they fire on a new bar |

Every metric carries its unit and the key of the formula it came from (`references.py`, with
arXiv sources where one exists), plus the data window, source and warnings.

## Parametric analysis

Indicators are data, not code: `{"id": "bb", "fn": "bollinger", "params": {"n": 30, "k": 2.5}}`.
Derived series and conditions are expressions over the series by id:

```
close / bb.lower - 1
rsi14 < 30 and close > sma200
crosses_above(ema20, ema50)
volume > 2 * mean(volume, 20)
```

Expressions are parsed with `ast` and evaluated over a whitelist of node types and functions;
nothing reaches `eval`. Conditions use edge semantics (an event when the rule becomes true), as
QuantDinger's signal monitoring does.

## Hooks

```
BROKER_ANALYSIS_HOME=~/.broker-analysis          # hooks.json, alerts/*.jsonl, charts/
BROKER_ANALYSIS_WEBHOOK_ALLOW=https://hooks.example.com/   # webhooks are off without it
*/30 9-18 * * 1-5  broker-analysis-hooks          # evaluate saved hooks during market hours
```

## Skills

`skills/` holds five skills for Claude: `analisi-rischio`, `analisi-fondamentale`,
`analisi-tecnica`, `selezione-etf`, `analisi-parametrica`. Copy them to `.claude/skills/`.

## Run

```
uv sync
uv run broker-analysis-mcp                      # stdio MCP server
uv run pytest                                   # offline tests
uv run pytest -m live -o addopts=""             # MCP end to end on live Yahoo data
uv run python e2e/agent_e2e.py                  # a real agent answers analyst questions, scored (needs the claude CLI)
bash ../../scripts/ruff.sh check .              # Aura's ruff rules, through [tool.ruff] extend
```

`e2e/tsfm_eval.py` reruns the forecasting test; it needs an environment with torch, `timestral` and
`chronos-forecasting`, which this project does not install.

## Measured, not assumed

`docs/VERIFICA.md` records what was measured, including the formulas checked against
independent implementations and the forecasting models (Mistral Timestral, Amazon Chronos-Bolt and
Chronos-2) that were dropped for not beating naive baselines.
