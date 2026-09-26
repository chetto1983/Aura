#!/usr/bin/env python3
"""Time a stdio MCP server from spawn to its answers, the way Aura's box runtime starts one.

    python3 mcp_cold_start.py <command> [args...]

Prints one JSON line: seconds to the initialize answer, to the tools/list answer, the tool
count, and whether anything on stdout was not JSON-RPC (which would break a real client).
stderr goes to /tmp/mcp_cold_start.err, as the box runtime sends it to a log.
"""
import json
import subprocess
import sys
import time

INIT = {"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {
    "protocolVersion": "2025-06-18", "capabilities": {}, "clientInfo": {"name": "probe", "version": "0"}}}


def read_answer(proc, want_id, deadline, noise):
    while time.monotonic() < deadline:
        line = proc.stdout.readline()
        if not line:
            return None
        try:
            msg = json.loads(line)
        except ValueError:
            noise.append(line[:120])
            continue
        if msg.get("id") == want_id:
            return msg
    return None


def main():
    start = time.monotonic()
    deadline = start + 600
    noise = []
    with open("/tmp/mcp_cold_start.err", "w") as err:
        proc = subprocess.Popen(sys.argv[1:], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                stderr=err, text=True, bufsize=1)
        proc.stdin.write(json.dumps(INIT) + "\n")
        init = read_answer(proc, 1, deadline, noise)
        t_init = time.monotonic() - start
        tools = None
        if init is not None:
            proc.stdin.write(json.dumps({"jsonrpc": "2.0", "method": "notifications/initialized"}) + "\n")
            proc.stdin.write(json.dumps({"jsonrpc": "2.0", "id": 2, "method": "tools/list"}) + "\n")
            tools = read_answer(proc, 2, deadline, noise)
        t_list = time.monotonic() - start
        proc.kill()
        proc.wait()
    print(json.dumps({
        "initialize_s": round(t_init, 2) if init else None,
        "tools_list_s": round(t_list, 2) if tools else None,
        "tools": len(tools["result"]["tools"]) if tools else 0,
        "stdout_noise": noise,
    }))


if __name__ == "__main__":
    main()
