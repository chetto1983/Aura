"""What concurrency and llama.cpp's prompt cache buy d1-2B on this host.

The workload is the reading gate reshaped for the cache: the state is the passage alone and the
user's question moves into the noul instruction, so every question about one passage shares the
passage as prefix. Each case asks its 8 BM25 candidates (gate.jsonl), so a passage that several
questions retrieve is read again -- which is what the cache is for.

Run it once against a server started without --cache-ram and once with it; `--workers` sets how
many requests are in flight. Prints wall time, decisions per second and per-request latency.
"""

import argparse
import json
import statistics
import time
import urllib.request
from concurrent.futures import ThreadPoolExecutor

ANSWERS = ("Does this document contain the information that answers the question \"{query}\"? Yes when its text "
           "states the fact, value, name or passage the question asks for. No when it does not, even if it is "
           "about a related subject.")


def ask(url: str, state: str, query: str) -> tuple[float, float]:
    body = {"state": state, "questions": {"answers": {"type": "noul", "instructions": ANSWERS.format(query=query)}}}
    request = urllib.request.Request(url + "/v1/systemone", data=json.dumps(body).encode(),
                                     headers={"Content-Type": "application/json"})
    started = time.monotonic()
    with urllib.request.urlopen(request, timeout=900) as response:
        got = json.load(response)
    return got["answers"]["answers"]["noul"], time.monotonic() - started


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--url", default="http://127.0.0.1:8080")
    parser.add_argument("--passages", required=True)
    parser.add_argument("--gate", required=True, help="gate.jsonl from spikes/d1-reading-gate")
    parser.add_argument("--cases", required=True)
    parser.add_argument("--workers", type=int, default=1)
    parser.add_argument("--limit", type=int, default=80, help="pairs to run, in gate order")
    parser.add_argument("--label", required=True)
    parser.add_argument("--out", required=True)
    args = parser.parse_args()
    passages = {(p["doc"], p["ordinal"]): p["text"] for p in map(json.loads, open(args.passages, encoding="utf-8"))}
    queries = {c["qid"]: c["query"] for c in json.load(open(args.cases, encoding="utf-8"))["cases"]}
    pairs = [json.loads(line) for line in open(args.gate, encoding="utf-8")][:args.limit]
    started = time.monotonic()
    with ThreadPoolExecutor(max_workers=args.workers) as pool:
        results = list(pool.map(lambda r: ask(args.url, passages[(r["doc"], r["ordinal"])], queries[r["qid"]]), pairs))
    wall = time.monotonic() - started
    latencies = [seconds for _, seconds in results]
    seen, repeat = set(), []
    for r, (_, seconds) in zip(pairs, results):
        key = (r["doc"], r["ordinal"])
        repeat.append((key in seen, seconds))
        seen.add(key)
    summary = {
        "label": args.label, "workers": args.workers, "pairs": len(pairs), "wall_s": round(wall, 1),
        "decisions_per_s": round(len(pairs) / wall, 3),
        "latency_median_s": round(statistics.median(latencies), 2), "latency_max_s": round(max(latencies), 2),
        "first_read_median_s": round(statistics.median([s for again, s in repeat if not again]), 2),
        "repeat_read_median_s": round(statistics.median([s for again, s in repeat if again] or [0]), 2),
        "repeat_reads": sum(again for again, _ in repeat),
        "scores": [round(score, 4) for score, _ in results],
    }
    with open(args.out, "a", encoding="utf-8") as sink:
        sink.write(json.dumps(summary) + "\n")
    print(json.dumps({k: v for k, v in summary.items() if k != "scores"}))


if __name__ == "__main__":
    main()
