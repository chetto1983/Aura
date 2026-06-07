#!/usr/bin/env bash
# Spike 022 — harvest N FLEURS test clips + reference transcripts per language.
# Stream-extracts from the HF tar.gz and kills the download once N wavs landed
# (~60-100MB instead of ~1GB per language). Transcripts matched from test.tsv col 3
# (already-normalized lowercase text — the standard WER reference).
#
# Usage: bash harvest-fleurs.sh [N]   (default 15; outputs to D:/tmp/spike-022/{it,en}/)
set -u
N="${1:-15}"
BASE="https://huggingface.co/datasets/google/fleurs/resolve/main/data"
OUT=/d/tmp/spike-022

for pair in "it_it:it" "en_us:en"; do
  cfg="${pair%%:*}"; lang="${pair##*:}"
  dir="$OUT/$lang"
  mkdir -p "$dir"
  echo "=== $cfg → $dir (target $N clips) ==="

  curl -sL "$BASE/$cfg/test.tsv" -o "$dir/test.tsv"
  [ -s "$dir/test.tsv" ] || { echo "FATAL: test.tsv download failed for $cfg"; exit 1; }

  # stream-extract; kill once N files are present
  curl -sL "$BASE/$cfg/audio/test.tar.gz" | tar -xz -C "$dir" 2>/dev/null &
  CURL_PID=$!
  for _ in $(seq 1 600); do
    count=$(find "$dir" -name '*.wav' 2>/dev/null | wc -l)
    if [ "$count" -ge "$N" ]; then break; fi
    sleep 1
  done
  kill $CURL_PID 2>/dev/null; wait $CURL_PID 2>/dev/null

  # flatten (tar may nest under test/) and trim to exactly N
  find "$dir" -name '*.wav' -not -path "$dir/*.wav" -exec mv {} "$dir/" \; 2>/dev/null
  ls "$dir"/*.wav 2>/dev/null | tail -n +"$((N+1))" | xargs -r rm -f
  find "$dir" -type d -empty -delete 2>/dev/null

  # build refs.tsv: filename<TAB>normalized transcript for the harvested wavs only
  : > "$dir/refs.tsv"
  for w in "$dir"/*.wav; do
    fn=$(basename "$w")
    ref=$(awk -F'\t' -v f="$fn" '$2==f {print $4; exit}' "$dir/test.tsv")
    if [ -n "$ref" ]; then
      printf '%s\t%s\n' "$fn" "$ref" >> "$dir/refs.tsv"
    else
      echo "WARN: no transcript for $fn — removing clip"
      rm -f "$w"
    fi
  done
  echo "harvested: $(wc -l < "$dir/refs.tsv") clips with refs"
done
echo "=== DONE ==="
