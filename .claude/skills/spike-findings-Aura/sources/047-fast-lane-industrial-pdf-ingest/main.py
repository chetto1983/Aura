from __future__ import annotations

import argparse
import json
import math
import re
import statistics
import sys
import time
from pathlib import Path

import fitz
import numpy as np
from sklearn.feature_extraction.text import TfidfVectorizer
from sklearn.preprocessing import normalize


DEFAULT_PDF = Path(
    r"C:\Users\Davide\OneDrive - Sonepar\Documenti\G220_op_instr_0824_en-US.pdf"
)

QUERIES = [
    (
        "safety",
        "warning notice system DANGER WARNING CAUTION NOTICE qualified personnel proper use",
        ["danger", "warning", "caution", "notice", "qualified personnel"],
    ),
    (
        "mechanical",
        "minimum clearances cooling air flow converter degree of protection IP55 mounting",
        ["minimum clearances", "cooling air", "ip55", "mounting"],
    ),
    (
        "electrical",
        "connecting line system motor braking resistor shielded cables EMC converter",
        ["line system", "motor", "braking resistor", "emc"],
    ),
    (
        "commissioning-web",
        "commissioning web server SINAMICS G220 converter IP address parameter settings",
        ["commissioning", "web server", "parameter"],
    ),
    (
        "commissioning-startdrive",
        "commissioning Startdrive motor data factory settings firmware SINAMICS G220",
        ["startdrive", "commissioning", "motor"],
    ),
    (
        "diagnostics",
        "system messages faults alarms corrective maintenance fault code remedy converter",
        ["system messages", "fault", "alarm", "corrective maintenance"],
    ),
    (
        "technical-data",
        "technical data rated current overload ambient temperature SINAMICS G220 converter",
        ["technical data", "rated current", "ambient temperature"],
    ),
    (
        "options",
        "memory card function ordering data option module OM-SMT OM-IIoT expansion adapter",
        ["memory card", "ordering data", "option module"],
    ),
]


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Fast-lane industrial PDF ingest benchmark: page text extraction, chunking, sparse index, retrieval score."
    )
    parser.add_argument("--pdf", type=Path, default=DEFAULT_PDF)
    parser.add_argument("--max-chars", type=int, default=1800)
    parser.add_argument("--overlap", type=int, default=180)
    parser.add_argument("--json", action="store_true", help="Emit full JSON only.")
    args = parser.parse_args()

    result = run(args.pdf, args.max_chars, args.overlap)
    if args.json:
        print(json.dumps(result, ensure_ascii=True, indent=2))
        return 0 if result["verdict"] == "VALIDATED" else 1

    print("[SPIKE] 047 fast-lane industrial PDF ingest")
    print(
        "[FILE] path={path} size={mib:.2f}MiB pages={pages} chars={chars} chunks={chunks}".format(
            **result["file"]
        )
    )
    timing = result["timing_sec"]
    print(
        "[INGEST] extract={extract:.3f}s chunk={chunk:.3f}s sparse_index={sparse_index:.3f}s total_fast_ingest={total_fast_ingest:.3f}s".format(
            **timing
        )
    )
    print(
        "[RETRIEVAL] avg={retrieval_avg:.4f}s p95={retrieval_p95:.4f}s queries={queries}".format(
            **timing
        )
    )
    score = result["industrial_score"]
    print(
        "[SCORE] industrial={score_0_100:.1f}/100 grade={grade} rubric={rubric}".format(
            **score
        )
    )
    print("[RESULTS]")
    for row in result["queries"]:
        print(
            "  {category}: points={points_0_5:.2f}/5 top_page={top_page} latency={seconds:.4f}s hits={hits}".format(
                category=row["category"],
                points_0_5=row["points_0_5"],
                top_page=row["top_page"],
                seconds=row["seconds"],
                hits=",".join(row["top1_hits"]) or "-",
            )
        )
        print("    " + row["snippet"])
    print(
        "[SUMMARY] verdict={verdict} fast_ingest={fast:.3f}s retrieval_p95={p95:.4f}s industrial_score={score:.1f}".format(
            verdict=result["verdict"],
            fast=timing["total_fast_ingest"],
            p95=timing["retrieval_p95"],
            score=score["score_0_100"],
        )
    )
    return 0 if result["verdict"] == "VALIDATED" else 1


