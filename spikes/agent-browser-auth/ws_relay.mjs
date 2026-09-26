// In-box relay: bridges agent-browser's loopback stream WebSocket to stdin/stdout as
// NDJSON, so a host that only has `docker exec` (Aura's only channel into a box) can
// watch frames and send input without publishing a port. Usage: node ws_relay.mjs <port>
import readline from "node:readline";

const ws = new WebSocket(`ws://127.0.0.1:${process.argv[2]}/`);
ws.onmessage = (ev) => process.stdout.write(ev.data + "\n");
ws.onclose = () => process.exit(0);
await new Promise((r) => (ws.onopen = r));
for await (const line of readline.createInterface({ input: process.stdin })) {
  if (line.trim()) ws.send(line);
}
ws.close();
