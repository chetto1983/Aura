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
const children = new Set();

const browser = (...args) => new Promise((resolve, reject) => {
  const child = execFile('agent-browser', [
    '--session', session, '--profile', profile, '--restore-save', 'never', ...args,
  ], {
    timeout: args[0] === 'close' ? 4000 : 35000,
    maxBuffer: 4096,
    env: { ...process.env, AGENT_BROWSER_AUTOSAVE_INTERVAL_MS: '0' },
  }, (err) => {
    children.delete(child);
    if (err) reject(new Error('browser_unavailable'));
    else resolve();
  });
  children.add(child);
});

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
    await browser('open', url.href);
    if (!closing) emit({ type: 'navigated' });
  }
} catch { emit({ type: 'error' }); }
finally { clearTimeout(deadline); await close(); }