def run(pdf: Path, max_chars: int, overlap: int) -> dict:
    if not pdf.exists():
        raise SystemExit(f"PDF not found: {pdf}")

    t0 = time.perf_counter()
    doc = fitz.open(pdf)
    metadata = dict(doc.metadata or {})
    pages: list[dict] = []
    empty_pages = 0
    low_text_pages = 0
    for page_index, page in enumerate(doc):
        text = page.get_text("text") or ""
        stripped = text.strip()
        if not stripped:
            empty_pages += 1
        if len(stripped) < 80:
            low_text_pages += 1
        pages.append({"page": page_index + 1, "text": text})
    doc.close()
    extract_seconds = time.perf_counter() - t0

    t1 = time.perf_counter()
    chunks: list[dict] = []
    for page in pages:
        for start, end, text in split_text(page["text"], max_chars, overlap):
            chunks.append(
                {
                    "id": len(chunks),
                    "page": page["page"],
                    "start": start,
                    "end": end,
                    "text": text,
                }
            )
    chunk_seconds = time.perf_counter() - t1

    texts = [chunk["text"] for chunk in chunks]
    t2 = time.perf_counter()
    vectorizer = TfidfVectorizer(
        lowercase=True,
        ngram_range=(1, 2),
        strip_accents="unicode",
        sublinear_tf=True,
        max_df=0.92,
    )
    sparse = normalize(vectorizer.fit_transform(texts), axis=1)
    sparse_seconds = time.perf_counter() - t2

    retrieval_rows: list[dict] = []
    latencies: list[float] = []
    points_total = 0.0
    categories_hit = set()
    for category, query, must_terms in QUERIES:
        tq = time.perf_counter()
        qv = normalize(vectorizer.transform([query]), axis=1)
        scores = (sparse @ qv.T).toarray().ravel()
        order = np.argsort(-scores)[:5]
        elapsed = time.perf_counter() - tq
        latencies.append(elapsed)

        top = texts[int(order[0])] if len(order) else ""
        top5 = "\n".join(texts[int(i)] for i in order)
        top1_hits = [term for term in must_terms if contains(top, term)]
        top5_hits = [term for term in must_terms if contains(top5, term)]
        query_points = (3 * len(top1_hits) / len(must_terms)) + (
            2 * len(top5_hits) / len(must_terms)
        )
        points_total += query_points
        if query_points >= 3.5:
            categories_hit.add(category)

        top_idx = int(order[0]) if len(order) else -1
        retrieval_rows.append(
            {
                "category": category,
                "seconds": round(elapsed, 4),
                "points_0_5": round(query_points, 2),
                "top_page": chunks[top_idx]["page"] if top_idx >= 0 else None,
                "top1_hits": top1_hits,
                "top5_hits": top5_hits,
                "snippet": clean(top[:320]),
            }
        )

    p95 = sorted(latencies)[min(len(latencies) - 1, math.ceil(len(latencies) * 0.95) - 1)]
    avg_chars = sum(len(page["text"]) for page in pages) / max(1, len(pages))
    page_coverage = (len(pages) - empty_pages) / max(1, len(pages))
    low_text_ratio = low_text_pages / max(1, len(pages))

    ingest_score = 0.0
    ingest_score += 10 if extract_seconds < 2 else 7 if extract_seconds < 5 else 4
    ingest_score += 8 if page_coverage >= 0.98 else 6 if page_coverage >= 0.95 else 3
    ingest_score += 6 if avg_chars >= 1200 else 4 if avg_chars >= 800 else 2
    ingest_score += 6 if len(chunks) > 0 and max_chars > overlap else 0
    ingest_score += 6 if sparse_seconds < 1 else 4 if sparse_seconds < 3 else 2
    ingest_score += 4 if low_text_ratio <= 0.05 else 2
    retrieval_score = points_total + (
        10 if p95 < 0.05 else 8 if p95 < 0.2 else 5 if p95 < 1 else 2
    )
    essential = {
        "safety",
        "mechanical",
        "electrical",
        "commissioning-web",
        "diagnostics",
        "technical-data",
    }
    coverage_score = 6 * len(categories_hit.intersection(essential)) / len(essential)
    citation_score = 2 if all(row["top_page"] for row in retrieval_rows) else 0
    terminology_terms = [
        "danger",
        "warning",
        "ip55",
        "braking resistor",
        "commissioning",
        "fault",
        "technical data",
        "memory card",
    ]
    top_text = "\n".join(row["snippet"].lower() for row in retrieval_rows)
    terminology_score = 2 * sum(1 for term in terminology_terms if term in top_text) / len(
        terminology_terms
    )
    industrial_score = ingest_score + retrieval_score + coverage_score + citation_score + terminology_score

    verdict = "VALIDATED"
    if industrial_score < 75 or extract_seconds + chunk_seconds + sparse_seconds > 5 or p95 > 0.2:
        verdict = "PARTIAL"

    return {
        "verdict": verdict,
        "file": {
            "path": str(pdf),
            "bytes": pdf.stat().st_size,
            "mib": pdf.stat().st_size / (1 << 20),
            "pages": len(pages),
            "chars": sum(len(page["text"]) for page in pages),
            "chunks": len(chunks),
            "empty_pages": empty_pages,
            "low_text_pages_lt80": low_text_pages,
            "metadata": {key: value for key, value in metadata.items() if value},
        },
        "timing_sec": {
            "extract": extract_seconds,
            "chunk": chunk_seconds,
            "sparse_index": sparse_seconds,
            "total_fast_ingest": extract_seconds + chunk_seconds + sparse_seconds,
            "retrieval_avg": statistics.mean(latencies),
            "retrieval_p95": p95,
            "queries": len(QUERIES),
        },
        "industrial_score": {
            "score_0_100": industrial_score,
            "grade": grade(industrial_score),
            "rubric": {
                "ingest_index_quality_0_40": round(ingest_score, 1),
                "retrieval_quality_0_50": round(retrieval_score, 1),
                "industrial_usefulness_0_10": round(
                    coverage_score + citation_score + terminology_score, 1
                ),
            },
            "categories_hit": sorted(categories_hit),
        },
        "queries": retrieval_rows,
    }


