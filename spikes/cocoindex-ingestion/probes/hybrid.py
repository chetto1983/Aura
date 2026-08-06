"""Hybrid retrieval WITH abstention, measured on two numbers.

  1. recall@1 over questions that DO have an answer
  2. correct-abstention rate over questions that do NOT

The union of dense + exact-token is only a CANDIDATE set. The reranker decides, and it
is explicitly allowed to answer "nessuno" — because distance cannot: measured, the worst
true answer (0.5886) sits farther than the best false one (0.5764), so no threshold
separates them.
"""
import base64, json, os, re, urllib.request

import instructor
import litellm
import pydantic

litellm.drop_params = True

ARCADE = os.environ.get("ARCADE_HTTP", "http://aura-arcadedb:2480")
DB = os.environ.get("ARCADE_DB", "aura_ingest_spike")
PW = os.environ["ARCADEDB_PASSWORD"]
EMBED_URL = os.environ.get("EMBED_URL", "http://aura-llama-embed:8081/v1/embeddings")
MODEL = os.environ.get("KG_LLM_MODEL", "openrouter/deepseek/deepseek-v4-flash-0731:nitro")
AUTH = "Basic " + base64.b64encode(f"root:{PW}".encode()).decode()
K_DENSE, K_LEX = 10, 10
CAND_CHARS = int(os.environ.get("CAND_CHARS", "1400"))

ANSWERABLE = [
    ("Quanto devo pagare in totale per estinguere il mutuo?", "53.368,85"),
    ("Qual e' il capitale residuo da restituire?", "53.362,55"),
    ("Entro quale data devo effettuare il versamento?", "12/03/2026"),
    ("Quando e' stato sottoscritto il contratto di mutuo?", "13/05/2020"),
    ("A chi devo intestare il bonifico e con quale IBAN?", "IT84H"),
    ("Chi sono gli intestatari del mutuo?", "MARCHETTO"),
    ("Qual e' il codice PDR del punto di riconsegna del gas?", "00881404688150"),
    ("Chi e' il precedente intestatario della fornitura gas?", "LERDA ELIO"),
    ("Quanto costa la voltura per uso domestico?", "19"),
    ("A quale indirizzo email vanno inviati i documenti?", "smartenergy"),
    ("Qual e' la lettura del contatore dichiarata?", "008837"),
    ("Qual e' il codice fiscale del precedente intestatario?", "LDLEI66H09D205G"),
]
UNANSWERABLE = [
    "Qual e' la targa dell'automobile assicurata?",
    "Quanto costa l'abbonamento mensile a Netflix?",
    "Chi e' il medico curante indicato nel referto?",
    "Qual e' l'orario del volo di ritorno da Barcellona?",
    "Quanti dipendenti aveva l'azienda nel 2024?",
    "Qual e' la password della rete wifi dell'ufficio?",
]

_CASES = os.environ.get("CASES_FILE")
if _CASES:
    _j = json.load(open(_CASES, encoding="utf-8"))
    ANSWERABLE = [(q, t) for q, t in _j["answerable"]]
    UNANSWERABLE = _j["unanswerable"]

STOP = set("""qual quale quali cosa chi come dove quando quanto quanta quante quanti devo deve
essere stato stata sono il lo la i gli le un uno una di del della dei delle degli a al alla ai
alle da dal dalla in nel nella con su per tra fra che non piu e o si no mio mia miei indicato
dichiarata effettuare vanno""".split())


def sql(cmd):
    req = urllib.request.Request(
        f"{ARCADE}/api/v1/command/{DB}",
        data=json.dumps({"language": "sql", "command": cmd}).encode(),
        headers={"Authorization": AUTH, "Content-Type": "application/json"})
    return json.loads(urllib.request.urlopen(req, timeout=60).read())["result"]


