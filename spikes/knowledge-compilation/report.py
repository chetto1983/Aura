"""Step 5: the numbers FINDINGS.md quotes, from the four artefacts of steps 1-4.

Tokens are counted with EmbeddingGemma's tokenizer (ingest chunk.count_tokens), the one Aura's
passages are cut by -- not the answering model's. Both arms use the same counter, so the ratios
hold; the absolute counts are not what a given chat model would bill.
"""

import json
import statistics
import sys

from gate import render
from ingest.chunk import count_tokens
from oracle import carries

THRESHOLD = 0.5  # the 2026-10-25 spike's untuned threshold, not fitted here


def auroc(positives: list[float], negatives: list[float]) -> float:
    pairs = [(p > n) + 0.5 * (p == n) for p in positives for n in negatives]
    return sum(pairs) / len(pairs)


def main(passages_path: str, cases_path: str, claims_path: str, gate_path: str) -> None:
    passages = {(p["doc"], p["ordinal"]): p for p in map(json.loads, open(passages_path, encoding="utf-8"))}
    compiled = {(c["doc"], c["ordinal"]): c for c in map(json.loads, open(claims_path, encoding="utf-8"))}
    gates = {g["qid"]: g for g in map(json.loads, open(gate_path, encoding="utf-8"))}
    cases = [c for c in json.load(open(cases_path, encoding="utf-8"))["cases"] if (c["doc"], c["ordinal"]) in compiled]

    print("## Compilation, per passage")
    kept = sum(len(c["kept"]) for c in compiled.values())
    dropped = sum(len(c["dropped"]) for c in compiled.values())
    ratios = []
    for key, c in sorted(compiled.items()):
        raw = count_tokens(passages[key]["text"])
        sub = count_tokens(render(c["kept"]))
        ratios.append(sub / raw)
        print(f"{key[0]}#{key[1]}: raw {raw} tok, substitute {sub} tok ({sub / raw:.2f}x), "
              f"{len(c['kept'])} kept / {len(c['dropped'])} dropped, {c['seconds']}s, "
              f"{c['usage']['completion_tokens']} generated")
    print(f"pairs kept {kept}, dropped for unlocatable evidence {dropped} ({dropped / max(kept + dropped, 1):.1%})")
    print(f"substitute/raw token ratio: median {statistics.median(ratios):.2f}, "
          f"min {min(ratios):.2f}, max {max(ratios):.2f}")

    print("\n## E2 substitution and the d1 gate, per question")
    deltas, rows = [], []
    for case in cases:
        key = (case["doc"], case["ordinal"])
        raw_text, sub_text = passages[key]["text"], render(compiled[key]["kept"])
        raw, sub = count_tokens(raw_text), count_tokens(sub_text)
        hit = carries(sub_text, case["answer"])
        delta = sub / raw - 1 if hit else (sub + raw) / raw - 1
        deltas.append(delta)
        g = gates.get(case["qid"], {})
        rows.append((case, hit, delta, g))
        print(f"{case['qid']:28} {case['category']:10} sufficient={hit!s:5} delta={delta:+.0%} "
              f"d1 raw={g.get('raw', float('nan')):.2f} claims={g.get('claims', float('nan')):.2f} "
              f"neg={g.get('negative', float('nan')):.2f}")
    hits = [r for r in rows if r[1]]
    p = len(hits) / len(rows)
    print(f"\nsufficiency (oracle) p_h = {len(hits)}/{len(rows)} = {p:.0%}")
    print(f"mean token delta under E2 with raw fallback: {statistics.mean(deltas):+.1%}")
    hit_d = [r[2] for r in rows if r[1]]
    miss_d = [r[2] for r in rows if not r[1]]
    if hit_d and miss_d:
        print(f"hit delta mean {statistics.mean(hit_d):+.1%}, miss delta mean {statistics.mean(miss_d):+.1%}")

    gated = [r for r in rows if r[3]]
    if gated:
        print("\n## d1-2B gate (Q4_K_M, llama.cpp b11960)")
        for arm in ("raw", "claims"):
            print(f"AUROC {arm} vs negative: {auroc([r[3][arm] for r in gated], [r[3]['negative'] for r in gated]):.3f}")
        accepted = [r for r in gated if r[3]["claims"] >= THRESHOLD]
        false_accept = [r for r in accepted if not r[1]]
        false_reject = [r for r in gated if r[3]["claims"] < THRESHOLD and r[1]]
        print(f"claims accepted at {THRESHOLD}: {len(accepted)}/{len(gated)}; "
              f"false accepts (oracle says insufficient) {len(false_accept)}: {[r[0]['qid'] for r in false_accept]}; "
              f"false rejects {len(false_reject)}: {[r[0]['qid'] for r in false_reject]}")
        print(f"raw accepted at {THRESHOLD}: {sum(r[3]['raw'] >= THRESHOLD for r in gated)}/{len(gated)}; "
              f"negatives accepted: {sum(r[3]['negative'] >= THRESHOLD for r in gated)}/{len(gated)}")
        # E3: serve the claims when d1 accepts them, else the raw passage alone. The gate's own read is
        # d1's input, not the answering model's, and is reported apart.
        policy = []
        for case, hit, _, g in gated:
            key = (case["doc"], case["ordinal"])
            raw = count_tokens(passages[key]["text"])
            sub = count_tokens(render(compiled[key]["kept"]))
            policy.append(sub / raw - 1 if g["claims"] >= THRESHOLD else 0.0)
        print(f"E3 gated substitution, mean token delta to the answering model: {statistics.mean(policy):+.1%}; "
              f"answers lost to false accepts: {len(false_accept)}/{len(gated)}")
        print(f"d1 input tokens per claims decision, median: {statistics.median(r[3]['claims_input_tokens'] for r in gated)}")


if __name__ == "__main__":
    main(*sys.argv[1:5])
