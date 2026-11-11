#!/usr/bin/env python3
"""Drive the cockpit's HTTP API as the bootstrapped operator, the way the browser does.

usage (env.e2e.sh sourced, as for every command of the run):
  cockpit.py login                   sign in operator@aura.test
  cockpit.py get PATH                e.g. /api/approvals, /api/message-drafts?conversation_id=ID
  cockpit.py put|post PATH JSON      e.g. post /api/conversations '{}'
  cockpit.py run THREAD TEXT         a chat turn; the AG-UI stream goes to stdout
  cockpit.py resume THREAD           the turn after an approval resolved with outcome "continue"
"""
import json
import os
import sys
import urllib.error
import urllib.request
import uuid

BASE = "http://127.0.0.1:9080"
STATE = os.path.join(os.environ["HAND"], "cockpit-session.json")


def load_state():
    if os.path.exists(STATE):
        with open(STATE) as f:
            return json.load(f)
    return {"cookies": {}, "csrf": {}}


state = load_state()


def save_state():
    # It holds the operator's session cookie: owner-only, like any credential file.
    fd = os.open(STATE, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    os.fchmod(fd, 0o600)
    with os.fdopen(fd, "w") as f:
        json.dump(state, f)


def call(method, path, body=None, stream=False):
    data = None if body is None else json.dumps(body).encode()
    req = urllib.request.Request(BASE + path, data=data, method=method)
    req.add_header("Content-Type", "application/json")
    if method != "GET":
        req.add_header("Idempotency-Key", str(uuid.uuid4()))
    for name, value in state["csrf"].items():
        req.add_header(name, value)
    # Authula's cookies are Secure (__Host-). A browser still sends them to http://127.0.0.1,
    # a trustworthy origin; urllib does not, so they are kept and sent by hand.
    if state["cookies"]:
        req.add_header("Cookie", "; ".join(f"{k}={v}" for k, v in state["cookies"].items()))
    try:
        res = urllib.request.urlopen(req, timeout=None if stream else 60)
    except urllib.error.HTTPError as e:
        sys.exit(f"HTTP {e.code}: {e.read().decode()[:400]}")
    for header in res.headers.get_all("Set-Cookie") or []:
        name, _, rest = header.partition("=")
        state["cookies"][name.strip()] = rest.split(";", 1)[0]
    save_state()
    if stream:
        for line in res:
            sys.stdout.write(line.decode())
            sys.stdout.flush()
        return None, None
    return res.read().decode(), res.headers


def login():
    raw, headers = call("GET", "/api/auth/config")
    cfg = json.loads(raw)
    name = cfg.get("csrf_header_name", "X-AUTHULA-CSRF-TOKEN")
    # The header token pairs with the cookie set by the same response; the body's does not.
    state["csrf"] = {name: headers.get(name) or cfg.get("csrf_token", "")}
    raw, _ = call("POST", cfg.get("auth_base_path", "/auth") + "/email-password/sign-in",
                  {"email": os.environ["AURA_E2E_OPERATOR_EMAIL"],
                   "password": os.environ["AURA_E2E_OPERATOR_PASSWORD"]})
    print(raw[:300])


def run(thread, text=None):
    messages = [] if text is None else [{"id": uuid.uuid4().hex, "role": "user", "content": text}]
    call("POST", "/agent/run", {"threadId": thread, "messages": messages}, stream=True)


cmd, args = sys.argv[1], sys.argv[2:]
if cmd == "login":
    login()
elif cmd == "get":
    print(call("GET", args[0])[0])
elif cmd in ("put", "post"):
    print(call(cmd.upper(), args[0], json.loads(args[1]))[0])
elif cmd == "run":
    run(args[0], args[1])
elif cmd == "resume":
    run(args[0])
else:
    sys.exit(__doc__)
