"""Step 1: the passages Aura's ingest would write, for every file of the two fixture corpora.

Calls the production modules (services/ingest) rather than a copy: media.index_text, outline,
app._card (aura-filecard) and the same rows/chunk branch as app.index_object. Nothing is
embedded and nothing is written to ArcadeDB -- the spike needs the passage TEXT only.
"""

import json
import os
import pathlib
import sys

# app.py reads its deployment settings at import; none of them is used here.
for _name in ("ARCADEDB_PASSWORD", "AURA_EMBED_SPACE", "AURA_INGEST_S3_BUCKET",
              "AURA_INGEST_S3_ACCESS_KEY_ID", "AURA_INGEST_S3_SECRET_ACCESS_KEY"):
    os.environ.setdefault(_name, "spike-unused")
os.environ.setdefault("AURA_INGEST_IDENTITY_ID", "00000000-0000-4000-8000-000000000000")
from ingest import app, chunk, media, outline, rows  # noqa: E402

FIXTURES = pathlib.Path(__file__).resolve().parents[2] / "scripts/fixtures/document_retrieval_eval"


def passages(path: pathlib.Path) -> list[dict]:
    text = media.index_text(str(path), path.name)
    anchors = outline.anchors_in(text, outline.titles_of(str(path)))
    card = app._card(str(path), path.name)
    if card.rows:
        pieces = rows.group(card.rows, path.name)
    else:
        pieces = chunk.chunk(text, max_tokens=chunk.document_budget(), anchors=anchors)
    return [{"doc": path.name, "ordinal": i, "text": p.text, "tokens": chunk.count_tokens(p.text)}
            for i, p in enumerate(pieces)]


def main(out: str) -> None:
    with open(out, "w", encoding="utf-8") as sink:
        for corpus in ("corpus", "complex_corpus"):
            for path in sorted((FIXTURES / corpus).iterdir()):
                found = passages(path)
                print(f"{corpus}/{path.name}: {len(found)} passages", file=sys.stderr)
                for row in found:
                    sink.write(json.dumps({"corpus": corpus, **row}, ensure_ascii=False) + "\n")


if __name__ == "__main__":
    main(sys.argv[1])
