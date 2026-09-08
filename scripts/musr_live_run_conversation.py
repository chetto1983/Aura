#!/usr/bin/env python3
"""One identity's POST /agent/run SSE conversation: streams the response, timestamps every
frame at receive time, writes it to a transcript jsonl, and writes tool-call start/end timing
entries to a per-identity timing jsonl (merged across identities by the caller once both have
finished — see scripts/musr_live_run_merge_timings.py). stdlib only (http.client for the
streaming POST — requests/urllib3 are not assumed present).

Two instances of this script are launched as background jobs by scripts/musr_live_run.sh,
released together, one per identity — that concurrency is what makes the captured timings
prove an actual race rather than a a sequential ordering wearing raced-looking timestamps.

Usage: musr_live_run_conversation.py <base_url> <cookie> <thread_id> <prompt>
                                      <transcript_path> <timing_path> <label a|b>
Exit code: 0 iff the SSE stream reached a terminal RUN_FINISHED frame.
"""
import http.client
import json
import sys
import time
import urllib.parse

TERMINAL_EVENTS = ("RUN_FINISHED", "RUN_ERROR")


def main() -> None:
    if len(sys.argv) != 8:
        sys.stderr.write(
            "usage: musr_live_run_conversation.py <base_url> <cookie> <thread_id> <prompt> "
            "<transcript_path> <timing_path> <label a|b>\n"
        )
        sys.exit(2)
    base_url, cookie, thread_id, prompt, transcript_path, timing_path, label = sys.argv[1:8]

    parsed = urllib.parse.urlsplit(base_url)
    conn = http.client.HTTPConnection(parsed.hostname, parsed.port, timeout=360)
    body = json.dumps(
        {"threadId": thread_id, "messages": [{"id": "m1", "role": "user", "content": prompt}]},
        separators=(",", ":"),
    )
    headers = {
        "Cookie": cookie,
        "Content-Type": "application/json",
        "Idempotency-Key": f"musr-live-run-{label}-{thread_id}",
    }
    conn.request("POST", "/agent/run", body=body, headers=headers)
    resp = conn.getresponse()
    if resp.status != 200:
        sys.stderr.write(f"musr_live_run[{label}]: POST /agent/run returned HTTP {resp.status}\n")
        sys.stderr.write(resp.read().decode("utf-8", "replace") + "\n")
        sys.exit(1)

    ok = stream_sse(resp, transcript_path, timing_path, label)
    conn.close()
    sys.exit(0 if ok else 1)


def stream_sse(resp, transcript_path: str, timing_path: str, label: str) -> bool:
    """Reads resp byte-by-byte (SSE frames are line-delimited and can arrive at any pace),
    stamping each line at receive time so tool-call start/end intervals reflect real wall-clock
    arrival, not a batch read after the fact. Returns True iff the terminal frame was
    RUN_FINISHED."""
    tool_names: dict[str, str] = {}
    last_event = ""
    ok = False
    with open(transcript_path, "w", encoding="utf-8") as tf, open(
        timing_path, "w", encoding="utf-8"
    ) as tmf:
        event = None
        buf = b""
        while True:
            chunk = resp.read(1)
            if not chunk:
                break
            buf += chunk
            if not buf.endswith(b"\n"):
                continue
            ts = time.time()
            line = buf.decode("utf-8", "replace").rstrip("\r\n")
            buf = b""
            if line.startswith("event:"):
                event = line[len("event:"):].strip()
                continue
            if not (line.startswith("data:") and event):
                continue
            raw = line[len("data:"):].strip()
            try:
                data = json.loads(raw) if raw else {}
            except json.JSONDecodeError:
                data = raw
            tf.write(json.dumps({"ts": ts, "event": event, "data": data}) + "\n")
            tf.flush()
            record_tool_timing(tmf, event, data, tool_names, label, ts)
            last_event = event
            if event in TERMINAL_EVENTS:
                ok = event == "RUN_FINISHED"
                break
            event = None
    sys.stderr.write(f"musr_live_run[{label}]: terminal event = {last_event or 'none'}\n")
    return ok


def record_tool_timing(tmf, event: str, data, tool_names: dict[str, str], label: str, ts: float) -> None:
    if not isinstance(data, dict):
        return
    if event == "TOOL_CALL_START":
        tool_call_id = data.get("toolCallId", "")
        tool_names[tool_call_id] = data.get("toolCallName", "")
        entry = {"identity": label, "tool": data.get("toolCallName", ""),
                  "tool_call_id": tool_call_id, "phase": "start", "ts": ts}
    elif event == "TOOL_CALL_END":
        tool_call_id = data.get("toolCallId", "")
        entry = {"identity": label, "tool": tool_names.get(tool_call_id, ""),
                  "tool_call_id": tool_call_id, "phase": "end", "ts": ts}
    else:
        return
    tmf.write(json.dumps(entry) + "\n")
    tmf.flush()


if __name__ == "__main__":
    main()
