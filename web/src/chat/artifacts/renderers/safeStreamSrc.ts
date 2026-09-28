// Media components only receive a stream sibling from the active Aura asset tier.
// This rejects provider, cross-origin and ambiguous query/fragment URLs.
export function safeStreamSrc(raw: string): string | null {
  if (!raw.startsWith('/') || raw.startsWith('//')) return null;
  try {
    const url = new URL(raw, location.origin);
    if (url.origin !== location.origin || url.search || url.hash) return null;
    const path = url.pathname;
    if (
      !/^\/api\/assets\/[^/]+\/stream$/.test(path) &&
      !/^\/s\/[^/]+\/asset\/[^/]+\/stream$/.test(path) &&
      !/^\/api\/shares\/[^/]+\/asset\/[^/]+\/stream$/.test(path)
    )
      return null;
    return url.href;
  } catch {
    return null;
  }
}
