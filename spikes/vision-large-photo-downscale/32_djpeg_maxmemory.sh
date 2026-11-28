#!/bin/bash
# libjpeg-turbo ships no backing store (jmemnobs.c), so -maxmemory should turn an over-budget
# progressive decode into an error instead of an OOM kill. Check it does, and what it costs when it fits.
set -u
S=/tmp/vision-spike; B=$S/bin; I=$S/img; O=$S/out
for kb in 100000 200000 300000; do
	for f in p48_progressive p48_444_progressive p48_baseline; do
		res=$(docker run --rm --memory 768m --memory-swap 768m --cpus 1.0 -v $B:/b:ro -v $I:/img:ro -v $O:/out \
			vision-spike-djpeg:local /b/rss djpeg -maxmemory $kb -scale 1/4 -bmp -outfile /out/$f.mm.bmp /img/$f.jpg 2>&1 | tr '\n' ' ')
		echo "ROW|djpeg-1/4 -maxmemory ${kb}K|$f|$res"
	done
done
