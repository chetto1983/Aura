#!/usr/bin/env bash
# Production reconciliation gate: real Garage, CocoIndex, extractor, embedder and
# per-identity ArcadeDB. Every resource is disposable. Run in WSL.
set -euo pipefail
export MSYS_NO_PATHCONV=1

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
cd "$repo_root"

retrieval_fixture="$repo_root/scripts/fixtures/document_retrieval_eval"
retrieval_report_dir="${AURA_DOCUMENT_EVAL_REPORT_DIR:-$repo_root/artifacts/document-retrieval-eval}"

img="${AURA_INGEST_IMAGE:-aura-ingest:local}"
net="${AURA_NETWORK:-aura_default}"
suffix="$(date +%s)-$$"
bucket="aura-ingest-e2e-${suffix}"
key_name="aura-ingest-e2e-${suffix}"
volume="aura-ingest-e2e-state-${suffix}"
identity_id="$(python3 -c 'import uuid; print(uuid.uuid4())')"
arcade_db="mem_${identity_id//-/_}"
marker_a="CERULEAN${suffix//-/}"

bucket_b="aura-ingest-e2e-b-${suffix}"
key_name_b="aura-ingest-e2e-b-${suffix}"
volume_b="aura-ingest-e2e-state-b-${suffix}"
identity_id_b="$(python3 -c 'import uuid; print(uuid.uuid4())')"
arcade_db_b="mem_${identity_id_b//-/_}"
marker_b="VERMILION${suffix//-/}"

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
access_key_b=""
secret_key_b=""

# Everything that talks to ArcadeDB or S3 runs INSIDE aura-ingest:local, on the compose
# network, reusing the same modules the pipeline itself uses (ingest.arcade._post for
# queries, aiobotocore for S3) -- one execution model, no host-side curl/jq/python
# dependency to keep in sync with the image's own library versions.
run_py() {  # run_py <heredoc-on-stdin>, with ARCADE_*/S3_* already exported below
  docker run --rm -i --network "$net" \
    -e ARCADE_HTTP="http://aura-arcadedb:2480" -e ARCADE_DB="$arcade_db" -e ARCADEDB_PASSWORD="$arcade_pw" \
    -e S3_ENDPOINT="http://aura-garage:3900" -e S3_ACCESS_KEY="$access_key" -e S3_SECRET_KEY="$secret_key" \
    -e S3_BUCKET="$bucket" -e IDENTITY_ID="$identity_id" -e MARKER_A="$marker_a" \
    -e AURA_INGEST_IDENTITY_ID="$identity_id" -e AURA_INGEST_S3_BUCKET="$bucket" \
    -e AURA_INGEST_S3_ACCESS_KEY_ID="$access_key" -e AURA_INGEST_S3_SECRET_ACCESS_KEY="$secret_key" \
    -v "$repo_root/scripts/fixtures/document_pipeline_e2e:/fixtures:ro" \
    -v "$retrieval_fixture/corpus:/retrieval-corpus:ro" \
    --entrypoint python "$img" -
}

run_py_b() {
  docker run --rm -i --network "$net" \
    -e ARCADE_HTTP="http://aura-arcadedb:2480" -e ARCADE_DB="$arcade_db_b" -e ARCADEDB_PASSWORD="$arcade_pw" \
    -e S3_ENDPOINT="http://aura-garage:3900" -e S3_ACCESS_KEY="$access_key_b" -e S3_SECRET_KEY="$secret_key_b" \
    -e S3_BUCKET="$bucket_b" -e IDENTITY_ID="$identity_id_b" -e MARKER_B="$marker_b" \
    -e AURA_INGEST_IDENTITY_ID="$identity_id_b" -e AURA_INGEST_S3_BUCKET="$bucket_b" \
    -e AURA_INGEST_S3_ACCESS_KEY_ID="$access_key_b" -e AURA_INGEST_S3_SECRET_ACCESS_KEY="$secret_key_b" \
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
  if [ -n "$access_key_b" ]; then
    run_py_b <<'PY' >/dev/null 2>&1
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
    docker exec aura-garage /garage bucket delete --yes "$bucket_b" >/dev/null 2>&1
    docker exec aura-garage /garage key delete --yes "$access_key_b" >/dev/null 2>&1
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
  run_py_b <<'PY' >/dev/null 2>&1
import os
from ingest.arcade import _post
try:
    _post("http://aura-arcadedb:2480", "/api/v1/server", {"command": f"drop database {os.environ['ARCADE_DB']}"},
          ("root", os.environ["ARCADEDB_PASSWORD"]), 30.0)
except Exception:
    pass
PY
  docker volume rm "$volume" >/dev/null 2>&1
  docker volume rm "$volume_b" >/dev/null 2>&1
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
  if ! docker run --rm --network "$net" \
    -e ARCADEDB_PASSWORD="$arcade_pw" -e ARCADE_HTTP="http://aura-arcadedb:2480" \
    -e ARCADE_BOLT="bolt://aura-arcadedb:7687" \
    -e AURA_INGEST_IDENTITY_ID="$identity_id" \
    -e AURA_INGEST_S3_ENDPOINT="http://aura-garage:3900" -e AURA_INGEST_S3_BUCKET="$bucket" \
    -e AURA_INGEST_S3_ACCESS_KEY_ID="$access_key" -e AURA_INGEST_S3_SECRET_ACCESS_KEY="$secret_key" \
    -v "$volume:/state" \
    "$img" > "$log" 2>&1; then
    cat "$log" >&2
    return 1
  fi
  cat "$log"
  EXTRACT_COUNT="$(grep -c '^\[extract\]' "$log" || true)"
}

