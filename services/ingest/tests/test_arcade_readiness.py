"""ArcadeDB not being up yet is a wait, not a fatal error.

The defect these tests pin, measured on the live stack: `depends_on: condition:
service_healthy` is honoured only by `docker compose up`. When the Docker daemon itself
restarts -- host reboot, Docker Desktop restart -- it rebrings every `restart:
unless-stopped` container in arbitrary order and ignores both depends_on and the
healthchecks. aura-ingest therefore started 6 seconds after ArcadeDB on 2026-08-23,
`ensure_schema` issued `create database` into a port nothing was listening on yet, and
the URLError became a fatal ArcadeSchemaError that killed the process. 14 crashes in
five days, in bursts of 2-5, one burst per stack restart, ~1100 lines of traceback.

The classification is the whole fix, and ArcadeDB's own GetReadyHandler defines it:
GET /api/v1/ready needs no authentication, answers 204 once server status is ONLINE
(and, under HA, once the node has joined the Raft group and caught up), and answers
503 while it is not. So:

  - transport failure (connection refused, DNS, timeout) -> not up yet, wait
  - HTTP 503                                             -> up but not ONLINE, wait
  - HTTP 204                                             -> ready, proceed
  - anything else                                        -> a fault waiting cannot fix

That last line is what keeps the fix from becoming a different bug: a schema ArcadeDB
actively REJECTS must still fail on the first attempt, as loudly as it does today.
"""

from __future__ import annotations

import io
import urllib.error

import pytest

from ingest import arcade
from ingest.arcade import ArcadeSchemaError

BASE = "http://arcadedb:2480"


class _Response:
    """The subset of an HTTPResponse context manager that arcade.py touches."""

    def __init__(self, status: int) -> None:
        self.status = status

    def __enter__(self) -> "_Response":
        return self

    def __exit__(self, *_exc) -> bool:
        return False

    def read(self) -> bytes:
        return b""


def _refused() -> urllib.error.URLError:
    return urllib.error.URLError(ConnectionRefusedError(111, "Connection refused"))


def _http(code: int, body: bytes = b"") -> urllib.error.HTTPError:
    return urllib.error.HTTPError(
        url=BASE + "/api/v1/ready", code=code, msg="", hdrs=None, fp=io.BytesIO(body)
    )


@pytest.fixture
def probes(monkeypatch):
    """Script the readiness endpoint's answers and drive the clock from the sleeps.

    sleep advances a FAKE monotonic clock instead of the wall clock, so a test that
    exercises a 180-second budget still runs in microseconds AND the budget arithmetic
    stays exact rather than racing real time. What is under test is the number and the
    classification of the probes, never how long they actually took.
    """
    calls: list[str] = []
    slept: list[float] = []
    now = [0.0]

    def install(sequence):
        remaining = list(sequence)

        def fake_urlopen(request, timeout=None):  # noqa: ARG001
            calls.append(request.full_url)
            outcome = remaining.pop(0) if remaining else sequence[-1]
            if isinstance(outcome, BaseException):
                raise outcome
            return _Response(outcome)

        def fake_sleep(seconds: float) -> None:
            slept.append(seconds)
            now[0] += seconds

        monkeypatch.setattr("ingest.arcade.urllib.request.urlopen", fake_urlopen)
        monkeypatch.setattr(arcade.time, "sleep", fake_sleep)
        monkeypatch.setattr(arcade.time, "monotonic", lambda: now[0])
        return calls, slept

    return install


def test_a_ready_server_is_probed_once_and_never_slept_on(probes):
    calls, slept = probes([204])

    arcade.wait_until_ready(BASE)

    assert len(calls) == 1
    assert calls[0] == BASE + arcade.READY_PATH
    assert slept == [], "a server that is already up must cost no delay at all"


def test_the_readiness_probe_carries_no_credential(probes, monkeypatch):
    # GetReadyHandler.isRequireAuthentication() is false, and the compose healthcheck
    # relies on that to avoid baking the root password into the compose file. Sending one
    # here would put it on the wire for a liveness probe that does not need it.
    seen: list[dict] = []

    def fake_urlopen(request, timeout=None):  # noqa: ARG001
        seen.append(dict(request.headers))
        return _Response(204)

    monkeypatch.setattr("ingest.arcade.urllib.request.urlopen", fake_urlopen)

    arcade.wait_until_ready(BASE)

    assert seen == [{}]


def test_connection_refused_is_waited_out_not_raised(probes):
    # The exact 2026-08-23 boot: three refusals while the JVM comes up, then ready.
    calls, slept = probes([_refused(), _refused(), _refused(), 204])

    arcade.wait_until_ready(BASE, poll_interval_s=0.25)

    assert len(calls) == 4
    assert slept == [0.25, 0.25, 0.25]


def test_503_is_waited_out_because_the_port_binds_before_the_server_is_online(probes):
    # The case reading the docs caught and a "any HTTP answer means it is up" rule would
    # have got wrong: the socket answers, the server is not ONLINE, DDL would still fail.
    calls, _ = probes([_http(503), _http(503), 204])

    arcade.wait_until_ready(BASE, poll_interval_s=0.01)

    assert len(calls) == 3


