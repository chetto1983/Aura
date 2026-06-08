# Aura markitdown sidecar — the official microsoft/markitdown lib behind a thin
# FastAPI /convert endpoint (app.py). Pinned for reproducibility (Aura supply-chain
# discipline). markitdown[all] pulls the document extractors (pdf/docx/pptx/xlsx/…).
FROM python:3.12-slim

# curl for the compose healthcheck; build-essential is NOT needed (wheels only).
RUN apt-get update && apt-get install -y --no-install-recommends curl \
    && rm -rf /var/lib/apt/lists/*

RUN pip install --no-cache-dir \
    "markitdown[all]==0.1.6" \
    "fastapi==0.115.6" \
    "uvicorn[standard]==0.34.0" \
    "python-multipart==0.0.20"

WORKDIR /app
COPY app.py .

EXPOSE 8080
CMD ["uvicorn", "app:app", "--host", "0.0.0.0", "--port", "8080"]
