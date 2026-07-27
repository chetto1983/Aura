#!/usr/bin/env bash
# scripts/workspace_toolchain_smoke.sh — assert the pre-baked document/data toolchain
# resolves with NO runtime install. The script is container-agnostic; the repo scripts/ dir
# is NOT bind-mounted, so run it by piping over stdin at whichever container actually runs
# the model's shell under the ACTIVE profile:
#   non-strict (dev/local_trusted) — shell_exec runs in the daemon container:
#     docker exec -i aura bash -s < scripts/workspace_toolchain_smoke.sh
#   strict (single_user_hardened/server_production) — shell_exec is routed into the box, so
#   the daemon container passing here proves NOTHING about what the model can reach:
#     docker exec -i "$(docker ps --format '{{.Names}}' | grep '^aura-box-' | head -1)" \
#       bash -s < scripts/workspace_toolchain_smoke.sh
# Covers Amendment #88 (base: docx/python-docx/openpyxl/pandas/pandoc/file/xxd) and
# Amendment #88.1 (pptx/xlsx/pdf skills: LibreOffice headless + poppler/qpdf/pdftk/
# tesseract + the pptx/pdf/excel pip+npm libs).
set -euo pipefail

# Node globals (NODE_PATH must be inherited from the image ENV).
node -e "require('docx'); require('pptxgenjs'); console.log('node docx/pptxgenjs ok')"

# Python stack for the docx/pptx/xlsx/pdf skills.
python3 - <<'PY'
import docx            # python-docx
import pptx            # python-pptx
import openpyxl
import xlsxwriter
import pandas
import pypdf
import pdfplumber
import reportlab
import pdf2image
import pytesseract
import PIL              # Pillow
import defusedxml
import lxml
import markitdown
print("py docx/pptx/openpyxl/xlsxwriter/pandas/pypdf/pdfplumber/reportlab/pdf2image/pytesseract/PIL/defusedxml/lxml/markitdown ok")
PY

# System binaries: base (pandoc/file/xxd) + office/pdf (soffice + poppler + qpdf +
# pdftk + tesseract).
for t in pandoc file xxd soffice pdftoppm pdftotext qpdf pdftk tesseract; do
    command -v "$t" >/dev/null || { echo "MISSING $t" >&2; exit 1; }
done

# soffice must actually run headless (the docx/pptx/xlsx skills invoke it).
soffice --headless --version >/dev/null

echo "toolchain smoke: ok"
