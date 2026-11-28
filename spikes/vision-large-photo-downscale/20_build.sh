#!/bin/bash
# Build the static harnesses into /tmp/vision-spike/bin, extract the pinned photocraft-cli,
# and build the throwaway djpeg image (Debian bookworm's libjpeg-turbo-progs, the base of aura).
export PATH="$HOME/.local/bin:$HOME/go/bin:/usr/local/go/bin:$PATH"
set -eu
S=/tmp/vision-spike; B=$S/bin; mkdir -p $B
SP=/mnt/d/Aura/spikes/vision-large-photo-downscale
cd /mnt/d/Aura
CGO_ENABLED=0 go build -o $B/rss $SP/rss.go
CGO_ENABLED=0 go build -o $B/baseline $SP/baseline.go
# jpegn is not in the repo's go.mod: build a copy of the probe in its own module under /tmp.
J=$S/jpegnprobe; rm -rf "$J"; mkdir -p "$J"; cp $SP/jpegnprobe/main.go "$J/"; cd "$J"
go mod init vision-spike/jpegnprobe
go get github.com/gen2brain/jpegn@v0.7.1 golang.org/x/image@v0.48.0
CGO_ENABLED=0 go build -o $B/jpegnprobe main.go
grep -E "jpegn|x/image" go.mod
cid=$(docker create ghcr.io/chetto1983/aura-sandbox:edge)
docker cp "$cid:/usr/local/bin/photocraft-cli" $B/photocraft-cli
docker rm "$cid" >/dev/null
ls -la $B; sha256sum $B/photocraft-cli
echo "glibc symbol versions photocraft-cli needs (highest 3):"; objdump -T $B/photocraft-cli | grep -oE "GLIBC_[0-9.]+" | sort -uV | tail -3
docker build -q -t vision-spike-djpeg:local - <<'DOCKERFILE'
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends libjpeg-turbo-progs && rm -rf /var/lib/apt/lists/*
DOCKERFILE
docker run --rm vision-spike-djpeg:local sh -c 'dpkg -l libjpeg-turbo-progs libjpeg62-turbo | tail -2; ldd --version | head -1; ls -la /usr/bin/djpeg; djpeg -help 2>&1 | head -40'
