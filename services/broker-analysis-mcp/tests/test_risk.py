"""Risk formulas against an independent implementation (empyrical), closed forms and Monte Carlo."""

import math

import empyrical as ep
import numpy as np
import pandas as pd
import pytest
from scipy import stats

from broker_analysis_mcp import risk


@pytest.fixture
def returns() -> pd.Series:
    rng = np.random.default_rng(7)
    r = rng.standard_t(df=4, size=1500) * 0.01 + 0.0004
    r[0] = abs(r[0])  # empyrical's drawdown ignores a loss on day one; ours counts it
    return pd.Series(r, index=pd.bdate_range("2020-01-01", periods=1500))


@pytest.fixture
def market(returns: pd.Series) -> pd.Series:
    rng = np.random.default_rng(11)
    return returns * 0.6 + pd.Series(rng.normal(0, 0.008, len(returns)), index=returns.index)


def test_matches_empyrical(returns: pd.Series, market: pd.Series) -> None:
    rf = 0.02
    assert risk.sharpe(returns, rf) == pytest.approx(ep.sharpe_ratio(returns, risk_free=risk.daily_rate(rf)), rel=1e-9)
    assert risk.volatility(returns) == pytest.approx(ep.annual_volatility(returns), rel=1e-9)
    assert risk.cagr(returns) == pytest.approx(ep.annual_return(returns), rel=1e-9)
    assert risk.max_drawdown(returns) == pytest.approx(-ep.max_drawdown(returns), rel=1e-9)
    assert risk.calmar(returns) == pytest.approx(ep.calmar_ratio(returns), rel=1e-9)
    assert risk.sortino(returns, rf) == pytest.approx(
        ep.sortino_ratio(returns, required_return=risk.daily_rate(rf)), rel=1e-9
    )
    assert risk.omega(returns, rf) == pytest.approx(ep.omega_ratio(returns, required_return=rf), rel=1e-9)
    assert risk.beta(returns, market) == pytest.approx(ep.beta(returns, market), rel=1e-9)
    assert risk.var_historical(returns, 0.95) == pytest.approx(-ep.value_at_risk(returns, cutoff=0.05), rel=1e-9)


def test_psr_reduces_to_lo_standard_error_under_normality() -> None:
    # With skew 0 and raw kurtosis 3 the PSR denominator is Lo's sqrt(1 + SR^2 / 2).
    sr, skew, kurt = 0.08, 0.0, 3.0
    assert math.sqrt(1 - skew * sr + (kurt - 1) / 4 * sr**2) == pytest.approx(math.sqrt(1 + sr**2 / 2))


def test_psr_is_one_half_at_the_benchmark(returns: pd.Series) -> None:
    sr_annual = risk.sharpe(returns)
    assert risk.probabilistic_sharpe(returns, sr_annual) == pytest.approx(0.5, abs=1e-12)
    assert risk.probabilistic_sharpe(returns, sr_annual - 0.5) > 0.5 > risk.probabilistic_sharpe(returns, sr_annual + 0.5)


def test_psr_matches_monte_carlo_frequency() -> None:
    # Gaussian returns with true annual SR 0.5: PSR(SR*=0) should approximate the share of
    # samples whose estimated Sharpe ratio is positive, P(SR_hat > 0).
    rng = np.random.default_rng(3)
    mu, sigma, n = 0.5 / math.sqrt(252) * 0.01, 0.01, 504
    sample = pd.Series(rng.normal(mu, sigma, n))
    estimated = np.mean([rng.normal(mu, sigma, n).mean() > 0 for _ in range(4000)])
    expected = stats.norm.cdf((mu / sigma) * math.sqrt(n - 1) / math.sqrt(1 + (mu / sigma) ** 2 / 2))
    assert estimated == pytest.approx(expected, abs=0.03)
    assert 0 < risk.probabilistic_sharpe(sample) < 1


def test_cornish_fisher_is_gaussian_without_higher_moments() -> None:
    z = stats.norm.ppf(0.05)
    assert risk.cornish_fisher_quantile(z, 0.0, 0.0) == z
    assert risk.cornish_fisher_quantile(z, -1.0, 0.0) < z  # left skew fattens the loss tail
    # The kurtosis term (z^3 - 3z) K / 24 changes sign at |z| = sqrt(3), about 95.8%:
    # fat tails shrink the 95% quantile and widen the 99% one.
    assert risk.cornish_fisher_quantile(z, 0.0, 3.0) > z
    z99 = stats.norm.ppf(0.01)
    assert risk.cornish_fisher_quantile(z99, 0.0, 3.0) < z99


