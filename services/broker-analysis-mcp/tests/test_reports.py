"""Report wiring without the network: benchmark metrics and score gating."""

import numpy as np
import pandas as pd
import pytest

from broker_analysis_mcp import analysis
from broker_analysis_mcp.fundamentals import Year


def _prices(years: int, seed: int = 4) -> tuple[pd.Series, pd.Series]:
    days = pd.bdate_range(end="2026-09-30", periods=252 * years)
    rng = np.random.default_rng(seed)
    bench = rng.normal(0.0003, 0.01, len(days))
    stock = 1.3 * bench + rng.normal(0.0, 0.01, len(days))
    return (
        pd.Series(100 * np.cumprod(1 + stock), index=days),
        pd.Series(100 * np.cumprod(1 + bench), index=days),
    )


def test_benchmark_metrics_carry_units_and_the_bloomberg_window() -> None:
    warnings: list[str] = []
    stock, bench = _prices(3)
    m = analysis._benchmark_metrics(stock, bench, 0.02, 252, warnings)
    assert warnings == []
    assert m["weeks_for_beta"]["value"] == 104
    raw = m["beta_weekly_2y"]["value"]
    assert m["beta_adjusted"]["value"] == pytest.approx(0.67 * raw + 0.33, abs=1e-4)
    assert 1.0 < raw < 1.6
    assert m["up_capture"]["unit"] == "%" and m["up_capture"]["value"] > 100
    # The series ends on the last business day of September, so every month after the first is complete.
    assert m["up_months"]["value"] + m["down_months"]["value"] == stock.index.to_period("M").nunique() - 1
    assert {m[k]["ref"] for k in ("jensen_alpha", "treynor", "m_squared")} == {"capm"}


def test_short_history_warns_about_the_beta_window() -> None:
    warnings: list[str] = []
    m = analysis._benchmark_metrics(*_prices(1), 0.0, 252, warnings)
    assert m["weeks_for_beta"]["value"] < 104
    assert any("beta_weekly_2y" in w for w in warnings)


def test_scores_are_skipped_for_banks() -> None:
    warnings: list[str] = []
    metrics, details = analysis._scores([Year(), Year()], 1e9, "Financial Services", warnings)
    assert metrics == {} and details == {}
    assert any("banks and insurers" in w for w in warnings)


def test_missing_score_inputs_are_reported() -> None:
    warnings: list[str] = []
    metrics, details = analysis._scores([Year(revenue=10.0)], 1e9, "Energy", warnings)
    assert metrics["piotroski_f_score"]["value"] is None
    assert details["piotroski"]["missing"]
    assert any(w.startswith("Beneish M-score: no value for") for w in warnings)


def test_peer_report_shares_references_and_drops_score_components(monkeypatch) -> None:
    def fake_report(ticker: str) -> dict:
        if ticker == "BAD":
            raise analysis.data.DataError("no profile for 'BAD'")
        return {"ticker": ticker, "metrics": {}, "scores": {"piotroski": {}}, "references": {f"ref_{ticker}": "x"}}

    monkeypatch.setattr(analysis, "fundamentals_report", fake_report)
    r = analysis.peer_report(["A", "BAD", "B"])
    assert set(r["companies"]) == {"A", "B"}
    assert r["errors"] == {"BAD": "no profile for 'BAD'"}
    assert r["references"] == {"ref_A": "x", "ref_B": "x"}
    assert all("scores" not in c and "references" not in c for c in r["companies"].values())


def test_a_crypto_asset_is_compared_on_the_dates_both_trade() -> None:
    # The asset equals the index on every weekday and wanders at weekends. Returns taken after
    # aligning the prices are identical; returns taken first would compare a Sunday-to-Monday
    # move with a Friday-to-Monday one.
    _, index_close = _prices(3)
    every_day = pd.date_range(index_close.index[0], index_close.index[-1], freq="D")
    crypto = index_close.reindex(every_day).ffill()
    weekend = crypto.index.day_of_week >= 5
    crypto[weekend] *= np.random.default_rng(9).uniform(0.9, 1.1, int(weekend.sum()))
    m = analysis._benchmark_metrics(crypto, index_close, 0.0, 252, [])
    assert m["beta"]["value"] == pytest.approx(1.0, abs=1e-4)
    assert m["correlation"]["value"] == pytest.approx(1.0, abs=1e-4)
    assert m["jensen_alpha"]["value"] == pytest.approx(0.0, abs=1e-4)
    assert m["information_ratio"]["value"] is None  # no active return to scale


def test_the_52_week_window_is_calendar_time() -> None:
    days = pd.date_range("2025-01-01", periods=500, freq="D")
    prices = pd.DataFrame({"High": np.arange(500.0)}, index=days)
    window = analysis.last_52_weeks(prices)
    assert len(window) == 364  # a year of a crypto asset, not 252 bars
    sessions = pd.DataFrame({"High": np.arange(500.0)}, index=pd.bdate_range("2025-01-01", periods=500))
    assert 255 <= len(analysis.last_52_weeks(sessions)) <= 261
