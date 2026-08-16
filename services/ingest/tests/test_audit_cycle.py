"""The live audit names a loss only once it has stopped being explainable as newness.

Live mode never returns, so the catch-up audit that turns a missing document into an exit
code never runs there. Without an in-cycle audit the only thing that can tell a lost
document from an empty one does not execute at all — which is how two documents went
missing on 2026-08-09 under a clean-looking log.
"""

import pytest

from ingest import app


@pytest.fixture(autouse=True)
def _fresh_cycle_state(monkeypatch):
    monkeypatch.setattr(app, "_missing_previous_cycle", set())


def sets(expected: set[str], indexed: set[str]):
    return lambda: (expected, indexed)


def test_a_key_missing_for_one_cycle_is_not_reported(monkeypatch, capsys):
    monkeypatch.setattr(app, "_bucket_versus_index", sets({"chat/new.pdf"}, set()))

    app.audit_cycle()

    assert "MISSING" not in capsys.readouterr().out, (
        "an object the walker has not offered yet is new, not lost"
    )


def test_a_key_missing_for_two_cycles_is_named(monkeypatch, capsys):
    monkeypatch.setattr(app, "_bucket_versus_index", sets({"chat/lost.pdf"}, set()))

    app.audit_cycle()
    capsys.readouterr()
    app.audit_cycle()

    assert "[audit] MISSING chat/lost.pdf" in capsys.readouterr().out


def test_a_key_that_arrives_on_the_second_cycle_is_never_named(monkeypatch, capsys):
    monkeypatch.setattr(app, "_bucket_versus_index", sets({"chat/slow.pdf"}, set()))
    app.audit_cycle()
    capsys.readouterr()

    monkeypatch.setattr(
        app, "_bucket_versus_index", sets({"chat/slow.pdf"}, {"chat/slow.pdf"}))
    app.audit_cycle()

    assert "MISSING" not in capsys.readouterr().out


def test_a_key_that_recovers_and_goes_missing_again_needs_two_cycles_afresh(
    monkeypatch, capsys
):
    # A document reindexed and then lost again must not inherit the earlier strike: the
    # rule is two CONSECUTIVE cycles, not two cycles ever.
    monkeypatch.setattr(app, "_bucket_versus_index", sets({"chat/a.pdf"}, set()))
    app.audit_cycle()
    monkeypatch.setattr(app, "_bucket_versus_index", sets({"chat/a.pdf"}, {"chat/a.pdf"}))
    app.audit_cycle()
    monkeypatch.setattr(app, "_bucket_versus_index", sets({"chat/a.pdf"}, set()))
    capsys.readouterr()

    app.audit_cycle()

    assert "MISSING" not in capsys.readouterr().out


def test_an_index_ahead_of_the_bucket_reports_nothing(monkeypatch, capsys):
    # Rows for objects that no longer exist are the delete path's business, not this one's.
    monkeypatch.setattr(app, "_bucket_versus_index", sets(set(), {"chat/deleted.pdf"}))

    app.audit_cycle()
    app.audit_cycle()

    assert "MISSING" not in capsys.readouterr().out
