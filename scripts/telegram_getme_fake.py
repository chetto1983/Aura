#!/usr/bin/env python3
"""A Telegram Bot API stand-in for CI that answers getMe and nothing else.

Provisioning a second identity names the bot in the onboarding deep link through a live getMe
(internal/agui/onboarding_provision.go), so a CI with no bot cannot create one. web-e2e resolves
api.telegram.org to 127.0.0.1 and gives aura serve a CA bundle holding the CA that signed CERT
(scripts/web_e2e_telegram_fake.sh): the product's own client then reaches this server unchanged.

    telegram_getme_fake.py CERT KEY [PORT]
"""

import json
import ssl
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

BOT = {"id": 1, "is_bot": True, "first_name": "Aura CI", "username": "aura_ci_bot"}


class Handler(BaseHTTPRequestHandler):
    def _answer(self) -> None:
        length = int(self.headers.get("Content-Length") or 0)
        if length:
            self.rfile.read(length)
        if self.path.split("?", 1)[0].endswith("/getMe"):
            status, body = 200, {"ok": True, "result": BOT}
        else:
            status = 404
            body = {"ok": False, "error_code": 404, "description": "Not Found: only getMe is served"}
        data = json.dumps(body).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    do_GET = _answer
    do_POST = _answer

    # Same signature as BaseHTTPRequestHandler.log_message, whose parameter is named `format`.
    def log_message(self, format: str, *args: object) -> None:  # pylint: disable=redefined-builtin
        # The request line carries the bot token: log the method name only.
        method = self.command or "?"
        sys.stderr.write(f"telegram-fake: {method} {self.path.rsplit('/', 1)[-1]}\n")


def main() -> None:
    cert, key = sys.argv[1], sys.argv[2]
    port = int(sys.argv[3]) if len(sys.argv) > 3 else 443
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.load_cert_chain(cert, key)
    server = ThreadingHTTPServer(("127.0.0.1", port), Handler)
    server.socket = context.wrap_socket(server.socket, server_side=True)
    server.serve_forever()


if __name__ == "__main__":
    main()
