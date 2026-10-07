"""Stage 1 of the CLM decision spike: zero-shot CLM-8B on Aura's own evaluation sets.

    modal run spikes/clm-decision/modal_clm.py

The variants and metrics are pre-registered in README.md; change them there first.
"""
import contextlib
import json
import math
import statistics
import subprocess
import time
import urllib.request
from pathlib import Path

import modal

HERE = Path(__file__).parent
EMB_URL = "http://127.0.0.1:8090/v1/embeddings"

app = modal.App("aura-clm-spike")
cache = modal.Volume.from_name("aura-clm-cache", create_if_missing=True)
image = (
    modal.Image.debian_slim(python_version="3.12")
    .pip_install("vllm==0.31.0", "contrastive-lm==0.1.0", "huggingface_hub>=0.20")
    .env({"HF_HOME": "/cache/hf", "HF_XET_HIGH_PERFORMANCE": "1", "CLM_CKPT_DIR": "/cache/clm"})
    .add_local_dir(HERE / "data", "/data")
)

EFFORT_QUESTION = "Classify the latest user request into one reasoning tier for the next assistant turn."
ROUTER_CRITERIA = {
    "none": "greeting/simple stable fact/direct short transform",
    "low": "current web/news/weather/prices/schedules/lookups or small tool use",
    "high": "coding/debugging/design/proofs/scraping/multi-step analysis",
}
TOOL_QUESTION = "Which tool should handle this request?"
# trivialGreetings in internal/agent/prompt/reasoning_classifier.go, 2026-10-07.
GREETINGS = {
    "ciao", "ciao ciao", "salve", "buongiorno", "buonasera", "buonanotte", "ehi", "hey", "grazie",
    "grazie mille", "ti ringrazio", "ti ringrazio molto", "ok", "okay", "perfetto", "ok perfetto",
    "ok grazie", "va bene", "capito", "a presto", "a dopo", "thanks", "thank you", "a presto!",
}


def normalize_greeting(s):
    return s.strip().lower().rstrip(" .!?,;:")


def choice(engine, state, criteria, question, model):
    q = {"type": "choice", "instructions": question, "criteria": criteria}
    started = time.perf_counter()
    answer = engine.answer(state, {"q": q}, model=model)["answers"]["q"]
    return answer, (time.perf_counter() - started) * 1000


def sanity(engine):
    state = "Customer: my invoice was charged twice and nobody answers the phone!"
    questions = {
        "urgency": {"type": "noul", "instructions": "Is this urgent?"},
        "department": {"type": "choice", "instructions": "Which team should handle this?",
                       "criteria": {"billing": "Charges, invoices, refunds", "technical": "Bugs and outages"}},
        "frustration": {"type": "score", "instructions": "How frustrated is the customer?",
                        "criteria": ["Calm", "Frustrated", "Very angry"]},
    }
    a = engine.answer(state, questions)["answers"]
    got = {"urgency": a["urgency"]["noul"], "department": a["department"]["probabilities"]["billing"],
           "frustration": a["frustration"]["score"]}
    expected = {"urgency": 0.41022, "department": 0.93878, "frustration": 1.98386}
    return {"got": got, "expected": expected, "max_abs_delta": max(abs(got[k] - expected[k]) for k in expected)}


def score_effort(cases, preds):
    vm_start = next(i for i, c in enumerate(cases) if c["prompt"] == "chi sei?")
    correct = nvr = vm = 0
    confusion, misses = {}, []
    for i, (c, got) in enumerate(zip(cases, preds)):
        ok = got in c["accept"]
        correct += ok
        vm += ok and i >= vm_start
        strict = len(c["accept"]) == 1
        if (not strict and ok) or (strict and (got == "none") == (c["accept"][0] == "none")):
            nvr += 1
        if not ok:
            key = f'{c["accept"][0]}->{got}'
            confusion[key] = confusion.get(key, 0) + 1
            misses.append({"prompt": c["prompt"], "accept": c["accept"], "pred": got})
    return {"accuracy": f"{correct}/{len(cases)}", "none_vs_rest": f"{nvr}/{len(cases)}",
            "vm_traffic": f"{vm}/{len(cases) - vm_start}", "hard_to_none": confusion.get("high->none", 0),
            "confusion": confusion, "misses": misses}


