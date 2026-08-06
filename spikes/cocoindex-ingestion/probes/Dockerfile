FROM python:3.13-slim-bookworm

ENV DEBIAN_FRONTEND=noninteractive \
    EXIFTOOL_PATH=/usr/bin/exiftool \
    FFMPEG_PATH=/usr/bin/ffmpeg \
    PYTHONUNBUFFERED=1

RUN apt-get update && apt-get install -y --no-install-recommends \
        exiftool ffmpeg \
    && rm -rf /var/lib/apt/lists/*

# cocoindex        — the incremental ETL engine (Rust core, Python DSL)
# markitdown[all]  — fast no-ML document -> markdown
# markitdown-ocr   — per-page VLM fallback for pages with no text layer (GLM-OCR)
# neo4j            — official driver; ArcadeDB's Bolt plugin is certified against 5.x/6.x
RUN pip install --no-cache-dir \
        cocoindex \
        "markitdown[all]" markitdown-ocr openai \
        neo4j \
        litellm instructor pydantic \
        asyncpg pypdfium2 \
        aiobotocore

WORKDIR /work
