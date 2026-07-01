import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  fetchMcpServers,
  fetchSchedulerRuns,
  fetchSchedulerTasks,
  fetchSkills,
  fetchSkillsAudit,
  probeMcpServer,
  whatsappConnectLogout,
  whatsappConnectStatus,
} from '../governanceApi';
import {
  createPimAccount,
  deletePimAccount,
  listPimAccounts,
  pimAuthCancel,
  pimAuthStatus,
  pimDeviceStart,
  pimGoogleStart,
  pimLogout,
} from '../pimApi';

// governanceApi test — the data layer over the six Plan-02 reads. It asserts the same-origin
// throwing-fetch contract (a non-200, INCLUDING 401, THROWS Error("HTTP <n>") so the boards route
// the auth/error state), the credentials: 'same-origin' belt, the URL each fetcher hits, and the
// safe-empty unwrapping ({servers}/{skills}/{rows}/{tasks}/{runs} → [] when absent).

function okJSON(body: unknown) {
  return vi.fn(() => Promise.resolve(new Response(JSON.stringify(body), { status: 200 })));
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('governanceApi same-origin throwing fetch', () => {
  it('fetchMcpServers unwraps {servers} and sends Accept + same-origin', async () => {
    const fetchMock = okJSON({
      servers: [
        {
          name: 'github',
          source: 'recipe:github',
          trust: 'trusted',
          riskPolicy: 'trusted_recipe',
          runtime: 'stdio',
          startupState: 'configured',
          authStatus: 'ok',
          profiles: ['work'],
          networkAllowlist: ['api.github.com'],
          envKeys: [{ key: 'GITHUB_TOKEN', redacted: true }],
        },
      ],
    });
    vi.stubGlobal('fetch', fetchMock);

    const rows = await fetchMcpServers();
    expect(rows).toEqual([
      {
        name: 'github',
        source: 'recipe:github',
        trust: 'trusted',
        riskPolicy: 'trusted_recipe',
        runtime: 'stdio',
        startupState: 'configured',
        authStatus: 'ok',
        profiles: ['work'],
        networkAllowlist: ['api.github.com'],
        envKeys: [{ key: 'GITHUB_TOKEN', redacted: true }],
      },
    ]);

    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe('/api/governance/mcp');
    expect(init.credentials).toBe('same-origin');
    expect((init.headers as Record<string, string>).Accept).toBe('application/json');
  });

  it('fetchMcpServers returns [] when the body omits servers', async () => {
    vi.stubGlobal('fetch', okJSON({}));
    expect(await fetchMcpServers()).toEqual([]);
  });

  it('fetchMcpServers normalizes null or missing arrays before UI render', async () => {
    vi.stubGlobal(
      'fetch',
      okJSON({
        servers: [
          {
            name: 'calendar',
            trust: 'trusted_recipe',
            runtime: 'remote_http',
            startupState: 'unknown',
            authStatus: 'unsupported',
            profiles: null,
            networkAllowlist: null,
            envKeys: null,
          },
          {
            name: 'whatsapp',
            trust: 'trusted_recipe',
            runtime: 'remote_http',
            startupState: 'unknown',
            authStatus: 'unsupported',
          },
        ],
      }),
    );

    expect(await fetchMcpServers()).toEqual([
      {
        name: 'calendar',
        source: '',
        trust: 'trusted_recipe',
        riskPolicy: '',
        runtime: 'remote_http',
        startupState: 'unknown',
        authStatus: 'unsupported',
        profiles: [],
        networkAllowlist: [],
        envKeys: [],
      },
      {
        name: 'whatsapp',
        source: '',
        trust: 'trusted_recipe',
        riskPolicy: '',
        runtime: 'remote_http',
        startupState: 'unknown',
        authStatus: 'unsupported',
        profiles: [],
        networkAllowlist: [],
        envKeys: [],
      },
    ]);
  });

  it('probeMcpServer encodes the name and hits the probe path', async () => {
    const fetchMock = okJSON({ name: 'a b', ok: true, tool_count: 1, detail: 'ok' });
    vi.stubGlobal('fetch', fetchMock);

    const res = await probeMcpServer('a b');
    expect(res.ok).toBe(true);
    const [url] = fetchMock.mock.calls[0] as unknown as [string];
    expect(url).toBe('/api/governance/mcp/a%20b/probe');
  });

  it('fetchSkills passes the stage and unwraps {skills}', async () => {
    const fetchMock = okJSON({ skills: [{ name: 's', description: '', type: 'instruction' }] });
    vi.stubGlobal('fetch', fetchMock);

    const rows = await fetchSkills('pending');
    expect(rows).toHaveLength(1);
    const [url] = fetchMock.mock.calls[0] as unknown as [string];
    expect(url).toBe('/api/governance/skills?stage=pending');
  });

  it('fetchSkillsAudit unwraps {rows}', async () => {
    vi.stubGlobal('fetch', okJSON({ rows: [{ ID: 'a' }] }));
    const rows = await fetchSkillsAudit();
    expect(rows).toHaveLength(1);
  });

  it('fetchSchedulerTasks unwraps {tasks}', async () => {
    vi.stubGlobal('fetch', okJSON({ tasks: [{ ID: 't' }] }));
    expect(await fetchSchedulerTasks()).toHaveLength(1);
  });

  it('fetchSchedulerRuns builds the limit/offset query and unwraps {runs}', async () => {
    const fetchMock = okJSON({ runs: [{ ID: 'r' }] });
    vi.stubGlobal('fetch', fetchMock);

    const rows = await fetchSchedulerRuns('task-1', 25, 50);
    expect(rows).toHaveLength(1);
    const [url] = fetchMock.mock.calls[0] as unknown as [string];
    expect(url).toBe('/api/governance/scheduler/task-1/runs?limit=25&offset=50');
  });

  it('returns [] when skills/audit/tasks/runs bodies omit their arrays', async () => {
    vi.stubGlobal('fetch', okJSON({}));
    expect(await fetchSkills('active')).toEqual([]);
    expect(await fetchSkillsAudit()).toEqual([]);
    expect(await fetchSchedulerTasks()).toEqual([]);
    expect(await fetchSchedulerRuns('t', 25, 0)).toEqual([]);
  });

  it('throws Error("HTTP 401") on a 401 so the board routes the auth state', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response('', { status: 401 }))),
    );
    await expect(fetchMcpServers()).rejects.toThrow('HTTP 401');
  });

  it('throws Error("HTTP 503") on a 503 (service unavailable)', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response('', { status: 503 }))),
    );
    await expect(probeMcpServer('x')).rejects.toThrow('HTTP 503');
  });

  it('throws Error("HTTP 404") on a probe for an unknown server', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response('', { status: 404 }))),
    );
    await expect(probeMcpServer('ghost')).rejects.toThrow('HTTP 404');
  });

  it('whatsappConnectStatus normalizes the bridge status passthrough', async () => {
    const fetchMock = okJSON({
      state: 'connected',
      paired: true,
      jid: '123@s.whatsapp.net',
      connected: true,
      qr_available: false,
    });
    vi.stubGlobal('fetch', fetchMock);

    const status = await whatsappConnectStatus();
    expect(status).toEqual({
      state: 'connected',
      paired: true,
      jid: '123@s.whatsapp.net',
      connected: true,
      qrAvailable: false,
    });
    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe('/api/connect/whatsapp/status');
    expect(init.credentials).toBe('same-origin');
  });

  it('whatsappConnectStatus coerces a partial/odd body to safe defaults', async () => {
    vi.stubGlobal('fetch', okJSON({ state: 'waiting_qr' }));
    expect(await whatsappConnectStatus()).toEqual({
      state: 'waiting_qr',
      paired: false,
      jid: '',
      connected: false,
      qrAvailable: false,
    });
  });

  it('whatsappConnectStatus throws Error("HTTP 503") when the bridge is unconfigured', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response('', { status: 503 }))),
    );
    await expect(whatsappConnectStatus()).rejects.toThrow('HTTP 503');
  });

  it('whatsappConnectLogout POSTs the logout route', async () => {
    const fetchMock = vi.fn(() => Promise.resolve(new Response(null, { status: 204 })));
    vi.stubGlobal('fetch', fetchMock);

    await whatsappConnectLogout();
    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe('/api/connect/whatsapp/logout');
    expect(init.method).toBe('POST');
    expect(init.credentials).toBe('same-origin');
  });

  it('listPimAccounts unwraps {accounts} and drops malformed entries', async () => {
    vi.stubGlobal(
      'fetch',
      okJSON({
        accounts: [
          { id: 'work', displayName: 'Work', provider: 'google', enabled: true },
          { displayName: 'no-id' }, // dropped — missing id
          'garbage', // dropped — not an object
        ],
      }),
    );
    const { accounts } = await listPimAccounts();
    expect(accounts).toEqual([
      { id: 'work', displayName: 'Work', provider: 'google', enabled: true },
    ]);
  });

  it('listPimAccounts returns [] when the body omits accounts and throws on 503', async () => {
    vi.stubGlobal('fetch', okJSON({}));
    expect((await listPimAccounts()).accounts).toEqual([]);
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response('', { status: 503 }))),
    );
    await expect(listPimAccounts()).rejects.toThrow('HTTP 503');
  });

  it('createPimAccount POSTs the body and throws Error("HTTP 409") on a duplicate', async () => {
    const fetchMock = okJSON({
      id: 'work',
      displayName: 'Work',
      provider: 'google',
      enabled: true,
    });
    vi.stubGlobal('fetch', fetchMock);
    const body = {
      id: 'work',
      displayName: 'Work',
      provider: 'google' as const,
      providerConfig: { clientId: 'cid', clientSecret: 'sec' },
    };
    await createPimAccount(body);
    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe('/api/connect/pim/accounts');
    expect(init.method).toBe('POST');
    expect(init.body).toBe(JSON.stringify(body));

    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response('', { status: 409 }))),
    );
    await expect(createPimAccount(body)).rejects.toThrow('HTTP 409');
  });

  it('deletePimAccount DELETEs the encoded id route (204)', async () => {
    const fetchMock = vi.fn(() => Promise.resolve(new Response(null, { status: 204 })));
    vi.stubGlobal('fetch', fetchMock);
    await deletePimAccount('a/b');
    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe('/api/connect/pim/accounts/a%2Fb');
    expect(init.method).toBe('DELETE');
  });

  it('pimGoogleStart GETs the start route and returns {authUrl,redirectUri}', async () => {
    const fetchMock = okJSON({
      authUrl: 'https://accounts.google.com/x',
      redirectUri: 'http://localhost/cb',
    });
    vi.stubGlobal('fetch', fetchMock);
    const start = await pimGoogleStart('work');
    expect(start).toEqual({
      authUrl: 'https://accounts.google.com/x',
      redirectUri: 'http://localhost/cb',
    });
    const [url] = fetchMock.mock.calls[0] as unknown as [string];
    expect(url).toBe('/api/connect/pim/accounts/work/google/start');
  });

  it('pimLogout POSTs the logout route', async () => {
    const fetchMock = okJSON({ ok: true });
    vi.stubGlobal('fetch', fetchMock);
    await pimLogout('work');
    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe('/api/connect/pim/accounts/work/logout');
    expect(init.method).toBe('POST');
  });

  it('pimDeviceStart POSTs auth/start and parses the device-code grant', async () => {
    const fetchMock = okJSON({
      accountId: 'ms',
      userCode: 'ABCD-EFGH',
      verificationUrl: 'https://microsoft.com/devicelogin',
      message: 'enter the code',
      expiresIn: 900,
    });
    vi.stubGlobal('fetch', fetchMock);
    const start = await pimDeviceStart('ms');
    expect(start).toEqual({
      userCode: 'ABCD-EFGH',
      verificationUrl: 'https://microsoft.com/devicelogin',
      message: 'enter the code',
      expiresIn: 900,
    });
    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe('/api/connect/pim/accounts/ms/auth/start');
    expect(init.method).toBe('POST');
  });

  it('pimDeviceStart coerces a missing/non-numeric expiresIn to 0', async () => {
    vi.stubGlobal('fetch', okJSON({ userCode: 'X', verificationUrl: 'u', message: 'm' }));
    expect((await pimDeviceStart('ms')).expiresIn).toBe(0);
  });

  it('pimAuthStatus GETs auth/status and keeps optional code/url when present', async () => {
    const fetchMock = okJSON({
      status: 'awaiting_user',
      message: 'enter the code',
      userCode: 'ABCD-EFGH',
      verificationUrl: 'https://microsoft.com/devicelogin',
    });
    vi.stubGlobal('fetch', fetchMock);
    const status = await pimAuthStatus('ms');
    expect(status).toEqual({
      status: 'awaiting_user',
      message: 'enter the code',
      userCode: 'ABCD-EFGH',
      verificationUrl: 'https://microsoft.com/devicelogin',
    });
    const [url] = fetchMock.mock.calls[0] as unknown as [string];
    expect(url).toBe('/api/connect/pim/accounts/ms/auth/status');
  });

  it('pimAuthStatus drops absent optional code/url to undefined', async () => {
    vi.stubGlobal('fetch', okJSON({ status: 'completed', message: 'ok' }));
    const status = await pimAuthStatus('ms');
    expect(status.userCode).toBeUndefined();
    expect(status.verificationUrl).toBeUndefined();
  });

  it('pimAuthCancel POSTs the auth/cancel route', async () => {
    const fetchMock = okJSON({ message: 'cancelled' });
    vi.stubGlobal('fetch', fetchMock);
    await pimAuthCancel('ms');
    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe('/api/connect/pim/accounts/ms/auth/cancel');
    expect(init.method).toBe('POST');
  });
});