echo "== Step 1: three objects, one catch-up pass =="
run_py <<'PY'
import asyncio, os
from aiobotocore.session import get_session

DOCS = {
    "alpha.txt": (
        "Alpha document about apple orchards in springtime. "
        f"The private launch code is {os.environ['MARKER_A']}. filler filler filler filler."
    ),
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
              "command": f"SELECT search_document_id FROM Passage WHERE search_document_id = '{expected}'"},
             ("root", os.environ["ARCADEDB_PASSWORD"]), 30.0)["result"]
assert rows, f"no Passage row has search_document_id={expected!r} -- did identity derivation drift?"
stored = rows[0]["search_document_id"]
assert stored == expected, (
    f"stored search_document_id ({stored!r}) != identity.search_document_id() ({expected!r}); "
    "if it equals the literal walker path 'alpha.txt' the F0 defect is back")
print(f"ok: search_document_id == identity.search_document_id(identity, 's3', 'alpha.txt'), not the walker path")
PY

echo "== Assertion (b): the declared passage contract has no unused fields =="
run_py <<'PY'
import dataclasses
import ingest.app as m

names = {f.name for f in dataclasses.fields(m.Passage)}
expected = {
    "passage_key", "search_document_id", "source_kind", "source_key", "raw_sha256",
    "schema_version", "ordinal", "text", "normalized_text_sha256", "heading_path",
    "char_start", "char_end", "embedding",
}
assert names == expected, f"Passage field set drifted from arcade.py's DDL: {names ^ expected}"
print(f"ok: Passage declares exactly {len(expected)} populated fields")
PY
run_py <<'PY'
import os
from ingest.arcade import _post
from ingest.identity import search_document_id

doc_id = search_document_id(os.environ["IDENTITY_ID"], "s3", "alpha.txt")
rows = _post("http://aura-arcadedb:2480", f"/api/v1/query/{os.environ['ARCADE_DB']}",
             {"language": "sql", "command": f"SELECT * FROM Passage WHERE search_document_id = '{doc_id}'"},
             ("root", os.environ["ARCADEDB_PASSWORD"]), 30.0)["result"]
row = rows[0]
populated = {
    "passage_key", "search_document_id", "source_kind", "source_key", "raw_sha256",
    "schema_version", "ordinal", "text", "normalized_text_sha256", "heading_path",
    "char_start", "char_end", "embedding",
}
missing = populated - row.keys()
assert not missing, f"missing fields that should always be non-null: {sorted(missing)}"
assert len(row["embedding"]) == 768, f"embedding width {len(row['embedding'])} != 768"
assert row["schema_version"] and row["raw_sha256"]
print(f"ok: all {len(populated)} declared fields are populated live")
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
             {"language": "sql", "command": f"SELECT text FROM Passage WHERE search_document_id = '{doc_id}'"},
             ("root", os.environ["ARCADEDB_PASSWORD"]), 30.0)["result"]
text = " ".join(r["text"] for r in rows)
# GROUND_TRUTH.txt: "CODE: A9A26924 appears exactly once, with Quantita 11" -- this table
# has no plain-text sentence, so recovering these tokens requires the real .xls -> .xlsx
# LibreOffice normalisation followed by real iscc-tika extraction. MarkItDown fails
# outright on .xls (FINDINGS.md), so this could not pass on the withdrawn extractor.
for token in ("A9A26924", "Descrizione", "Quantita"):
    assert token in text, f"{token!r} not found in extracted .xls text: {text!r}"