def test_a_budget_that_runs_out_raises_naming_the_endpoint_and_the_last_reason(probes):
    probes([_refused()])

    with pytest.raises(ArcadeSchemaError) as caught:
        arcade.wait_until_ready(BASE, timeout_s=0.05, poll_interval_s=0.01)

    message = str(caught.value)
    assert arcade.READY_PATH in message
    assert "Connection refused" in message, "the reason that kept it waiting must survive"


def test_a_zero_budget_still_probes_once_rather_than_failing_blind(probes):
    calls, _ = probes([204])

    arcade.wait_until_ready(BASE, timeout_s=0.0)

    assert len(calls) == 1


def test_an_unexpected_status_fails_immediately_instead_of_burning_the_budget(probes):
    # 204 and 503 are the only statuses GetReadyHandler produces. Anything else is a
    # fault waiting cannot fix -- a proxy in the way, a wrong port, a broken build --
    # and spending three minutes on it would hide it rather than report it.
    calls, slept = probes([_http(500, b"boom")])

    with pytest.raises(ArcadeSchemaError) as caught:
        arcade.wait_until_ready(BASE, timeout_s=180.0, poll_interval_s=0.01)

    assert len(calls) == 1
    assert slept == []
    assert "500" in str(caught.value)


def test_a_200_is_not_ready_because_only_arcadedb_answers_204_there(probes):
    # urlopen raises for 4xx/5xx but hands a 2xx straight back, so a 200 arrives as a
    # RESPONSE, not an error -- and something answering 200 on this path is not ArcadeDB
    # (a proxy in front, the wrong port, a different service on it). Accepting it would
    # send the whole DDL somewhere that cannot execute it.
    calls, slept = probes([200])

    with pytest.raises(ArcadeSchemaError) as caught:
        arcade.wait_until_ready(BASE, timeout_s=180.0, poll_interval_s=0.01)

    assert len(calls) == 1
    assert slept == []
    assert "200" in str(caught.value)


def test_a_404_on_the_ready_path_is_not_mistaken_for_a_server_still_starting(probes):
    calls, _ = probes([_http(404)])

    with pytest.raises(ArcadeSchemaError):
        arcade.wait_until_ready(BASE, poll_interval_s=0.01)

    assert len(calls) == 1


def test_ensure_schema_waits_before_it_writes_anything(monkeypatch):
    """The ordering IS the bug: ensure_schema wrote first and asked afterwards."""
    order: list[str] = []

    monkeypatch.setattr(
        "ingest.arcade.wait_until_ready",
        lambda base_url, **kwargs: order.append("wait"),  # noqa: ARG005
    )
    monkeypatch.setattr(
        "ingest.arcade._post",
        lambda *a, **k: order.append("post") or {},  # noqa: ARG005
    )

    arcade.ensure_schema(BASE, "mem_x", ("root", "pw"), 768)

    assert order[0] == "wait", "ensure_schema issued a write before ArcadeDB was ready"
    assert "post" in order


def test_ensure_schema_hands_the_wait_its_own_budget_not_the_ddl_timeout(monkeypatch):
    # The DDL timeout is per-statement and small; the readiness budget must outlast a
    # cold JVM. Collapsing the two would reintroduce the crash on a slow host.
    seen: dict = {}

    monkeypatch.setattr(
        "ingest.arcade.wait_until_ready",
        lambda base_url, **kwargs: seen.update(kwargs),
    )
    monkeypatch.setattr("ingest.arcade._post", lambda *a, **k: {})

    arcade.ensure_schema(BASE, "mem_x", ("root", "pw"), 768, timeout_s=5.0, ready_timeout_s=42.0)

    assert seen["timeout_s"] == 42.0


def test_a_rejected_schema_statement_still_fails_on_the_first_attempt(monkeypatch):
    """The regression the fix must not introduce: retrying a real rejection."""
    attempts: list[str] = []

    def fake_urlopen(request, timeout=None):  # noqa: ARG001
        attempts.append(request.full_url)
        if request.full_url.endswith(arcade.READY_PATH):
            return _Response(204)
        raise _http(400, b"Invalid DDL")

    monkeypatch.setattr("ingest.arcade.urllib.request.urlopen", fake_urlopen)
    monkeypatch.setattr(arcade.time, "sleep", lambda _s: pytest.fail("a rejection was retried"))

    with pytest.raises(ArcadeSchemaError) as caught:
        arcade.ensure_schema(BASE, "mem_x", ("root", "pw"), 768)

    assert "400" in str(caught.value)
    # ready, then the create-database that got rejected. Nothing more.
    assert len(attempts) == 2


def test_the_wait_is_reported_once_not_on_every_probe(probes, capsys):
    # A three-minute silent block reads as a hang; a line every two seconds reads as the
    # traceback storm this replaces. One line entering the wait, one leaving it.
    probes([_refused(), _refused(), _refused(), _refused(), 204])

    arcade.wait_until_ready(BASE, poll_interval_s=0.01)

    lines = [ln for ln in capsys.readouterr().out.splitlines() if ln.strip()]
    assert len(lines) == 2, f"expected one line in and one out, got {lines}"
    assert "Connection refused" in lines[0]
    assert BASE in lines[0]


def test_a_server_that_was_up_all_along_says_nothing(probes, capsys):
    probes([204])

    arcade.wait_until_ready(BASE)

    assert capsys.readouterr().out == "", "the common case must stay silent"
