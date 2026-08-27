#!/usr/bin/env bash
# Disposable image/audio retrieval gate. Uses free Ollama Cloud vision and Aura's local STT.
set -euo pipefail
export MSYS_NO_PATHCONV=1

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
cd "$repo_root"

image="${AURA_INGEST_IMAGE:-aura-ingest:local}"
network="${AURA_NETWORK:-aura_default}"
suffix="$(date +%s)-$$"
bucket="aura-media-e2e-$suffix"
key_name="aura-media-e2e-$suffix"
state_volume="aura-media-e2e-state-$suffix"
identity_id="$(python3 -c 'import uuid; print(uuid.uuid4())')"
arcade_db="mem_${identity_id//-/_}"
scratch="$(mktemp -d)"
access_key=""
secret_key=""

for container in aura-arcadedb aura-garage aura-llama-embed aura-stt aura-tts; do
  if [ "$(docker inspect -f '{{.State.Running}}' "$container" 2>/dev/null || echo false)" != "true" ]; then
    echo "FAIL: $container is not running" >&2
    exit 1
  fi
done
if ! docker run --rm --add-host host.docker.internal:host-gateway --entrypoint python "$image" \
  -c 'import urllib.request; urllib.request.urlopen("http://host.docker.internal:11434/api/version", timeout=5).read()'; then
  echo "FAIL: Ollama is not reachable from the ingest container" >&2
  exit 1
fi

arcade_password="$(docker inspect aura-arcadedb --format '{{range .Config.Env}}{{println .}}{{end}}' \
  | grep -oP '(?<=rootPassword=)\S+')"
postgres_password="$(docker inspect aura-postgres --format '{{range .Config.Env}}{{println .}}{{end}}' \
  | grep -oP '(?<=POSTGRES_PASSWORD=)\S+')"
tenant_secret="$(docker inspect aura --format '{{range .Config.Env}}{{println .}}{{end}}' \
  | grep -oP '(?<=AURA_ARCADEDB_TENANT_SECRET=)\S+')"

run_py() {
  docker run --rm -i --network "$network" \
    -e ARCADE_HTTP=http://aura-arcadedb:2480 -e ARCADE_DB="$arcade_db" \
    -e ARCADEDB_PASSWORD="$arcade_password" \
    -e S3_ENDPOINT=http://aura-garage:3900 -e S3_BUCKET="$bucket" \
    -e S3_ACCESS_KEY="$access_key" -e S3_SECRET_KEY="$secret_key" \
    -v "$repo_root/public:/fixtures:ro" \
    --entrypoint python "$image" -
}

cleanup() {
  local exit_code=$?
  set +e
  if [ -n "$access_key" ]; then
    run_py <<'PY' >/dev/null 2>&1
import asyncio, os
from aiobotocore.session import get_session

async def main():
    async with get_session().create_client(
        "s3", endpoint_url=os.environ["S3_ENDPOINT"],
        aws_access_key_id=os.environ["S3_ACCESS_KEY"],
        aws_secret_access_key=os.environ["S3_SECRET_KEY"], region_name="garage",
    ) as client:
        page = await client.list_objects_v2(Bucket=os.environ["S3_BUCKET"])
        for item in page.get("Contents", []):
            await client.delete_object(Bucket=os.environ["S3_BUCKET"], Key=item["Key"])

asyncio.run(main())
PY
    docker exec aura-garage /garage bucket delete --yes "$bucket" >/dev/null 2>&1
    docker exec aura-garage /garage key delete --yes "$access_key" >/dev/null 2>&1
  fi
  run_py <<'PY' >/dev/null 2>&1
import os
from ingest.arcade import _post
try:
    _post(
        os.environ["ARCADE_HTTP"], "/api/v1/server",
        {"command": f"drop database {os.environ['ARCADE_DB']}"},
        ("root", os.environ["ARCADEDB_PASSWORD"]), 30.0,
    )
except Exception:
    pass
PY
  docker volume rm "$state_volume" >/dev/null 2>&1
  rm -rf "$scratch"
  exit "$exit_code"
}
trap cleanup EXIT

