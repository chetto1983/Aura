"""Fundamental ratios from annual statements. Pure functions, no I/O.

Definitions follow the USMx AF6010 "Accounting and Financial Ratios and Formulas" sheet (edX),
with averages of opening and closing balances where it uses them. Inputs are plain numbers in the
statements' currency; the caller converts market values first. A ratio whose inputs are missing,
or whose denominator is not positive, is None rather than a number nobody can interpret.
"""

from dataclasses import dataclass

DAYS = 365
# Interest over average debt above this is not a cost of debt. Measured 2026-10-22 on year-end debt: ten
# issuers read 1.4-7.1% (XOM to BP), ENI.MI read 23.9% because Yahoo's interest line held its gross
# finance costs, exchange and derivative losses included.
MAX_PLAUSIBLE_INTEREST_RATE = 0.15


@dataclass(frozen=True)
class Statements:
    revenue: list[float | None]  # newest first, one value per fiscal year
    gross_profit: float | None
    ebitda: float | None
    ebit: float | None
    net_income: float | None
    interest_expense: float | None
    tax_rate: float | None
    equity: float | None
    equity_prior: float | None
    total_debt: float | None
    cash: float | None
    invested_capital: float | None
    free_cash_flow: float | None
    dividends_paid: float | None
    diluted_eps: float | None
    total_assets: float | None = None
    total_assets_prior: float | None = None
    current_assets: float | None = None
    current_liabilities: float | None = None
    current_liabilities_prior: float | None = None
    receivables: float | None = None
    receivables_prior: float | None = None
    inventory: float | None = None
    inventory_prior: float | None = None
    payables: float | None = None
    payables_prior: float | None = None
    total_liabilities: float | None = None
    long_term_debt: float | None = None
    total_debt_prior: float | None = None
    short_term_investments: float | None = None
    depreciation: float | None = None
    cost_of_revenue: float | None = None
    pretax_income: float | None = None
    operating_cash_flow: float | None = None


@dataclass(frozen=True)
class CapitalInputs:
    """What the analyst supplies for the cost of capital: nothing is assumed."""

    risk_free: float
    equity_risk_premium: float
    beta: float


def _div(num: float | None, den: float | None) -> float | None:
    if num is None or den is None or den <= 0:
        return None
    return num / den


def _avg(now: float | None, prior: float | None) -> float | None:
    if now is None:
        return None
    return now if prior is None else (now + prior) / 2


def _sub(a: float | None, b: float | None) -> float | None:
    return None if a is None or b is None else a - b


def _add(*values: float | None) -> float | None:
    return None if any(v is None for v in values) else sum(values)


def revenue_cagr(revenue: list[float | None]) -> float | None:
    """CAGR between the newest and oldest reported years; a missing year in between still counts."""
    years = [i for i, v in enumerate(revenue) if v is not None]
    if len(years) < 2:
        return None
    newest, oldest = revenue[years[0]], revenue[years[-1]]
    if newest <= 0 or oldest <= 0:
        return None
    return (newest / oldest) ** (1 / (years[-1] - years[0])) - 1


def interest_rate(s: Statements) -> float | None:
    """Interest expense over average debt: the pre-tax cost of debt."""
    return _div(s.interest_expense, _avg(s.total_debt, s.total_debt_prior))


def interest_warning(s: Statements) -> str | None:
    rate = interest_rate(s)
    if rate is None or rate <= MAX_PLAUSIBLE_INTEREST_RATE:
        return None
    return (
        f"interest expense is {rate:.1%} of average debt, implausible as a cost of debt: the statement line "
        "likely holds gross finance costs (exchange and derivative losses), so interest_coverage, "
        "cash_coverage, cost_of_debt_pretax, wacc and eva are unreliable"
    )


def _days(turnover: float | None) -> float | None:
    return _div(DAYS, turnover)


