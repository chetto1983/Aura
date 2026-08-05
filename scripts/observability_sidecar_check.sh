#!/usr/bin/env bash
# Liveness for the two sidecars that share aura's network namespace.
#
# Their own healthchecks cannot detect the failure mode that matters: after the
# aura container is restarted or recreated, both processes stay alive attached to
# a namespace nobody can reach, and keep reporting healthy. Measured 2026-08-05.
# Reachability must therefore be asserted from OUTSIDE the namespace.
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
probe "http://aura:3200/ready"    || { echo "tempo unreachable at aura:3200/ready"; fail=1; }
probe "http://aura:9090/-/ready"  || { echo "prometheus unreachable at aura:9090/-/ready"; fail=1; }
[[ "$fail" -eq 0 ]] && echo "observability sidecars reachable"
exit "$fail"
