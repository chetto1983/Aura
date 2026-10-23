"""Parametric indicators, safe expressions, conditions, event study and charts."""

import math

import numpy as np
import pandas as pd
import pytest

from broker_analysis_mcp import custom, expr, indicators
from broker_analysis_mcp.catalog import CATALOG, SpecError, resolve_params
from broker_analysis_mcp.charts import render
from broker_analysis_mcp.models import ConditionSpec, IndicatorSpec, LevelSpec, SeriesSpec


@pytest.fixture
def prices() -> pd.DataFrame:
    rng = np.random.default_rng(4)
    close = 50 * np.exp(np.cumsum(rng.normal(0.0003, 0.015, 600)))
    spread = np.abs(rng.normal(0, 0.008, 600)) * close
    return pd.DataFrame(
        {
            "Open": close * (1 + rng.normal(0, 0.002, 600)),
            "High": close + spread,
            "Low": close - spread,
            "Close": close,
            "Volume": rng.integers(1_000, 5_000, 600).astype(float),
        },
        index=pd.bdate_range("2023-01-02", periods=600),
    )


def ns_of(prices: pd.DataFrame) -> dict[str, pd.Series]:
    return custom.base_namespace(prices)


class TestExpressions:
    def test_arithmetic_comparison_and_logic(self, prices) -> None:
        ns = ns_of(prices)
        value = expr.evaluate("(high + low) / 2 - close", ns)
        pd.testing.assert_series_equal(value, (prices["High"] + prices["Low"]) / 2 - prices["Close"], check_names=False)
        rule = expr.evaluate("close > open and not (volume < 2000) or close < 0", ns)
        expected = (prices["Close"] > prices["Open"]) & ~(prices["Volume"] < 2000)
        assert (rule == expected).all()

    def test_chained_comparison_is_elementwise(self, prices) -> None:
        ns = ns_of(prices) | {"r": pd.Series([10.0, 40.0, 80.0])}
        assert expr.evaluate("30 < r < 70", ns).tolist() == [False, True, False]

    def test_crosses_on_known_series(self) -> None:
        ns = {"a": pd.Series([1.0, 2.0, 3.0, 2.0, 1.0]), "b": pd.Series([2.0, 2.0, 2.0, 2.0, 2.0])}
        assert expr.evaluate("crosses_above(a, b)", ns).tolist() == [False, False, True, False, False]
        assert expr.evaluate("crosses_below(a, b)", ns).tolist() == [False, False, False, False, True]
        assert expr.evaluate("crosses_above(a, 2)", ns).tolist() == [False, False, True, False, False]

    def test_functions(self) -> None:
        x = pd.Series([1.0, 2.0, 4.0, 3.0])
        ns = {"x": x}
        assert expr.evaluate("prev(x, 1)", ns).tolist()[1:] == [1.0, 2.0, 4.0]
        assert expr.evaluate("change(x, 2)", ns).round(6).tolist()[2:] == [300.0, 50.0]
        assert expr.evaluate("highest(x, 2)", ns).tolist()[1:] == [2.0, 4.0, 4.0]
        assert expr.evaluate("between(x, 2, 3)", ns).tolist() == [False, True, False, True]
        assert expr.evaluate("max(x, 2.5)", ns).tolist() == [2.5, 2.5, 4.0, 3.0]

    def test_multi_output_attribute(self) -> None:
        ns = {"bb.lower": pd.Series([1.0, 2.0]), "close": pd.Series([1.5, 1.5])}
        assert expr.evaluate("close < bb.lower", ns).tolist() == [False, True]
        assert expr.names("close < bb.lower and rsi14 > 50") == {"close", "bb.lower", "rsi14"}

    @pytest.mark.parametrize(
        "hostile",
        [
            "__import__('os').system('id')",
            "close.__class__",
            "().__class__.__bases__",
            "(lambda: 1)()",
            "close[0]",
            "[c for c in close]",
            "open('/etc/passwd')",
            "prev(close, n=1)",
            "'text'",
            "close ** 2",
            "close if close > 1 else open",
            "close + " * 120 + "close",
            "unknown_series > 1",
            "highest(close, 1e9)",
        ],
    )
    def test_hostile_or_invalid_expressions_are_rejected(self, prices, hostile) -> None:
        with pytest.raises(SpecError):
            expr.evaluate(hostile, ns_of(prices))