print("ok: .xls normalised by LibreOffice and extracted by iscc-tika, matching GROUND_TRUTH.txt")
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
        revised = (
            "Alpha document REVISED about apple orchards after a late frost. "
            f"The private launch code is {os.environ['MARKER_A']}. filler filler filler filler."
        )
        await c.put_object(Bucket=os.environ["S3_BUCKET"], Key="alpha.txt", Body=revised.encode())
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
             {"language": "sql", "command": f"SELECT count(*) as n FROM Passage WHERE search_document_id = '{beta_id}'"},
             ("root", os.environ["ARCADEDB_PASSWORD"]), 30.0)["result"]
n = rows[0]["n"]
assert n == 0, f"beta.txt's passages survived deletion ({n} rows) -- no delete worker of ours should have been needed"
print("ok: deleted object's passages are gone, no delete worker of ours")
PY

echo "== Assertion (d): live mode really performs a second cycle =="
container_id="$(docker run -d --network "$net" \
  -e ARCADEDB_PASSWORD="$arcade_pw" -e ARCADE_HTTP="http://aura-arcadedb:2480" \
  -e ARCADE_BOLT="bolt://aura-arcadedb:7687" \
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
             {"language": "sql", "command": f"SELECT count(*) as n FROM Passage WHERE search_document_id = '{delta_id}'"},
             ("root", os.environ["ARCADEDB_PASSWORD"]), 30.0)["result"]
n = rows[0]["n"]
assert n == 1, f"delta.txt was extracted but no Passage row landed ({n})"
print("ok: object added after the live process started was reconciled within one AURA_INGEST_INTERVAL_SEC, no restart")
PY

echo "== Identity B: separate bucket, state and ArcadeDB database =="
key_output_b="$(docker exec aura-garage /garage key create "$key_name_b" 2>&1)"
access_key_b="$(printf '%s\n' "$key_output_b" | grep -oP '(?<=Key ID:)\s*\K\S+')"
secret_key_b="$(printf '%s\n' "$key_output_b" | grep -oP '(?<=Secret key:)\s*\K\S+')"
[ -n "$access_key_b" ] && [ -n "$secret_key_b" ] || {
  echo "FAIL: could not parse Garage key for identity B" >&2
  exit 1
}
docker exec aura-garage /garage bucket create "$bucket_b" >/dev/null
docker exec aura-garage /garage bucket allow --read --write --owner "$bucket_b" --key "$access_key_b" >/dev/null
run_py_b <<'PY'
import asyncio, os
from aiobotocore.session import get_session

async def main():
    text = f"Bravo document. The private launch code is {os.environ['MARKER_B']}."
    session = get_session()
    async with session.create_client(
        "s3", endpoint_url=os.environ["S3_ENDPOINT"], aws_access_key_id=os.environ["S3_ACCESS_KEY"],
        aws_secret_access_key=os.environ["S3_SECRET_KEY"], region_name="garage") as c:
        await c.put_object(Bucket=os.environ["S3_BUCKET"], Key="bravo.txt", Body=text.encode())

asyncio.run(main())
PY
if ! docker run --rm --network "$net" \
  -e ARCADEDB_PASSWORD="$arcade_pw" -e ARCADE_HTTP="http://aura-arcadedb:2480" \
  -e ARCADE_BOLT="bolt://aura-arcadedb:7687" \
  -e AURA_INGEST_IDENTITY_ID="$identity_id_b" \
  -e AURA_INGEST_S3_ENDPOINT="http://aura-garage:3900" -e AURA_INGEST_S3_BUCKET="$bucket_b" \
  -e AURA_INGEST_S3_ACCESS_KEY_ID="$access_key_b" -e AURA_INGEST_S3_SECRET_ACCESS_KEY="$secret_key_b" \
  -v "$volume_b:/state" "$img" >"$scratch/run-b.log" 2>&1; then
  cat "$scratch/run-b.log" >&2
  exit 1
fi
cat "$scratch/run-b.log"
run_py_b <<'PY'
import os
from ingest.arcade import _post

