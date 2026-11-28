#!/bin/bash
# Build the jpegli (wazero) probe in its own module under /tmp, as 20_build.sh does for jpegn.
export PATH="$HOME/.local/bin:$HOME/go/bin:/usr/local/go/bin:$PATH"
set -eu
S=/tmp/vision-spike; B=$S/bin; SP=/mnt/d/Aura/spikes/vision-large-photo-downscale
J=$S/jpegliprobe; rm -rf "$J"; mkdir -p "$J"; cp $SP/jpegliprobe/main.go "$J/"; cd "$J"
go mod init vision-spike/jpegliprobe
go get github.com/gen2brain/jpegli@v0.4.2 golang.org/x/image@v0.48.0
CGO_ENABLED=0 go build -o $B/jpegliprobe main.go
grep -E "jpegli|wazero|x/image" go.mod; ls -la $B/jpegliprobe