class TestCatalog:
    def test_params_merge_defaults_and_check_bounds(self) -> None:
        assert resolve_params("rsi", {}) == {"source": "close", "n": 14}
        assert resolve_params("bollinger", {"n": "30", "k": 2.5})["n"] == 30
        with pytest.raises(SpecError, match="outside"):
            resolve_params("rsi", {"n": 1})
        with pytest.raises(SpecError, match="unknown parameters"):
            resolve_params("sma", {"length": 5})
        with pytest.raises(SpecError, match="expected int"):
            resolve_params("sma", {"n": "five"})

    def test_every_indicator_computes_aligned_outputs(self, prices) -> None:
        ns = ns_of(prices)
        for name, spec in CATALOG.items():
            outputs = spec.compute(ns, resolve_params(name, {}))
            assert tuple(outputs) == spec.outputs, name
            assert all(len(s) == len(prices) for s in outputs.values()), name
            assert all(s.dropna().size > 0 for s in outputs.values()), name

    def test_catalog_matches_the_tested_indicators(self, prices) -> None:
        ns = ns_of(prices)
        rsi = CATALOG["rsi"].compute(ns, resolve_params("rsi", {"n": 10}))["value"]
        pd.testing.assert_series_equal(rsi, indicators.rsi(prices["Close"], 10), check_names=False)
        atr = CATALOG["atr"].compute(ns, resolve_params("atr", {}))["value"]
        pd.testing.assert_series_equal(atr, indicators.atr(prices, 14), check_names=False)

    def test_stochastic_hand_value(self) -> None:
        idx = pd.RangeIndex(3)
        ns = {"high": pd.Series([10.0, 12.0, 11.0], idx), "low": pd.Series([8.0, 9.0, 7.0], idx), "close": pd.Series([9.0, 11.0, 10.0], idx)}
        k = CATALOG["stochastic"].compute(ns, {"n": 3, "d": 1})["k"]
        assert k.iloc[-1] == pytest.approx(100 * (10 - 7) / (12 - 7))


class TestBuildAndConditions:
    def test_build_chains_sources_and_expressions(self, prices) -> None:
        ns, plots = custom.build(
            prices,
            [
                IndicatorSpec(id="rsi14", fn="rsi"),
                IndicatorSpec(id="rsi_smooth", fn="ema", params={"source": "rsi14", "n": 5}),
                IndicatorSpec(id="bb", fn="bollinger"),
            ],
            [SeriesSpec(id="dist", expr="close / bb.lower - 1")],
        )
        pd.testing.assert_series_equal(ns["rsi_smooth"], indicators.ema(ns["rsi14"], 5), check_names=False)
        assert {"bb.upper", "bb.middle", "bb.lower", "dist"} <= set(ns)
        assert [p["pane"] for p in plots if p["group"] == "rsi_smooth"] == ["price"]

    @pytest.mark.parametrize(
        ("specs", "series", "message"),
        [
            ([IndicatorSpec(id="x", fn="nope")], [], "unknown indicator"),
            ([IndicatorSpec(id="x", fn="sma"), IndicatorSpec(id="x", fn="ema")], [], "duplicate"),
            ([IndicatorSpec(id="x", fn="sma", params={"source": "later"})], [], "not an earlier series"),
            ([], [SeriesSpec(id="flag", expr="close > open")], "must be numeric"),
            ([IndicatorSpec(id=f"r{i}", fn="rsi") for i in range(5)], [], "at most 4 lower panes"),
        ],
    )
    def test_build_rejects_bad_specs(self, prices, specs, series, message) -> None:
        with pytest.raises(SpecError, match=message):
            custom.build(prices, specs, series)

    def test_edge_mode_fires_once_per_episode_state_mode_every_bar(self) -> None:
        ns = {"x": pd.Series([0.0, 5.0, 6.0, 0.0, 7.0, 8.0, 9.0])}
        _, edges = custom.condition_series(ConditionSpec(name="up", when="x > 1"), ns)
        _, state = custom.condition_series(ConditionSpec(name="up", when="x > 1", mode="state"), ns)
        assert edges.tolist() == [False, True, False, False, True, False, False]
        assert int(state.sum()) == 5

    def test_numeric_condition_is_rejected(self, prices) -> None:
        with pytest.raises(SpecError, match="true/false"):
            custom.condition_series(ConditionSpec(name="bad", when="close - open"), ns_of(prices))

    def test_event_study_hand_computed(self) -> None:
        close = pd.Series([100.0, 110.0, 99.0, 108.9, 120.0, 132.0, 100.0], index=pd.bdate_range("2024-01-01", periods=7))
        events = pd.Series([True, False, True, False, False, False, False], index=close.index)
        out = custom.event_study(events, close)["5d"]
        after = [132 / 100 - 1, None]  # the event at bar 2 has no 5-bar future
        assert out["after_events"]["n"] == 1
        assert out["after_events"]["mean_pct"] == pytest.approx(after[0] * 100, abs=1e-3)
        all_bars = [132 / 100 - 1, 100 / 110 - 1]
        assert out["all_bars"]["mean_pct"] == pytest.approx(np.mean(all_bars) * 100, abs=1e-3)

    def test_condition_report_and_chart(self, prices, tmp_path) -> None:
        ns, plots = custom.build(
            prices,
            [IndicatorSpec(id="sma50", fn="sma", params={"n": 50}), IndicatorSpec(id="rsi14", fn="rsi"),
             IndicatorSpec(id="macd", fn="macd")],
            [],
        )
        spec = ConditionSpec(name="Back above SMA50", when="crosses_above(close, sma50) and rsi14 > 0")
        report, events = custom.condition_report(spec, ns)
        assert report["events"] == int(events.sum()) > 0
        assert set(report["forward_returns"]) == {"5d", "20d"}
        path = render(str(tmp_path / "c.png"), "test", prices, ns, plots, [(spec, events)],
                      [LevelSpec(price=60, label="level")], "EUR")
        with open(path, "rb") as f:
            assert f.read(8) == b"\x89PNG\r\n\x1a\n"
        chart = custom.chart_spec(prices, ns, plots, [(spec, events)], [LevelSpec(price=60)])
        assert len(chart["plots"][0]["data"]) == len(prices)
        assert sum(v is not None for v in chart["signals"][0]["data"]) == report["events"]


