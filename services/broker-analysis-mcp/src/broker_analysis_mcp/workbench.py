"""Runs a user-defined analysis on one ticker: indicators, derived series, conditions, chart."""

import json
import re
from datetime import UTC, datetime

from . import charts, custom, data, expr
from .catalog import describe
from .hooks import home
from .models import ConditionSpec, IndicatorSpec, LevelSpec, SeriesSpec


def chart_dir():
    path = home() / "charts"
    path.mkdir(parents=True, exist_ok=True)
    return path


def catalog() -> dict:
    return {
        "indicators": describe(),
        "expression_functions": {name: doc for name, (_, doc) in expr.FUNCTIONS.items()},
        "base_series": ["open", "high", "low", "close", "volume"],
        "syntax": "arithmetic + - * /, comparisons < <= > >= == !=, and / or / not; a multi-output "
        "indicator is read as id.output, e.g. bb.lower or macd.histogram",
    }


def run(
    ticker: str,
    period: str,
    indicators: list[IndicatorSpec],
    series: list[SeriesSpec],
    conditions: list[ConditionSpec],
    levels: list[LevelSpec],
    chart: bool,
    title: str | None = None,
) -> tuple[dict, str | None]:
    prices, meta = data.history(ticker, period)
    ns, plots = custom.build(prices, indicators, series, meta.periods_per_year)
    reports, marked = [], []
    for spec in conditions:
        report, events = custom.condition_report(spec, ns)
        reports.append(report)
        marked.append((spec, events))
    result = {
        "ticker": ticker,
        "currency": meta.currency,
        "window": {"start": str(prices.index[0].date()), "end": str(prices.index[-1].date()), "bars": len(prices)},
        "last_close": round(float(prices["Close"].iloc[-1]), 4),
        "series_last_values": custom.last_values(ns, plots),
        "series_definitions": [{"id": p["id"], "fn": p["fn"], "params": p["params"], "pane": p["pane"]} for p in plots],
        "conditions": reports,
        "source": meta.source,
        "retrieved_at": meta.retrieved_at,
        "warnings": meta.warnings,
    }
    png = None
    if chart:
        stamp = datetime.now(UTC).strftime("%Y%m%dT%H%M%S")
        base = chart_dir() / f"{re.sub(r'[^A-Za-z0-9]+', '_', ticker)}_{stamp}"
        png = charts.render(
            str(base.with_suffix(".png")), title or f"{ticker} · {result['window']['start']} → {result['window']['end']}",
            prices, ns, plots, marked, levels, meta.currency,
        )
        spec = custom.chart_spec(prices, ns, plots, marked, levels)
        base.with_suffix(".json").write_text(json.dumps(spec))
        result["chart"] = {"png": png, "spec": str(base.with_suffix(".json")), "format": "QuantDinger-style plots/signals/layers"}
    return result, png
