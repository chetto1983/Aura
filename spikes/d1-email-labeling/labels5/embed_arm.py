"""CLM's shape with the embedding model Aura already runs, on G1's labels and mails.

CLM encodes the state and each option apart, and picks the option nearest the state, so the options' vectors
are computed once and reused. EmbeddingGemma (the local sidecar) does the same without CLM's trained heads:
each label's description is embedded once, each mail once, and a softmax over the cosines at a temperature T
gives the distribution. T and the threshold are chosen on the 200 mails by g1's rule, right labels minus 5 x
wrong ones, before the held-out sets are scored. Prompts from
https://ai.google.dev/gemma/docs/embeddinggemma/model_card ("Prompt instructions").
"""

import argparse
import json
import math
import statistics
import time
from pathlib import Path

import g1

PROMPTS = {
    "retrieval": ("task: search result | query: ", "title: {name} | text: {description}"),
    "classification": ("task: classification | query: ", "task: classification | query: {name}: {description}"),
}
TEMPERATURES = (0.01, 0.02, 0.03, 0.05, 0.1)
COST = 5


def embed(base_url, text):
    return g1.request_json(base_url + "/v1/embeddings", {"input": [text]})["data"][0]["embedding"]


def cosines(base_url, cases, prompt):
    query, document = PROMPTS[prompt]
    options = {name: embed(base_url, document.format(name=name, description=text)) for name, text in g1.ITALIAN.items()}
    rows = []
    for case in cases:
        t0 = time.perf_counter()
        vector = embed(base_url, query + g1.state_for(case, None))
        latency = (time.perf_counter() - t0) * 1000
        # --embd-normalize 2 on the sidecar: the dot product is the cosine.
        cos = {name: sum(a * b for a, b in zip(vector, option)) for name, option in options.items()}
        rows.append({"id": case["id"], "gold": case["gold5"], "cosines": cos, "latency_ms": latency})
    return rows


def at_temperature(rows, temperature):
    out = []
    for row in rows:
        top = max(row["cosines"].values())
        weights = {k: math.exp((v - top) / temperature) for k, v in row["cosines"].items()}
        total = sum(weights.values())
        probs = {k: w / total for k, w in weights.items()}
        predicted = max(probs, key=probs.get)
        out.append({**row, "predicted": predicted, "top": probs[predicted], "probabilities": probs})
    return out


def choose(rows):
    best = None
    for temperature in TEMPERATURES:
        scored = at_temperature(rows, temperature)
        for threshold in g1.THRESHOLDS:
            cell = g1.at_threshold(scored, threshold)
            value = cell["right"] - COST * (cell["applied"] - cell["right"])
            if best is None or value > best[0]:
                best = (value, temperature, threshold)
    return best[1], best[2]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base-url", default="http://127.0.0.1:8081")
    parser.add_argument("--out", type=Path, required=True)
    args = parser.parse_args()
    args.out.mkdir(parents=True, exist_ok=True)
    train, held_out = g1.load_cases(g1.TRAIN), g1.load_cases(g1.HELD_OUT)
    summary = {"models": g1.request_json(args.base_url + "/v1/models"), "cost": COST, "prompts": {}}
    for prompt in PROMPTS:
        sets = {"200": cosines(args.base_url, train, prompt), "held-out": cosines(args.base_url, held_out, prompt)}
        temperature, threshold = choose(sets["200"])
        result = {"temperature": temperature, "threshold": threshold}
        for name, rows in sets.items():
            scored = at_temperature(rows, temperature)
            (args.out / f"{prompt}.{name}.predictions.jsonl").write_text(
                "".join(json.dumps(r, ensure_ascii=False) + "\n" for r in scored), encoding="utf-8")
            result[name] = {
                "argmax_right": sum(r["gold"] == r["predicted"] for r in scored),
                "total": len(scored),
                "at_chosen": g1.at_threshold(scored, threshold),
                "latency_ms_p50": round(statistics.median(r["latency_ms"] for r in rows), 1),
            }
        summary["prompts"][prompt] = result
        print(prompt, json.dumps(result, ensure_ascii=False), flush=True)
    (args.out / "summary.json").write_text(json.dumps(summary, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


if __name__ == "__main__":
    main()
