"""Read-only MCP server for financial analysis on Yahoo Finance data (prototype).

Every tool returns computed numbers with unit, formula reference, data window, source and
warnings. The model interprets them; it never computes a metric itself.
"""

import json
from collections.abc import Callable
from typing import Any

from mcp.server.mcpserver import Image, MCPServer
from mcp.server.mcpserver.exceptions import ToolError
from mcp.types import ToolAnnotations

from . import analysis, hooks, workbench
from .catalog import SpecError
from .data import DataError
from .models import ConditionSpec, HookAction, IndicatorSpec, LevelSpec, SeriesSpec

INSTRUCTIONS = (
    "Read-only financial analysis on Yahoo Finance data (prototype, unofficial source). "
    "Use these tools for every number you report; quote each value with its unit, data window "
    "and source; never compute a metric yourself. Yahoo tickers: Milan .MI, London .L, "
    "Xetra .DE, Paris .PA, Amsterdam .AS; indices start with ^ (^GSPC, ^SP500TR, FTSEMIB.MI)."
)

server = MCPServer("broker-analysis", instructions=INSTRUCTIONS)
READ_ONLY = ToolAnnotations(readOnlyHint=True, openWorldHint=True)
WRITES_HOOKS = ToolAnnotations(readOnlyHint=False, destructiveHint=False, openWorldHint=True)


def _run(fn: Callable[..., Any], *args: Any) -> Any:
    try:
        return fn(*args)
    except (DataError, SpecError) as err:
        raise ToolError(str(err)) from err


@server.tool(annotations=READ_ONLY)
def risk_metrics(
    ticker: str,
    period: str = "3y",
    benchmark: str | None = None,
    risk_free_annual: float = 0.0,
    confidence: float = 0.95,
) -> dict:
    """Risk and performance of one security or index from daily total-return prices.

    Returns CAGR, close-to-close and range-based volatility (Garman-Klass, Parkinson,
    Rogers-Satchell), Sharpe with its standard error, Probabilistic Sharpe Ratio, Sortino,
    Omega, maximum drawdown, CDaR, Calmar, historical and Cornish-Fisher one-day VaR and
    expected shortfall. With a benchmark ticker (converted to the security's currency) also daily
    beta and correlation, Bloomberg-style beta (two years of weekly returns, adjusted 0.67 raw +
    0.33), Jensen's alpha, Treynor, M2, information ratio and tracking error, and Morningstar-style
    up and down capture on monthly returns. period: 1y, 2y, 3y, 5y, 10y or max. risk_free_annual
    as a decimal (0.03 = 3%).
    """
    return _run(analysis.risk_report, ticker, period, benchmark, risk_free_annual, confidence)


@server.tool(annotations=READ_ONLY)
def technical_indicators(
    ticker: str,
    period: str = "1y",
    sma_fast: int = 50,
    sma_slow: int = 200,
    rsi_period: int = 14,
    macd_fast: int = 12,
    macd_slow: int = 26,
    macd_signal: int = 9,
    bollinger_n: int = 20,
    bollinger_k: float = 2.0,
    atr_period: int = 14,
) -> dict:
    """Snapshot of standard technical indicators with adjustable parameters: close, two SMAs with
    the price's distance from each, RSI, MACD, Bollinger bands, ATR in currency and as % of the
    close, 52-week high and low with distances. Keys carry the period (sma50, rsi14, atr14).
    Use a period long enough for the slow SMA. For custom indicators, expressions, conditions or
    a chart use custom_analysis."""
    params = dict(
        sma_fast=sma_fast, sma_slow=sma_slow, rsi_period=rsi_period, macd_fast=macd_fast, macd_slow=macd_slow,
        macd_signal=macd_signal, bollinger_n=bollinger_n, bollinger_k=bollinger_k, atr_period=atr_period,
    )
    return _run(analysis.technical_report, ticker, period, params)


@server.tool(annotations=READ_ONLY)
def fundamental_ratios(
    ticker: str,
    risk_free_annual: float | None = None,
    equity_risk_premium: float | None = None,
    beta: float | None = None,
) -> dict:
    """Ratios from the latest annual statements (USMx accounting and financial ratios sheet):
    valuation (P/E, EV/EBITDA, EV/EBIT, P/B, P/S, FCF yield), profitability and DuPont (ROE = net
    margin x asset turnover x equity multiplier, ROA, ROIC, margins), turnover (receivables,
    inventory, days sales/inventory/payables, cash conversion cycle), liquidity (current, quick,
    cash ratios, operating cash flow coverage, working capital), leverage and coverage, payout.
    With risk_free_annual and equity_risk_premium (decimals, from the analyst) also CAPM cost of
    equity, WACC, EVA and unlevered beta; beta defaults to Yahoo's. Also the Piotroski F-score
    (financial strength, 0-9), Altman Z and Z'' (distress) and Beneish M-score (earnings
    manipulation), with their components in `scores`; not computed for banks and insurers.
    Ratios not meaningful (losses, banks without current assets) are null."""
    return _run(analysis.fundamentals_report, ticker, risk_free_annual, equity_risk_premium, beta)


