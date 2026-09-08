import { expect, test } from '@playwright/test';
import { gotoAuthenticated } from './auth';

interface WorkerVersion {
  versionId: string;
  scriptURL: string;
  status: string;
  runningStatus: string;
}

test.use({ serviceWorkers: 'allow' });

test('PWA caches assets but leaves live agent streams on the native network', async ({
  page,
  browserName,
}) => {
  test.setTimeout(90_000);
  await gotoAuthenticated(page, '/');
  // Auth's shared helper stubs onboarding/readiness; this regression uses real HTTP.
  await page.unrouteAll({ behavior: 'wait' });
  await page.evaluate(async () => {
    await navigator.serviceWorker.ready;
  });
  await expect
    .poll(() => page.evaluate(() => navigator.serviceWorker.controller?.state))
    .toBe('activated');

  const assetResponse = page.waitForResponse((response) => response.url().endsWith('/pwa-192.png'));
  await page.evaluate(async () => {
    await fetch('/pwa-192.png');
  });
  expect((await assetResponse).fromServiceWorker()).toBe(true);

  const created = await page.evaluate(async () => {
    const response = await fetch('/api/conversations', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ title: 'PWA stream lifetime regression' }),
    });
    return { status: response.status, body: (await response.json()) as { ID: string } };
  });
  expect(created.status).toBe(201);
  const endpoint = `/api/conversations/${created.body.ID}/swarm/events`;
  const stream = await page.evaluateHandle(() => ({
    source: null as EventSource | null,
    errors: 0,
  }));
  const cdp = browserName === 'chromium' ? await page.context().newCDPSession(page) : null;
  try {
    const versions = new Map<string, WorkerVersion>();
    if (cdp !== null) {
      cdp.on(
        'ServiceWorker.workerVersionUpdated',
        ({ versions: updated }: { versions: WorkerVersion[] }) => {
          for (const version of updated) versions.set(version.versionId, version);
        },
      );
      await cdp.send('ServiceWorker.enable');
    }
    const responsePromise = page.waitForResponse((response) => response.url().endsWith(endpoint));
    await stream.evaluate((probe, url) => {
      probe.source = new EventSource(url);
      probe.source.addEventListener('error', () => {
        probe.errors++;
      });
    }, endpoint);
    const response = await responsePromise;
    expect(response.status()).toBe(200);
    expect(response.headers()['content-type']).toContain('text/event-stream');
    await expect.poll(() => stream.evaluate((probe) => probe.source?.readyState)).toBe(1);
    expect(response.fromServiceWorker()).toBe(false);

    if (cdp !== null) {
      const controller = await page.evaluate(() => navigator.serviceWorker.controller?.scriptURL);
      const active = () =>
        [...versions.values()].find(
          (version) =>
            version.scriptURL === controller &&
            version.status === 'activated' &&
            version.runningStatus === 'running',
        );
      await expect.poll(() => active()?.versionId).toBeDefined();
      const worker = active();
      if (worker === undefined) throw new Error('Active PWA worker disappeared before stop');
      await cdp.send('ServiceWorker.stopWorker', { versionId: worker.versionId });
      await expect.poll(() => stream.evaluate((probe) => probe.source?.readyState)).toBe(1);
    }
    // Cross the server's 15-second heartbeat after the worker stops. A reconnect
    // cannot pass: any EventSource error, including a transient one, is retained.
    await page.waitForTimeout(16_000);
    expect(
      await stream.evaluate((probe) => ({ state: probe.source?.readyState, errors: probe.errors })),
    ).toEqual({ state: 1, errors: 0 });
  } finally {
    await stream.evaluate((probe) => probe.source?.close());
    await stream.dispose();
    await cdp?.detach();
    const deleted = await page.evaluate(async (id) => {
      const response = await fetch(`/api/conversations/${id}`, { method: 'DELETE' });
      return response.status;
    }, created.body.ID);
    expect([200, 204]).toContain(deleted);
  }
});
