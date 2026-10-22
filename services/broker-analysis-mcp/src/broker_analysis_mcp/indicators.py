"""Technical indicators on price series. Pure functions, no I/O."""

import pandas as pd


def sma(close: pd.Series, n: int) -> pd.Series:
    return close.rolling(n, min_periods=n).mean()


def ema(close: pd.Series, n: int) -> pd.Series:
    return close.ewm(span=n, adjust=False, min_periods=n).mean()


def wilder_smooth(values: pd.Series, n: int) -> pd.Series:
    """Wilder's smoothing: seeded with the mean of the first n values, then
    avg_t = (avg_{t-1} * (n - 1) + x_t) / n. Values must start at the first valid observation."""
    out = pd.Series(float("nan"), index=values.index)
    if len(values) < n:
        return out
    avg = float(values.iloc[:n].mean())
    out.iloc[n - 1] = avg
    for i in range(n, len(values)):
        avg = (avg * (n - 1) + float(values.iloc[i])) / n
        out.iloc[i] = avg
    return out


def rsi(close: pd.Series, n: int = 14) -> pd.Series:
    delta = close.diff().iloc[1:]
    gain = wilder_smooth(delta.clip(lower=0), n)
    loss = wilder_smooth(-delta.clip(upper=0), n)
    return (100 - 100 / (1 + gain / loss)).reindex(close.index)


def macd(close: pd.Series, fast: int = 12, slow: int = 26, signal: int = 9) -> pd.DataFrame:
    line = ema(close, fast) - ema(close, slow)
    signal_line = line.ewm(span=signal, adjust=False, min_periods=signal).mean()
    return pd.DataFrame({"macd": line, "signal": signal_line, "histogram": line - signal_line})


def bollinger(close: pd.Series, n: int = 20, k: float = 2.0) -> pd.DataFrame:
    mid = sma(close, n)
    sd = close.rolling(n, min_periods=n).std(ddof=0)
    return pd.DataFrame({"middle": mid, "upper": mid + k * sd, "lower": mid - k * sd})


def true_range(ohlc: pd.DataFrame) -> pd.Series:
    """max(H - L, |H - C_prev|, |L - C_prev|); on the first day, with no previous close, H - L."""
    prev_close = ohlc["Close"].shift(1)
    ranges = pd.concat(
        [
            ohlc["High"] - ohlc["Low"],
            (ohlc["High"] - prev_close).abs(),
            (ohlc["Low"] - prev_close).abs(),
        ],
        axis=1,
    )
    return ranges.max(axis=1)


def atr(ohlc: pd.DataFrame, n: int = 14) -> pd.Series:
    return wilder_smooth(true_range(ohlc), n)
