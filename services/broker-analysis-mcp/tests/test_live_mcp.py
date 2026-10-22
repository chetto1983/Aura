"""End-to-end over the MCP protocol against live Yahoo Finance data.

Starts the real server as a subprocess over stdio, calls every tool with real tickers and checks
the results against Yahoo's own figures and against an independent recomputation. Needs network:
run with `pytest -m live`.
"""

import asyncio
import base64
import json
import os
import sys
import tempfile

import empyrical as ep
import numpy as np
import pandas as pd
import pytest
import yfinance as yf
from mcp import ClientSession, StdioServerParameters, stdio_client

pytestmark = pytest.mark.live

CALLS = {
    "risk_eni": ("risk_metrics", {"ticker": "ENI.MI", "period": "3y", "benchmark": "FTSEMIB.MI"}),
    "risk_aapl": ("risk_metrics", {"ticker": "AAPL", "period": "3y", "risk_free_annual": 0.03}),
    "tech_aapl": ("technical_indicators", {"ticker": "AAPL", "period": "2y"}),
    "fund_aapl": ("fundamental_ratios", {"ticker": "AAPL"}),
    "fund_eni": ("fundamental_ratios", {"ticker": "ENI.MI"}),
    "peers": ("peer_comparison", {"tickers": ["ENI.MI", "TTE.PA", "SHEL.L", "NOTATICKER123"]}),
    "etf_spy": ("etf_analysis", {"ticker": "SPY", "benchmark": "^SP500TR", "period": "3y"}),
    "etf_swda": ("etf_analysis", {"ticker": "SWDA.MI", "benchmark": "XDWD.DE", "period": "3y"}),
    "etf_swda_us": ("etf_analysis", {"ticker": "SWDA.MI", "benchmark": "URTH", "period": "3y"}),
    "bad_ticker": ("risk_metrics", {"ticker": "NOTATICKER123"}),
    "tech_custom": ("technical_indicators", {"ticker": "AAPL", "period": "2y", "sma_fast": 20, "rsi_period": 9}),
    "catalog": ("list_indicator_catalog", {}),
    "custom": (
        "custom_analysis",
        {
            "ticker": "ENI.MI",
            "period": "2y",
            "indicators": [
                {"id": "sma50", "fn": "sma", "params": {"n": 50}},
                {"id": "sma200", "fn": "sma", "params": {"n": 200}},
                {"id": "rsi14", "fn": "rsi"},
                {"id": "bb", "fn": "bollinger", "params": {"n": 20, "k": 2}},
            ],
            "series": [{"id": "dist_lower", "expr": "(close / bb.lower - 1) * 100"}],
            "conditions": [
                {"name": "Pullback in uptrend", "when": "rsi14 < 40 and close > sma200"},
                {"name": "Golden cross", "when": "crosses_above(sma50, sma200)"},
            ],
            "levels": [{"price": 25, "label": "25 EUR"}],
        },
    ),
    "custom_hostile": (
        "custom_analysis",
        {"ticker": "ENI.MI", "indicators": [], "conditions": [{"name": "x", "when": "__import__('os').getcwd()"}]},
    ),
    "hook_create": (
        "create_hook",
        {
            "name": "close above sma20",
            "ticker": "AAPL",
            "indicators": [{"id": "sma20", "fn": "sma", "params": {"n": 20}}],
            "condition": {"name": "Above SMA20", "when": "crosses_above(close, sma20)"},
            "action": {"type": "file", "target": "e2e"},
        },
    ),
    "hook_webhook_refused": (
        "create_hook",
        {
            "name": "x", "ticker": "AAPL", "condition": {"name": "x", "when": "close > 0"},
            "action": {"type": "webhook", "target": "http://169.254.169.254/latest"},
        },
    ),
    "fund_wacc": ("fundamental_ratios", {"ticker": "ENI.MI", "risk_free_annual": 0.035, "equity_risk_premium": 0.055}),
    "fund_bank": ("fundamental_ratios", {"ticker": "ISP.MI"}),
    "hook_list": ("list_hooks", {}),
    "hook_run": ("run_hooks", {}),
}
HOME = tempfile.mkdtemp(prefix="broker-analysis-e2e-")


