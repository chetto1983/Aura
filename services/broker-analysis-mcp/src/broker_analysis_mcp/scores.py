"""Accounting scores of financial strength, distress and earnings manipulation. Pure functions, no I/O.

Each follows its paper as closely as the statement lines allow. A score with a missing input is
None and names what it lacked; it is never filled with a guess. None of them is meant for banks
or insurers.
"""

import math

from .fundamentals import Year, add, div, sub, year

PIOTROSKI_SIGNALS = (
    "roa_positive", "cfo_positive", "roa_improved", "cfo_exceeds_roa", "leverage_fell",
    "current_ratio_rose", "no_equity_issued", "gross_margin_rose", "asset_turnover_rose",
)

ALTMAN_DISTRESS_BELOW = 1.81
ALTMAN_SAFE_ABOVE = 2.99

BENEISH_CONSTANT = -4.840
BENEISH_WEIGHTS = {
    "dsri": 0.920, "gmi": 0.528, "aqi": 0.404, "sgi": 0.892,
    "depi": 0.115, "sgai": -0.172, "tata": 4.679, "lvgi": -0.327,
}
# Score above which the unweighted probit flags a manipulator at relative error costs of 20:1 or 30:1.
BENEISH_CUTOFF = -1.78


def _mean(a: float | None, b: float | None) -> float | None:
    return None if a is None or b is None else (a + b) / 2


def _positive(x: float | None) -> int | None:
    return None if x is None else int(x > 0)


def _no_equity_issued(c: Year, p: Year) -> tuple[int | None, str | None]:
    if c.stock_issuance is not None:
        return int(c.stock_issuance <= 0), "common stock issuance in the cash flow statement"
    if c.shares is not None and p.shares is not None:
        return int(c.shares <= p.shares), "change in shares outstanding (no issuance line reported)"
    return None, None


def piotroski(years: list[Year]) -> dict:
    """Piotroski (2000) F-score: nine binary signals comparing fiscal years t and t-1.

    ROA and CFO are scaled by total assets at the start of the year; leverage is long-term debt
    (with its current part) over average total assets; turnover is sales over total assets at the
    start of the year, as in the paper's section 2.3.3.
    """
    c, p, pp = year(years, 0), year(years, 1), year(years, 2)
    roa, roa_prior = div(c.net_income_continuing, p.total_assets), div(p.net_income_continuing, pp.total_assets)
    cfo = div(c.operating_cash_flow, p.total_assets)
    lever = div(add(c.long_term_debt, c.current_debt), _mean(c.total_assets, p.total_assets))
    lever_prior = div(add(p.long_term_debt, p.current_debt), _mean(p.total_assets, pp.total_assets))
    no_issue, issue_basis = _no_equity_issued(c, p)
    signals = {
        "roa_positive": _positive(roa),
        "cfo_positive": _positive(cfo),
        "roa_improved": _positive(sub(roa, roa_prior)),
        "cfo_exceeds_roa": _positive(sub(cfo, roa)),
        "leverage_fell": _positive(sub(lever_prior, lever)),
        "current_ratio_rose": _positive(
            sub(div(c.current_assets, c.current_liabilities), div(p.current_assets, p.current_liabilities))
        ),
        "no_equity_issued": no_issue,
        "gross_margin_rose": _positive(sub(div(c.gross_profit, c.revenue), div(p.gross_profit, p.revenue))),
        "asset_turnover_rose": _positive(sub(div(c.revenue, p.total_assets), div(p.revenue, pp.total_assets))),
    }
    missing = [k for k in PIOTROSKI_SIGNALS if signals[k] is None]
    partial = sum(v for v in signals.values() if v is not None)
    return {
        "score": None if missing else partial,
        "partial_score": partial,
        "signals_available": len(PIOTROSKI_SIGNALS) - len(missing),
        "signals": signals,
        "missing": missing,
        "equity_issuance_basis": issue_basis,
    }


def _altman_zone(z: float | None) -> str | None:
    if z is None:
        return None
    if z < ALTMAN_DISTRESS_BELOW:
        return "distress"
    return "safe" if z > ALTMAN_SAFE_ABOVE else "grey"


