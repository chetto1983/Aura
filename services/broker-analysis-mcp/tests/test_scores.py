"""Piotroski, Altman and Beneish scores against hand computations and the papers' own numbers."""

from dataclasses import replace

import pytest

from broker_analysis_mcp import scores
from broker_analysis_mcp.fundamentals import Year

OLDEST = Year(total_assets=900.0, revenue=850.0)
PRIOR = Year(
    total_assets=1000.0, net_income_continuing=50.0, operating_cash_flow=80.0, revenue=950.0, gross_profit=380.0,
    current_assets=400.0, current_liabilities=250.0, long_term_debt=300.0, current_debt=50.0, shares=100.0,
)
LATEST = Year(
    total_assets=1100.0, net_income_continuing=70.0, operating_cash_flow=110.0, revenue=1100.0, gross_profit=451.0,
    current_assets=450.0, current_liabilities=250.0, long_term_debt=280.0, current_debt=40.0, shares=100.0,
)


def test_piotroski_hand_computed_all_signals() -> None:
    # ROA 70/1000 = 7% > 5.6% = 50/900; CFO 11% > ROA; leverage 320/1050 < 350/950; current ratio
    # 1.8 > 1.6; gross margin 41% > 40%; turnover 1100/1000 > 950/900; no new shares.
    f = scores.piotroski([LATEST, PRIOR, OLDEST])
    assert f["signals"] == dict.fromkeys(scores.PIOTROSKI_SIGNALS, 1)
    assert f["score"] == 9 and f["missing"] == []
    assert f["equity_issuance_basis"].startswith("change in shares outstanding")


def test_piotroski_losses_and_issuance_lose_points() -> None:
    latest = replace(LATEST, net_income_continuing=-10.0, stock_issuance=5.0)
    f = scores.piotroski([latest, PRIOR, OLDEST])
    assert f["signals"]["roa_positive"] == 0 and f["signals"]["roa_improved"] == 0
    assert f["signals"]["cfo_exceeds_roa"] == 1
    assert f["signals"]["no_equity_issued"] == 0
    assert f["equity_issuance_basis"].startswith("common stock issuance")
    assert f["score"] == 6


def test_piotroski_missing_input_gives_no_score_but_a_partial_one() -> None:
    f = scores.piotroski([replace(LATEST, gross_profit=None), PRIOR, OLDEST])
    assert f["score"] is None
    assert f["partial_score"] == 8 and f["signals_available"] == 8
    assert f["missing"] == ["gross_margin_rose"]
    assert scores.piotroski([LATEST])["missing"]  # one year cannot show any change


def test_altman_hand_computed() -> None:
    y = Year(
        current_assets=900.0, current_liabilities=600.0, total_assets=2400.0, retained_earnings=500.0, ebit=200.0,
        total_liabilities=1500.0, equity=900.0, revenue=1200.0,
    )
    a = scores.altman([y], market_cap=1800.0)
    x1, x2, x3, x4m, x4b, x5 = 300 / 2400, 500 / 2400, 200 / 2400, 1800 / 1500, 900 / 1500, 1200 / 2400
    assert a["z"] == pytest.approx(1.2 * x1 + 1.4 * x2 + 3.3 * x3 + 0.6 * x4m + 1.0 * x5)
    assert a["z"] == pytest.approx(1.936667, abs=1e-6)
    assert a["z_zone"] == "grey"
    assert a["z_double_prime"] == pytest.approx(6.56 * x1 + 3.26 * x2 + 6.72 * x3 + 1.05 * x4b)
    assert a["components"]["x4_book_equity_to_liabilities"] == pytest.approx(0.6)
    missing = scores.altman([replace(y, retained_earnings=None)], market_cap=1800.0)
    assert missing["z"] is None and missing["z_double_prime"] is None
    assert missing["missing"] == ["x2_retained_earnings_to_assets"]


@pytest.mark.parametrize(("z", "zone"), [(1.80, "distress"), (1.81, "grey"), (2.99, "grey"), (3.0, "safe")])
def test_altman_zone_boundaries(z: float, zone: str) -> None:
    assert scores._altman_zone(z) == zone


def test_beneish_weights_separate_the_papers_mean_profiles() -> None:
    # Table 2 of Beneish (1999): means of the 50 manipulators and of their 1,708 controls.
    manipulators = dict(dsri=1.465, gmi=1.193, aqi=1.254, sgi=1.607, depi=1.077, sgai=1.041, lvgi=1.111, tata=0.031)
    controls = dict(dsri=1.031, gmi=1.014, aqi=1.039, sgi=1.134, depi=1.001, sgai=1.054, lvgi=1.037, tata=0.018)
    assert scores.m_score(manipulators) == pytest.approx(-1.195681, abs=1e-6)
    assert scores.m_score(controls) == pytest.approx(-2.245854, abs=1e-6)
    assert scores.m_score(manipulators) > scores.BENEISH_CUTOFF > scores.m_score(controls)


BENEISH_PRIOR = Year(
    revenue=1000.0, cost_of_revenue=600.0, receivables=100.0, current_assets=400.0, net_ppe=300.0,
    total_assets=1000.0, depreciation=50.0, sga=100.0, long_term_debt=200.0, current_liabilities=150.0,
    cash=50.0, current_debt=20.0, tax_payable=10.0,
)
BENEISH_LATEST = Year(
    revenue=1100.0, cost_of_revenue=693.0, receivables=121.0, current_assets=440.0, net_ppe=320.0,
    total_assets=1100.0, depreciation=50.0, sga=115.5, long_term_debt=230.0, current_liabilities=170.0,
    cash=60.0, current_debt=25.0, tax_payable=12.0, depreciation_amortization=70.0,
)


def test_beneish_variables_hand_computed() -> None:
    b = scores.beneish([BENEISH_LATEST, BENEISH_PRIOR])
    expected = {
        "dsri": (121 / 1100) / (100 / 1000),
        "gmi": (400 / 1000) / (407 / 1100),
        "aqi": (1 - 760 / 1100) / (1 - 700 / 1000),
        "sgi": 1.1,
        "depi": (50 / 350) / (50 / 370),
        "sgai": (115.5 / 1100) / (100 / 1000),
        "lvgi": (400 / 1100) / (350 / 1000),
        # ((dCA 40 - dCash 10) - (dCL 20 - dCurrent debt 5 - dTax payable 2) - D&A 70) / TA 1100
        "tata": -53 / 1100,
    }
    assert b["variables"] == pytest.approx(expected)
    assert b["m"] == pytest.approx(scores.m_score(expected))
    assert b["above_cutoff"] is (b["m"] > -1.78)
    assert b["neutralised"] == [] and b["missing"] == []


def test_beneish_sets_the_papers_neutral_values() -> None:
    latest = replace(BENEISH_LATEST, sga=None, depreciation=None)
    b = scores.beneish([latest, BENEISH_PRIOR])
    assert b["variables"]["sgai"] == 1.0 and b["variables"]["depi"] == 1.0
    assert b["neutralised"] == ["depi", "sgai"]
    hard_assets_only = replace(BENEISH_PRIOR, current_assets=700.0)  # asset quality 0 in year t-1
    assert scores.beneish([BENEISH_LATEST, hard_assets_only])["variables"]["aqi"] == 1.0


def test_beneish_without_receivables_has_no_score() -> None:
    b = scores.beneish([replace(BENEISH_LATEST, receivables=None), BENEISH_PRIOR])
    assert b["m"] is None and b["above_cutoff"] is None
    assert b["missing"] == ["dsri"]
