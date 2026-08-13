"""Spike: is a conversation's chain-of-thought worth indexing as searchable memory?

The question is NOT "can we store it" (Aura already does: aura.conversation_turns.reasoning).
It is whether the CoT carries retrievable information the visible answer does not. If the
reasoning is the answer restated, indexing it buys nothing and costs an embed call per turn.

Two measurements, both with ground truth that is NOT self-generated:

  M1 marginal information — cos(reasoning_i, answer_i) per turn. A distribution near 1.0
     means the CoT is a paraphrase of its own answer: no marginal signal, idea dead.

  M2 retrievability — the REAL user message that produced turn i is the query; the pairing
     (message -> its own answer / its own reasoning) is a fact of the transcript, not a
     label anyone invented here. recall@1 over the answer index vs the reasoning index
     says whether each surface is reachable from the question at all.

Usage:  python probe.py <corpus.json> [--out results.txt]
The corpus is real operator conversation data and deliberately lives OUTSIDE the repo.
"""

from __future__ import annotations

import argparse
import json
import math
import sys
import urllib.error
import urllib.request
from concurrent.futures import ThreadPoolExecutor

EMBED_URL = "http://127.0.0.1:8081/v1/embeddings"
# EmbeddingGemma is asymmetric. These two prefixes are the repo's own convention
# (internal/embeddings/tasks.go, services/ingest/chunk.py) — a spike that invented its
# own would measure a retrieval stack Aura does not run.
DOC_PREFIX = "title: none | text: "
QUERY_PREFIX = "task: search result | query: "
# llama.cpp serves 4 slots; anything past that just queues.
CONCURRENCY = 4


def embed(text: str, prefix: str) -> list[float]:
    payload = json.dumps({"input": prefix + text, "model": "embeddinggemma"}).encode()
    req = urllib.request.Request(
        EMBED_URL, data=payload, headers={"Content-Type": "application/json"}
    )
    with urllib.request.urlopen(req, timeout=120) as resp:
        return json.load(resp)["data"][0]["embedding"]


def embed_all(texts: list[str], prefix: str) -> list[list[float]]:
    with ThreadPoolExecutor(max_workers=CONCURRENCY) as pool:
        return list(pool.map(lambda t: embed(t, prefix), texts))


def cosine(a: list[float], b: list[float]) -> float:
    dot = sum(x * y for x, y in zip(a, b))
    na = math.sqrt(sum(x * x for x in a))
    nb = math.sqrt(sum(y * y for y in b))
    return dot / (na * nb) if na and nb else 0.0


def quantiles(xs: list[float]) -> dict[str, float]:
    s = sorted(xs)
    at = lambda q: s[min(len(s) - 1, int(q * len(s)))]
    return {"min": s[0], "p25": at(0.25), "median": at(0.50), "p75": at(0.75), "max": s[-1]}


def recall_at(queries: list[list[float]], docs: list[list[float]], k: int,
              subset: list[int] | None = None) -> float:
    """Fraction of queries whose OWN document ranks in the top k. Index i pairs with i.

    subset restricts which queries are SCORED; every document stays in the pool, so a
    subset result is still competing against the full corpus and is comparable to the
    full-corpus number.
    """
    idx = range(len(queries)) if subset is None else subset
    idx = list(idx)
    hits = 0
    for i in idx:
        q = queries[i]
        ranked = sorted(range(len(docs)), key=lambda j: cosine(q, docs[j]), reverse=True)
        if i in ranked[:k]:
            hits += 1
    return hits / len(idx) if idx else 0.0