def altman(years: list[Year], market_cap: float) -> dict:
    """Altman Z (1968, public manufacturers) and Z'' (non-manufacturers, book equity), latest year."""
    c = year(years, 0)
    x1 = div(sub(c.current_assets, c.current_liabilities), c.total_assets)
    x2 = div(c.retained_earnings, c.total_assets)
    x3 = div(c.ebit, c.total_assets)
    x4m, x4b = div(market_cap, c.total_liabilities), div(c.equity, c.total_liabilities)
    x5 = div(c.revenue, c.total_assets)
    x = {
        "x1_working_capital_to_assets": x1,
        "x2_retained_earnings_to_assets": x2,
        "x3_ebit_to_assets": x3,
        "x4_market_equity_to_liabilities": x4m,
        "x4_book_equity_to_liabilities": x4b,
        "x5_sales_to_assets": x5,
    }
    z = None if None in (x1, x2, x3, x4m, x5) else 1.2 * x1 + 1.4 * x2 + 3.3 * x3 + 0.6 * x4m + 1.0 * x5
    z2 = None if None in (x1, x2, x3, x4b) else 6.56 * x1 + 3.26 * x2 + 6.72 * x3 + 1.05 * x4b
    return {
        "z": z,
        "z_zone": _altman_zone(z),
        "z_double_prime": z2,
        "components": x,
        "missing": [k for k, v in x.items() if v is None],
    }


def _asset_quality(y: Year) -> float | None:
    """Share of assets other than current assets and PP&E, whose future benefit is less certain."""
    hard = add(y.current_assets, y.net_ppe)
    ratio = div(hard, y.total_assets)
    return None if ratio is None else 1 - ratio


def _depreciation_rate(y: Year) -> float | None:
    return div(y.depreciation, add(y.depreciation, y.net_ppe))


def _total_accruals(c: Year, p: Year) -> float | None:
    """(dCA - dCash) - (dCL - dCurrent debt - dTax payable) - D&A, as in Beneish's Table 2."""
    working = sub(sub(c.current_assets, p.current_assets), sub(c.cash, p.cash))
    liabilities = sub(
        sub(c.current_liabilities, p.current_liabilities),
        add(sub(c.current_debt, p.current_debt), sub(c.tax_payable, p.tax_payable)),
    )
    accruals = sub(sub(working, liabilities), c.depreciation_amortization)
    return div(accruals, c.total_assets)


def m_score(variables: dict[str, float]) -> float:
    return BENEISH_CONSTANT + math.fsum(BENEISH_WEIGHTS[k] * variables[k] for k in BENEISH_WEIGHTS)


def beneish(years: list[Year]) -> dict:
    """Beneish (1999) eight-variable M-score on fiscal years t and t-1, unweighted probit weights.

    As in the paper, AQI is 1 when the prior year's asset quality is zero, and DEPI and SGAI are 1
    when depreciation (without amortisation) or SG&A are not reported.
    """
    c, p = year(years, 0), year(years, 1)
    gm_c, gm_p = div(sub(c.revenue, c.cost_of_revenue), c.revenue), div(sub(p.revenue, p.cost_of_revenue), p.revenue)
    aq_c, aq_p = _asset_quality(c), _asset_quality(p)
    neutral = []
    if aq_c is not None and aq_p == 0:
        aqi = 1.0
        neutral.append("aqi")
    else:
        aqi = None if aq_c is None or aq_p is None else aq_c / aq_p
    if c.depreciation is None or p.depreciation is None:
        depi = 1.0
        neutral.append("depi")
    else:
        depi = div(_depreciation_rate(p), _depreciation_rate(c))
    if c.sga is None or p.sga is None:
        sgai = 1.0
        neutral.append("sgai")
    else:
        sgai = div(div(c.sga, c.revenue), div(p.sga, p.revenue))
    variables = {
        "dsri": div(div(c.receivables, c.revenue), div(p.receivables, p.revenue)),
        "gmi": div(gm_p, gm_c),
        "aqi": aqi,
        "sgi": div(c.revenue, p.revenue),
        "depi": depi,
        "sgai": sgai,
        "tata": _total_accruals(c, p),
        "lvgi": div(
            div(add(c.long_term_debt, c.current_liabilities), c.total_assets),
            div(add(p.long_term_debt, p.current_liabilities), p.total_assets),
        ),
    }
    missing = [k for k, v in variables.items() if v is None]
    m = None if missing else m_score(variables)
    return {
        "m": m,
        "above_cutoff": None if m is None else m > BENEISH_CUTOFF,
        "variables": variables,
        "neutralised": neutral,
        "missing": missing,
    }
