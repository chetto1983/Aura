import { expect, test } from '@playwright/test';
import { gotoAuthenticated } from './auth';

// origin-isolation-live.spec.ts is the LIVE half of the artifact origin-isolation work
// (2026-09-09). Its unit halves are internal/agui/artifact_sandbox_test.go (the policy is
// generated) and internal/agui/data_proxy_test.go (the proxy handler). This one proves the
// running daemon actually serves them, because a green unit test on an unshipped binary
// proves nothing about the stack the operator uses.
//
// Two claims, both measured against the real cockpit:
//   1. GET /api/fetch relays an external JSON API for an authenticated cockpit session.
//   2. A sealed artifact document is served with `sandbox allow-scripts` — the directive
//      that gives it an opaque origin even when opened as a top-level tab, which is what
//      makes an operator connect-src wildcard safe.
//
// Gated because the artifact legs need a stack that HAS an artifact:
//   AURA_E2E_LIVE_ARTIFACT=1
//   AURA_E2E_ARTIFACT_ASSET=<asset uuid of an accepted text/html asset this identity owns>
//
// No-skip-as-green: the gate is declared once, at describe level, so the suite either does
// not run this file at all or runs every leg of it. There is deliberately NO branch inside
// the test that lets a missing artifact pass quietly — an absent id fails the expect below.
// An earlier draft of this file had exactly that branch, and it reported green while
// asserting nothing about the policy it exists to protect.

const live = process.env.AURA_E2E_LIVE_ARTIFACT === '1';
const assetID = process.env.AURA_E2E_ARTIFACT_ASSET ?? '';

test.describe('artifact origin isolation (live)', () => {
  test.skip(
    !live,
    'set AURA_E2E_LIVE_ARTIFACT=1 with AURA_E2E_ARTIFACT_ASSET against a live stack',
  );

  test('the daemon serves the data proxy and the sandboxed artifact policy', async ({ page }) => {
    let proofs = 0;
    expect(assetID, 'AURA_E2E_ARTIFACT_ASSET must name an artifact this identity owns').not.toBe(
      '',
    );

    await gotoAuthenticated(page, '/');

    // The session cookie rides the PAGE, not Playwright's APIRequestContext, so every
    // probe below runs as in-page fetch — the same way the cockpit itself calls the API.
    const call = async (url: string, maxBody = 200) =>
      page.evaluate(
        async ([target, cap]: [string, number]) => {
          const r = await fetch(target);
          return {
            status: r.status,
            contentType: r.headers.get('content-type'),
            cacheControl: r.headers.get('cache-control'),
            body: (await r.text()).slice(0, cap),
          };
        },
        [url, maxBody] as [string, number],
      );

    // 1. The data proxy relays a real external API under the operator's session.
    const relayed = await call(
      '/api/fetch?url=' + encodeURIComponent('https://api.coinbase.com/v2/prices/BTC-USD/spot'),
    );
    expect(
      relayed.status,
      `proxy should relay, got ${String(relayed.status)}: ${relayed.body}`,
    ).toBe(200);
    expect(relayed.contentType ?? '').toContain('application/json');
    expect(relayed.cacheControl ?? '').toBe('no-store');
    const quote = JSON.parse(relayed.body) as { data?: { amount?: string } };
    expect(quote.data?.amount, 'a real quote should come back').toBeTruthy();
    proofs += 1;

    // The allowlist is enforced server-side: HTML is data the proxy must never relay,
    // because it would run with Aura's own origin.
    const html = await call('/api/fetch?url=' + encodeURIComponent('https://example.com/'));
    expect(html.status, `an HTML target must be refused: ${html.body}`).toBe(502);
    proofs += 1;

    // 2. A sealed artifact document, loaded as a top-level tab, holds an opaque origin:
    // its own CSP is what proves it, and a live fetch under that CSP is what proves the
    // policy is usable rather than merely safe.
    {
      const rendered = await page.evaluate(async (id: string) => {
        const r = await fetch(`/api/assets/${id}/render`);
        return { status: r.status, csp: r.headers.get('content-security-policy') };
      }, assetID);
      expect(rendered.status, 'the artifact must render').toBe(200);
      const csp = rendered.csp ?? '';
      expect(csp, 'opaque origin comes from the sandbox directive').toContain(
        'sandbox allow-scripts',
      );
      expect(csp, 'allow-same-origin would let the document reclaim Aura’s origin').not.toContain(
        'allow-same-origin',
      );
      expect(csp, 'data connections are open, no allowlist to maintain').toContain('connect-src *');
      proofs += 1;

      // Now run INSIDE that document, under that exact policy.
      await page.goto(`/api/assets/${assetID}/render`);
      // Explicit fields, not Record<string, string>: an index signature makes every read
      // `string | undefined`, which the assertions below then cannot interpolate.
      const live = await page.evaluate(async () => {
        const out: { external: string; aura: string } = { external: '', aura: '' };
        try {
          const r = await fetch('https://api.coinbase.com/v2/prices/BTC-USD/spot');
          const j = (await r.json()) as { data?: { amount?: string } };
          out.external = `OK:${j.data?.amount ?? ''}`;
        } catch (e) {
          out.external = `BLOCKED:${(e as Error).name}`;
        }
        try {
          const r = await fetch('/api/me', { credentials: 'include' });
          out.aura = `REACHED:${String(r.status)}`;
        } catch (e) {
          out.aura = `BLOCKED:${(e as Error).name}`;
        }
        return out;
      });
      expect(live.external, `a live external fetch must work: ${live.external}`).toContain('OK:');
      expect(live.aura, `the artifact must not reach Aura: ${live.aura}`).toContain('BLOCKED:');
      proofs += 1;
    }

    expect(proofs, 'every leg must have asserted something').toBe(4);
  });
});
