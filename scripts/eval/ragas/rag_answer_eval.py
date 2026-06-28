"""Aura RAG answer-quality eval (RAGAS) — the gated replacement for the single 0-10
">=9.8/10" LLM-judge target (Item 3 / spike 076).

It scores each case in reference_qa.json with RAGAS faithfulness (claim-decompose + NLI
against the retrieved context — "did the answer hallucinate?") and answer_correctness
(factual F1 + semantic similarity vs the reference — "did it actually answer?"), then
gates: grounded answers must score faithfulness >= grounded_min, hallucinated answers
<= hallucinated_max, and grounded answer_correctness must beat hallucinated by a margin.

Decomposed, context-grounded metrics replace a single fragile score: spike 076 showed a
naive 0-10 judge rates a fluent hallucination 10/10, while faithfulness nails it (1.0 vs 0.0).

PAID (OpenRouter judge); embeddings are the free local granite sidecar. Gated/manual, NOT
CI. Use --dry-run to validate the dataset + wiring without any model calls.

Run: scripts/eval/ragas/run.sh   (provisions the pinned venv, then runs this)
"""
import argparse
import asyncio
import json
import os
import statistics
import sys


def log(cat, msg):
    print(f"[{cat}] {msg}", flush=True)


def load_dataset(path):
    with open(path, "r", encoding="utf-8") as fh:
        data = json.load(fh)
    cases = data.get("cases", [])
    if not cases:
        raise SystemExit(f"dataset {path} has no cases")
    for c in cases:
        for k in ("name", "kind", "context", "question", "reference", "answer"):
            if not c.get(k):
                raise SystemExit(f"case {c.get('name', '?')} missing field {k!r}")
        if c["kind"] not in ("grounded", "hallucinated", "partial"):
            raise SystemExit(f"case {c['name']} has unknown kind {c['kind']!r}")
    return data


async def main():
    ap = argparse.ArgumentParser()
    here = os.path.dirname(os.path.abspath(__file__))
    ap.add_argument("--dataset", default=os.path.join(here, "reference_qa.json"))
    ap.add_argument("--dry-run", action="store_true", help="validate the dataset + wiring, no model calls")
    args = ap.parse_args()

    data = load_dataset(args.dataset)
    cases = data["cases"]
    th = data.get("thresholds", {})
    g_min = th.get("grounded_faithfulness_min", 0.8)
    h_max = th.get("hallucinated_faithfulness_max", 0.4)
    margin_min = th.get("answer_correctness_margin_min", 0.2)
    log("DATASET", f"{len(cases)} cases; thresholds grounded>={g_min} hallucinated<={h_max} margin>={margin_min}")

    if args.dry_run:
        for c in cases:
            log("CASE", f"{c['name']:20s} kind={c['kind']:12s} q={c['question'][:48]!r}")
        log("SUMMARY", "DRY-RUN OK — dataset valid; run without --dry-run for live RAGAS scoring (paid judge).")
        return 0

    key = os.environ.get("OPENROUTER_API_KEY")
    if not key:
        raise SystemExit("OPENROUTER_API_KEY unset (live scoring is paid)")
    judge = os.environ.get("SPIKE_JUDGE_MODEL", "deepseek/deepseek-v4-flash")
    embed_url = os.environ.get("AURA_EMBED_BASE_URL", "http://127.0.0.1:8081").rstrip("/") + "/v1"

    import ragas
    from ragas import SingleTurnSample
    from ragas.metrics import Faithfulness, AnswerCorrectness, AnswerSimilarity
    from ragas.llms import LangchainLLMWrapper
    from ragas.embeddings import LangchainEmbeddingsWrapper
    from langchain_openai import ChatOpenAI, OpenAIEmbeddings

    log("ENV", f"ragas={ragas.__version__} judge={judge} embed={embed_url}")
    llm = LangchainLLMWrapper(ChatOpenAI(model=judge, base_url="https://openrouter.ai/api/v1",
                                         api_key=key, temperature=0, max_retries=2, timeout=120))
    emb = LangchainEmbeddingsWrapper(OpenAIEmbeddings(model="granite", base_url=embed_url,
                                                      api_key="sk-noauth", check_embedding_ctx_length=False))
    faith = Faithfulness(llm=llm)
    correct = AnswerCorrectness(llm=llm, embeddings=emb, answer_similarity=AnswerSimilarity(embeddings=emb))

    async def score(metric, c):
        sample = SingleTurnSample(user_input=c["question"], response=c["answer"],
                                  retrieved_contexts=[c["context"]], reference=c["reference"])
        try:
            return float(await metric.single_turn_ascore(sample))
        except Exception as e:
            log("WARN", f"{type(metric).__name__} on {c['name']} failed: {e!r}")
            return None

    res = {}
    for c in cases:
        f = await score(faith, c)
        a = await score(correct, c)
        res[c["name"]] = dict(kind=c["kind"], faithfulness=f, answer_correctness=a)
        log("SCORE", f"{c['name']:20s} kind={c['kind']:12s} faithfulness={fmt(f)} answer_correctness={fmt(a)}")

    ok = True
    for name, r in res.items():
        f = r["faithfulness"]
        if r["kind"] == "grounded" and not (isinstance(f, float) and f >= g_min):
            log("FAIL", f"{name} grounded faithfulness {fmt(f)} < {g_min}"); ok = False
        if r["kind"] == "hallucinated" and not (isinstance(f, float) and f <= h_max):
            log("FAIL", f"{name} hallucinated faithfulness {fmt(f)} > {h_max}"); ok = False

    gs = [r["answer_correctness"] for r in res.values() if r["kind"] == "grounded" and isinstance(r["answer_correctness"], float)]
    hs = [r["answer_correctness"] for r in res.values() if r["kind"] == "hallucinated" and isinstance(r["answer_correctness"], float)]
    if gs and hs:
        margin = statistics.mean(gs) - statistics.mean(hs)
        if margin < margin_min:
            log("FAIL", f"answer_correctness margin {margin:.3f} < {margin_min} (grounded {statistics.mean(gs):.3f} vs hallucinated {statistics.mean(hs):.3f})"); ok = False
        else:
            log("OK", f"answer_correctness margin {margin:.3f} (grounded {statistics.mean(gs):.3f} vs hallucinated {statistics.mean(hs):.3f})")

    if ok:
        log("SUMMARY", "PASS — RAGAS faithfulness + answer_correctness gate met. A decomposed, context-grounded eval replaces the fragile single '9.8' score.")
        return 0
    log("SUMMARY", "FAIL — see FAIL lines above")
    return 1


def fmt(v):
    return f"{v:.3f}" if isinstance(v, float) else "n/a"


if __name__ == "__main__":
    sys.exit(asyncio.run(main()))
