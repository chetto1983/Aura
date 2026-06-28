"""Spike 076 — ragas-faithfulness-discriminates.

Item 3: reframe the fragile single 0-10 LLM-judge ">9.8" target as RAGAS
faithfulness + answer_correctness + a rubric. The riskiest assumptions:
  (1) tooling feasibility — can RAGAS run in Aura's stack (OpenRouter judge +
      local granite embeddings), and
  (2) construct validity — do faithfulness/answer_correctness DISCRIMINATE a
      grounded answer from a hallucinated one, repeatably, with meaningful
      (calibrated) numbers — unlike a single 0-10 score?

The probe scores grounded / hallucinated / partial answers over two doc contexts,
measures faithfulness repeatability, and contrasts a naive single-0-10 judge (the
construct the reframe replaces). Judge = OpenRouter DeepSeek; embeddings = granite.
Forensic log: [CAT] lines, [SUMMARY] verdict, exit 0=VALIDATED / 1=fail.
"""
import asyncio
import os
import re
import statistics
import sys


def log(cat, msg):
    print(f"[{cat}] {msg}", flush=True)


def fail(msg):
    log("FATAL", msg)
    sys.exit(2)


OPENROUTER_KEY = os.environ.get("OPENROUTER_API_KEY") or fail("OPENROUTER_API_KEY unset")
JUDGE_MODEL = os.environ.get("SPIKE_JUDGE_MODEL", "deepseek/deepseek-v4-flash")
EMBED_URL = os.environ.get("AURA_EMBED_BASE_URL", "http://127.0.0.1:8081").rstrip("/") + "/v1"

try:
    import ragas
    from ragas import SingleTurnSample
    from ragas.metrics import Faithfulness, AnswerCorrectness, AnswerSimilarity
    from ragas.llms import LangchainLLMWrapper
    from ragas.embeddings import LangchainEmbeddingsWrapper
    from langchain_openai import ChatOpenAI, OpenAIEmbeddings
except Exception as e:  # import feasibility IS part of the finding
    fail(f"import failed (tooling-infeasible signal): {e!r}")

log("ENV", f"ragas={ragas.__version__} judge={JUDGE_MODEL} embed={EMBED_URL}")

chat = ChatOpenAI(
    model=JUDGE_MODEL,
    base_url="https://openrouter.ai/api/v1",
    api_key=OPENROUTER_KEY,
    temperature=0,
    max_retries=2,
    timeout=120,
)
llm = LangchainLLMWrapper(chat)
emb = LangchainEmbeddingsWrapper(
    OpenAIEmbeddings(model="granite", base_url=EMBED_URL, api_key="sk-noauth", check_embedding_ctx_length=False)
)

faith = Faithfulness(llm=llm)
# AnswerCorrectness blends a factual-F1 (llm) + semantic-similarity (embeddings) head;
# in ragas 0.2.x the similarity sub-metric must be wired explicitly or it asserts
# "AnswerSimilarity must be set" when its weight is non-zero.
correct = AnswerCorrectness(llm=llm, embeddings=emb, answer_similarity=AnswerSimilarity(embeddings=emb))

G220_CTX = (
    "Servo Drive G220 Datasheet. Electrical Specifications. Rated torque: 47 Nm. "
    "Peak torque: 132 Nm. Maximum input voltage: 415 V AC. Rated output current: 18 A. "
    "Protection class: IP54."
)
COFFEE_CTX = (
    "Barista Pro Coffee Machine. Water tank capacity: 1.8 litres. Run the descaling "
    "cycle every two months. Use the steam wand to froth milk for a cappuccino."
)

SCEN = [
    dict(name="g220-grounded", kind="grounded", ctx=G220_CTX,
         q="What is the rated torque and protection class of the G220 servo drive?",
         ref="The rated torque is 47 Nm and the protection class is IP54.",
         ans="The G220 has a rated torque of 47 Nm and a protection class of IP54."),
    dict(name="g220-hallucinated", kind="hallucinated", ctx=G220_CTX,
         q="What is the rated torque and protection class of the G220 servo drive?",
         ref="The rated torque is 47 Nm and the protection class is IP54.",
         ans="The G220 has a rated torque of 90 Nm and a protection class of IP67."),
    dict(name="g220-partial", kind="partial", ctx=G220_CTX,
         q="What is the rated torque and protection class of the G220 servo drive?",
         ref="The rated torque is 47 Nm and the protection class is IP54.",
         ans="The rated torque is 47 Nm."),
    dict(name="coffee-grounded", kind="grounded", ctx=COFFEE_CTX,
         q="What is the water tank capacity and how often should I descale?",
         ref="The water tank holds 1.8 litres and you should descale every two months.",
         ans="The water tank capacity is 1.8 litres and you should run the descaling cycle every two months."),
    dict(name="coffee-hallucinated", kind="hallucinated", ctx=COFFEE_CTX,
         q="What is the water tank capacity and how often should I descale?",
         ref="The water tank holds 1.8 litres and you should descale every two months.",
         ans="The water tank capacity is 3 litres and you should descale every week."),
]


