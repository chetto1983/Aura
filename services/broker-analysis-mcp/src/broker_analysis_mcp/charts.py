"""PNG charts: price pane with overlays, condition markers and levels; one lower pane per group."""

import matplotlib

matplotlib.use("Agg")

import matplotlib.dates as mdates
import matplotlib.pyplot as plt
import numpy as np
import pandas as pd

from .models import ConditionSpec, LevelSpec

CANDLE_LIMIT = 260
UP, DOWN, INK, GRID = "#16A34A", "#DC2626", "#1F2937", "#E5E7EB"


def _candles(ax, prices: pd.DataFrame) -> None:
    x = mdates.date2num(prices.index.to_pydatetime())
    width = 0.6 * float(np.median(np.diff(x))) if len(x) > 1 else 0.6  # weekends make some gaps wider
    rising = prices["Close"] >= prices["Open"]
    colors = [UP if r else DOWN for r in rising]
    ax.vlines(x, prices["Low"], prices["High"], color=colors, linewidth=0.7)
    bottoms = prices[["Open", "Close"]].min(axis=1)
    heights = (prices["Close"] - prices["Open"]).abs().clip(lower=1e-9)
    ax.bar(x, heights, width, bottom=bottoms, color=colors, edgecolor=colors, linewidth=0.5)


def render(
    path: str,
    title: str,
    prices: pd.DataFrame,
    ns: dict[str, pd.Series],
    plots: list[dict],
    conditions: list[tuple[ConditionSpec, pd.Series]],
    levels: list[LevelSpec],
    currency: str,
) -> str:
    groups = list(dict.fromkeys(p["group"] for p in plots if p["pane"] == "lower"))
    ratios = [3.2] + [1.0] * len(groups)
    fig, axes = plt.subplots(
        len(ratios), 1, sharex=True, figsize=(12, 5 + 1.8 * len(groups)),
        gridspec_kw={"height_ratios": ratios, "hspace": 0.06}, squeeze=False,
    )
    axes = axes[:, 0]
    price_ax = axes[0]
    if len(prices) <= CANDLE_LIMIT:
        _candles(price_ax, prices)
    else:
        price_ax.plot(prices.index, prices["Close"], color=INK, linewidth=1.0, label="close")
    for p in plots:
        if p["pane"] == "price":
            price_ax.plot(prices.index, ns[p["id"]], color=p["color"], linewidth=1.2, label=p["id"])
    for spec, events in conditions:
        hits = prices[events.to_numpy()]
        if spec.marker == "up":
            price_ax.scatter(hits.index, hits["Low"] * 0.985, marker="^", s=55, color=UP, zorder=5, label=spec.name)
        else:
            price_ax.scatter(hits.index, hits["High"] * 1.015, marker="v", s=55, color=DOWN, zorder=5, label=spec.name)
    for level in levels:
        price_ax.axhline(level.price, color="#6B7280", linestyle="--", linewidth=0.9)
        if level.label:
            price_ax.annotate(level.label, (prices.index[0], level.price), fontsize=8, color="#374151",
                              xytext=(4, 3), textcoords="offset points")
    price_ax.set_title(title, loc="left", fontsize=12, fontweight="bold", color=INK)
    price_ax.set_ylabel(currency)
    price_ax.legend(loc="upper left", fontsize=8, frameon=False, ncol=4)

    for ax, group in zip(axes[1:], groups, strict=True):
        members = [p for p in plots if p["group"] == group]
        for p in members:
            if p["id"].endswith(".histogram"):
                ax.bar(prices.index, ns[p["id"]], color=p["color"], alpha=0.5, width=1.0, label=p["id"])
            else:
                ax.plot(prices.index, ns[p["id"]], color=p["color"], linewidth=1.1, label=p["id"])
        for guide in members[0]["guides"]:
            ax.axhline(guide, color="#9CA3AF", linestyle=":", linewidth=0.9)
        ax.legend(loc="upper left", fontsize=8, frameon=False, ncol=3)
    for ax in axes:
        ax.grid(color=GRID, linewidth=0.6)
        ax.spines[["top", "right"]].set_visible(False)
    axes[-1].xaxis.set_major_formatter(mdates.ConciseDateFormatter(mdates.AutoDateLocator()))
    fig.text(0.99, 0.005, "Source: Yahoo Finance via yfinance (unofficial, prototype)", ha="right", fontsize=7, color="#6B7280")
    fig.savefig(path, dpi=110, bbox_inches="tight")
    plt.close(fig)
    return path
