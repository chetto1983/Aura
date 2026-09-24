"""A route change re-embeds every document once, re-extracts none, and never mislabels a row.

Spec §6 measured end to end on the production indexing path (space_driver.py) against a real
ArcadeDB: four catch-up passes over one CocoIndex state.

1. Space A: both files are extracted, embedded and stamped A.
2. Space A again: nothing runs, because the memo holds.
3. Space B, which refuses the refused file's last chunk:
   - nothing is re-extracted, because _extract is memoized by content, apart from embedding;
   - the healthy file is re-embedded and stamped B;
   - the refused file keeps all its A rows, its healthy first chunk included (CocoIndex's
     contract for a failing component), where the documents gate sees them and stays closed.
   Whether the refused chunk shared a request with a healthy one here depends on timing
   across components; test_embed_route.py pins the batch split itself.
4. Space A again: the healthy file returns to A. Memo entries are keyed by the fingerprint
   they were recorded under, so a change back is a change.

In every pass, every row's vector is the one the fake embedder produces for that row's own
stamp.
"""

import os
import re
import subprocess
import sys

import pytest

from ingest import chunk, embed
from ingest.arcade import ArcadeSchemaError, _post
from ingest.tests import fake_embedder

ARCADE_HTTP = os.environ.get("ARCADE_HTTP", "http://arcadedb:2480")
AUTH = ("root", os.environ["ARCADEDB_PASSWORD"])
DATABASE = "aura_t_space_reruns"
SPACE_A, SPACE_B = "es1-aaaaaaaaaaaaaaaa", "es1-bbbbbbbbbbbbbbbb"
HEALTHY, REFUSED = "fornitori.txt", "rifiutato.txt"
# Past one chunk under either tokenizer (the sidecar's, or chunk.py's 3 chars/token fallback),
# so the marker lands in a second chunk and the file also has a chunk the route accepts.
_FILLER = "La nota elenca i controlli di qualità eseguiti su ogni lotto prima della spedizione. " * 150
OBJECTS = [
    f"{HEALTHY}=Il fornitore consegna i ricambi ogni martedì mattina.",
    f"{REFUSED}={_FILLER}In fondo la nota contiene {fake_embedder.REFUSED}.",
]


def _drop() -> None:
    try:
        _post(ARCADE_HTTP, "/api/v1/server", {"command": f"drop database {DATABASE}"}, AUTH, 30.0)
    except ArcadeSchemaError as exc:
        if "not exist" not in str(exc).lower():
            raise


@pytest.fixture
def database():
    _drop()
    try:
        yield DATABASE
    finally:
        _drop()


def _pass(state, space, refusing=""):
    env = dict(os.environ, AURA_EMBED_SPACE=space, COCOINDEX_DB=str(state), SPACE_THAT_REFUSES=refusing)
    done = subprocess.run(
        [sys.executable, "-m", "ingest.tests.space_driver", DATABASE, *OBJECTS],
        env=env, capture_output=True, text=True, timeout=600,
    )
    assert done.returncode == 0, done.stdout + done.stderr
    requests = int(re.search(r"^REQUESTS (\d+)$", done.stdout, re.MULTILINE).group(1))
    return done.stdout.count("[extract] "), requests


def _stamps() -> dict[str, set[str]]:
    """Each file's stamps, after checking that every row's vector is its own stamp's."""
    stamps: dict[str, set[str]] = {}
    for type_name, field in (("Passage", "text"), ("IndexedDocument", "card")):
        body = _post(ARCADE_HTTP, f"/api/v1/query/{DATABASE}", {
            "language": "sql",
            "command": f"SELECT source_key, embed_space, {field} AS sent, embedding FROM {type_name}",
        }, AUTH, 30.0)
        for row in body["result"]:
            stamps.setdefault(row["source_key"], set()).add(row["embed_space"])
            if not (row["sent"] or "").strip():
                continue  # an empty card stores a zero vector, out of scope for this design
            expected = fake_embedder.vector(row["embed_space"], chunk.EMBED_DOC_PREFIX + row["sent"],
                                            embed.DIMENSIONS)
            assert row["embedding"] == pytest.approx(expected, abs=1e-6), (
                f"{type_name} {row['source_key']}: the vector is not its stamp's"
            )
    return stamps


def test_a_route_change_re_embeds_once_re_extracts_nothing_and_never_mislabels(database, tmp_path):
    state = tmp_path / "coco.db"

    extracted, requests = _pass(state, SPACE_A)
    assert extracted == 2 and requests > 0
    assert _stamps() == {HEALTHY: {SPACE_A}, REFUSED: {SPACE_A}}

    assert _pass(state, SPACE_A) == (0, 0), "an unchanged pass re-ran: the memo does not hold"

    extracted, requests = _pass(state, SPACE_B, refusing=SPACE_B)
    assert extracted == 0, "a route change re-extracted: extraction is not memoized apart from embedding"
    assert requests > 0
    assert _stamps() == {HEALTHY: {SPACE_B}, REFUSED: {SPACE_A}}

    extracted, _ = _pass(state, SPACE_A)
    assert extracted == 0
    assert _stamps() == {HEALTHY: {SPACE_A}, REFUSED: {SPACE_A}}
