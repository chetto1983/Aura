import http from 'node:http';
import { execFile } from 'node:child_process';
import { rm } from 'node:fs/promises';
import readline from 'node:readline';

const session = process.argv[2] ?? '';
if (!/^chatgpt-[a-f0-9]{24}$/.test(session)) process.exit(2);
const profile = `/tmp/.aura-chatgpt-profile-${session}`;
const emit = (value) => process.stdout.write(`${JSON.stringify(value)}\n`);
let closing;
let expectedState;
let consumed = false;
let devtools;
const children = new Set();

const browser = (...args) => new Promise((resolve, reject) => {
  const child = execFile('agent-browser', [
    '--session', session, '--profile', profile, '--restore-save', 'never', ...args,
  ], {
    timeout: args[0] === 'close' ? 4000 : 35000,
    maxBuffer: 4096,
    env: { ...process.env, AGENT_BROWSER_AUTOSAVE_INTERVAL_MS: '0' },
  }, (err, stdout) => {
    children.delete(child);
    if (err) reject(new Error('browser_unavailable'));
    else resolve(stdout.trim());
  });
  children.add(child);
});

// A passkey request opens Chrome's own dialog, which the live view's page stream never shows,
// and while it is up the page takes no click or key: OpenAI's passkey challenge froze with "Try
// another method" dead (prd.md §6, 2026-10-06). Nothing in the box could answer it anyway. With
// the DevTools WebAuthn domain on and its UI off there is no dialog: the request fails at its
// timeout, as on a machine without passkeys, and the page stays usable meanwhile. The override
// lasts as long as this connection, so it stays open until close() ends the login.
const disablePasskeyDialog = async () => {
  const endpoint = await browser('get', 'cdp-url');
  if (!/^ws:\/\/127\.0\.0\.1:\d{1,5}\/devtools\/browser\/[a-f0-9-]{36}$/.test(endpoint)) {
    throw new Error('invalid_devtools');
  }
  const socket = new WebSocket(endpoint);
  devtools = socket;
  await new Promise((resolve, reject) => {
    socket.onopen = resolve;
    socket.onerror = () => reject(new Error('devtools_unreachable'));
  });
  socket.onclose = () => { if (!closing) { emit({ type: 'error' }); void close(); } };
  const replies = new Map();
  let id = 0;
  socket.onmessage = (event) => {
    const message = JSON.parse(event.data);
    replies.get(message.id)?.(message);
    replies.delete(message.id);
  };
  const send = (method, params, sessionId) => new Promise((resolve, reject) => {
    id += 1;
    replies.set(id, (message) => (message.error ? reject(new Error('devtools_refused')) : resolve(message.result)));
    socket.send(JSON.stringify({ id, method, params, sessionId }));
  });
  const { targetInfos } = await send('Target.getTargets', {});
  const pages = targetInfos.filter((target) => target.type === 'page');
  if (pages.length === 0) throw new Error('no_tab');
  for (const page of pages) {
    const { sessionId } = await send('Target.attachToTarget', { targetId: page.targetId, flatten: true });
    await send('WebAuthn.enable', { enableUI: false }, sessionId);
  }
};

const server = http.createServer((request, response) => {
  response.setHeader('Cache-Control', 'no-store');
  response.setHeader('Referrer-Policy', 'no-referrer');
  response.setHeader('Content-Type', 'text/plain; charset=utf-8');
  response.setHeader('Content-Security-Policy', "default-src 'none'; frame-ancestors 'none'");
  let url;
  try { url = new URL(request.url, redirectURI); }
  catch { response.writeHead(400).end('Accesso non valido.'); return; }
  if (request.method !== 'GET' || request.headers.host !== `127.0.0.1:${server.address().port}` ||
      url.pathname !== '/auth/callback' || request.url.length > 16384 || consumed ||
      !expectedState || url.searchParams.getAll('state').length !== 1 ||
      url.searchParams.get('state') !== expectedState) {
    response.writeHead(400).end('Accesso non valido. Torna ad Aura e riprova.');
    return;
  }
  consumed = true;
  emit({ type: 'callback', query: url.search.slice(1) });
  // Returning sign-in can redirect during `open`: its load must complete before Go receives navigated.
  response.end("Torna ad Aura per vedere l'esito dell'accesso. Puoi chiudere questa finestra.");
});
server.requestTimeout = 30000;
server.headersTimeout = 10000;
server.maxHeadersCount = 32;
server.on('error', () => { emit({ type: 'error' }); void close(); });

const close = () => {
  if (closing) return closing;
  closing = (async () => {
    for (const child of children) child.kill('SIGTERM');
    server.closeAllConnections();
    server.close();
    devtools?.close();
    try { await browser('close'); } catch { emit({ type: 'cleanup_error' }); }
    try { await rm(profile, { recursive: true, force: true }); }
    catch { emit({ type: 'cleanup_error' }); }
    process.exitCode = 0;
  })();
  return closing;
};
for (const signal of ['SIGTERM', 'SIGINT']) process.on(signal, () => { void close(); });
await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
const redirectURI = `http://127.0.0.1:${server.address().port}/auth/callback`;
emit({ type: 'listening', redirect_uri: redirectURI });
const input = readline.createInterface({ input: process.stdin, crlfDelay: Infinity });
input.on('close', () => { void close(); });
const deadline = setTimeout(() => { input.close(); void close(); }, 600000);
try {
  for await (const line of input) {
    if (closing) break;
    const command = JSON.parse(line);
    if (command.type !== 'navigate' || expectedState) throw new Error('invalid_command');
    const url = new URL(command.url);
    if (url.origin !== 'https://auth.openai.com' || url.pathname !== '/api/accounts/authorize' ||
        url.username || url.password || url.hash ||
        url.searchParams.get('redirect_uri') !== redirectURI || !url.searchParams.get('state')) {
      throw new Error('invalid_authorization');
    }
    expectedState = url.searchParams.get('state');
    // A bare `open` relaunches the browser it has just started: 2.3-3.2 s instead of 0.8 s.
    await browser('open', 'about:blank');
    await disablePasskeyDialog();
    await browser('open', url.href);
    if (!closing) emit({ type: 'navigated' });
  }
} catch { emit({ type: 'error' }); }
finally { clearTimeout(deadline); await close(); }