def echoes_question(reasoning: str, question: str, min_run: int = 15) -> bool:
    """True when the reasoning quotes a long-enough literal run of the user's question.

    Chain-of-thought habitually opens by restating the prompt ("The user asked '…'"), so a
    question->reasoning retrieval win could be nothing but that copy coming back. Rows where
    the copy exists are separated out rather than edited: mutilating the text would measure a
    document Aura never stores.
    """
    q = " ".join(question.split()).lower()
    r = " ".join(reasoning.split()).lower()
    if len(q) < min_run:
        return q in r
    return any(q[i:i + min_run] in r for i in range(len(q) - min_run + 1))


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("corpus")
    ap.add_argument("--out", default=None)
    args = ap.parse_args()

    with open(args.corpus, encoding="utf-8") as fh:
        rows = json.load(fh)
    # A turn with no visible answer cannot take part in the answer-vs-reasoning comparison.
    rows = [r for r in rows if (r.get("reasoning") or "").strip() and (r.get("answer") or "").strip()]

    reasoning = [r["reasoning"] for r in rows]
    answers = [r["answer"] for r in rows]
    questions = [r["user_msg"] or "" for r in rows]

    print(f"corpus: {len(rows)} turns, {len({r['conversation_id'] for r in rows})} conversations")
    print("embedding reasoning / answers / questions ...", flush=True)
    e_reasoning = embed_all(reasoning, DOC_PREFIX)
    e_answers = embed_all(answers, DOC_PREFIX)
    e_questions = embed_all(questions, QUERY_PREFIX)

    pairwise = [cosine(e_reasoning[i], e_answers[i]) for i in range(len(rows))]
    m1 = quantiles(pairwise)

    m2 = {
        "answers_recall@1": recall_at(e_questions, e_answers, 1),
        "reasoning_recall@1": recall_at(e_questions, e_reasoning, 1),
        "answers_recall@3": recall_at(e_questions, e_answers, 3),
        "reasoning_recall@3": recall_at(e_questions, e_reasoning, 3),
    }

    # M3: the confound. CoT usually opens by restating the prompt, so M2's reasoning win
    # could be that quotation retrieved back. Score the two subsets separately.
    echo = [i for i in range(len(rows)) if echoes_question(reasoning[i], questions[i])]
    noecho = [i for i in range(len(rows)) if i not in set(echo)]
    m3 = {
        "echo_rows": len(echo),
        "noecho_rows": len(noecho),
        "noecho_answers_recall@1": recall_at(e_questions, e_answers, 1, noecho),
        "noecho_reasoning_recall@1": recall_at(e_questions, e_reasoning, 1, noecho),
        "echo_reasoning_recall@1": recall_at(e_questions, e_reasoning, 1, echo),
    }

    # M4: "grazie" / "quindi?" have no retrievable target on ANY surface and drag both
    # numbers down equally. Re-score on substantive queries only, still against the full pool.
    substantive = [i for i in noecho if len(" ".join(questions[i].split())) >= 25]
    m4 = {
        "substantive_noecho_rows": len(substantive),
        "answers_recall@1": recall_at(e_questions, e_answers, 1, substantive),
        "reasoning_recall@1": recall_at(e_questions, e_reasoning, 1, substantive),
    }

    lines = [
        f"turns={len(rows)}  conversations={len({r['conversation_id'] for r in rows})}",
        "",
        "M1 cos(reasoning_i, answer_i) — marginal information of the CoT over its own answer",
        "  " + "  ".join(f"{k}={v:.3f}" for k, v in m1.items()),
        "",
        "M2 recall@k with the real user message as query (ground truth = the transcript pairing)",
        "  " + "  ".join(f"{k}={v:.3f}" for k, v in m2.items()),
        "",
        "M3 confound check — does the CoT just quote the question back?",
        "  " + "  ".join(
            f"{k}={v:.3f}" if isinstance(v, float) else f"{k}={v}" for k, v in m3.items()
        ),
        "",
        "M4 same as M3's no-echo subset, minus degenerate queries ('grazie', 'quindi?')",
        "  " + "  ".join(
            f"{k}={v:.3f}" if isinstance(v, float) else f"{k}={v}" for k, v in m4.items()
        ),
        "",
        "least-similar pairs (largest marginal signal — READ THESE before trusting the number):",
    ]
    for i in sorted(range(len(rows)), key=lambda j: pairwise[j])[:5]:
        lines.append(f"  cos={pairwise[i]:.3f}  q={questions[i][:70]!r}")
    report = "\n".join(lines)
    print("\n" + report)
    if args.out:
        with open(args.out, "w", encoding="utf-8") as fh:
            fh.write(report + "\n")
    return 0


if __name__ == "__main__":
    sys.exit(main())
