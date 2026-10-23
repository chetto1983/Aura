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
