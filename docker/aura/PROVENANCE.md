# Aura image vendored sources

## mail-mcp (recipe:mail)

- Upstream: `https://github.com/martinzarfl/mail-mcp` (`@martinzarfl/mail-mcp` v1.0.2)
- Pinned commit: `7271c46442b7562bfaa7b8ffe8f13f8d40ab0dff`
- Vendored source archive: `mail-mcp-src.tar.gz` (GitHub source tarball at the pinned commit; no `node_modules`/`.git`)
- Build: `npm ci && npm run build && npm install -g` in `docker/aura/Dockerfile`, exposing the `mail-mcp` bin on PATH.

Vendored because no `chetto1983` fork exists for this package; pinning + vendoring
gives a reproducible, fully-offline build (Phase 17 AC7 — pre-baked recipes run
with egress blocked). The `recipe:mail` catalog entry invokes the globally
installed `mail-mcp` bin (not `npx`), so it needs no network at runtime.

> License note: upstream `mail-mcp` ships no explicit LICENSE file at the pinned
> commit. Re-confirm redistribution terms before publishing the image publicly.

## Other recipe sources (not vendored)

- `recipe:calculator` → `chetto1983/calculator-mcp-server` @ `46a1e66709bc387e8c223f15ec25fb5ae3a1af08` (own fork, pinned; uvx warm-cached at build).
- `recipe:whatsapp` → vendored separately under `docker/whatsapp/` (see its PROVENANCE.md).
