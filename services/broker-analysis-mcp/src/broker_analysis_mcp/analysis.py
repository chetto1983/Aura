"""Joins data and formulas into the reports the MCP tools return.

Every metric carries its value, unit and the key of the formula it came from (references.py),
so the agent can quote a number with its method and never has to compute one itself.
"""

import math
from datetime import datetime, timedelta
from zoneinfo import ZoneInfo, ZoneInfoNotFoundError

import pandas as pd

from . import data, indicators, risk, scores
from .fundamentals import CapitalInputs, Year, cost_of_capital, interest_warning, ratios
from .references import REFERENCES


def _metric(value: float | None, unit: str, ref: str, digits: int = 4) -> dict:
    if value is None or (isinstance(value, float) and not math.isfinite(value)):
        return {"value": None, "unit": unit, "ref": ref}
    scaled = value * 100 if unit == "%" else value
    return {"value": round(scaled, digits), "unit": unit, "ref": ref}


def _window(prices: pd.DataFrame) -> dict:
    return {"start": str(prices.index[0].date()), "end": str(prices.index[-1].date()), "days": len(prices)}


def _in_currency(prices: pd.DataFrame, meta: data.Meta, currency: str, period: str) -> pd.DataFrame:
    """Converts a price frame into another currency with daily Yahoo FX closes."""
    if meta.currency == currency:
        return prices
    fx = data.fx_series(meta.currency, currency, period).reindex(prices.index).ffill()
    converted = prices.copy()
    for col in ("Open", "High", "Low", "Close"):
        converted[col] = converted[col] * fx
    meta.warnings.append(f"{meta.ticker} converted from {meta.currency} to {currency} with daily FX closes")
    return converted.dropna(subset=["Close"])


def utc_offset(timezone: str) -> timedelta | None:
    try:
        return datetime.now(ZoneInfo(timezone)).utcoffset()
    except (ZoneInfoNotFoundError, ValueError):
        return None


def normalise_quote(price: float, market_cap: float, currency: str) -> tuple[float, float, str]:
    """London lines: Yahoo quotes the price in pence (GBp) but the market cap in pounds."""
    if currency == "GBp":
        return price / 100, market_cap, "GBP"
    return price, market_cap, currency


def references(keys: set[str]) -> dict[str, str]:
    return {k: REFERENCES[k] for k in sorted(keys)}


def _rounded(obj, digits: int = 4):
    if isinstance(obj, dict):
        return {k: _rounded(v, digits) for k, v in obj.items()}
    return round(obj, digits) if isinstance(obj, float) else obj


def _benchmark_metrics(close: pd.Series, bench_close: pd.Series, rf_annual: float, warnings: list[str]) -> dict:
    r, b = risk.simple_returns(close), risk.simple_returns(bench_close)
    prices = risk.aligned(close, bench_close)
    weekly = risk.weekly_returns(prices)
    beta_weeks = weekly.iloc[-risk.BETA_WEEKS:]
    if len(beta_weeks) < risk.BETA_WEEKS:
        warnings.append(
            f"beta_weekly_2y uses {len(beta_weeks)} weekly returns instead of {risk.BETA_WEEKS}: "
            "request period 2y or longer to match Bloomberg's default window"
        )
    raw = risk.beta(beta_weeks.iloc[:, 0], beta_weeks.iloc[:, 1])
    monthly = risk.monthly_returns(prices)
    capm = risk.capm_measures(r, b, rf_annual)
    capture = risk.capture_ratios(monthly.iloc[:, 0], monthly.iloc[:, 1])
    return {
        "beta": _metric(risk.beta(r, b), "ratio", "beta"),
        "correlation": _metric(risk.correlation(r, b), "ratio", "beta"),
        "beta_weekly_2y": _metric(raw, "ratio", "beta_adjusted"),
        "beta_adjusted": _metric(risk.adjusted_beta(raw), "ratio", "beta_adjusted"),
        "weeks_for_beta": _metric(len(beta_weeks), "weeks", "beta_adjusted", 0),
        "jensen_alpha": _metric(capm["jensen_alpha"], "%", "capm"),
        "treynor": _metric(capm["treynor"], "%", "capm"),
        "m_squared": _metric(capm["m_squared"], "%", "capm"),
        "m_squared_vs_benchmark": _metric(capm["m_squared_vs_benchmark"], "%", "capm"),
        "information_ratio": _metric(
            risk.information_ratio(weekly.iloc[:, 0], weekly.iloc[:, 1]), "ratio", "information_ratio"
        ),
        "tracking_error": _metric(risk.tracking(close, bench_close)["tracking_error"], "%", "information_ratio"),
        "up_capture": _metric(capture["up_capture"], "%", "capture"),
        "down_capture": _metric(capture["down_capture"], "%", "capture"),
        "up_months": _metric(capture["up_months"], "months", "capture", 0),
        "down_months": _metric(capture["down_months"], "months", "capture", 0),
    }


