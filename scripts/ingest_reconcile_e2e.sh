#!/usr/bin/env bash
# Task 6's own test -- proves what is OURS, not CocoIndex's reconciliation.
#
# spikes/cocoindex-ingestion/FINDINGS.md's "S3 incrementale" already measured add/modify/
# delete reconciliation against Garage (3 added / 3 unchanged in 0.1s / 1 reprocessed /
# delete removes all 249 of a document's passages) "senza una riga di codice nostro" --
# re-measuring that here would be test babysitting of the library, not of this pipeline.
#
# What IS ours, and unproven before this script:
#   (a) the passage identity is search_document_id(identity, "s3", key), not the walker's
#       prefix-relative path -- the F0 defect FINDINGS §5b recorded in the spike.
#   (b) the written Passage carries arcade.py's full field set, not five fields.
#   (c) the real ingest.extract (iscc-tika + LibreOffice) and ingest.chunk are in the
#       chain, not the withdrawn MarkItDown / naive char splitter.
#   (d) live mode (auto_refresh + update_blocking(live=True)) really performs a SECOND
#       cycle on an object added after the process started.
# Plus two wiring probes for OUR bugs, not CocoIndex's: rerun-unchanged -> zero
# re-extractions (an ephemeral LMDB or an unstable component path would both show up
# here), and delete -> the deleted document's passages are gone.
#
# Runs against the REAL stack: a disposable Garage bucket + key and a disposable
# ArcadeDB database, both created and torn down here -- NEVER aura_memory or any
# mem_<uuid>, which are production identities. Run in WSL, not Git Bash.
set -euo pipefail
export MSYS_NO_PATHCONV=1

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
cd "$repo_root"

img="${AURA_INGEST_IMAGE:-aura-ingest:local}"
net="${AURA_NETWORK:-aura_default}"
suffix="$(date +%s)-$$"
bucket="aura-ingest-e2e-${suffix}"
key_name="aura-ingest-e2e-${suffix}"
arcade_db="aura_ingest_e2e_$(printf '%s' "$suffix" | tr -c '0-9a-zA-Z' '_')"
volume="aura-ingest-e2e-state-${suffix}"
identity_id="aura-ingest-e2e-identity-${suffix}"

for c in aura-arcadedb aura-garage aura-llama-embed; do
  if [ "$(docker inspect -f '{{.State.Running}}' "$c" 2>/dev/null || echo false)" != "true" ]; then
    echo "FAIL: $c is not running -- bring the stack up first" >&2
    exit 1
  fi
done

arcade_pw="$(docker inspect aura-arcadedb --format '{{range .Config.Env}}{{println .}}{{end}}' \
  | grep -oP '(?<=rootPassword=)\S+')"

scratch="$(mktemp -d)"
access_key=""
secret_key=""
container_id=""

# Everything that talks to ArcadeDB or S3 runs INSIDE aura-ingest:local, on the compose
# network, reusing the same modules the pipeline itself uses (ingest.arcade._post for
# queries, aiobotocore for S3) -- one execution model, no host-side curl/jq/python
# dependency to keep in sync with the image's own library versions.
run_py() {  # run_py <heredoc-on-stdin>, with ARCADE_*/S3_* already exported below
  docker run --rm -i --network "$net" \
    -e ARCADE_HTTP="http://aura-arcadedb:2480" -e ARCADE_DB="$arcade_db" -e ARCADEDB_PASSWORD="$arcade_pw" \
    -e S3_ENDPOINT="http://aura-garage:3900" -e S3_ACCESS_KEY="$access_key" -e S3_SECRET_KEY="$secret_key" \
    -e S3_BUCKET="$bucket" -e IDENTITY_ID="$identity_id" \
    -e AURA_INGEST_IDENTITY_ID="$identity_id" -e AURA_INGEST_S3_BUCKET="$bucket" \
    -e AURA_INGEST_S3_ACCESS_KEY_ID="$access_key" -e AURA_INGEST_S3_SECRET_ACCESS_KEY="$secret_key" \
    -v "$repo_root/scripts/fixtures/document_pipeline_e2e:/fixtures:ro" \
    --entrypoint python "$img" -
}

