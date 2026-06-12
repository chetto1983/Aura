FROM golang:1.26.4-alpine AS build

WORKDIR /src
RUN apk add --no-cache ca-certificates git

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/aura ./cmd/aura

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/aura /aura
USER 65532

ENTRYPOINT ["/aura"]
CMD ["serve"]
