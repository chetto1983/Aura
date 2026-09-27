#!/usr/bin/env bash
# The box-runtime browser end to end through Aura's production tool registry (prd.md §12):
# `aura toolpipe` builds the same registry `aura serve` does (buildRegistryWithMCP, the real
# sandbox router, the operator's identity), so every call below goes through the mounted
# browser__ tools, the per-identity session pool and an exec into the operator's box. Only the
# model choosing the calls is missing.
#
# Midway the box is stopped the way the idle reaper suspends it, and the next browser call must
# bring the box and its MCP server back by itself.
#
# Needs: a migrated database with the operator seeded, `aura mcp install browser`, and the
# aura-sandbox image. Usage: AURA_BIN=/tmp/aura bash mcp_toolpipe_e2e.sh
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
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

session='"session":"mcp-e2e","restore":true'
call '{"tool":"tool_search","args":{"query":"browser open page snapshot"}}'
records 1
docker cp "$here/fixture_site.py" "$box:/workspace/fixture_site.py"
call '{"tool":"shell_exec","args":{"command":"FIXTURE_STATE=/workspace/sessions.json nohup python3 fixture_site.py 8765 >/workspace/site.log 2>&1 & sleep 1"}}'
call "{\"tool\":\"browser__agent_browser_open\",\"args\":{$session,\"url\":\"http://127.0.0.1:8765/login\"}}"
call "{\"tool\":\"browser__agent_browser_snapshot\",\"args\":{$session,\"interactive\":true}}"
records 4

docker stop -t 2 "$box" >/dev/null # what the idle reaper's Suspend does
sleep "${AURA_E2E_AFTER_STOP:-5}"
call "{\"tool\":\"browser__agent_browser_open\",\"args\":{$session,\"url\":\"data:text/html,<title>after-suspend</title>ok\"}}"
call "{\"tool\":\"browser__agent_browser_get_title\",\"args\":{$session}}"
call "{\"tool\":\"browser__agent_browser_close\",\"args\":{$session}}"
records 7
exec 3>&-
wait "$pipe"

python3 - "$work/out.jsonl" <<'PY'
import json, sys
rows = [json.loads(l) for l in open(sys.argv[1]) if l.strip()]
for r in rows:
    print(f"{r.get('tool', '(summary)'):36} {r['status']:6} {r['duration_ms']:6} ms  {(r.get('error') or r.get('preview', ''))[:160]!r}")
calls = [r for r in rows if r.get('tool')]
checks = {
    "tool_search finds the deferred browser tools": "browser__agent_browser_open" in calls[0].get("preview", ""),
    "every call succeeded": all(r["status"] == "ok" for r in calls),
    "the login page opened in the box": "Password" in calls[3].get("preview", "") or "password" in calls[3].get("preview", ""),
    "the stopped box came back for the next call": "after-suspend" in calls[5].get("preview", ""),
}
for name, ok in checks.items():
    print(("PASS " if ok else "FAIL ") + name)
sys.exit(0 if all(checks.values()) else 1)
PY
