#!/usr/bin/env python3
"""Stdlib unittest for the 2b session-bound sidecar routes (D-01/D-02/D-03).

Runs the real Handler against a loopback ThreadingHTTPServer and drives it with
urllib (all stdlib — the sidecar's stdlib-only invariant extends to its tests).
Covers: python session persistence (x=42 survives across two same-session calls),
shell cwd re-application, an exception in python exec captured (server stays up),
and an unknown path -> 404. The stateless /exec/python path is also smoke-checked
so the 2b router does not regress 2a.

Run: python -m unittest sandbox.sidecar_test   (or)   python sandbox/sidecar_test.py
"""

import json
import os
import sys
import threading
import unittest
import urllib.error
import urllib.request
from http.server import ThreadingHTTPServer

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import sidecar  # noqa: E402 — path injected above so the test runs from any cwd


class SessionSidecarTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        cls.server = ThreadingHTTPServer(("127.0.0.1", 0), sidecar.Handler)
        cls.base = f"http://127.0.0.1:{cls.server.server_address[1]}"
        cls.thread = threading.Thread(target=cls.server.serve_forever, daemon=True)
        cls.thread.start()

    @classmethod
    def tearDownClass(cls) -> None:
        cls.server.shutdown()
        cls.server.server_close()
        cls.thread.join(timeout=5)

    def setUp(self) -> None:
        sidecar.SESSIONS.clear()

    def _post(self, path: str, code: str, timeout_sec: int = 30) -> tuple[int, dict]:
        body = json.dumps({"code": code, "timeout_sec": timeout_sec}).encode("utf-8")
        req = urllib.request.Request(
            self.base + path, data=body, headers={"Content-Type": "application/json"}, method="POST"
        )
        try:
            with urllib.request.urlopen(req, timeout=10) as resp:
                return resp.status, json.loads(resp.read())
        except urllib.error.HTTPError as exc:
            return exc.code, json.loads(exc.read())

    def test_python_namespace_persists_across_calls(self) -> None:
        status, res = self._post("/session/s1/exec/python", "x = 42")
        self.assertEqual(status, 200)
        self.assertEqual(res["exit_code"], 0)

        status, res = self._post("/session/s1/exec/python", "print(x)")
        self.assertEqual(status, 200)
        self.assertEqual(res["exit_code"], 0)
        self.assertEqual(res["stdout"].strip(), "42", "x=42 must survive into the second same-session call")

    def test_python_sessions_are_isolated(self) -> None:
        self._post("/session/a/exec/python", "y = 1")
        _, res = self._post("/session/b/exec/python", "print('y' in dir())")
        self.assertEqual(res["stdout"].strip(), "False", "session b must not see session a's namespace")

    def test_shell_reapplies_session_cwd(self) -> None:
        # Pin the session cwd to a dir that exists on any host (the test dir itself),
        # so the assertion is OS-portable. A `cd ..` inside the snippet must NOT
        # persist — the next call re-applies the tracked cwd (D-02 asymmetric).
        cwd = os.path.dirname(os.path.abspath(__file__))
        sidecar.SESSIONS["sh1"] = {"ns": {}, "cwd": cwd}

        _, first = self._post("/session/sh1/exec/shell", "pwd")
        self.assertEqual(first["exit_code"], 0)
        baseline = first["stdout"].strip()

        # cd away inside the call; it must not leak into the tracked cwd.
        self._post("/session/sh1/exec/shell", "cd .. && pwd")

        _, again = self._post("/session/sh1/exec/shell", "pwd")
        self.assertEqual(
            again["stdout"].strip(), baseline, "shell cd must NOT persist; the tracked cwd is re-applied each call"
        )

    def test_python_exception_is_captured_not_crashing(self) -> None:
        status, res = self._post("/session/err/exec/python", "raise ValueError('boom')")
        self.assertEqual(status, 200)
        self.assertEqual(res["exit_code"], 1)
        self.assertIn("ValueError", res["stderr"])
        self.assertIn("boom", res["stderr"])

        # The server must still serve the next request (one bad call != dead session).
        status, res = self._post("/session/err/exec/python", "print('alive')")
        self.assertEqual(status, 200)
        self.assertEqual(res["stdout"].strip(), "alive")

    def test_unknown_path_404(self) -> None:
        status, res = self._post("/session/s1/exec/ruby", "puts 1")
        self.assertEqual(status, 404)
        self.assertIn("error", res)

        status, _ = self._post("/session//exec/python", "x = 1")
        self.assertEqual(status, 404, "an empty session id must 404")

    def test_stateless_path_unchanged(self) -> None:
        status, res = self._post("/exec/python", "print(1 + 1)")
        self.assertEqual(status, 200)
        self.assertEqual(res["stdout"].strip(), "2")
        # A stateless call must NOT register a session.
        self.assertEqual(len(sidecar.SESSIONS), 0)


if __name__ == "__main__":
    unittest.main()
