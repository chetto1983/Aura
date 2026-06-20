#!/usr/bin/env python3
"""Spike 070b - INDUSTRIAL GraphRAG + timing (not a toy flat-vector test).
Real Aura shape: Neo4j :Chunk nodes CONNECTED by :NEXT_CHUNK (reading-order adjacency).
Pipeline per query, TIMED per stage:
  1. vector seed   -> db.index.vector.queryNodes (Bolt)
  2. graph expand  -> 1-hop :NEXT_CHUNK neighbors (connected nodes = local context)
  3. rerank        -> Qwen3-Reranker-0.6B over the expanded candidate set
Reports per-stage p50/p95 + the rerank effect on the graph-expanded pool."""
import fitz, requests, numpy as np, time, statistics, os, json
from neo4j import GraphDatabase
EMBED="http://aura-llama-embed:8081/v1/embeddings"; RERANK="http://aura-rerank:8085/v1/rerank"
N4="bolt://neo4j-spike:7687"; N4_AUTH=("neo4j","spikepass123")
PDF="/in/G220_op_instr_0824_en-US.pdf"; CAP,TARGET,OVERLAP=600,900,120
SEED_K, FINAL_K, REPS = 10, 5, 8
def log(*a): print(*a, flush=True)
def embed(texts,b=32):
    out=[]
    for i in range(0,len(texts),b):
        r=requests.post(EMBED,json={"input":texts[i:i+b]},timeout=120); r.raise_for_status()
        out+=[d["embedding"] for d in r.json()["data"]]
    return out
def rerank(q,docs):
    docs=[(d[:480] if d.strip() else "n/a") for d in docs]
    r=requests.post(RERANK,json={"query":q[:480],"documents":docs},timeout=60); r.raise_for_status()
    return sorted([(x["index"],x["relevance_score"]) for x in r.json()["results"]],key=lambda x:-x[1])

def get_corpus():
    cache="/work/corpus.json"
    if os.path.exists(cache): d=json.load(open(cache)); log(f"[cache] {len(d['chunks'])} chunks"); return d["chunks"],d["vecs"]
    doc=fitz.open(PDF); ch=[]
    for p in range(doc.page_count):
        t=doc.load_page(p).get_text("text").strip()
        if not t: continue
        i=0
        while i<len(t):
            piece=" ".join(t[i:i+TARGET].split())
            if len(piece)>40: ch.append({"id":f"c{len(ch)}","page":p+1,"text":piece})
            i+=TARGET-OVERLAP
            if len(ch)>=CAP: break
        if len(ch)>=CAP: break
    t=time.time(); vecs=embed([c["text"] for c in ch]); log(f"[embed] {len(vecs)} in {time.time()-t:.1f}s")
    json.dump({"chunks":ch,"vecs":vecs},open(cache,"w")); return ch,vecs

QUERIES=[
    "How do I reset the converter to factory settings?",
    "tightening torque specification for the terminals",
    "the machine keeps shutting off when it gets too warm, what stops overheating?",
    "what thickness of wire do I need for the power input?",
    "the display shows a code starting with F and the drive stopped, what now?",
    "how to connect the line supply mains cables",
]

def main():
    chunks,vecs=get_corpus(); drv=GraphDatabase.driver(N4,auth=N4_AUTH)
    with drv.session() as s:
        s.run("MATCH (c:Chunk) DETACH DELETE c")
        s.run("CREATE VECTOR INDEX chunk_vec IF NOT EXISTS FOR (c:Chunk) ON c.embedding OPTIONS {indexConfig:{`vector.dimensions`:384,`vector.similarity_function`:'cosine'}}")
        t=time.time()
        for i in range(0,len(chunks),200):
            rows=[{"id":c["id"],"page":c["page"],"text":c["text"],"vec":v} for c,v in zip(chunks[i:i+200],vecs[i:i+200])]
            s.run("UNWIND $rows AS r MERGE (c:Chunk{id:r.id}) SET c.page=r.page,c.content=r.text,c.embedding=r.vec",rows=rows)
        # connect reading-order adjacency
        pairs=[{"a":f"c{i}","b":f"c{i+1}"} for i in range(len(chunks)-1)]
        s.run("UNWIND $p AS x MATCH (a:Chunk{id:x.a}),(b:Chunk{id:x.b}) MERGE (a)-[:NEXT_CHUNK]->(b)",p=pairs)
        for _ in range(30):
            st=s.run("SHOW INDEXES YIELD name,state WHERE name='chunk_vec' RETURN state").single()
            if st and st["state"]=="ONLINE": break
            time.sleep(1)
        rels=s.run("MATCH (:Chunk)-[r:NEXT_CHUNK]->() RETURN count(r) AS n").single()["n"]
    log(f"[neo4j] {len(chunks)} :Chunk + {rels} :NEXT_CHUNK edges loaded in {time.time()-t:.1f}s, index ONLINE\n")

    qvecs=embed(QUERIES)
    tv,tg,tr,texp=[],[],[],[]   # stage timings (ms)
    for q,qv in zip(QUERIES,qvecs):
        # measured pipeline (REPS for timing)
        for _ in range(REPS):
            with drv.session() as s:
                t=time.time()
                seeds=[r["id"] for r in s.run("CALL db.index.vector.queryNodes('chunk_vec',$k,$v) YIELD node,score RETURN node.id AS id ORDER BY score DESC",k=SEED_K,v=qv)]
                tv.append((time.time()-t)*1000)
                t=time.time()
                exp=[(r["id"],r["page"],r["content"]) for r in s.run(
                    "MATCH (c:Chunk) WHERE c.id IN $s OPTIONAL MATCH (c)-[:NEXT_CHUNK]-(n:Chunk) "
                    "WITH collect(DISTINCT c)+collect(DISTINCT n) AS xs UNWIND xs AS x RETURN DISTINCT x.id AS id,x.page AS page,x.content AS content",s=seeds)]
                tg.append((time.time()-t)*1000)
            texp.append(len(exp))
            t=time.time(); rr=rerank(q,[e[2] for e in exp]); tr.append((time.time()-t)*1000)
        top=[exp[i] for i,_ in rr[:FINAL_K]]
        log(f"Q: {q}")
        log(f"  seeds={SEED_K} -> graph-expanded pool={len(exp)} -> rerank top{FINAL_K} pages: {[e[1] for e in top]}")
        log(f"  rank#1 p{top[0][1]}: {top[0][2][:120]}")
    def p(a,q): return statistics.quantiles(a,n=100)[q-1] if len(a)>1 else a[0]
    log("\n=== TIMING (per query, ms) ===")
    log(f"  1. vector seed    p50 {p(tv,50):.1f}  p95 {p(tv,95):.1f}")
    log(f"  2. graph expand   p50 {p(tg,50):.1f}  p95 {p(tg,95):.1f}   (avg pool {statistics.mean(texp):.0f} nodes from {SEED_K} seeds)")
    log(f"  3. rerank         p50 {p(tr,50):.1f}  p95 {p(tr,95):.1f}   <-- the cost")
    tot=[a+b+c for a,b,c in zip(tv,tg,tr)]
    log(f"  END-TO-END        p50 {p(tot,50):.1f}  p95 {p(tot,95):.1f}")
    drv.close()
if __name__=="__main__":
    try: main()
    except Exception as e:
        import traceback; traceback.print_exc()
