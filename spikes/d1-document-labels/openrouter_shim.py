"""Serve llama.cpp's /v1/systemone on localhost and forward it to OpenRouter's Decisions API.

It lets the spikes' unchanged benchmarks (gate.py, labels.py, model_comparison.py) run against a
hosted decision model. Three differences between the two contracts are bridged, and only these:
- OpenRouter needs `model`; the shim sets it from --model.
- OpenRouter needs noul `criteria` with "true" and "false" keys (api-reference, DecisionsRequest);
  llama.cpp takes `instructions` alone. A noul without criteria gets {"true": "Yes", "false": "No"}.
- The benchmarks read /v1/models and /props for their manifests; the shim answers both itself.
The key is read from OPENROUTER_API_KEY and never logged.
"""

import argparse
import json
import os
import urllib.error
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

ENDPOINT = "https://openrouter.ai/api/alpha/decisions"


def handler(model: str, key: str):
    class Shim(BaseHTTPRequestHandler):
        def _send(self, status: int, body: bytes) -> None:
            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def do_GET(self) -> None:  # noqa: N802 -- http.server's name
            if self.path == "/v1/models":
                self._send(200, json.dumps({"data": [{"id": model, "route": "openrouter"}]}).encode())
            elif self.path in ("/props", "/health"):
                self._send(200, json.dumps({"route": "openrouter", "model": model}).encode())
            else:
                self._send(404, b"{}")

        def do_POST(self) -> None:  # noqa: N802
            if self.path != "/v1/systemone":
                self._send(404, b"{}")
                return
            payload = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
            payload["model"] = model
            for question in payload.get("questions", {}).values():
                if question.get("type") == "noul" and not question.get("criteria"):
                    question["criteria"] = {"true": "Yes", "false": "No"}
            request = urllib.request.Request(ENDPOINT, data=json.dumps(payload).encode(), headers={
                "Authorization": f"Bearer {key}", "Content-Type": "application/json"})
            try:
                with urllib.request.urlopen(request, timeout=300) as response:
                    self._send(200, response.read())
            except urllib.error.HTTPError as err:
                self._send(err.code, err.read())
            except (urllib.error.URLError, OSError) as err:
                # A dropped TLS stream is the upstream's failure, not the shim's: answer as a gateway.
                self._send(502, json.dumps({"error": {"code": 502, "message": type(err).__name__}}).encode())

        def log_message(self, *args) -> None:
            pass

    return Shim


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--model", required=True)
    parser.add_argument("--port", type=int, default=8090)
    args = parser.parse_args()
    key = os.environ["OPENROUTER_API_KEY"]
    ThreadingHTTPServer(("127.0.0.1", args.port), handler(args.model, key)).serve_forever()


if __name__ == "__main__":
    main()
