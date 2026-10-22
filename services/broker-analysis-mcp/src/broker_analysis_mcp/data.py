"""Yahoo Finance access through yfinance.

Prototype only: yfinance reads Yahoo's unofficial endpoints, is meant for personal and research
use, and can break without notice. Every result carries its source and retrieval time so an
analyst can tell where a number came from.
"""

import warnings
from dataclasses import dataclass, field
from datetime import UTC, datetime

import pandas as pd
import yfinance as yf

from .fundamentals import Statements

SOURCE = "Yahoo Finance via yfinance (unofficial, prototype only)"

warnings.filterwarnings("ignore", category=FutureWarning)
warnings.filterwarnings("ignore", module="yfinance")


class DataError(ValueError):
    """The source returned nothing usable for the request."""


@dataclass
class Meta:
    ticker: str
    currency: str
    timezone: str = "?"
    retrieved_at: str = field(default_factory=lambda: datetime.now(UTC).isoformat(timespec="seconds"))
    source: str = SOURCE
    warnings: list[str] = field(default_factory=list)


def _normalise_currency(prices: pd.DataFrame, currency: str) -> tuple[pd.DataFrame, str]:
    # London lines quote in pence (GBp); everything downstream works in pounds.
    if currency == "GBp":
        cols = [c for c in ("Open", "High", "Low", "Close") if c in prices]
        prices = prices.copy()
        prices[cols] = prices[cols] / 100
        return prices, "GBP"
    return prices, currency


def history(ticker: str, period: str = "3y") -> tuple[pd.DataFrame, Meta]:
    """Daily OHLC adjusted for splits and dividends (total-return closes)."""
    t = yf.Ticker(ticker)
    prices = t.history(period=period, interval="1d", auto_adjust=True)
    if prices.empty:
        raise DataError(f"no price history for {ticker!r}")
    prices = prices[["Open", "High", "Low", "Close", "Volume"]].dropna(subset=["Close"])
    prices.index = prices.index.tz_localize(None).normalize()
    meta = t.history_metadata or {}
    currency = meta.get("currency") or t.info.get("currency") or "?"
    prices, currency = _normalise_currency(prices, currency)
    return prices, Meta(ticker=ticker, currency=currency, timezone=meta.get("exchangeTimezoneName") or "?")


def fx_rate(from_ccy: str, to_ccy: str) -> float:
    if from_ccy == to_ccy:
        return 1.0
    pair = f"{from_ccy}{to_ccy}=X"
    closes = yf.Ticker(pair).history(period="5d")["Close"].dropna()
    if closes.empty:
        raise DataError(f"no FX rate for {pair}")
    return float(closes.iloc[-1])


def fx_series(from_ccy: str, to_ccy: str, period: str) -> pd.Series:
    if from_ccy == to_ccy:
        raise ValueError("same currency")
    closes = yf.Ticker(f"{from_ccy}{to_ccy}=X").history(period=period)["Close"].dropna()
    if closes.empty:
        raise DataError(f"no FX history for {from_ccy}{to_ccy}")
    closes.index = closes.index.tz_localize(None).normalize()
    return closes


def info(ticker: str) -> dict:
    data = yf.Ticker(ticker).info or {}
    if not data or data.get("quoteType") is None:
        raise DataError(f"no profile for {ticker!r}")
    return data


def _row(df: pd.DataFrame, names: tuple[str, ...], col: int = 0) -> float | None:
    for name in names:
        if name in df.index and df.shape[1] > col:
            value = df.loc[name].iloc[col]
            if pd.notna(value):
                return float(value)
    return None


def statements(ticker: str) -> tuple[Statements, str, str]:
    """Latest annual statements, their currency and fiscal year end."""
    t = yf.Ticker(ticker)
    inc, bal, cf = t.income_stmt, t.balance_sheet, t.cashflow
    if inc.empty or bal.empty:
        raise DataError(f"no annual statements for {ticker!r}")
    revenue = [_row(inc, ("Total Revenue", "Operating Revenue"), i) for i in range(inc.shape[1])]
    s = Statements(
        revenue=revenue,
        gross_profit=_row(inc, ("Gross Profit",)),
        ebitda=_row(inc, ("EBITDA", "Normalized EBITDA")),
        ebit=_row(inc, ("EBIT", "Operating Income")),
        net_income=_row(inc, ("Net Income Common Stockholders", "Net Income")),
        interest_expense=_row(inc, ("Interest Expense", "Interest Expense Non Operating")),
        tax_rate=_row(inc, ("Tax Rate For Calcs",)),
        equity=_row(bal, ("Stockholders Equity", "Common Stock Equity")),
        equity_prior=_row(bal, ("Stockholders Equity", "Common Stock Equity"), 1),
        total_debt=_row(bal, ("Total Debt",)),
        cash=_row(bal, ("Cash And Cash Equivalents", "Cash Cash Equivalents And Short Term Investments")),
        invested_capital=_row(bal, ("Invested Capital",)),
        free_cash_flow=_row(cf, ("Free Cash Flow",)),
        dividends_paid=_row(cf, ("Cash Dividends Paid", "Common Stock Dividend Paid")),
        diluted_eps=_row(inc, ("Diluted EPS",)),
        total_assets=_row(bal, ("Total Assets",)),
        total_assets_prior=_row(bal, ("Total Assets",), 1),
        current_assets=_row(bal, ("Current Assets",)),
        current_liabilities=_row(bal, ("Current Liabilities",)),
        current_liabilities_prior=_row(bal, ("Current Liabilities",), 1),
        receivables=_row(bal, ("Accounts Receivable", "Receivables")),
        receivables_prior=_row(bal, ("Accounts Receivable", "Receivables"), 1),
        inventory=_row(bal, ("Inventory",)),
        inventory_prior=_row(bal, ("Inventory",), 1),
        payables=_row(bal, ("Accounts Payable", "Payables")),
        payables_prior=_row(bal, ("Accounts Payable", "Payables"), 1),
        total_liabilities=_row(bal, ("Total Liabilities Net Minority Interest",)),
        long_term_debt=_row(bal, ("Long Term Debt",)),
        total_debt_prior=_row(bal, ("Total Debt",), 1),
        short_term_investments=_row(bal, ("Other Short Term Investments",)),
        depreciation=_row(cf, ("Depreciation And Amortization",)) or _row(inc, ("Reconciled Depreciation",)),
        cost_of_revenue=_row(inc, ("Cost Of Revenue", "Reconciled Cost Of Revenue")),
        pretax_income=_row(inc, ("Pretax Income",)),
        operating_cash_flow=_row(cf, ("Operating Cash Flow",)),
    )
    profile = t.info or {}
    currency = profile.get("financialCurrency") or profile.get("currency") or "?"
    return s, currency, str(inc.columns[0].date())
