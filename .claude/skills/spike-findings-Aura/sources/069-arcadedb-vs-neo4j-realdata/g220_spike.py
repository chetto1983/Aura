#!/usr/bin/env python3
"""Spike 069 - real-data head-to-head: ArcadeDB vs Neo4j on the Siemens G220 manual.
Extract (PyMuPDF) -> embed (live Granite 384d) -> load BOTH DBs -> query -> measure
retrieval overlap + p50/p95 latency. Runs in a python container joined to g220-spike
(neo4j-spike, arcadedb-spike) AND aura_default (aura-llama-embed)."""
import fitz, requests, time, statistics, sys, json, os
from neo4j import GraphDatabase

PDF   = "/in/G220_op_instr_0824_en-US.pdf"
EMBED = "http://aura-llama-embed:8081/v1/embeddings"
DIM   = 384
CHUNK_CAP = 1500          # cap for a gentle CPU run; logged
TARGET, OVERLAP = 900, 120
N4_URI = "bolt://neo4j-spike:7687"; N4_AUTH = ("neo4j", "spikepass123")
AB = "http://arcadedb-spike:2480/api/v1"; AB_AUTH = ("root", "playwithdata"); AB_DB = "g220"
REPS = 15                 # latency repetitions per query
K = 5

def log(*a): print(*a, flush=True)

# ---------- 1. extract ----------
def extract():
    doc = fitz.open(PDF); chunks = []
    for pno in range(doc.page_count):
        txt = doc.load_page(pno).get_text("text").strip()
        if not txt: continue
        i = 0
        while i < len(txt):
            piece = " ".join(txt[i:i+TARGET].split())
            if len(piece) > 40:
                chunks.append({"id": f"p{pno+1}_{i}", "page": pno+1, "text": piece})
            i += TARGET - OVERLAP
    return doc.page_count, chunks

# ---------- 2. embed ----------
def embed(texts, batch=32):
    out = []
    for i in range(0, len(texts), batch):
        b = texts[i:i+batch]
        r = requests.post(EMBED, json={"input": b}, timeout=120); r.raise_for_status()
        out.extend([d["embedding"] for d in r.json()["data"]])
        if i % 320 == 0: log(f"  embedded {i+len(b)}/{len(texts)}")
    return out

# ---------- ArcadeDB helpers ----------
def ab(cmd, lang="sql"):
    r = requests.post(f"{AB}/command/{AB_DB}", json={"language": lang, "command": cmd}, auth=AB_AUTH, timeout=120)
    if r.status_code >= 400: raise RuntimeError(f"AB {r.status_code}: {r.text[:200]} :: {cmd[:80]}")
    return r.json().get("result", [])
def ab_param(cmd, params):  # bound params = bulletproof vs mojibake/quotes (parity with Bolt)
    r = requests.post(f"{AB}/command/{AB_DB}", json={"language": "sql", "command": cmd, "params": params}, auth=AB_AUTH, timeout=60)
    if r.status_code >= 400: raise RuntimeError(f"AB {r.status_code}: {r.text[:200]}")
    return r.json().get("result", [])
def vlit(v): return "[" + ",".join(f"{x:.7g}" for x in v) + "]"
def san(t): return t.replace("\\", " ").replace("'", "''")[:1500]

def wait_ready():
    for host, url in [("arcadedb", f"{AB}/ready")]:
        for _ in range(40):
            try:
                requests.get(url, timeout=3); break
            except Exception: time.sleep(2)
    for _ in range(40):
        try:
            d = GraphDatabase.driver(N4_URI, auth=N4_AUTH); d.verify_connectivity(); d.close(); break
        except Exception: time.sleep(2)

def get_chunks():
    """Resumable: load cached chunks+vecs from /work, else pull from neo4j-spike (already
    loaded), else extract+embed from scratch. Avoids re-paying the ~200s embed on retries."""
    cache = "/work/g220_chunks.json"
    if os.path.exists(cache):
        d = json.load(open(cache)); log(f"[cache] {len(d)} chunks loaded from json"); return d
    try:
        drv = GraphDatabase.driver(N4_URI, auth=N4_AUTH)
        with drv.session() as s:
            rows = [r.data() for r in s.run("MATCH (c:Chunk) RETURN c.id AS id, c.page AS page, c.content AS text, c.embedding AS vec")]
        drv.close()
        if rows and rows[0].get("vec"):
            json.dump(rows, open(cache, "w")); log(f"[cache] pulled {len(rows)} chunks from neo4j-spike (no re-embed)"); return rows
    except Exception as e: log("[cache] neo4j pull failed:", e)
    pages, chunks = extract()
    if len(chunks) > CHUNK_CAP: chunks = chunks[:CHUNK_CAP]
    log(f"[extract] {pages} pages -> {len(chunks)} chunks")
    texts = [c["text"] for c in chunks]
    t = time.time(); vecs = embed(texts); log(f"[embed] {len(vecs)}x{DIM}d in {time.time()-t:.1f}s")
    for c, v in zip(chunks, vecs): c["vec"] = v
    json.dump(chunks, open(cache, "w")); return chunks

