import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { expect, type Page } from '@playwright/test';
import { auraJSON, required } from './remoteAccessLive';

const execute = promisify(execFile);
async function docker(args: string[]) {
  try {
    return (await execute('docker', args, { timeout: 120_000, maxBuffer: 16 * 1024 * 1024 }))
      .stdout;
  } catch {
    throw new Error('Dedicated appliance diagnostic failed (output suppressed to protect secrets)');
  }
}
const compose = (...args: string[]) =>
  docker(['compose', '--project-directory', required('AURA_E2E_APPLIANCE_DIR'), ...args]);

export async function volumeIdentity() {
  const result: Record<string, string[]> = {};
  for (const service of ['postgres', 'aura', 'aura-cloudflared']) {
    const id = (await compose('ps', '-q', service)).trim();
    if (!id) throw new Error(`Missing dedicated ${service} container`);
    const mounts = JSON.parse(await docker(['inspect', '--format', '{{json .Mounts}}', id])) as {
      Type: string;
      Name?: string;
    }[];
    result[service] = mounts
      .filter((mount) => mount.Type === 'volume')
      .map((mount) => mount.Name ?? '')
      .sort();
    expect(result[service]?.length).toBeGreaterThan(0);
  }
  return result;
}

export async function idleAndNoProjection() {
  await compose(
    'exec',
    '-T',
    'aura-cloudflared',
    '/usr/local/bin/aura-cloudflared-supervisor',
    'healthcheck',
    'http://127.0.0.1:8085/healthz',
  );
  const id = (await compose('ps', '-q', 'aura-cloudflared')).trim();
  await expect
    .poll(async () => /(?:^|\/)cloudflared\s/m.test(await docker(['top', id, '-eo', 'args'])), {
      timeout: 30_000,
    })
    .toBe(false);
  const mounts = JSON.parse(await docker(['inspect', '--format', '{{json .Mounts}}', id])) as {
    Destination: string;
    Name?: string;
  }[];
  const name = mounts.find((mount) => mount.Destination === '/state')?.Name;
  if (!name) throw new Error('Projection volume missing');
  await docker([
    'run',
    '--rm',
    '--network',
    'none',
    '-v',
    `${name}:/state:ro`,
    'alpine:3.24',
    'sh',
    '-ec',
    'test ! -e /state/token; test -z "$(find /state -type f ! -name desired.json -print -quit)"',
  ]);
}

export async function auditSecretContainment(page: Page, tokens: string[]) {
  const id = (await compose('ps', '-q', 'aura-cloudflared')).trim();
  const outputs = [
    await compose('logs', '--no-color', 'aura', 'aura-cloudflared'),
    await docker(['top', id, '-eo', 'args']),
    await docker(['inspect', '--format', '{{json .Config.Env}}', id]),
    JSON.stringify(await auraJSON(page, '/api/settings/remote-access')),
    JSON.stringify(await auraJSON(page, '/api/settings')),
    await page.content(),
  ];
  // Check booleans only: failed expectations must not serialize a leaked credential.
  expect(
    outputs.some((output) => tokens.some((token) => token.length > 0 && output.includes(token))),
  ).toBe(false);
  const encrypted = await compose(
    'exec',
    '-T',
    'postgres',
    'sh',
    '-ec',
    'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Atc "SELECT count(*) = 2 AND bool_and(is_secret AND value LIKE \'enc:v1:%\') FROM aura.settings WHERE key IN (\'CLOUDFLARE_API_TOKEN\', \'CLOUDFLARE_TUNNEL_TOKEN\')"',
  );
  expect(encrypted.trim()).toBe('t');
}

export async function longLivedStream(page: Page, threadId: string) {
  const result = await page.evaluate(async (threadId) => {
    const source = new EventSource(
      `/api/conversations/${encodeURIComponent(threadId)}/swarm/events`,
    );
    let errors = 0;
    source.onerror = () => {
      errors++;
    };
    try {
      await new Promise<void>((resolve, reject) => {
        const timer = setTimeout(() => {
          reject(new Error('Long-lived stream never opened'));
        }, 15_000);
        source.onopen = () => {
          clearTimeout(timer);
          resolve();
        };
      });
      await new Promise((resolve) => setTimeout(resolve, 16_000));
      return { errors, state: source.readyState };
    } finally {
      source.close();
    }
  }, threadId);
  expect(result).toEqual({ errors: 0, state: 1 });
}
