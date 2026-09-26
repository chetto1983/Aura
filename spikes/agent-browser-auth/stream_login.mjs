// Plays the human in the cockpit: logs into the fixture through agent-browser's
// stream WebSocket only (mouse + keyboard events), while measuring frames.
// Usage: node stream_login.mjs <port> <userBox json> <passBox json> <user> <password>
const [port, userBoxJson, passBoxJson, user, password] = process.argv.slice(2);
const center = (b) => ({ x: b.x + b.width / 2, y: b.y + b.height / 2 });

const ws = new WebSocket(`ws://127.0.0.1:${port}/`);
const frames = [];
let lastUrl = "";
ws.onmessage = (ev) => {
  const msg = JSON.parse(ev.data);
  if (msg.type === "frame") frames.push({ at: Date.now(), bytes: msg.data.length * 0.75, captured: msg.metadata.timestamp, w: msg.metadata.deviceWidth, h: msg.metadata.deviceHeight });
  if (msg.type === "url") lastUrl = msg.url;
};
const send = (o) => ws.send(JSON.stringify(o));
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function click({ x, y }) {
  for (const eventType of ["mousePressed", "mouseReleased"]) {
    send({ type: "input_mouse", eventType, x, y, button: "left", clickCount: 1 });
  }
  await sleep(150);
}

async function typeText(text) {
  for (const ch of text) {
    // A char code is NOT a virtual key code ('.' is 46 = VK_DELETE); printable text travels in `text`.
    const base = { type: "input_keyboard", key: ch, code: "", windowsVirtualKeyCode: 0, modifiers: 0 };
    send({ ...base, eventType: "keyDown", text: ch });
    send({ ...base, eventType: "keyUp" });
  }
  await sleep(150);
}

await new Promise((r) => (ws.onopen = r));
await sleep(2000);
const idle = frames.length;
const t0 = Date.now();
await click(center(JSON.parse(userBoxJson)));
const firstAfterInput = await (async () => {
  const n = frames.length;
  while (frames.length === n && Date.now() - t0 < 3000) await sleep(5);
  return frames.length > n ? frames[n].at - t0 : null;
})();
await typeText(user);
await click(center(JSON.parse(passBoxJson)));
await typeText(password);
if (!process.env.NO_SUBMIT) send({ type: "input_keyboard", eventType: "keyDown", key: "Enter", code: "Enter", windowsVirtualKeyCode: 13, text: "\r", modifiers: 0 });
if (!process.env.NO_SUBMIT) send({ type: "input_keyboard", eventType: "keyUp", key: "Enter", code: "Enter", windowsVirtualKeyCode: 13, modifiers: 0 });
await sleep(2500);
ws.close();

const active = frames.slice(idle);
const secs = (active.at(-1)?.at - active[0]?.at) / 1000 || 1;
const kb = active.map((f) => f.bytes / 1024).sort((a, b) => a - b);
console.log(JSON.stringify({
  idle_frames_2s: idle,
  active_frames: active.length,
  active_fps: +(active.length / secs).toFixed(1),
  frame_kb_median: +kb[Math.floor(kb.length / 2)]?.toFixed(1),
  frame_kb_max: +kb.at(-1)?.toFixed(1),
  viewport: active[0] ? `${active[0].w}x${active[0].h}` : null,
  first_frame_after_click_ms: firstAfterInput,
  last_url: lastUrl,
}, null, 1));
