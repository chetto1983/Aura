"""Fundamental ratios from annual statements. Pure functions, no I/O.

Definitions follow the USMx AF6010 "Accounting and Financial Ratios and Formulas" sheet (edX),
with averages of opening and closing balances where it uses them. Statements arrive as one Year
per fiscal year, newest first, in the statements' currency; the caller converts market values
first. A ratio whose inputs are missing, or whose denominator is not positive, is None rather than
a number nobody can interpret.
"""

from dataclasses import dataclass

DAYS = 365
# Interest over average debt above this is not a cost of debt. Measured 2026-10-22 on year-end debt: ten
# issuers read 1.4-7.1% (XOM to BP), ENI.MI read 23.9% because Yahoo's interest line held its gross
# finance costs, exchange and derivative losses included.
MAX_PLAUSIBLE_INTEREST_RATE = 0.15


@dataclass(frozen=True)
class Year:
    """One fiscal year of statement lines; None where the source has no value."""

    revenue: float | None = None
    cost_of_revenue: float | None = None
    gross_profit: float | None = None
    sga: float | None = None
    ebitda: float | None = None
    ebit: float | None = None
    pretax_income: float | None = None
    net_income: float | None = None
    net_income_continuing: float | None = None
    interest_expense: float | None = None
    tax_rate: float | None = None
    diluted_eps: float | None = None
    total_assets: float | None = None
    current_assets: float | None = None
    current_liabilities: float | None = None
    cash: float | None = None
    short_term_investments: float | None = None
    receivables: float | None = None
    inventory: float | None = None
    payables: float | None = None
    net_ppe: float | None = None
    total_debt: float | None = None
    long_term_debt: float | None = None
    current_debt: float | None = None
    tax_payable: float | None = None
    total_liabilities: float | None = None
    retained_earnings: float | None = None
    equity: float | None = None
    invested_capital: float | None = None
    shares: float | None = None
    operating_cash_flow: float | None = None
    free_cash_flow: float | None = None
    dividends_paid: float | None = None
    depreciation: float | None = None  # excluding amortisation
    depreciation_amortization: float | None = None
    stock_issuance: float | None = None


@dataclass(frozen=True)
class CapitalInputs:
    """What the analyst supplies for the cost of capital: nothing is assumed."""

    risk_free: float
    equity_risk_premium: float
    beta: float


def div(num: float | None, den: float | None) -> float | None:
    if num is None or den is None or den <= 0:
        return None
    return num / den


def avg(now: float | None, prior: float | None) -> float | None:
    if now is None:
        return None
    return now if prior is None else (now + prior) / 2


def sub(a: float | None, b: float | None) -> float | None:
    return None if a is None or b is None else a - b


def add(*values: float | None) -> float | None:
    return None if any(v is None for v in values) else sum(values)


def year(years: list[Year], index: int) -> Year:
    """The fiscal year `index` steps back from the latest, or an empty one when not reported."""
    return years[index] if index < len(years) else Year()


def revenue_cagr(revenue: list[float | None]) -> float | None:
    """CAGR between the newest and oldest reported years; a missing year in between still counts."""
    years = [i for i, v in enumerate(revenue) if v is not None]
    if len(years) < 2:
        return None
    newest, oldest = revenue[years[0]], revenue[years[-1]]
    if newest <= 0 or oldest <= 0:
        return None
    return (newest / oldest) ** (1 / (years[-1] - years[0])) - 1


def interest_rate(years: list[Year]) -> float | None:
    """Interest expense over average debt: the pre-tax cost of debt."""
    c, p = year(years, 0), year(years, 1)
    return div(c.interest_expense, avg(c.total_debt, p.total_debt))


def interest_warning(years: list[Year]) -> str | None:
    rate = interest_rate(years)
    if rate is None or rate <= MAX_PLAUSIBLE_INTEREST_RATE:
        return None
    return (
        f"interest expense is {rate:.1%} of average debt, implausible as a cost of debt: the statement line "
        "likely holds gross finance costs (exchange and derivative losses), so interest_coverage, "
        "cash_coverage, cost_of_debt_pretax, wacc and eva are unreliable"
    )


def _days(turnover: float | None) -> float | None:
    return div(DAYS, turnover)