def test_expected_shortfall_exceeds_var(returns: pd.Series) -> None:
    assert risk.expected_shortfall(returns, 0.95) > risk.var_historical(returns, 0.95) > 0


def test_cdar_hand_example() -> None:
    r = pd.Series([0.10, -0.20, 0.05, -0.10, 0.30])
    dd = risk.drawdowns(r).round(6).tolist()
    assert dd == pytest.approx([0.0, 0.2, 0.16, 0.244, 0.0172], abs=1e-6)
    assert risk.cdar(r, 0.6) == pytest.approx((0.244 + 0.2) / 2, abs=1e-6)
    assert risk.max_drawdown(r) == pytest.approx(0.244, abs=1e-9)


def test_tracking_of_a_fund_that_loses_a_fixed_daily_cost(returns: pd.Series) -> None:
    cost = 0.002 / 252  # 20 bp a year charged daily
    bench = (1 + returns).cumprod()
    fund = ((1 + returns) * (1 - cost)).cumprod()
    t = risk.tracking(fund, bench)
    bench_cagr = risk.cagr(returns.iloc[1:])
    expected = (1 + bench_cagr) * (1 - cost) ** 252 - 1 - bench_cagr
    assert t["tracking_difference"] == pytest.approx(expected, rel=1e-6)
    assert t["tracking_difference"] == pytest.approx(-0.002 * (1 + bench_cagr), rel=0.01)
    assert t["tracking_error"] < 1e-4
    assert risk.tracking(bench, bench)["tracking_error"] == 0


def test_weekly_tracking_error_ignores_a_one_day_close_offset(returns: pd.Series) -> None:
    # The same index observed one session late (a different market close) is not a tracking
    # error; weekly returns keep it small while daily differences would be large.
    bench = (1 + returns).cumprod()
    late = bench.shift(1).dropna()
    daily_noise = float((late.pct_change() - bench.pct_change()).dropna().std()) * math.sqrt(252)
    weekly = risk.tracking(late, bench)["tracking_error"]
    assert weekly < daily_noise / 2


def _gbm_bars(sigma: float, days: int, steps: int, seed: int) -> pd.DataFrame:
    """Daily OHLC bars sampled from a driftless geometric Brownian motion."""
    rng = np.random.default_rng(seed)
    increments = rng.normal(-0.5 * sigma**2 / (252 * steps), sigma / math.sqrt(252 * steps), (days, steps))
    paths = np.cumsum(increments, axis=1)
    opens = np.concatenate([[0.0], paths[:-1, -1]]).cumsum()
    log_path = opens[:, None] + np.concatenate([np.zeros((days, 1)), paths], axis=1)
    return pd.DataFrame(
        {
            "Open": np.exp(log_path[:, 0]),
            "High": np.exp(log_path.max(axis=1)),
            "Low": np.exp(log_path.min(axis=1)),
            "Close": np.exp(log_path[:, -1]),
        }
    )


@pytest.mark.parametrize("estimator", [risk.parkinson, risk.garman_klass, risk.rogers_satchell])
def test_range_estimators_recover_known_volatility(estimator) -> None:
    # A path sampled in finitely many steps understates the true high-low range (the bias
    # shrinks like 1/sqrt(steps)); 20,000 steps a day keep it under 1.5%.
    bars = _gbm_bars(sigma=0.25, days=300, steps=20_000, seed=5)
    assert estimator(bars) == pytest.approx(0.25, rel=0.04)


def test_garman_klass_is_more_efficient_than_close_to_close() -> None:
    gk, cc = [], []
    for seed in range(60):
        bars = _gbm_bars(sigma=0.2, days=20, steps=390, seed=100 + seed)
        gk.append(risk.garman_klass(bars))
        cc.append(math.sqrt(float((np.log(bars["Close"] / bars["Open"]) ** 2).mean()) * 252))
    # arXiv:2312.01426 reports efficiency 7.4 for GK; with 60 samples demand at least 3.
    assert np.var(cc) / np.var(gk) > 3


def test_parkinson_single_bar_golden_value() -> None:
    bar = pd.DataFrame({"Open": [100.0], "High": [102.0], "Low": [99.0], "Close": [101.0]})
    expected = math.sqrt(math.log(102 / 99) ** 2 / (4 * math.log(2)) * 252)
    assert risk.parkinson(bar) == pytest.approx(expected, rel=1e-12)
