"""Indicators against the independent `ta` library; fundamentals against hand-computed values."""

import numpy as np
import pandas as pd
import pytest
from ta.momentum import RSIIndicator
from ta.trend import MACD
from ta.volatility import AverageTrueRange, BollingerBands

from broker_analysis_mcp import indicators
from broker_analysis_mcp.fundamentals import (
    CapitalInputs,
    Year,
    cost_of_capital,
    interest_warning,
    ratios,
    revenue_cagr,
)


@pytest.fixture
def ohlc() -> pd.DataFrame:
    rng = np.random.default_rng(1)
    close = 100 * np.exp(np.cumsum(rng.normal(0, 0.015, 400)))
    spread = np.abs(rng.normal(0, 0.01, 400)) * close
    return pd.DataFrame(
        {"Open": close * (1 + rng.normal(0, 0.003, 400)), "High": close + spread, "Low": close - spread, "Close": close}
    )


STOCKCHARTS_CLOSES = [
    44.34, 44.09, 44.15, 43.61, 44.33, 44.83, 45.10, 45.42, 45.84, 46.08, 45.89, 46.03, 45.61,
    46.28, 46.28, 46.00, 46.03, 46.41, 46.22, 45.64, 46.21, 46.25, 45.71, 46.45, 45.78, 45.35,
    44.03, 44.18, 44.22, 44.57, 43.42, 42.66, 43.13,
]
# Published 14-day RSI for those closes (StockCharts ChartSchool, Wilder's worked example).
STOCKCHARTS_RSI = [
    70.53, 66.32, 66.55, 69.41, 66.36, 57.97, 62.93, 63.26, 56.06, 62.38, 54.71, 50.42, 39.99,
    41.46, 41.87, 45.46, 37.30, 33.08, 37.77,
]


def test_rsi_reproduces_wilders_worked_example() -> None:
    ours = indicators.rsi(pd.Series(STOCKCHARTS_CLOSES), 14).dropna().tolist()
    # First value by hand from the rounded closes: gains 3.34 / 14, losses 1.40 / 14.
    assert ours[0] == pytest.approx(100 - 100 / (1 + 3.34 / 1.40), abs=1e-9)
    # The published table was computed on unrounded prices; the gap is under 0.1 and fades.
    assert np.max(np.abs(np.array(ours) - STOCKCHARTS_RSI)) < 0.1
    assert abs(ours[-1] - STOCKCHARTS_RSI[-1]) < 0.03


def test_rsi_converges_to_ta(ohlc: pd.DataFrame) -> None:
    # ta seeds the average differently; Wilder smoothing forgets the seed like (13/14)^t.
    ours = indicators.rsi(ohlc["Close"], 14)
    theirs = RSIIndicator(ohlc["Close"], 14).rsi()
    assert np.max(np.abs(ours.iloc[200:] - theirs.iloc[200:])) < 1e-4


def test_macd_matches_ta(ohlc: pd.DataFrame) -> None:
    ours = indicators.macd(ohlc["Close"])
    theirs = MACD(ohlc["Close"])
    pd.testing.assert_series_equal(ours["macd"].iloc[40:], theirs.macd().iloc[40:], check_names=False, rtol=1e-9)
    pd.testing.assert_series_equal(
        ours["signal"].iloc[40:], theirs.macd_signal().iloc[40:], check_names=False, rtol=1e-9
    )


def test_bollinger_matches_ta(ohlc: pd.DataFrame) -> None:
    ours = indicators.bollinger(ohlc["Close"])
    theirs = BollingerBands(ohlc["Close"], 20, 2)
    pd.testing.assert_series_equal(ours["upper"].iloc[20:], theirs.bollinger_hband().iloc[20:], check_names=False)
    pd.testing.assert_series_equal(ours["lower"].iloc[20:], theirs.bollinger_lband().iloc[20:], check_names=False)


def test_atr_matches_ta(ohlc: pd.DataFrame) -> None:
    ours = indicators.atr(ohlc, 14)
    theirs = AverageTrueRange(ohlc["High"], ohlc["Low"], ohlc["Close"], 14).average_true_range()
    pd.testing.assert_series_equal(ours.iloc[13:], theirs.iloc[13:], check_names=False, rtol=1e-9)


def _statements(**overrides) -> list[Year]:
    """Fiscal years newest first: `revenue` lists one value per year, `<line>_prior` sets the year before."""
    lines = dict(
        revenue=[1200.0, 1100.0, 1000.0],
        gross_profit=480.0,
        ebitda=300.0,
        ebit=200.0,
        net_income=120.0,
        interest_expense=20.0,
        tax_rate=0.25,
        equity=900.0,
        equity_prior=700.0,
        total_debt=500.0,
        cash=100.0,
        invested_capital=1300.0,
        free_cash_flow=90.0,
        dividends_paid=-60.0,
        diluted_eps=1.2,
    ) | overrides
    revenue = lines.pop("revenue")
    prior = {k.removesuffix("_prior"): lines.pop(k) for k in list(lines) if k.endswith("_prior")}
    return [Year(revenue=revenue[0], **lines), Year(revenue=revenue[1], **prior), *(Year(revenue=v) for v in revenue[2:])]