key_output="$(docker exec aura-garage /garage key create "$key_name" 2>&1)"
access_key="$(printf '%s\n' "$key_output" | grep -oP '(?<=Key ID:)\s*\K\S+')"
secret_key="$(printf '%s\n' "$key_output" | grep -oP '(?<=Secret key:)\s*\K\S+')"
[ -n "$access_key" ] && [ -n "$secret_key" ] || {
  echo "FAIL: could not create disposable Garage credentials" >&2
  exit 1
}
docker exec aura-garage /garage bucket create "$bucket" >/dev/null
docker exec aura-garage /garage bucket allow --read --write --owner "$bucket" --key "$access_key" >/dev/null

run_py <<'PY'
import asyncio, json, os, urllib.request
from pathlib import Path
from aiobotocore.session import get_session

speech = (
    "La verifica multimodale Aura riguarda il progetto Fenice quarantadue "
    "e richiede approvazione manuale."
)
request = urllib.request.Request(
    "http://aura-tts:8880/v1/audio/speech",
    data=json.dumps({"input": speech, "voice": "if_sara", "response_format": "wav"}).encode(),
    headers={"Content-Type": "application/json"},
)
with urllib.request.urlopen(request, timeout=120) as response:
    audio = response.read()
assert audio[:4] == b"RIFF", "TTS fixture is not WAV"

async def main():
    async with get_session().create_client(
        "s3", endpoint_url=os.environ["S3_ENDPOINT"],
        aws_access_key_id=os.environ["S3_ACCESS_KEY"],
        aws_secret_access_key=os.environ["S3_SECRET_KEY"], region_name="garage",
    ) as client:
        await client.put_object(
            Bucket=os.environ["S3_BUCKET"], Key="cockpit.png",
            Body=Path("/fixtures/cockpit.png").read_bytes(), ContentType="image/png",
        )
        await client.put_object(
            Bucket=os.environ["S3_BUCKET"], Key="fenice.wav",
            Body=audio, ContentType="audio/wav",
        )

asyncio.run(main())
PY

run_ingest() {
  docker run --rm --network "$network" --add-host host.docker.internal:host-gateway \
    -e AURA_DB_URL= -e AURA_LLM_PROVIDER=llamacpp \
    -e AURA_VISION_CLOUD=true -e AURA_LLM_MODEL=gemma4:31b-cloud \
    -e AURA_LLM_BASE_URL=http://host.docker.internal:11434/v1 -e OPENROUTER_API_KEY= \
    -e MULTIMODAL_BASE_URL=http://aura-ocr-vl:8082/v1 -e MULTIMODAL_MODEL=glm-ocr \
    -e STT_BASE_URL=http://aura-stt:9000/v1 -e STT_MODEL=large-v3-turbo \
    -e STT_LANGUAGE=it -e AURA_STT_CLOUD_MODEL= -e MULTIMODAL_TIMEOUT_SEC=120 \
    -e ARCADEDB_PASSWORD="$arcade_password" -e ARCADE_HTTP=http://aura-arcadedb:2480 \
    -e ARCADE_BOLT=bolt://aura-arcadedb:7687 \
    -e AURA_EMBED_BASE_URL=http://aura-llama-embed:8081 \
    -e AURA_INGEST_IDENTITY_ID="$identity_id" \
    -e AURA_INGEST_S3_ENDPOINT=http://aura-garage:3900 -e AURA_INGEST_S3_BUCKET="$bucket" \
    -e AURA_INGEST_S3_ACCESS_KEY_ID="$access_key" \
    -e AURA_INGEST_S3_SECRET_ACCESS_KEY="$secret_key" \
    -v "$state_volume:/state" --entrypoint python "$image" -m ingest.app
}

echo "== media ingest: image via gemma4:31b-cloud, audio via local STT =="
run_ingest | tee "$scratch/ingest.log"
[ "$(grep -c '^\[extract\]' "$scratch/ingest.log" || true)" -eq 2 ] || {
  echo "FAIL: the first pass did not process exactly two media objects" >&2
  exit 1
}

run_py <<'PY'
import os
from ingest.arcade import _post

rows = _post(
    os.environ["ARCADE_HTTP"], f"/api/v1/query/{os.environ['ARCADE_DB']}",
    {"language": "sql", "command": "SELECT source_key, text, embedding FROM Passage"},
    ("root", os.environ["ARCADEDB_PASSWORD"]), 30.0,
)["result"]
by_key = {}
for row in rows:
    by_key.setdefault(row["source_key"], []).append(row)
