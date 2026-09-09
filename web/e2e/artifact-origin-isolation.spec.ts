import { expect, test } from '@playwright/test';
import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';

// artifact-origin-isolation.spec.ts pins the BROWSER BEHAVIOUR the artifact policy relies
// on. Its Go counterpart (internal/agui/artifact_connect_test.go) pins that the daemon
// GENERATES the policy; only a real browser can show what that policy DOES, and the whole
// design rests on what it does — so the reasoning is measured here rather than asserted in
// a comment.
//
// Self-contained on purpose: two throwaway servers on ephemeral ports stand in for Aura and
// for an external API, so this needs no stack, no session and no artifact, and it cannot go
// stale against a fixture.
//
// The four cases below are the table measured on 2026-09-09 that decided the design:
// removing the allowlist was safe ONLY because `sandbox` makes the origin opaque, and the
// no-sandbox row is what proves the directive is load-bearing rather than decorative.

const SEALED_FLOOR = "default-src 'none'; script-src 'unsafe-inline'";

/** The hostile document: it tries to read Aura's API with the operator's cookie, then an
 *  external API. Exactly what an artifact could attempt if a model were talked into it. */
const HOSTILE = `<!doctype html><html><head><title>a</title></head><body><div id="out">running</div>
<script>
const r = {};
(async () => {
  try {
    const x = await fetch('/api/secret', { credentials: 'include' });
    r.aura = 'READ:' + (await x.text()).slice(0, 32);
  } catch (e) { r.aura = 'BLOCKED'; }
  try {
    const m = await fetch(window.__EXTERNAL__ + '/quote');
    r.external = m.ok ? 'OK' : 'status' + m.status;
  } catch (e) { r.external = 'BLOCKED'; }
  try { r.cookie = document.cookie === '' ? 'empty' : 'VISIBLE'; } catch (e) { r.cookie = 'THREW'; }
  document.getElementById('out').textContent = JSON.stringify(r);
})();
</script></body></html>`;

interface Probe {
  readonly aura: string;
  readonly external: string;
  readonly cookie: string;
}

let aura: Server;
let external: Server;
let auraOrigin = '';
let externalOrigin = '';
let servedCSP = '';

const listen = (s: Server) =>
  new Promise<number>((resolve) => s.listen(0, '127.0.0.1', () => resolve((s.address() as AddressInfo).port)));

test.beforeAll(async () => {
  external = createServer((_q, res) => {
    res.writeHead(200, { 'Access-Control-Allow-Origin': '*', 'Content-Type': 'application/json' });
    res.end('{"amount":"1"}');
  });
  aura = createServer((q, res) => {
    if (q.url === '/api/secret') {
      const authed = (q.headers.cookie ?? '').includes('aura_session=');
      res.writeHead(authed ? 200 : 401, { 'Content-Type': 'text/plain' });
      return res.end(authed ? 'private-conversations' : 'unauthorized');
    }
    const headers: Record<string, string> = { 'Content-Type': 'text/html; charset=utf-8' };
    if (servedCSP !== '') headers['Content-Security-Policy'] = servedCSP;
    res.writeHead(200, headers);
    res.end(HOSTILE.replace('window.__EXTERNAL__', JSON.stringify(externalOrigin)));
  });
  externalOrigin = `http://127.0.0.1:${await listen(external)}`;
  auraOrigin = `http://127.0.0.1:${await listen(aura)}`;
});

test.afterAll(() => {
  aura.close();
  external.close();
});

/** Loads the hostile document as a TOP-LEVEL TAB (the path an iframe's own sandbox
 *  attribute does not cover) under `csp`, and reports what it managed to do. */
async function probe(page: import('@playwright/test').Page, csp: string): Promise<Probe> {
  servedCSP = csp;
  const ctx = page.context();
  await ctx.clearCookies();
  await ctx.addCookies([
    { name: 'aura_session', value: 'operator', url: `${auraOrigin}/`, httpOnly: true, sameSite: 'Strict' },
  ]);
  await page.goto(`${auraOrigin}/artifact`);
  await page.locator('#out').filter({ hasNotText: 'running' }).waitFor({ timeout: 8000 });
  return JSON.parse((await page.locator('#out').textContent()) ?? '{}') as Probe;
}

test.describe('artifact origin isolation (browser behaviour)', () => {
  // Serial because the cases share one stand-in server whose served CSP is the variable
  // under test. Playwright isolates workers today, so this is belt-and-braces against a
  // future config change turning that isolation off underneath the suite.
  test.describe.configure({ mode: 'serial' });

  test('sandbox denies Aura while an open connect-src still reaches external APIs', async ({ page }) => {
    const r = await probe(page, `sandbox allow-scripts; ${SEALED_FLOOR}; connect-src *`);
    expect(r.aura, 'an opaque origin sends no cookie, so Aura refuses').toBe('BLOCKED');
    expect(r.external, 'external data must stay reachable — that is the point').toBe('OK');
    expect(r.cookie, 'document.cookie is unreachable from an opaque origin').not.toBe('VISIBLE');
  });

  test('WITHOUT sandbox the same document reads Aura with the operator cookie', async ({ page }) => {
    // The control. If this ever starts reporting BLOCKED, the first test has stopped
    // proving anything and the suite would go green over a policy that no longer protects.
    const r = await probe(page, `${SEALED_FLOOR}; connect-src *`);
    expect(r.aura, 'without the sandbox directive a top-level artifact IS same-origin').toContain('READ:');
    expect(r.external).toBe('OK');
  });

  test('a closed connect-src blocks the legitimate API too — the cost the allowlist had', async ({ page }) => {
    const r = await probe(page, `sandbox allow-scripts; ${SEALED_FLOOR}; connect-src 'none'`);
    expect(r.aura).toBe('BLOCKED');
    expect(r.external, "the old allowlist's real cost: legitimate data blocked as well").toBe('BLOCKED');
  });

  test('sandbox never grants allow-same-origin, which would undo it', async ({ page }) => {
    const r = await probe(page, `sandbox allow-scripts allow-same-origin; ${SEALED_FLOOR}; connect-src *`);
    // Documents why the token is forbidden in artifactRenderCSP: it hands the origin back.
    expect(r.aura, 'allow-same-origin restores the origin and with it the cookie').toContain('READ:');
  });
});
