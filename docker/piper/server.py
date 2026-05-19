#!/usr/bin/env python3
"""Thin HTTP server wrapping piper-tts for Italian TTS synthesis.

POST /v1/synthesize  JSON {"text": "...", "voice": "..."}  →  audio/wav
GET  /health                                               →  200 once loaded

Raw PCM from piper is framed into a WAV container via stdlib `wave`.
Headers X-Duration-S and X-Elapsed-Ms carry synthesis telemetry.
"""
import http.server
import io
import json
import logging
import threading
import time
import wave

logging.basicConfig(level=logging.INFO, format="%(message)s")
log = logging.getLogger(__name__)

VOICE_PATH = "/voices/it_IT-paola-medium.onnx"
PORT = 8083

_voice = None
_voice_lock = threading.Lock()
_voice_ready = threading.Event()


def _load_voice() -> None:
    global _voice
    from piper.voice import PiperVoice  # deferred import — large ONNX init
    t0 = time.monotonic()
    _voice = PiperVoice.load(VOICE_PATH)
    elapsed_ms = int((time.monotonic() - t0) * 1000)
    log.info(
        "piper: voice loaded in %d ms  sample_rate=%d",
        elapsed_ms,
        _voice.config.sample_rate,
    )
    _voice_ready.set()


class _Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):  # suppress default per-request log
        pass

    # ── GET /health ──────────────────────────────────────────────────────────
    def do_GET(self) -> None:
        if self.path != "/health":
            self.send_error(404)
            return
        if _voice_ready.is_set():
            body = b'{"ok":true}'
            code = 200
        else:
            body = b'{"ok":false,"status":"loading"}'
            code = 503
        self._write_json(code, body)

    # ── POST /v1/synthesize ──────────────────────────────────────────────────
    def do_POST(self) -> None:
        if self.path != "/v1/synthesize":
            self.send_error(404)
            return
        if not _voice_ready.is_set():
            self.send_error(503, "Voice not ready")
            return

        length = int(self.headers.get("Content-Length", 0))
        raw = self.rfile.read(length)
        try:
            req = json.loads(raw)
        except Exception:
            self.send_error(400, "Invalid JSON")
            return

        text = str(req.get("text", "")).strip()
        if not text:
            self.send_error(400, "Missing text")
            return

        # Serialize concurrent synthesis through a lock — onnxruntime session
        # is not guaranteed thread-safe for concurrent inference on the same
        # session object. synthesize_wav() (piper-tts ≥1.4) auto-sets WAV
        # format (channels/sampwidth/framerate) from the voice config.
        buf = io.BytesIO()
        t0 = time.monotonic()
        with _voice_lock, wave.open(buf, "wb") as wf:
            _voice.synthesize_wav(text, wf)
        elapsed_ms = int((time.monotonic() - t0) * 1000)
        wav_bytes = buf.getvalue()

        # Re-open the framed WAV to read duration without trusting the
        # raw-PCM-length heuristic (sampwidth may not be 2 on future voices).
        with wave.open(io.BytesIO(wav_bytes), "rb") as wf:
            duration_s = wf.getnframes() / float(wf.getframerate())

        self.send_response(200)
        self.send_header("Content-Type", "audio/wav")
        self.send_header("Content-Length", str(len(wav_bytes)))
        self.send_header("X-Duration-S", f"{duration_s:.2f}")
        self.send_header("X-Elapsed-Ms", str(elapsed_ms))
        self.end_headers()
        self.wfile.write(wav_bytes)

    # ── helpers ──────────────────────────────────────────────────────────────
    def _write_json(self, code: int, body: bytes) -> None:
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


if __name__ == "__main__":
    threading.Thread(target=_load_voice, daemon=True).start()
    server = http.server.HTTPServer(("0.0.0.0", PORT), _Handler)
    log.info("piper-server: listening on 0.0.0.0:%d", PORT)
    server.serve_forever()