cleanup() {
  local ec=$?
  set +e
  if [ -n "$container_id" ]; then
    docker stop "$container_id" >/dev/null 2>&1
    docker rm "$container_id" >/dev/null 2>&1
  fi
  if [ -n "$access_key" ]; then
    run_py <<'PY' >/dev/null 2>&1
import asyncio, os
from aiobotocore.session import get_session

async def main():
    session = get_session()
    async with session.create_client(
        "s3", endpoint_url=os.environ["S3_ENDPOINT"], aws_access_key_id=os.environ["S3_ACCESS_KEY"],
        aws_secret_access_key=os.environ["S3_SECRET_KEY"], region_name="garage") as c:
        paginator = c.get_paginator("list_objects_v2")
        async for page in paginator.paginate(Bucket=os.environ["S3_BUCKET"]):
            for obj in page.get("Contents", []):
                await c.delete_object(Bucket=os.environ["S3_BUCKET"], Key=obj["Key"])

asyncio.run(main())
PY
    docker exec aura-garage /garage bucket delete --yes "$bucket" >/dev/null 2>&1
    docker exec aura-garage /garage key delete --yes "$access_key" >/dev/null 2>&1
  fi
  run_py <<'PY' >/dev/null 2>&1
import os
from ingest.arcade import _post
try:
    _post("http://aura-arcadedb:2480", "/api/v1/server", {"command": f"drop database {os.environ['ARCADE_DB']}"},
          ("root", os.environ["ARCADEDB_PASSWORD"]), 30.0)
except Exception:
    pass
PY
  docker volume rm "$volume" >/dev/null 2>&1
  rm -rf "$scratch"
  exit "$ec"
}
trap cleanup EXIT

echo "== disposable fixtures: bucket=$bucket db=$arcade_db identity=$identity_id =="
key_output="$(docker exec aura-garage /garage key create "$key_name" 2>&1)"
access_key="$(printf '%s\n' "$key_output" | grep -oP '(?<=Key ID:)\s*\K\S+')"
secret_key="$(printf '%s\n' "$key_output" | grep -oP '(?<=Secret key:)\s*\K\S+')"
[ -n "$access_key" ] && [ -n "$secret_key" ] || { echo "FAIL: could not parse garage key create output" >&2; exit 1; }
docker exec aura-garage /garage bucket create "$bucket" >/dev/null
docker exec aura-garage /garage bucket allow --read --write --owner "$bucket" --key "$access_key" >/dev/null

# EXTRACT_COUNT is set by run_pass -- the number of "[extract] <key>" lines printed,
# which app.py's process_file logs only when memo=True actually let its body execute.
# Zero on an unchanged rerun is wiring probe 1; the log line IS the observable.
EXTRACT_COUNT=0
run_pass() {
  local log="$scratch/run.log"
  docker run --rm --network "$net" \
    -e ARCADEDB_PASSWORD="$arcade_pw" -e ARCADE_HTTP="http://aura-arcadedb:2480" \
    -e ARCADE_BOLT="bolt://aura-arcadedb:7687" -e ARCADE_DB="$arcade_db" \
    -e AURA_INGEST_IDENTITY_ID="$identity_id" \
    -e AURA_INGEST_S3_ENDPOINT="http://aura-garage:3900" -e AURA_INGEST_S3_BUCKET="$bucket" \
    -e AURA_INGEST_S3_ACCESS_KEY_ID="$access_key" -e AURA_INGEST_S3_SECRET_ACCESS_KEY="$secret_key" \
    -v "$volume:/state" \
    "$img" > "$log" 2>&1
  cat "$log"
  EXTRACT_COUNT="$(grep -c '^\[extract\]' "$log" || true)"
}

echo "== Step 1: three objects, one catch-up pass =="
run_py <<'PY'
import asyncio, os
from aiobotocore.session import get_session

DOCS = {
    "alpha.txt": "Alpha document about apple orchards in springtime, filler filler filler filler.",
    "beta.txt": "Beta document about basalt columns on volcanic coastlines, filler filler filler.",
    "gamma.txt": "Gamma document about glacier melt rates in the Alps, filler filler filler filler.",
}