async def _session_results() -> tuple[list[str], dict]:
    env = os.environ | {"BROKER_ANALYSIS_HOME": HOME}
    env.pop("BROKER_ANALYSIS_WEBHOOK_ALLOW", None)
    params = StdioServerParameters(command=sys.executable, args=["-m", "broker_analysis_mcp.server"], env=env)
    async with stdio_client(params) as (read, write), ClientSession(read, write) as session:
        await session.initialize()
        tools = [t.name for t in (await session.list_tools()).tools]
        results = {}
        for key, (tool, args) in CALLS.items():
            res = await session.call_tool(tool, args)
            text = res.content[0].text if res.content else ""
            images = [c for c in res.content if getattr(c, "type", "") == "image"]
            results[key] = {
                "error": res.is_error,
                "data": None if res.is_error else json.loads(text),
                "text": text,
                "images": [base64.b64decode(i.data) for i in images],
            }
        return tools, results


@pytest.fixture(scope="module")
def live() -> tuple[list[str], dict]:
    return asyncio.run(_session_results())


def _v(report: dict, name: str) -> float:
    value = report["metrics"][name]["value"]
    assert value is not None, f"{name} is null"
    return value


def test_server_exposes_its_tools(live) -> None:
    tools, _ = live
    assert sorted(tools) == sorted(
        [
            "risk_metrics", "technical_indicators", "fundamental_ratios", "peer_comparison", "etf_analysis",
            "list_indicator_catalog", "custom_analysis", "create_hook", "list_hooks", "run_hooks", "delete_hook",
        ]
    )


def test_risk_metrics_are_complete_and_plausible(live) -> None:
    r = live[1]["risk_eni"]["data"]
    assert r["window"]["days"] > 700 and r["currency"] == "EUR"
    assert all(m["value"] is not None for m in r["metrics"].values())
    cc, gk = _v(r, "volatility_close_to_close"), _v(r, "volatility_garman_klass")
    assert 5 < cc < 80 and 0.5 < gk / cc < 1.5
    assert 0 <= _v(r, "probabilistic_sharpe_vs_0") <= 1
    assert _v(r, "expected_shortfall_1d") > _v(r, "var_historical_1d") > 0
    assert -1 <= _v(r, "correlation") <= 1
    assert r["references"]["garman_klass"].startswith("Garman-Klass")


def test_risk_metrics_match_an_independent_recomputation(live) -> None:
    r = live[1]["risk_aapl"]["data"]
    closes = yf.Ticker("AAPL").history(start=r["window"]["start"], auto_adjust=True)["Close"]
    closes.index = closes.index.tz_localize(None).normalize()
    closes = closes.loc[: r["window"]["end"]]
    returns = closes.pct_change().dropna()
    # A live quote can move between the two fetches, so compare within 1%.
    assert _v(r, "volatility_close_to_close") == pytest.approx(ep.annual_volatility(returns) * 100, rel=0.01)
    rf_daily = 1.03 ** (1 / 252) - 1
    assert _v(r, "sharpe") == pytest.approx(ep.sharpe_ratio(returns, risk_free=rf_daily), rel=0.02)
    assert _v(r, "max_drawdown") == pytest.approx(-ep.max_drawdown(returns) * 100, rel=0.01)


def test_technical_indicators(live) -> None:
    r = live[1]["tech_aapl"]["data"]
    assert 0 <= _v(r, "rsi14") <= 100
    assert _v(r, "bollinger_lower") < _v(r, "bollinger_upper")
    assert _v(r, "low_52w") <= _v(r, "close") <= _v(r, "high_52w")
    last = float(yf.Ticker("AAPL").history(period="5d")["Close"].iloc[-1])
    assert _v(r, "close") == pytest.approx(last, rel=0.02)


def test_fundamentals_agree_with_yahoo_trailing_figures(live) -> None:
    for key in ("fund_aapl", "fund_eni"):
        r = live[1][key]["data"]
        pe, yahoo_pe = _v(r, "pe"), _v(r, "yahoo_trailing_pe_ttm")
        # Ours uses the last fiscal year, Yahoo the trailing twelve months: same order of magnitude.
        assert 0.6 < pe / yahoo_pe < 1.6, (key, pe, yahoo_pe)
        assert 0 < _v(r, "net_margin") < 100
        assert _v(r, "enterprise_value") > 0


