#!/usr/bin/env python3
"""Spike 019 fixture generator — real artifact files of each type Aura must deliver.

Writes to D:/tmp/spike-019-fixtures/ (volatile scratch per conventions).
xlsx via openpyxl (host has 3.1.5); pdf + docx handcrafted minimal-valid (no extra deps).
"""
import os
import sys
import time
import zipfile

OUT = sys.argv[1] if len(sys.argv) > 1 else "D:/tmp/spike-019-fixtures"
TAG = f"AURA-SPIKE-019-{int(time.time())}"
os.makedirs(OUT, exist_ok=True)

# --- xlsx (real, openpyxl) ---------------------------------------------------
import openpyxl

wb = openpyxl.Workbook()
ws = wb.active
ws.title = "Report"
ws.append(["Fase", "Nome", "Coverage", "Stato"])
for row in [[11, "Skills", 0.917, "Done"], [12, "AG-UI", None, "Next"], [13, "Telegram", None, "Spiking"]]:
    ws.append(row)
ws["F1"] = TAG
wb.save(f"{OUT}/report.xlsx")

# --- pdf (minimal valid, handcrafted) -----------------------------------------
content = f"BT /F1 18 Tf 60 700 Td ({TAG}) Tj 0 -30 Td (Artifact delivery probe - PDF) Tj ET"
objs = [
    "<< /Type /Catalog /Pages 2 0 R >>",
    "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
    "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
    f"<< /Length {len(content)} >>\nstream\n{content}\nendstream",
    "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
]
pdf = b"%PDF-1.4\n"
offsets = []
for i, o in enumerate(objs, 1):
    offsets.append(len(pdf))
    pdf += f"{i} 0 obj\n{o}\nendobj\n".encode()
xref = len(pdf)
pdf += f"xref\n0 {len(objs)+1}\n0000000000 65535 f \n".encode()
for off in offsets:
    pdf += f"{off:010d} 00000 n \n".encode()
pdf += f"trailer\n<< /Size {len(objs)+1} /Root 1 0 R >>\nstartxref\n{xref}\n%%EOF\n".encode()
with open(f"{OUT}/report.pdf", "wb") as f:
    f.write(pdf)

# --- docx (minimal valid OOXML zip, handcrafted) -------------------------------
DOC = f"""<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:body><w:p><w:r><w:t>{TAG}</w:t></w:r></w:p>
<w:p><w:r><w:t>Artifact delivery probe - DOCX</w:t></w:r></w:p></w:body></w:document>"""
CT = """<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
</Types>"""
RELS = """<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>"""
with zipfile.ZipFile(f"{OUT}/report.docx", "w", zipfile.ZIP_DEFLATED) as z:
    z.writestr("[Content_Types].xml", CT)
    z.writestr("_rels/.rels", RELS)
    z.writestr("word/document.xml", DOC)

# --- csv ------------------------------------------------------------------------
with open(f"{OUT}/report.csv", "w", newline="") as f:
    f.write(f"tag,{TAG}\nfase,nome,stato\n11,Skills,Done\n13,Telegram,Spiking\n")

print(TAG)
for name in ["report.xlsx", "report.pdf", "report.docx", "report.csv"]:
    print(f"{name}: {os.path.getsize(f'{OUT}/{name}')} bytes")
