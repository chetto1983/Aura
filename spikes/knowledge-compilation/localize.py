"""Step 2: one case per question -- the gold passage a harness would hand the model.

A case exists when the gold answer is stated in a single passage of a gold document (oracle.carries)
and, when the question names a code (A006, RIC-10959), that passage names it too. Questions the
oracle cannot judge (oracle.weak) or whose answer is computed across passages are listed as
excluded with the reason, never dropped silently.
"""

import collections
import json
import pathlib
import sys

from oracle import carries, identifiers, tokens, weak

# Answers computed over many rows, whose tokens the oracle finds in the passage by coincidence:
# read by hand on 2026-11-02, "376" is "Riga 376" and not the count of items under minimum stock,
# and "Verdi Paolo" is a name in the rota, not the argmax of its night shifts.
COMPUTED = {"parts-below-min": "count over 5,000 rows; 376 matches a row number",
            "rota-nights": "argmax over the rota matrix; the name is present regardless"}

FIXTURES = pathlib.Path(__file__).resolve().parents[2] / "scripts/fixtures/document_retrieval_eval"


def main(passages_path: str, out: str) -> None:
    by_doc = collections.defaultdict(list)
    for line in open(passages_path, encoding="utf-8"):
        row = json.loads(line)
        by_doc[row["doc"]].append(row)
    cases, excluded = [], []
    for name in ("qrels.json", "complex_qrels.json"):
        for q in json.load(open(FIXTURES / name, encoding="utf-8"))["questions"]:
            if q["qid"] in COMPUTED:
                excluded.append((q["qid"], COMPUTED[q["qid"]]))
                continue
            if weak(q["answer"]):
                excluded.append((q["qid"], "weak oracle"))
                continue
            named = identifiers(q["query"])
            found = [p for d in q["gold_docs"] for p in by_doc.get(d, [])
                     if carries(p["text"], q["answer"]) and named <= tokens(p["text"]) | named_in(p["text"], named)]
            if not found:
                excluded.append((q["qid"], "answer not stated in one passage"))
                continue
            hit = min(found, key=lambda p: p["tokens"])
            cases.append({"qid": q["qid"], "category": q["category"], "query": q["query"], "answer": q["answer"], "gold_docs": q["gold_docs"],
                          "doc": hit["doc"], "ordinal": hit["ordinal"], "tokens": hit["tokens"]})
    with open(out, "w", encoding="utf-8") as sink:
        json.dump({"cases": cases, "excluded": excluded}, sink, ensure_ascii=False, indent=1)
    print(f"{len(cases)} cases, {len(excluded)} excluded: {excluded}", file=sys.stderr)


def named_in(text: str, named: set[str]) -> set[str]:
    folded = text.lower()
    return {code for code in named if code in folded}


if __name__ == "__main__":
    main(sys.argv[1], sys.argv[2])
