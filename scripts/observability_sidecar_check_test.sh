#!/usr/bin/env bash
set -Eeuo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT
mkdir -p "$tmp_dir/bin"

cat >"$tmp_dir/bin/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
url="${*: -1}"
case "$url" in
  http://tempo:3200/ready)
    [ "${OBS_TEST_SCENARIO:-healthy}" != "tempo-down" ] || exit 1
    printf 'ready\n'
    ;;
  http://prometheus:9090/-/ready)
    printf 'ready\n'
    ;;
  *api/datasources/proxy/uid/aura-prometheus/api/v1/query*)
    value=1
    [ "${OBS_TEST_SCENARIO:-healthy}" != "grafana-datasource-down" ] || value=0
    printf '{"status":"success","data":{"result":[{"metric":{"job":"aura"},"value":[1,"%s"]}]}}\n' "$value"
    ;;
  http://127.0.0.1:3000/api/health)
    printf '{"database":"ok"}\n'
    ;;
  http://127.0.0.1:3000/api/datasources/uid/aura-tempo/health)
    [ "${OBS_TEST_SCENARIO:-healthy}" != "grafana-datasource-down" ] || exit 1
    printf '{"message":"Data source is working","status":"OK"}\n'
    ;;
  *api/v1/query*)
    value=1
    [ "${OBS_TEST_SCENARIO:-healthy}" != "scrape-down" ] || value=0
    printf '{"status":"success","data":{"result":[{"metric":{"job":"aura"},"value":[1,"%s"]}]}}\n' "$value"
    ;;
  *)
    printf 'unexpected fake docker call: %s\n' "$*" >&2
    exit 2
    ;;
esac
EOF
chmod +x "$tmp_dir/bin/docker"

run_check() {
  PATH="$tmp_dir/bin:$PATH" OBS_TEST_SCENARIO="$1" \
    bash "$repo_root/scripts/observability_sidecar_check.sh" 2>&1
}

healthy="$(run_check healthy)"
[[ "$healthy" == *"reachable and scraping aura"* ]] || {
  printf 'healthy scenario failed: %s\n' "$healthy" >&2
  exit 1
}

if scrape_down="$(run_check scrape-down)"; then
  echo "scrape-down scenario unexpectedly passed" >&2
  exit 1
fi
[[ "$scrape_down" == *'up{job="aura"} != 1'* ]] || {
  printf 'scrape-down diagnostic missing: %s\n' "$scrape_down" >&2
  exit 1
}

if grafana_down="$(run_check grafana-datasource-down)"; then
  echo "grafana-datasource-down scenario unexpectedly passed" >&2
  exit 1
fi
[[ "$grafana_down" == *"Grafana"* ]] || {
  printf 'Grafana datasource diagnostic missing: %s\n' "$grafana_down" >&2
  exit 1
}

if tempo_down="$(run_check tempo-down)"; then
  echo "tempo-down scenario unexpectedly passed" >&2
  exit 1
fi
[[ "$tempo_down" == *"tempo unreachable"* ]] || {
  printf 'tempo-down diagnostic missing: %s\n' "$tempo_down" >&2
  exit 1
}

echo "observability sidecar check contract passed"