assert set(by_key) == {"cockpit.png", "fenice.wav"}, by_key.keys()

image_text = " ".join(row["text"] for row in by_key["cockpit.png"])
audio_text = " ".join(row["text"] for row in by_key["fenice.wav"])
for token in ("customer-reconciliation", "Pilot workspace", "Expired"):
    assert token.casefold() in image_text.casefold(), (token, image_text)
for token in ("Fenice", "approvazione manuale"):
    assert token.casefold() in audio_text.casefold(), (token, audio_text)
assert "42" in audio_text or "quarantadue" in audio_text.casefold(), audio_text
assert all(len(row["embedding"]) == 768 for row in rows)

documents = _post(
    os.environ["ARCADE_HTTP"], f"/api/v1/query/{os.environ['ARCADE_DB']}",
    {"language": "sql", "command": "SELECT source_key, passage_count FROM IndexedDocument"},
    ("root", os.environ["ARCADEDB_PASSWORD"]), 30.0,
)["result"]
assert {row["source_key"] for row in documents} == {"cockpit.png", "fenice.wav"}
assert all(row["passage_count"] > 0 for row in documents), documents
print("ok: image OCR and audio transcript became 768-dimension ArcadeDB passages")
PY

echo "== real production agent over the two derived passages =="
if ! docker run --rm --network "$network" --add-host host.docker.internal:host-gateway \
  -e AURA_PROFILE=dev \
  -e POSTGRES_PASSWORD="$postgres_password" \
  -e AURA_DB_URL="postgres://aura_app:${postgres_password}@aura-postgres:5432/aura?sslmode=disable" \
  -e AURA_LLM_PROVIDER=llamacpp -e AURA_LLM_MODEL=gemma4:31b-cloud \
  -e AURA_LLM_BASE_URL=http://host.docker.internal:11434/v1 \
  -e OPENROUTER_API_KEY=ollama-local \
  -e ARCADEDB_URL=http://aura-arcadedb:2480 -e ARCADEDB_ADMIN_USER=root \
  -e ARCADEDB_ADMIN_PASSWORD="$arcade_password" -e ARCADEDB_PASSWORD="$arcade_password" \
  -e AURA_ARCADEDB_TENANT_SECRET="$tenant_secret" \
  -e AURA_EMBED_BASE_URL=http://aura-llama-embed:8081 -e AURA_EMBED_DIMENSIONS=768 \
  -e AURA_DOCUMENT_E2E_MEDIA_IDENTITY="$identity_id" \
  -v "$repo_root:/src" -w /src golang:1.26-bookworm \
  go test -count=1 -tags document_live_e2e -run '^TestMediaDocumentProductionAgentE2E$' \
    -timeout 6m -v ./cmd/aura >"$scratch/agent.log" 2>&1; then
  tail -n 200 "$scratch/agent.log" >&2
  exit 1
fi
grep -E -- 'real-agent multimodal score|--- PASS: TestMediaDocumentProductionAgentE2E|^ok[[:space:]]' \
  "$scratch/agent.log"

run_py <<'PY'
import asyncio, os
from aiobotocore.session import get_session

async def main():
    async with get_session().create_client(
        "s3", endpoint_url=os.environ["S3_ENDPOINT"],
        aws_access_key_id=os.environ["S3_ACCESS_KEY"],
        aws_secret_access_key=os.environ["S3_SECRET_KEY"], region_name="garage",
    ) as client:
        await client.delete_object(Bucket=os.environ["S3_BUCKET"], Key="cockpit.png")
        await client.delete_object(Bucket=os.environ["S3_BUCKET"], Key="fenice.wav")

asyncio.run(main())
PY
run_ingest >"$scratch/delete.log"

run_py <<'PY'
import os
from ingest.arcade import _post
for record_type in ("Passage", "IndexedDocument"):
    result = _post(
        os.environ["ARCADE_HTTP"], f"/api/v1/query/{os.environ['ARCADE_DB']}",
        {"language": "sql", "command": f"SELECT count(*) AS n FROM {record_type}"},
        ("root", os.environ["ARCADEDB_PASSWORD"]), 30.0,
    )["result"]
    assert result[0]["n"] == 0, (record_type, result)
print("ok: deleting both media objects removed their document and passage state")
PY

echo "ok: disposable Garage media reconciliation passed"
