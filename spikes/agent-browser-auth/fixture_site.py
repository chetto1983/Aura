"""Disposable login fixture for the agent-browser spike.

A stdlib-only site shaped like a real document portal: password login, a TOTP
second factor, an HttpOnly session cookie and a protected PDF download. It exists
so the spike measures agent-browser against a flow it does not know, without
automating a third-party site.
"""

import base64
import hashlib
import hmac
import http.server
import json
import os
import secrets
import struct
import sys
import time
import urllib.parse

USERNAME = "alice@example.test"
PASSWORD = "Sp1ke-Passw0rd!x7"
TOTP_SECRET = "JBSWY3DPEHPK3PXP"
# FIXTURE_STATE persists sessions so a box suspend/resume does not log the user out
# on the SERVER side: only the browser's own persistence is then under test.
STATE_FILE = os.environ.get("FIXTURE_STATE")
SESSIONS: dict[str, str] = json.load(open(STATE_FILE)) if STATE_FILE and os.path.exists(STATE_FILE) else {}
PENDING: set[str] = set()
PDF = (
    b"%PDF-1.4\n1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj\n"
    b"2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj\n"
    b"3 0 obj<</Type/Page/Parent 2 0 R/MediaBox[0 0 200 200]>>endobj\n"
    b"trailer<</Root 1 0 R>>\n%%EOF\n"
)


def totp(secret: str, at: float | None = None) -> str:
    key = base64.b32decode(secret)
    counter = int((at or time.time()) // 30)
    digest = hmac.new(key, struct.pack(">Q", counter), hashlib.sha1).digest()
    offset = digest[-1] & 0x0F
    code = struct.unpack(">I", digest[offset : offset + 4])[0] & 0x7FFFFFFF
    return f"{code % 1_000_000:06d}"


def page(title: str, body: str) -> bytes:
    return (
        f"<!doctype html><html><head><meta charset=utf-8><title>{title}</title></head>"
        f"<body><h1>{title}</h1>{body}</body></html>"
    ).encode()


class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):
        sys.stderr.write(f"{self.command} {self.path.split('?')[0]} -> {args[1] if len(args) > 1 else ''}\n")

    def cookie(self, name: str) -> str | None:
        for part in (self.headers.get("Cookie") or "").split(";"):
            k, _, v = part.strip().partition("=")
            if k == name:
                return v
        return None

    def send(self, code: int, body: bytes, ctype="text/html", headers=()):
        self.send_response(code)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(body)))
        for k, v in headers:
            self.send_header(k, v)
        self.end_headers()
        self.wfile.write(body)

    def redirect(self, to: str, headers=()):
        self.send(303, b"", headers=[("Location", to), *headers])

    def form(self) -> dict[str, str]:
        raw = self.rfile.read(int(self.headers.get("Content-Length") or 0)).decode()
        return {k: v[0] for k, v in urllib.parse.parse_qs(raw).items()}

    def do_GET(self):
        path = self.path.split("?")[0]
        user = SESSIONS.get(self.cookie("sid") or "")
        if path in ("/", "/login"):
            self.send(200, page("Sign in", (
                '<form method=post action=/login>'
                '<label for=u>Email</label><input id=u name=username type=email autocomplete=username>'
                '<label for=p>Password</label><input id=p name=password type=password autocomplete=current-password>'
                '<button type=submit>Sign in</button></form>')))
        elif path == "/otp":
            self.send(200, page("Verification code", (
                '<form method=post action=/otp>'
                '<label for=c>Code</label><input id=c name=code inputmode=numeric autocomplete=one-time-code>'
                '<button type=submit>Verify</button></form>')))
        elif path == "/docs":
            if not user:
                return self.redirect("/login")
            self.send(200, page("Documents", f'<p>Signed in as {user}</p><a href=/docs/report.pdf download>report.pdf</a>'))
        elif path == "/docs/report.pdf":
            if not user:
                return self.send(403, page("Forbidden", "<p>login required</p>"))
            self.send(200, PDF, "application/pdf", [("Content-Disposition", 'attachment; filename="report.pdf"')])
        else:
            self.send(404, page("Not found", ""))

    def do_POST(self):
        path = self.path.split("?")[0]
        f = self.form()
        if path == "/login":
            ok = f.get("username") == USERNAME and f.get("password") == PASSWORD
            sys.stderr.write(f"LOGIN username_ok={f.get('username') == USERNAME} password_ok={f.get('password') == PASSWORD}\n")
            if not ok:
                return self.send(401, page("Sign in failed", "<a href=/login>retry</a>"))
            pre = secrets.token_hex(16)
            PENDING.add(pre)
            self.redirect("/otp", [("Set-Cookie", f"pre={pre}; Path=/; HttpOnly; SameSite=Lax")])
        elif path == "/otp":
            pre = self.cookie("pre")
            ok = pre in PENDING and f.get("code") == totp(TOTP_SECRET)
            sys.stderr.write(f"OTP ok={ok}\n")
            if not ok:
                return self.send(401, page("Code rejected", "<a href=/otp>retry</a>"))
            PENDING.discard(pre)
            sid = secrets.token_hex(16)
            SESSIONS[sid] = USERNAME
            if STATE_FILE:
                with open(STATE_FILE, "w") as f:
                    json.dump(SESSIONS, f)
            self.redirect("/docs", [("Set-Cookie", f"sid={sid}; Path=/; HttpOnly; SameSite=Lax; Max-Age=86400")])
        else:
            self.send(404, b"")


if __name__ == "__main__":
    if sys.argv[1:] == ["totp"]:
        print(totp(TOTP_SECRET))
        sys.exit(0)
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 8765
    http.server.ThreadingHTTPServer(("127.0.0.1", port), Handler).serve_forever()
