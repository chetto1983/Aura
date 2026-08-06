"""Can the system tell "no answer here" from "answer here"?

Retrieval always returns k results. If unanswerable questions come back with the same
similarity as answerable ones, then NO distance threshold can abstain, and abstention
has to be someone else's job (the reranker). This measures the separation.
"""
import base64, json, os, statistics, urllib.request

ARCADE = os.environ.get("ARCADE_HTTP", "http://aura-arcadedb:2480")
DB = os.environ.get("ARCADE_DB", "aura_ingest_spike")
PW = os.environ["ARCADEDB_PASSWORD"]
EMBED_URL = os.environ.get("EMBED_URL", "http://aura-llama-embed:8081/v1/embeddings")
AUTH = "Basic " + base64.b64encode(f"root:{PW}".encode()).decode()

ANSWERABLE = [
    "Quanto devo pagare in totale per estinguere il mutuo?",
    "Qual e' il capitale residuo da restituire?",
    "Entro quale data devo effettuare il versamento?",
    "Quando e' stato sottoscritto il contratto di mutuo?",
    "Chi e' il precedente intestatario della fornitura gas?",
    "Quanto costa la voltura per uso domestico?",
]

# Plausible questions about documents of this KIND whose answer is simply absent here.
UNANSWERABLE = [
    "Qual e' la targa dell'automobile assicurata?",
    "Quanto costa l'abbonamento mensile a Netflix?",
    "Chi e' il medico curante indicato nel referto?",
    "Qual e' l'orario del volo di ritorno da Barcellona?",
    "Quanti dipendenti aveva l'azienda nel 2024?",
    "Qual e' la password della rete wifi dell'ufficio?",
]


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


def top1_distance(q):
    vec = embed(q)
    rows = sql(f'SELECT expand(vector.neighbors("Passage[embedding]", {json.dumps(vec)}, 1))')
    if not rows:
        return None, ""
    r = rows[0]
    return r.get("distance"), (r.get("text") or "").replace("\n", " ")[:70]


print(f"{'':4}{'distanza top-1':>15}  domanda")
print("-" * 90)
groups = {}
for label, qs in (("SI ", ANSWERABLE), ("NO ", UNANSWERABLE)):
    ds = []
    for q in qs:
        d, txt = top1_distance(q)
        ds.append(d)
        print(f"{label} {d:15.4f}  {q}")
        print(f"{'':20}-> {txt}…")
    groups[label] = ds

a, u = groups["SI "], groups["NO "]
print("\n" + "=" * 90)
print(f"risposta ESISTE    : min {min(a):.4f}  mediana {statistics.median(a):.4f}  max {max(a):.4f}")
print(f"risposta NON esiste: min {min(u):.4f}  mediana {statistics.median(u):.4f}  max {max(u):.4f}")
overlap = max(a) >= min(u)
print(f"\nsovrapposizione: {'SI — nessuna soglia separa i due gruppi' if overlap else 'NO — una soglia funziona'}")
if overlap:
    print(f"  la peggiore risposta VERA ({max(a):.4f}) e' piu' lontana della migliore FALSA ({min(u):.4f})")
