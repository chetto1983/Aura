"""Step 3: compile each localized passage into claim-evidence pairs (paper §4, the measured unit).

The extractor is a local generative model behind llama-server's OpenAI-compatible endpoint.
A pair survives only if its evidence is a verbatim span of the passage -- the paper's own rule
("claims whose spans cannot be located in the source are discarded"). Matching is exact first,
then whitespace-collapsed, and the offsets recorded are the passage's own.
"""

import json
import re
import sys
import time
import urllib.request

PROMPT = """Extract every atomic fact stated in the passage below.

Rules:
- "claim": one self-contained sentence written in the SAME language as the passage (an Italian
  passage gets Italian claims). Name the subject explicitly (document, machine, customer, code,
  section) so the claim is understood without the passage.
- "evidence": the shortest span copied EXACTLY, character for character, from the passage that
  states the claim. Never paraphrase the evidence.
- Do not infer, compute, total or summarise anything the passage does not state.
- Cover every fact: numbers, codes, names, dates, units, every table row.

Passage (from file {doc}):
<<<
{text}
>>>"""

SCHEMA = {"type": "object", "properties": {"claims": {"type": "array", "items": {
    "type": "object", "properties": {"claim": {"type": "string"}, "evidence": {"type": "string"}},
    "required": ["claim", "evidence"]}}}, "required": ["claims"]}


def extract(url: str, doc: str, text: str) -> list[dict]:
    body = {"messages": [{"role": "user", "content": PROMPT.format(doc=doc, text=text)}],
            "temperature": 0, "max_tokens": 4096,
            "response_format": {"type": "json_schema", "json_schema": {"name": "claims", "schema": SCHEMA}}}
    request = urllib.request.Request(url + "/v1/chat/completions", data=json.dumps(body).encode(),
                                     headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(request, timeout=3600) as response:
        answer = json.load(response)
    choice = answer["choices"][0]
    if choice["finish_reason"] != "stop":
        raise ValueError(f"finish_reason {choice['finish_reason']}")
    return json.loads(choice["message"]["content"])["claims"], answer["usage"]


SEGMENT_CHARS = 2400  # ~600 tokens in: one request's claims then fit max_tokens with room to spare


def segments(text: str) -> list[str]:
    """The passage cut at line boundaries, so no request's output is truncated by max_tokens."""
    out, current = [], ""
    for line in text.splitlines(keepends=True):
        if current and len(current) + len(line) > SEGMENT_CHARS:
            out.append(current)
            current = ""
        current += line
    return out + [current] if current.strip() else out


def locate(text: str, evidence: str) -> tuple[int, int] | None:
    at = text.find(evidence)
    if at >= 0:
        return at, at + len(evidence)
    words = evidence.split()
    if not words:
        return None
    found = re.search(r"\s+".join(map(re.escape, words)), text)
    return (found.start(), found.end()) if found else None


def main(url: str, passages_path: str, cases_path: str, out: str) -> None:
    passages = {(p["doc"], p["ordinal"]): p for p in map(json.loads, open(passages_path, encoding="utf-8"))}
    wanted = sorted({(c["doc"], c["ordinal"]) for c in json.load(open(cases_path))["cases"]})
    with open(out, "w", encoding="utf-8") as sink:
        for key in wanted:
            passage = passages[key]
            started = time.monotonic()
            claims, usage, failures = [], {"completion_tokens": 0, "prompt_tokens": 0}, []
            for segment in segments(passage["text"]):
                try:
                    found, used = extract(url, key[0], segment)
                except (ValueError, KeyError) as exc:
                    failures.append(repr(exc)[:200])
                    continue
                claims += found
                usage = {k: usage[k] + used[k] for k in usage}
            kept, dropped = [], []
            for c in claims:
                span = locate(passage["text"], c["evidence"])
                (kept if span else dropped).append({**c, "span": span})
            row = {"doc": key[0], "ordinal": key[1], "kept": kept, "dropped": dropped, "usage": usage, "failures": failures,
                   "seconds": round(time.monotonic() - started, 1)}
            sink.write(json.dumps(row, ensure_ascii=False) + "\n")
            sink.flush()
            print(f"{key}: {len(kept)} kept, {len(dropped)} dropped, {len(failures)} failed, {row['seconds']}s",
                  file=sys.stderr, flush=True)


if __name__ == "__main__":
    main(*sys.argv[1:5])
