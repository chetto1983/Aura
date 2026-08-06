"""Where is the boundary between 'open the document' and 'must RAG it'?"""
import glob, os
import pypdfium2 as pdfium

# ~4 chars/token for Latin text is the usual rule of thumb; deliberately conservative.
CHARS_PER_TOKEN = 3.6
BUDGETS = {"128k": 128_000, "200k": 200_000, "1M": 1_000_000}

print(f"{'documento':34} {'pag':>4} {'caratteri':>10} {'~token':>9}   entra in…")
print("-" * 86)
for p in sorted(glob.glob("/corpus/*.pdf")):
    doc = pdfium.PdfDocument(p)
    try:
        chars = 0
        for i in range(len(doc)):
            page = doc[i]
            tp = page.get_textpage()
            try:
                chars += len(tp.get_text_range())
            finally:
                tp.close()
                page.close()
        n = len(doc)
    finally:
        doc.close()
    tok = chars / CHARS_PER_TOKEN
    fits = [k for k, v in BUDGETS.items() if tok < v * 0.5]  # half the window, room to reason
    print(f"{os.path.basename(p)[:34]:34} {n:4} {chars:10,} {tok:9,.0f}   "
          f"{', '.join(fits) if fits else 'NESSUNO -> serve RAG'}")

print("\nregola: se il documento entra in META' della finestra, aprirlo batte il RAG")
print("        (niente chunking, niente retrieval, nessuna risposta mancata)")
print(f"\nun PDF da 700 pagine a ~2.200 caratteri/pagina = {700*2200:,} caratteri "
      f"= ~{700*2200/CHARS_PER_TOKEN:,.0f} token -> non entra in nessuna finestra")
