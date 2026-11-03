# syntax=docker/dockerfile:1.7
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
COPY cmd/aura-cloudflared-supervisor ./cmd/aura-cloudflared-supervisor
COPY internal/cloudflaresupervisor ./internal/cloudflaresupervisor
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/aura-cloudflared-supervisor ./cmd/aura-cloudflared-supervisor

# Official distroless runtime includes CA certificates; digest is the amd64/arm64 index.
FROM cloudflare/cloudflared:2026.10.7@sha256:4955524aeaa379e154023d5a027c250f7b35041a2e7e950cf13f144bc9b9c5ba
COPY --from=build /out/aura-cloudflared-supervisor /usr/local/bin/aura-cloudflared-supervisor
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/aura-cloudflared-supervisor"]
CMD []
HEALTHCHECK --interval=15s --timeout=5s --retries=3 CMD ["/usr/local/bin/aura-cloudflared-supervisor", "healthcheck", "http://127.0.0.1:8085/healthz"]
