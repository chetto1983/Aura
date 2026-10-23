"""Condition hooks: creation, edge-triggered delivery, retries, file and webhook actions."""

import json
import threading
from http.server import BaseHTTPRequestHandler, HTTPServer
from typing import ClassVar

import numpy as np
import pandas as pd
import pytest

from broker_analysis_mcp import data, hooks
from broker_analysis_mcp.catalog import SpecError
from broker_analysis_mcp.models import ConditionSpec, HookAction, IndicatorSpec


def frame(closes: list[float]) -> pd.DataFrame:
    c = np.array(closes)
    return pd.DataFrame(
        {"Open": c, "High": c * 1.01, "Low": c * 0.99, "Close": c, "Volume": np.ones(len(c))},
        index=pd.bdate_range("2025-01-01", periods=len(c)),
    )


class Market:
    """A loader whose history grows as the test appends bars."""

    def __init__(self, closes: list[float]):
        self.closes = list(closes)

    def __call__(self, ticker: str, period: str):
        return frame(self.closes), data.Meta(ticker=ticker, currency="EUR")


CROSS = ConditionSpec(name="Above SMA3", when="crosses_above(close, sma3)")
SMA3 = [IndicatorSpec(id="sma3", fn="sma", params={"n": 3})]
# Crosses above its 3-bar mean on bar 4 (index 3) and again on bar 9.
HISTORY = [10, 9, 8, 11, 12, 11, 9, 8, 12, 13]


@pytest.fixture(autouse=True)
def isolated_home(tmp_path, monkeypatch):
    monkeypatch.setenv("BROKER_ANALYSIS_HOME", str(tmp_path))
    monkeypatch.delenv("BROKER_ANALYSIS_WEBHOOK_ALLOW", raising=False)


def test_creation_bookmarks_history_and_does_not_replay_it() -> None:
    market = Market(HISTORY)
    created = hooks.create("cross", "TEST.MI", CROSS, HookAction(type="file", target="alerts"), SMA3, [], loader=market)
    assert created["past_events"] == 2
    assert created["hook"]["last_event"] == str(frame(HISTORY).index[8].date())
    assert hooks.run(loader=market)["fired"] == []


def test_new_event_fires_once_and_is_written_to_file(tmp_path) -> None:
    market = Market(HISTORY)
    hooks.create("cross", "TEST.MI", CROSS, HookAction(type="file", target="alerts"), SMA3, [], loader=market)
    market.closes += [10, 9, 14]  # drops below, then crosses above again on the last bar
    first = hooks.run(loader=market)
    assert [e["event_date"] for e in first["fired"]] == [str(frame(market.closes).index[-1].date())]
    payload = first["fired"][0]
    assert payload["values"]["close"] == 14 and "sma3" in payload["values"]
    assert hooks.run(loader=market)["fired"] == []
    lines = (tmp_path / "alerts" / "alerts.jsonl").read_text().splitlines()
    assert len(lines) == 1 and json.loads(lines[0])["ticker"] == "TEST.MI"


def test_webhooks_need_an_operator_allowlist() -> None:
    with pytest.raises(SpecError, match="disabled"):
        hooks.check_action(HookAction(type="webhook", target="https://example.com/hook"))


@pytest.mark.parametrize(
    "target",
    [
        "https://hooks.example.com/broker/../admin",
        "https://hooks.example.com/broker/%2e%2E/admin",  # percent-encoded
        "https://hooks.example.com/broker/..%2fadmin",  # encoded separator
        "https://hooks.example.com/broker/./x",
    ],
)
def test_dot_segments_cannot_leave_the_allowed_path(monkeypatch, target) -> None:
    # A receiver or proxy that resolves dot segments (RFC 3986, 5.2.4) would deliver outside the prefix.
    monkeypatch.setenv("BROKER_ANALYSIS_WEBHOOK_ALLOW", "https://hooks.example.com/broker")
    hooks.check_action(HookAction(type="webhook", target="https://hooks.example.com/broker/alerts"))
    with pytest.raises(SpecError, match="not in the allowed"):
        hooks.check_action(HookAction(type="webhook", target=target))


@pytest.mark.parametrize("target", ["../escape", "a/b", "x.jsonl", ""])
def test_file_targets_cannot_escape_the_alerts_folder(target) -> None:
    with pytest.raises(SpecError):
        hooks.check_action(HookAction(type="file", target=target))


class _Receiver(BaseHTTPRequestHandler):
    received: ClassVar[list] = []
    status = 200

    def do_POST(self):
        body = self.rfile.read(int(self.headers["Content-Length"]))
        _Receiver.received.append(json.loads(body))
        self.send_response(_Receiver.status)
        if 300 <= _Receiver.status < 400:
            self.send_header("Location", "http://169.254.169.254/latest/meta-data")
        self.end_headers()

    def log_message(self, *args):
        pass


