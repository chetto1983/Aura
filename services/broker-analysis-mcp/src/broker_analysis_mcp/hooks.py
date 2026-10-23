"""Condition hooks: a saved condition on a ticker that, when it fires, delivers a JSON event.

Delivery is at-least-once and edge-triggered: on creation the hook remembers the latest past
event so history is not replayed; each run delivers the events newer than that and advances
the bookmark only for the deliveries that succeeded, so a failed webhook retries next run.

Webhooks are refused unless the operator lists allowed URL prefixes in
BROKER_ANALYSIS_WEBHOOK_ALLOW: an agent must not be able to make the server call arbitrary hosts.
A target matches a prefix on the exact scheme and host[:port] and at a path-segment boundary, and
redirects are not followed, so neither a look-alike host nor an allowed host's redirect reaches
an address the operator did not list. File deliveries append JSON lines under
$BROKER_ANALYSIS_HOME/alerts/.
"""

import json
import os
import re
import urllib.request
from collections.abc import Callable
from datetime import UTC, datetime
from pathlib import Path
from urllib.parse import urlsplit

import pandas as pd

from . import custom, data, expr
from .catalog import SpecError
from .models import ConditionSpec, HookAction, IndicatorSpec, SeriesSpec

Loader = Callable[[str, str], tuple[pd.DataFrame, data.Meta]]
FILE_NAME = re.compile(r"^[A-Za-z0-9_-]{1,64}$")


def home() -> Path:
    path = Path(os.environ.get("BROKER_ANALYSIS_HOME", Path.home() / ".broker-analysis"))
    path.mkdir(parents=True, exist_ok=True)
    return path


def _store() -> Path:
    return home() / "hooks.json"


def load_hooks() -> dict[str, dict]:
    path = _store()
    return json.loads(path.read_text(encoding="utf-8")) if path.exists() else {}


def _save(hooks: dict[str, dict]) -> None:
    tmp = _store().with_suffix(".tmp")
    tmp.write_text(json.dumps(hooks, indent=1, ensure_ascii=False), encoding="utf-8")
    tmp.replace(_store())


def _allowed_prefixes() -> list[str]:
    raw = os.environ.get("BROKER_ANALYSIS_WEBHOOK_ALLOW", "")
    return [p.strip() for p in raw.split(",") if p.strip()]


def _within(target: str, prefix: str) -> bool:
    t, p = urlsplit(target), urlsplit(prefix)
    base = p.path.rstrip("/")
    return (t.scheme, t.netloc.lower()) == (p.scheme, p.netloc.lower()) and (
        t.path == base or t.path.startswith(base + "/")
    )


def check_action(action: HookAction) -> None:
    if action.type == "file":
        if not FILE_NAME.fullmatch(action.target):
            raise SpecError("file target must be a plain name of letters, digits, _ or - (no paths)")
        return
    if not action.target.startswith(("https://", "http://")):
        raise SpecError("webhook target must be an http(s) URL")
    allowed = _allowed_prefixes()
    if not allowed:
        raise SpecError("webhooks are disabled: the operator must set BROKER_ANALYSIS_WEBHOOK_ALLOW")
    if not any(_within(action.target, p) for p in allowed):
        raise SpecError(f"webhook target not in the allowed prefixes {allowed}")


def _evaluate(hook: dict, loader: Loader) -> tuple[pd.DataFrame, dict, pd.Series, data.Meta]:
    prices, meta = loader(hook["ticker"], hook["period"])
    indicators = [IndicatorSpec(**i) for i in hook["indicators"]]
    series = [SeriesSpec(**s) for s in hook["series"]]
    ns, _ = custom.build(prices, indicators, series, meta.periods_per_year)
    _, events = custom.condition_series(ConditionSpec(**hook["condition"]), ns)
    return prices, ns, events, meta


def _slug(name: str) -> str:
    return re.sub(r"[^a-z0-9]+", "-", name.lower()).strip("-")[:40] or "hook"


