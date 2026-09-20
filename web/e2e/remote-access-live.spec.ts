import { readFileSync } from 'node:fs';
import { expect, test } from '@playwright/test';
import { createConversation } from './live';
import {
  auditSecretContainment,
  idleAndNoProjection,
  longLivedStream,
  volumeIdentity,
} from './support/remoteAccessLiveAppliance';
import {
  auraJSON,
  checkpoint,
  cloudflare,
  cloudflareList,
  ids,
  inventory,
  required,
  restartAppliance,
  status,
  streamAndControl,
  uploadAndDownload,
  verifyTunnelNetwork,
} from './support/remoteAccessLive';

const enabled = process.env.AURA_E2E_CLOUDFLARE === '1';
async function enterToken(page: import('@playwright/test').Page, token: string) {
  await page.getByLabel('Cloudflare API token', { exact: true }).evaluate((element, token) => {
    const descriptor = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value');
    if (!descriptor?.set) throw new Error('Token input unavailable');
    descriptor.set.call(element, token);
    element.dispatchEvent(new Event('input', { bubbles: true }));
  }, token);
}
test.describe('attended named-tunnel acceptance', () => {
  test.skip(!enabled, 'Not opted in; this is not live acceptance evidence');
  test.use({
    trace: 'off',
    screenshot: 'off',
    video: 'off',
    serviceWorkers: 'allow',
    ignoreHTTPSErrors: false,
  });
  test.describe.configure({ retries: 0, mode: 'serial' });

  test('OTP, Authula, SSE, Garage, Studio, Gateway, rotation, recovery and owned cleanup', async ({
    browser,
  }, info) => {
    test.setTimeout(60 * 60 * 1000);
    const account = required('CLOUDFLARE_ACCOUNT_ID');
    const zoneName = required('CLOUDFLARE_TEST_ZONE');
    const run = required('AURA_E2E_CLOUDFLARE_RUN');
    const admin = required('AURA_E2E_ADMIN_EMAIL');
    const team = required('CLOUDFLARE_TEAM_NAME');
    required('CLOUDFLARE_API_TOKEN');
    required('CLOUDFLARE_REPLACEMENT_API_TOKEN');
    required('AURA_E2E_DENIED_EMAIL');
    required('AURA_E2E_UNRELATED_TXT_NAME');
    required('AURA_E2E_APPLIANCE_DIR');
    expect(account).toMatch(/^[a-fA-F0-9]{32}$/);
    expect(zoneName).toMatch(/^[a-z0-9-]+(?:\.[a-z0-9-]+)+$/);
    expect(zoneName.endsWith('trycloudflare.com')).toBe(false);
    expect(new URL(required('AURA_E2E_ORIGIN')).protocol).toBe('https:');
    expect(process.env.AURA_E2E_CLOUDFLARE_DEDICATED).toBe('1');
    expect(run).toMatch(/^aura-e2e-[0-9]{14}-[a-f0-9]{6}$/);
    const hostname = `${run}.${zoneName}`;
    const warpHostname = `${run}-warp.${zoneName}`;
    const origin = `https://${hostname}`;
    const direct = await browser.newContext({ ignoreHTTPSErrors: true, serviceWorkers: 'block' });
    const external = await browser.newContext({
      ignoreHTTPSErrors: false,
      serviceWorkers: 'allow',
    });
    const local = await direct.newPage();
    const publicPage = await external.newPage();
    const evidence: Record<string, unknown> = {
      run,
      hostname,
      warpHostname,
      live: true,
      complete: false,
    };
    let configured = false;
    let before: Awaited<ReturnType<typeof inventory>> | undefined;
    let zone = '';
    // Register cleanup before candidate validation or configuration can mutate anything.
    // The only delete goes through Aura's persisted-ID/ownership guard. No CF DELETE exists here.
    const cleanup = async () => {
      if (!configured) return;
      await local.goto(`${required('AURA_E2E_ORIGIN')}/?settings=remote-access`);
      const current = await status(local);
      if (
        current.public_hostname &&
        (current.public_hostname !== hostname ||
          current.warp_hostname !== warpHostname ||
          current.account_id !== account ||
          current.zone_name !== zoneName)
      ) {
        throw new Error('Cleanup refused: integration no longer belongs to this run');
      }
      if (current.public_hostname) {
        await auraJSON(local, '/api/settings/remote-access', 'DELETE', { hostname });
        await expect
          .poll(async () => (await status(local)).public_hostname, { timeout: 180_000 })
          .toBeUndefined();
      }
      if (before) {
        const after = await inventory(account, zone);
        for (const kind of ['dns', 'tunnels', 'apps', 'posture', 'routes'] as const) {
          expect(ids(after[kind]), `cleanup ${kind}: no owned leaks or foreign deletion`).toEqual(
            ids(before[kind]),
          );
        }
        const remainingZone = await cloudflare<{ id: string }>(`zones/${zone}`);
        expect(remainingZone.result.id).toBe(zone);
        expect(after.dns.filter((row) => row.type === 'TXT')).toEqual(
          before.dns.filter((row) => row.type === 'TXT'),
        );
      }
      evidence.cleanup = 'zero leaked run resources; baseline resources and zone preserved';
      configured = false;
    };
    try {
      await local.goto(`${required('AURA_E2E_ORIGIN')}/?settings=remote-access`);
      await checkpoint(
        `Sign into the dedicated direct cockpit as ${admin}. Complete initial setup and use English. Do not configure Remote Access yet.`,
      );
      const me = await auraJSON<{ name: string; capabilities: string[] }>(local, '/api/me');
      expect(me.name.toLowerCase()).toBe(admin.toLowerCase());
      expect(me.capabilities).toContain('identity.create');
      const pristine = await status(local);
      expect(pristine).toMatchObject({
        enabled: false,
        phase: 'disabled',
        api_token_set: false,
        tunnel_token_set: false,
      });
      expect(pristine.public_hostname).toBeUndefined();
      await idleAndNoProjection();
      const zones = await cloudflareList(
        `zones?name=${encodeURIComponent(zoneName)}&account.id=${encodeURIComponent(account)}`,
      );
      expect(zones).toHaveLength(1);
      expect(zones[0]?.status, 'test zone must be pending to measure nameserver resume').toBe(
        'pending',
      );
      zone = zones[0]?.id ?? '';
      before = await inventory(account, zone);
      expect(
        before.dns.some(
          (row) => row.type === 'TXT' && row.name === required('AURA_E2E_UNRELATED_TXT_NAME'),
        ),
      ).toBe(true);
      expect(before.dns.some((row) => row.name === hostname || row.name === warpHostname)).toBe(
        false,
      );
      await local.goto(`${required('AURA_E2E_ORIGIN')}/?settings=remote-access`);
      await enterToken(local, required('CLOUDFLARE_API_TOKEN'));
      await local.getByRole('button', { name: 'Verify token', exact: true }).click();
      await local.getByLabel('Cloudflare account', { exact: true }).selectOption(account);
      await local.getByLabel('Registered domain', { exact: true }).fill(zoneName);
      await local.getByLabel('Public hostname label', { exact: true }).fill(run);
      await local.getByLabel('WARP hostname label', { exact: true }).fill(`${run}-warp`);
      configured = true;
      await local.getByRole('button', { name: 'Save and start setup' }).click();
      // A pending test zone is deliberate: an already-active zone cannot prove the wait leg.
      await expect.poll(async () => (await status(local)).phase).toBe('waiting_nameservers');
      const waiting = await status(local);
      await restartAppliance('aura');
      await expect
        .poll(
          async () => {
            try {
              return (await status(local)).phase;
            } catch {
              return 'restarting';
            }
          },
          { timeout: 120_000 },
        )
        .toBe('waiting_nameservers');
      await local.reload();
      expect((await status(local)).nameservers).toEqual(waiting.nameservers);
      await checkpoint(
        'Update nameservers at your registrar to the nameservers shown in the cockpit. Continue once Cloudflare has activated the zone.',
      );
      await expect
        .poll(async () => (await status(local)).phase, { timeout: 300_000 })
        .toBe('connecting');
      evidence.nameserverRestartResume = true;
      const resources = await inventory(account, zone);
      expect(ids(resources.routes)).toEqual(ids(before.routes));
      const dns = resources.dns.find((row) => row.name === hostname && row.type === 'CNAME');
      if (!dns?.content || !dns.comment) throw new Error('Owned CNAME missing');
      const tunnelId = dns.content.replace(/\.cfargotunnel\.com\.?$/, '');
      const tunnel = resources.tunnels.find(
        (row) => row.id === tunnelId && row.name === dns.comment,
      );
      expect(tunnel).toBeDefined();
      const config = await cloudflare<{
        config: {
          ingress: { hostname?: string; service: string }[];
          'warp-routing'?: { enabled: boolean };
        };
      }>(`accounts/${account}/cfd_tunnel/${tunnelId}/configurations`);
      expect(config.result.config.ingress).toEqual([
        { hostname, service: 'http://caddy:8080' },
        { hostname: warpHostname, service: 'http://caddy:8080' },
        { service: 'http_status:404' },
      ]);
      expect(config.result.config['warp-routing']?.enabled ?? false).toBe(false);
      await verifyTunnelNetwork();
      evidence.noPrivateRoute = true;

      await publicPage.goto(`${origin}/?settings=remote-access`);
      await checkpoint(
        'In the public browser use the allowed administrator email, receive the real OTP, complete Access and Authula, and leave Remote access open.',
      );
      // A separate unauthenticated context must be gated at the edge.
      const denied = await browser.newContext({ ignoreHTTPSErrors: false });
      try {
        const page = await denied.newPage();
        let submitted = false;
        page.on('request', (request) => {
          const body = request.postData() ?? '';
          const email = required('AURA_E2E_DENIED_EMAIL');
          if (
            request.method() === 'POST' &&
            (body.includes(email) || body.includes(encodeURIComponent(email)))
          )
            submitted = true;
        });
        await page.goto(origin);
        await checkpoint(
          `Try Access with the unlisted email ${required('AURA_E2E_DENIED_EMAIL')} in the new browser. It must not reach Aura.`,
        );
        expect(submitted, 'unlisted identity was actually submitted to Access').toBe(true);
        const response = await denied.request.get(`${origin}/api/me`, { maxRedirects: 0 });
        expect([302, 303, 401, 403]).toContain(response.status());
        expect(
          (await denied.cookies(origin)).some((cookie) => cookie.name === 'CF_Authorization'),
        ).toBe(false);
      } finally {
        await denied.close();
      }
      expect(new URL(publicPage.url()).origin).toBe(origin);
      const publicMe = await auraJSON<{ name: string }>(publicPage, '/api/me');
      expect(publicMe.name.toLowerCase()).toBe(admin.toLowerCase());
      expect(
        (await external.cookies(origin)).some((cookie) => cookie.name === 'CF_Authorization'),
      ).toBe(true);
      await publicPage.getByRole('button', { name: 'Confirm external access' }).click();
      await expect.poll(async () => (await status(local)).phase).toBe('healthy');
      evidence.otpAndAuthula = true;
      const thread = await createConversation(publicPage, run);
      evidence.sse = await streamAndControl(publicPage, thread);
      await longLivedStream(publicPage, thread);
      await publicPage.evaluate(async () => {
        await navigator.serviceWorker.ready;
      });
      expect(await publicPage.evaluate(() => navigator.serviceWorker.controller?.state)).toBe(
        'activated',
      );
      const asset = await uploadAndDownload(
        publicPage,
        readFileSync('e2e/fixtures/media-edit/photo.png').toString('base64'),
        `${run}.png`,
      );
      evidence.garage = asset;
      const finalized = publicPage.waitForResponse(
        (response) =>
          response.url().startsWith(`${origin}/api/studio/uploads/`) &&
          response.url().endsWith('/finalize') &&
          response.request().method() === 'POST',
        { timeout: 900_000 },
      );
      await checkpoint(
        'In the public cockpit open the uploaded image from Documents, choose Edit, apply Sepia, and Save to library. Keep the browser open, then continue.',
      );
      const saved = await finalized;
      expect(saved.ok()).toBe(true);
      const edited = (await saved.json()) as { id: string };
      expect(edited.id).not.toBe(asset);
      const library = await auraJSON<{ assets: { id: string }[] }>(
        publicPage,
        '/api/studio/library',
      );
      expect(library.assets.some((row) => row.id === edited.id)).toBe(true);
      const download = await external.request.get(`${origin}/api/assets/${edited.id}/download`);
      expect(download.ok()).toBe(true);
      expect(
        (await download.body()).equals(readFileSync('e2e/fixtures/media-edit/photo.png')),
      ).toBe(false);
      evidence.studio = edited.id;

      for (const enrolled of [false, true]) {
        await checkpoint(
          enrolled
            ? `Enroll and connect this browser machine's Cloudflare One client to organization ${team} through Gateway. Do not use consumer WARP. Complete OTP and Authula at https://${warpHostname} in the new browser after continuing.`
            : 'Disconnect the Cloudflare One client on this browser machine. The next request must be denied.',
        );
        const context = await browser.newContext({ ignoreHTTPSErrors: false });
        try {
          const page = await context.newPage();
          await page.goto(`https://${warpHostname}/`);
          await checkpoint(
            enrolled
              ? 'Complete Access OTP and Authula in the WARP browser.'
              : 'Try the allowed admin email and OTP at the WARP hostname; confirm the Gateway posture denies access.',
          );
          if (enrolled) {
            expect((await auraJSON<{ name: string }>(page, '/api/me')).name.toLowerCase()).toBe(
              admin.toLowerCase(),
            );
          } else {
            await expect(
              page.getByText(/you do not have access|access denied|forbidden/i).first(),
            ).toBeVisible();
            const response = await context.request.get(`https://${warpHostname}/api/me`, {
              maxRedirects: 0,
            });
            expect([302, 303, 401, 403]).toContain(response.status());
          }
        } finally {
          await context.close();
        }
      }
      evidence.gatewayAllowDeny = true;
      await publicPage.goto(`${origin}/?settings=remote-access`);
      const replacement = required('CLOUDFLARE_REPLACEMENT_API_TOKEN');
      expect(replacement === required('CLOUDFLARE_API_TOKEN')).toBe(false);
      const replacementGeneration = (await status(local)).generation;
      await enterToken(publicPage, replacement);
      await publicPage.getByRole('button', { name: 'Verify token', exact: true }).click();
      await expect
        .poll(async () => (await status(local)).generation)
        .toBeGreaterThan(replacementGeneration);
      await expect
        .poll(async () => (await status(local)).phase, { timeout: 120_000 })
        .toBe('connecting');
      await publicPage.reload();
      await publicPage.getByRole('button', { name: 'Confirm external access' }).click();
      await expect.poll(async () => (await status(local)).phase).toBe('healthy');
      const oldToken = (
        await cloudflare<string>(`accounts/${account}/cfd_tunnel/${tunnelId}/token`)
      ).result;
      const refreshGeneration = (await status(local)).generation;
      const samples: { at: number; status: number }[] = [];
      let polling = false;
      const timer = setInterval(() => {
        if (polling) return;
        polling = true;
        void external.request
          .get(`${origin}/api/me`, { timeout: 5000, maxRedirects: 0 })
          .then((response) => samples.push({ at: Date.now(), status: response.status() }))
          .catch(() => samples.push({ at: Date.now(), status: 0 }))
          .finally(() => {
            polling = false;
          });
      }, 1000);
      try {
        await checkpoint(
          'Rotate ONLY this run’s tunnel token through the documented Cloudflare Dashboard action. Do not copy the token. Return here after rotation; Aura will refresh it next.',
        );
        const rotatedToken = (
          await cloudflare<string>(`accounts/${account}/cfd_tunnel/${tunnelId}/token`)
        ).result;
        expect(rotatedToken !== oldToken, 'Dashboard actually rotated the connector token').toBe(
          true,
        );
        await publicPage.getByRole('button', { name: 'Refresh Dashboard-rotated token' }).click();
        await expect
          .poll(async () => (await status(local)).generation)
          .toBeGreaterThan(refreshGeneration);
        await expect
          .poll(async () => (await status(local)).phase, { timeout: 120_000 })
          .toBe('connecting');
        await publicPage.reload();
        await publicPage.getByRole('button', { name: 'Confirm external access' }).click();
        await expect.poll(async () => (await status(local)).phase).toBe('healthy');
        await auditSecretContainment(publicPage, [
          required('CLOUDFLARE_API_TOKEN'),
          replacement,
          oldToken,
          rotatedToken,
        ]);
      } finally {
        clearInterval(timer);
      }
      evidence.rotationHealth = {
        samples,
        failed: samples.filter((sample) => sample.status !== 200).length,
      };
      for (const service of ['aura', 'aura-cloudflared'] as const) {
        await restartAppliance(service);
        await expect
          .poll(
            async () => {
              try {
                return (await status(local)).public_hostname;
              } catch {
                return '';
              }
            },
            { timeout: 120_000 },
          )
          .toBe(hostname);
        await expect
          .poll(
            async () => {
              try {
                return (
                  await external.request.get(`${origin}/api/me`, { maxRedirects: 0 })
                ).status();
              } catch {
                return 0;
              }
            },
            { timeout: 120_000 },
          )
          .toBe(200);
      }
      evidence.restartRecovery = true;
      const volumes = await volumeIdentity();
      await checkpoint(
        'On this dedicated Ubuntu appliance rerun the same self-extracting installer, then run its installed edge image updater. Do not delete volumes. Continue only after both finish.',
      );
      expect(await volumeIdentity()).toEqual(volumes);
      expect((await status(local)).public_hostname).toBe(hostname);
      expect((await external.request.get(`${origin}/api/me`, { maxRedirects: 0 })).status()).toBe(
        200,
      );
      evidence.installerRerunAndUpdate = true;
      await auraJSON(local, '/api/settings/remote-access/disable', 'POST', {});
      await expect.poll(async () => (await status(local)).phase).toBe('disabled');
      await idleAndNoProjection();
      expect(ids((await inventory(account, zone)).dns)).toEqual(ids(resources.dns));
      await cleanup();
      evidence.complete = true;
    } finally {
      try {
        await cleanup();
      } finally {
        await info.attach('live-acceptance', {
          body: JSON.stringify(evidence, null, 2),
          contentType: 'application/json',
        });
        await external.close();
        await direct.close();
      }
    }
  });
});
