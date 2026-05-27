#!/usr/bin/env python3
"""Test PDF→PNG rasterize→Gemma vision path (the real markitdown image-converter pattern).

This is the actual path Aura would use post-migration: PDF rasterized page-by-page,
each page sent as image_url to Gemma. Tests whether the rasterized visual contains
the correct field values (vs the corrupted text layer the embedded extractor would see).
"""
import base64
import io
import json
import sqlite3
import sys
import time
import urllib.request

# Force UTF-8 stdout on Windows
try:
    sys.stdout.reconfigure(encoding="utf-8")
except Exception:
    pass

import fitz  # PyMuPDF

DB_PATH = r"D:\Aura\data\aura.db"
PDF_PATH = r"C:\Users\Davide\Desktop\6MBU00242200.pdf"
MODEL = "google/gemma-4-31b-it"
API_URL = "https://openrouter.ai/api/v1/chat/completions"
DPI = 200  # 200dpi balance quality vs size


def load_key():
    con = sqlite3.connect(DB_PATH)
    try:
        row = con.execute("SELECT value FROM secrets WHERE key='llm_api_key'").fetchone()
        return row[0] if row else ""
    finally:
        con.close()


def pdf_pages_to_png(path):
    """Rasterize each page to PNG bytes."""
    doc = fitz.open(path)
    pages = []
    for i, page in enumerate(doc):
        mat = fitz.Matrix(DPI / 72, DPI / 72)
        pix = page.get_pixmap(matrix=mat)
        pages.append(pix.tobytes("png"))
    doc.close()
    return pages


def post(body, timeout=180):
    req = urllib.request.Request(
        API_URL,
        data=json.dumps(body).encode("utf-8"),
        headers={
            "Authorization": f"Bearer {load_key()}",
            "Content-Type": "application/json",
            "HTTP-Referer": "https://aura.local",
            "X-Title": "Aura Gemma PDF rasterize smoke",
        },
        method="POST",
    )
    t0 = time.time()
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return resp.status, resp.read().decode("utf-8"), time.time() - t0
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode("utf-8", errors="replace"), time.time() - t0


def main():
    print("=" * 60)
    print("PDF rasterize → PNG → Gemma vision (image_url path)")
    print("=" * 60)
    pages = pdf_pages_to_png(PDF_PATH)
    print(f"Rasterized {len(pages)} page(s) at {DPI} dpi, sizes: {[len(p)//1024 for p in pages]} KB")

    # Build multimodal content: image(s) FIRST, text LAST (Google directive)
    content = []
    for i, png_bytes in enumerate(pages):
        b64 = base64.b64encode(png_bytes).decode("ascii")
        content.append({
            "type": "image_url",
            "image_url": {"url": f"data:image/png;base64,{b64}"},
        })
    content.append({
        "type": "text",
        "text": (
            "Estrai TUTTI i campi tecnici da questa scheda prodotto Finder in formato JSON. "
            "Rispondi SOLO con il JSON, senza preamboli, senza markdown fences:\n"
            "{\n"
            '  "codice_prodotto": "...",\n'
            '  "categoria": "...",\n'
            '  "descrizione_commerciale": "testo integrale 1-2 frasi",\n'
            '  "dimensioni_mm": "L x A x P",\n'
            '  "protocollo_comunicazione": "...",\n'
            '  "temperatura_ambiente_min_C": numero_int,\n'
            '  "temperatura_ambiente_max_C": numero_int,\n'
            '  "tensione_alimentazione_V": numero_int,\n'
            '  "tipo_alimentazione": "AC/DC o AC o DC",\n'
            '  "porta_ethernet_velocita": "...",\n'
            '  "porta_modbus_rtu_baud_max": numero,\n'
            '  "max_client_ethernet": numero,\n'
            '  "max_slave_modbus": numero,\n'
            '  "isolamento_V": numero,\n'
            '  "numero_led_interfaccia": numero,\n'
            '  "omologazioni_visibili": ["lista loghi a piè pagina"],\n'
            '  "numero_verde": "...",\n'
            '  "data_creazione_documento": "GG/MM/AAAA",\n'
            '  "anno_copyright": numero\n'
            "}"
        ),
    })

    body = {
        "model": MODEL,
        "messages": [{"role": "user", "content": content}],
        "max_tokens": 500,
        "temperature": 0,
    }
    status, raw, elapsed = post(body)
    print(f"\nHTTP {status} in {elapsed:.2f}s")
    try:
        d = json.loads(raw)
        if "error" in d:
            print(f"ERROR: {d['error']}")
            print("Body:", raw[:1000])
            return
        msg = d.get("choices", [{}])[0].get("message", {}).get("content", "")
        usage = d.get("usage", {})
        print(f"\n--- Gemma response ---\n{msg}\n")
        print(f"--- usage ---\n{json.dumps(usage, indent=2)}")
    except Exception as e:
        print(f"parse error: {e}")
        print(raw[:1500])


if __name__ == "__main__":
    main()