def test_volatility_indicator_annualises_with_the_instruments_year() -> None:
    days = pd.date_range("2025-01-01", periods=120, freq="D")
    close = pd.Series(100 * np.cumprod(1 + np.random.default_rng(1).normal(0, 0.03, 120)), index=days)
    prices = pd.DataFrame({"Open": close, "High": close, "Low": close, "Close": close, "Volume": 1.0})
    spec = [IndicatorSpec(id="vol", fn="volatility", params={"n": 30})]
    crypto, plots = custom.build(prices, spec, [], periods=365)
    stock, _ = custom.build(prices, spec, [])
    assert plots[0]["params"]["periods"] == 365
    assert crypto["vol"].iloc[-1] / stock["vol"].iloc[-1] == pytest.approx(math.sqrt(365 / 252))
    chosen, _ = custom.build(prices, [IndicatorSpec(id="vol", fn="volatility", params={"n": 30, "periods": 252})], [], periods=365)
    assert chosen["vol"].iloc[-1] == pytest.approx(stock["vol"].iloc[-1])


@pytest.mark.parametrize("raw", ["inf", "nan", "-inf"])
def test_non_finite_parameters_are_refused(raw: str) -> None:
    with pytest.raises(SpecError):
        resolve_params("sma", {"n": raw})
    with pytest.raises(SpecError):
        resolve_params("bollinger", {"k": raw})


@pytest.mark.parametrize("text", ["1 / 0", "highest(5, 3)", "crosses_above(1, 2)"])
def test_runtime_errors_in_expressions_are_spec_errors(text: str) -> None:
    with pytest.raises(SpecError, match="cannot evaluate"):
        expr.evaluate(text, {"close": pd.Series([1.0, 2.0, 3.0])})


def test_a_series_cannot_reuse_a_multi_output_indicator_id() -> None:
    days = pd.date_range("2025-01-01", periods=40, freq="D")
    close = pd.Series(np.linspace(10, 20, 40), index=days)
    prices = pd.DataFrame({"Open": close, "High": close, "Low": close, "Close": close, "Volume": 1.0})
    with pytest.raises(SpecError, match="duplicate series id 'bb'"):
        custom.build(prices, [IndicatorSpec(id="bb", fn="bollinger")], [SeriesSpec(id="bb", expr="close * 2")])


def test_candles_take_the_typical_bar_spacing() -> None:
    import matplotlib.pyplot as plt

    from broker_analysis_mcp import charts

    days = pd.bdate_range("2025-01-03", periods=10)  # starts on a Friday: the first gap is three days
    prices = pd.DataFrame({"Open": 1.0, "High": 2.0, "Low": 0.5, "Close": 1.5}, index=days)
    fig, ax = plt.subplots()
    charts._candles(ax, prices)
    assert ax.patches[0].get_width() == pytest.approx(0.6)
    plt.close(fig)
