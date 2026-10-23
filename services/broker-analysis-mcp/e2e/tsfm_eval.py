"""Out-of-sample test of time-series foundation models against naive baselines on market data.

Models: Mistral Timestral (5M, 28M, 123M) and Amazon Chronos-Bolt-base and Chronos-2.

Protocol, fixed before looking at any result:
  universe     10 daily series, 10 years of Yahoo adjusted OHLC
  origins      every 10 sessions over the last 750 sessions, 1,024 sessions of context
  task A       price at +5 and +20 sessions
               point: median vs random walk (last close), mean absolute log error
               distribution: mean pinball loss over the quantiles 0.1, 0.2, ..., 0.9 (the levels
               every model provides; Chronos-Bolt has no others) vs historical simulation
               (the last 500 sessions' h-day log returns applied to the last close); coverage
               of the 10-90% and 5-95% intervals
  added after the first run, before deciding: a drift baseline (mean daily log return of the last
  500 sessions times h) and Diebold-Mariano tests on the per-origin loss differences averaged
  across tickers, with Newey-West standard errors (lag = ceil(h / step)) for overlapping windows
  task B       mean daily Garman-Klass variance over the next 20 sessions
               model fed the daily GK variance; forecast = mean of the 21 quantiles, averaged
               over the 20 steps; baselines EWMA (lambda 0.94) and the 60-session mean
               loss QLIKE (Patton 2011) and MSE of log variance

A component that learns must beat its dumb baseline out of sample, or it is dropped.
Run with an environment holding `timestral` and torch:
  python e2e/tsfm_eval.py --models timestral-28M chronos-bolt-base chronos-2 --out e2e/runs/tsfm.json
  python e2e/tsfm_eval.py --tickers BTC-USD ETH-USD SOL-USD XRP-USD --out e2e/runs/tsfm_crypto.json
Kronos (arXiv:2508.02739, NeoQuasar/Kronos-*) differs by design, fixed before its first run: it
reads OHLCV bars with their dates instead of a close series, so its price quantiles come from
--samples independent sampled paths and its volatility forecast is the mean Garman-Klass variance
of the sampled bars, the target's own estimator. Kronos-small and -base accept at most 512 bars of
context, so they read the last 512 of the 1,024; Kronos-mini reads all 1,024. Its code is not on
PyPI: pass the cloned repository with --kronos-src.
Crypto assets trade every day, so for them a session is a calendar day. --skip-last N drops the
latest N sessions before anything is computed, so a second test can run on an earlier window
disjoint from the first; each ticker's window dates are in the report.
"""

import argparse
import json
import math
import sys
import time

import numpy as np
import pandas as pd
import torch
import yfinance as yf

UNIVERSE = ["ENI.MI", "ISP.MI", "UCG.MI", "AAPL", "MSFT", "NVDA", "^GSPC", "FTSEMIB.MI", "^STOXX50E", "SWDA.MI"]
CONTEXT, STEP, SPAN, HORIZONS, VOL_H = 1024, 10, 750, (5, 20), 20
LEVELS = [0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9]


class Univariate:
    """Models that read one series: the close for prices, the daily Garman-Klass variance for volatility."""

    def forecast(self, df: pd.DataFrame, origins: list[int], hmax: int) -> tuple[np.ndarray, np.ndarray]:
        close, var = df["Close"].to_numpy(), gk_variance(df)
        qp = self.quantiles(np.stack([close[o - CONTEXT:o] for o in origins]), hmax)  # (B, H, Q)
        qv = self.quantiles(np.stack([var[o - CONTEXT:o] for o in origins]), VOL_H)
        # The mean of the quantiles approximates the predictive mean of the variance.
        return qp, qv.mean(axis=2).mean(axis=1)


class Timestral(Univariate):
    def __init__(self, size: str):
        from timestral import TimestralForecaster

        self.model = TimestralForecaster.from_pretrained(f"mistralai/Timestral-{size}", device="cpu")

    def quantiles(self, contexts: np.ndarray, horizon: int) -> np.ndarray:
        """(B, H, len(LEVELS)) quantile forecasts."""
        with torch.no_grad():
            f = self.model.forecast(contexts, prediction_length=horizon)
        idx = [list(f.quantile_levels).index(q) for q in LEVELS]
        return f.quantiles[:, 0][..., idx].cpu().numpy()


