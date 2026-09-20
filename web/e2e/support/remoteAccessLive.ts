import { execFile } from 'node:child_process';
import { createReadStream } from 'node:fs';
import { createInterface } from 'node:readline/promises';
import { promisify } from 'node:util';
import { expect, type Page } from '@playwright/test';
import type { RemoteAccessStatusDTO } from '../../src/settings/remoteAccess/remoteAccessApi';

const execute = promisify(execFile);
export function required(name: string): string {
  const value = process.env[name];
  if (!value?.trim()) throw new Error(`Live acceptance requires ${name}`);
  return value;
}

export async function checkpoint(instruction: string) {
  const input = createReadStream('/dev/tty');
  const prompt = createInterface({ input, output: process.stdout });
  try {
    const answer = await prompt.question(`${instruction}\nType CONTINUE when ready: `);
    if (answer !== 'CONTINUE') throw new Error('Operator checkpoint incomplete');
  } finally {
    prompt.close();
    input.destroy();
  }
}

export async function auraJSON<T>(
  page: Page,
  path: string,
  method = 'GET',
  body?: unknown,
): Promise<T> {
  return page.evaluate(
    async ({ path, method, body }) => {
      const response = await fetch(path, {
        method,
        credentials: 'same-origin',
        redirect: 'error',
        headers: { 'Content-Type': 'application/json', 'Idempotency-Key': crypto.randomUUID() },
        ...(body === undefined ? {} : { body: JSON.stringify(body) }),
      });
      if (!response.ok) throw new Error(`Aura HTTP ${String(response.status)}`);
      return response.json() as Promise<T>;
    },
    { path, method, body },
  );
}
export const status = (page: Page) =>
  auraJSON<RemoteAccessStatusDTO>(page, '/api/settings/remote-access');

interface CloudflareEnvelope<T> {
  success: boolean;
  result: T;
  result_info?: { total_pages?: number };
}
export async function cloudflare<T>(path: string): Promise<CloudflareEnvelope<T>> {
  const response = await fetch(`https://api.cloudflare.com/client/v4/${path}`, {
    headers: { Authorization: `Bearer ${required('CLOUDFLARE_API_TOKEN')}` },
    signal: AbortSignal.timeout(30_000),
    redirect: 'error',
  });
  if (!response.ok) throw new Error(`Cloudflare read failed: HTTP ${String(response.status)}`);
  const body = (await response.json()) as CloudflareEnvelope<T>;
  if (!body.success) throw new Error('Cloudflare read refused');
  return body;
}
export interface Resource {
  id: string;
  status?: string;
  name?: string;
  type?: string;
  content?: string;
  comment?: string;
  domain?: string;
  tunnel_id?: string;
}
export async function cloudflareList(path: string): Promise<Resource[]> {
  const rows: Resource[] = [];
  for (let page = 1; page <= 100; page++) {
    const reply = await cloudflare<Resource[]>(
      `${path}${path.includes('?') ? '&' : '?'}per_page=100&page=${String(page)}`,
    );
    if (!Array.isArray(reply.result)) throw new Error('Cloudflare list envelope changed');
    rows.push(...reply.result);
    if (page >= (reply.result_info?.total_pages ?? 1)) return rows;
  }
  throw new Error('Cloudflare pagination exceeded safety bound');
}
export async function inventory(account: string, zone: string) {
  const [dns, tunnels, apps, posture, routes] = await Promise.all([
    cloudflareList(`zones/${zone}/dns_records`),
    cloudflareList(`accounts/${account}/cfd_tunnel?is_deleted=false`),
    cloudflareList(`accounts/${account}/access/apps`),
    cloudflareList(`accounts/${account}/devices/posture`),
    cloudflareList(`accounts/${account}/teamnet/routes?is_deleted=false`),
  ]);
  return { dns, tunnels, apps, posture, routes };
}
export function ids(rows: readonly Resource[]) {
  return rows.map((row) => row.id).sort();
}

