#!/usr/bin/env bash
# Reachability of the observability pack, plus the one assertion no healthcheck makes:
# that Prometheus is actually SCRAPING aura.
#
# This existed because Prometheus and Tempo shared aura's network namespace and neither
# could detect its own orphaning: after aura was recreated both processes stayed alive
# attached to a namespace nobody could reach, and kept reporting healthy (measured
# 2026-08-05). The assertion therefore had to come from outside that namespace.
#
# The sharing is gone — they are ordinary services on the project network now, so the
# orphaning cannot happen. The useful half survives and is why this file is kept: a
# Prometheus that is up while its aura target is down looks perfectly healthy from every
# container, and that exact state went unnoticed for five and a half hours on 2026-08-13.
set -Eeuo pipefail

probe() {
  # -O - (stdout) rather than -O /dev/null: MSYS rewrites the /dev/null argument to
  # `nul` before it reaches busybox wget in the container, which then fails to open it
  # and reports a false unreachable. `2>&1 >/dev/null` captures wget's diagnostic while
  # discarding the response body — order matters, the reverse discards both.
  local err
  if ! err="$(docker exec aura-grafana-1 wget -q -T 5 -O - "$1" 2>&1 >/dev/null)"; then
    [[ -n "$err" ]] && printf '  %s\n' "$err" >&2
    return 1
  fi
}

fail=0
probe "http://tempo:3200/ready"        || { echo "tempo unreachable at tempo:3200/ready"; fail=1; }
probe "http://prometheus:9090/-/ready" || { echo "prometheus unreachable at prometheus:9090/-/ready"; fail=1; }

# up{job="aura"} is 1 only when the LAST scrape succeeded, so this is the difference
# between "the metrics pipeline exists" and "the metrics pipeline works".
if [[ "$fail" -eq 0 ]]; then
  scraped="$(docker exec aura-grafana-1 wget -q -T 5 -O - \
      'http://prometheus:9090/api/v1/query?query=up%7Bjob%3D%22aura%22%7D' 2>/dev/null |
    grep -o '"value":\[[^]]*"1"\]' || true)"
  if [[ -z "$scraped" ]]; then
    echo 'prometheus is up but is NOT scraping aura (up{job="aura"} != 1)'
    fail=1
  fi
fi

[[ "$fail" -eq 0 ]] && echo "observability pack reachable and scraping aura"
exit "$fail"