def test_ratios_hand_computed() -> None:
    r = ratios(_statements(), price=18.0, market_cap=1800.0)
    assert r["pe"] == pytest.approx(15.0)
    assert r["enterprise_value"] == pytest.approx(2200.0)
    assert r["ev_ebitda"] == pytest.approx(2200 / 300)
    assert r["pb"] == pytest.approx(2.0)
    assert r["fcf_yield"] == pytest.approx(0.05)
    assert r["roe"] == pytest.approx(120 / 800)
    assert r["roic"] == pytest.approx(150 / 1300)
    assert r["gross_margin"] == pytest.approx(0.4)
    assert r["net_debt_ebitda"] == pytest.approx(400 / 300)
    assert r["interest_coverage"] == pytest.approx(10.0)
    assert r["payout"] == pytest.approx(0.5)
    assert r["revenue_cagr"] == pytest.approx((1200 / 1000) ** 0.5 - 1)


def test_meaningless_ratios_are_null() -> None:
    r = ratios(_statements(diluted_eps=-0.5, ebitda=-10.0, net_income=-40.0), price=18.0, market_cap=1800.0)
    assert r["pe"] is None
    assert r["ev_ebitda"] is None
    assert r["net_debt_ebitda"] is None
    assert r["payout"] is None


def test_revenue_cagr_skips_missing_years() -> None:
    assert revenue_cagr([1210.0, None, 1000.0]) == pytest.approx(0.1)
    assert revenue_cagr([1000.0]) is None


def _full_statements(**overrides) -> list[Year]:
    return _statements(
        total_assets=2400.0, total_assets_prior=1600.0, current_assets=900.0, current_liabilities=600.0,
        current_liabilities_prior=400.0, receivables=200.0, receivables_prior=100.0, inventory=150.0,
        inventory_prior=150.0, payables=120.0, payables_prior=80.0, total_liabilities=1500.0, long_term_debt=350.0,
        total_debt_prior=300.0, short_term_investments=50.0, depreciation_amortization=100.0, cost_of_revenue=720.0,
        pretax_income=160.0, operating_cash_flow=250.0, **overrides,
    )


def test_dupont_identity_holds_on_average_balances() -> None:
    r = ratios(_full_statements(), price=18.0, market_cap=1800.0)
    assert r["roe"] == pytest.approx(r["net_margin"] * r["asset_turnover"] * r["equity_multiplier"])
    assert r["asset_turnover"] == pytest.approx(1200 / 2000)
    assert r["equity_multiplier"] == pytest.approx(2000 / 800)
    assert r["roa"] == pytest.approx(120 / 2000)


def test_working_capital_cycle_hand_computed() -> None:
    r = ratios(_full_statements(), price=18.0, market_cap=1800.0)
    assert r["receivables_turnover"] == pytest.approx(1200 / 150)
    assert r["days_sales_outstanding"] == pytest.approx(365 / 8)
    assert r["inventory_turnover"] == pytest.approx(720 / 150)
    assert r["days_inventory"] == pytest.approx(365 / 4.8)
    assert r["days_payables"] == pytest.approx(100 / (720 / 365))
    assert r["cash_conversion_cycle"] == pytest.approx(365 / 8 + 365 / 4.8 - 100 / (720 / 365))
    assert r["current_ratio"] == pytest.approx(1.5)
    assert r["quick_ratio"] == pytest.approx((100 + 50 + 200) / 600)
    assert r["cash_ratio"] == pytest.approx(100 / 500)
    assert r["working_capital"] == pytest.approx(300)
    assert r["cash_coverage"] == pytest.approx((200 + 100) / 20)
    assert r["liabilities_to_assets"] == pytest.approx(1500 / 2400)
    assert r["ev_ebit"] == pytest.approx(2200 / 200)


def test_banks_without_current_balances_get_nulls_not_zeros() -> None:
    r = ratios(_statements(), price=18.0, market_cap=1800.0)
    for key in ("current_ratio", "quick_ratio", "days_inventory", "cash_conversion_cycle", "asset_turnover"):
        assert r[key] is None, key


def test_cost_of_capital_hand_computed() -> None:
    s = _full_statements()
    c = cost_of_capital(s, 1800.0, CapitalInputs(risk_free=0.03, equity_risk_premium=0.05, beta=1.2))
    ke = 0.03 + 1.2 * 0.05
    kd = 20 / ((500 + 300) / 2)
    wacc = 1800 / 2300 * ke + 500 / 2300 * kd * 0.75
    assert c["cost_of_equity"] == pytest.approx(ke)
    assert c["cost_of_debt_pretax"] == pytest.approx(kd)
    assert c["wacc"] == pytest.approx(wacc)
    assert c["eva"] == pytest.approx(150 - wacc * 1300)
    assert c["unlevered_beta"] == pytest.approx(1.2 / (1 + 0.75 * 500 / 1800))


def test_gross_finance_costs_are_flagged_not_trusted() -> None:
    # ENI.MI's latest annual statements as Yahoo reported them on 2026-10-22, in EUR.
    eni = _statements(interest_expense=8.17e9, total_debt=34.202e9, total_debt_prior=36.843e9)
    warning = interest_warning(eni)
    assert warning is not None and "23.0% of average debt" in warning
    assert interest_warning(_full_statements()) is None
    assert interest_warning(_statements(interest_expense=None)) is None