async def main():
    session = get_session()
    async with session.create_client(
        "s3", endpoint_url=os.environ["S3_ENDPOINT"], aws_access_key_id=os.environ["S3_ACCESS_KEY"],
        aws_secret_access_key=os.environ["S3_SECRET_KEY"], region_name="garage") as c:
        for key, text in DOCS.items():
            await c.put_object(Bucket=os.environ["S3_BUCKET"], Key=key, Body=text.encode("utf-8"))

asyncio.run(main())
PY
run_pass
[ "$EXTRACT_COUNT" -eq 3 ] || { echo "FAIL: expected 3 extractions on the first pass, got $EXTRACT_COUNT" >&2; exit 1; }
run_py <<'PY'
import os
from ingest.arcade import _post
rows = _post("http://aura-arcadedb:2480", f"/api/v1/query/{os.environ['ARCADE_DB']}",
             {"language": "sql", "command": "SELECT count(*) as n FROM Passage"},
             ("root", os.environ["ARCADEDB_PASSWORD"]), 30.0)["result"]
n = rows[0]["n"]
assert n == 3, f"expected 3 Passage rows, found {n}"
print("ok: 3 objects -> 3 extractions -> 3 Passage rows")
PY

echo "== Assertion (a): passage identity is search_document_id, not the walker path =="
run_py <<'PY'
import os
from ingest.arcade import _post
from ingest.identity import search_document_id

expected = search_document_id(os.environ["IDENTITY_ID"], "s3", "alpha.txt")
rows = _post("http://aura-arcadedb:2480", f"/api/v1/query/{os.environ['ARCADE_DB']}",
             {"language": "sql",
              "command": f"SELECT search_document_id, document_id FROM Passage WHERE document_id = '{expected}'"},
             ("root", os.environ["ARCADEDB_PASSWORD"]), 30.0)["result"]
assert rows, f"no Passage row has document_id={expected!r} -- did identity derivation drift?"
stored = rows[0]["search_document_id"]
assert stored == expected, (
    f"stored search_document_id ({stored!r}) != identity.search_document_id() ({expected!r}); "
    "if it equals the literal walker path 'alpha.txt' the F0 defect is back")
print(f"ok: search_document_id == identity.search_document_id(identity, 's3', 'alpha.txt'), not the walker path")
PY

echo "== Assertion (b): the full arcade.py field set, not five fields =="
# Two checks, because ArcadeDB's Bolt plugin follows Neo4j's own SET n += \$props
# semantics: a None value in the props map REMOVES that property from the vertex
# rather than storing a null -- measured directly (INSERT ... SET b = null keeps the
# key; CocoIndex's declare_record with a None field does not). So a live row can only
# ever show the fields THIS pipeline populates with real values; the other arcade.py
# fields (layout/version/projection -- genuinely inapplicable to plain-text chunking,
# see app.py's Passage docstring) are proven by the STRUCTURAL check instead.
run_py <<'PY'
import dataclasses
import ingest.app as m

names = {f.name for f in dataclasses.fields(m.Passage)}
expected = {
    "passage_key", "passage_id", "projection_key", "document_id", "search_document_id",
    "version_id", "version_number", "raw_sha256", "pipeline_generation", "schema_version",
    "ordinal", "text", "normalized_text_sha256", "self_ref", "heading_path", "captions",
    "page_number", "bbox_left", "bbox_top", "bbox_right", "bbox_bottom", "char_start",
    "char_end", "sheet_name", "table_name", "row_number", "column_number", "cell_reference",
    "embedding", "active", "created_at", "tombstoned_at",
}
assert names == expected, f"Passage field set drifted from arcade.py's DDL: {names ^ expected}"
print(f"ok: Passage declares all {len(expected)} arcade.py fields (the spike's Passage had 5)")
PY
run_py <<'PY'
import os
from ingest.arcade import _post
from ingest.identity import search_document_id

