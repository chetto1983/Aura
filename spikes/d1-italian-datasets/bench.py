"""d1 zero-shot on the two public Italian sets that survived inspection on 2026-11-02.

No public corpus of real Italian mail with categories was found. Of three small Italian sets:
- tanaos/synthetic-spam-detection-dataset-italian (MIT, rev 492e11c6): spam vs not, synthetic.
  Filtered: rows under 8 words, rows with a 6-character run of one letter ("SSSS..."), and rows that
  announce their own class before a colon in the first 45 characters ("Malware o link dannosi: ...").
- Noise144/ticket_classification_IT_EN (no licence stated, rev bcde65b6): IT help-desk requests,
  5 categories x 18 subcategories, mixed Italian and English. Only Italian rows are used, and the
  data are not committed -- only this script and the aggregate results.
- MARKOPOSI/italian-intent-classification-300 was dropped: all 300 rows name their own label.

The sample is fixed by seed 20261102. One request per item: the spam set asks one noul, the ticket
set asks category and subcategory as two choices in one call.
"""

import csv
import json
import random
import re
import sys
import time
import urllib.error
import urllib.request

import openpyxl

SEED = 20261102
SPAM_QUESTION = ("Is this message spam: unsolicited commercial advertising, a fraudulent scheme, phishing, "
                 "deceptive content, a malware link or adult content?")
ITALIAN = re.compile(r"\b(il|la|non|mio|mia|di|che|per|del|della|sono|ho|nostro|nostra)\b", re.I)


def clean_spam(rows: list[dict]) -> list[tuple[int, dict]]:
    return [(i, r) for i, r in enumerate(rows) if len(r["text"].split()) >= 8
            and not re.search(r"(\w)\1{5}", r["text"]) and not re.match(r"^[^:]{3,45}:", r["text"])]


def spam_items(path: str, per_class: int) -> list[dict]:
    rows = list(csv.DictReader(open(path, encoding="utf-8")))
    clean = clean_spam(rows)
    pick = random.Random(SEED)
    items = []
    for label in ("0", "1"):
        pool = [(i, r) for i, r in clean if r["labels"] == label]
        items += [{"row": i, "text": r["text"], "gold": label == "1"} for i, r in pick.sample(pool, per_class)]
    print(f"spam: {len(rows)} rows, {len(clean)} after the filter, {len(items)} sampled", file=sys.stderr)
    return items


def ticket_rows(path: str) -> list[tuple]:
    return list(openpyxl.load_workbook(path, read_only=True)["Data"].iter_rows(values_only=True))[1:]


def italian_tickets(rows: list[tuple]) -> list[tuple[int, tuple]]:
    return [(i, r) for i, r in enumerate(rows) if r[2] and ITALIAN.search(r[2])]


def ticket_items(path: str, per_sub: int) -> list[dict]:
    rows = ticket_rows(path)
    italian = italian_tickets(rows)
    pick = random.Random(SEED)
    items = []
    for sub in sorted({r[1] for _, r in italian}):
        pool = [(i, r) for i, r in italian if r[1] == sub]
        items += [{"row": i, "text": r[2], "category": r[0], "subcategory": r[1]}
                  for i, r in pick.sample(pool, min(per_sub, len(pool)))]
    print(f"tickets: {len(rows)} rows, {len(italian)} Italian, {len(items)} sampled", file=sys.stderr)
    return items


def ask(url: str, state: str, questions: dict) -> tuple[dict, float]:
    """One decision; a gateway error (the hosted route's dropped stream) is retried twice."""
    body = json.dumps({"state": state, "questions": questions}).encode()
    for attempt in range(3):
        request = urllib.request.Request(url + "/v1/systemone", data=body, headers={"Content-Type": "application/json"})
        started = time.monotonic()
        try:
            with urllib.request.urlopen(request, timeout=600) as response:
                return json.load(response)["answers"], time.monotonic() - started
        except urllib.error.HTTPError as err:
            if err.code != 502 or attempt == 2:
                raise
    raise AssertionError("unreachable")


def main(url: str, spam_csv: str, ticket_xlsx: str, out: str) -> None:
    tickets = ticket_items(ticket_xlsx, 10)
    categories = sorted({t["category"] for t in tickets})
    subcategories = sorted({t["subcategory"] for t in tickets})
    with open(out, "w", encoding="utf-8") as sink:
        for item in spam_items(spam_csv, 100):
            answers, seconds = ask(url, item["text"], {"spam": {"type": "noul", "instructions": SPAM_QUESTION}})
            sink.write(json.dumps({"set": "spam", "row": item["row"], "gold": item["gold"], "p": answers["spam"]["noul"],
                                   "seconds": round(seconds, 3)}) + "\n")
        for item in tickets:
            answers, seconds = ask(url, item["text"], {
                "category": {"type": "choice", "instructions": "Which IT support category does this help-desk request belong to?",
                             "criteria": {c: c for c in categories}},
                "subcategory": {"type": "choice", "instructions": "Which component is this help-desk request about?",
                                "criteria": {s: s for s in subcategories}}})
            sink.write(json.dumps({"set": "tickets", "row": item["row"],
                                   "category_probs": answers["category"].get("probabilities"),
                                   "subcategory_probs": answers["subcategory"].get("probabilities"),
                                   "gold_category": item["category"], "gold_subcategory": item["subcategory"],
                                   "category": answers["category"]["choice"],
                                   "category_p": answers["category"].get("confidence"),
                                   "subcategory": answers["subcategory"]["choice"],
                                   "subcategory_p": answers["subcategory"].get("confidence"),
                                   "seconds": round(seconds, 3)}) + "\n")


if __name__ == "__main__":
    main(*sys.argv[1:5])
