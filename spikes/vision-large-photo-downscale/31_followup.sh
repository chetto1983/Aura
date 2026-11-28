#!/bin/bash
# Follow-ups to 30_measure.sh:
#  (a) PhotoCraft refused every 48 MP JPEG under 768 MiB with its own admission check
#      (crates/io/src/memory.rs, which reads the free memory of its cgroup): try
#      --approve-high-memory, and measure its true demand with no limit (4 GiB).
#  (b) A child process exec'd by aura shares aura's cgroup. Hold a ballast of 452 MiB in /dev/shm
#      (shmem is charged to the container's memory cgroup) — the VM .158 peak the file's comments
#      record — and run each candidate in the 316 MiB left.
export PATH="$HOME/.local/bin:$HOME/go/bin:/usr/local/go/bin:$PATH"
set -u
S=/tmp/vision-spike; B=$S/bin; I=$S/img; O=$S/out
REPS=${REPS:-2}
BASE=debian:bookworm-slim
DJPEG=vision-spike-djpeg:local

# run <label> <image-name> <rep> <mem> <ballast MiB> <docker image> <command...>
run() {
	local label=$1 img=$2 rep=$3 mem=$4 ballast=$5 dimg=$6; shift 6
	local res
	res=$(docker run --rm --memory "$mem" --memory-swap "$mem" --cpus 1.0 --shm-size 600m -e HOME=/tmp \
		-v $B:/b:ro -v $I:/img:ro -v $O:/out "$dimg" sh -c \
		'if [ "$0" -gt 0 ]; then dd if=/dev/zero of=/dev/shm/ballast bs=1M count="$0" 2>/dev/null; fi; exec /b/rss "$@"' \
		"$ballast" "$@" 2>&1 | tr '\n' ' ')
	echo "ROW|$label|$img|$rep|mem=$mem ballast=${ballast}MiB|$res"
}

pc() { echo /b/photocraft-cli run "/img/$1.jpg" --cmd image.imageSize --params '{"width":1024}' --out "/out/$1.$2.jpg" --quality 85 --no-gpu; }

for rep in $(seq 1 "$REPS"); do
	for f in p48_baseline p48_progressive p48_444_progressive; do
		run photocraft-approve "$f" "$rep" 768m 0 $BASE $(pc $f pca) --approve-high-memory
		run photocraft-nolimit "$f" "$rep" 4g 0 $BASE $(pc $f pcn)
	done
	# (b) the 316 MiB a decode has beside aura's 452 MiB peak
	run ballast-only none "$rep" 768m 452 $BASE /b/rss true
	for f in p48_baseline p48_progressive p48_444_progressive; do
		run djpeg-1/4 "$f" "$rep" 768m 452 $DJPEG djpeg -scale 1/4 -bmp -outfile /out/$f.b.bmp /img/$f.jpg
		run jpegn "$f" "$rep" 768m 452 $BASE /b/jpegnprobe /img/$f.jpg /out/$f.bjpegn.jpg
	done
	run photocraft p24_progressive "$rep" 768m 452 $BASE $(pc p24_progressive bpc)
	run photocraft-approve p24_progressive "$rep" 768m 452 $BASE $(pc p24_progressive bpca) --approve-high-memory
	run photocraft p12_control "$rep" 768m 452 $BASE $(pc p12_control bpc)
done
