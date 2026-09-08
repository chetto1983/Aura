#!/usr/bin/env python3
"""Merges the two per-identity timing jsonl files scripts/musr_live_run_conversation.py wrote
into ONE timings.jsonl, sorted by wall-clock timestamp, which is the file
scripts/musr_live_run_assert.go reads for the overlap check.

Usage: musr_live_run_merge_timings.py <timing_a> <timing_b> <out_path>
A missing input file (a conversation that never started) is treated as empty, not an error —
scripts/musr_live_run_assert.go's own completion check is what fails a missing conversation.
"""
import json
import sys


def main() -> None:
    if len(sys.argv) != 4:
        sys.stderr.write("usage: musr_live_run_merge_timings.py <timing_a> <timing_b> <out_path>\n")
        sys.exit(2)
    a_path, b_path, out_path = sys.argv[1:4]

    entries = []
    for path in (a_path, b_path):
        try:
            with open(path, encoding="utf-8") as f:
                for line in f:
                    line = line.strip()
                    if line:
                        entries.append(json.loads(line))
        except FileNotFoundError:
            pass
    entries.sort(key=lambda e: e.get("ts", 0))

    with open(out_path, "w", encoding="utf-8") as f:
        for entry in entries:
            f.write(json.dumps(entry) + "\n")


if __name__ == "__main__":
    main()
