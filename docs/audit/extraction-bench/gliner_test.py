"""Does onnx-community/gliner_multi-v2.1 earn its place in the extraction pipeline?

Measured against the sidecar's REAL configuration: the 15 default POLE+O labels from
extraction/factory.py:43-60 and the shipped threshold of 0.5 (config/settings.py:217).

Fixtures are real: rows 4-6 are verbatim user turns pulled from aura.conversation_turns,
and row 7 is the weather-table header noise the LLM extractor actually turned into
"entities" (Vento 73, Pioggia 50, Nebbia 28 ...) by ingesting tool results.
"""

import json
import os
import statistics
import time

from gliner import GLiNER

LABELS = [
    "person", "organization", "location", "event", "object",
    "vehicle", "phone number", "email address", "document", "device",
    "address", "city", "country", "company", "meeting",
]
THRESHOLD = 0.5

CASES = [
    ("signal", "Davide vive a Caraglio e lavora in Go sugli AI agents.",
     {"Davide", "Caraglio"}),
    ("signal", "Il fornitore VerifyCorp ha sede a Bologna.",
     {"VerifyCorp", "Bologna"}),
    ("signal", "Ho parlato con Marco Bianchi di Acme S.r.l. a Torino martedi.",
     {"Marco Bianchi", "Acme S.r.l.", "Torino"}),
    ("real-turn", "Nella tua memoria il mio nome risulta sbagliato e ci sono dei "
     "doppioni. Sistema la tua memoria.", set()),
    ("real-turn", "Lo scheduler alle 8 non esiste cancellalo e controlla che non ci "
     "sia piu in memoria", set()),
    ("real-turn", "Ogni giorno alle 8:00 controlla i log di sistema degli ultimi 7 "
     "giorni e mandami un riassunto degli errori ricorrenti", set()),
    ("tool-noise", "Vento Pioggia Nebbia Quota Pressione Temperatura Visibilita "
     "Raffiche Umidita Percepita Nuvolosita", set()),
    ("real-turn", "ciao", set()),
]

REPO = os.environ.get("GLINER_REPO", "onnx-community/gliner_multi-v2.1")
USE_ONNX = os.environ.get("GLINER_ONNX", "1") == "1"
kwargs = (
    {"load_onnx_model": True, "onnx_model_file": os.environ.get("GLINER_ONNX_FILE", "onnx/model_fp16.onnx")}
    if USE_ONNX
    else {}
)
print(f"loading {REPO} (onnx={USE_ONNX}, cpu) ...", flush=True)
load_start = time.time()
model = GLiNER.from_pretrained(REPO, **kwargs)
print(f"loaded in {time.time() - load_start:.1f}s\n", flush=True)

# One warm-up: first inference pays graph-init costs that no real message would.
model.predict_entities("warm up", LABELS, threshold=THRESHOLD)

latencies = []
recalled = missed = spurious = 0
for kind, text, expected in CASES:
    started = time.time()
    found = model.predict_entities(text, LABELS, threshold=THRESHOLD)
    elapsed_ms = (time.time() - started) * 1000
    latencies.append(elapsed_ms)

    names = {e["text"] for e in found}
    hits = {n for n in names if any(n in exp or exp in n for exp in expected)} if expected else set()
    extra = names - hits

    recalled += len(hits)
    missed += max(0, len(expected) - len(hits))
    spurious += len(extra)

    detail = ", ".join(f"{e['text']}={e['label']}({e['score']:.2f})" for e in found) or "-"
    print(f"[{kind:10}] {elapsed_ms:6.0f}ms  {text[:62]!r}")
    print(f"{'':13} expected={sorted(expected) or '-'}")
    print(f"{'':13} found   ={detail}\n")

print("=" * 72)
print(json.dumps({
    "expected_entities": recalled + missed,
    "recalled": recalled,
    "missed": missed,
    "spurious_on_no_entity_text": spurious,
    "latency_ms_median": round(statistics.median(latencies)),
    "latency_ms_max": round(max(latencies)),
}, indent=2))