def ratios(s: Statements, price: float, market_cap: float) -> dict[str, float | None]:
    net_debt = _sub(s.total_debt, s.cash)
    ev = None if net_debt is None else market_cap + net_debt
    avg_equity = _avg(s.equity, s.equity_prior)
    avg_assets = _avg(s.total_assets, s.total_assets_prior)
    avg_cl = _avg(s.current_liabilities, s.current_liabilities_prior)
    nopat = None if s.ebit is None or s.tax_rate is None else s.ebit * (1 - s.tax_rate)
    revenue = s.revenue[0] if s.revenue else None

    receivables_turnover = _div(revenue, _avg(s.receivables, s.receivables_prior))
    inventory_turnover = _div(s.cost_of_revenue, _avg(s.inventory, s.inventory_prior))
    dpo = _div(_avg(s.payables, s.payables_prior), _div(s.cost_of_revenue, DAYS))
    dso, dio = _days(receivables_turnover), _days(inventory_turnover)
    quick_assets = _add(s.cash, s.short_term_investments or 0.0, s.receivables)

    return {
        # market
        "pe": _div(price, s.diluted_eps),
        "ev_ebitda": _div(ev, s.ebitda),
        "ev_ebit": _div(ev, s.ebit),
        "pb": _div(market_cap, s.equity),
        "ps": _div(market_cap, revenue),
        "fcf_yield": _div(s.free_cash_flow, market_cap),
        "enterprise_value": ev,
        # profitability and DuPont: roe = net_margin x asset_turnover x equity_multiplier
        "roe": _div(s.net_income, avg_equity),
        "roa": _div(s.net_income, avg_assets),
        "ebit_roa": _div(s.ebit, avg_assets),
        "roic": _div(nopat, s.invested_capital),
        "gross_margin": _div(s.gross_profit, revenue),
        "ebitda_margin": _div(s.ebitda, revenue),
        "operating_margin": _div(s.ebit, revenue),
        "pretax_margin": _div(s.pretax_income, revenue),
        "net_margin": _div(s.net_income, revenue),
        "asset_turnover": _div(revenue, avg_assets),
        "equity_multiplier": _div(avg_assets, avg_equity),
        "revenue_cagr": revenue_cagr(s.revenue),
        # turnover and working capital
        "receivables_turnover": receivables_turnover,
        "days_sales_outstanding": dso,
        "inventory_turnover": inventory_turnover,
        "days_inventory": dio,
        "days_payables": dpo,
        "cash_conversion_cycle": None if None in (dso, dio, dpo) else dso + dio - dpo,
        "working_capital": _sub(s.current_assets, s.current_liabilities),
        # liquidity
        "current_ratio": _div(s.current_assets, s.current_liabilities),
        "quick_ratio": _div(quick_assets, s.current_liabilities),
        "cash_ratio": _div(s.cash, avg_cl),
        "operating_cash_flow_to_current_liabilities": _div(s.operating_cash_flow, avg_cl),
        # leverage and coverage
        "net_debt": net_debt,
        "net_debt_ebitda": None if net_debt is None else _div(net_debt, s.ebitda),
        "debt_to_equity": _div(s.total_debt, s.equity),
        "long_term_debt_to_equity": _div(s.long_term_debt, s.equity),
        "liabilities_to_assets": _div(s.total_liabilities, s.total_assets),
        "liabilities_to_equity": _div(s.total_liabilities, s.equity),
        "equity_to_assets": _div(s.equity, s.total_assets),
        "interest_coverage": _div(s.ebit, s.interest_expense),
        "cash_coverage": _div(_add(s.ebit, s.depreciation), s.interest_expense),
        "payout": None if s.dividends_paid is None else _div(abs(s.dividends_paid), s.net_income),
    }


def cost_of_capital(s: Statements, market_cap: float, inputs: CapitalInputs) -> dict[str, float | None]:
    """CAPM cost of equity, WACC with market-value equity and book debt, EVA and Hamada beta.

    ke = rf + beta * ERP; kd = interest expense / average debt; WACC = E/V ke + D/V kd (1 - t);
    EVA = NOPAT - WACC * invested capital; unlevered beta = beta / (1 + (1 - t) D/E).
    """
    ke = inputs.risk_free + inputs.beta * inputs.equity_risk_premium
    debt = s.total_debt or 0.0
    kd = interest_rate(s)
    t = s.tax_rate if s.tax_rate is not None else None
    value = market_cap + debt
    if kd is None or t is None or value <= 0:
        wacc = None if debt > 0 else ke
    else:
        wacc = market_cap / value * ke + debt / value * kd * (1 - t)
    nopat = None if s.ebit is None or t is None else s.ebit * (1 - t)
    eva = None if nopat is None or wacc is None or s.invested_capital is None else nopat - wacc * s.invested_capital
    unlevered = None if t is None or market_cap <= 0 else inputs.beta / (1 + (1 - t) * debt / market_cap)
    return {
        "cost_of_equity": ke,
        "cost_of_debt_pretax": kd,
        "wacc": wacc,
        "eva": eva,
        "unlevered_beta": unlevered,
    }
