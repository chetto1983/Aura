#!/usr/bin/env bash
# Puts the library the web-e2e document specs read into the seeded operator's bucket and waits
# until the ingest sidecar has indexed all of it:
#   - fattura_2026_0323/0386/0267.xlsx, the invoices documents-live's rule picks out, from the
#     scale-corpus generator (spikes/document-routing-scale, fixed seed); the 400-odd other files it
#     writes stay in the temporary directory;
#   - the retrieval-eval corpus (scripts/fixtures/document_retrieval_eval), which
#     documents-filter-eval-live scores and documents-survival-live renames, moves and edits.
# It needs the stack and env of ci.yml's web-e2e job (Postgres, Garage, ArcadeDB, the embedder,
# aura-ingest) and the aura binary built at the repository root.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
library="$work/library"
mkdir -p "$library" "$work/gen"

python3 -m venv "$work/venv"
"$work/venv/bin/pip" install --quiet openpyxl==3.1.5
# The generator writes next to itself, so it runs from a copy.
cp spikes/document-routing-scale/generate_scale_corpus.py "$work/gen/"
"$work/venv/bin/python" "$work/gen/generate_scale_corpus.py" >/dev/null
cp "$work"/gen/corpus/fattura_2026_0{323,386,267}.xlsx "$library/"
cp scripts/fixtures/document_retrieval_eval/corpus/* "$library/"
want="$(find "$library" -type f | wc -l)"

# `aura docs ingest` reads only below AURA_WORKSPACE_DIR, and puts the bytes in the operator's
# bucket, which is what the sidecar indexes.
for file in "$library"/*; do
  AURA_WORKSPACE_DIR="$library" ./aura docs ingest -- "$file" >/dev/null
done

identity="$(printf "SELECT id FROM aura.identities WHERE name = :'email';\n" |
  docker compose exec -T postgres psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -At -v email="$AURA_E2E_AUTHULA_EMAIL")"
if [ -z "$identity" ]; then
  echo "no identity named $AURA_E2E_AUTHULA_EMAIL" >&2
  exit 1
fi
# internal/arcadedb/tenant.go: one database per identity.
database="mem_${identity//-/_}"

indexed() {
  curl -fsS -u "$ARCADEDB_ADMIN_USER:$ARCADEDB_ADMIN_PASSWORD" -H 'Content-Type: application/json' \
    -d '{"language":"sql","command":"SELECT count(*) AS n FROM IndexedDocument"}' \
    "http://127.0.0.1:2480/api/v1/query/$database" | jq -r '.result[0].n'
}

deadline=$((SECONDS + 900))
n=0
until n="$(indexed 2>/dev/null)" && [ "$n" -ge "$want" ]; do
  if [ "$SECONDS" -ge "$deadline" ]; then
    echo "indexed ${n:-0} of $want documents in 900 s" >&2
    docker compose logs --tail 120 aura-ingest >&2 || true
    exit 1
  fi
  sleep 5
done
echo "indexed $n of $want documents in ${SECONDS} s"
