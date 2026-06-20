import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  fetchMcpServers,
  fetchSchedulerRuns,
  fetchSchedulerTasks,
  fetchSkills,
  fetchSkillsAudit,
  probeMcpServer,
} from '../governanceApi';

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
    const fetchMock = okJSON({ servers: [{ name: 'github' }] });
    vi.stubGlobal('fetch', fetchMock);

    const rows = await fetchMcpServers();
    expect(rows).toEqual([{ name: 'github' }]);

    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe('/api/governance/mcp');
    expect(init.credentials).toBe('same-origin');
    expect((init.headers as Record<string, string>).Accept).toBe('application/json');
  });

  it('fetchMcpServers returns [] when the body omits servers', async () => {
    vi.stubGlobal('fetch', okJSON({}));
    expect(await fetchMcpServers()).toEqual([]);
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
});
