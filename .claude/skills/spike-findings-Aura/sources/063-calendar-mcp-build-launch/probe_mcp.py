#!/usr/bin/env python3
"""Spike 063 — raw MCP stdio handshake probe for MarimerLLC/calendar-mcp (.NET 10).

Independent of Aura's Go client (that's spike 064). Proves the published .NET
stdio server boots, negotiates the MCP protocol, and advertises the tool names
the existing internal/mcp/calendar_integration_test.go scaffold asserts.

Run in WSL:
  wsl -e bash -lc 'PATH="$HOME/.dotnet:$PATH" python3 /mnt/d/Aura/.planning/spikes/063-calendar-mcp-build-launch/probe_mcp.py'
"""
import json, os, select, subprocess, sys, tempfile, time

APPHOST = os.path.expanduser("~/calendar-mcp-publish/CalendarMcp.StdioServer")
EXPECTED = ["list_accounts", "get_calendar_events", "create_event", "send_email"]


def log(cat, msg):
    print(f"[{time.strftime('%Y-%m-%dT%H:%M:%S')}] [{cat}] {msg}", flush=True)


def main():
    if not os.path.exists(APPHOST):
        log("FAIL", f"apphost not found: {APPHOST}")
        return 1
    # Empty config dir -> zero accounts, clean wire handshake (no creds needed).
    cfg = tempfile.mkdtemp(prefix="aura-spike-063-")
    env = dict(os.environ, CALENDAR_MCP_CONFIG=cfg)

    t0 = time.time()
    proc = subprocess.Popen(
        [APPHOST], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
        stderr=subprocess.DEVNULL, text=True, bufsize=1, env=env,
    )
    log("BOOT", f"spawned pid={proc.pid}; CALENDAR_MCP_CONFIG={cfg}")

    def send(obj):
        proc.stdin.write(json.dumps(obj) + "\n")
        proc.stdin.flush()

    def read_resp(timeout):
        end = time.time() + timeout
        while time.time() < end:
            remaining = max(0.0, end - time.time())
            r, _, _ = select.select([proc.stdout], [], [], remaining)
            if not r:
                continue
            line = proc.stdout.readline()
            if not line:
                continue
            line = line.strip()
            if not line:
                continue
            try:
                obj = json.loads(line)
            except json.JSONDecodeError:
                log("STDOUT-NONJSON", line[:120])
                continue
            return obj
        return None

    send({"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {
        "protocolVersion": "2025-06-18", "capabilities": {},
        "clientInfo": {"name": "aura-spike-063", "version": "0.0.1"}}})
    init = read_resp(40)
    if init is None:
        log("FAIL", "no initialize response within 40s (cold-start/handshake failure)")
        proc.kill()
        return 1
    boot_ms = (time.time() - t0) * 1000
    result = init.get("result", {})
    pv = result.get("protocolVersion")
    sv = result.get("serverInfo", {})
    caps = sorted(result.get("capabilities", {}).keys())
    log("INIT", f"cold-start+initialize={boot_ms:.0f}ms protocolVersion={pv} serverInfo={sv} capabilities={caps}")

    send({"jsonrpc": "2.0", "method": "notifications/initialized"})
    send({"jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": {}})
    tl = read_resp(20)
    tools = (tl or {}).get("result", {}).get("tools", [])
    names = sorted(t["name"] for t in tools)
    log("TOOLS", f"count={len(names)}")
    for n in names:
        log("TOOL", n)

    # also confirm list_accounts is callable with zero accounts (returns []-shape, not error)
    send({"jsonrpc": "2.0", "id": 3, "method": "tools/call",
          "params": {"name": "list_accounts", "arguments": {}}})
    call = read_resp(20)
    call_ok = bool(call and "result" in call and not call["result"].get("isError"))
    preview = json.dumps((call or {}).get("result", {}))[:200]
    log("CALL", f"list_accounts ok={call_ok} preview={preview}")

    proc.terminate()
    try:
        proc.wait(5)
    except subprocess.TimeoutExpired:
        proc.kill()

    missing = [e for e in EXPECTED if e not in names]
    if missing:
        log("FAIL", f"missing scaffold-expected tools: {missing}")
        return 1
    if not call_ok:
        log("FAIL", "list_accounts call did not return a clean result")
        return 1
    log("SUMMARY", f"VALIDATED .NET10 stdio MCP — {len(names)} tools, "
                   f"scaffold-expected {EXPECTED} all present, protocol {pv}, "
                   f"cold-start {boot_ms:.0f}ms, list_accounts callable")
    return 0


if __name__ == "__main__":
    sys.exit(main())