def test_peer_comparison_reports_failures_and_converts_currencies(live) -> None:
    r = live[1]["peers"]["data"]
    assert set(r["companies"]) == {"ENI.MI", "TTE.PA", "SHEL.L"}
    assert "NOTATICKER123" in r["errors"]
    shell = r["companies"]["SHEL.L"]
    assert shell["statement_currency"] == "USD"
    assert any("converted" in w for w in shell["warnings"])
    for company in r["companies"].values():
        assert 2 < company["metrics"]["ev_ebitda"]["value"] < 15


def test_etf_tracking(live) -> None:
    spy = live[1]["etf_spy"]["data"]
    assert _v(spy, "ter") == pytest.approx(0.0945, abs=0.01)
    # SPY against its own total-return index: a small negative gap, a small error.
    assert -0.6 < _v(spy, "tracking_difference") < 0.1
    assert _v(spy, "tracking_error") < 1.0
    # Two MSCI World ETFs listed in the same time zone (Milan and Xetra, both EUR).
    swda = live[1]["etf_swda"]["data"]
    assert abs(_v(swda, "tracking_difference")) < 1.0
    assert _v(swda, "tracking_error") < 1.5
    assert not any("asynchronous" in w for w in swda["warnings"])
    # Against a US-listed benchmark the tool converts the currency and warns about the closes.
    swda_us = live[1]["etf_swda_us"]["data"]
    assert any("converted from USD to EUR" in w for w in swda_us["warnings"])
    assert any("asynchronous closes" in w for w in swda_us["warnings"])


def test_unknown_ticker_is_a_tool_error_not_a_crash(live) -> None:
    bad = live[1]["bad_ticker"]
    assert bad["error"] is True
    assert "NOTATICKER123" in bad["text"]


def test_technical_indicators_take_parameters(live) -> None:
    r = live[1]["tech_custom"]["data"]
    assert "sma20" in r["metrics"] and "rsi9" in r["metrics"] and r["parameters"]["rsi_period"] == 9


def test_catalog_lists_indicators_and_functions(live) -> None:
    c = live[1]["catalog"]["data"]
    assert {"sma", "ema", "rsi", "macd", "bollinger", "atr", "stochastic"} <= set(c["indicators"])
    assert c["indicators"]["rsi"]["params"]["n"]["default"] == 14
    assert "crosses_above" in c["expression_functions"]


def test_custom_analysis_returns_values_conditions_and_a_chart(live) -> None:
    res = live[1]["custom"]
    r = res["data"]
    assert set(r["series_last_values"]) >= {"sma50", "sma200", "rsi14", "bb.upper", "bb.lower", "dist_lower"}
    expected = (r["last_close"] / r["series_last_values"]["bb.lower"] - 1) * 100
    assert r["series_last_values"]["dist_lower"] == pytest.approx(expected, abs=1e-3)
    pullback, cross = r["conditions"]
    assert pullback["events"] == len(pullback["recent_events"]) or pullback["events"] > 10
    assert set(pullback["forward_returns"]) == {"5d", "20d"}
    assert cross["mode"] == "edge"
    assert res["images"] and res["images"][0][:8] == b"\x89PNG\r\n\x1a\n"
    assert os.path.exists(r["chart"]["png"]) and r["chart"]["png"].startswith(HOME)
    with open(r["chart"]["spec"]) as fh:
        spec = json.load(fh)
    assert len(spec["plots"][0]["data"]) == r["window"]["bars"]


def test_hostile_expression_is_refused(live) -> None:
    bad = live[1]["custom_hostile"]
    assert bad["error"] is True and "unknown function" in bad["text"]


def test_hooks_lifecycle(live) -> None:
    created = live[1]["hook_create"]["data"]
    assert created["hook"]["action"] == {"type": "file", "target": "e2e"}
    assert "not delivered" in created["note"]
    refused = live[1]["hook_webhook_refused"]
    assert refused["error"] is True and "disabled" in refused["text"]
    assert [h["id"] for h in live[1]["hook_list"]["data"]["hooks"]] == [created["hook"]["id"]]
    run = live[1]["hook_run"]["data"]
    assert run["failed"] == [] and run["fired"] == []  # no bar newer than the bookmark yet
    assert run["checked"][0]["hook_id"] == created["hook"]["id"]