class Chronos(Univariate):
    def __init__(self, repo: str):
        from chronos import BaseChronosPipeline

        self.pipeline = BaseChronosPipeline.from_pretrained(f"amazon/{repo}", device_map="cpu")

    def quantiles(self, contexts: np.ndarray, horizon: int) -> np.ndarray:
        x = torch.tensor(contexts, dtype=torch.float32)
        if type(self.pipeline).__name__ == "Chronos2Pipeline":
            x = x[:, None, :]  # Chronos-2 takes (series, variates, time)
        with torch.no_grad():
            q, _ = self.pipeline.predict_quantiles(x, prediction_length=horizon, quantile_levels=LEVELS)
        if isinstance(q, list):  # Chronos-2: one (variates, H, Q) tensor per series
            q = torch.stack([x[0] for x in q])
        return q.cpu().numpy()


KRONOS = {  # model: (tokenizer, maximum context)
    "Kronos-mini": ("Kronos-Tokenizer-2k", 2048),
    "Kronos-small": ("Kronos-Tokenizer-base", 512),
    "Kronos-base": ("Kronos-Tokenizer-base", 512),
}


class KronosModel:
    def __init__(self, name: str, src: str, samples: int, chunk: int = 256):
        sys.path.insert(0, src)
        from model import Kronos, KronosPredictor, KronosTokenizer

        tokenizer_id, max_context = KRONOS[name]
        self.context = min(CONTEXT, max_context)
        self.samples, self.chunk = samples, chunk
        self.predictor = KronosPredictor(
            Kronos.from_pretrained(f"NeoQuasar/{name}"), KronosTokenizer.from_pretrained(f"NeoQuasar/{tokenizer_id}"),
            device="cpu", max_context=self.context,
        )

    def forecast(self, df: pd.DataFrame, origins: list[int], hmax: int) -> tuple[np.ndarray, np.ndarray]:
        """Sampled paths: one batch row per (origin, sample), each drawn with sample_count=1 so that
        Kronos does not average them."""
        bars = df[["Open", "High", "Low", "Close", "Volume"]].rename(columns=str.lower)
        dates = pd.Series(df.index.tz_localize(None) if df.index.tz is not None else df.index)
        rows = [(o, s) for o in origins for s in range(self.samples)]
        paths = []
        for start in range(0, len(rows), self.chunk):
            batch = rows[start:start + self.chunk]
            paths += self.predictor.predict_batch(
                [bars.iloc[o - self.context:o] for o, _ in batch],
                [dates.iloc[o - self.context:o].reset_index(drop=True) for o, _ in batch],
                [dates.iloc[o:o + hmax].reset_index(drop=True) for o, _ in batch],
                pred_len=hmax, sample_count=1, verbose=False,
            )
        close = np.array([p["close"].to_numpy() for p in paths]).reshape(len(origins), self.samples, hmax)
        variance = np.array([gk_variance(p.rename(columns=str.title)) for p in paths])
        qp = np.quantile(close, LEVELS, axis=1).transpose(1, 2, 0)  # (B, H, Q)
        return qp, variance.reshape(len(origins), self.samples, hmax)[:, :, :VOL_H].mean(axis=(1, 2))


def make(name: str, args: argparse.Namespace):
    if name.startswith("timestral-"):
        return Timestral(name.removeprefix("timestral-"))
    if name in KRONOS:
        return KronosModel(name, args.kronos_src, args.samples)
    return Chronos(name)


def load(ticker: str) -> pd.DataFrame:
    df = yf.Ticker(ticker).history(period="10y", auto_adjust=True)[["Open", "High", "Low", "Close", "Volume"]]
    df = df.dropna(subset=["Open", "High", "Low", "Close"])
    return df[(df[["Open", "High", "Low", "Close"]] > 0).all(axis=1)]


