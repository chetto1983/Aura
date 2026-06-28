#!/usr/bin/env python3
"""Spike 070 - is reranking worth it for Aura, or overengineered?
Pure retrieval-quality test (no DB): granite cosine top-15 (vector) vs cosine->Qwen3-Reranker
(vector+rerank), on the real G220 manual. Natural queries (bi-encoder easy case) + HARD
paraphrased queries (lexical gap, where rerank should earn its keep). Prints both rankings +
score separation + reorder magnitude + snippets for human judgment."""
import fitz, requests, numpy as np, time
EMBED  = "http://aura-llama-embed:8081/v1/embeddings"
RERANK = "http://aura-rerank:8085/v1/rerank"
PDF = "/in/G220_op_instr_0824_en-US.pdf"
CAP, TARGET, OVERLAP, TOPN, SHOW = 600, 900, 120, 15, 5

def log(*a): print(*a, flush=True)
def embed(texts, b=32):
    out=[]
    for i in range(0,len(texts),b):
        r=requests.post(EMBED,json={"input":texts[i:i+b]},timeout=120); r.raise_for_status()
        out+=[d["embedding"] for d in r.json()["data"]]
    return np.array(out,dtype=np.float32)
def rerank(q, docs):
    docs=[(d[:480] if d.strip() else "n/a") for d in docs]   # cap per-pair tokens (-c 2048)
    r=requests.post(RERANK,json={"query":q[:480],"documents":docs},timeout=60); r.raise_for_status()
    return [(x["index"], x["relevance_score"]) for x in r.json()["results"]]

def extract():
    doc=fitz.open(PDF); ch=[]
    for p in range(doc.page_count):
        t=doc.load_page(p).get_text("text").strip()
        if not t: continue
        i=0
        while i<len(t):
            piece=" ".join(t[i:i+TARGET].split())
            if len(piece)>40: ch.append({"page":p+1,"text":piece})
            i+=TARGET-OVERLAP
            if len(ch)>=CAP: return ch
    return ch

NATURAL = [
    "How do I reset the converter to factory settings?",
    "What is the permitted ambient operating temperature?",
    "tightening torque specification for the terminals",
    "how to connect the line supply mains cables",
]
HARD = [  # everyday phrasing vs manual terminology -> stresses the bi-encoder
    "the machine keeps shutting off when it gets too warm, what stops it from overheating?",
    "I messed up the configuration and just want everything back to how it came from the box",
    "what thickness of wire do I need for the power input?",
    "the display is showing a code starting with F and the drive stopped, what now?",
]

def main():
    t=time.time(); chunks=extract(); texts=[c["text"] for c in chunks]
    cev=embed(texts); cev/=np.linalg.norm(cev,axis=1,keepdims=True)+1e-9
    log(f"[setup] {len(chunks)} chunks embedded in {time.time()-t:.1f}s\n")
    for label, qs in [("NATURAL (bi-encoder easy)", NATURAL), ("HARD (lexical-gap)", HARD)]:
        log(f"########## {label} ##########")
        qev=embed(qs); qev/=np.linalg.norm(qev,axis=1,keepdims=True)+1e-9
        for q,qv in zip(qs,qev):
            cos=cev@qv
            vtop=np.argsort(-cos)[:TOPN]                      # vector top-N (candidates)
            try: rr=rerank(q,[texts[i] for i in vtop])        # rerank the candidates
            except Exception as e: log(f"\nQ: {q}\n  rerank ERROR: {e}"); continue
            rscores={vtop[idx]:sc for idx,sc in rr}
            rtop=[vtop[idx] for idx,_ in sorted(rr,key=lambda x:-x[1])]
            vset={int(chunks[i]['page']) for i in vtop[:SHOW]}
            rset={int(chunks[i]['page']) for i in rtop[:SHOW]}
            spread=max(s for _,s in rr)-min(s for _,s in rr)
            new_in_top5=len(rset-vset)
            log(f"\nQ: {q}")
            log(f"  vector top5 (page,cos): {[(int(chunks[i]['page']),round(float(cos[i]),3)) for i in vtop[:SHOW]]}")
            log(f"  rerank top5 (page,score): {[(int(chunks[i]['page']),round(float(rscores[i]),3)) for i in rtop[:SHOW]]}")
            log(f"  rerank score spread: {spread:.3f}   new-in-top5 after rerank: {new_in_top5}/5")
            log(f"  vec #1  p{chunks[vtop[0]]['page']}: {texts[vtop[0]][:140]}")
            log(f"  rank #1 p{chunks[rtop[0]]['page']}: {texts[rtop[0]][:140]}")
    log("\n[done]")

if __name__=="__main__":
    try: main()
    except Exception as e:
        import traceback; traceback.print_exc()
