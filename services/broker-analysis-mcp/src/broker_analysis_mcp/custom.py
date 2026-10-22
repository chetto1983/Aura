"""Builds user-defined indicators, derived series and conditions on one price history.

Conditions follow QuantDinger's signal semantics: a persistent condition becomes an event only
on its False -> True edge, so a state that stays true for ten bars notifies once. Each condition
also gets an event study: forward returns after its events against the forward returns of all
bars (the baseline a condition has to beat before anyone should care about it).
"""

import math

import numpy as np
import pandas as pd

from . import expr
from .catalog import CATALOG, SpecError, resolve_params
from .models import ConditionSpec, IndicatorSpec, SeriesSpec

PALETTE = ["#2563EB", "#F59E0B", "#10B981", "#EF4444", "#8B5CF6", "#0EA5E9", "#D946EF", "#84CC16"]
HORIZONS = (5, 20)
MAX_LOWER_PANES = 4


def base_namespace(prices: pd.DataFrame) -> dict[str, pd.Series]:
    return {c.lower(): prices[c].astype(float) for c in ("Open", "High", "Low", "Close", "Volume")}


def build(
    prices: pd.DataFrame, indicators: list[IndicatorSpec], series: list[SeriesSpec]
) -> tuple[dict[str, pd.Series], list[dict]]:
    """Evaluates indicators then derived series in order; returns the namespace and plot specs."""
    ns = base_namespace(prices)
    plots: list[dict] = []
    for i, spec in enumerate(indicators):
        if spec.fn not in CATALOG:
            raise SpecError(f"unknown indicator {spec.fn!r}; available: {sorted(CATALOG)}")
        if spec.id in ns or any(k.startswith(spec.id + ".") for k in ns):
            raise SpecError(f"duplicate series id {spec.id!r}")
        entry = CATALOG[spec.fn]
        params = resolve_params(spec.fn, spec.params)
        if "source" in params and params["source"] not in ns:
            raise SpecError(f"{spec.id}: source {params['source']!r} is not an earlier series; available {sorted(ns)}")
        outputs = entry.compute(ns, params)
        pane = entry.pane if spec.pane == "auto" else spec.pane
        for j, (name, values) in enumerate(outputs.items()):
            key = spec.id if len(outputs) == 1 else f"{spec.id}.{name}"
            ns[key] = values.astype(float)
            plots.append({
                "id": key, "group": spec.id, "fn": spec.fn, "params": params, "pane": pane,
                "color": spec.color if spec.color and j == 0 else PALETTE[(i + j) % len(PALETTE)],
                "guides": list(entry.guides),
            })
    for k, spec in enumerate(series):
        if spec.id in ns:
            raise SpecError(f"duplicate series id {spec.id!r}")
        value = expr.evaluate(spec.expr, ns)
        if not isinstance(value, pd.Series) or value.dtype == bool:
            raise SpecError(f"series {spec.id!r} must be numeric and vary over time; use a condition for true/false rules")
        ns[spec.id] = value.astype(float)
        plots.append({
            "id": spec.id, "group": spec.id, "fn": "expr", "params": {"expr": spec.expr}, "pane": spec.pane,
            "color": spec.color or PALETTE[(len(indicators) + k) % len(PALETTE)], "guides": [],
        })
    lower = {p["group"] for p in plots if p["pane"] == "lower"}
    if len(lower) > MAX_LOWER_PANES:
        raise SpecError(f"at most {MAX_LOWER_PANES} lower panes, got {len(lower)}: {sorted(lower)}")
    return ns, plots


def condition_series(spec: ConditionSpec, ns: dict[str, pd.Series]) -> tuple[pd.Series, pd.Series]:
    """The condition's state on every bar and its events (edges or every true bar)."""
    value = expr.evaluate(spec.when, ns)
    if not isinstance(value, pd.Series) or value.dtype != bool:
        raise SpecError(f"condition {spec.name!r} must be a true/false rule such as 'rsi14 < 30', got a number")
    state = value.fillna(False).astype(bool)
    edges = state & ~state.shift(1, fill_value=False).astype(bool)
    return state, (edges if spec.mode == "edge" else state)