def risk_report(
    ticker: str,
    period: str = "3y",
    benchmark: str | None = None,
    rf_annual: float = 0.0,
    alpha: float = 0.95,
) -> dict:
    prices, meta = data.history(ticker, period)
    r = risk.simple_returns(prices["Close"])
    sr = risk.sharpe(r, rf_annual)
    se = risk.sharpe_standard_error(sr, len(r))
    metrics = {
        "cagr": _metric(risk.cagr(r), "%", "calmar"),
        "volatility_close_to_close": _metric(risk.volatility(r), "%", "sharpe"),
        "volatility_garman_klass": _metric(risk.garman_klass(prices), "%", "garman_klass"),
        "volatility_parkinson": _metric(risk.parkinson(prices), "%", "parkinson"),
        "volatility_rogers_satchell": _metric(risk.rogers_satchell(prices), "%", "rogers_satchell"),
        "sharpe": _metric(sr, "ratio", "sharpe"),
        "sharpe_standard_error": _metric(se, "ratio", "sharpe"),
        "sharpe_t_stat": _metric(sr / se, "standard errors from zero", "sharpe"),
        "probabilistic_sharpe_vs_0": _metric(risk.probabilistic_sharpe(r, 0.0, rf_annual), "probability", "psr"),
        "sortino": _metric(risk.sortino(r, rf_annual), "ratio", "sortino"),
        "omega": _metric(risk.omega(r, rf_annual), "ratio", "omega"),
        "max_drawdown": _metric(risk.max_drawdown(r), "%", "max_drawdown"),
        "cdar": _metric(risk.cdar(r, alpha), "%", "cdar"),
        "calmar": _metric(risk.calmar(r), "ratio", "calmar"),
        "var_historical_1d": _metric(risk.var_historical(r, alpha), "%", "var_historical"),
        "expected_shortfall_1d": _metric(risk.expected_shortfall(r, alpha), "%", "var_historical"),
        "var_cornish_fisher_1d": _metric(risk.var_cornish_fisher(r, alpha), "%", "var_cornish_fisher"),
    }
    if benchmark:
        bench_prices, bench_meta = data.history(benchmark, period)
        bench_prices = _in_currency(bench_prices, bench_meta, meta.currency, period)
        meta.warnings.extend(bench_meta.warnings)
        metrics |= _benchmark_metrics(prices["Close"], bench_prices["Close"], rf_annual, meta.warnings)
    return {
        "ticker": ticker,
        "currency": meta.currency,
        "window": _window(prices),
        "parameters": {"risk_free_annual": rf_annual, "confidence": alpha, "benchmark": benchmark},
        "metrics": metrics,
        "references": references({m["ref"] for m in metrics.values()}),
        "source": meta.source,
        "retrieved_at": meta.retrieved_at,
        "warnings": meta.warnings,
    }


TECHNICAL_DEFAULTS = dict(
    sma_fast=50, sma_slow=200, rsi_period=14, macd_fast=12, macd_slow=26, macd_signal=9,
    bollinger_n=20, bollinger_k=2.0, atr_period=14,
)