export async function restartAppliance(service: 'aura' | 'aura-cloudflared') {
  try {
    await execute(
      'docker',
      ['compose', '--project-directory', required('AURA_E2E_APPLIANCE_DIR'), 'restart', service],
      { timeout: 120_000 },
    );
  } catch {
    throw new Error(`Dedicated appliance ${service} restart failed`);
  }
}

export async function verifyTunnelNetwork() {
  const base = ['compose', '--project-directory', required('AURA_E2E_APPLIANCE_DIR')];
  const { stdout: id } = await execute('docker', [...base, 'ps', '-q', 'aura-cloudflared']);
  if (!id.trim()) throw new Error('Dedicated cloudflared container absent');
  const { stdout: inspected } = await execute('docker', [
    'inspect',
    '--format',
    '{{json .NetworkSettings.Networks}}',
    id.trim(),
  ]);
  const networks = Object.keys(JSON.parse(inspected) as Record<string, unknown>);
  expect(networks).toHaveLength(1);
  const network = networks[0];
  if (!network?.endsWith('aura-tunnel')) throw new Error('Unexpected tunnel network');
  const privateTargets: string[] = [];
  for (const [service, port] of [
    ['postgres', '5432'],
    ['garage', '3900'],
    ['arcadedb-mcp', '8096'],
  ] as const) {
    const { stdout: serviceId } = await execute('docker', [...base, 'ps', '-q', service]);
    if (!serviceId.trim())
      throw new Error(`Private service ${service} absent; cannot prove isolation`);
    const { stdout: addresses } = await execute('docker', [
      'inspect',
      '--format',
      '{{json .NetworkSettings.Networks}}',
      serviceId.trim(),
    ]);
    const attached = JSON.parse(addresses) as Record<string, { IPAddress: string }>;
    expect(Object.keys(attached)).not.toContain(network);
    for (const address of Object.values(attached)) {
      if (!/^\d+\.\d+\.\d+\.\d+$/.test(address.IPAddress))
        throw new Error('Private IPv4 probe target missing');
      privateTargets.push(`${address.IPAddress}:${port}`);
    }
  }
  // This is a disposable diagnostic container on the dedicated appliance, never a production
  // tunnel or private route. Failure to start/pull the probe is a failure, not isolation proof.
  const { stdout } = await execute(
    'docker',
    [
      'run',
      '--rm',
      '--network',
      network,
      'alpine:3.24',
      'sh',
      '-ec',
      'nc -z -w 5 caddy 8080; for pair in "$@"; do host=${pair%:*}; port=${pair#*:}; if nc -z -w 2 "$host" "$port"; then exit 1; fi; done; echo isolated',
      'probe',
      ...privateTargets,
    ],
    { timeout: 120_000 },
  );
  expect(stdout.trim()).toBe('isolated');
}

