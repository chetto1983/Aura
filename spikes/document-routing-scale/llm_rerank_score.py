"""Score the LLM-as-reranker picks against the hidden ground truth.

Separates the two retrieval stages:
  - CANDIDATE RECALL (ceiling): did hybrid retrieval surface the needle at all?
  - RERANK PRECISION: of the surfaced needles, how many did the LLM pick?
A perfect reranker cannot exceed the candidate-recall ceiling.
"""

import json
import os

HERE = os.path.dirname(os.path.abspath(__file__))


def load(name):
    return json.load(open(os.path.join(HERE, name)))


def main():
    cands = {c["section"]: c for c in load("llm_rerank_candidates.json")}
    truth = {t["section"]: t for t in load("llm_rerank_truth.json")}
    picks = {p["section"]: p["pick"] for p in load("llm_rerank_picks.json")["picks"]}

    ceiling = 0          # needle present in top-K
    recovered = 0        # LLM picked a candidate that contains the needle
    abstain_ok = 0       # LLM abstained AND needle really was absent
    hallucinated = 0     # LLM picked something when needle was absent, or picked wrong
    n = len(truth)

    print("sec  needle_in_topK  llm_pick  outcome")
    for sec in sorted(truth):
        gt = truth[sec]["gt_candidate_id"]
        needle = truth[sec]["needle"]
        present = gt is not None
        ceiling += 1 if present else 0
        pick = picks[sec]

        picked_text = ""
        if pick is not None and pick >= 0:
            picked_text = next(c["text"] for c in cands[sec]["candidates"] if c["id"] == pick)
        hit = needle[:30] in picked_text

        if hit:
            recovered += 1; outcome = "RECOVERED"
        elif pick == -1 and not present:
            abstain_ok += 1; outcome = "abstain-correct"
        elif pick == -1 and present:
            outcome = "abstain-WRONG (was present!)"
        else:
            hallucinated += 1; outcome = "wrong-pick"
        print("%-4d %-15s %-9s %s" % (sec, str(present), str(pick), outcome))

    print("\n--- summary (8 paraphrased queries) ---")
    print("candidate-recall ceiling (retrieval): %d/%d" % (ceiling, n))
    print("LLM-rerank recovered:                 %d/%d" % (recovered, n))
    print("LLM precision on recoverable:         %d/%d" % (recovered, ceiling))
    print("correct abstentions (needle absent):  %d/%d" % (abstain_ok, n - ceiling))
    print("hallucinated / wrong picks:           %d" % hallucinated)
    print("\nreading: the reranker is %s on what retrieval surfaced;" %
          ("PERFECT" if recovered == ceiling else "imperfect"))
    print("the ceiling (%d/%d) is set by RETRIEVAL, not by the reranker." % (ceiling, n))


if __name__ == "__main__":
    main()
