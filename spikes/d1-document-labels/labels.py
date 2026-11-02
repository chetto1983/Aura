"""d1-2B labelling Aura's document cards: type, counterparty and tags, against the operator's rules.

The state is what Aura holds for a document after ingest: aura-filecard's card plus the header pairs
filecard keeps for a workbook (services/ingest/app.py, IndexedDocument.card / header_pairs). The
lists are the operator's, names only -- docfields keeps no descriptions -- so the criteria are the
names themselves.

Two conditions, because the fixtures' file names give the type away: `named` as ingested, and
`blind`, every occurrence of the file's stem replaced by "documento". Real uploads are often
scan_0012.pdf; the blind condition is the closer one to those.

The baseline is the rule an operator writes first: each type, counterparty and tag matches its own
name as an exact, case-insensitive word sequence (docfields/rules Exact, Paperless-ngx's behaviour).
Several matches: the longest name wins; none: no value. Beside it, what Aura sets today with no
rule at all: the extractor's counterparty, read from a workbook's "Cliente"/"Fornitore" header pair
(internal/docfields/pass.go, counterpartyKeys); type and tags stay empty.
"""

import json
import pathlib
import re
import subprocess
import sys
import time
import urllib.request

HERE = pathlib.Path(__file__).resolve().parent
FIXTURES = HERE.parents[1] / "scripts/fixtures/document_retrieval_eval"
NONE = "nessuno"


def gold() -> dict:
    office = json.load(open(FIXTURES / "document_filter_questions.json", encoding="utf-8"))["library"]
    complex_ = json.load(open(HERE / "gold_complex.json", encoding="utf-8"))["library"]
    return {**{("corpus", k): v for k, v in office.items()}, **{("complex_corpus", k): v for k, v in complex_.items()}}


def card(corpus: str, name: str) -> str:
    done = subprocess.run(["aura-filecard", "-json", "-name", name, str(FIXTURES / corpus / name)],
                          check=True, capture_output=True)
    rendered = json.loads(done.stdout)
    header = "\n".join(f"{p['key']}: {p['value']}" for p in rendered.get("header") or [])
    return rendered["card"] + ("\n" + header if header else "")


def extractor_counterparty(corpus: str, name: str) -> str:
    done = subprocess.run(["aura-filecard", "-json", "-name", name, str(FIXTURES / corpus / name)],
                          check=True, capture_output=True)
    for pair in json.loads(done.stdout).get("header") or []:
        if pair["key"].strip().lower() in ("cliente", "fornitore") and pair["value"].strip():
            return pair["value"].strip()
    return NONE


def blind(text: str, name: str) -> str:
    stem = pathlib.PurePath(name).stem
    return text.replace(stem, "documento")


def words(text: str) -> list[str]:
    return re.findall(r"\w+", text.lower())


def rule(text: str, names: list[str]) -> str:
    seq = words(text)
    hits = [n for n in names
            if any(seq[i:i + len(words(n))] == words(n) for i in range(len(seq) - len(words(n)) + 1))]
    return max(hits, key=len) if hits else NONE


def decide(url: str, state: str, types: list[str], parties: list[str], tags: list[str]) -> tuple[dict, float, int]:
    questions = {
        "type": {"type": "choice", "instructions": "Which document type is this document?",
                 "criteria": {t: t for t in types}},
        "counterparty": {"type": "choice",
                         "instructions": "Which counterparty (customer or supplier) is this document addressed to or "
                                         f"issued by? Answer {NONE} when it concerns none or several of them.",
                         "criteria": {p: p for p in parties + [NONE]}},
    }
    for tag in tags:
        questions["tag:" + tag] = {"type": "noul", "instructions": f"Should this document carry the tag '{tag}'?"}
    body = {"state": state, "questions": questions}
    request = urllib.request.Request(url + "/v1/systemone", data=json.dumps(body).encode(),
                                     headers={"Content-Type": "application/json"})
    started = time.monotonic()
    with urllib.request.urlopen(request, timeout=600) as response:
        got = json.load(response)
    return got["answers"], time.monotonic() - started, got["usage"]["input_tokens"]


def main(url: str, out: str) -> None:
    library = gold()
    types = sorted({v["type"] for v in library.values()})
    parties = sorted({v["counterparty"] for v in library.values() if v.get("counterparty")})
    tags = sorted({t for v in library.values() for t in v.get("tags", [])})
    with open(out, "w", encoding="utf-8") as sink:
        for (corpus, name), want in sorted(library.items()):
            text = card(corpus, name)
            for condition, state in (("named", text), ("blind", blind(text, name))):
                answers, seconds, tokens = decide(url, state, types, parties, tags)
                row = {"doc": name, "corpus": corpus, "condition": condition,
                       "gold_type": want["type"], "gold_counterparty": want.get("counterparty", NONE),
                       "gold_tags": want.get("tags", []),
                       "d1_type": answers["type"]["choice"], "d1_counterparty": answers["counterparty"]["choice"],
                       "d1_tags": {t: answers["tag:" + t]["noul"] for t in tags},
                       "rule_type": rule(state, types), "rule_counterparty": rule(state, parties),
                       "rule_tags": [t for t in tags if rule(state, [t]) != NONE],
                       "extractor_counterparty": extractor_counterparty(corpus, name),
                       "seconds": round(seconds, 2), "input_tokens": tokens, "state_chars": len(state)}
                sink.write(json.dumps(row, ensure_ascii=False) + "\n")
                sink.flush()
                print(f"{condition} {name}: type {row['d1_type']} / rule {row['rule_type']} / gold {row['gold_type']}",
                      file=sys.stderr, flush=True)


if __name__ == "__main__":
    main(*sys.argv[1:3])