def split_text(text: str, max_chars: int, overlap: int) -> list[tuple[int, int, str]]:
    text = text.strip()
    if not text:
        return []
    chunks: list[tuple[int, int, str]] = []
    start = 0
    while start < len(text):
        end = min(len(text), start + max_chars)
        if end < len(text):
            window = text[start:end]
            cut = max(window.rfind("\n"), window.rfind(". "), window.rfind("; "))
            if cut > max_chars * 0.55:
                end = start + cut + 1
        chunk = text[start:end].strip()
        if chunk:
            chunks.append((start, end, chunk))
        if end >= len(text):
            break
        start = max(0, end - overlap)
    return chunks


def clean(text: str) -> str:
    cleaned = re.sub(r"\s+", " ", text or "").strip()
    return cleaned.encode("ascii", "replace").decode("ascii")


def contains(text: str, term: str) -> bool:
    return term.lower() in text.lower()


def grade(score: float) -> str:
    if score >= 85:
        return "excellent"
    if score >= 75:
        return "good"
    if score >= 65:
        return "usable"
    return "weak"


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except ModuleNotFoundError as exc:
        print(f"missing Python dependency: {exc}", file=sys.stderr)
        print("expected local deps: pymupdf, scikit-learn, numpy", file=sys.stderr)
        raise SystemExit(2)
