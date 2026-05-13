# aura-init-models — one-shot init container that fetches large model
# artifacts (embedding GGUFs today, OCR weights tomorrow) before any sidecar
# that depends on them starts. See docs/wave-2.10-install-bootstrap.md.
#
# Compose dependency:
#   aura-llama-embed.depends_on.aura-init-models.condition = service_completed_successfully
#
# Pinned to the same Go toolchain as the main Aura Dockerfile to share the
# module cache. Distroless final image keeps the runtime surface minimal —
# this container only does HTTPS GET + SHA-256 + atomic file rename, so no
# shell, no apt, no package manager is needed.
FROM golang:1.26.2-bookworm AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/aura-init-models ./cmd/aura-init-models

# distroless/static-debian12:nonroot ships a minimal libc-free root FS with a
# pinned non-root UID/GID (65532). Includes /etc/ssl/certs/ca-certificates.crt
# so HTTPS to huggingface.co works out of the box.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/aura-init-models /aura-init-models

# /models is the canonical mount point matching compose.yaml's `./data:/models`
# bind. The volume is shared with aura-llama-embed (read-only there,
# read-write here).
WORKDIR /models

ENTRYPOINT ["/aura-init-models"]
