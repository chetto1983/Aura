"""Run inside the ingestion image with disposable S3/ArcadeDB credentials.

Uses the production CocoIndex app, including S3 discovery and target reconciliation.
The caller owns provisioning/cleanup and must provide a dedicated identity/bucket.
"""

import argparse
import asyncio
import cProfile
import hashlib
import json
import pathlib
import pstats
import time

from ingest import app, source


def query(command):
    return app.arcade._post(
        app.ARCADE_HTTP, f"/api/v1/query/{app.ARCADE_DB}",
        {"language": "sql", "command": command},
        ("root", app.ARCADE_PASSWORD), 120,
    )["result"]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("pdf", type=pathlib.Path)
    parser.add_argument("output", type=pathlib.Path)
    parser.add_argument("--reference", required=True, type=pathlib.Path)
    parser.add_argument("--minimum-coverage", type=float, default=1.0)
    args = parser.parse_args()
    if not 0 <= args.minimum_coverage <= 1:
        parser.error("--minimum-coverage must be between zero and one")
    args.output.mkdir(parents=True, exist_ok=True)
    content = args.pdf.read_bytes()
    reference = args.reference.read_text()

    async def upload():
        async with source.create_client(app._S3_CONFIG) as client:
            await client.put_object(
                Bucket=app._S3_CONFIG.bucket, Key=args.pdf.name, Body=content,
            )

    start = time.perf_counter()
    asyncio.run(upload())
    upload_s = time.perf_counter() - start
    runs = []
    for label in ("initial", "unchanged"):
        profile = cProfile.Profile()
        start = time.perf_counter()
        profile.enable()
        app.app.update_blocking()
        profile.disable()
        elapsed = time.perf_counter() - start
        assert app.audit_pass() == 0, "source missing from index"
        profile.dump_stats(str(args.output / f"{label}.prof"))
        stats = pstats.Stats(profile)
        stages = {}
        for (filename, _, name), (primitive, calls, own, total, _) in stats.stats.items():
            if "/ingest/" in filename and name in {
                "extract_text", "_card", "chunk", "_embed", "_embed_once", "_windows",
            }:
                stages[f"{pathlib.Path(filename).name}:{name}"] = {
                    "calls": calls, "seconds": total,
                }
        run = {"label": label, "seconds": elapsed, "stages": stages}
        runs.append(run)
        print(json.dumps(run), flush=True)

    rows = query("SELECT * FROM Passage ORDER BY ordinal")
    documents = query("SELECT FROM IndexedDocument")
    assert len(documents) == 1
    assert documents[0]["passage_count"] == len(rows)
    raw_hash = hashlib.sha256(content).hexdigest()
    assert all(r["raw_sha256"] == raw_hash for r in rows)
    assert all(len(r["embedding"]) == app.EMBED_DIMENSIONS for r in rows)
    assert all(reference[r["char_start"]:r["char_end"]] == r["text"] for r in rows)
    assert all(hashlib.sha256(r["text"].encode()).hexdigest() == r["normalized_text_sha256"] for r in rows)
    covered = bytearray(len(reference))
    for row in rows:
        covered[row["char_start"]:row["char_end"]] = b"\1" * len(row["text"])
    nonspace = sum(not c.isspace() for c in reference)
    covered_nonspace = sum(bool(covered[i]) and not c.isspace() for i, c in enumerate(reference))
    result = {
        "file": args.pdf.name, "bytes": len(content), "sha256": raw_hash,
        "upload_seconds": upload_s, "runs": runs, "passages": len(rows),
        "reference_chars": len(reference), "covered_nonspace_chars": covered_nonspace,
        "reference_nonspace_chars": nonspace,
        "text_coverage": covered_nonspace / nonspace,
        "last_character": max(r["char_end"] for r in rows),
        "identity": app._S3_CONFIG.identity_id, "database": app.ARCADE_DB,
        "offsets_hashes_dimensions_valid": True,
    }
    (args.output / "passages.json").write_text(json.dumps(rows))
    (args.output / "result.json").write_text(json.dumps(result, indent=2))
    print(json.dumps(result), flush=True)
    assert result["text_coverage"] >= args.minimum_coverage, "document text is missing from the index"


if __name__ == "__main__":
    main()
