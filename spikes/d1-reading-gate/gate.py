"""Step 3: d1-2B, local, as the reading gate (paper §4.3, E3) over each question's candidates.

The candidates are the top-8 passages (Aura's default `limit`, internal/documents/retrieval.go) of a
BM25 ranking over every passage of both corpora -- the paper's own harness is BM25, and Aura's
lexical mode is too. BM25 is the routing spike's implementation, imported rather than copied.

The question is the `answers` question of the 2026-10-25 judge spike, folded into one noul
instruction because llama.cpp's noul takes `instructions` only (d1-2B model card, question types).
"""

import json
import pathlib
import sys
import time
import urllib.request

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[1] / "document-routing-benchmark"))
from benchmark import BM25, tok  # noqa: E402

from oracle import carries  # noqa: E402

LIMIT = 8
ANSWERS = ("Does `document` contain the information that answers `query`? Yes when the document's text "
           "states the fact, value, name or passage that `query` asks for. No when it does not, even if it "
           "is about a related subject.")


def answers(url: str, query: str, title: str, text: str) -> tuple[float, int, float]:
    body = {"state": {"query": query, "document": {"title": title, "text": text}},
            "questions": {"answers": {"type": "noul", "instructions": ANSWERS}}}
    request = urllib.request.Request(url + "/v1/systemone", data=json.dumps(body).encode(),
                                     headers={"Content-Type": "application/json"})
    started = time.monotonic()
    with urllib.request.urlopen(request, timeout=600) as response:
        got = json.load(response)
    return got["answers"]["answers"]["noul"], got["usage"]["input_tokens"], time.monotonic() - started


def main(url: str, passages_path: str, cases_path: str, out: str) -> None:
    passages = [json.loads(line) for line in open(passages_path, encoding="utf-8")]
    index = BM25([(i, tok(p["text"])) for i, p in enumerate(passages)])
    cases = json.load(open(cases_path, encoding="utf-8"))["cases"]
    with open(out, "w", encoding="utf-8") as sink:
        for case in cases:
            for rank, (i, bm25) in enumerate(index.rank(case["query"])[:LIMIT], start=1):
                p = passages[i]
                score, input_tokens, seconds = answers(url, case["query"], p["doc"], p["text"])
                row = {"qid": case["qid"], "category": case["category"], "rank": rank, "bm25": bm25,
                       "doc": p["doc"], "ordinal": p["ordinal"], "tokens": p["tokens"],
                       "answers": carries(p["text"], case["answer"]) and p["doc"] in case["gold_docs"],
                       "d1": score, "d1_input_tokens": input_tokens, "d1_seconds": round(seconds, 2)}
                sink.write(json.dumps(row, ensure_ascii=False) + "\n")
                sink.flush()
            print(f"{case['qid']} done", file=sys.stderr, flush=True)


if __name__ == "__main__":
    main(*sys.argv[1:5])
