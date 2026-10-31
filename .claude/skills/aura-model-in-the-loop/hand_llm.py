#!/usr/bin/env python3
"""A llama.cpp-shaped OpenAI endpoint that parks every completion for a human (or Claude) to answer.

POST /v1/chat/completions writes the request to DIR/req-N.json and waits for DIR/resp-N.json:
  {"content": "text", "tool_calls": [{"id": "c1", "name": "board", "arguments": {...}}]}
While it waits it streams SSE comments so no idle timer fires. GET /props, /v1/models and /health
answer what Aura probes. Usage: hand_llm.py DIR PORT MODEL
"""
import json
import os
import sys
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

DIR, PORT, MODEL = sys.argv[1], int(sys.argv[2]), sys.argv[3]
os.makedirs(DIR, exist_ok=True)
lock = threading.Lock()
counter = [len([f for f in os.listdir(DIR) if f.startswith("req-")])]


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, fmt, *args):
        sys.stderr.write("hand_llm: " + fmt % args + "\n")

    def json(self, code, body):
        raw = json.dumps(body).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def do_GET(self):
        if self.path.rstrip("/").endswith("/props"):
            return self.json(200, {"default_generation_settings": {"n_ctx": 131072}, "model_alias": MODEL})
        if self.path.rstrip("/").endswith("/models"):
            return self.json(200, {"object": "list", "data": [{"id": MODEL, "object": "model"}]})
        return self.json(200, {"status": "ok"})

    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers.get("Content-Length", "0"))) or b"{}")
        with lock:
            counter[0] += 1
            n = counter[0]
        req_path, resp_path = f"{DIR}/req-{n}.json", f"{DIR}/resp-{n}.json"
        with open(req_path + ".tmp", "w") as f:
            json.dump(body, f, indent=1)
        os.rename(req_path + ".tmp", req_path)
        stream = bool(body.get("stream"))
        if stream:
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.send_header("Cache-Control", "no-cache")
            self.end_headers()
        last = time.time()
        while not os.path.exists(resp_path):
            time.sleep(0.5)
            if stream and time.time() - last > 5:
                self.wfile.write(b": parked\n\n")
                self.wfile.flush()
                last = time.time()
        time.sleep(0.2)
        with open(resp_path) as f:
            answer = json.load(f)
        content = answer.get("content") or ""
        calls = [{"index": i, "id": c.get("id") or f"call_{n}_{i}", "type": "function",
                  "function": {"name": c["name"], "arguments": c["arguments"] if isinstance(c["arguments"], str) else json.dumps(c["arguments"])}}
                 for i, c in enumerate(answer.get("tool_calls") or [])]
        finish = "tool_calls" if calls else "stop"
        usage = {"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2}
        if not stream:
            message = {"role": "assistant", "content": content or None}
            if calls:
                message["tool_calls"] = [{k: v for k, v in c.items() if k != "index"} for c in calls]
            return self.json(200, {"id": f"hand-{n}", "object": "chat.completion", "created": int(time.time()), "model": MODEL,
                                   "choices": [{"index": 0, "message": message, "finish_reason": finish}], "usage": usage})

        def chunk(delta, finish_reason=None, with_usage=False):
            c = {"id": f"hand-{n}", "object": "chat.completion.chunk", "created": int(time.time()), "model": MODEL,
                 "choices": [{"index": 0, "delta": delta, "finish_reason": finish_reason}]}
            if with_usage:
                c["usage"] = usage
            self.wfile.write(b"data: " + json.dumps(c).encode() + b"\n\n")

        chunk({"role": "assistant", "content": content})
        if calls:
            chunk({"tool_calls": calls})
        chunk({}, finish, with_usage=True)
        self.wfile.write(b"data: [DONE]\n\n")
        self.wfile.flush()
        self.close_connection = True


ThreadingHTTPServer(("0.0.0.0", PORT), Handler).serve_forever()
