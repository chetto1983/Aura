#!/usr/bin/env bash
# Waits up to $2 seconds (default 120) for the oldest parked completion with no answer in the
# hand endpoint's directory $1, and prints its number N (req-N.json); prints "none" otherwise.
# Never `ls | tail` the directory instead: a request already parked hides behind newer files.
D="${1:?usage: pending.sh <llm-dir> [seconds]}"
for _ in $(seq 1 "${2:-120}"); do
  for r in $(ls "$D"/req-*.json 2>/dev/null | sed 's/.*req-\([0-9]*\)\.json/\1/' | sort -n); do
    [ -f "$D/resp-$r.json" ] || { echo "$r"; exit 0; }
  done
  sleep 1
done
echo none
