"""Entity extraction worker: one JSON request per line in, one per line out.

Why a long-lived subprocess rather than cgo or a sidecar. go-spacy embeds a
Python interpreter through cgo, which forces the runtime image, the build image
and libspacy_wrapper.so onto one libpython ABI -- a mismatch fails at load, not
at build. A separate HTTP sidecar would be one more container to run, health-check
and version. A pipe costs neither: the model loads once at boot, the parent owns
the lifetime, and a crash here degrades entity extraction instead of taking the
server down.

Protocol, deliberately boring:

    ->  {"texts": ["...", "..."], "lang": "en"}
    <-  {"entities": [["Melanie", "a sunrise"], [...]]}
    <-  {"error": "..."}

Models are loaded lazily per language and kept, because loading costs 100-500 ms
and extraction costs microseconds.
"""

import json
import sys

import spacy

MODELS = {"en": "en_core_web_sm", "it": "it_core_news_sm"}
DEFAULT_LANG = "en"

# Heads too generic to identify anything, from mem0/utils/entity_extraction.py.
GENERIC_HEADS = {
    "thing", "things", "stuff", "way", "ways", "time", "times", "experience",
    "situation", "case", "fact", "matter", "issue", "idea", "ideas", "thought",
    "thoughts", "feeling", "feelings", "place", "area", "part", "kind", "type",
    "sort", "lot", "bit", "day", "days", "year", "years", "week", "weeks",
    "month", "months", "one", "ones", "something", "anything", "everything",
    "nothing", "someone", "anyone", "everyone", "cosa", "cose", "modo", "volta",
    "anno", "anni", "giorno", "giorni", "parte", "caso", "tempo",
}

# Entity labels worth keeping: the ones that name a thing rather than measure it.
# DATE/TIME/CARDINAL are dropped on purpose -- "three" and "yesterday" link
# unrelated turns to each other and would make the hub penalty do all the work.
KEEP_LABELS = {
    "PERSON", "ORG", "GPE", "LOC", "FAC", "NORP", "PRODUCT",
    "WORK_OF_ART", "EVENT", "LANGUAGE", "PER", "MISC",
}

_loaded = {}


def model_for(lang):
    name = MODELS.get(lang, MODELS[DEFAULT_LANG])
    if name not in _loaded:
        _loaded[name] = spacy.load(name, disable=["lemmatizer"])
    return _loaded[name]


def entities_of(doc):
    found, seen = [], set()

    def push(text):
        text = " ".join(text.split())
        folded = text.lower()
        if len(text) < 3 or folded in GENERIC_HEADS or folded in seen:
            return
        seen.add(folded)
        found.append(text)

    for ent in doc.ents:
        if ent.label_ in KEEP_LABELS:
            push(ent.text)
    # Noun chunks are the half a regex cannot reach: "a sunrise", "her education".
    # Determiners and pronouns are stripped so "her education" files under
    # "education" rather than under a possessive that names nobody.
    if doc.has_annotation("DEP"):
        for chunk in doc.noun_chunks:
            tokens = [t for t in chunk if t.pos_ not in ("DET", "PRON")]
            while tokens and tokens[0].pos_ in ("ADP", "PART"):
                tokens = tokens[1:]
            if not tokens or tokens[-1].text.lower() in GENERIC_HEADS:
                continue
            push(" ".join(t.text for t in tokens))
    return found


def main():
    # Line-buffered so the parent sees each reply without waiting for a flush.
    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        try:
            request = json.loads(line)
            nlp = model_for(request.get("lang", DEFAULT_LANG))
            texts = request.get("texts") or []
            reply = {"entities": [entities_of(d) for d in nlp.pipe(texts, batch_size=200)]}
        except Exception as exc:  # a bad request must not kill the worker
            reply = {"error": f"{type(exc).__name__}: {exc}"}
        sys.stdout.write(json.dumps(reply, ensure_ascii=False) + "\n")
        sys.stdout.flush()


if __name__ == "__main__":
    main()
