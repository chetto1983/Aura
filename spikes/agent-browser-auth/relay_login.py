"""Host side of the exec relay: logs into the fixture through `docker exec -i <box>
node ws_relay.mjs <port>` only, and measures what a cockpit viewer would see.

Usage: relay_login.py <container> <port> '<email box json>' '<password box json>'
"""

import json
import subprocess
import sys
import threading
import time

container, port, user_box, pass_box = sys.argv[1:5]
relay = subprocess.Popen(
    ["docker", "exec", "-i", "-w", "/workspace", container, "node", "spike/ws_relay.mjs", port],
    stdin=subprocess.PIPE, stdout=subprocess.PIPE, text=True, bufsize=1,
)
frames: list[tuple[float, int, int]] = []
urls: list[str] = []


def read():
    for line in relay.stdout:
        msg = json.loads(line)
        if msg.get("type") == "frame":
            frames.append((time.time(), len(line), msg["metadata"]["timestamp"]))
        elif msg.get("type") == "url":
            urls.append(msg["url"])


threading.Thread(target=read, daemon=True).start()


def send(**o):
    relay.stdin.write(json.dumps(o) + "\n")
    relay.stdin.flush()


def click(box):
    b = json.loads(box)
    x, y = b["x"] + b["width"] / 2, b["y"] + b["height"] / 2
    for ev in ("mousePressed", "mouseReleased"):
        send(type="input_mouse", eventType=ev, x=x, y=y, button="left", clickCount=1)
    time.sleep(0.15)


def type_text(text):
    for ch in text:
        send(type="input_keyboard", eventType="keyDown", key=ch, code="", text=ch, windowsVirtualKeyCode=0, modifiers=0)
        send(type="input_keyboard", eventType="keyUp", key=ch, code="", windowsVirtualKeyCode=0, modifiers=0)
    time.sleep(0.15)


time.sleep(2)
idle = len(frames)
t0 = time.time()
click(user_box)
while len(frames) == idle and time.time() - t0 < 3:
    time.sleep(0.005)
first_ms = round((frames[idle][0] - t0) * 1000) if len(frames) > idle else None
type_text("alice@example.test")
click(pass_box)
type_text("Sp1ke-Passw0rd!x7")
send(type="input_keyboard", eventType="keyDown", key="Enter", code="Enter", windowsVirtualKeyCode=13, text="\r", modifiers=0)
send(type="input_keyboard", eventType="keyUp", key="Enter", code="Enter", windowsVirtualKeyCode=13, modifiers=0)
time.sleep(2.5)
relay.stdin.close()
relay.wait(timeout=5)

active = frames[idle:]
secs = (active[-1][0] - active[0][0]) or 1
lag = sorted(round(at * 1000 - cap) for at, _, cap in active)
print(json.dumps({
    "transport": "docker exec -i relay (NDJSON over stdin/stdout)",
    "active_frames": len(active),
    "active_fps": round(len(active) / secs, 1),
    "line_kb_median": round(sorted(n for _, n, _ in active)[len(active) // 2] / 1024, 1),
    "first_frame_after_click_ms": first_ms,
    "capture_to_host_ms_median": lag[len(lag) // 2],
    "last_url": urls[-1] if urls else None,
}, indent=1))
