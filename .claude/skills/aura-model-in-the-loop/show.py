#!/usr/bin/env python3
"""Prints what a parked completion asks: its kind, the tools offered and the new messages.

Usage: show.py <llm-dir>/req-N.json [chars]. The kind is read off the first system message:
a title request and the reasoning-tier classifier are not agent turns and want plain text.
"""
import json
import sys

req = json.load(open(sys.argv[1]))
width = int(sys.argv[2]) if len(sys.argv) > 2 else 1500
msgs = req["messages"]
first = msgs[0].get("content") or ""
first = first if isinstance(first, str) else json.dumps(first)
kind = "agent turn"
if first.startswith("You generate a concise"):
    kind = "title (answer: a 4-6 word title)"
elif first.startswith("Classify the latest user request"):
    kind = 'reasoning tier (answer: {"tier":"low"})'
print(f"kind: {kind}; messages: {len(msgs)}; tools: {[t['function']['name'] for t in req.get('tools') or []]}")
tail = msgs[1:] if kind != "agent turn" else msgs[-6:]
for m in tail:
    c = m.get("content")
    if isinstance(c, list):
        c = " ".join(p.get("text", "") for p in c if isinstance(p, dict))
    calls = [t["function"]["name"] for t in m.get("tool_calls") or []]
    print(f"-- {m['role']} {m.get('tool_call_id') or ''} {calls or ''}\n{(c or '')[:width]}")
