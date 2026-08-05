#!/usr/bin/env bash
# Liveness for the two sidecars that share aura's network namespace.
#
# Their own healthchecks cannot detect the failure mode that matters: after the
# aura container is restarted or recreated, both processes stay alive attached to
# a namespace nobody can reach, and keep reporting healthy. Measured 2026-08-05.
# Reachability must therefore be asserted from OUTSIDE the namespace.
set -Eeuo pipefail
probe() {
  # -O - (stdout), not -O /dev/null: on Git Bash / MSYS the /dev/null argument gets
  # rewritten to the Windows "nul" device before it reaches docker exec, which
  # busybox wget inside the container then fails to open ("Permission denied") —
  # a false negative with the stack fully healthy. Writing to stdout instead and
  # discarding it on the host side needs no path argument, so there is nothing for
  # MSYS to mangle; behaves identically on Linux (CI, WSL) and Windows Git Bash.
  docker exec aura-grafana-1 wget -q -T 5 -O - "$1" >/dev/null 2>&1
}
fail=0
probe "http://aura:3200/ready"    || { echo "tempo unreachable at aura:3200/ready"; fail=1; }
probe "http://aura:9090/-/ready"  || { echo "prometheus unreachable at aura:9090/-/ready"; fail=1; }
[[ "$fail" -eq 0 ]] && echo "observability sidecars reachable"
exit "$fail"
