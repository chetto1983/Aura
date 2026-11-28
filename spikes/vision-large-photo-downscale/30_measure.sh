#!/bin/bash
# The matrix: every candidate x every image, one fresh container per run, limited as compose limits
# the aura container (mem_limit 768m, cpus 1.0) with swap off (--memory-swap = --memory), so an
# over-limit process is killed instead of paging. rss prints the child's ru_maxrss (= VmHWM) and
# the container's memory.peak (page cache included, so a cross-check only). REPS runs per cell.
export PATH="$HOME/.local/bin:$HOME/go/bin:/usr/local/go/bin:$PATH"
set -u
S=/tmp/vision-spike; B=$S/bin; I=$S/img; O=$S/out
REPS=${REPS:-2}
MEM=${MEM:-768m}
mkdir -p $O
SANDBOX=ghcr.io/chetto1983/aura-sandbox:edge
BASE=debian:bookworm-slim
DJPEG=vision-spike-djpeg:local
JPEGS="p12_control p24_progressive p48_baseline p48_progressive p48_444_progressive"

# run <label> <image-name> <rep> <docker image> <command...>
run() {
	local label=$1 img=$2 rep=$3 dimg=$4; shift 4
	local res
	res=$(docker run --rm --memory "$MEM" --memory-swap "$MEM" --cpus 1.0 -e HOME=/tmp \
		-v $B:/b:ro -v $I:/img:ro -v $O:/out "$dimg" /b/rss "$@" 2>&1 | tr '\n' ' ')
	echo "ROW|$label|$img|$rep|$MEM|$res"
}

# largest N in 8,4,2 whose 1/N still has a long edge of at least 1024 px
denom() {
	case $1 in
		p12_control) echo 2 ;;
		*) echo 4 ;;
	esac
}

for rep in $(seq 1 "$REPS"); do
	run go-empty none "$rep" $BASE /b/baseline /nonexistent
	for f in $JPEGS; do
		run go-today "$f" "$rep" $BASE /b/baseline /img/$f.jpg /out/$f.today.jpg
		n=$(denom $f)
		run "djpeg-1/$n" "$f" "$rep" $DJPEG djpeg -scale 1/$n -bmp -outfile /out/$f.djpeg.bmp /img/$f.jpg
		run "go-after-djpeg-1/$n" "$f" "$rep" $BASE /b/baseline /out/$f.djpeg.bmp /out/$f.djpeg.jpg
		run jpegn "$f" "$rep" $BASE /b/jpegnprobe /img/$f.jpg /out/$f.jpegn.jpg
		run photocraft "$f" "$rep" $BASE /b/photocraft-cli run /img/$f.jpg --cmd image.imageSize \
			--params '{"width":1024}' --out /out/$f.pc.jpg --quality 85 --no-gpu
	done
	# what a scaled decode saves, on the progressive 48 MP: full size and 1/8 beside the 1/4 above
	run djpeg-1/1 p48_progressive "$rep" $DJPEG djpeg -bmp -outfile /out/p48_progressive.djpeg1.bmp /img/p48_progressive.jpg
	run djpeg-1/8 p48_progressive "$rep" $DJPEG djpeg -scale 1/8 -bmp -outfile /out/p48_progressive.djpeg8.bmp /img/p48_progressive.jpg
	run go-today p48_png "$rep" $BASE /b/baseline /img/p48.png /out/p48_png.today.jpg
	run photocraft p48_png "$rep" $BASE /b/photocraft-cli run /img/p48.png --cmd image.imageSize \
		--params '{"width":1024}' --out /out/p48_png.pc.jpg --quality 85 --no-gpu
	run photocraft-info p48_progressive "$rep" $BASE /b/photocraft-cli info /img/p48_progressive.jpg
	# the same binary in the image it ships in today
	run photocraft-in-sandbox-image p48_progressive "$rep" $SANDBOX /b/photocraft-cli run /img/p48_progressive.jpg \
		--cmd image.imageSize --params '{"width":1024}' --out /out/p48_progressive.pcsb.jpg --quality 85 --no-gpu
done
