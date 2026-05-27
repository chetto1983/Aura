#!/usr/bin/env python3
"""Direct test of Gemma 4 31B IT multimodal capabilities via OpenRouter.

Tests:
  1. PDF input — extract structured fields from a real Italian commercial document
  2. YouTube URL — describe the video

No new deps. Standard library only.
"""
import base64
import json
import os
import sqlite3
import sys
import time
import urllib.request

DB_PATH = r"D:\Aura\data\aura.db"
PDF_PATH = r"C:\Users\Davide\Desktop\6MBU00242200.pdf"
YOUTUBE_URL = "https://youtube.com/shorts/rCylR-VvAzQ?si=EwYQOiJPmSsEgstA"
MODEL = "google/gemma-4-31b-it"
API_URL = "https://openrouter.ai/api/v1/chat/completions"


def load_key():
    con = sqlite3.connect(DB_PATH)
    try:
        row = con.execute("SELECT value FROM secrets WHERE key='llm_api_key'").fetchone()
        return row[0] if row else ""
    finally:
        con.close()


def post(body, timeout=120):
    req = urllib.request.Request(
        API_URL,
        data=json.dumps(body).encode("utf-8"),
        headers={
            "Authorization": f"Bearer {load_key()}",
            "Content-Type": "application/json",
            "HTTP-Referer": "https://aura.local",
            "X-Title": "Aura Gemma multimodal smoke",
        },
        method="POST",
    )
    t0 = time.time()
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            body = resp.read().decode("utf-8")
            return resp.status, body, time.time() - t0
    except urllib.error.HTTPError as e:
        body = e.read().decode("utf-8", errors="replace")
        return e.code, body, time.time() - t0


def test_pdf():
    print("=" * 60)
    print("TEST 1: PDF input via Gemma 4 31B IT")
    print("=" * 60)
    with open(PDF_PATH, "rb") as f:
        pdf_b64 = base64.b64encode(f.read()).decode("ascii")
    size_kb = len(pdf_b64) * 3 // 4 // 1024
    print(f"PDF size: ~{size_kb} KB")

    body = {
        "model": MODEL,
        "messages": [
            {
                "role": "user",
                "content": [
                    # Ordering: file BEFORE text per Google multimodal directive
                    {
                        "type": "file",
                        "file": {
                            "filename": "test.pdf",
                            "file_data": f"data:application/pdf;base64,{pdf_b64}",
                        },
                    },
                    {
                        "type": "text",
                        "text": (
                            "Estrai TUTTI i campi tecnici da questa scheda prodotto Finder in formato JSON. "
                            "Rispondi SOLO con il JSON, senza preamboli:\n"
                            "{\n"
                            '  "codice_prodotto": "...",\n'
                            '  "categoria": "...",\n'
                            '  "descrizione_commerciale": "testo integrale",\n'
                            '  "dimensioni_mm": "L x A x P",\n'
                            '  "protocollo_comunicazione": "...",\n'
                            '  "temperatura_ambiente_min_C": numero,\n'
                            '  "temperatura_ambiente_max_C": numero,\n'
                            '  "tensione_alimentazione_V": numero,\n'
                            '  "tipo_alimentazione": "AC/DC o solo AC o solo DC",\n'
                            '  "porta_ethernet_velocita": "...",\n'
                            '  "porta_modbus_rtu_baud_max": numero,\n'
                            '  "max_client_ethernet": numero,\n'
                            '  "max_slave_modbus": numero,\n'
                            '  "isolamento_V": numero,\n'
                            '  "numero_led_interfaccia": numero,\n'
                            '  "omologazioni_visibili": ["CE", "UKCA", ...],\n'
                            '  "numero_verde": "...",\n'
                            '  "data_creazione_documento": "...",\n'
                            '  "anno_copyright": numero\n'
                            "}"
                        ),
                    },
                ],
            }
        ],
        "max_tokens": 400,
        "temperature": 0,
    }
    status, raw, elapsed = post(body)
    print(f"HTTP {status} in {elapsed:.2f}s")
    try:
        d = json.loads(raw)
        if "error" in d:
            print(f"ERROR: {d['error']}")
        else:
            content = d.get("choices", [{}])[0].get("message", {}).get("content", "")
            usage = d.get("usage", {})
            print(f"\n--- Gemma response ---\n{content}\n")
            print(f"--- usage ---\n{json.dumps(usage, indent=2)}")
    except Exception as e:
        print(f"parse error: {e}")
        print(raw[:1500])


def test_youtube():
    print("\n" + "=" * 60)
    print("TEST 2: YouTube video via Gemma 4 31B IT")
    print("=" * 60)
    body = {
        "model": MODEL,
        "messages": [
            {
                "role": "user",
                "content": [
                    # Ordering: video BEFORE text per Google multimodal directive
                    {
                        "type": "video_url",
                        "video_url": {"url": YOUTUBE_URL},
                    },
                    {
                        "type": "text",
                        "text": (
                            "Descrivi questo video YouTube in 5-8 frasi in italiano. "
                            "Includi: soggetto principale, ambientazione, eventuali testi a schermo, "
                            "intento percepito del creatore."
                        ),
                    },
                ],
            }
        ],
        "max_tokens": 500,
        "temperature": 0,
    }
    status, raw, elapsed = post(body)
    print(f"HTTP {status} in {elapsed:.2f}s")
    try:
        d = json.loads(raw)
        if "error" in d:
            print(f"ERROR: {d['error']}")
        else:
            content = d.get("choices", [{}])[0].get("message", {}).get("content", "")
            usage = d.get("usage", {})
            print(f"\n--- Gemma response ---\n{content}\n")
            print(f"--- usage ---\n{json.dumps(usage, indent=2)}")
    except Exception as e:
        print(f"parse error: {e}")
        print(raw[:1500])


if __name__ == "__main__":
    test_pdf()
    test_youtube()