def effort(engine):
    cases = json.loads(Path("/data/effort_gate.json").read_text())
    tiers = json.loads(Path("/data/tiers.json").read_text())
    defs = {t: tiers["defs"][t] for t in tiers["order"]}
    seed_options, seed_tier = {}, {}
    for t in tiers["order"]:
        for j, text in enumerate([tiers["defs"][t]] + tiers["seeds"][t]):
            seed_options[f"{t}:{j}"], seed_tier[f"{t}:{j}"] = text, t

    def by_criteria(criteria, model):
        return criteria_run(engine, cases, criteria, EFFORT_QUESTION, model)

    def by_seeds():
        preds, lat = [], []
        for c in cases:
            a, ms = choice(engine, c["prompt"], seed_options, EFFORT_QUESTION, "clm-latest")
            per_tier = {}
            for key, p in a["probabilities"].items():
                per_tier.setdefault(seed_tier[key], []).append(math.log(max(p, 1e-30)))
            preds.append(max(per_tier, key=lambda t: statistics.mean(sorted(per_tier[t], reverse=True)[:3])))
            lat.append(ms)
        return preds, None, lat

    runs = {"E1": by_criteria(defs, "clm-latest"), "E2": by_criteria(ROUTER_CRITERIA, "clm-latest"),
            "E3": by_seeds(), "E1-raw": by_criteria(defs, "clm-raw"), "E2-raw": by_criteria(ROUTER_CRITERIA, "clm-raw")}
    return {name: effort_report(cases, *run) for name, run in runs.items()}


def criteria_run(engine, cases, criteria, question, model):
    preds, conf, lat = [], [], []
    for c in cases:
        a, ms = choice(engine, c["prompt"], criteria, question, model)
        preds.append(a["choice"]); conf.append(a["confidence"]); lat.append(ms)
    return preds, conf, lat


def effort_report(cases, preds, conf, lat):
    greeted = ["none" if normalize_greeting(c["prompt"]) in GREETINGS else p for c, p in zip(cases, preds)]
    return {"plain": score_effort(cases, preds), "greeting_fast_path": score_effort(cases, greeted),
            "latency_ms_p50": statistics.median(lat[1:]), "latency_ms_max": max(lat[1:]),
            "predictions": preds, "confidence": conf}


# Stage 1b, registered in README.md before its run.
FRAMINGS = {
    "A1": ("You are Aura, a personal assistant. Before you reply to this message, decide how much you need to think.", {
        "none": "Reply right away, briefly: a greeting, a thank-you, a stable fact you already know, "
                "a small calculation or a short translation.",
        "low": "Look up current information that changes over time (weather, news, prices, opening hours, "
               "timetables, traffic, sports results) or use a tool for a small task, then reply.",
        "high": "Think it through step by step before replying: write or debug code, design a schema or system, "
                "prove something, optimise an algorithm, scrape, or analyse in several steps.",
    }),
    "A2": ("Sei Aura, un'assistente personale. Prima di rispondere a questo messaggio, decidi quanto devi ragionare.", {
        "none": "Rispondi subito e in breve: un saluto, un ringraziamento, un fatto stabile che conosci già, "
                "un piccolo calcolo o una traduzione breve.",
        "low": "Cerca un'informazione corrente che cambia nel tempo (meteo, notizie, prezzi, orari di apertura, "
               "orari dei mezzi, traffico, risultati sportivi) oppure usa uno strumento per un compito piccolo, poi rispondi.",
        "high": "Ragiona passo per passo prima di rispondere: scrivi o correggi codice, progetta uno schema o un sistema, "
                "dimostra qualcosa, ottimizza un algoritmo, fai scraping o analizza in più passaggi.",
    }),
}


def framing_effort(engine):
    cases = json.loads(Path("/data/effort_gate.json").read_text())
    return {name: effort_report(cases, *criteria_run(engine, cases, options, question, "clm-latest"))
            for name, (question, options) in FRAMINGS.items()}


def rank_tools(engine, query, names, texts, model):
    criteria = {str(i): t for i, t in enumerate(texts)}
    a, ms = choice(engine, query, criteria, TOOL_QUESTION, model)
    order = sorted(a["probabilities"].items(), key=lambda kv: -kv[1])
    return [names[int(i)] for i, _ in order], ms


def score_tools(engine, cases, names, texts, model):
    top1 = r3 = r5 = 0
    lat, misses = [], []
    scored = [c for c in cases if c["gold"]]
    for c in scored:
        ranked, ms = rank_tools(engine, c["query"], names, texts, model)
        lat.append(ms)
        top1 += ranked[0] in c["gold"]
        r3 += any(n in c["gold"] for n in ranked[:3])
        hit5 = any(n in c["gold"] for n in ranked[:5])
        r5 += hit5
        if ranked[0] not in c["gold"]:
            misses.append({"query": c["query"], "gold": c["gold"], "top3": ranked[:3]})
    n = len(scored)
    return {"top1": f"{top1}/{n}", "recall3": f"{r3}/{n}", "recall5": f"{r5}/{n}",
            "latency_ms_p50": statistics.median(lat[1:]) if len(lat) > 1 else None, "misses": misses}


