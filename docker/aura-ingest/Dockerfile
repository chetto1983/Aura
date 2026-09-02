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
# proxy.golang.org drops a stream mid-fetch often enough to redden a whole CI run on a
# build that has nothing wrong with it: measured 2026-09-01 on Actions run 33552817871,
# `stream error: stream ID 323; INTERNAL_ERROR; received from peer` on one module .mod
# file, which killed the Agent Memory job before a single test body ran. Three tries with
# a widening pause: a transport blip is ridden out, a module that genuinely is not there
# still stops the build on the third. Every branch is `if`-guarded because under `set -e`
# a bare `[ ... ] && ...` whose test is false exits the shell with the test's status.
RUN set -e; \
    for attempt in 1 2 3; do \
      if go mod download; then break; fi; \
      if [ "$attempt" = 3 ]; then echo "go mod download failed after 3 attempts" >&2; exit 1; fi; \
      echo "go mod download failed (attempt $attempt); retrying" >&2; \
      sleep $((attempt * 5)); \
    done
COPY cmd/aura-filecard ./cmd/aura-filecard
COPY cmd/aura-media-index ./cmd/aura-media-index
COPY cmd/aura-ingest-supervisor ./cmd/aura-ingest-supervisor
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -o /out/aura-filecard ./cmd/aura-filecard
RUN CGO_ENABLED=0 go build -trimpath -o /out/aura-media-index ./cmd/aura-media-index
RUN CGO_ENABLED=0 go build -trimpath -o /out/aura-ingest-supervisor ./cmd/aura-ingest-supervisor

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
COPY --from=filecard /out/aura-media-index /usr/local/bin/aura-media-index
COPY --from=filecard /out/aura-ingest-supervisor /usr/local/bin/aura-ingest-supervisor

COPY services/ingest /app/ingest
WORKDIR /app

# The supervisor overrides this with /state/<identity>/coco.db for every child. The
# fallback still resolves to the volume: an ephemeral path silently disables memoization.
ENV COCOINDEX_DB=/state/coco.db
VOLUME ["/state"]

ENTRYPOINT ["aura-ingest-supervisor"]
