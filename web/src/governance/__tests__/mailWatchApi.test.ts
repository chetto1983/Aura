import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { installMutationIdempotency } from '../../api/idempotency';
import { HttpError } from '../../api/json';
import { mailWatchServer, watchStatus } from '../../test/mailWatchServer';
import {
  addLabel,
  addRule,
  correctDecision,
  disableMailWatch,
  enableMailWatch,
  getDecisions,
  getMailWatch,
  listLabels,
  listRules,
  removeLabel,
  removeRule,
  saveMailWatchSettings,
  updateLabel,
} from '../mailWatchApi';

// The wire of /api/mail-watch: each call's method, path and body, and that a failure keeps the
// server's code, because the UI translates it. The mutations carry the key the shared wrapper
// attaches, and that wrapper is what is installed here, as main.tsx installs it.

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('routes', () => {
  let server: ReturnType<typeof mailWatchServer>;
  beforeEach(() => {
    server = mailWatchServer();
    for (const route of [
      'PUT /api/mail-watch/settings',
      'POST /api/mail-watch/accounts/a%2Fb/enable',
      'POST /api/mail-watch/accounts/a%2Fb/disable',
      'POST /api/mail-watch/accounts/work/decisions/correct',
      'GET /api/mail-watch/accounts/a%2Fb/decisions',
      'POST /api/mail-watch/rules',
      'DELETE /api/mail-watch/rules/r%2F1',
      'POST /api/mail-watch/labels',
      'PUT /api/mail-watch/labels/l%2F1',
      'DELETE /api/mail-watch/labels/l%2F1',
    ]) {
      server.on(route, () => ({ body: {} }));
    }
  });

  it('reads the status, the rules and the labels', async () => {
    await expect(getMailWatch()).resolves.toEqual(watchStatus());
    await expect(listRules()).resolves.toEqual([]);
    await expect(listLabels()).resolves.toEqual([]);
    expect(server.calls.map((call) => `${call.method} ${call.path}`)).toEqual([
      'GET /api/mail-watch',
      'GET /api/mail-watch/rules',
      'GET /api/mail-watch/labels',
    ]);
  });

  it('asks for today by leaving the day out, because only the server knows the zone', async () => {
    await getDecisions('a/b', null);
    expect(server.calls.at(-1)?.query).toBe('');
  });

  it('asks for a day by name', async () => {
    await getDecisions('a/b', '2026-10-28');
    expect(server.calls.at(-1)?.query).toBe('?day=2026-10-28');
  });

  it('turns a watch on and off, escaping the account', async () => {
    await enableMailWatch('a/b');
    await disableMailWatch('a/b');
    expect(server.writes('POST', '/api/mail-watch/accounts/a%2Fb/enable')).toHaveLength(1);
    expect(server.writes('POST', '/api/mail-watch/accounts/a%2Fb/disable')).toHaveLength(1);
  });

  it('sends each body as the API section names it', async () => {
    await saveMailWatchSettings({ digest_hour: 7, daily_alert_cap: 3 });
    await correctDecision('work', { resource: 'mail', item_id: 'm1', tier: 'now' });
    await addRule({ key: 'x.org', tier: 'later' });
    await addLabel({ name: 'Client', tier: '', description: '', discard: false });
    await updateLabel('l/1', {
      name: 'Clients',
      tier: 'now',
      description: '',
      discard: false,
    });
    expect(server.writes('PUT', '/api/mail-watch/settings')[0]?.body).toEqual({
      digest_hour: 7,
      daily_alert_cap: 3,
    });
    expect(
      server.writes('POST', '/api/mail-watch/accounts/work/decisions/correct')[0]?.body,
    ).toEqual({ resource: 'mail', item_id: 'm1', tier: 'now' });
    expect(server.writes('POST', '/api/mail-watch/rules')[0]?.body).toEqual({
      key: 'x.org',
      tier: 'later',
    });
    expect(server.writes('POST', '/api/mail-watch/labels')[0]?.body).toEqual({
      name: 'Client',
      tier: '',
      description: '',
      discard: false,
    });
    expect(server.writes('PUT', '/api/mail-watch/labels/l%2F1')[0]?.body).toEqual({
      name: 'Clients',
      tier: 'now',
      description: '',
      discard: false,
    });
  });

  it('removes a rule and a label by escaped id', async () => {
    await removeRule('r/1');
    await removeLabel('l/1');
    expect(server.writes('DELETE', '/api/mail-watch/rules/r%2F1')).toHaveLength(1);
    expect(server.writes('DELETE', '/api/mail-watch/labels/l%2F1')).toHaveLength(1);
  });

  it('keeps the fixed code of a failed read, which getJSON would drop', async () => {
    server.on('GET /api/mail-watch', () => ({
      status: 502,
      body: { error: 'provider_auth' },
    }));
    const failure = await getMailWatch().catch((error: unknown) => error);
    expect(failure).toBeInstanceOf(HttpError);
    expect(failure).toMatchObject({ status: 502, reason: 'provider_auth' });
  });

  it('keeps the sentence of a refused write', async () => {
    server.on('POST /api/mail-watch/rules', () => ({
      status: 400,
      body: { error: 'a rule gives a tier or a label' },
    }));
    await expect(addRule({ key: 'x.org' })).rejects.toMatchObject({
      status: 400,
      reason: 'a rule gives a tier or a label',
    });
  });
});

describe('Idempotency-Key', () => {
  const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

  it('rides on every mutation and on no read', async () => {
    const seen: { readonly method: string; readonly key: string | null }[] = [];
    const nativeFetch = window.fetch;
    window.fetch = vi.fn((_input: RequestInfo | URL, init?: RequestInit) => {
      seen.push({
        method: init?.method ?? 'GET',
        key: new Headers(init?.headers).get('Idempotency-Key'),
      });
      return Promise.resolve(new Response('{}', { status: 200 }));
    });
    installMutationIdempotency();

    await getMailWatch();
    await listRules();
    await listLabels();
    await getDecisions('work', null);
    const reads = seen.splice(0);
    await saveMailWatchSettings({ digest_hour: 7, daily_alert_cap: 3 });
    await enableMailWatch('work');
    await disableMailWatch('work');
    await correctDecision('work', { resource: 'mail', item_id: 'm1', tier: 'now' });
    await addRule({ key: 'x.org', tier: 'later' });
    await removeRule('r1');
    await addLabel({ name: 'Client', tier: '', description: '', discard: false });
    await updateLabel('l1', { name: 'Clients', tier: '', description: '', discard: false });
    await removeLabel('l1');
    window.fetch = nativeFetch;

    expect(reads.every((read) => read.key === null)).toBe(true);
    expect(seen).toHaveLength(9);
    for (const write of seen) expect(write.key).toMatch(UUID);
    expect(new Set(seen.map((write) => write.key)).size).toBe(9);
  });
});
