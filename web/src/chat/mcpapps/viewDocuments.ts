import type { McpViewDocument } from './hostProtocol';

// viewDocuments — the per-session cache of MCP Apps view documents.
//
// A view document is STATIC for the life of a mount: the backend reads it once,
// when the server mounts, and serves the same bytes until the daemon restarts.
// Fetching it per rendered card would re-download the same ~20 KB for every tool
// call in a conversation, so the pair (server, resourceUri) is fetched once and
// shared — the same key the backend catalogs by.
//
// The cache stores the PROMISE, not the result: two cards rendering in the same
// tick must issue one request, not two. A failed fetch is evicted so a transient
// 503 (the daemon still wiring its mounts) does not poison the key for the rest
// of the session.

const documents = new Map<string, Promise<McpViewDocument>>();

const cacheKey = (server: string, resourceURI: string) => `${server}|${resourceURI}`;

export function fetchViewDocument(server: string, resourceURI: string): Promise<McpViewDocument> {
  const key = cacheKey(server, resourceURI);
  const cached = documents.get(key);
  if (cached) return cached;

  const pending = fetch(
    `/api/mcp/view?server=${encodeURIComponent(server)}&uri=${encodeURIComponent(resourceURI)}`,
    { credentials: 'same-origin', headers: { Accept: 'application/json' } },
  ).then(async (res) => {
    if (!res.ok) throw new Error(`mcp view ${String(res.status)}`);
    return (await res.json()) as McpViewDocument;
  });

  pending.catch(() => documents.delete(key));
  documents.set(key, pending);
  return pending;
}