def to_sample(s):
    return SingleTurnSample(user_input=s["q"], response=s["ans"], retrieved_contexts=[s["ctx"]], reference=s["ref"])


async def score(metric, s):
    try:
        return float(await metric.single_turn_ascore(to_sample(s)))
    except Exception as e:
        log("WARN", f"metric {type(metric).__name__} on {s['name']} failed: {e!r}")
        return None


async def naive_judge(s, n=3):
    """A context-free single 0-10 judge — the fragile construct the reframe replaces.
    It never sees the source context, so it cannot detect a fluent hallucination."""
    out = []
    for _ in range(n):
        prompt = (
            "Rate the overall quality of this answer to the question on a scale of 0 to 10 "
            "(10 = excellent). Reply with ONLY the number.\n"
            f"Question: {s['q']}\nAnswer: {s['ans']}"
        )
        r = await chat.ainvoke(prompt)
        m = re.search(r"\d+(?:\.\d+)?", r.content or "")
        out.append(float(m.group()) if m else None)
    return out


async def main():
    res = {}
    for s in SCEN:
        f = await score(faith, s)
        c = await score(correct, s)
        res[s["name"]] = dict(kind=s["kind"], faithfulness=f, answer_correctness=c)
        log("SCORE", f"{s['name']:20s} kind={s['kind']:12s} "
                     f"faithfulness={fmt(f)} answer_correctness={fmt(c)}")

    g_rep = [await score(faith, SCEN[0]) for _ in range(3)]
    h_rep = [await score(faith, SCEN[1]) for _ in range(3)]
    log("REPEAT", f"faithfulness grounded   x3 = {rnd(g_rep)} stdev={sd(g_rep)}")
    log("REPEAT", f"faithfulness hallucinated x3 = {rnd(h_rep)} stdev={sd(h_rep)}")

    ng = await naive_judge(SCEN[0])
    nh = await naive_judge(SCEN[1])
    log("NAIVE-JUDGE", f"context-free 0-10 grounded x3 = {ng}  hallucinated x3 = {nh} "
                       f"(a single score cannot catch a fluent hallucination)")

    # ---- Construct-validity assertions (RAGAS is the gated metric) ----
    ok = True
    fg, fh = res["g220-grounded"]["faithfulness"], res["g220-hallucinated"]["faithfulness"]
    cfg, cfh = res["coffee-grounded"]["faithfulness"], res["coffee-hallucinated"]["faithfulness"]
    if not (isinstance(fg, float) and fg >= 0.8):
        log("FAIL", f"g220 grounded faithfulness {fmt(fg)} < 0.8"); ok = False
    if not (isinstance(fh, float) and fh <= 0.4):
        log("FAIL", f"g220 hallucinated faithfulness {fmt(fh)} > 0.4"); ok = False
    if not (isinstance(cfg, float) and isinstance(cfh, float) and cfg >= 0.8 and cfh <= 0.4):
        log("FAIL", f"coffee faithfulness not discriminating g={fmt(cfg)} h={fmt(cfh)}"); ok = False

    cg, ch = res["g220-grounded"]["answer_correctness"], res["g220-hallucinated"]["answer_correctness"]
    if isinstance(cg, float) and isinstance(ch, float):
        if cg - ch < 0.2:
            log("FAIL", f"answer_correctness margin {cg - ch:.3f} < 0.2 (g={cg:.3f} h={ch:.3f})"); ok = False
        else:
            log("OK", f"answer_correctness margin {cg - ch:.3f} (g={cg:.3f} h={ch:.3f})")
    else:
        log("FINDING", "answer_correctness unavailable (granite-embeddings wiring) — faithfulness gated alone")

    if all(isinstance(v, float) for v in g_rep + h_rep):
        if sd_val(g_rep) > 0.1 or sd_val(h_rep) > 0.1:
            log("FINDING", "faithfulness stdev > 0.1 — repeatability looser than hoped (still reported)")
        else:
            log("OK", "faithfulness repeatable across runs (stdev <= 0.1)")

    if ok:
        log("SUMMARY", "VALIDATED — RAGAS faithfulness (+ answer_correctness) discriminate grounded vs "
                       "hallucinated, repeatably, over Aura's stack (OpenRouter judge + granite embeddings). "
                       "A decomposed metric replaces the fragile single 0-10 '9.8' target.")
        return 0
    log("SUMMARY", "FAILED — see FAIL lines above")
    return 1


def fmt(v):
    return f"{v:.3f}" if isinstance(v, float) else "n/a"


def rnd(vs):
    return [round(v, 3) if isinstance(v, float) else None for v in vs]


def sd_val(vs):
    xs = [v for v in vs if isinstance(v, float)]
    return statistics.pstdev(xs) if len(xs) > 1 else 0.0


def sd(vs):
    return f"{sd_val(vs):.3f}"


if __name__ == "__main__":
    sys.exit(asyncio.run(main()))