def technical_report(ticker: str, period: str = "1y", params: dict | None = None) -> dict:
    p = TECHNICAL_DEFAULTS | (params or {})
    prices, meta = data.history(ticker, period)
    close = prices["Close"]
    last = float(close.iloc[-1])
    fast, slow = p["sma_fast"], p["sma_slow"]
    m = indicators.macd(close, p["macd_fast"], p["macd_slow"], p["macd_signal"]).iloc[-1]
    bands = indicators.bollinger(close, p["bollinger_n"], p["bollinger_k"]).iloc[-1]
    sma_f, sma_s = indicators.sma(close, fast).iloc[-1], indicators.sma(close, slow).iloc[-1]
    atr = float(indicators.atr(prices, p["atr_period"]).iloc[-1])
    high, low = float(prices["High"].iloc[-252:].max()), float(prices["Low"].iloc[-252:].min())
    rsi_key, atr_key = f"rsi{p['rsi_period']}", f"atr{p['atr_period']}"
    metrics = {
        "close": _metric(last, meta.currency, "price"),
        f"sma{fast}": _metric(float(sma_f), meta.currency, "sma"),
        f"sma{slow}": _metric(float(sma_s), meta.currency, "sma"),
        f"price_vs_sma{fast}": _metric(last / sma_f - 1 if pd.notna(sma_f) else None, "%", "sma"),
        f"price_vs_sma{slow}": _metric(last / sma_s - 1 if pd.notna(sma_s) else None, "%", "sma"),
        rsi_key: _metric(float(indicators.rsi(close, p["rsi_period"]).iloc[-1]), "index 0-100", "rsi"),
        "macd": _metric(float(m["macd"]), meta.currency, "macd"),
        "macd_signal": _metric(float(m["signal"]), meta.currency, "macd"),
        "macd_histogram": _metric(float(m["histogram"]), meta.currency, "macd"),
        "bollinger_upper": _metric(float(bands["upper"]), meta.currency, "bollinger"),
        "bollinger_lower": _metric(float(bands["lower"]), meta.currency, "bollinger"),
        "price_vs_bollinger_upper": _metric(last / float(bands["upper"]) - 1, "%", "bollinger"),
        "price_vs_bollinger_lower": _metric(last / float(bands["lower"]) - 1, "%", "bollinger"),
        atr_key: _metric(atr, meta.currency, "atr"),
        f"{atr_key}_pct_of_close": _metric(atr / last, "%", "atr"),
        "high_52w": _metric(high, meta.currency, "price"),
        "low_52w": _metric(low, meta.currency, "price"),
        "price_vs_high_52w": _metric(last / high - 1, "%", "price"),
        "price_vs_low_52w": _metric(last / low - 1, "%", "price"),
    }
    if pd.isna(sma_s):
        meta.warnings.append(f"fewer than {slow} sessions: sma{slow} unavailable, request a longer period")
    return {
        "ticker": ticker,
        "currency": meta.currency,
        "window": _window(prices),
        "parameters": p,
        "metrics": metrics,
        "references": references({v["ref"] for v in metrics.values()}),
        "source": meta.source,
        "retrieved_at": meta.retrieved_at,
        "warnings": meta.warnings,
    }


PCT_RATIOS = {
    "fcf_yield", "roe", "roa", "ebit_roa", "roic", "gross_margin", "ebitda_margin", "operating_margin",
    "pretax_margin", "net_margin", "payout", "revenue_cagr", "cost_of_equity", "cost_of_debt_pretax", "wacc",
}
MONEY = {"enterprise_value", "net_debt", "working_capital", "eva"}
DAY_RATIOS = {"days_sales_outstanding", "days_inventory", "days_payables", "cash_conversion_cycle"}


def _unit(name: str, currency: str) -> str:
    if name in PCT_RATIOS:
        return "%"
    if name in MONEY:
        return currency
    return "days" if name in DAY_RATIOS else "ratio"


# Yahoo's sector for banks and insurers, whose balance sheets these scores do not describe.
FINANCIAL_SECTORS = {"Financial Services"}


def _scores(years: list[Year], market_cap: float, sector: str | None, warnings: list[str]) -> tuple[dict, dict]:
    if sector in FINANCIAL_SECTORS:
        warnings.append("Piotroski, Altman and Beneish scores are not computed for banks and insurers")
        return {}, {}
    f, z, m = scores.piotroski(years), scores.altman(years, market_cap), scores.beneish(years)
    for name, result in (("Piotroski F-score", f), ("Altman Z", z), ("Beneish M-score", m)):
        if result["missing"]:
            warnings.append(f"{name}: no value for {', '.join(result['missing'])}")
    metrics = {
        "piotroski_f_score": _metric(f["score"], "points of 9", "piotroski", 0),
        "altman_z": _metric(z["z"], "score", "altman", 2),
        "altman_z_double_prime": _metric(z["z_double_prime"], "score", "altman", 2),
        "beneish_m": _metric(m["m"], "score", "beneish", 2),
    }
    return metrics, _rounded({"piotroski": f, "altman": z, "beneish": m})


