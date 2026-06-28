#!/usr/bin/env python3
"""Reusable MCP stdio harness for calendar-mcp (Windows server.exe).

Runs a JSON list of steps in ONE server session. Each step is one of:
  {"op": "list"}                          -> print all tool names
  {"op": "schema", "tool": "name"}        -> print a tool's inputSchema
  {"op": "call", "tool": "name", "args": {...}}  -> tools/call, print result text

Usage:  python mcp_harness.py steps.json
"""
import json, subprocess, sys, threading, time, queue

SERVER = r"D:\tmp\calendar-mcp-win\server\CalendarMcp.StdioServer.exe"


def main():
    steps = json.load(open(sys.argv[1], encoding="utf-8"))
    proc = subprocess.Popen([SERVER], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                            stderr=subprocess.DEVNULL, text=True, bufsize=1, encoding="utf-8")
    q = queue.Queue()

    def reader():
        for line in proc.stdout:
            line = line.strip()
            if line:
                q.put(line)
    threading.Thread(target=reader, daemon=True).start()

    _id = [0]

    def nextid():
        _id[0] += 1
        return _id[0]

    def send(obj):
        proc.stdin.write(json.dumps(obj) + "\n")
        proc.stdin.flush()

    def wait_for(want_id, timeout=60):
        end = time.time() + timeout
        while time.time() < end:
            try:
                line = q.get(timeout=end - time.time())
            except queue.Empty:
                break
            try:
                obj = json.loads(line)
            except json.JSONDecodeError:
                continue
            if obj.get("id") == want_id:
                return obj
        return None

    # handshake
    iid = nextid()
    send({"jsonrpc": "2.0", "id": iid, "method": "initialize",
          "params": {"protocolVersion": "2025-06-18", "capabilities": {},
                     "clientInfo": {"name": "aura-harness", "version": "0.1"}}})
    init = wait_for(iid, 40)
    if not init:
        print("FATAL: no initialize response"); proc.kill(); return 1
    send({"jsonrpc": "2.0", "method": "notifications/initialized"})

    # cache tools/list once
    lid = nextid()
    send({"jsonrpc": "2.0", "id": lid, "method": "tools/list", "params": {}})
    tools = (wait_for(lid, 30) or {}).get("result", {}).get("tools", [])
    by_name = {t["name"]: t for t in tools}

    for step in steps:
        op = step.get("op", "call")
        print("\n" + "=" * 70)
        if op == "list":
            print("TOOLS:", ", ".join(sorted(by_name)))
        elif op == "schema":
            t = by_name.get(step["tool"])
            print(f"SCHEMA {step['tool']}:")
            print(json.dumps(t.get("inputSchema", {}) if t else {"ERROR": "not found"}, indent=2))
        elif op == "call":
            cid = nextid()
            print(f"CALL {step['tool']} args={json.dumps(step.get('args', {}))}")
            send({"jsonrpc": "2.0", "id": cid, "method": "tools/call",
                  "params": {"name": step["tool"], "arguments": step.get("args", {})}})
            resp = wait_for(cid, step.get("timeout", 90))
            if resp is None:
                print("  -> TIMEOUT")
            elif "error" in resp:
                print("  -> ERROR:", json.dumps(resp["error"]))
            else:
                res = resp.get("result", {})
                is_err = res.get("isError")
                for c in res.get("content", []):
                    if c.get("type") == "text":
                        print(f"  -> {'[isError] ' if is_err else ''}{c['text']}")
                    else:
                        print(f"  -> [{c.get('type')}]")

    proc.terminate()
    try:
        proc.wait(5)
    except subprocess.TimeoutExpired:
        proc.kill()
    return 0


if __name__ == "__main__":
    sys.exit(main())