def _stats(returns: np.ndarray) -> dict:
    if len(returns) == 0:
        return {"n": 0, "mean_pct": None, "median_pct": None, "hit_rate_pct": None}
    return {
        "n": len(returns),
        "mean_pct": round(float(np.mean(returns)) * 100, 3),
        "median_pct": round(float(np.median(returns)) * 100, 3),
        "hit_rate_pct": round(float(np.mean(returns > 0)) * 100, 1),
    }


def event_study(events: pd.Series, close: pd.Series) -> dict:
    """Forward returns after events against all bars, per horizon (returns of the close)."""
    out = {}
    positions = np.flatnonzero(events.to_numpy())
    values = close.to_numpy()
    for h in HORIZONS:
        valid = len(values) - h
        forward = values[h:] / values[:-h] - 1
        after = forward[positions[positions < valid]]
        cond, base = _stats(after), _stats(forward)
        t = None
        if cond["n"] >= 2:
            se = math.sqrt(np.var(after, ddof=1) / len(after) + np.var(forward, ddof=1) / len(forward))
            t = round(float((np.mean(after) - np.mean(forward)) / se), 2) if se > 0 else None
        out[f"{h}d"] = {"after_events": cond, "all_bars": base, "difference_t_stat": t}
    return out


def condition_report(spec: ConditionSpec, ns: dict[str, pd.Series]) -> tuple[dict, pd.Series]:
    state, events = condition_series(spec, ns)
    dates = [str(d.date()) for d in events.index[events.to_numpy()]]
    last = dates[-1] if dates else None
    bars_since = int(len(events) - 1 - np.flatnonzero(events.to_numpy())[-1]) if dates else None
    warnings = []
    if 0 < len(dates) < 20:
        warnings.append(f"only {len(dates)} events: forward-return statistics are anecdotal, not evidence")
    warnings.append("overlapping forward windows and no transaction costs: an event study, not a backtest")
    report = {
        "name": spec.name,
        "when": spec.when,
        "mode": spec.mode,
        "active_on_last_bar": bool(state.iloc[-1]),
        "fired_on_last_bar": bool(events.iloc[-1]),
        "events": len(dates),
        "last_event": last,
        "bars_since_last_event": bars_since,
        "recent_events": dates[-10:],
        "forward_returns": event_study(events, ns["close"]),
        "warnings": warnings,
    }
    return report, events


def last_values(ns: dict[str, pd.Series], plots: list[dict]) -> dict:
    out = {}
    for p in plots:
        s = ns[p["id"]].dropna()
        out[p["id"]] = None if s.empty else round(float(s.iloc[-1]), 4)
    return out


def chart_spec(prices: pd.DataFrame, ns: dict, plots: list[dict], conditions: list[tuple[ConditionSpec, pd.Series]], levels: list) -> dict:
    """QuantDinger-style output contract: plots aligned to the bars, sparse signals, layers."""
    def clean(s: pd.Series) -> list:
        return [None if pd.isna(v) or not math.isfinite(v) else round(float(v), 6) for v in s]

    signals = []
    for spec, events in conditions:
        anchor = prices["Low"] * 0.99 if spec.marker == "up" else prices["High"] * 1.01
        signals.append({
            "type": "buy" if spec.marker == "up" else "sell", "text": spec.name,
            "data": [round(float(a), 6) if e else None for a, e in zip(anchor, events, strict=True)],
        })
    return {
        "time": [str(d.date()) for d in prices.index],
        "plots": [
            {"name": p["id"], "data": clean(ns[p["id"]]), "color": p["color"], "type": "line", "overlay": p["pane"] == "price"}
            for p in plots
        ],
        "signals": signals,
        "layers": [{"type": "line", "price": lv.price, "text": lv.label, "dashed": True} for lv in levels],
    }