def gk_variance(df: pd.DataFrame) -> np.ndarray:
    hl = np.log(df["High"] / df["Low"])
    co = np.log(df["Close"] / df["Open"])
    return np.maximum((0.5 * hl**2 - (2 * math.log(2) - 1) * co**2).to_numpy(), 1e-10)


def pinball(y: np.ndarray, q: np.ndarray, levels: np.ndarray) -> np.ndarray:
    """Mean pinball loss across quantile levels; y (N,), q (N, Q)."""
    diff = y[:, None] - q
    return np.mean(np.maximum(levels * diff, (levels - 1) * diff), axis=1)


def qlike(actual: np.ndarray, forecast: np.ndarray) -> np.ndarray:
    ratio = actual / forecast
    return ratio - np.log(ratio) - 1


def evaluate(model, df: pd.DataFrame) -> dict:
    close = df["Close"].to_numpy()
    var = gk_variance(df)
    n = len(close)
    origins = [o for o in range(n - SPAN, n - max((*HORIZONS, VOL_H))) if (o - (n - SPAN)) % STEP == 0]
    origins = [o for o in origins if o >= CONTEXT]
    hmax = max((*HORIZONS, VOL_H))

    levels = np.array(LEVELS)
    qp, model_var = model.forecast(df, origins, hmax)

    out = {
        "origins": len(origins),
        "first_origin": str(df.index[origins[0]].date()),
        "last_target": str(df.index[origins[-1] + hmax - 1].date()),
    }
    for h in HORIZONS:
        last = close[np.array(origins) - 1]
        actual = np.log(close[np.array(origins) + h - 1] / last)
        model_q = np.log(np.maximum(qp[:, h - 1, :], 1e-12) / last[:, None])
        model_med = model_q[:, list(levels).index(0.5)]
        hist = []
        for o in origins:
            past = np.log(close[o - 500 + h:o] / close[o - 500:o - h])
            hist.append(np.quantile(past, levels))
        hist = np.array(hist)
        drift = np.array([np.mean(np.diff(np.log(close[o - 501:o]))) * h for o in origins])
        out[f"h{h}"] = {
            "mae_drift": float(np.mean(np.abs(actual - drift))),
            "loss_series": {
                "abs_model": np.abs(actual - model_med).tolist(),
                "abs_random_walk": np.abs(actual).tolist(),
                "abs_drift": np.abs(actual - drift).tolist(),
                "pinball_model": pinball(actual, model_q, levels).tolist(),
                "pinball_historical": pinball(actual, hist, levels).tolist(),
            },
            "mae_model": float(np.mean(np.abs(actual - model_med))),
            "mae_random_walk": float(np.mean(np.abs(actual))),
            "pinball_model": float(np.mean(pinball(actual, model_q, levels))),
            "pinball_historical": float(np.mean(pinball(actual, hist, levels))),
            "coverage80_model": float(np.mean((actual >= model_q[:, 0]) & (actual <= model_q[:, -1]))),
            "coverage80_historical": float(np.mean((actual >= hist[:, 0]) & (actual <= hist[:, -1]))),
        }

    realised = np.array([var[o:o + VOL_H].mean() for o in origins])
    ewma = []
    for o in origins:
        w = 0.94 ** np.arange(250)[::-1]
        ewma.append(float(np.sum(w * var[o - 250:o]) / np.sum(w)))
    ewma = np.array(ewma)
    mean60 = np.array([var[o - 60:o].mean() for o in origins])
    out["vol20_loss_series"] = {
        "qlike_model": qlike(realised, model_var).tolist(),
        "qlike_ewma": qlike(realised, ewma).tolist(),
        "sqlog_model": ((np.log(realised) - np.log(model_var)) ** 2).tolist(),
        "sqlog_ewma": ((np.log(realised) - np.log(ewma)) ** 2).tolist(),
    }
    out["vol20"] = {
        "qlike_model": float(np.mean(qlike(realised, model_var))),
        "qlike_ewma": float(np.mean(qlike(realised, ewma))),
        "qlike_mean60": float(np.mean(qlike(realised, mean60))),
        "mse_logvar_model": float(np.mean((np.log(realised) - np.log(model_var)) ** 2)),
        "mse_logvar_ewma": float(np.mean((np.log(realised) - np.log(ewma)) ** 2)),
        "mse_logvar_mean60": float(np.mean((np.log(realised) - np.log(mean60)) ** 2)),
    }
    return out


