import { afterEach, describe, expect, it, vi } from 'vitest';
import { HttpError } from '../../api/json';
import {
  connectorTarget,
  fetchPlugins,
  installPlugin,
  removePlugin,
  reviewPlugin,
  type PluginConnector,
} from '../pluginApi';

// pluginApi: the URL and method each call sends, and the decoding of what Go writes — an empty
// slice arrives as null, and the names arrive in snake_case.

function respond(body: unknown, status = 200) {
  const fetchMock = vi.fn((_url: string, _init?: RequestInit) =>
    Promise.resolve(new Response(JSON.stringify(body), { status })),
  );
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}

function sent(fetchMock: ReturnType<typeof respond>) {
  const [url, init] = fetchMock.mock.calls[0] ?? [];
  return { url, method: init?.method ?? 'GET', body: init?.body };
}

const GO_REVIEW = {
  ref: 'o/r/p',
  commit: 'c'.repeat(40),
  digest: 'd'.repeat(64),
  name: 'p',
  version: '',
  author: '',
  format: 'claude',
  skills: null,
  skipped_skills: [{ name: 's', reason: 'why' }],
  connectors: [
    {
      name: 'x',
      transport: 'stdio',
      placement: 'box',
      trust: 'untrusted',
      oauth: false,
      command: 'uvx',
      args: ['--from', 'a b'],
    },
    {
      name: 'y',
      transport: 'streamable_http',
      placement: 'remote',
      trust: 'untrusted',
      oauth: true,
      url: 'https://r.example/mcp',
    },
  ],
  skipped_connectors: null,
  notes: null,
  mcp_disabled: '',
};

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('pluginApi', () => {
  it('lists the installed plugins, a null slice read as empty', async () => {
    const fetchMock = respond({
      plugins: [
        {
          name: 'p',
          version: '1',
          commit: 'c',
          skills: null,
          connectors: null,
          phase: 'installed',
        },
      ],
    });

    await expect(fetchPlugins()).resolves.toEqual([
      { name: 'p', version: '1', commit: 'c', skills: [], connectors: [], phase: 'installed' },
    ]);
    expect(sent(fetchMock)).toMatchObject({ url: '/api/governance/plugins', method: 'GET' });
  });

  it('reviews a ref and decodes the review', async () => {
    const fetchMock = respond(GO_REVIEW);

    const review = await reviewPlugin('o/r/p');
    expect(sent(fetchMock)).toEqual({
      url: '/api/governance/plugins/review',
      method: 'POST',
      body: JSON.stringify({ ref: 'o/r/p' }),
    });
    expect(review).toMatchObject({
      skills: [],
      skippedSkills: [{ name: 's', reason: 'why' }],
      skippedConnectors: [],
      notes: [],
      mcpDisabled: '',
    });
    expect(review.connectors[0]).toMatchObject({
      placement: 'box',
      command: 'uvx',
      args: ['--from', 'a b'],
      url: '',
    });
    expect(review.connectors[1]).toMatchObject({
      command: '',
      args: [],
      url: 'https://r.example/mcp',
    });
  });

  it('installs the approved triple and decodes the report', async () => {
    const fetchMock = respond({
      review: GO_REVIEW,
      skills: [{ name: 's', outcome: 'ok' }],
      connectors: null,
    });

    const report = await installPlugin({ ref: 'o/r/p', commit: 'c'.repeat(40), digest: 'd' });
    expect(sent(fetchMock)).toEqual({
      url: '/api/governance/plugins',
      method: 'POST',
      body: JSON.stringify({ ref: 'o/r/p', commit: 'c'.repeat(40), digest: 'd' }),
    });
    expect(report.skills).toEqual([{ name: 's', outcome: 'ok', reason: '' }]);
    expect(report.connectors).toEqual([]);
  });

  it('asks to delete the data only when told to', async () => {
    let fetchMock = respond({ name: 'a b', skills: null, connectors: null, data_deleted: false });
    await expect(removePlugin('a b', false)).resolves.toEqual({
      name: 'a b',
      skills: [],
      connectors: [],
      dataDeleted: false,
      error: '',
    });
    expect(sent(fetchMock)).toMatchObject({
      url: '/api/governance/plugins/a%20b',
      method: 'DELETE',
    });

    fetchMock = respond({ name: 'p', skills: [], connectors: [], data_deleted: true });
    await expect(removePlugin('p', true)).resolves.toMatchObject({ dataDeleted: true });
    expect(sent(fetchMock).url).toBe('/api/governance/plugins/p?data=delete');
  });

  it('reads a malformed payload as empty rather than throwing', async () => {
    respond({ plugins: [null, 'p', { name: 'kept', skills: 'x', connectors: [1, 'c'] }] });
    await expect(fetchPlugins()).resolves.toEqual([
      { name: 'kept', version: '', commit: '', skills: [], connectors: ['c'], phase: '' },
    ]);

    respond({ review: null, skills: {}, connectors: [null] });
    const report = await installPlugin({ ref: 'o/r/p', commit: 'c', digest: 'd' });
    expect(report.review).toMatchObject({ ref: '', skills: [], notes: [] });
    expect(report.connectors).toEqual([]);
  });

  it('decodes a write that stopped part way: what it did, and why', async () => {
    respond(
      {
        error: 'database is unreachable',
        report: { review: GO_REVIEW, skills: [{ name: 's', outcome: 'ok' }], connectors: null },
      },
      207,
    );
    const report = await installPlugin({ ref: 'o/r/p', commit: 'c'.repeat(40), digest: 'd' });
    expect(report).toMatchObject({
      error: 'database is unreachable',
      skills: [{ name: 's', outcome: 'ok', reason: '' }],
      connectors: [],
    });
    expect(report.review.name).toBe('p');

    respond(
      {
        error: 'database is unreachable',
        report: {
          name: 'p',
          skills: null,
          connectors: [{ name: 'c', outcome: 'ok' }],
          data_deleted: false,
        },
      },
      207,
    );
    expect(await removePlugin('p', false)).toMatchObject({
      name: 'p',
      error: 'database is unreachable',
      connectors: [{ name: 'c', outcome: 'ok', reason: '' }],
    });
  });

  it('carries the server reason on a refusal', async () => {
    respond({ error: 'plugin already installed: p' }, 409);

    const err: unknown = await installPlugin({ ref: 'o/r/p', commit: 'c', digest: 'd' }).catch(
      (e: unknown) => e,
    );
    expect(err).toBeInstanceOf(HttpError);
    expect((err as HttpError).status).toBe(409);
    expect((err as HttpError).reason).toBe('plugin already installed: p');
  });
});

describe('connectorTarget', () => {
  const box: PluginConnector = {
    name: 'x',
    transport: 'stdio',
    placement: 'box',
    trust: 'untrusted',
    oauth: false,
    command: '/plugins/p/bin/srv',
    args: ['--name', 'a b', '', 'say "hi"'],
    url: '',
  };

  it('shows every argument boundary of a box command', () => {
    expect(connectorTarget(box)).toBe('/plugins/p/bin/srv --name "a b" "" "say \\"hi\\""');
  });

  it('shows the URL of a remote connector', () => {
    expect(connectorTarget({ ...box, command: '', args: [], url: 'https://r.example/mcp' })).toBe(
      'https://r.example/mcp',
    );
  });
});
