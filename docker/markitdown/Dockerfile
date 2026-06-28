# Aura markitdown sidecar — the official microsoft/markitdown lib behind a thin
# FastAPI /convert endpoint (app.py). Pinned for reproducibility (Aura supply-chain
# discipline). markitdown[all] pulls the document extractors (pdf/docx/pptx/xlsx/…).
FROM python:3.12-slim

# curl for the compose healthcheck; build-essential is NOT needed (wheels only).
RUN apt-get update && apt-get install -y --no-install-recommends curl \
    && rm -rf /var/lib/apt/lists/*

# markitdown[all] already pulls these transitively, but the /extract handlers
# import them directly (PyMuPDF/openpyxl/python-docx/python-pptx), so they are
# pinned explicitly for supply-chain reproducibility. HTML extraction reuses the
# markitdown converter and CSV uses the stdlib, so they add no new dependency.
RUN pip install --no-cache-dir \
    "markitdown[all]==0.1.6" \
    "markitdown-ocr==0.1.0" \
    "openai==1.59.6" \
    "fastapi==0.115.6" \
    "uvicorn[standard]==0.34.0" \
    "python-multipart==0.0.20" \
    "PyMuPDF==1.24.14" \
    "openpyxl==3.1.5" \
    "python-docx==1.1.2" \
    "python-pptx==1.0.2"

WORKDIR /app
COPY app.py .

EXPOSE 8080
CMD ["uvicorn", "app:app", "--host", "0.0.0.0", "--port", "8080"]
