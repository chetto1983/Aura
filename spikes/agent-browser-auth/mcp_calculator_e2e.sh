#!/usr/bin/env bash
# Counter-proof for the box runtime with a server that has nothing to do with agent-browser:
# chetto1983/calculator-mcp-server (Python, FastMCP, numpy/sympy/matplotlib), installed by
# shell_exec into a venv on the operator's /workspace volume and declared with
# `aura mcp add calculator --box`. Like mcp_toolpipe_e2e.sh it drives `aura toolpipe` over the
# production registry, and stops the box midway the way the idle reaper suspends it.
#
# Usage: AURA_BIN=/tmp/aura bash mcp_calculator_e2e.sh
set -euo pipefail

aura=${AURA_BIN:-aura}
box=${AURA_E2E_BOX:-aura-box-00000000-0000-0000-0000-000000000001}
work=$(mktemp -d)
mkfifo "$work/in"

"$aura" toolpipe <"$work/in" >"$work/out.jsonl" 2>"$work/err.log" &
pipe=$!
exec 3>"$work/in"
call() { printf '%s\n' "$1" >&3; }
records() {
	local want=$1 deadline=$((SECONDS + 120))
	until [ "$(wc -l <"$work/out.jsonl")" -ge "$want" ]; do
		[ "$SECONDS" -lt "$deadline" ] || { echo "timed out waiting for $want records" >&2; cat "$work/err.log" >&2; exit 1; }
		sleep 0.2
	done
}

call '{"tool":"tool_search","args":{"query":"matrix determinant"}}'
call '{"tool":"calculator__calculate","args":{"expression":"2**10"}}'
call '{"tool":"calculator__solve_equation","args":{"equation":"x**2 - 4 = 0"}}'
call '{"tool":"calculator__matrix_determinant","args":{"matrix":[[1,2],[3,4]]}}'
records 4
docker stop -t 2 "$box" >/dev/null # what the idle reaper's Suspend does
sleep "${AURA_E2E_AFTER_STOP:-3}"
call '{"tool":"calculator__mean","args":{"data":[1,2,3,4]}}'
call '{"tool":"calculator__plot_function","args":{"expression":"sin(x)","start":0,"end":6,"step":200}}'
records 6
exec 3>&-
wait "$pipe"

python3 - "$work/out.jsonl" <<'PY'
import json, sys
rows = [json.loads(l) for l in open(sys.argv[1]) if l.strip()]
for r in rows:
    print(f"{r.get('tool', '(summary)'):36} {r['status']:6} {r['duration_ms']:6} ms  {(r.get('error') or r.get('preview', ''))[:120]!r}")
calls = [r for r in rows if r.get('tool')]
text = [c.get("preview", "") for c in calls]
checks = {
    "tool_search loads the deferred calculator tools": "calculator__matrix_determinant" in text[0],
    "every call succeeded": all(r["status"] == "ok" for r in calls),
    "2**10 = 1024": "1024" in text[1],
    "x**2 - 4 = 0 solves to -2 and 2": "-2" in text[2] and "2" in text[2].replace("-2", ""),
    "det [[1,2],[3,4]] = -2": "-2" in text[3],
    "the stopped box came back: mean [1,2,3,4] = 2.5": "2.5" in text[4],
    "a plot renders in the box": "successfully" in text[5].lower(),
}
for name, ok in checks.items():
    print(("PASS " if ok else "FAIL ") + name)
sys.exit(0 if all(checks.values()) else 1)
PY
