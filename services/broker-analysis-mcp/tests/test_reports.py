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
    m = analysis._benchmark_metrics(stock, bench, 0.02, warnings)
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
    m = analysis._benchmark_metrics(*_prices(1), 0.0, warnings)
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
