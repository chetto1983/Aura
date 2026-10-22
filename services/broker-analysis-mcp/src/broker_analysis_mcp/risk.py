"""Risk and performance measures on daily return series. Pure functions, no I/O."""

import math

import numpy as np
import pandas as pd
from scipy import stats

PERIODS = 252


def simple_returns(prices: pd.Series) -> pd.Series:
    return prices.pct_change().dropna()


def daily_rate(annual_rate: float) -> float:
    return (1 + annual_rate) ** (1 / PERIODS) - 1


def cagr(returns: pd.Series) -> float:
    growth = float((1 + returns).prod())
    years = len(returns) / PERIODS
    return growth ** (1 / years) - 1


def volatility(returns: pd.Series) -> float:
    return float(returns.std(ddof=1)) * math.sqrt(PERIODS)


def sharpe(returns: pd.Series, rf_annual: float = 0.0) -> float:
    excess = returns - daily_rate(rf_annual)
    return float(excess.mean() / excess.std(ddof=1)) * math.sqrt(PERIODS)


def sharpe_standard_error(sr_annual: float, n: int) -> float:
    """Lo (2002) Gaussian standard error of the annualised Sharpe ratio."""
    sr = sr_annual / math.sqrt(PERIODS)
    return math.sqrt((1 + sr**2 / 2) / n) * math.sqrt(PERIODS)


def probabilistic_sharpe(
    returns: pd.Series, benchmark_sr_annual: float = 0.0, rf_annual: float = 0.0
) -> float:
    """Probability that the true Sharpe ratio exceeds the benchmark one (Bailey & Lopez de Prado)."""
    excess = returns - daily_rate(rf_annual)
    sr = float(excess.mean() / excess.std(ddof=1))
    sr_star = benchmark_sr_annual / math.sqrt(PERIODS)
    skew = float(stats.skew(excess))
    kurt = float(stats.kurtosis(excess, fisher=False))
    denominator = math.sqrt(1 - skew * sr + (kurt - 1) / 4 * sr**2)
    z = (sr - sr_star) * math.sqrt(len(excess) - 1) / denominator
    return float(stats.norm.cdf(z))


def downside_deviation(returns: pd.Series, mar_annual: float = 0.0) -> float:
    shortfall = np.minimum(returns - daily_rate(mar_annual), 0.0)
    return math.sqrt(float(np.mean(shortfall**2))) * math.sqrt(PERIODS)


def sortino(returns: pd.Series, mar_annual: float = 0.0) -> float:
    excess_mean = float((returns - daily_rate(mar_annual)).mean()) * PERIODS
    return excess_mean / downside_deviation(returns, mar_annual)


def omega(returns: pd.Series, threshold_annual: float = 0.0) -> float:
    excess = returns - daily_rate(threshold_annual)
    gains = float(excess[excess > 0].sum())
    losses = float(-excess[excess < 0].sum())
    return gains / losses if losses > 0 else math.inf


def drawdowns(returns: pd.Series) -> pd.Series:
    """Drawdown at each date as a positive fraction of the running peak."""
    wealth = (1 + returns).cumprod()
    peak = np.maximum(wealth.cummax(), 1.0)
    return 1 - wealth / peak


def max_drawdown(returns: pd.Series) -> float:
    return float(drawdowns(returns).max())


def cdar(returns: pd.Series, alpha: float = 0.95) -> float:
    """Mean of the worst (1 - alpha) share of drawdowns (CVaR of the drawdown series)."""
    dd = np.sort(drawdowns(returns).to_numpy())[::-1]
    tail = max(1, math.ceil(len(dd) * (1 - alpha)))
    return float(dd[:tail].mean())


def calmar(returns: pd.Series) -> float:
    mdd = max_drawdown(returns)
    return cagr(returns) / mdd if mdd > 0 else math.inf


def var_historical(returns: pd.Series, alpha: float = 0.95) -> float:
    """One-day VaR as a positive loss: minus the (1 - alpha) quantile of returns."""
    return -float(np.percentile(returns, 100 * (1 - alpha)))


def expected_shortfall(returns: pd.Series, alpha: float = 0.95) -> float:
    cutoff = -var_historical(returns, alpha)
    return -float(returns[returns <= cutoff].mean())


def cornish_fisher_quantile(z: float, skew: float, excess_kurtosis: float) -> float:
    return (
        z
        + (z**2 - 1) * skew / 6
        + (z**3 - 3 * z) * excess_kurtosis / 24
        - (2 * z**3 - 5 * z) * skew**2 / 36
    )


def var_cornish_fisher(returns: pd.Series, alpha: float = 0.95) -> float:
    z = float(stats.norm.ppf(1 - alpha))
    z_cf = cornish_fisher_quantile(
        z, float(stats.skew(returns)), float(stats.kurtosis(returns, fisher=True))
    )
    return -(float(returns.mean()) + z_cf * float(returns.std(ddof=1)))


def beta(returns: pd.Series, market: pd.Series) -> float:
    aligned = pd.concat([returns, market], axis=1, join="inner").dropna()
    cov = np.cov(aligned.iloc[:, 0], aligned.iloc[:, 1], ddof=1)
    return float(cov[0, 1] / cov[1, 1])


def correlation(returns: pd.Series, other: pd.Series) -> float:
    aligned = pd.concat([returns, other], axis=1, join="inner").dropna()
    return float(aligned.iloc[:, 0].corr(aligned.iloc[:, 1]))


def tracking(fund_prices: pd.Series, benchmark_prices: pd.Series) -> dict[str, float]:
    """Tracking difference from aligned daily prices; tracking error from weekly returns.

    Daily returns of markets that close at different times (a Milan ETF against a US index)
    are not synchronous, and their differences inflate the tracking error with noise the fund
    never had; Friday-to-Friday returns absorb most of that offset.
    """
    aligned = pd.concat([fund_prices, benchmark_prices], axis=1, join="inner").dropna()
    daily = aligned.pct_change().dropna()
    weekly = aligned.resample("W-FRI").last().pct_change().dropna()
    diff = weekly.iloc[:, 0] - weekly.iloc[:, 1]
    return {
        "tracking_difference": cagr(daily.iloc[:, 0]) - cagr(daily.iloc[:, 1]),
        "tracking_error": float(diff.std(ddof=1)) * math.sqrt(52),
        "observations": float(len(daily)),
        "weeks": float(len(weekly)),
    }


def _annualised(variance: pd.Series) -> float:
    return math.sqrt(float(variance.mean()) * PERIODS)


def parkinson(ohlc: pd.DataFrame) -> float:
    hl = np.log(ohlc["High"] / ohlc["Low"])
    return _annualised(hl**2 / (4 * math.log(2)))


def garman_klass(ohlc: pd.DataFrame) -> float:
    hl = np.log(ohlc["High"] / ohlc["Low"])
    co = np.log(ohlc["Close"] / ohlc["Open"])
    return _annualised(0.5 * hl**2 - (2 * math.log(2) - 1) * co**2)


def rogers_satchell(ohlc: pd.DataFrame) -> float:
    ho = np.log(ohlc["High"] / ohlc["Open"])
    hc = np.log(ohlc["High"] / ohlc["Close"])
    lo = np.log(ohlc["Low"] / ohlc["Open"])
    lc = np.log(ohlc["Low"] / ohlc["Close"])
    return _annualised(ho * hc + lo * lc)