def embed(text, kind="query"):
    text = (f"task: search result | query: {text}" if kind == "query"
            else f"title: none | text: {text}")
    req = urllib.request.Request(
        EMBED_URL, data=json.dumps({"input": text, "model": "embeddinggemma"}).encode(),
        headers={"Content-Type": "application/json"})
    return json.loads(urllib.request.urlopen(req, timeout=120).read())["data"][0]["embedding"]


def dense_leg(q):
    vec = embed(q)
    rows = sql(f'SELECT expand(vector.neighbors("Passage[embedding]", {json.dumps(vec)}, {K_DENSE}))')
    return {r["id"]: (r.get("text") or "") for r in rows if r.get("id")}


def lexical_leg(q):
    """Count how many content terms each passage contains — a crude BM25 with no
    dependence on Lucene query syntax we would otherwise be guessing at."""
    terms = [w for w in re.findall(r"[\w.,/-]{4,}", q.lower()) if w not in STOP]
    hits: dict[str, int] = {}
    texts: dict[str, str] = {}
    for t in terms:
        safe = t.replace("'", "")
        try:
            rows = sql(f"SELECT id, text FROM Passage WHERE text CONTAINSTEXT '{safe}' LIMIT 25")
        except Exception:
            continue
        for r in rows:
            pid = r.get("id")
            if pid:
                hits[pid] = hits.get(pid, 0) + 1
                texts[pid] = r.get("text") or ""
    ranked = sorted(hits, key=lambda p: -hits[p])[:K_LEX]
    return {p: texts[p] for p in ranked}


class Verdict(pydantic.BaseModel):
    indice: int = pydantic.Field(description="1-based index of the passage that answers, or 0 if none does")
    motivo: str


RERANK_PROMPT = (
    "Ti do una domanda e alcuni passaggi numerati estratti da documenti. "
    "Scegli l'UNICO passaggio che contiene la risposta esplicita alla domanda. "
    "Se NESSUN passaggio contiene la risposta, rispondi indice=0. "
    "Non dedurre, non inferire: se il dato non e' scritto, indice=0."
)


def rerank(q, cands):
    client = instructor.from_litellm(litellm.acompletion, mode=instructor.Mode.JSON)
    # Never truncate below the chunk size: at 700 chars against 1200-char chunks the
    # reranker was abstaining on answers it had simply never been shown.
    listing = "\n\n".join(f"[{i + 1}] {t[:CAND_CHARS]}" for i, (_, t) in enumerate(cands))
    import asyncio

    async def go():
        return await client.chat.completions.create(
            model=MODEL, response_model=Verdict, temperature=0.0,
            messages=[{"role": "system", "content": RERANK_PROMPT},
                      {"role": "user", "content": f"DOMANDA: {q}\n\nPASSAGGI:\n{listing}"}])

    return asyncio.run(go())


print(f"corpus: {sql('SELECT count(*) AS n FROM Passage')[0]['n']} passaggi\n")
hits = 0
for q, truth in ANSWERABLE:
    merged = {**lexical_leg(q), **dense_leg(q)}
    cands = list(merged.items())
    v = rerank(q, cands)
    ok = 1 <= v.indice <= len(cands) and truth in cands[v.indice - 1][1]
    hits += ok
    print(f"{'HIT ' if ok else 'MISS'} [{len(cands):2} cand] idx={v.indice}  {q}")

print()
abst = 0
for q in UNANSWERABLE:
    merged = {**lexical_leg(q), **dense_leg(q)}
    cands = list(merged.items())
    v = rerank(q, cands)
    ok = v.indice == 0
    abst += ok
    print(f"{'ASTIENE ' if ok else 'SBAGLIA '} [{len(cands):2} cand] idx={v.indice}  {q}")
    if not ok:
        print(f"          motivo: {v.motivo[:110]}")

print("\n" + "=" * 74)
print(f"HYBRID+RERANK  recall@1 = {hits}/{len(ANSWERABLE)}")
print(f"               astensioni corrette = {abst}/{len(UNANSWERABLE)}")