@server.tool(annotations=READ_ONLY)
def peer_comparison(tickers: list[str]) -> dict:
    """Fundamental ratios and scores for several companies side by side (2 to 10 tickers), for
    relative valuation within one sector. Failed tickers are reported, not dropped silently.
    Score components and their inputs are left out: call fundamental_ratios for one company."""
    if not 2 <= len(tickers) <= 10:
        raise ToolError("pass between 2 and 10 tickers")
    return analysis.peer_report(tickers)


@server.tool(annotations=READ_ONLY)
def etf_analysis(ticker: str, benchmark: str, period: str = "3y") -> dict:
    """ETF cost and replication quality: TER, tracking difference (fund minus benchmark,
    annualised) and tracking error against a benchmark ticker, which is converted to the
    fund's currency. Prefer a total-return index or a reference ETF on the same index."""
    return _run(analysis.etf_report, ticker, benchmark, period)


@server.tool(annotations=READ_ONLY)
def list_indicator_catalog() -> dict:
    """Parametric building blocks for custom_analysis and create_hook: every catalog indicator with
    its typed parameters (default, min, max), outputs and pane; the functions allowed in
    expressions and conditions; the expression syntax."""
    return workbench.catalog()


@server.tool(annotations=READ_ONLY, structured_output=False)
def custom_analysis(
    ticker: str,
    indicators: list[IndicatorSpec],
    period: str = "1y",
    series: list[SeriesSpec] | None = None,
    conditions: list[ConditionSpec] | None = None,
    levels: list[LevelSpec] | None = None,
    chart: bool = True,
    title: str | None = None,
) -> list:
    """User-defined technical analysis on one ticker, with an optional chart.

    indicators: catalog indicators with parameters, e.g.
      [{"id": "ema20", "fn": "ema", "params": {"n": 20}}, {"id": "rsi14", "fn": "rsi"},
       {"id": "bb", "fn": "bollinger", "params": {"n": 20, "k": 2}}]
    series: derived series by expression, e.g. [{"id": "dist", "expr": "close / bb.lower - 1"}]
    conditions: true/false rules evaluated on every bar, e.g.
      [{"name": "Oversold in uptrend", "when": "rsi14 < 30 and close > sma200"},
       {"name": "Golden cross", "when": "crosses_above(sma50, sma200)"}]
      For each: active now, number and dates of events, and forward returns after events against
      all bars (5 and 20 sessions) with a t statistic.
    levels: horizontal lines, e.g. [{"price": 25.5, "label": "support"}].
    Returns JSON with last values and condition reports, plus the PNG chart when chart is true.
    Call list_indicator_catalog first to see indicators, parameters and functions."""
    result, png = _run(
        workbench.run, ticker, period, indicators, series or [], conditions or [], levels or [], chart, title
    )
    content: list = [json.dumps(result, ensure_ascii=False)]
    if png:
        content.append(Image(path=png))
    return content


@server.tool(annotations=WRITES_HOOKS)
def create_hook(
    name: str,
    ticker: str,
    condition: ConditionSpec,
    action: HookAction,
    indicators: list[IndicatorSpec] | None = None,
    series: list[SeriesSpec] | None = None,
    period: str = "2y",
) -> dict:
    """Save a condition hook: when the condition fires on a new bar, a JSON event is delivered.

    action: {"type": "file", "target": "alerts_name"} appends JSON lines on the server, or
    {"type": "webhook", "target": "https://..."} posts JSON (only to prefixes the operator allowed).
    Past events are not replayed. Hooks are evaluated by run_hooks or the broker-analysis-hooks
    command (schedule it with cron). Analysis alerts only: nothing is ever traded."""
    return _run(hooks.create, name, ticker, condition, action, indicators or [], series or [], period)


@server.tool(annotations=READ_ONLY)
def list_hooks() -> dict:
    """Saved condition hooks with their condition, action and last delivered event."""
    return {"hooks": list(hooks.load_hooks().values())}


@server.tool(annotations=WRITES_HOOKS)
def run_hooks() -> dict:
    """Evaluate every saved hook now and deliver the events newer than each hook's last one."""
    return hooks.run()


@server.tool(annotations=ToolAnnotations(readOnlyHint=False, destructiveHint=True, idempotentHint=True))
def delete_hook(hook_id: str) -> dict:
    """Delete a saved hook by id."""
    return {"deleted": hooks.delete(hook_id), "hook_id": hook_id}


def main() -> None:
    server.run()


if __name__ == "__main__":
    main()
