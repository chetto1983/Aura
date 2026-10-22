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

from .fundamentals import Year

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


# Statement lines by Year field: the first (statement, row) pair Yahoo reports wins.
ROWS: dict[str, tuple[tuple[str, str], ...]] = {
    "revenue": (("inc", "Total Revenue"), ("inc", "Operating Revenue")),
    "cost_of_revenue": (("inc", "Cost Of Revenue"), ("inc", "Reconciled Cost Of Revenue")),
    "gross_profit": (("inc", "Gross Profit"),),
    "sga": (("inc", "Selling General And Administration"),),
    "ebitda": (("inc", "EBITDA"), ("inc", "Normalized EBITDA")),
    "ebit": (("inc", "EBIT"), ("inc", "Operating Income")),
    "pretax_income": (("inc", "Pretax Income"),),
    "net_income": (("inc", "Net Income Common Stockholders"), ("inc", "Net Income")),
    "net_income_continuing": (
        ("inc", "Net Income From Continuing Operation Net Minority Interest"),
        ("inc", "Net Income Continuous Operations"),
    ),
    "interest_expense": (("inc", "Interest Expense"), ("inc", "Interest Expense Non Operating")),
    "tax_rate": (("inc", "Tax Rate For Calcs"),),
    "diluted_eps": (("inc", "Diluted EPS"),),
    "total_assets": (("bal", "Total Assets"),),
    "current_assets": (("bal", "Current Assets"),),
    "current_liabilities": (("bal", "Current Liabilities"),),
    "cash": (("bal", "Cash And Cash Equivalents"), ("bal", "Cash Cash Equivalents And Short Term Investments")),
    "short_term_investments": (("bal", "Other Short Term Investments"),),
    "receivables": (("bal", "Accounts Receivable"), ("bal", "Receivables")),
    "inventory": (("bal", "Inventory"),),
    "payables": (("bal", "Accounts Payable"), ("bal", "Payables")),
    "net_ppe": (("bal", "Net PPE"),),
    "total_debt": (("bal", "Total Debt"),),
    "long_term_debt": (("bal", "Long Term Debt"),),
    "current_debt": (("bal", "Current Debt"),),
    "tax_payable": (("bal", "Income Tax Payable"), ("bal", "Total Tax Payable")),
    "total_liabilities": (("bal", "Total Liabilities Net Minority Interest"),),
    "retained_earnings": (("bal", "Retained Earnings"),),
    "equity": (("bal", "Stockholders Equity"), ("bal", "Common Stock Equity")),
    "invested_capital": (("bal", "Invested Capital"),),
    "shares": (("bal", "Ordinary Shares Number"), ("bal", "Share Issued")),
    "operating_cash_flow": (("cf", "Operating Cash Flow"), ("cf", "Cash Flow From Continuing Operating Activities")),
    "free_cash_flow": (("cf", "Free Cash Flow"),),
    "dividends_paid": (("cf", "Cash Dividends Paid"), ("cf", "Common Stock Dividend Paid")),
    "depreciation": (("cf", "Depreciation"),),
    "depreciation_amortization": (("cf", "Depreciation And Amortization"), ("inc", "Reconciled Depreciation")),
    "stock_issuance": (("cf", "Common Stock Issuance"), ("cf", "Issuance Of Capital Stock")),
}


def _value(frames: dict[str, pd.DataFrame], pairs: tuple[tuple[str, str], ...], date: pd.Timestamp) -> float | None:
    for statement, row in pairs:
        df = frames[statement]
        if row in df.index and date in df.columns:
            value = df.at[row, date]
            if pd.notna(value):
                return float(value)
    return None


def statements(ticker: str) -> tuple[list[Year], str, str]:
    """Annual statements newest first, their currency and the latest fiscal year end.

    The three statements can list different numbers of years, so lines are matched by fiscal
    year-end date, with the income statement's years as the reference.
    """
    t = yf.Ticker(ticker)
    frames = {"inc": t.income_stmt, "bal": t.balance_sheet, "cf": t.cashflow}
    if frames["inc"].empty or frames["bal"].empty:
        raise DataError(f"no annual statements for {ticker!r}")
    years = [
        Year(**{name: _value(frames, pairs, date) for name, pairs in ROWS.items()})
        for date in frames["inc"].columns
    ]
    profile = t.info or {}
    currency = profile.get("financialCurrency") or profile.get("currency") or "?"
    return years, currency, str(frames["inc"].columns[0].date())
