#!/usr/bin/env bash
# Colour tags of the source and each render, and each render's PSNR against the source read as
# BT.601 (ffmpeg's assumption for an untagged SD-or-HD stream) and as BT.709 (a browser's for HD).
cd "$(dirname "$0")"
src=fixtures/clip-1080p-5s.mp4
for f in $src out/s1-render-*.mp4; do
  echo "== $f $(ffprobe -v error -select_streams v:0 -show_entries stream=pix_fmt,color_range,color_space,color_primaries -of compact=p=0:nk=1 "$f")"
done
for f in out/s1-render-*.mp4; do
  for m in bt470bg bt709; do
    p=$(ffmpeg -v info -i "$f" -i $src -lavfi "[1:v]setparams=range=tv:colorspace=$m:color_primaries=$m:color_trc=$m,format=gbrp[s];[0:v]format=gbrp[r];[r][s]psnr" -f null - 2>&1 | grep -o "average:[0-9.inf]*")
    echo "$f vs source-as-$m: $p"
  done
done
