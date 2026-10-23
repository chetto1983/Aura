"""The agent E2E scorer matches quoted numbers to tool numbers with their sign."""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "e2e"))

import agent_e2e


def test_a_sign_flip_is_accepted_only_for_losses() -> None:
    results = [{"metrics": {"max_drawdown": {"value": 53.06}, "beta": {"value": 1.2}, "jensen_alpha": {"value": 19.05}}}]
    pool = agent_e2e._tool_numbers(results)
    assert agent_e2e._matches(-53.06, pool) and agent_e2e._matches(53.06, pool)
    assert agent_e2e._matches(1.2, pool) and not agent_e2e._matches(-1.2, pool)
    assert not agent_e2e._matches(-19.05, pool)


def test_a_minus_in_tool_prose_keeps_its_sign() -> None:
    pool = agent_e2e._tool_numbers([{"references": {"beneish": "Above -1.78 the model flags a manipulator."}}])
    assert agent_e2e._matches(-1.78, pool)


def _readings(text: str) -> list[list[tuple[float, int]]]:
    return agent_e2e._answer_decimals(text)


def test_a_dash_before_a_number_is_a_minus_but_not_inside_a_range() -> None:
    assert _readings("down capture \u2013132,7%") == [[(-132.7, 1)]]
    assert _readings("drawdown \u201153,1%") == [[(-53.1, 1)]]  # non-breaking hyphen
    assert [r[0] for r in _readings("zona grigia (1,81\u20132,99)")] == [(1.81, 2), (2.99, 2)]


def test_indicator_parameters_are_not_decimals() -> None:
    assert [r[0] for r in _readings("MACD (12,26,9) a 4,20 USD")] == [(4.2, 2)]


def test_italian_thousands_and_scale_words_add_readings() -> None:
    assert _readings("SMA50 a 81.792 USD")[0][:2] == [(81.792, 3), (81792.0, 0)]
    (readings,) = _readings("debito netto di 15,8 miliardi")
    billions, digits = readings[1]
    assert readings[0] == (15.8, 1) and digits == -8  # shown to the nearest 100 million
    assert agent_e2e._matches(billions, agent_e2e._tool_numbers([{"net_debt": 15_801_000_000}]), digits)
    assert not agent_e2e._matches(billions, [15_900_000_000.0], digits)


def test_an_unsigned_quote_may_carry_its_sign_in_words_but_a_written_sign_counts() -> None:
    def grounded(text: str) -> bool:
        return all(any(agent_e2e._matches(v, [-8.2, 1.2], d) for v, d in r) for r in _readings(text))

    assert grounded("8,2 punti in meno") and grounded("\u22128,2 punti")
    assert grounded("beta 1,2") and not grounded("beta \u22121,2") and not grounded("+8,2 punti")


def test_a_decimal_point_is_read_too() -> None:
    assert _readings("beta 0.28")[0][0] == (0.28, 2)
