// A static server for the probe pages: the bundles under /dist, the cockpit's fonts under /fonts,
// the fixtures under /media/<file> and a fake asset-status route for projectStore's loadProject.
// Media answers Range requests and ignores presigned-style query strings, the way a Garage
// presigned GET would. `cors: false` answers without Access-Control-Allow-Origin, to show which
// pipeline needs the object store's CORS and which does not. POST /upload/<name> writes the
// body to out/, which is how a page hands a rendered MP4 back without a base64 round trip.
import { createServer } from 'node:http';
import { createReadStream, createWriteStream, statSync } from 'node:fs';
import { extname, join, normalize } from 'node:path';
import { fileURLToPath } from 'node:url';

export const ROOT = fileURLToPath(new URL('..', import.meta.url));
const TYPES = {
  '.html': 'text/html', '.js': 'text/javascript', '.mjs': 'text/javascript', '.css': 'text/css',
  '.wasm': 'application/wasm', '.woff2': 'font/woff2', '.json': 'application/json',
  '.mp4': 'video/mp4', '.wav': 'audio/wav', '.mp3': 'audio/mpeg', '.ogg': 'audio/ogg',
  '.m4a': 'audio/mp4', '.png': 'image/png',
};
const MOUNTS = { dist: 'dist', fonts: 'dist/fonts', media: 'fixtures' };

function send(res, path, req, cors) {
  let size;
  try {
    size = statSync(path).size;
  } catch {
    res.writeHead(404).end();
    return;
  }
  const headers = { 'Content-Type': TYPES[extname(path)] ?? 'application/octet-stream', 'Accept-Ranges': 'bytes' };
  if (cors) headers['Access-Control-Allow-Origin'] = '*';
  const range = /^bytes=(\d*)-(\d*)$/.exec(req.headers.range ?? '');
  if (range) {
    const start = range[1] === '' ? size - Number(range[2]) : Number(range[1]);
    const end = range[1] !== '' && range[2] !== '' ? Number(range[2]) : size - 1;
    res.writeHead(206, { ...headers, 'Content-Range': `bytes ${start}-${end}/${size}`, 'Content-Length': end - start + 1 });
    if (req.method === 'HEAD') return res.end();
    createReadStream(path, { start, end }).pipe(res);
    return;
  }
  res.writeHead(200, { ...headers, 'Content-Length': size });
  if (req.method === 'HEAD') return res.end();
  createReadStream(path).pipe(res);
}

/** Starts a server on 127.0.0.1:`port`; every request is logged in `log` as `{method, path, query, status}`. */
export function startServer({ port, cors = true } = {}) {
  const log = [];
  const server = createServer((req, res) => {
    const url = new URL(req.url, 'http://x');
    const entry = { method: req.method, path: url.pathname, query: url.search !== '' };
    log.push(entry);
    res.on('finish', () => {
      entry.status = res.statusCode;
    });
    if (req.method === 'OPTIONS') {
      res.writeHead(204, cors ? { 'Access-Control-Allow-Origin': '*', 'Access-Control-Allow-Methods': 'GET, POST' } : {}).end();
      return;
    }
    if (req.method === 'POST' && url.pathname.startsWith('/upload/')) {
      const name = url.pathname.slice('/upload/'.length).replace(/[^\w.-]/g, '_');
      const out = createWriteStream(join(ROOT, 'out', name));
      req.pipe(out);
      out.on('finish', () => res.writeHead(200, cors ? { 'Access-Control-Allow-Origin': '*' } : {}).end());
      return;
    }
    // projectStore.loadProject asks the metadata route whether each source is gone; every
    // fixture is alive.
    const asset = /^\/api\/assets\/([^/]+)$/.exec(url.pathname);
    if (asset) {
      res.writeHead(200, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify({ id: decodeURIComponent(asset[1]), status: 'ready' }));
      return;
    }
    const [, mount, ...rest] = url.pathname.split('/');
    const base = MOUNTS[mount];
    const relative = normalize(decodeURIComponent(rest.join('/'))).replace(/^(\.\.[/\\])+/, '');
    if (base === undefined || relative.startsWith('..')) {
      res.writeHead(404).end();
      return;
    }
    send(res, join(ROOT, base, relative), req, cors);
  });
  return new Promise((resolve) => {
    server.listen(port, '127.0.0.1', () => {
      resolve({ origin: `http://127.0.0.1:${server.address().port}`, log, close: () => new Promise((r) => server.close(r)) });
    });
  });
}
