// Aura's live-view relay for one agent-browser session (prd.md §12). The viewport stream binds
// loopback inside the box netns and exec is Aura's only channel in, so the cockpit reaches it
// through this process: stream messages go out on stdout as NDJSON, input events come in on
// stdin as NDJSON. Measured 2026-09-26 over `docker exec -i`: 14 fps, first frame 56 ms after a
// click. Usage: node browser-relay.mjs <session>
import { readFileSync } from "node:fs";
import readline from "node:readline";

// Only viewer input crosses into the browser; anything else a caller sends is dropped.
const INPUT_TYPES = new Set(["input_mouse", "input_keyboard", "input_touch", "config"]);
const fail = (reason) => {
  process.stdout.write(JSON.stringify({ type: "relay_error", reason }) + "\n");
  process.exit(2);
};

const session = process.argv[2] ?? "";
if (!/^[A-Za-z0-9_-]{1,48}$/.test(session)) fail("invalid_session");
let port;
try {
  port = readFileSync(`${process.env.AURA_AGENT_BROWSER_HOME}/.agent-browser/${session}.stream`, "utf8").trim();
} catch {
  fail("no_such_session");
}
if (!/^\d{1,5}$/.test(port)) fail("no_stream_port");

const ws = new WebSocket(`ws://127.0.0.1:${port}/`);
// Closing the socket on stdin EOF also fires onerror; only an unrequested failure is an error.
let closing = false;
ws.onerror = () => closing || fail("stream_unreachable");
ws.onclose = () => (closing ? process.exit(0) : fail("stream_closed"));
ws.onmessage = (ev) => process.stdout.write(ev.data + "\n");
await new Promise((resolve) => (ws.onopen = resolve));

for await (const line of readline.createInterface({ input: process.stdin })) {
  let msg;
  try {
    msg = JSON.parse(line);
  } catch {
    continue;
  }
  if (INPUT_TYPES.has(msg?.type)) ws.send(JSON.stringify(msg));
}
closing = true;
ws.close();
