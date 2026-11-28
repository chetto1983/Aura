#!/bin/bash
# jpegli (WASM on wazero, ScaleTarget = the 1024 px target) alone under 768 MiB, then beside the
# 452 MiB ballast as 31_followup.sh does. REPS runs.
set -u
S=/tmp/vision-spike; B=$S/bin; I=$S/img; O=$S/out
REPS=${REPS:-2}
run() {
	local label=$1 img=$2 rep=$3 ballast=$4; shift 4
	res=$(docker run --rm --memory 768m --memory-swap 768m --cpus 1.0 --shm-size 600m -e HOME=/tmp \
		-v $B:/b:ro -v $I:/img:ro -v $O:/out debian:bookworm-slim sh -c \
		'if [ "$0" -gt 0 ]; then dd if=/dev/zero of=/dev/shm/ballast bs=1M count="$0" 2>/dev/null; fi; exec /b/rss "$@"' \
		"$ballast" "$@" 2>&1 | tr '\n' ' ')
	echo "ROW|$label|$img|$rep|ballast=${ballast}MiB|$res"
}
for rep in $(seq 1 "$REPS"); do
	for f in p12_control p24_progressive p48_baseline p48_progressive p48_444_progressive; do
		run jpegli "$f" "$rep" 0 /b/jpegliprobe /img/$f.jpg /out/$f.jpegli.jpg
	done
	for f in p48_baseline p48_progressive p48_444_progressive; do
		run jpegli "$f" "$rep" 452 /b/jpegliprobe /img/$f.jpg /out/$f.bjpegli.jpg
	done
done