def main():
    wait_ready()
    chunks = get_chunks()

    # ---------- 3a. Neo4j load ----------
    drv = GraphDatabase.driver(N4_URI, auth=N4_AUTH)
    with drv.session() as s:
        s.run("MATCH (c:Chunk) DETACH DELETE c")
        s.run("CREATE VECTOR INDEX chunk_vec IF NOT EXISTS FOR (c:Chunk) ON c.embedding "
              "OPTIONS {indexConfig:{`vector.dimensions`:$d,`vector.similarity_function`:'cosine'}}", d=DIM)
        t = time.time()
        for i in range(0, len(chunks), 200):
            b = [{"id": c["id"], "page": c["page"], "text": c["text"], "vec": c["vec"]} for c in chunks[i:i+200]]
            s.run("UNWIND $rows AS r MERGE (c:Chunk {id:r.id}) SET c.page=r.page, c.content=r.text, c.embedding=r.vec", rows=b)
        n4_load = time.time() - t
        for _ in range(30):
            st = s.run("SHOW INDEXES YIELD name,state WHERE name='chunk_vec' RETURN state").single()
            if st and st["state"] == "ONLINE": break
            time.sleep(1)
    log(f"[neo4j] loaded {len(chunks)} chunks in {n4_load:.1f}s, index ONLINE")

    # ---------- 3b. ArcadeDB load ----------
    try: ab("DROP TYPE Doc UNSAFE")
    except Exception: pass
    ab("CREATE VERTEX TYPE Doc"); ab("CREATE PROPERTY Doc.embedding ARRAY_OF_FLOATS")
    ab("CREATE INDEX ON Doc (embedding) LSM_VECTOR METADATA {dimensions:%d, similarity:'COSINE', maxConnections:16, beamWidth:100}" % DIM)
    t = time.time()
    for c in chunks:
        ab_param(f"INSERT INTO Doc SET id=:id, page=:page, content=:content, embedding={vlit(c['vec'])}",
                 {"id": c["id"], "page": c["page"], "content": c["text"][:1500]})
    ab_load = time.time() - t
    log(f"[arcadedb] loaded {len(chunks)} chunks in {ab_load:.1f}s")

    # ---------- 4. queries ----------
    queries = [
        "How do I reset the converter to factory settings?",
        "What is the permitted ambient operating temperature?",
        "How to set the motor rated current parameter?",
        "minimum clearance required for installation and cooling",
        "how to connect the line supply mains cables",
        "commissioning the drive with the operator panel",
        "tightening torque specification for the terminals",
        "what does a fault or alarm code mean and how to acknowledge it",
    ]
    qvecs = embed(queries)
    def n4_search(qv):
        with drv.session() as s:
            rec = s.run("CALL db.index.vector.queryNodes('chunk_vec',$k,$v) YIELD node,score "
                        "RETURN node.id AS id, node.page AS page, score ORDER BY score DESC", k=K, v=qv)
            return [(r["id"], r["page"], r["score"]) for r in rec]
    def ab_search(qv):
        rows = ab(f"SELECT id, page, $distance AS dist FROM (SELECT expand(vectorNeighbors('Doc[embedding]', {vlit(qv)}, {K})))")
        return [(r.get("id"), r.get("page"), r.get("dist")) for r in rows]

    n4_lat, ab_lat, overlaps = [], [], []
    log("\n=== per-query top-5 (overlap = same chunk ids) ===")
    for q, qv in zip(queries, qvecs):
        n4 = n4_search(qv); abr = ab_search(qv)
        ov = len(set(x[0] for x in n4) & set(x[0] for x in abr))
        overlaps.append(ov)
        for _ in range(REPS):
            t = time.time(); n4_search(qv); n4_lat.append((time.time()-t)*1000)
            t = time.time(); ab_search(qv); ab_lat.append((time.time()-t)*1000)
        log(f"\nQ: {q}")
        log(f"  neo4j top-5 pages: {[x[1] for x in n4]}")
        log(f"  arcade top-5 pages: {[x[1] for x in abr]}")
        log(f"  chunk-id overlap@5: {ov}/5")

    def pct(a, p): return statistics.quantiles(a, n=100)[p-1] if len(a) > 1 else a[0]
    log("\n=== SUMMARY ===")
    log(f"corpus: {len(chunks)} chunks ({DIM}d), {len(queries)} queries x {REPS} reps")
    log(f"load time:   neo4j {n4_load:.1f}s | arcadedb {ab_load:.1f}s")
    log(f"search p50:  neo4j {pct(n4_lat,50):.1f}ms | arcadedb {pct(ab_lat,50):.1f}ms")
    log(f"search p95:  neo4j {pct(n4_lat,95):.1f}ms | arcadedb {pct(ab_lat,95):.1f}ms")
    log(f"mean overlap@5: {statistics.mean(overlaps):.1f}/5  (per-query: {overlaps})")
    drv.close()

if __name__ == "__main__":
    try: main()
    except Exception as e:
        import traceback; traceback.print_exc(); sys.exit(1)
