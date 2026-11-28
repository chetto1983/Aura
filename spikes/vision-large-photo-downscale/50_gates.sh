#!/bin/bash
# The spike's Go files stay out of the module's gates: //go:build ignore, built by file name.
export PATH="$HOME/.local/bin:$HOME/go/bin:/usr/local/go/bin:$PATH"
cd /mnt/d/Aura || exit 1
echo "go_packages.sh lines naming the spike: $(bash scripts/go_packages.sh | grep -c vision-large-photo || true)"
echo "gofmt -l (empty = clean):"; "$(go env GOROOT)/bin/gofmt" -l spikes/vision-large-photo-downscale; echo "gofmt done"
go vet spikes/vision-large-photo-downscale/baseline.go && go vet spikes/vision-large-photo-downscale/rss.go && echo "vet ok"
