#!/bin/bash
# PhotoCraft where it already runs: the identity's box has a 2 GiB memory limit
# (internal/config/config_sandbox.go defaultSandboxMemoryLimit). Also the PNG beside aura's 452 MiB peak.
set -u
S=/tmp/vision-spike; B=$S/bin; I=$S/img; O=$S/out
run() {
	local label=$1 mem=$2 ballast=$3 in=$4 out=$5
	res=$(docker run --rm --memory "$mem" --memory-swap "$mem" --cpus 1.0 --shm-size 600m -e HOME=/tmp \
		-v $B:/b:ro -v $I:/img:ro -v $O:/out debian:bookworm-slim sh -c \
		'if [ "$0" -gt 0 ]; then dd if=/dev/zero of=/dev/shm/ballast bs=1M count="$0" 2>/dev/null; fi; exec /b/rss "$@"' \
		"$ballast" /b/photocraft-cli run "/img/$in" --cmd image.imageSize --params '{"width":1024}' --out "/out/$out" --quality 85 --no-gpu 2>&1 | tr '\n' ' ')
	echo "ROW|$label|$in|mem=$mem ballast=${ballast}MiB|$res"
}
for f in p48_baseline p48_progressive p48_444_progressive; do run photocraft-box 2g 0 $f.jpg $f.pcbox.jpg; done
run photocraft-box 2g 0 p48.png p48_png.pcbox.jpg
run photocraft 768m 452 p48.png p48_png.bpc.jpg
