# syntax=docker/dockerfile:1.7
# Build context is the repo root: the server shares internal/arcadedb with Aura,
# so it builds from the same module rather than vendoring a second copy.
#
# The Go binary stays statically linked and cgo-free. spaCy rides alongside it as
# a long-lived worker process on a pipe (docker/arcadedb-mcp/spacy_worker.py),
# NOT through cgo: go-spacy embeds a Python interpreter, which pins the build
# image, the runtime image and libspacy_wrapper.so to a single libpython ABI and
# fails at load rather than at build when they drift. A pipe costs one process and
# no ABI contract.
#
# Both models are installed. The Italian one is not optional: a full-text index
# with the wrong analyser more than halved retrieval recall here (18.6% against
# 43.8% on the same 1536 questions), and entity extraction has the same
# per-language dependency.
FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY cmd/arcadedb-mcp ./cmd/arcadedb-mcp
COPY internal/arcadedb ./internal/arcadedb

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/arcadedb-mcp ./cmd/arcadedb-mcp

FROM python:3.12-slim

ENV AURA_SPACY_WORKER=/opt/arcadedb-mcp/spacy_worker.py \
    PYTHONUNBUFFERED=1

# click is pinned because spaCy's CLI imports names that click 9 removed, and its
# own dependency range does not exclude it: a plain `pip install spacy` installs a
# combination that fails at import with "No module named 'click'".
RUN pip install --no-cache-dir "click<9" spacy \
    && pip install --no-cache-dir \
        https://github.com/explosion/spacy-models/releases/download/en_core_web_sm-3.8.0/en_core_web_sm-3.8.0-py3-none-any.whl \
        https://github.com/explosion/spacy-models/releases/download/it_core_news_sm-3.8.0/it_core_news_sm-3.8.0-py3-none-any.whl \
    && python -c "import spacy; spacy.load('en_core_web_sm'); spacy.load('it_core_news_sm')"

COPY docker/arcadedb-mcp/spacy_worker.py /opt/arcadedb-mcp/spacy_worker.py
COPY --from=build /out/arcadedb-mcp /usr/local/bin/arcadedb-mcp

RUN useradd --uid 65532 --create-home --shell /usr/sbin/nologin nonroot
USER 65532:65532
EXPOSE 8096
ENTRYPOINT ["/usr/local/bin/arcadedb-mcp"]