def test_dupont_and_cost_of_capital_on_live_statements(live) -> None:
    r = live[1]["fund_wacc"]["data"]
    roe, margin, turnover, multiplier = (_v(r, k) for k in ("roe", "net_margin", "asset_turnover", "equity_multiplier"))
    # Percent metrics are scaled by 100; the identity holds on the underlying fractions.
    assert roe / 100 == pytest.approx(margin / 100 * turnover * multiplier, rel=1e-3)
    assert r["parameters"]["beta_source"] == "Yahoo Finance profile"
    ke = 3.5 + r["parameters"]["beta"] * 5.5
    assert _v(r, "cost_of_equity") == pytest.approx(ke, abs=1e-3)
    # WACC weighs ke and kd (1 - t), so it can exceed ke when debt costs more than equity.
    kd = _v(r, "cost_of_debt_pretax")
    assert 0 < _v(r, "wacc") <= max(ke, kd)
    assert (kd > 15) == any("of average debt" in w for w in r["warnings"])
    assert _v(r, "current_ratio") > 0 and _v(r, "days_sales_outstanding") > 0


def test_bank_ratios_that_do_not_apply_are_null(live) -> None:
    r = live[1]["fund_bank"]["data"]
    assert r["metrics"]["current_ratio"]["value"] is None
    assert r["metrics"]["cash_conversion_cycle"]["value"] is None
    assert any("risk_free_annual" in w for w in r["warnings"])
    assert _v(r, "pb") > 0 and _v(r, "roe") > 0


def _closes(ticker: str, start: str, end: str):
    closes = yf.Ticker(ticker).history(start=start, auto_adjust=True)["Close"]
    closes.index = closes.index.tz_localize(None).normalize()
    return closes.loc[:end]


def test_benchmark_relative_metrics_match_a_recomputation(live) -> None:
    r = live[1]["risk_eni"]["data"]
    raw = _v(r, "beta_weekly_2y")
    assert _v(r, "beta_adjusted") == pytest.approx(0.67 * raw + 0.33, abs=2e-4)
    assert _v(r, "weeks_for_beta") == 104
    w = r["window"]
    prices = pd.concat([_closes("ENI.MI", w["start"], w["end"]), _closes("FTSEMIB.MI", w["start"], w["end"])], axis=1)
    prices = prices.dropna()
    weekly = prices.resample("W-FRI").last().pct_change().dropna().iloc[-104:]
    cov = np.cov(weekly.iloc[:, 0], weekly.iloc[:, 1])
    assert raw == pytest.approx(cov[0, 1] / cov[1, 1], rel=0.02)
    monthly = prices.resample("ME").last().pct_change().dropna()
    if prices.index[-1] < prices.index[-1] + pd.offsets.BMonthEnd(0):
        monthly = monthly.iloc[:-1]
    up = ep.up_capture(monthly.iloc[:, 0], monthly.iloc[:, 1], period="monthly") * 100
    assert _v(r, "up_capture") == pytest.approx(up, rel=0.02)
    assert _v(r, "up_months") + _v(r, "down_months") == len(monthly)


def test_scores_on_live_statements(live) -> None:
    r = live[1]["fund_eni"]["data"]
    s = r["scores"]
    f = s["piotroski"]
    assert f["signals_available"] + len(f["missing"]) == 9
    assert (r["metrics"]["piotroski_f_score"]["value"] is None) == bool(f["missing"])
    x = s["altman"]["components"]
    z = 1.2 * x["x1_working_capital_to_assets"] + 1.4 * x["x2_retained_earnings_to_assets"] + 3.3 * x[
        "x3_ebit_to_assets"
    ] + 0.6 * x["x4_market_equity_to_liabilities"] + 1.0 * x["x5_sales_to_assets"]
    assert _v(r, "altman_z") == pytest.approx(z, abs=0.01)
    inc, bal = yf.Ticker("ENI.MI").income_stmt, yf.Ticker("ENI.MI").balance_sheet
    sales_to_assets = inc.loc["Total Revenue"].iloc[0] / bal.loc["Total Assets"].iloc[0]
    assert x["x5_sales_to_assets"] == pytest.approx(sales_to_assets, rel=1e-3)
    m = s["beneish"]
    if m["m"] is not None:
        assert _v(r, "beneish_m") == pytest.approx(m["m"], abs=0.01)
        assert m["above_cutoff"] == (m["m"] > -1.78)
    assert r["references"]["piotroski"].startswith("Piotroski (2000)")


def test_scores_are_not_computed_for_a_bank(live) -> None:
    r = live[1]["fund_bank"]["data"]
    assert r["scores"] == {}
    assert "altman_z" not in r["metrics"]
    assert any("banks and insurers" in w for w in r["warnings"])
