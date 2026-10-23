"""Parametric indicator catalog.

Each entry declares typed parameters with defaults and bounds, the series it outputs, and where
it is drawn (on the price chart or in a lower pane): the same contract QuantDinger's indicator
IDE uses (`# @param period int 20 range=5:60:5`), expressed as data so a user or an agent can
compose indicators without running code.
"""

import math
from collections.abc import Callable
from dataclasses import dataclass

import numpy as np
import pandas as pd

from . import indicators as ind

Series = pd.Series
Namespace = dict[str, Series]


@dataclass(frozen=True)
class Param:
    kind: type
    default: float | int | str
    low: float | None = None
    high: float | None = None
    doc: str = ""


@dataclass(frozen=True)
class Indicator:
    compute: Callable[[Namespace, dict], dict[str, Series]]
    params: dict[str, Param]
    outputs: tuple[str, ...]
    pane: str  # "price" or "lower"
    doc: str
    guides: tuple[float, ...] = ()  # reference levels drawn in the pane, e.g. RSI 30/70


SOURCE = Param(str, "close", doc="open, high, low, close, volume, or the id of an earlier series")
N = Param(int, 20, 2, 1000, "lookback in sessions")


def _src(ns: Namespace, p: dict) -> Series:
    return ns[p["source"]]


def _ordered(frame: pd.DataFrame, columns: tuple[str, ...]) -> dict[str, Series]:
    return {c: frame[c] for c in columns}


def _stochastic(ns: Namespace, p: dict) -> dict[str, Series]:
    low, high = ns["low"].rolling(p["n"]).min(), ns["high"].rolling(p["n"]).max()
    k = 100 * (ns["close"] - low) / (high - low)
    return {"k": k, "d": k.rolling(p["d"]).mean()}


def _donchian(ns: Namespace, p: dict) -> dict[str, Series]:
    upper, lower = ns["high"].rolling(p["n"]).max(), ns["low"].rolling(p["n"]).min()
    return {"upper": upper, "middle": (upper + lower) / 2, "lower": lower}


def _rolling_vol(ns: Namespace, p: dict) -> dict[str, Series]:
    returns = _src(ns, p).pct_change()
    return {"value": returns.rolling(p["n"]).std(ddof=1) * math.sqrt(p["periods"]) * 100}


def _zscore(ns: Namespace, p: dict) -> dict[str, Series]:
    x = _src(ns, p)
    return {"value": (x - x.rolling(p["n"]).mean()) / x.rolling(p["n"]).std(ddof=0)}


def _obv(ns: Namespace, p: dict) -> dict[str, Series]:
    direction = np.sign(ns["close"].diff()).fillna(0)
    return {"value": (direction * ns["volume"]).cumsum()}


CATALOG: dict[str, Indicator] = {
    "sma": Indicator(
        lambda ns, p: {"value": ind.sma(_src(ns, p), p["n"])},
        {"source": SOURCE, "n": N}, ("value",), "price", "Simple moving average",
    ),
    "ema": Indicator(
        lambda ns, p: {"value": ind.ema(_src(ns, p), p["n"])},
        {"source": SOURCE, "n": N}, ("value",), "price", "Exponential moving average (span n)",
    ),
    "rsi": Indicator(
        lambda ns, p: {"value": ind.rsi(_src(ns, p), p["n"])},
        {"source": SOURCE, "n": Param(int, 14, 2, 200, "Wilder period")}, ("value",), "lower",
        "Relative Strength Index, Wilder smoothing", (30.0, 70.0),
    ),
    "macd": Indicator(
        lambda ns, p: _ordered(ind.macd(_src(ns, p), p["fast"], p["slow"], p["signal"]), ("macd", "signal", "histogram")),
        {
            "source": SOURCE,
            "fast": Param(int, 12, 2, 200),
            "slow": Param(int, 26, 3, 400),
            "signal": Param(int, 9, 2, 100),
        },
        ("macd", "signal", "histogram"), "lower", "MACD line, signal line and histogram", (0.0,),
    ),
    "bollinger": Indicator(
        lambda ns, p: _ordered(ind.bollinger(_src(ns, p), p["n"], p["k"]), ("upper", "middle", "lower")),
        {"source": SOURCE, "n": N, "k": Param(float, 2.0, 0.1, 5.0, "standard deviations")},
        ("upper", "middle", "lower"), "price", "Bollinger bands (population standard deviation)",
    ),
    "atr": Indicator(
        lambda ns, p: {"value": ind.atr(pd.DataFrame({c.title(): ns[c] for c in ("high", "low", "close")}), p["n"])},
        {"n": Param(int, 14, 2, 200, "Wilder period")}, ("value",), "lower", "Average True Range",
    ),
    "donchian": Indicator(
        _donchian, {"n": N}, ("upper", "middle", "lower"), "price",
        "Donchian channel: rolling highest high and lowest low",
    ),
    "roc": Indicator(
        lambda ns, p: {"value": (_src(ns, p) / _src(ns, p).shift(p["n"]) - 1) * 100},
        {"source": SOURCE, "n": N}, ("value",), "lower", "Rate of change over n sessions, in %", (0.0,),
    ),
    "volatility": Indicator(
        _rolling_vol,
        {
            "source": SOURCE, "n": N,
            "periods": Param(int, 252, 1, 366, "bars per year to annualise; defaults to 365 for crypto assets"),
        },
        ("value",), "lower",
        "Rolling annualised close-to-close volatility, in %",
    ),
    "zscore": Indicator(
        _zscore, {"source": SOURCE, "n": N}, ("value",), "lower",
        "Distance from the rolling mean in rolling standard deviations", (-2.0, 0.0, 2.0),
    ),
    "stochastic": Indicator(
        _stochastic, {"n": Param(int, 14, 2, 200), "d": Param(int, 3, 1, 50, "%D smoothing")},
        ("k", "d"), "lower", "Stochastic oscillator %K and %D", (20.0, 80.0),
    ),
    "obv": Indicator(_obv, {}, ("value",), "lower", "On-balance volume"),
}


class SpecError(ValueError):
    """An indicator, expression or condition the user supplied is not valid."""


def resolve_params(name: str, given: dict, defaults: dict | None = None) -> dict:
    """Merges user parameters over the defaults, checking names, types and bounds. `defaults`
    replaces a catalog default for this instrument (periods per year for a crypto asset)."""
    spec = CATALOG[name]
    unknown = set(given) - set(spec.params)
    if unknown:
        raise SpecError(f"{name}: unknown parameters {sorted(unknown)}; allowed {sorted(spec.params)}")
    out = {}
    for key, param in spec.params.items():
        raw = given.get(key, (defaults or {}).get(key, param.default))
        try:
            value = param.kind(raw) if param.kind is not int else int(float(raw))
            if isinstance(value, float) and not math.isfinite(value):
                raise ValueError("not finite")
        except (TypeError, ValueError, OverflowError) as err:
            raise SpecError(f"{name}.{key}: expected {param.kind.__name__}, got {raw!r}") from err
        if (param.low is not None and value < param.low) or (param.high is not None and value > param.high):
            raise SpecError(f"{name}.{key}={value} outside [{param.low}, {param.high}]")
        out[key] = value
    return out


def describe() -> dict:
    return {
        name: {
            "doc": spec.doc,
            "pane": spec.pane,
            "outputs": list(spec.outputs),
            "guides": list(spec.guides),
            "params": {
                k: {"type": p.kind.__name__, "default": p.default, "min": p.low, "max": p.high, "doc": p.doc}
                for k, p in spec.params.items()
            },
        }
        for name, spec in CATALOG.items()
    }