def fundamentals_report(
    ticker: str,
    risk_free_annual: float | None = None,
    equity_risk_premium: float | None = None,
    beta: float | None = None,
) -> dict:
    profile = data.info(ticker)
    years, fin_ccy, fiscal_end = data.statements(ticker)
    price_ccy = profile.get("currency") or "?"
    price = profile.get("currentPrice") or profile.get("regularMarketPrice")
    market_cap = profile.get("marketCap")
    if price is None or market_cap is None:
        raise data.DataError(f"no price or market cap for {ticker!r}")
    warnings = []
    price, market_cap, price_ccy = normalise_quote(float(price), float(market_cap), price_ccy)
    if price_ccy != fin_ccy:
        fx = data.fx_rate(price_ccy, fin_ccy)
        price, market_cap = price * fx, market_cap * fx
        warnings.append(
            f"market values converted from {price_ccy} to statement currency {fin_ccy} at {fx:.4f}; "
            "for depositary receipts check the share ratio before trusting per-share ratios"
        )
    values = ratios(years, float(price), float(market_cap))
    if warning := interest_warning(years):
        warnings.append(warning)
    parameters = {"risk_free_annual": risk_free_annual, "equity_risk_premium": equity_risk_premium, "beta": beta}
    if risk_free_annual is not None and equity_risk_premium is not None:
        used_beta = beta if beta is not None else profile.get("beta")
        if used_beta is None:
            warnings.append("no beta from Yahoo: pass beta to compute the cost of capital")
        else:
            parameters["beta"] = used_beta
            parameters["beta_source"] = "analyst" if beta is not None else "Yahoo Finance profile"
            values |= cost_of_capital(years, float(market_cap), CapitalInputs(risk_free_annual, equity_risk_premium, used_beta))
    else:
        warnings.append("cost of equity, WACC and EVA need risk_free_annual and equity_risk_premium from the analyst")
    metrics = {k: _metric(v, _unit(k, fin_ccy), "fundamentals", 2 if k in MONEY else 4) for k, v in values.items()}
    metrics["yahoo_trailing_pe_ttm"] = _metric(profile.get("trailingPE"), "ratio", "fundamentals")
    metrics["yahoo_dividend_yield"] = _metric(
        (profile.get("dividendYield") or 0) / 100 if profile.get("dividendYield") else None, "%", "fundamentals"
    )
    score_metrics, score_details = _scores(years, float(market_cap), profile.get("sector"), warnings)
    metrics |= score_metrics
    return {
        "ticker": ticker,
        "name": profile.get("longName") or profile.get("shortName"),
        "sector": profile.get("sector"),
        "industry": profile.get("industry"),
        "statement_currency": fin_ccy,
        "fiscal_year_end": fiscal_end,
        "basis": "latest annual statements, averages of opening and closing balances where the ratio uses them; "
        "yahoo_* fields are Yahoo's own trailing figures; scores compare the latest two fiscal years (Piotroski "
        "also uses total assets of the third) and Altman's market-value X4 uses today's market capitalisation",
        "parameters": parameters,
        "metrics": metrics,
        "scores": score_details,
        "references": references({m["ref"] for m in metrics.values()}),
        "source": data.SOURCE,
        "warnings": warnings,
    }


def peer_report(tickers: list[str]) -> dict:
    """One fundamentals report per company, with references once and without score components,
    which would push a ten-company answer past an MCP client's tool-result limit."""
    rows, errors, refs = {}, {}, {}
    for ticker in tickers:
        try:
            report = fundamentals_report(ticker)
        except data.DataError as err:
            errors[ticker] = str(err)
            continue
        refs |= report.pop("references")
        report.pop("scores")
        rows[ticker] = report
    return {"companies": rows, "errors": errors, "references": refs}


def etf_report(ticker: str, benchmark: str, period: str = "3y") -> dict:
    profile = data.info(ticker)
    if profile.get("quoteType") != "ETF":
        raise data.DataError(f"{ticker!r} is a {profile.get('quoteType')}, not an ETF")
    fund_prices, meta = data.history(ticker, period)
    bench_prices, bench_meta = data.history(benchmark, period)
    bench_prices = _in_currency(bench_prices, bench_meta, meta.currency, period)
    t = risk.tracking(fund_prices["Close"], bench_prices["Close"])
    ter = profile.get("netExpenseRatio")
    metrics = {
        "ter": _metric(ter / 100 if ter is not None else None, "%", "tracking"),
        "tracking_difference": _metric(t["tracking_difference"], "%", "tracking"),
        "tracking_error": _metric(t["tracking_error"], "%", "tracking"),
        "aligned_days": _metric(t["observations"], "days", "tracking", 0),
        "weeks_for_tracking_error": _metric(t["weeks"], "weeks", "tracking", 0),
        "total_assets": _metric(profile.get("totalAssets"), profile.get("currency") or "?", "tracking", 0),
    }
    warnings = meta.warnings + bench_meta.warnings
    if utc_offset(meta.timezone) != utc_offset(bench_meta.timezone):
        warnings.append(
            f"fund closes in {meta.timezone}, benchmark in {bench_meta.timezone}: asynchronous closes "
            "inflate the tracking error even on weekly returns (measured: SWDA.MI vs URTH 6.5% weekly, "
            "vs IWDA.AS 0.2%); use a benchmark listed in the same time zone for tracking error"
        )
    warnings.append(
        "tracking difference uses Yahoo total-return closes; if the benchmark is a price index "
        "(dividends excluded) the difference is biased upwards by the dividend yield"
    )
    return {
        "ticker": ticker,
        "name": profile.get("longName") or profile.get("shortName"),
        "fund_family": profile.get("fundFamily"),
        "currency": meta.currency,
        "benchmark": benchmark,
        "window": _window(fund_prices),
        "metrics": metrics,
        "references": references({"tracking"}),
        "source": meta.source,
        "retrieved_at": meta.retrieved_at,
        "warnings": warnings,
    }
