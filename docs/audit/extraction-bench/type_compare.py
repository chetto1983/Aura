"""Type correctness, not just recall.

The entity MERGE key is {name, type, deduplication_scope}. A wrong TYPE does not merely
mislabel a node — it creates a second one that no deduplication can ever collapse. That is
M-6 on the live graph: two "David" nodes differing only by OBJECT vs PERSON.

So recall is the easy half. This measures the half that decides whether stage 1 makes the
duplicate problem better or worse.
"""

import spacy
from gliner import GLiNER

LABELS = [
    "person", "organization", "location", "event", "object",
    "vehicle", "phone number", "email address", "document", "device",
    "address", "city", "country", "company", "meeting",
]

# name -> the coarse class the graph should end up with
TRUTH = {
    "Davide": "PERSON",
    "Caraglio": "LOCATION",
    "VerifyCorp": "ORG",
    "Bologna": "LOCATION",
    "Marco Bianchi": "PERSON",
    "Acme S.r.l.": "ORG",
    "Torino": "LOCATION",
}
TEXTS = [
    "Davide vive a Caraglio e lavora in Go sugli AI agents.",
    "Il fornitore VerifyCorp ha sede a Bologna.",
    "Ho parlato con Marco Bianchi di Acme S.r.l. a Torino martedi.",
]

COARSE = {
    "PER": "PERSON", "PERSON": "PERSON", "person": "PERSON",
    "LOC": "LOCATION", "GPE": "LOCATION", "location": "LOCATION",
    "city": "LOCATION", "country": "LOCATION", "address": "LOCATION",
    "ORG": "ORG", "organization": "ORG", "company": "ORG",
}


def score(name, extracted):
    correct = wrong = 0
    report = []
    for surface, truth in TRUTH.items():
        got = None
        for text, label in extracted:
            if surface in text or text in surface:
                got = label
                break
        coarse = COARSE.get(got, got)
        ok = coarse == truth
        correct += ok
        wrong += (got is not None) and not ok
        report.append(f"{surface}->{got or 'MISSED'}{'' if ok else '  <-- ' + truth}")
    print(f"\n### {name}: {correct}/{len(TRUTH)} types correct, {wrong} miscategorised")
    for line in report:
        print("   ", line)


model = GLiNER.from_pretrained(
    "onnx-community/gliner_multi-v2.1",
    load_onnx_model=True,
    onnx_model_file="onnx/model_fp16.onnx",
)
found = []
for text in TEXTS:
    found += [(e["text"], e["label"]) for e in model.predict_entities(text, LABELS, threshold=0.5)]
score("gliner_multi-v2.1 fp16", found)

for model_name in ("it_core_news_sm", "xx_ent_wiki_sm", "en_core_web_sm"):
    nlp = spacy.load(model_name)
    found = []
    for text in TEXTS:
        found += [(ent.text, ent.label_) for ent in nlp(text).ents]
    score(model_name, found)
