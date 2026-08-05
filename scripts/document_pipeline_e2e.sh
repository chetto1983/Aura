#!/usr/bin/env bash
set -Eeuo pipefail
if (set +H) 2>/dev/null; then
  set +H
fi
ROOT="$(git rev-parse --show-toplevel)"
cd "$ROOT"
die() {
  printf 'document pipeline E2E: %s\n' "$1" >&2
  exit 2
}
need() {
  command -v "$1" >/dev/null 2>&1 || die "missing required WSL command: $1"
}
grep -qi microsoft /proc/sys/kernel/osrelease 2>/dev/null || die "this production gate must run inside WSL2"
for command in awk curl date dirname grep id install jq mktemp python3 realpath sha256sum sleep stat tr wc; do
  need "$command"
done
GO_BIN="${AURA_DOCUMENT_E2E_GO_BIN:-}"
if [[ -z "$GO_BIN" ]]; then
  GO_BIN="$(command -v go || true)"
fi
if [[ -z "$GO_BIN" && -x "$HOME/.local/bin/go" ]]; then
  GO_BIN="$HOME/.local/bin/go"
fi
[[ -x "$GO_BIN" ]] || die "Go toolchain is unavailable; set AURA_DOCUMENT_E2E_GO_BIN"
[[ "${AURA_DOCUMENT_E2E_PRODUCTION_CONFIRM:-}" == "I_ACKNOWLEDGE_PRODUCTION_E2E" ]] ||
  die "set AURA_DOCUMENT_E2E_PRODUCTION_CONFIRM=I_ACKNOWLEDGE_PRODUCTION_E2E to arm the real production run"
BASE_URL="${AURA_DOCUMENT_E2E_BASE_URL:-}"
[[ -n "$BASE_URL" ]] || die "AURA_DOCUMENT_E2E_BASE_URL is required"
BASE_URL="${BASE_URL%/}"
case "$BASE_URL" in
https://*) ;;
http://127.0.0.1:* | http://localhost:*) ;;
*) die "base URL must be HTTPS or an explicit loopback production endpoint" ;;
esac

EXPECTED_MODEL="${AURA_DOCUMENT_E2E_EXPECT_MODEL:-}"
EXPECTED_EMBED_MODEL="${AURA_DOCUMENT_E2E_EXPECT_EMBED_MODEL:-}"
EXPECTED_EMBED_VERSION="${AURA_DOCUMENT_E2E_EXPECT_EMBED_VERSION:-}"
EXPECTED_DOCLING="${AURA_DOCUMENT_E2E_EXPECT_DOCLING_PRODUCER:-}"
[[ -n "$EXPECTED_MODEL" && -n "$EXPECTED_EMBED_MODEL" && -n "$EXPECTED_EMBED_VERSION" && -n "$EXPECTED_DOCLING" ]] ||
  die "all AURA_DOCUMENT_E2E_EXPECT_{MODEL,EMBED_MODEL,EMBED_VERSION,DOCLING_PRODUCER} values are required"

positive_int() {
  [[ "$2" =~ ^[1-9][0-9]*$ ]] || die "$1 must be a positive integer"
}
INGEST_TIMEOUT="${AURA_DOCUMENT_E2E_INGEST_TIMEOUT_SEC:-1800}"
RECLAIM_TIMEOUT="${AURA_DOCUMENT_E2E_RECLAIM_TIMEOUT_SEC:-2700}"
DELETE_TIMEOUT="${AURA_DOCUMENT_E2E_DELETE_TIMEOUT_SEC:-900}"
AGENT_TIMEOUT="${AURA_DOCUMENT_E2E_AGENT_TIMEOUT_SEC:-900}"
CLAIM_TIMEOUT="${AURA_DOCUMENT_E2E_CLAIM_TIMEOUT_SEC:-120}"
RESTART_READY_TIMEOUT="${AURA_DOCUMENT_E2E_RESTART_READY_TIMEOUT_SEC:-180}"
for timeout_pair in "INGEST_TIMEOUT:$INGEST_TIMEOUT" "RECLAIM_TIMEOUT:$RECLAIM_TIMEOUT" \
  "DELETE_TIMEOUT:$DELETE_TIMEOUT" "AGENT_TIMEOUT:$AGENT_TIMEOUT" "CLAIM_TIMEOUT:$CLAIM_TIMEOUT" \
  "RESTART_READY_TIMEOUT:$RESTART_READY_TIMEOUT"; do
  positive_int "${timeout_pair%%:*}" "${timeout_pair#*:}"
done