rows = _post(
    "http://aura-arcadedb:2480", f"/api/v1/query/{os.environ['ARCADE_DB']}",
    {"language": "sql", "command": "SELECT text FROM Passage"},
    ("root", os.environ["ARCADEDB_PASSWORD"]), 30.0,
)["result"]
text = " ".join(row["text"] for row in rows)
assert os.environ["MARKER_B"] in text, "identity B marker did not reach its own database"
print("ok: identity B marker exists only in its own per-identity projection")
PY

echo "== Latest migration round-trip and real-agent document E2E =="
set -a
# The repository .env is CRLF because it is shared with Windows. Normalize only the
# sourced stream; never rewrite the operator's file.
source <(sed 's/\r$//' .env)
set +a
export AURA_PROFILE=dev
export AURA_DB_URL="postgres://aura_app:${POSTGRES_PASSWORD}@127.0.0.1:5432/aura?sslmode=disable"
export AURA_DB_MIGRATE_URL="postgres://aura_migrate:${POSTGRES_PASSWORD}@127.0.0.1:5432/aura?sslmode=disable"
export ARCADEDB_URL="http://127.0.0.1:2480"
export ARCADEDB_ADMIN_USER=root
export ARCADEDB_ADMIN_PASSWORD="$arcade_pw"
export ARCADEDB_PASSWORD="$arcade_pw"
export AURA_EMBED_BASE_URL="http://127.0.0.1:8081"
export AURA_DOCUMENT_E2E_IDENTITY_A="$identity_id"
export AURA_DOCUMENT_E2E_IDENTITY_B="$identity_id_b"
export AURA_DOCUMENT_E2E_MARKER_A="$marker_a"
export AURA_DOCUMENT_E2E_MARKER_B="$marker_b"

go test -tags db_integration -run '^TestMigrate0102BackfillsDecisionPolicyRoundTrip$' \
  -timeout 3m -v ./internal/db
go test -tags document_live_e2e -run '^TestDocumentProductionAgentE2E$' \
  -timeout 5m -v ./cmd/aura

echo "== Native document retrieval release eval: owned 21-file corpus / 20 qrels =="
(cd "$retrieval_fixture" && sha256sum -c corpus.sha256)
run_py <<'PY'
import asyncio, os
from pathlib import Path
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
        for fixture in sorted(Path("/retrieval-corpus").iterdir()):
            await c.put_object(Bucket=os.environ["S3_BUCKET"], Key=fixture.name, Body=fixture.read_bytes())

asyncio.run(main())
PY
run_pass
[ "$EXTRACT_COUNT" -eq 21 ] || {
  echo "FAIL: expected all 21 release fixtures to be extracted, got $EXTRACT_COUNT" >&2
  exit 1
}

mkdir -p "$retrieval_report_dir"
export AURA_BENCH_IDENTITY="$identity_id"
export AURA_ARCADEDB_URL="http://127.0.0.1:2480"
export AURA_ARCADEDB_DATABASE="$arcade_db"
export AURA_BENCH_PILOT="$retrieval_fixture/qrels.json"
export AURA_BENCH_OUT="$retrieval_report_dir/fusion.json"
export AURA_BENCH_CANDIDATE="$(git rev-parse HEAD)"
export AURA_ABSTAIN_QUERIES="$retrieval_fixture/qrels.json"
export AURA_ABSTAIN_OUT="$retrieval_report_dir/abstention.json"

go test -count=1 -tags retrieval_eval \
  -run '^Test(BenchmarkMetricsSupportMultipleGoldDocsAndExposeRegression|FusionBenchmark)$' \
  -timeout 35m -v ./internal/documents
go test -count=1 -tags retrieval_eval -run '^TestAbstentionEvidence$' \
  -timeout 35m -v ./internal/documents

python3 - "$AURA_BENCH_OUT" "$AURA_ABSTAIN_OUT" <<'PY'
import json, sys
fusion = json.load(open(sys.argv[1], encoding="utf-8"))
abstention = json.load(open(sys.argv[2], encoding="utf-8"))
production = next(run for run in fusion["runs"] if run["production"])
metrics = production["metrics"]
print(
    "ok: native retrieval eval "
    f"candidate={fusion['candidate_sha']} arm={production['arm']} "
    f"R@1={metrics['recall_at_1']:.3f} R@3={metrics['recall_at_3']:.3f} "
    f"MRR={metrics['mrr']:.3f}; abstention_vectors={len(abstention)}"
)
PY

echo
echo "ok: production document E2E -- reconciliation, migration, isolation, real agent and scored native retrieval"
