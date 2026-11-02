"""Step 4: d1-2B, local, as the sufficiency gate (paper §4.3, E3) -- over the raw passage, over its
compiled claims, and over a passage of another document as the negative.

The question is the `answers` question of the 2026-10-25 judge spike, folded into one noul
instruction because llama.cpp's noul takes `instructions` only (d1-2B model card, question types).
"""

import json
import random
import sys
import urllib.request

ANSWERS = ("Does `document` contain the information that answers `query`? Yes when the document's text "
           "states the fact, value, name or passage that `query` asks for. No when it does not, even if it "
           "is about a related subject.")


def render(claims: list[dict]) -> str:
    """E2's substitute for the passage: each pair as its claim, then its verbatim evidence."""
    return "\n".join(f"- {c['claim']} «{c['evidence']}»" for c in claims)


def answers(url: str, query: str, title: str, text: str) -> tuple[float, int]:
    body = {"state": {"query": query, "document": {"title": title, "text": text}},
            "questions": {"answers": {"type": "noul", "instructions": ANSWERS}}}
    request = urllib.request.Request(url + "/v1/systemone", data=json.dumps(body).encode(),
                                     headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(request, timeout=600) as response:
        got = json.load(response)
    return got["answers"]["answers"]["noul"], got["usage"]["input_tokens"]


def main(url: str, passages_path: str, cases_path: str, claims_path: str, out: str) -> None:
    passages = [json.loads(line) for line in open(passages_path, encoding="utf-8")]
    by_key = {(p["doc"], p["ordinal"]): p for p in passages}
    compiled = {(c["doc"], c["ordinal"]): c for c in map(json.loads, open(claims_path, encoding="utf-8"))}
    cases = json.load(open(cases_path, encoding="utf-8"))["cases"]
    pick = random.Random(20261102)
    with open(out, "w", encoding="utf-8") as sink:
        for case in cases:
            key = (case["doc"], case["ordinal"])
            if key not in compiled:
                continue
            gold_docs = set(case["gold_docs"])
            negative = pick.choice([p for p in passages if p["doc"] not in gold_docs and p["text"].strip()])
            row = {"qid": case["qid"]}
            for arm, title, text in (("raw", case["doc"], by_key[key]["text"]),
                                     ("claims", case["doc"], render(compiled[key]["kept"])),
                                     ("negative", negative["doc"], negative["text"])):
                row[arm], row[arm + "_input_tokens"] = answers(url, case["query"], title, text)
            row["negative_doc"] = negative["doc"]
            sink.write(json.dumps(row, ensure_ascii=False) + "\n")
            print(row, file=sys.stderr)


if __name__ == "__main__":
    main(*sys.argv[1:6])
