#!/usr/bin/env bash
# ffprobe summary of each file: container duration, then per-stream codec/size/rotation/duration.
cd "$(dirname "$0")"
for f in "$@"; do
  MSYS_NO_PATHCONV=1 docker run --rm -v "$(pwd -W)/$(dirname "$f"):/m" --entrypoint ffprobe jrottenberg/ffmpeg:7.1-alpine \
    -v error -show_entries format=duration,size:stream=index,codec_name,profile,width,height,r_frame_rate,duration,nb_frames:stream_side_data=rotation \
    -of compact=p=0:nk=0 "/m/$(basename "$f")" 2>&1 | grep -v -E "set_mempolicy|Fontconfig" | sed "s#^#$(basename "$f") | #"
done
