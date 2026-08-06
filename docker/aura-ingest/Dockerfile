# The ingestion sidecar: CocoIndex reconciles, iscc-tika extracts, LibreOffice
# normalises what neither of them can open.
#
# Debian rather than Alpine on purpose: iscc-tika ships a GraalVM native image
# built against glibc, and LibreOffice is packaged here.
FROM python:3.12-slim-bookworm

# LibreOffice exists for a narrow, measured reason: internal/documents/filecard
# routes only .xlsx and .xlsm, so an ordinary 2010 .xls -- what Excel and every
# older office still emits -- has no way into Aura at all. `soffice --convert-to`
# preserves what filecard needs: banner rows, the real header below them, and
# numeric cells that stay numeric. SAL_USE_VCLPLUGIN=svp keeps it headless with
# no X server.
RUN apt-get update && apt-get install -y --no-install-recommends \
      libreoffice-writer libreoffice-calc libreoffice-impress \
      fonts-dejavu-core \
    && rm -rf /var/lib/apt/lists/*
ENV SAL_USE_VCLPLUGIN=svp \
    HOME=/tmp \
    PYTHONUNBUFFERED=1

COPY docker/aura-ingest/requirements.txt /tmp/requirements.txt
RUN pip install --no-cache-dir -r /tmp/requirements.txt

COPY services/ingest /app/ingest
WORKDIR /app

# The LMDB state MUST resolve to a volume. In an ephemeral path every run starts
# from zero: memoization silently stops working, the corpus is re-extracted and
# re-embedded every pass, and nothing about it looks wrong.
ENV COCOINDEX_DB=/state/coco.db
VOLUME ["/state"]

ENTRYPOINT ["python", "-m", "ingest.app"]