doc_id = search_document_id(os.environ["IDENTITY_ID"], "s3", "alpha.txt")
rows = _post("http://aura-arcadedb:2480", f"/api/v1/query/{os.environ['ARCADE_DB']}",
             {"language": "sql", "command": f"SELECT * FROM Passage WHERE document_id = '{doc_id}'"},
             ("root", os.environ["ARCADEDB_PASSWORD"]), 30.0)["result"]
row = rows[0]
populated = {
    "passage_key", "passage_id", "document_id", "search_document_id", "raw_sha256",
    "pipeline_generation", "schema_version", "ordinal", "text", "normalized_text_sha256",
    "heading_path", "char_start", "char_end", "embedding", "active", "created_at",
}
missing = populated - row.keys()
assert not missing, f"missing fields that should always be non-null: {sorted(missing)}"
assert len(row["embedding"]) == 768, f"embedding width {len(row['embedding'])} != 768"
assert row["active"] is True
assert row["pipeline_generation"] and row["schema_version"] and row["raw_sha256"]
print(f"ok: all {len(populated)} populated fields present live (raw_sha256/pipeline_generation/"
      f"schema_version/normalized_text_sha256 did not exist in the spike's Passage at all)")
PY

echo "== Assertion (c): the real extractor+chunker are in the chain =="
run_py <<'PY'
import asyncio, os
from aiobotocore.session import get_session

async def main():
    data = open("/fixtures/sample.xls", "rb").read()
    session = get_session()
    async with session.create_client(
        "s3", endpoint_url=os.environ["S3_ENDPOINT"], aws_access_key_id=os.environ["S3_ACCESS_KEY"],
        aws_secret_access_key=os.environ["S3_SECRET_KEY"], region_name="garage") as c:
        await c.put_object(Bucket=os.environ["S3_BUCKET"], Key="order.xls", Body=data)

asyncio.run(main())
PY
run_pass
[ "$EXTRACT_COUNT" -eq 1 ] || { echo "FAIL: expected exactly 1 extraction for the new file, got $EXTRACT_COUNT" >&2; exit 1; }
run_py <<'PY'
import os
from ingest.arcade import _post
from ingest.identity import search_document_id

doc_id = search_document_id(os.environ["IDENTITY_ID"], "s3", "order.xls")
rows = _post("http://aura-arcadedb:2480", f"/api/v1/query/{os.environ['ARCADE_DB']}",
             {"language": "sql", "command": f"SELECT text FROM Passage WHERE document_id = '{doc_id}'"},
             ("root", os.environ["ARCADEDB_PASSWORD"]), 30.0)["result"]
text = " ".join(r["text"] for r in rows)
# GROUND_TRUTH.txt: "CODE: A9A26924 appears exactly once, with Quantita 11" -- this table
# has no plain-text sentence, so recovering these tokens requires the real .xls -> .xlsx
# LibreOffice normalisation followed by real iscc-tika extraction. MarkItDown fails
# outright on .xls (FINDINGS.md), so this could not pass on the withdrawn extractor.
for token in ("A9A26924", "Descrizione", "Quantita"):
    assert token in text, f"{token!r} not found in extracted .xls text: {text!r}"
print("ok: legacy .xls normalised by LibreOffice and extracted by iscc-tika, matching GROUND_TRUTH.txt")
PY

echo "== Wiring probe 1: rerun unchanged -> zero re-extractions =="
run_pass
[ "$EXTRACT_COUNT" -eq 0 ] || { echo "FAIL: expected 0 re-extractions on an unchanged rerun, got $EXTRACT_COUNT (ephemeral LMDB or unstable component key?)" >&2; exit 1; }
echo "ok: 0 re-extractions on an unchanged rerun"

echo "== Modify one, delete one, wiring probe 2: delete -> passages gone =="
run_py <<'PY'
import asyncio, os
from aiobotocore.session import get_session

async def main():
    session = get_session()
    async with session.create_client(
        "s3", endpoint_url=os.environ["S3_ENDPOINT"], aws_access_key_id=os.environ["S3_ACCESS_KEY"],
        aws_secret_access_key=os.environ["S3_SECRET_KEY"], region_name="garage") as c:
        await c.put_object(Bucket=os.environ["S3_BUCKET"], Key="alpha.txt",
                            Body=b"Alpha document REVISED about apple orchards after a late frost, "
                                 b"filler filler filler filler filler.")
        await c.delete_object(Bucket=os.environ["S3_BUCKET"], Key="beta.txt")

