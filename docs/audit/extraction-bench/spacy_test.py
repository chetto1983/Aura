"""Which spaCy model, if any, earns stage 1 on this deployment's Italian content?

Same fixtures and same metric as the GLiNER run, so the two are directly comparable:
three signal sentences, three verbatim user turns from aura.conversation_turns, the
weather-table header noise the LLM extractor turned into 13 "entities", and "ciao".

en_core_web_sm is included not as a candidate but as the control: it is what
config/settings.py:207 ships as the default, so it is what stage 1 would do today if
someone simply installed the extras without changing anything.
"""

import json
import statistics
import time

import spacy

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

MODELS = ["en_core_web_sm", "it_core_news_sm", "it_core_news_lg", "xx_ent_wiki_sm"]

for model_name in MODELS:
    try:
        nlp = spacy.load(model_name)
    except Exception as exc:  # noqa: BLE001
        print(f"\n### {model_name}: NOT AVAILABLE ({type(exc).__name__})")
        continue

    nlp("warm up")
    latencies = []
    recalled = missed = spurious = 0
    lines = []
    for kind, text, expected in CASES:
        started = time.time()
        doc = nlp(text)
        latencies.append((time.time() - started) * 1000)

        names = {ent.text for ent in doc.ents}
        hits = (
            {n for n in names if any(n in exp or exp in n for exp in expected)}
            if expected
            else set()
        )
        extra = names - hits
        recalled += len(hits)
        missed += max(0, len(expected) - len(hits))
        spurious += len(extra)

        detail = ", ".join(f"{e.text}={e.label_}" for e in doc.ents) or "-"
        lines.append(f"  [{kind:10}] {detail}")

    print(f"\n### {model_name}")
    print("\n".join(lines))
    print("  " + json.dumps({
        "recalled": recalled,
        "of_expected": recalled + missed,
        "spurious_on_no_entity_text": spurious,
        "latency_ms_median": round(statistics.median(latencies), 1),
    }))
