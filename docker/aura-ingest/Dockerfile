# The ingestion sidecar: CocoIndex reconciles, iscc-tika extracts, LibreOffice
# normalises what neither of them can open, and aura-filecard describes.
#
# Debian rather than Alpine on purpose: iscc-tika ships a GraalVM native image
# built against glibc, and LibreOffice is packaged here.

# aura-filecard is built from Aura's OWN source rather than reimplemented in Python.
# The card is what makes a spreadsheet answerable -- header detection under a merged
# banner, column types, value distributions -- and that logic is 2,592 lines of Go with
# real edge cases in it. A Python port would be a second implementation to keep in step,
# which is the duplication this rewrite exists to remove.
FROM golang:1.26-bookworm AS filecard
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/aura-filecard ./cmd/aura-filecard
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -o /out/aura-filecard ./cmd/aura-filecard

FROM python:3.12-slim-bookworm

# LibreOffice exists for a narrow, measured reason: internal/documents/filecard
# routes only .xlsx and .xlsm, so an ordinary 2010 .xls -- what Excel and every
# older office still emits -- has no way into Aura at all. `soffice --convert-to`
# preserves what filecard needs: banner rows, the real header below them, and
# numeric cells that stay numeric. SAL_USE_VCLPLUGIN=svp keeps it headless with
# no X server.
# poppler-utils is aura-filecard's dependency, not LibreOffice's: filecard's PDF reader
# shells out to pdftotext and says so, expecting it "installed in the SAME container as
# ingest (docker/aura/Dockerfile)". This sidecar is a NEW container that never inherited
# it, so every PDF card failed with "a PDF's text could not be extracted" -- measured
# 2026-08-08 across the fixture set.
RUN apt-get update && apt-get install -y --no-install-recommends \
      libreoffice-writer libreoffice-calc libreoffice-impress \
      fonts-dejavu-core \
      poppler-utils \
    && rm -rf /var/lib/apt/lists/*
ENV SAL_USE_VCLPLUGIN=svp \
    HOME=/tmp \
    PYTHONUNBUFFERED=1

COPY docker/aura-ingest/requirements.txt /tmp/requirements.txt
RUN pip install --no-cache-dir -r /tmp/requirements.txt

COPY --from=filecard /out/aura-filecard /usr/local/bin/aura-filecard

COPY services/ingest /app/ingest
WORKDIR /app

# The LMDB state MUST resolve to a volume. In an ephemeral path every run starts
# from zero: memoization silently stops working, the corpus is re-extracted and
# re-embedded every pass, and nothing about it looks wrong.
ENV COCOINDEX_DB=/state/coco.db
VOLUME ["/state"]

ENTRYPOINT ["python", "-m", "ingest.app"]
