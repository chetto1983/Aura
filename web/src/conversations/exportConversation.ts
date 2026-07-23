// Conversation export (fix-plan 2.10 "wire export UI"): the server already owns
// GET /api/conversations/{id}/export — owner-scoped (foreign/absent -> 404),
// read-only, absent/unrecognized `format` degrades to Markdown. This module is
// the client leg: fetch the body and hand it to the browser as a download named
// by the server's Content-Disposition (RFC-6266: quoted ASCII fallback +
// filename*=UTF-8'' extended param — internal/agui/content_disposition.go).

/**
 * Extract the download filename from an RFC-6266 Content-Disposition value.
 * Prefers the unicode `filename*=UTF-8''…` extended param, falls back to the
 * quoted `filename="…"`, then to the caller's fallback. Never throws: a
 * malformed header or bad percent-encoding degrades to the fallback.
 */
export function filenameFromContentDisposition(header: string | null, fallback: string): string {
  if (header === null || header.length === 0) return fallback;
  const extended = /filename\*=UTF-8''([^;]+)/i.exec(header)?.[1];
  if (extended !== undefined) {
    try {
      const decoded = decodeURIComponent(extended).trim();
      if (decoded.length > 0) return decoded;
    } catch {
      // fall through to the quoted form
    }
  }
  const quoted = /filename="([^"]*)"/i.exec(header)?.[1]?.trim();
  return quoted !== undefined && quoted.length > 0 ? quoted : fallback;
}

/** Standard blob+anchor download; revokes the object URL after the click. */
function downloadBlob(blob: Blob, filename: string): void {
  const url = URL.createObjectURL(blob);
  try {
    const anchor = document.createElement('a');
    anchor.href = url;
    anchor.download = filename;
    document.body.appendChild(anchor);
    anchor.click();
    anchor.remove();
  } finally {
    URL.revokeObjectURL(url);
  }
}

/**
 * GET the Markdown export for a conversation and trigger a browser download.
 * Read-only, same-origin credentials, no Idempotency-Key. Throws on a non-2xx
 * response (404 for foreign/absent ids) — the caller decides how to surface it.
 */
export async function exportConversationMarkdown(conversationId: string): Promise<void> {
  const res = await fetch(`/api/conversations/${encodeURIComponent(conversationId)}/export`, {
    method: 'GET',
    credentials: 'same-origin',
  });
  if (!res.ok) {
    throw new Error(`HTTP ${String(res.status)}`);
  }
  const blob = await res.blob();
  const filename = filenameFromContentDisposition(
    res.headers.get('Content-Disposition'),
    'conversation.md',
  );
  downloadBlob(blob, filename);
}