export async function streamAndControl(page: Page, threadId: string) {
  const result = await page.evaluate(async (threadId) => {
    const response = await fetch('/agent/run', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'Idempotency-Key': crypto.randomUUID() },
      body: JSON.stringify({
        threadId,
        messages: [
          {
            id: crypto.randomUUID(),
            role: 'user',
            content:
              'Write a long detailed numbered explanation of network protocols, at least 100 paragraphs. Do not use tools.',
          },
        ],
      }),
      signal: AbortSignal.timeout(180_000),
    });
    if (
      !response.ok ||
      !response.body ||
      !response.headers.get('content-type')?.includes('text/event-stream')
    )
      throw new Error('SSE response missing');
    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let buffer = '',
      runId = '',
      terminal = false,
      steer = 0,
      cancel = 0;
    const arrivals: number[] = [];
    try {
      while (!terminal) {
        const chunk = await reader.read();
        if (chunk.done) break;
        let contentInChunk = false;
        buffer += decoder.decode(chunk.value, { stream: true }).replace(/\r\n/g, '\n');
        let end: number;
        while ((end = buffer.indexOf('\n\n')) >= 0) {
          const block = buffer.slice(0, end);
          buffer = buffer.slice(end + 2);
          const data = block
            .split('\n')
            .filter((line) => line.startsWith('data:'))
            .map((line) => line.slice(5))
            .join('\n');
          if (!data) continue;
          const frame = JSON.parse(data) as { type: string; runId?: string };
          if (frame.type === 'RUN_STARTED') runId = frame.runId ?? '';
          if (frame.type === 'TEXT_MESSAGE_CONTENT') contentInChunk = true;
          if (frame.type === 'RUN_FINISHED' || frame.type === 'RUN_ERROR') terminal = true;
        }
        if (contentInChunk) arrivals.push(performance.now());
        if (runId && arrivals.length >= 2 && !steer && !terminal) {
          const response = await fetch(`/agent/runs/${encodeURIComponent(runId)}/steer`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json', 'Idempotency-Key': crypto.randomUUID() },
            body: JSON.stringify({ text: 'Focus on HTTPS and keep explaining.' }),
          });
          steer = response.status;
          const stopped = await fetch(`/agent/runs/${encodeURIComponent(runId)}/cancel`, {
            method: 'POST',
            headers: { 'Idempotency-Key': crypto.randomUUID() },
          });
          cancel = stopped.status;
        }
      }
    } finally {
      await reader.cancel();
    }
    return { arrivals, steer, cancel, terminal };
  }, threadId);
  expect(result.arrivals.length).toBeGreaterThanOrEqual(2);
  const first = result.arrivals[0];
  const last = result.arrivals.at(-1);
  if (first === undefined || last === undefined) throw new Error('Incremental SSE missing');
  expect(last - first).toBeGreaterThan(0);
  expect(result.steer).toBe(202);
  expect(result.cancel).toBeGreaterThanOrEqual(200);
  expect(result.cancel).toBeLessThan(300);
  expect(result.terminal).toBe(true);
  return { chunks: result.arrivals.length, spanMs: last - first };
}

export async function uploadAndDownload(page: Page, bytes: string, fileName: string) {
  return page.evaluate(
    async ({ bytes, fileName }) => {
      const body = Uint8Array.from(atob(bytes), (char) => char.charCodeAt(0));
      const response = await fetch('/api/assets/presign', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'Idempotency-Key': crypto.randomUUID() },
        body: JSON.stringify({
          thread_id: '',
          file_name: fileName,
          mime_type: 'image/png',
          size_bytes: body.byteLength,
          modality_hint: 'unknown',
        }),
      });
      if (!response.ok) throw new Error('presign failed');
      const signed = (await response.json()) as {
        asset: { id: string };
        upload: { upload_url: string; required_headers?: Record<string, string> };
      };
      if (new URL(signed.upload.upload_url).origin !== location.origin)
        throw new Error('Garage upload escaped the public hostname');
      const put = await fetch(signed.upload.upload_url, {
        method: 'PUT',
        headers: signed.upload.required_headers ?? {},
        body,
      });
      if (!put.ok) throw new Error('Garage upload failed');
      const finalized = await fetch(`/api/assets/${signed.asset.id}/finalize`, {
        method: 'POST',
        headers: { 'Idempotency-Key': crypto.randomUUID() },
      });
      if (!finalized.ok) throw new Error('Garage finalize failed');
      const download = await fetch(`/api/assets/${signed.asset.id}/download`);
      if (!download.ok || new URL(download.url).origin !== location.origin)
        throw new Error('Garage download escaped the public hostname');
      const actual = new Uint8Array(await download.arrayBuffer());
      if (body.length !== actual.length || !body.every((byte, index) => byte === actual[index]))
        throw new Error('Garage round-trip bytes differ');
      return signed.asset.id;
    },
    { bytes, fileName },
  );
}