@pytest.fixture
def receiver(monkeypatch):
    server = HTTPServer(("127.0.0.1", 0), _Receiver)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    url = f"http://127.0.0.1:{server.server_port}"
    monkeypatch.setenv("BROKER_ANALYSIS_WEBHOOK_ALLOW", url)
    _Receiver.received, _Receiver.status = [], 200
    yield url
    server.shutdown()


def test_webhook_delivery_to_an_allowed_url(receiver) -> None:
    market = Market(HISTORY)
    hooks.create("cross", "TEST.MI", CROSS, HookAction(type="webhook", target=f"{receiver}/hook"), SMA3, [], loader=market)
    port = receiver.rsplit(":", 1)[1]
    look_alikes = [
        "http://169.254.169.254/latest",
        f"{receiver}.evil.example/hook",  # the allowed text continues into another host
        f"http://127.0.0.1:{port}@evil.example/hook",  # the allowed text is only the userinfo
        f"{receiver}0/hook",  # a longer port that starts with the allowed one
    ]
    for target in look_alikes:
        with pytest.raises(SpecError, match="not in the allowed"):
            hooks.check_action(HookAction(type="webhook", target=target))
    hooks.check_action(HookAction(type="webhook", target=f"{receiver}/hook"))
    market.closes += [10, 9, 14]
    assert len(hooks.run(loader=market)["fired"]) == 1
    assert _Receiver.received[0]["condition"] == "crosses_above(close, sma3)"


def test_failed_webhook_is_retried_on_the_next_run(receiver) -> None:
    market = Market(HISTORY)
    hooks.create("cross", "TEST.MI", CROSS, HookAction(type="webhook", target=f"{receiver}/hook"), SMA3, [], loader=market)
    market.closes += [10, 9, 14]
    _Receiver.status = 500
    failed = hooks.run(loader=market)
    assert failed["fired"] == [] and len(failed["failed"]) == 1
    _Receiver.status = 200
    assert len(hooks.run(loader=market)["fired"]) == 1


def test_an_allowed_host_cannot_redirect_the_delivery(receiver) -> None:
    market = Market(HISTORY)
    hooks.create("cross", "TEST.MI", CROSS, HookAction(type="webhook", target=f"{receiver}/hook"), SMA3, [], loader=market)
    market.closes += [10, 9, 14]
    _Receiver.status = 302  # points at the cloud metadata address
    run = hooks.run(loader=market)
    assert run["fired"] == [] and "302" in run["failed"][0]["error"]
    assert len(_Receiver.received) == 1  # only the allowed host was called


def test_a_failing_hook_does_not_stop_the_others_and_bookmarks_survive() -> None:
    market = Market(HISTORY)
    ok = hooks.create("ok", "TEST.MI", CROSS, HookAction(type="file", target="a"), SMA3, [], loader=market)["hook"]["id"]
    broken = hooks.create("broken", "BAD.MI", CROSS, HookAction(type="file", target="a"), SMA3, [], loader=market)["hook"]["id"]
    market.closes += [10, 9, 14]

    def loader(ticker: str, period: str):
        if ticker == "BAD.MI":
            raise KeyError("unexpected payload")  # not a DataError: the kind that used to escape
        return market(ticker, period)

    run = hooks.run(loader=loader)
    assert [e["hook_id"] for e in run["fired"]] == [ok]
    assert run["failed"][0]["hook_id"] == broken and run["failed"][0]["error"].startswith("KeyError")
    assert hooks.load_hooks()[ok]["last_event"] == str(frame(market.closes).index[-1].date())


def test_a_hook_created_during_a_run_is_kept() -> None:
    market = Market(HISTORY)
    hooks.create("first", "TEST.MI", CROSS, HookAction(type="file", target="a"), SMA3, [], loader=market)
    market.closes += [10, 9, 14]

    def loader(ticker: str, period: str):
        hooks.create("second", "NEW.MI", CROSS, HookAction(type="file", target="a"), SMA3, [], loader=market)
        return market(ticker, period)

    hooks.run(loader=loader)
    assert sorted(h["name"] for h in hooks.load_hooks().values()) == ["first", "second"]


def test_delete() -> None:
    market = Market(HISTORY)
    hook_id = hooks.create("cross", "TEST.MI", CROSS, HookAction(type="file", target="a"), SMA3, [], loader=market)["hook"]["id"]
    assert hooks.delete(hook_id) is True
    assert hooks.load_hooks() == {}
