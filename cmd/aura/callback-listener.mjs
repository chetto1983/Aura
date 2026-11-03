import http from 'node:http';
import readline from 'node:readline';
import { fileURLToPath } from 'node:url';

export async function listen({ path, port = 0, host = '127.0.0.1', expectedState, emit }) {
  let redirectURI;
  let consumed = false;
  const server = http.createServer((request, response) => {
    response.setHeader('Cache-Control', 'no-store');
    response.setHeader('Referrer-Policy', 'no-referrer');
    response.setHeader('Content-Type', 'text/plain; charset=utf-8');
    response.setHeader('Content-Security-Policy', "default-src 'none'; frame-ancestors 'none'");
    let url;
    try { url = new URL(request.url, redirectURI); }
    catch { response.writeHead(400).end('Accesso non valido.'); return; }
    const state = typeof expectedState === 'function' ? expectedState() : expectedState;
    const authority = `${host.includes(':') ? `[${host}]` : host}:${server.address().port}`;
    if (request.method !== 'GET' || request.headers.host !== authority ||
        url.pathname !== path || request.url.length > 16384 || consumed ||
        !state || url.searchParams.getAll('state').length !== 1 ||
        url.searchParams.get('state') !== state) {
      response.writeHead(400).end('Accesso non valido. Torna ad Aura e riprova.');
      return;
    }
    consumed = true;
    // A returning login can redirect during navigation, whose load must complete first.
    response.end("Torna ad Aura per vedere l'esito dell'accesso. Puoi chiudere questa finestra.",
      () => emit({ type: 'callback', query: url.search.slice(1) }));
  });
  server.requestTimeout = 30000;
  server.headersTimeout = 10000;
  server.maxHeadersCount = 32;
  await new Promise((resolve, reject) => {
    server.once('error', reject);
    // On Docker's IPv4 bridge Node listened on ::1 while its HTTP client dialed 127.0.0.1.
    // Keep the registered localhost URI, but make its loopback binding deterministic.
    server.listen(port, host === 'localhost' ? '127.0.0.1' : host, () => { server.off('error', reject); resolve(); });
  });
  redirectURI = `http://${host.includes(':') ? `[${host}]` : host}:${server.address().port}${path}`;
  return { server, redirectURI };
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const emit = (value) => process.stdout.write(`${JSON.stringify(value)}\n`);
  const input = readline.createInterface({ input: process.stdin, crlfDelay: Infinity });
  let listener;
  const close = () => {
    listener?.server.closeAllConnections();
    listener?.server.close();
    input.close();
    process.stdin.destroy();
  };
  for (const signal of ['SIGTERM', 'SIGINT']) process.on(signal, close);
  try {
    for await (const line of input) {
      if (listener) throw new Error('already_listening');
      const config = JSON.parse(line);
      if (config.type !== 'listen' || typeof config.state !== 'string' || !config.state ||
          typeof config.path !== 'string' || !config.path.startsWith('/') ||
          !Number.isInteger(config.port) || config.port < 1 || config.port > 65535) {
        throw new Error('invalid_listener');
      }
      listener = await listen({ ...config, expectedState: config.state, emit: (event) => {
        emit(event);
        if (event.type === 'callback') close();
      } });
      listener.server.on('error', () => { emit({ type: 'error' }); close(); });
      emit({ type: 'listening', redirect_uri: listener.redirectURI });
    }
  } catch {
    emit({ type: 'error' });
    process.exitCode = 1;
  } finally { close(); }
}