def ratios(years: list[Year], price: float, market_cap: float) -> dict[str, float | None]:
    c, p = year(years, 0), year(years, 1)
    net_debt = sub(c.total_debt, c.cash)
    ev = None if net_debt is None else market_cap + net_debt
    avg_equity = avg(c.equity, p.equity)
    avg_assets = avg(c.total_assets, p.total_assets)
    avg_cl = avg(c.current_liabilities, p.current_liabilities)
    nopat = None if c.ebit is None or c.tax_rate is None else c.ebit * (1 - c.tax_rate)
    revenue = c.revenue

    receivables_turnover = div(revenue, avg(c.receivables, p.receivables))
    inventory_turnover = div(c.cost_of_revenue, avg(c.inventory, p.inventory))
    dpo = div(avg(c.payables, p.payables), div(c.cost_of_revenue, DAYS))
    dso, dio = _days(receivables_turnover), _days(inventory_turnover)
    quick_assets = add(c.cash, c.short_term_investments or 0.0, c.receivables)

    return {
        # market
        "pe": div(price, c.diluted_eps),
        "ev_ebitda": div(ev, c.ebitda),
        "ev_ebit": div(ev, c.ebit),
        "pb": div(market_cap, c.equity),
        "ps": div(market_cap, revenue),
        "fcf_yield": div(c.free_cash_flow, market_cap),
        "enterprise_value": ev,
        # profitability and DuPont: roe = net_margin x asset_turnover x equity_multiplier
        "roe": div(c.net_income, avg_equity),
        "roa": div(c.net_income, avg_assets),
        "ebit_roa": div(c.ebit, avg_assets),
        "roic": div(nopat, c.invested_capital),
        "gross_margin": div(c.gross_profit, revenue),
        "ebitda_margin": div(c.ebitda, revenue),
        "operating_margin": div(c.ebit, revenue),
        "pretax_margin": div(c.pretax_income, revenue),
        "net_margin": div(c.net_income, revenue),
        "asset_turnover": div(revenue, avg_assets),
        "equity_multiplier": div(avg_assets, avg_equity),
        "revenue_cagr": revenue_cagr([y.revenue for y in years]),
        # turnover and working capital
        "receivables_turnover": receivables_turnover,
        "days_sales_outstanding": dso,
        "inventory_turnover": inventory_turnover,
        "days_inventory": dio,
        "days_payables": dpo,
        "cash_conversion_cycle": None if None in (dso, dio, dpo) else dso + dio - dpo,
        "working_capital": sub(c.current_assets, c.current_liabilities),
        # liquidity
        "current_ratio": div(c.current_assets, c.current_liabilities),
        "quick_ratio": div(quick_assets, c.current_liabilities),
        "cash_ratio": div(c.cash, avg_cl),
        "operating_cash_flow_to_current_liabilities": div(c.operating_cash_flow, avg_cl),
        # leverage and coverage
        "net_debt": net_debt,
        "net_debt_ebitda": None if net_debt is None else div(net_debt, c.ebitda),
        "debt_to_equity": div(c.total_debt, c.equity),
        "long_term_debt_to_equity": div(c.long_term_debt, c.equity),
        "liabilities_to_assets": div(c.total_liabilities, c.total_assets),
        "liabilities_to_equity": div(c.total_liabilities, c.equity),
        "equity_to_assets": div(c.equity, c.total_assets),
        "interest_coverage": div(c.ebit, c.interest_expense),
        "cash_coverage": div(add(c.ebit, c.depreciation_amortization), c.interest_expense),
        "payout": None if c.dividends_paid is None else div(abs(c.dividends_paid), c.net_income),
    }


def cost_of_capital(years: list[Year], market_cap: float, inputs: CapitalInputs) -> dict[str, float | None]:
    """CAPM cost of equity, WACC with market-value equity and book debt, EVA and Hamada beta.

    ke = rf + beta * ERP; kd = interest expense / average debt; WACC = E/V ke + D/V kd (1 - t);
    EVA = NOPAT - WACC * invested capital; unlevered beta = beta / (1 + (1 - t) D/E).
    """
    c = year(years, 0)
    ke = inputs.risk_free + inputs.beta * inputs.equity_risk_premium
    debt = c.total_debt or 0.0
    kd = interest_rate(years)
    t = c.tax_rate
    value = market_cap + debt
    if kd is None or t is None or value <= 0:
        wacc = None if debt > 0 else ke
    else:
        wacc = market_cap / value * ke + debt / value * kd * (1 - t)
    nopat = None if c.ebit is None or t is None else c.ebit * (1 - t)
    eva = None if nopat is None or wacc is None or c.invested_capital is None else nopat - wacc * c.invested_capital
    unlevered = None if t is None or market_cap <= 0 else inputs.beta / (1 + (1 - t) * debt / market_cap)
    return {
        "cost_of_equity": ke,
        "cost_of_debt_pretax": kd,
        "wacc": wacc,
        "eva": eva,
        "unlevered_beta": unlevered,
    }
