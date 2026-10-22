"""Condition hooks: a saved condition on a ticker that, when it fires, delivers a JSON event.

Delivery is at-least-once and edge-triggered: on creation the hook remembers the latest past
event so history is not replayed; each run delivers the events newer than that and advances
the bookmark only for the deliveries that succeeded, so a failed webhook retries next run.

Webhooks are refused unless the operator lists allowed URL prefixes in
BROKER_ANALYSIS_WEBHOOK_ALLOW: an agent must not be able to make the server call arbitrary hosts.
File deliveries append JSON lines under $BROKER_ANALYSIS_HOME/alerts/.
"""

import json
import os
import re
import urllib.request
from collections.abc import Callable
from datetime import UTC, datetime
from pathlib import Path

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
    return json.loads(path.read_text()) if path.exists() else {}


def _save(hooks: dict[str, dict]) -> None:
    tmp = _store().with_suffix(".tmp")
    tmp.write_text(json.dumps(hooks, indent=1, ensure_ascii=False))
    tmp.replace(_store())


def _allowed_prefixes() -> list[str]:
    raw = os.environ.get("BROKER_ANALYSIS_WEBHOOK_ALLOW", "")
    return [p.strip() for p in raw.split(",") if p.strip()]


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
    if not any(action.target.startswith(p) for p in allowed):
        raise SpecError(f"webhook target not in the allowed prefixes {allowed}")


def _evaluate(hook: dict, loader: Loader) -> tuple[pd.DataFrame, dict, pd.Series, data.Meta]:
    prices, meta = loader(hook["ticker"], hook["period"])
    indicators = [IndicatorSpec(**i) for i in hook["indicators"]]
    series = [SeriesSpec(**s) for s in hook["series"]]
    ns, _ = custom.build(prices, indicators, series)
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


def deliver(action: dict, payload: dict) -> None:
    if action["type"] == "file":
        with (home() / "alerts").joinpath(f"{action['target']}.jsonl").open("a") as f:
            f.write(json.dumps(payload, ensure_ascii=False) + "\n")
        return
    check_action(HookAction(**action))
    request = urllib.request.Request(
        action["target"], data=json.dumps(payload).encode(), headers={"Content-Type": "application/json"}, method="POST"
    )
    with urllib.request.urlopen(request, timeout=10) as response:  # target is prefix-allowlisted by check_action
        if response.status >= 300:
            raise OSError(f"webhook answered {response.status}")


def run(loader: Loader = data.history) -> dict:
    (home() / "alerts").mkdir(exist_ok=True)
    hooks = load_hooks()
    fired, failed, checked = [], [], []
    for hook_id, hook in hooks.items():
        try:
            prices, ns, events, meta = _evaluate(hook, loader)
        except (data.DataError, SpecError) as err:
            failed.append({"hook_id": hook_id, "error": str(err)})
            continue
        checked.append({"hook_id": hook_id, "data_until": str(prices.index[-1].date())})
        new = [d for d in events.index[events.to_numpy()] if hook["last_event"] is None or str(d.date()) > hook["last_event"]]
        for date in new:
            payload = _payload(hook, date, ns, meta)
            try:
                deliver(hook["action"], payload)
            except (OSError, SpecError) as err:
                failed.append({"hook_id": hook_id, "event_date": payload["event_date"], "error": str(err)})
                break
            hook["last_event"] = payload["event_date"]
            fired.append(payload)
    _save(hooks)
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