def tools(engine):
    data = json.loads(Path("/data/tools.json").read_text())
    names = [t["name"] for t in data["tools"]]
    candidates = {
        "T-A": [t["document"] for t in data["tools"]],
        "T-B": [f'{t["name"]}: {t["summary"]}' if t["summary"] else t["name"] for t in data["tools"]],
    }
    blind = data["blind"]["cases"]
    sets = {"gate": data["gate"], "heldout": data["heldout"],
            "blind_en": [c for c in blind if c["lang"] == "en"], "blind_it": [c for c in blind if c["lang"] == "it"]}
    out = {}
    for name, texts, model in [("T-A", candidates["T-A"], "clm-latest"), ("T-B", candidates["T-B"], "clm-latest"),
                               ("T-A-raw", candidates["T-A"], "clm-raw")]:
        out[name] = {s: score_tools(engine, cases, names, texts, model) for s, cases in sets.items()}
    return out


def wait_for_encoder(server, deadline_s=1500):
    started = time.time()
    while time.time() - started < deadline_s:
        if server.poll() is not None:
            raise RuntimeError(f"vllm exited with {server.returncode}")
        try:
            with urllib.request.urlopen("http://127.0.0.1:8090/v1/models", timeout=5) as r:
                if r.status == 200:
                    return time.time() - started
        except OSError:
            pass
        time.sleep(5)
    raise TimeoutError("vllm did not come up")


@contextlib.contextmanager
def clm_engine():
    """vLLM serving Qwen3-8B with last-token pooling, as CLM's serve_qwen3_8b.sh does, and the reference head."""
    server = subprocess.Popen([
        "vllm", "serve", "Qwen/Qwen3-8B", "--served-model-name", "qwen3-8b", "--runner", "pooling",
        "--enforce-eager", "--enable-prefix-caching", "--max-model-len", "2048",
        "--gpu-memory-utilization", "0.90", "--port", "8090",
    ])
    try:
        boot_s = wait_for_encoder(server)
        from clm import Engine
        from clm.heads import download

        engine = Engine(emb_url=EMB_URL, emb_model="qwen3-8b", checkpoint=download())
        cache.commit()
        yield engine, boot_s
    finally:
        server.terminate()


GPU_FUNCTION = dict(image=image, gpu="L4", volumes={"/cache": cache}, timeout=3600,
                    secrets=[modal.Secret.from_name("aura-clm-hf")])


@app.function(**GPU_FUNCTION)
def stage1():
    with clm_engine() as (engine, boot_s):
        return {"encoder_boot_s": boot_s, "sanity": sanity(engine), "effort": effort(engine), "tools": tools(engine)}


@app.function(**GPU_FUNCTION)
def stage1b():
    with clm_engine() as (engine, boot_s):
        return {"encoder_boot_s": boot_s, "effort": framing_effort(engine)}


@app.function(**GPU_FUNCTION)
def check():
    """Setup validity only: the model card's rank example, and raw text vs CLM's training token recipe."""
    with clm_engine() as (engine, _):
        import numpy as np
        from transformers import AutoTokenizer

        tides = engine.rank("What causes tides on Earth?",
                            ["The Moon's gravitational pull.", "Photosynthesis in plants.", "Because the Earth is round."])
        tok = AutoTokenizer.from_pretrained("Qwen/Qwen3-8B")
        texts = ["che ore sono adesso", "Customer: my invoice was charged twice and nobody answers the phone!"]
        ids = [tok(t, add_special_tokens=False)["input_ids"] for t in texts]
        raw = _embed({"model": "qwen3-8b", "input": texts})
        recipe = _embed({"model": "qwen3-8b", "input": ids})
        cos = [float(np.dot(a, b) / (np.linalg.norm(a) * np.linalg.norm(b))) for a, b in zip(raw, recipe)]
        return {"tides": tides, "raw_vs_recipe_cosine": cos, "sanity": sanity(engine)}


def _embed(body):
    import numpy as np

    req = urllib.request.Request(EMB_URL, data=json.dumps(body).encode(), headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=120) as r:
        return [np.asarray(d["embedding"], dtype=np.float64) for d in json.load(r)["data"]]


@app.local_entrypoint()
def setup_check():
    print(json.dumps(check.remote(), indent=1, ensure_ascii=False))


def save(name, out):
    results = HERE / "results"
    results.mkdir(exist_ok=True)
    (results / f"{name}.json").write_text(json.dumps(out, indent=1, ensure_ascii=False), encoding="utf-8")


def print_effort(out):
    for name, run in out["effort"].items():
        print(name, {k: run["plain"][k] for k in ("accuracy", "none_vs_rest", "vm_traffic", "hard_to_none")},
              "greeting:", run["greeting_fast_path"]["accuracy"], f'p50 {run["latency_ms_p50"]:.1f} ms')


@app.local_entrypoint()
def framing():
    out = stage1b.remote()
    save("stage1b", out)
    print_effort(out)


@app.local_entrypoint()
def main():
    out = stage1.remote()
    save("stage1", out)
    print("sanity", out["sanity"])
    print_effort(out)
    for name, run in out["tools"].items():
        print(name, {s: (r["top1"], r["recall5"]) for s, r in run.items()})
