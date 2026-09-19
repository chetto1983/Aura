#!/usr/bin/env bash
# Builds the spike's test media into ./media. Videos carry a burned-in timestamp and frame
# counter so a trimmed clip's first frame says where the cut really landed. Photos are real
# photographs from picsum.photos (Unsplash licence).
set -euo pipefail
cd "$(dirname "$0")"
mkdir -p media
M="$(pwd -W 2>/dev/null || pwd)/media"
FONT_DIR="C:/Windows/Fonts"
ff() { MSYS_NO_PATHCONV=1 docker run --rm -v "$M:/m" -v "$FONT_DIR:/f:ro" jrottenberg/ffmpeg:7.1-alpine -hide_banner -loglevel error -y "$@"; }
STAMP="drawtext=fontfile=/f/consola.ttf:text='%{pts\:hms} f%{n}':fontsize=h/12:fontcolor=white:box=1:boxcolor=black@0.7:x=20:y=20"

video() { # name size fps seconds vcodec-args acodec-args extra-input-args
  local name=$1 size=$2 fps=$3 d=$4 v=$5 a=$6
  if [ "$a" = none ]; then
    ff -f lavfi -i "testsrc2=s=$size:r=$fps:d=$d" -vf "$STAMP" $v "/m/$name"
  else
    ff -f lavfi -i "testsrc2=s=$size:r=$fps:d=$d" -f lavfi -i "sine=f=440:d=$d:b=4" \
      -vf "$STAMP" $v $a -shortest "/m/$name"
  fi
}

# What an OpenRouter video model hands back: H.264 + AAC MP4, keyframe every 2 s.
video clip-h264-aac.mp4 1280x720 30 10 "-c:v libx264 -pix_fmt yuv420p -g 60 -movflags +faststart" "-c:a aac -b:a 128k"
# Several video models return no audio track at all.
video clip-h264-silent.mp4 1280x720 24 8 "-c:v libx264 -pix_fmt yuv420p -g 48" none
video clip-vp9-opus.webm 1280x720 30 10 "-c:v libvpx-vp9 -b:v 1M -g 60" "-c:a libopus"
# A phone upload: HEVC 1080p, tagged hvc1 the way iOS writes it.
video clip-hevc-aac.mp4 1920x1080 30 10 "-c:v libx265 -tag:v hvc1 -pix_fmt yuv420p -x265-params keyint=60:log-level=error" "-c:a aac"
# Long clip for speed and memory.
video clip-long-1080p.mp4 1920x1080 30 60 "-c:v libx264 -pix_fmt yuv420p -g 60 -preset veryfast" "-c:a aac"
# A portrait phone clip: landscape pixels plus a 90° display matrix.
ff -display_rotation 90 -i /m/clip-h264-aac.mp4 -c copy /m/clip-rot90.mp4

# Photos: a 12 MP photograph, the same with EXIF Orientation=6, and a Studio-sized PNG/WebP.
curl -sSL -o media/photo-12mp.jpg "https://picsum.photos/id/1018/4032/3024.jpg"
node exif-orientation.mjs media/photo-12mp.jpg media/photo-exif6.jpg 6
curl -sSL -o media/studio-src.jpg "https://picsum.photos/id/1025/1024/1024.jpg"
ff -i /m/studio-src.jpg /m/studio-1024.png
ff -i /m/studio-src.jpg -quality 90 /m/studio-1024.webp
rm -f media/studio-src.jpg media/probe.mp4
ls -la media