RESTART_HOOK="${AURA_DOCUMENT_E2E_RESTART_HOOK:-}"
[[ "$RESTART_HOOK" = /* && -f "$RESTART_HOOK" && ! -L "$RESTART_HOOK" && -x "$RESTART_HOOK" ]] ||
  die "AURA_DOCUMENT_E2E_RESTART_HOOK must be an absolute, executable, non-symlink file"
[[ "$(stat -c '%u' "$RESTART_HOOK")" == "$(id -u)" ]] || die "restart hook must be owned by the WSL operator"
hook_mode="$(stat -c '%a' "$RESTART_HOOK")"
(( (8#$hook_mode & 0022) == 0 )) || die "restart hook must not be group/world writable"

REPORT="${AURA_DOCUMENT_E2E_REPORT:-}"
[[ "$REPORT" = /* ]] || die "AURA_DOCUMENT_E2E_REPORT must be an absolute WSL path"
[[ -d "$(dirname "$REPORT")" ]] || die "the report parent directory must already exist"

AUTHORIZED_CORPUS="/mnt/d/tmp/aura-document-pipeline-references/document_ingestion/baseline-corpus"
CORPUS_DIR="${AURA_DOCUMENT_E2E_CORPUS_DIR:-$AUTHORIZED_CORPUS}"
[[ "$CORPUS_DIR" = /* && -d "$CORPUS_DIR" && ! -L "$CORPUS_DIR" ]] ||
  die "AURA_DOCUMENT_E2E_CORPUS_DIR must name the explicit operator-authorized corpus directory"
[[ "$(realpath "$CORPUS_DIR")" == "$AUTHORIZED_CORPUS" ]] ||
  die "the production gate is locked to the exact operator-authorized corpus"

WORK="$(mktemp -d -t aura-document-e2e.XXXXXXXX)"
chmod 700 "$WORK"
STATE="$WORK/state.json"
CREDENTIALS="$WORK/credentials.json"
CHECKS="$WORK/checks.tsv"
PREFLIGHT="$WORK/preflight.json"
CORPUS_INVENTORY="$WORK/corpus.tsv"
START_EPOCH="$(date +%s)"
RUN_ID="$(python3 - <<'PY'
import uuid
print(uuid.uuid4())
PY
)"
FAILED_CLASS=""
FINALIZED=0

EXPECTED_CHECKS=(
  wsl_and_production_preflight isolated_identity_provisioning
  tenant_a_ingest_ready tenant_b_restart_reclaim
  tenant_a_arcadedb_projection tenant_b_arcadedb_projection
  cross_tenant_nondisclosure tenant_a_real_agent_answer tenant_b_real_agent_answer
  tenant_a_production_model tenant_b_production_model
  tenant_a_delete_head_absence tenant_a_post_delete_non_recall
  tenant_b_delete_head_absence tenant_b_post_delete_non_recall cleanup
)

record_check() {
  local name="$1" detail="$2"
  printf '%s\tPASS\t%s\n' "$name" "$detail" >>"$CHECKS"
}

on_error() {
  local line="$1"
  if [[ -z "$FAILED_CLASS" ]]; then
    FAILED_CLASS="shell_line_${line}"
  fi
}
trap 'on_error "$LINENO"' ERR

finish() {
  local original_rc=$? cleanup_rc=0 report_rc=0
  [[ "$FINALIZED" -eq 0 ]] || return
  FINALIZED=1
  trap - ERR EXIT
  set +e
  if [[ -s "$STATE" ]] && jq -e '.identities | length > 0' "$STATE" >/dev/null 2>&1; then
    "$PROBE" cleanup --state "$STATE" >"$WORK/cleanup.json" 2>"$WORK/cleanup.err"
    cleanup_rc=$?
    if [[ "$cleanup_rc" -eq 0 ]]; then
      record_check cleanup resources_removed_and_postconditions_proven
    elif [[ -z "$FAILED_CLASS" ]]; then
      FAILED_CLASS=cleanup_postcondition
    fi
  else
    record_check cleanup no_resources_were_created
  fi

  END_EPOCH="$(date +%s)"
  export REPORT RUN_ID STATE CHECKS PREFLIGHT CORPUS_INVENTORY START_EPOCH END_EPOCH FAILED_CLASS WORK
  export EXPECTED_CHECKS_TEXT="${EXPECTED_CHECKS[*]}"
  python3 - <<'PY'
import hashlib, json, os, pathlib

def sha256_text(value):
    if not isinstance(value, str) or not value:
        return None
    return hashlib.sha256(value.encode("utf-8")).hexdigest()

expected = os.environ["EXPECTED_CHECKS_TEXT"].split()
seen = {}
checks_path = pathlib.Path(os.environ["CHECKS"])
if checks_path.exists():
    for line in checks_path.read_text(encoding="utf-8").splitlines():
        parts = line.split("\t", 2)
        if len(parts) == 3 and parts[0] in expected:
            seen[parts[0]] = {"name": parts[0], "status": parts[1], "evidence": parts[2]}
checks = [seen.get(name, {"name": name, "status": "FAIL", "evidence": "not_completed"}) for name in expected]
passed = sum(item["status"] == "PASS" for item in checks)
score = round(100.0 * passed / len(expected), 2)

state = {}
state_path = pathlib.Path(os.environ["STATE"])
if state_path.exists():
    try:
        state = json.loads(state_path.read_text(encoding="utf-8"))
    except Exception:
        state = {}
safe_identities = [{"label": item.get("label"), "identity_id_sha256": sha256_text(item.get("id")),
                    "arcade_database_sha256": sha256_text(item.get("arcade_database"))}
                   for item in state.get("identities", [])]
safe_documents = []
for label, snap in state.get("snapshots", {}).items():
    safe_documents.append({
        "label": label, "asset_id_sha256": sha256_text(snap.get("asset_id")),
        "document_id_sha256": sha256_text(snap.get("document_id")),
        "search_document_id_sha256": sha256_text(snap.get("search_document_id")),
        "version_id_sha256": sha256_text(snap.get("version_id")),
        "version_number": snap.get("version_number"), "sha256": snap.get("original_sha256"),
        "pipeline_generation": snap.get("pipeline_generation"), "asset_status": snap.get("asset_status"),
        "document_status": snap.get("document_status"), "version_status": snap.get("version_status"),
        "job_status": snap.get("job_status"), "job_attempts": snap.get("job_attempts"),
        "job_lease_generation": snap.get("job_lease_generation"), "stages": snap.get("stages"),
        "stage_producer_sha256": {
            stage: sha256_text(producer) for stage, producer in (snap.get("stage_producers") or {}).items()
        },
        "chunk_count": snap.get("chunk_count"), "embedding_count": snap.get("embedding_count"),
        "projection_count": snap.get("projection_count")
    })
agent_evidence = {}
work_path = pathlib.Path(os.environ["WORK"])
for label in ("a", "b", "a-post-delete", "b-post-delete"):
    evidence_path = work_path / f"agent-{label}.validation.json"
    if evidence_path.exists():
        try:
            evidence = json.loads(evidence_path.read_text(encoding="utf-8"))
        except Exception:
            continue
        tools = set(evidence.get("tools") or [])
        agent_evidence[label] = {
            "event_count": evidence.get("event_count"), "terminal_event": evidence.get("terminal_event"),
            "document_search_called": "document_search" in tools,
            "document_open_called": "document_open" in tools,
            "shell_exec_called": "shell_exec" in tools,
            "public_web_called": bool({"web_search", "web_fetch"} & tools),
            "answer_sha256": evidence.get("answer_sha256"),
            "conversation_id_sha256": evidence.get("conversation_id_sha256"),
            "answer_length": evidence.get("answer_length"), "citation_sha256": sha256_text(evidence.get("citation")),
            "mode": evidence.get("mode"),
        }
preflight = {}
preflight_path = pathlib.Path(os.environ["PREFLIGHT"])
if preflight_path.exists():
    try:
        preflight = json.loads(preflight_path.read_text(encoding="utf-8"))
    except Exception:
        preflight = {}
safe_preflight = {
    "profile": preflight.get("profile"), "migration_version": preflight.get("migration_version"),
    "migration_dirty": preflight.get("migration_dirty"),
    "docling_image_sha256": sha256_text(preflight.get("docling_image")),
    "embedding_dimensions": preflight.get("embedding_dimensions"),
    "agent_model_identifier_sha256": sha256_text(preflight.get("expected_agent_model")),
    "embedding_model_identifier_sha256": sha256_text(preflight.get("expected_embedding_model")),
    "embedding_revision_identifier_sha256": sha256_text(preflight.get("expected_embedding_version")),
    "docling_producer_identifier_sha256": sha256_text(preflight.get("expected_docling_producer")),
}
corpus = []
inventory_path = pathlib.Path(os.environ["CORPUS_INVENTORY"])
if inventory_path.exists():
    for line in inventory_path.read_text(encoding="utf-8").splitlines():
        basename, size, sha256 = line.split("\t", 2)
        corpus.append({"basename": basename, "size_bytes": int(size), "sha256": sha256})
report = {
    "gate": "document_pipeline_production_e2e", "run_id_sha256": sha256_text(os.environ["RUN_ID"]),
    "operator_authorized_corpus_only": True, "started_epoch": int(os.environ["START_EPOCH"]),
    "ended_epoch": int(os.environ["END_EPOCH"]),
    "duration_seconds": int(os.environ["END_EPOCH"]) - int(os.environ["START_EPOCH"]),
    "preflight": safe_preflight, "corpus_inventory": corpus,
    "identities": safe_identities, "documents": safe_documents, "agent_evidence": agent_evidence,
    "checks": checks, "hard_checks_passed": passed, "hard_checks_total": len(expected),
    "score_percent": score, "required_score_strictly_greater_than": 98,
    "verdict": "PASS" if passed == len(expected) and score > 98 else "FAIL",
    "failure_class": os.environ.get("FAILED_CLASS") or None,
}
target = pathlib.Path(os.environ["REPORT"])
target.write_text(json.dumps(report, indent=2, sort_keys=True) + "\n", encoding="utf-8")
target.chmod(0o600)
raise SystemExit(0 if report["verdict"] == "PASS" else 1)
PY
  report_rc=$?
  rm -rf -- "$WORK"
  if [[ "$original_rc" -eq 0 && "$cleanup_rc" -eq 0 && "$report_rc" -eq 0 ]]; then
    printf 'document pipeline production E2E: PASS (100%% hard checks)\nreport: %s\n' "$REPORT"
    exit 0
  fi
  printf 'document pipeline production E2E: FAIL (fail-closed)\nreport: %s\n' "$REPORT" >&2
  exit 1
}
trap finish EXIT

"$GO_BIN" build -o "$WORK/document-pipeline-e2e-probe" \
  ./scripts/document_pipeline_e2e_probe.go ./scripts/document_pipeline_e2e_support.go
PROBE="$WORK/document-pipeline-e2e-probe"

"$PROBE" preflight \
  --expected-model "$EXPECTED_MODEL" \
  --expected-embed-model "$EXPECTED_EMBED_MODEL" \
  --expected-embed-version "$EXPECTED_EMBED_VERSION" \
  --expected-docling-producer "$EXPECTED_DOCLING" >"$PREFLIGHT"
curl -fsS --max-time 10 "$BASE_URL/healthz" >/dev/null
curl -fsS --max-time 10 "$BASE_URL/readyz" >/dev/null
record_check wsl_and_production_preflight production_profile_and_real_dependencies_ready

"$PROBE" setup --base-url "$BASE_URL" --run-id "$RUN_ID" --state "$STATE" --credentials "$CREDENTIALS" >"$WORK/setup.json"
chmod 600 "$STATE" "$CREDENTIALS"
IDENTITY_A="$(jq -er '.identities[] | select(.label=="a") | .id' "$STATE")"
IDENTITY_B="$(jq -er '.identities[] | select(.label=="b") | .id' "$STATE")"
[[ "$IDENTITY_A" != "$IDENTITY_B" ]] || false
record_check isolated_identity_provisioning two_real_authula_sessions_and_two_private_garage_buckets

CORPUS_NAMES=(
  "Clienti.xlsx"
  "documenti da stampare.pdf"
  "Elenco Fornitori Acquisitori.xlsx"
  "gas_richiesta_voltura.pdf"
  "Lista Fornitori.xlsx"
  "Robot.pptx"
  "TESEBRO000050EN.pdf"
)
CORPUS_MIMES=(
  "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
  "application/pdf"
  "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
  "application/pdf"
  "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
  "application/vnd.openxmlformats-officedocument.presentationml.presentation"
  "application/pdf"
)
CORPUS_FILES=()
CORPUS_SHA=()
CORPUS_SIZE=()
mkdir -m 700 "$WORK/corpus"
for index in "${!CORPUS_NAMES[@]}"; do
  source_file="$CORPUS_DIR/${CORPUS_NAMES[$index]}"
  [[ -f "$source_file" && ! -L "$source_file" ]] || die "authorized corpus file is absent or not regular: ${CORPUS_NAMES[$index]}"
  staged_file="$WORK/corpus/$index-${CORPUS_NAMES[$index]}"
  install -m 600 -- "$source_file" "$staged_file"
  CORPUS_FILES+=("$staged_file")
  CORPUS_SHA+=("$(sha256sum "$staged_file" | awk '{print $1}')")
  CORPUS_SIZE+=("$(wc -c <"$staged_file" | tr -d '[:space:]')")
  printf 'corpus: %s size=%s sha256=%s\n' "${CORPUS_NAMES[$index]}" "${CORPUS_SIZE[$index]}" "${CORPUS_SHA[$index]}"
  printf '%s\t%s\t%s\n' "${CORPUS_NAMES[$index]}" "${CORPUS_SIZE[$index]}" "${CORPUS_SHA[$index]}" >>"$CORPUS_INVENTORY"
done

cookie_for() {
  jq -er --arg id "$1" '.cookies[$id]' "$CREDENTIALS"
}

api_call() {
  local identity="$1" method="$2" path="$3" output="$4" expected="$5" payload="${6:-}" key="${7:-}"
  local cookie code
  cookie="$(cookie_for "$identity")"
  local args=(-sS -o "$output" -w '%{http_code}' -X "$method" "$BASE_URL$path" -H "Cookie: $cookie" -H 'Accept: application/json')
  if [[ -n "$payload" ]]; then
    args+=(-H 'Content-Type: application/json' --data-binary "@$payload")
  fi
  if [[ -n "$key" ]]; then
    args+=(-H "Idempotency-Key: $key")
  fi
  code="$(curl "${args[@]}")"
  [[ "$code" == "$expected" ]]
}

upload_document() {
  local identity="$1" file="$2" label="$3" mime="$4" file_name="$5"
  local request="$WORK/presign-$label.request.json" response="$WORK/presign-$label.response.json"
  local headers="$WORK/upload-$label.curl" size asset upload_url method code cookie
  size="$(wc -c <"$file" | tr -d '[:space:]')"
  jq -n --arg file_name "$file_name" --arg mime "$mime" --argjson size "$size" \
    '{thread_id:"",file_name:$file_name,mime_type:$mime,size_bytes:$size,modality_hint:"document"}' >"$request"
  cookie="$(cookie_for "$identity")"
  code="$(curl -sS -o "$response" -w '%{http_code}' -X POST "$BASE_URL/api/assets/presign" \
    -H "Cookie: $cookie" -H 'Content-Type: application/json' -H 'Accept: application/json' \
    -H "Idempotency-Key: document-e2e-$RUN_ID-presign-$label" --data-binary "@$request")"
  [[ "$code" == "201" || "$code" == "200" ]]
  asset="$(jq -er '.asset.id' "$response")"
  upload_url="$(jq -er '.upload.upload_url' "$response")"
  method="$(jq -er '.upload.method // "PUT"' "$response")"
  python3 - "$response" "$headers" <<'PY'
import json, pathlib, sys
data = json.loads(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8"))
lines = []
for key, value in (data.get("upload", {}).get("required_headers") or {}).items():
    value = str(value).replace("\\", "\\\\").replace('"', '\\"')
    lines.append(f'header = "{key}: {value}"')
pathlib.Path(sys.argv[2]).write_text("\n".join(lines) + "\n", encoding="utf-8")
PY
  chmod 600 "$headers"
  curl -fsS --max-time 300 -K "$headers" -X "$method" "$upload_url" --data-binary "@$file" >/dev/null
  api_call "$identity" POST "/api/assets/$asset/finalize" "$WORK/finalize-$label.json" 200 "" \
    "document-e2e-$RUN_ID-finalize-$label"
  printf '%s\n' "$asset"
}

wait_asset_ready() {
  local identity="$1" asset="$2" label="$3" timeout_sec="$4"
  local deadline body="$WORK/asset-$label.json" status
  deadline=$((SECONDS + timeout_sec))
  while (( SECONDS < deadline )); do
    api_call "$identity" GET "/api/assets/$asset" "$body" 200
    status="$(jq -r '.status' "$body")"
    case "$status" in
      searchable | complete) return 0 ;;
      failed | refused | dead_letter | canceled) return 1 ;;
    esac
    sleep 1
  done
  return 1
}

snapshot() {
  local identity="$1" asset="$2" label="$3" sha="$4" output="$WORK/snapshot-$label.json"
  "$PROBE" snapshot --identity "$identity" --asset "$asset" --sha256 "$sha" --label "$label" --state "$STATE" \
    --expected-embed-model "$EXPECTED_EMBED_MODEL" --expected-embed-version "$EXPECTED_EMBED_VERSION" \
    --expected-docling-producer "$EXPECTED_DOCLING" >"$output"
}

declare -a ASSETS_A DOCS_A ASSETS_B DOCS_B
for index in "${!CORPUS_FILES[@]}"; do
  label="a-$index"
  ASSETS_A[$index]="$(upload_document "$IDENTITY_A" "${CORPUS_FILES[$index]}" "$label" "${CORPUS_MIMES[$index]}" "${CORPUS_NAMES[$index]}")"
  wait_asset_ready "$IDENTITY_A" "${ASSETS_A[$index]}" "$label" "$INGEST_TIMEOUT"
  snapshot "$IDENTITY_A" "${ASSETS_A[$index]}" "$label" "${CORPUS_SHA[$index]}"
  DOCS_A[$index]="$(jq -er '.document_id' "$WORK/snapshot-$label.json")"
done
record_check tenant_a_ingest_ready all_seven_reviewed_files_reached_ready_with_full_stage_evidence

RESTART_INDEX=6
label="b-$RESTART_INDEX"
ASSETS_B[$RESTART_INDEX]="$(upload_document "$IDENTITY_B" "${CORPUS_FILES[$RESTART_INDEX]}" "$label" \
  "${CORPUS_MIMES[$RESTART_INDEX]}" "${CORPUS_NAMES[$RESTART_INDEX]}")"
job_before="$WORK/job-b-before.json"
deadline=$((SECONDS + CLAIM_TIMEOUT))
while (( SECONDS < deadline )); do
  if "$PROBE" status --identity "$IDENTITY_B" --asset "${ASSETS_B[$RESTART_INDEX]}" >"$job_before" 2>/dev/null; then
    job_status="$(jq -r '.status' "$job_before")"
    [[ "$job_status" != "succeeded" ]] || false
    if [[ "$job_status" == "running" ]]; then
      break
    fi
  fi
  sleep 0.2
done
[[ -s "$job_before" && "$(jq -r '.status' "$job_before")" == "running" ]]
LEASE_BEFORE="$(jq -er '.lease_generation' "$job_before")"
ATTEMPT_BEFORE="$(jq -er '.attempt_count' "$job_before")"
LEASE_REMAINING="$(jq -er '.lease_remaining_seconds' "$job_before")"
"$RESTART_HOOK" >"$WORK/restart.log" 2>&1
deadline=$((SECONDS + RESTART_READY_TIMEOUT))
until curl -fsS --max-time 5 "$BASE_URL/readyz" >/dev/null 2>&1; do
  (( SECONDS < deadline )) || false
  sleep 1
done
reclaim_budget="$RECLAIM_TIMEOUT"
minimum_reclaim_budget=$((LEASE_REMAINING + INGEST_TIMEOUT + 60))
(( reclaim_budget >= minimum_reclaim_budget )) || reclaim_budget="$minimum_reclaim_budget"
wait_asset_ready "$IDENTITY_B" "${ASSETS_B[$RESTART_INDEX]}" "$label" "$reclaim_budget"
snapshot "$IDENTITY_B" "${ASSETS_B[$RESTART_INDEX]}" "$label" "${CORPUS_SHA[$RESTART_INDEX]}"
DOCS_B[$RESTART_INDEX]="$(jq -er '.document_id' "$WORK/snapshot-$label.json")"
LEASE_AFTER="$(jq -er '.job_lease_generation' "$WORK/snapshot-$label.json")"
ATTEMPT_AFTER="$(jq -er '.job_attempts' "$WORK/snapshot-$label.json")"
(( LEASE_AFTER > LEASE_BEFORE && ATTEMPT_AFTER > ATTEMPT_BEFORE ))
for index in "${!CORPUS_FILES[@]}"; do
  [[ "$index" -eq "$RESTART_INDEX" ]] && continue
  label="b-$index"
  ASSETS_B[$index]="$(upload_document "$IDENTITY_B" "${CORPUS_FILES[$index]}" "$label" "${CORPUS_MIMES[$index]}" "${CORPUS_NAMES[$index]}")"
  wait_asset_ready "$IDENTITY_B" "${ASSETS_B[$index]}" "$label" "$INGEST_TIMEOUT"
  snapshot "$IDENTITY_B" "${ASSETS_B[$index]}" "$label" "${CORPUS_SHA[$index]}"
  DOCS_B[$index]="$(jq -er '.document_id' "$WORK/snapshot-$label.json")"
done
record_check tenant_b_restart_reclaim all_seven_files_ready_and_large_pdf_job_reclaimed_with_higher_lease_generation

"$PROBE" arcade --identity "$IDENTITY_A" >"$WORK/arcade-a.json"
record_check tenant_a_arcadedb_projection private_database_and_tenant_credential_verified
"$PROBE" arcade --identity "$IDENTITY_B" >"$WORK/arcade-b.json"
record_check tenant_b_arcadedb_projection private_database_and_tenant_credential_verified

TARGET_INDEX=0
TARGET_SNAPSHOT_A="$WORK/snapshot-a-$TARGET_INDEX.json"
TARGET_SNAPSHOT_B="$WORK/snapshot-b-$TARGET_INDEX.json"
for index in "${!CORPUS_FILES[@]}"; do
  api_call "$IDENTITY_B" GET "/api/assets/${ASSETS_A[$index]}" "$WORK/cross-b-asset-a-$index.txt" 404
  api_call "$IDENTITY_B" GET "/api/documents/${DOCS_A[$index]}" "$WORK/cross-b-doc-a-$index.txt" 404
  api_call "$IDENTITY_A" GET "/api/assets/${ASSETS_B[$index]}" "$WORK/cross-a-asset-b-$index.txt" 404
  api_call "$IDENTITY_A" GET "/api/documents/${DOCS_B[$index]}" "$WORK/cross-a-doc-b-$index.txt" 404
done
api_call "$IDENTITY_A" GET "/api/documents" "$WORK/list-doc-a.json" 200
api_call "$IDENTITY_B" GET "/api/documents" "$WORK/list-doc-b.json" 200
for index in "${!CORPUS_FILES[@]}"; do
  ! grep -Fq "${DOCS_B[$index]}" "$WORK/list-doc-a.json"
  ! grep -Fq "${DOCS_A[$index]}" "$WORK/list-doc-b.json"
done
record_check cross_tenant_nondisclosure foreign_ids_are_404_and_absent_from_owner_lists

parse_sse() {
  python3 - "$@" <<'PY'
import hashlib, json, pathlib, re, sys
sse_path, snapshot_path, output_path, mode, expected_answer, conversation_id = sys.argv[1:7]
raw = pathlib.Path(sse_path).read_text(encoding="utf-8", errors="replace")
events, tools, answer, payload_text = [], [], [], []
for block in re.split(r"\r?\n\r?\n", raw):
    event = ""
    data = []
    for line in block.splitlines():
        if line.startswith("event:"):
            event = line.split(":", 1)[1].strip()
        elif line.startswith("data:"):
            data.append(line.split(":", 1)[1].lstrip())
    if not data:
        continue
    try:
        payload = json.loads("\n".join(data))
    except Exception:
        raise SystemExit("invalid SSE JSON payload")
    event = event or payload.get("type", "")
    events.append(event)
    encoded = json.dumps(payload, ensure_ascii=False, separators=(",", ":"))
    payload_text.append(encoded)
    if event == "TEXT_MESSAGE_CONTENT":
        answer.append(str(payload.get("delta", "")))
    if event == "TOOL_CALL_START":
        tools.append(str(payload.get("toolCallName", "")))
if not events or events[0] != "RUN_STARTED" or events[-1] != "RUN_FINISHED" or "RUN_ERROR" in events:
    raise SystemExit("agent run did not finish cleanly")
if "document_search" not in tools:
    raise SystemExit("real agent did not invoke document_search")
if mode == "present" and ("document_open" not in tools or "shell_exec" not in tools):
    raise SystemExit("real agent did not open and compute from the spreadsheet")
if "web_search" in tools or "web_fetch" in tools:
    raise SystemExit("real agent attempted a forbidden public-web lookup")
joined_payload = "\n".join(payload_text).lower()
if re.search(r'"(?:mock|fake|stub|fallback|degraded|skipped)"\s*:\s*true', joined_payload):
    raise SystemExit("agent/tool stream reported forbidden degraded evidence")
text = "".join(answer)
folded = text.casefold()
snapshot = json.loads(pathlib.Path(snapshot_path).read_text(encoding="utf-8"))
if mode == "present":
    sha = snapshot["original_sha256"]
    version = int(snapshot["version_number"])
    citations = snapshot["citation_tokens"]
    used = next((token for token in citations if token in text), "")
    if not re.search(rf"(?<!\d){re.escape(expected_answer)}(?!\d)", text):
        raise SystemExit("reviewed spreadsheet ground truth is absent")
    if sha.casefold() not in folded or not used:
        raise SystemExit("SHA-256 or canonical citation is absent")
    if not re.search(rf"version(?:e)?\D{{0,12}}{version}\b", folded):
        raise SystemExit("explicit source version is absent")
else:
    forbidden = [expected_answer, snapshot["original_sha256"],
                 f'document:{snapshot["search_document_id"]}@']
    if any(value.casefold() in folded for value in forbidden):
        raise SystemExit("deleted corpus evidence was recalled")
    used = ""
summary = {
    "event_count": len(events), "terminal_event": events[-1], "tools": sorted(set(tools)),
    "answer_sha256": hashlib.sha256(text.encode()).hexdigest(),
    "conversation_id_sha256": hashlib.sha256(conversation_id.encode()).hexdigest(),
    "answer_length": len(text), "citation": used, "mode": mode,
}
pathlib.Path(output_path).write_text(json.dumps(summary, indent=2) + "\n", encoding="utf-8")
PY
}

create_conversation() {
  local identity="$1" label="$2" request="$WORK/conversation-$label.request.json" response="$WORK/conversation-$label.response.json"
  jq -n --arg title "Document pipeline E2E $RUN_ID $label" '{title:$title}' >"$request"
  api_call "$identity" POST /api/conversations "$response" 201 "$request" "document-e2e-$RUN_ID-conversation-$label"
  jq -er '.ID // .id' "$response"
}

agent_turn() {
  local identity="$1" conversation="$2" label="$3" prompt="$4" mode="$5" snapshot_file="$6" expected_answer="$7"
  local request="$WORK/agent-$label.request.json" sse="$WORK/agent-$label.sse" code cookie
  jq -n --arg thread "$conversation" --arg prompt "$prompt" --arg msg "msg-$RUN_ID-$label" \
    '{threadId:$thread,messages:[{id:$msg,role:"user",content:$prompt}]}' >"$request"
  cookie="$(cookie_for "$identity")"
  code="$(curl -sS -N --max-time "$AGENT_TIMEOUT" -o "$sse" -w '%{http_code}' \
    -X POST "$BASE_URL/agent/run" -H "Cookie: $cookie" -H 'Content-Type: application/json' \
    -H 'Accept: text/event-stream' -H "Idempotency-Key: document-e2e-$RUN_ID-agent-$label" --data-binary "@$request")"
  [[ "$code" == "200" ]]
  parse_sse "$sse" "$snapshot_file" "$WORK/agent-$label.validation.json" "$mode" "$expected_answer" "$conversation"
}

GROUND_TRUTH="699"
PROMPT="Quanti clienti hanno Localita TORINO nel file Clienti.xlsx? Conta le righe del file, non usare la scheda. Rispondi in italiano in modo naturale e includi il numero di versione, l'impronta SHA-256 completa e la citazione canonica verificabile della fonte."
CONV_A="$(create_conversation "$IDENTITY_A" a)"
agent_turn "$IDENTITY_A" "$CONV_A" a "$PROMPT" present "$TARGET_SNAPSHOT_A" "$GROUND_TRUTH"
record_check tenant_a_real_agent_answer exact_ground_truth_sha_version_and_citation_from_document_search
"$PROBE" conversation --identity "$IDENTITY_A" --conversation "$CONV_A" --expected-model "$EXPECTED_MODEL" >"$WORK/model-a.json"
record_check tenant_a_production_model persisted_conversation_model_matches_expected_production_model

CONV_B="$(create_conversation "$IDENTITY_B" b)"
agent_turn "$IDENTITY_B" "$CONV_B" b "$PROMPT" present "$TARGET_SNAPSHOT_B" "$GROUND_TRUTH"
record_check tenant_b_real_agent_answer exact_ground_truth_sha_version_and_citation_from_document_search
"$PROBE" conversation --identity "$IDENTITY_B" --conversation "$CONV_B" --expected-model "$EXPECTED_MODEL" >"$WORK/model-b.json"
record_check tenant_b_production_model persisted_conversation_model_matches_expected_production_model

wait_deleted() {
  local identity="$1" label="$2" timeout_sec="$3" deadline
  deadline=$((SECONDS + timeout_sec))
  while (( SECONDS < deadline )); do
    if "$PROBE" verify-delete --identity "$identity" --label "$label" --state "$STATE" >"$WORK/delete-$label.json" 2>/dev/null; then
      return 0
    fi
    sleep 1
  done
  return 1
}

for index in "${!CORPUS_FILES[@]}"; do
  api_call "$IDENTITY_A" DELETE "/api/documents/${DOCS_A[$index]}" "$WORK/delete-a-$index.response.json" 200 "" "document-e2e-$RUN_ID-delete-a-$index"
  wait_deleted "$IDENTITY_A" "a-$index" "$DELETE_TIMEOUT"
done
record_check tenant_a_delete_head_absence all_seven_tombstones_ledgers_and_exact_garage_404_heads_verified
POST_DELETE_PROMPT="Quanti clienti hanno Localita TORINO nel file Clienti.xlsx? Cerca di nuovo la fonte; se non e disponibile, dichiaralo senza inventare numeri o citazioni."
CONV_A2="$(create_conversation "$IDENTITY_A" a-post-delete)"
agent_turn "$IDENTITY_A" "$CONV_A2" a-post-delete "$POST_DELETE_PROMPT" absent "$TARGET_SNAPSHOT_A" "$GROUND_TRUTH"
record_check tenant_a_post_delete_non_recall fresh_real_agent_search_cannot_recall_deleted_corpus

for index in "${!CORPUS_FILES[@]}"; do
  api_call "$IDENTITY_B" DELETE "/api/documents/${DOCS_B[$index]}" "$WORK/delete-b-$index.response.json" 200 "" "document-e2e-$RUN_ID-delete-b-$index"
  wait_deleted "$IDENTITY_B" "b-$index" "$DELETE_TIMEOUT"
done
record_check tenant_b_delete_head_absence all_seven_tombstones_ledgers_and_exact_garage_404_heads_verified
CONV_B2="$(create_conversation "$IDENTITY_B" b-post-delete)"
agent_turn "$IDENTITY_B" "$CONV_B2" b-post-delete "$POST_DELETE_PROMPT" absent "$TARGET_SNAPSHOT_B" "$GROUND_TRUTH"
record_check tenant_b_post_delete_non_recall fresh_real_agent_search_cannot_recall_deleted_corpus

exit 0