def create(
    name: str,
    ticker: str,
    condition: ConditionSpec,
    action: HookAction,
    indicators: list[IndicatorSpec],
    series: list[SeriesSpec],
    period: str = "2y",
    loader: Loader = data.history,
) -> dict:
    check_action(action)
    hook = {
        "id": _slug(f"{ticker}-{name}"),
        "name": name,
        "ticker": ticker,
        "period": period,
        "indicators": [i.model_dump() for i in indicators],
        "series": [s.model_dump() for s in series],
        "condition": condition.model_dump(),
        "action": action.model_dump(),
        "created_at": datetime.now(UTC).isoformat(timespec="seconds"),
    }
    prices, _, events, _ = _evaluate(hook, loader)
    dates = [str(d.date()) for d in events.index[events.to_numpy()]]
    hook["last_event"] = dates[-1] if dates else None
    hooks = load_hooks()
    hooks[hook["id"]] = hook
    _save(hooks)
    return {
        "hook": hook,
        "data_until": str(prices.index[-1].date()),
        "past_events": len(dates),
        "recent_events": dates[-5:],
        "note": "past events are not delivered; the hook fires on events after "
        f"{hook['last_event'] or 'its creation'}",
    }


def _payload(hook: dict, date: pd.Timestamp, ns: dict, meta: data.Meta) -> dict:
    referenced = sorted(expr.names(hook["condition"]["when"]) | {"close"})
    return {
        "hook_id": hook["id"],
        "name": hook["name"],
        "ticker": hook["ticker"],
        "condition": hook["condition"]["when"],
        "event_date": str(date.date()),
        "values": {k: round(float(ns[k].loc[date]), 6) for k in referenced if k in ns},
        "currency": meta.currency,
        "source": meta.source,
        "sent_at": datetime.now(UTC).isoformat(timespec="seconds"),
    }


class _NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None  # urllib then raises HTTPError for the 3xx: a failed delivery, retried next run


_OPENER = urllib.request.build_opener(_NoRedirect)


def deliver(action: dict, payload: dict) -> None:
    if action["type"] == "file":
        with (home() / "alerts").joinpath(f"{action['target']}.jsonl").open("a", encoding="utf-8") as f:
            f.write(json.dumps(payload, ensure_ascii=False) + "\n")
        return
    check_action(HookAction(**action))
    request = urllib.request.Request(
        action["target"], data=json.dumps(payload).encode(), headers={"Content-Type": "application/json"}, method="POST"
    )
    with _OPENER.open(request, timeout=10) as response:
        if response.status >= 300:
            raise OSError(f"webhook answered {response.status}")


def _save_bookmarks(bookmarks: dict[str, str]) -> None:
    """Writes advanced bookmarks into the store as it is now, so a hook created or deleted while
    the run was fetching prices is neither lost nor brought back."""
    latest = load_hooks()
    for hook_id, last_event in bookmarks.items():
        if hook_id in latest:
            latest[hook_id]["last_event"] = last_event
    _save(latest)


def run(loader: Loader = data.history) -> dict:
    """Evaluates every hook; one failing hook or delivery is reported and does not stop the others."""
    (home() / "alerts").mkdir(exist_ok=True)
    fired, failed, checked, bookmarks = [], [], [], {}
    try:
        for hook_id, hook in load_hooks().items():
            try:
                prices, ns, events, meta = _evaluate(hook, loader)
            except Exception as err:
                failed.append({"hook_id": hook_id, "error": f"{type(err).__name__}: {err}"})
                continue
            checked.append({"hook_id": hook_id, "data_until": str(prices.index[-1].date())})
            last = hook["last_event"]
            for date in [d for d in events.index[events.to_numpy()] if last is None or str(d.date()) > last]:
                payload = _payload(hook, date, ns, meta)
                try:
                    deliver(hook["action"], payload)
                except Exception as err:
                    failed.append({
                        "hook_id": hook_id, "event_date": payload["event_date"], "error": f"{type(err).__name__}: {err}",
                    })
                    break
                bookmarks[hook_id] = payload["event_date"]
                fired.append(payload)
    finally:
        _save_bookmarks(bookmarks)
    return {"checked": checked, "fired": fired, "failed": failed}


def delete(hook_id: str) -> bool:
    hooks = load_hooks()
    removed = hooks.pop(hook_id, None) is not None
    _save(hooks)
    return removed


def main() -> None:
    """Entry point for cron: evaluate every saved hook once and print what fired."""
    print(json.dumps(run(), ensure_ascii=False, indent=1))


if __name__ == "__main__":
    main()