def summarise(per_ticker: dict) -> dict:
    keys = {}
    for res in per_ticker.values():
        for block, metrics in res.items():
            if isinstance(metrics, dict):
                for k, v in metrics.items():
                    if isinstance(v, (int, float)):
                        keys.setdefault((block, k), []).append(v)
    return {f"{b}.{k}": float(np.mean(v)) for (b, k), v in sorted(keys.items())}


def diebold_mariano(per_ticker: dict, block: str, a: str, b: str, lag: int) -> dict:
    """Mean loss difference a - b across tickers per origin; negative favours a."""
    series = np.mean([res[block][a] for res in per_ticker.values()], axis=0) - np.mean(
        [res[block][b] for res in per_ticker.values()], axis=0
    )
    n, mean = len(series), float(np.mean(series))
    centred = series - mean
    var = float(np.dot(centred, centred) / n)
    for k in range(1, lag + 1):
        var += 2 * (1 - k / (lag + 1)) * float(np.dot(centred[k:], centred[:-k]) / n)
    t = mean / math.sqrt(var / n) if var > 0 else float("nan")
    return {"mean_difference": mean, "t_stat": round(t, 2), "origins": n, "lag": lag}


def tests(per_ticker: dict) -> dict:
    out = {}
    for h in HORIZONS:
        block, lag = f"h{h}", math.ceil(h / STEP)
        for a, b in [("abs_model", "abs_random_walk"), ("abs_model", "abs_drift"), ("pinball_model", "pinball_historical")]:
            sub = {t: {block: r[block]["loss_series"]} for t, r in per_ticker.items()}
            out[f"{block}: {a} vs {b}"] = diebold_mariano(sub, block, a, b, lag)
    lag = math.ceil(VOL_H / STEP)
    for a, b in [("qlike_model", "qlike_ewma"), ("sqlog_model", "sqlog_ewma")]:
        out[f"vol20: {a} vs {b}"] = diebold_mariano(per_ticker, "vol20_loss_series", a, b, lag)
    return out


def all_tests(per_ticker: dict) -> dict:
    """Pooled tests need the same origins for every ticker; per-ticker tests always run."""
    pooled = len({r["origins"] for r in per_ticker.values()}) == 1
    return {
        "pooled": tests(per_ticker) if pooled else "not computed: tickers have different numbers of origins",
        "per_ticker": {t: tests({t: r}) for t, r in per_ticker.items()},
    }


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--models", nargs="+", default=["timestral-28M", "chronos-bolt-base", "chronos-2"])
    parser.add_argument("--tickers", nargs="+", default=UNIVERSE)
    parser.add_argument("--skip-last", type=int, default=0)
    parser.add_argument("--kronos-src", help="path of a clone of github.com/shiyu-coder/Kronos")
    parser.add_argument("--samples", type=int, default=20, help="sampled paths per origin for Kronos")
    parser.add_argument("--hypothesis", default=None, help="written into the report before any result")
    parser.add_argument("--out", required=True)
    args = parser.parse_args()
    data = {t: load(t) for t in args.tickers}
    data = {t: df.iloc[: len(df) - args.skip_last] for t, df in data.items()}
    report = {
        "protocol": __doc__, "universe": args.tickers, "skip_last": args.skip_last,
        "hypothesis": args.hypothesis, "models": {},
    }
    for size in args.models:
        model = make(size, args)
        started = time.time()
        per_ticker = {t: evaluate(model, df) for t, df in data.items()}
        report["models"][size] = {
            "seconds": round(time.time() - started, 1),
            "per_ticker": per_ticker,
            "mean": summarise(per_ticker),
            "diebold_mariano": all_tests(per_ticker),
        }
        print(size, json.dumps(report["models"][size]["diebold_mariano"], indent=1), flush=True)
    with open(args.out, "w") as f:
        json.dump(report, f, indent=1)


if __name__ == "__main__":
    main()