asyncio.run(main())
PY
run_pass
[ "$EXTRACT_COUNT" -eq 1 ] || { echo "FAIL: expected exactly 1 re-extraction (the modified file), got $EXTRACT_COUNT" >&2; exit 1; }
run_py <<'PY'
import os
from ingest.arcade import _post
from ingest.identity import search_document_id

beta_id = search_document_id(os.environ["IDENTITY_ID"], "s3", "beta.txt")
rows = _post("http://aura-arcadedb:2480", f"/api/v1/query/{os.environ['ARCADE_DB']}",
             {"language": "sql", "command": f"SELECT count(*) as n FROM Passage WHERE document_id = '{beta_id}'"},
             ("root", os.environ["ARCADEDB_PASSWORD"]), 30.0)["result"]
n = rows[0]["n"]
assert n == 0, f"beta.txt's passages survived deletion ({n} rows) -- no delete worker of ours should have been needed"
print("ok: deleted object's passages are gone, no delete worker of ours")
PY

echo "== Assertion (d): live mode really performs a second cycle =="
container_id="$(docker run -d --network "$net" \
  -e ARCADEDB_PASSWORD="$arcade_pw" -e ARCADE_HTTP="http://aura-arcadedb:2480" \
  -e ARCADE_BOLT="bolt://aura-arcadedb:7687" -e ARCADE_DB="$arcade_db" \
  -e AURA_INGEST_IDENTITY_ID="$identity_id" \
  -e AURA_INGEST_S3_ENDPOINT="http://aura-garage:3900" -e AURA_INGEST_S3_BUCKET="$bucket" \
  -e AURA_INGEST_S3_ACCESS_KEY_ID="$access_key" -e AURA_INGEST_S3_SECRET_ACCESS_KEY="$secret_key" \
  -e AURA_INGEST_LIVE=1 -e AURA_INGEST_INTERVAL_SEC=5 \
  -v "$volume:/state" "$img")"
sleep 3
run_py <<'PY'
import asyncio, os
from aiobotocore.session import get_session

async def main():
    session = get_session()
    async with session.create_client(
        "s3", endpoint_url=os.environ["S3_ENDPOINT"], aws_access_key_id=os.environ["S3_ACCESS_KEY"],
        aws_secret_access_key=os.environ["S3_SECRET_KEY"], region_name="garage") as c:
        await c.put_object(Bucket=os.environ["S3_BUCKET"], Key="delta.txt",
                            Body=b"Delta document added while the app was already running, "
                                 b"filler filler filler filler.")

asyncio.run(main())
PY
sleep 10
delta_lines="$(docker logs "$container_id" 2>&1 | grep -c '^\[extract\] delta.txt' || true)"
docker stop "$container_id" >/dev/null; docker rm "$container_id" >/dev/null; container_id=""
[ "$delta_lines" -ge 1 ] || { echo "FAIL: an object added after the live process started was never picked up -- auto_refresh is not re-scanning" >&2; exit 1; }
run_py <<'PY'
import os
from ingest.arcade import _post
from ingest.identity import search_document_id

delta_id = search_document_id(os.environ["IDENTITY_ID"], "s3", "delta.txt")
rows = _post("http://aura-arcadedb:2480", f"/api/v1/query/{os.environ['ARCADE_DB']}",
             {"language": "sql", "command": f"SELECT count(*) as n FROM Passage WHERE document_id = '{delta_id}'"},
             ("root", os.environ["ARCADEDB_PASSWORD"]), 30.0)["result"]
n = rows[0]["n"]
assert n == 1, f"delta.txt was extracted but no Passage row landed ({n})"
print("ok: object added after the live process started was reconciled within one AURA_INGEST_INTERVAL_SEC, no restart")
PY

echo
echo "ok: ingest reconciliation -- 4/4 assertions, 2/2 wiring probes"
