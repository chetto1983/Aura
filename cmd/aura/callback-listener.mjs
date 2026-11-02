import http from 'node:http';

export async function listen({ path, port = 0, expectedState, emit }) {
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
    if (request.method !== 'GET' || request.headers.host !== `127.0.0.1:${server.address().port}` ||
        url.pathname !== path || request.url.length > 16384 || consumed ||
        !state || url.searchParams.getAll('state').length !== 1 ||
        url.searchParams.get('state') !== state) {
      response.writeHead(400).end('Accesso non valido. Torna ad Aura e riprova.');
      return;
    }
    consumed = true;
    emit({ type: 'callback', query: url.search.slice(1) });
    // A returning login can redirect during navigation, whose load must complete first.
    response.end("Torna ad Aura per vedere l'esito dell'accesso. Puoi chiudere questa finestra.");
  });
  server.requestTimeout = 30000;
  server.headersTimeout = 10000;
  server.maxHeadersCount = 32;
  await new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(port, '127.0.0.1', () => { server.off('error', reject); resolve(); });
  });
  redirectURI = `http://127.0.0.1:${server.address().port}${path}`;
  return { server, redirectURI };
}
