"""Quote normalisation measured on live Yahoo data (SHEL.L, 2026-10-22): price 3888.5 GBp,
market cap 221,211,492,352 GBP = price in pounds x 5,688,864,370 shares."""

import pytest

from broker_analysis_mcp.analysis import normalise_quote


def test_london_price_in_pence_market_cap_in_pounds() -> None:
    price, cap, ccy = normalise_quote(3888.5, 221_211_492_352, "GBp")
    assert ccy == "GBP"
    assert price == pytest.approx(38.885)
    assert cap == 221_211_492_352
    assert cap / price == pytest.approx(5_688_864_370, rel=1e-6)


def test_other_currencies_untouched() -> None:
    assert normalise_quote(26.5, 7.5e10, "EUR") == (26.5, 7.5e10, "EUR")


def test_same_market_hours_despite_different_zone_names() -> None:
    from broker_analysis_mcp.analysis import utc_offset

    assert utc_offset("Europe/Rome") == utc_offset("Europe/Berlin")
    assert utc_offset("Europe/Rome") != utc_offset("America/New_York")
    assert utc_offset("?") is None
