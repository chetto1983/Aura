#!/usr/bin/env bash
# Synthetic fixtures, made inside the spike container by its own ffmpeg. The sources are what a
# phone or a generator hands the library: H.264 High 4:2:0 at 1080p30 with a 1 s GOP, AAC-LC 48 kHz
# stereo. Each clip carries a sine only where the parity project listens for it (lib/project.mjs).
# The speech fixtures and the music are Plan A's committed files (web/e2e/fixtures/video-studio/audio).
set -euo pipefail
cd "$(dirname "$0")"
mkdir -p fixtures
AUDIO=fixtures-src
X264=(-c:v libx264 -profile:v high -level 4.0 -pix_fmt yuv420p -preset veryfast -crf 20 -g 30)
AAC=(-c:a aac -b:a 160k -ar 48000 -ac 2)
tone() { echo "aevalsrc=exprs='$1':s=48000:c=stereo:d=$2"; }

clip() { # name, video source, tone expression, seconds
  ffmpeg -v error -y -f lavfi -i "$2" -f lavfi -i "$(tone "$3" "$4")" -t "$4" "${X264[@]}" "${AAC[@]}" -movflags +faststart "fixtures/$1"
}

clip clip-1080p-5s.mp4 'testsrc2=size=1920x1080:rate=30' '0.25*sin(2*PI*440*t)' 5
clip clip-a.mp4 'testsrc2=size=1920x1080:rate=30' 'if(lt(t,5),0.25*sin(2*PI*1000*t),0)' 21
clip clip-b.mp4 'testsrc=size=1920x1080:rate=30' '0.25*sin(2*PI*600*t)' 21
clip clip-c.mp4 'gradients=size=1920x1080:rate=30:speed=0.02' 'if(gte(t,16),0.25*sin(2*PI*800*t),0)' 21
ffmpeg -v error -y -stream_loop -1 -i "$AUDIO/music.wav" -t 50 -c:a pcm_s16le fixtures/music-bed.wav
cp "$AUDIO/speech.wav" "$AUDIO/speech-noisy.wav" "$AUDIO/speech.truth.json" fixtures/
node --input-type=module -e "const {parityProject}=await import('./lib/project.mjs');const fs=await import('node:fs');fs.writeFileSync('fixtures/project.json',JSON.stringify(parityProject()));fs.writeFileSync('fixtures/project-720p.json',JSON.stringify(parityProject({width:1280,height:720})))"
ls -la fixtures
