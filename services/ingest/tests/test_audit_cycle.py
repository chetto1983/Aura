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
    monkeypatch.setattr(app, "_orphaned_previous_cycle", set())


@pytest.fixture(autouse=True)
def removed(monkeypatch):
    """The keys the sweep removed, in order; no test reaches a real ArcadeDB."""
    keys: list[str] = []
    monkeypatch.setattr(app.arcade, "remove_document",
                        lambda base_url, database, auth, key, timeout_s: keys.append(key))
    return keys


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
    # A row whose object is gone is an orphan for the sweep below, never a missing document.
    monkeypatch.setattr(app, "_bucket_versus_index", sets(set(), {"chat/deleted.pdf"}))

    app.audit_cycle()
    app.audit_cycle()

    assert "MISSING" not in capsys.readouterr().out


def test_an_orphan_seen_for_one_cycle_is_kept(monkeypatch, removed):
    # CocoIndex still tracks the row of an object deleted after its walker listed the
    # bucket, and removes it itself on the next cycle.
    monkeypatch.setattr(app, "_bucket_versus_index", sets(set(), {"chat/gone.md"}))

    app.audit_cycle()

    assert removed == []


def test_an_orphan_seen_for_two_cycles_is_removed(monkeypatch, capsys, removed):
    monkeypatch.setattr(app, "_bucket_versus_index", sets({"chat/kept.md"}, {"chat/kept.md", "chat/gone.md"}))

    app.audit_cycle()
    app.audit_cycle()

    assert removed == ["chat/gone.md"]
    assert "[audit] ORPHAN chat/gone.md removed" in capsys.readouterr().out


def test_a_key_back_in_the_bucket_between_the_cycles_is_kept(monkeypatch, removed):
    monkeypatch.setattr(app, "_bucket_versus_index", sets(set(), {"chat/back.md"}))
    app.audit_cycle()
    monkeypatch.setattr(app, "_bucket_versus_index", sets({"chat/back.md"}, {"chat/back.md"}))
    app.audit_cycle()
    monkeypatch.setattr(app, "_bucket_versus_index", sets(set(), {"chat/back.md"}))

    app.audit_cycle()

    assert removed == [], "the rule is two CONSECUTIVE cycles, not two cycles ever"


def test_a_key_in_the_bucket_and_the_index_is_never_removed(monkeypatch, removed):
    monkeypatch.setattr(app, "_bucket_versus_index", sets({"chat/a.md"}, {"chat/a.md"}))

    for _ in range(3):
        app.audit_cycle()

    assert removed == []


def test_a_failed_removal_is_printed_and_tried_again(monkeypatch, capsys):
    attempts: list[str] = []

    def refuse(base_url, database, auth, key, timeout_s):
        attempts.append(key)
        raise app.arcade.ArcadeSchemaError("POST /api/v1/command/mem_x: HTTP 503: busy")

    monkeypatch.setattr(app.arcade, "remove_document", refuse)
    monkeypatch.setattr(app, "_bucket_versus_index", sets(set(), {"chat/gone.md"}))

    app.audit_cycle()
    app.audit_cycle()  # must not raise: one stale row cannot stop the cycle
    app.audit_cycle()

    assert attempts == ["chat/gone.md", "chat/gone.md"]
    assert "[audit] ORPHAN chat/gone.md not removed: POST /api/v1/command/mem_x: HTTP 503: busy" in (
        capsys.readouterr().err)


def test_a_failed_lookup_is_printed_and_acts_on_nothing(monkeypatch, capsys, removed):
    """A lookup that fails (ArcadeDB busy, or an index past httpQueryMaxResultRows, which
    `limit: -1` turns into an HTTP 413) must not stop the live pipeline, and an orphan or a
    missing key must not be judged on the half of the comparison it could not read."""
    monkeypatch.setattr(app, "_bucket_versus_index", sets(set(), {"chat/gone.md"}))
    app.audit_cycle()

    def refuse():
        raise app.arcade.ArcadeSchemaError("POST /api/v1/query/mem_x: HTTP 413: too many rows")

    monkeypatch.setattr(app, "_bucket_versus_index", refuse)
    app.audit_cycle()

    assert removed == []
    assert app._orphaned_previous_cycle == {"chat/gone.md"}, "the last cycle that read is kept"
    assert "[audit] skipped: POST /api/v1/query/mem_x: HTTP 413: too many rows" in (
        capsys.readouterr().err)
