"""Retrieval over the DENSE-ONLY store: EmbeddingGemma query -> ArcadeDB vector.neighbors.

Ground truth is a literal string that must appear in the retrieved passage, so this
produces a recall@k number rather than an impression.
"""
import base64, json, os, time, urllib.request

ARCADE = os.environ.get("ARCADE_HTTP", "http://aura-arcadedb:2480")
DB = os.environ.get("ARCADE_DB", "aura_ingest_spike")
PW = os.environ["ARCADEDB_PASSWORD"]
EMBED_URL = os.environ.get("EMBED_URL", "http://aura-llama-embed:8081/v1/embeddings")
AUTH = "Basic " + base64.b64encode(f"root:{PW}".encode()).decode()

# (question, string that MUST appear in a correct passage, lexical keyword baseline)
# Spans TWO documents on purpose: with the whole corpus indexed, a mortgage question
# must not surface the gas form and vice versa. Single-document recall proves nothing
# about routing.
CASES = [
    # --- documenti da stampare.pdf (Fineco, estinzione mutuo) ---
    ("Quanto devo pagare in totale per estinguere il mutuo?", "53.368,85", "estinzione"),
    ("Qual e' il capitale residuo da restituire?",            "53.362,55", "capitale"),
    ("Entro quale data devo effettuare il versamento?",       "12/03/2026", "versamento"),
    ("Quando e' stato sottoscritto il contratto di mutuo?",   "13/05/2020", "sottoscritto"),
    ("A chi devo intestare il bonifico e con quale IBAN?",    "IT84H",      "bonifico"),
    ("Chi sono gli intestatari del mutuo?",                   "MARCHETTO",  "intestatari"),
    # --- gas_richiesta_voltura.pdf (Smart Energy, voltura gas) ---
    ("Qual e' il codice PDR del punto di riconsegna del gas?", "00881404688150", "PDR"),
    ("Chi e' il precedente intestatario della fornitura gas?", "LERDA ELIO",     "precedente"),
    ("Quanto costa la voltura per uso domestico?",             "19",             "Voltura"),
    ("A quale indirizzo email vanno inviati i documenti?",     "smartenergy",    "assistenza"),
    ("Qual e' la lettura del contatore dichiarata?",           "008837",         "contatore"),
    ("Qual e' il codice fiscale del precedente intestatario?", "LDLEI66H09D205G", "fiscale"),
]


def sql(cmd):
    req = urllib.request.Request(
        f"{ARCADE}/api/v1/command/{DB}",
        data=json.dumps({"language": "sql", "command": cmd}).encode(),
        headers={"Authorization": AUTH, "Content-Type": "application/json"})
    return json.loads(urllib.request.urlopen(req, timeout=60).read())["result"]


PREFIX = os.environ.get("EMBED_PREFIX", "1") == "1"


def embed(text, kind="query"):
    if PREFIX:
        text = (f"task: search result | query: {text}" if kind == "query"
                else f"title: none | text: {text}")
    req = urllib.request.Request(
        EMBED_URL,
        data=json.dumps({"input": text, "model": "embeddinggemma"}).encode(),
        headers={"Content-Type": "application/json"})
    return json.loads(urllib.request.urlopen(req, timeout=120).read())["data"][0]["embedding"]


total = sql("SELECT count(*) AS n FROM Passage")[0]["n"]
print(f"store: {DB}  passages={total}\n")

K = 3
hits1 = hits3 = lex1 = 0
lat = []
for q, truth, kw in CASES:
    t0 = time.time()
    vec = embed(q)
    t_embed = time.time() - t0
    t1 = time.time()
    rows = sql(f'SELECT expand(vector.neighbors("Passage[embedding]", {json.dumps(vec)}, {K}))')
    t_search = time.time() - t1
    lat.append((t_embed, t_search))

    ranks = [i for i, r in enumerate(rows) if truth in (r.get("text") or "")]
    r1 = bool(ranks) and ranks[0] == 0
    r3 = bool(ranks)
    hits1 += r1
    hits3 += r3

    lex = sql(f"SELECT ordinal, text FROM Passage WHERE text CONTAINSTEXT '{kw}' LIMIT {K}")
    lex_ok = any(truth in (r.get("text") or "") for r in lex)
    lex1 += lex_ok

    got = (rows[0].get("text") or "").replace("\n", " ")[:90] if rows else "(none)"
    print(f"Q: {q}")
    print(f"   truth '{truth}'  dense@1={'HIT ' if r1 else 'miss'} dense@3={'HIT ' if r3 else 'miss'}"
          f"  lexical={'HIT' if lex_ok else 'miss'}   (embed {t_embed*1000:.0f}ms, search {t_search*1000:.0f}ms)")
    print(f"   top1: {got}…\n")

n = len(CASES)
print("=" * 68)
print(f"DENSE   recall@1 = {hits1}/{n}    recall@3 = {hits3}/{n}")
print(f"LEXICAL recall@3 = {lex1}/{n}   (naive single-keyword baseline)")
print(f"latency median: embed {sorted(x for x,_ in lat)[n//2]*1000:.0f}ms, "
      f"vector search {sorted(y for _,y in lat)[n//2]*1000:.0f}ms")
