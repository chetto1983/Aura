import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  archiveSkill,
  installMcpServer,
  installSkill,
  removeMcpServer,
  restoreSkill,
  searchSkillCatalog,
  setMcpServerEnabled,
  setMcpServerEnv,
  trustMcpServer,
  type McpWriteResponse,
  type SkillsInstallInfo,
} from '../governanceApi';

// governanceWrite.coverage.test.tsx — the Phase-29-05 coverage backstop for the
// governanceApi WRITE layer (sendJSON + the six MCP + the skills write functions). It
// asserts the throwing-fetch contract (a non-200, INCLUDING 401/404/409, THROWS
// Error("HTTP <n>") so the mutation surfaces a visible error/auth state, never a silent
// no-op), the same-origin + JSON-content-type belt, the URL each write hits (with
// encodeURIComponent on every {name}), and the 204-No-Content coercion (remove/restore/
// archive return without a JSON-parse throw on the empty stream). These paths were the
// uncovered 60% of governanceApi.ts after plan 29-04.

function okJSON(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status });
}

function noContent() {
  // A genuine 204 with a null body — the coercion path must not JSON-parse this.
  return new Response(null, { status: 204 });
}

interface Call {
  readonly url: string;
  readonly init: RequestInit;
}

function captureFetch(response: Response) {
  const calls: Call[] = [];
  const mock = vi.fn((url: string, init: RequestInit) => {
    calls.push({ url, init });
    return Promise.resolve(response);
  });
  vi.stubGlobal('fetch', mock);
  return calls;
}

/** first asserts at least one fetch happened and returns the first call (non-undefined). */
function first(calls: readonly Call[]): Call {
  const c = calls[0];
  if (c === undefined) throw new Error('expected at least one fetch call, got none');
  return c;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

const MCP_RESP: McpWriteResponse = {
  name: 'gh',
  envKeys: [{ key: 'GITHUB_TOKEN', redacted: true }],
  cliEquivalent: 'aura mcp add gh',
  destination: '/home/op/.aura/mcp/servers.json',
  warnings: [],
};

const INSTALL_INFO: SkillsInstallInfo = {
  name: 'demo',
  source: 'owner/repo',
  content_hash: 'sha256:deadbeef',
  preview: '# demo',
  destination: '/skills/pending/demo',
  risk_tier: 'RISKY',
  status: 'pending_approval',
  approval_token: '00000000-0000-0000-0000-0000000000f1',
  checklist: [{ label: 'parsed', passed: true }],
};

describe('governanceApi MCP write layer', () => {
  it('installMcpServer POSTs to /api/governance/mcp with same-origin + JSON body', async () => {
    const calls = captureFetch(okJSON(MCP_RESP));
    const out = await installMcpServer({ name: 'gh', command: 'gh-bin', args: [], env: [] });
    expect(out.name).toBe('gh');
    expect(first(calls).url).toBe('/api/governance/mcp');
    expect(first(calls).init.method).toBe('POST');
    expect(first(calls).init.credentials).toBe('same-origin');
    expect((first(calls).init.headers as Record<string, string>)['Content-Type']).toBe(
      'application/json',
    );
    expect(JSON.parse(first(calls).init.body as string)).toMatchObject({ name: 'gh' });
  });

  it('setMcpServerEnv PATCHes the /env path and encodeURIComponents the name', async () => {
    const calls = captureFetch(okJSON(MCP_RESP));
    await setMcpServerEnv('weird name/v2', ['K=${K}']);
    expect(first(calls).init.method).toBe('PATCH');
    expect(first(calls).url).toBe('/api/governance/mcp/weird%20name%2Fv2/env');
    expect(JSON.parse(first(calls).init.body as string)).toEqual({ env: ['K=${K}'] });
  });

  it('trustMcpServer POSTs the class + reason to /trust', async () => {
    const calls = captureFetch(okJSON(MCP_RESP));
    await trustMcpServer('gh', 'trusted_local', 'vetted');
    expect(first(calls).url).toBe('/api/governance/mcp/gh/trust');
    expect(JSON.parse(first(calls).init.body as string)).toEqual({
      class: 'trusted_local',
      reason: 'vetted',
    });
  });

  it('setMcpServerEnabled switches enable/disable verb', async () => {
    const calls = captureFetch(okJSON(MCP_RESP));
    await setMcpServerEnabled('gh', true);
    expect(first(calls).url).toBe('/api/governance/mcp/gh/enable');
    vi.unstubAllGlobals();
    const calls2 = captureFetch(okJSON(MCP_RESP));
    await setMcpServerEnabled('gh', false);
    expect(first(calls2).url).toBe('/api/governance/mcp/gh/disable');
  });

  it('removeMcpServer DELETEs and coerces a 204 to void without a parse throw', async () => {
    const calls = captureFetch(noContent());
    await expect(removeMcpServer('gh')).resolves.toBeUndefined();
    expect(first(calls).init.method).toBe('DELETE');
    expect(first(calls).url).toBe('/api/governance/mcp/gh');
  });

  it('a non-200 (409 duplicate) throws Error("HTTP 409")', async () => {
    captureFetch(okJSON({ error: 'exists' }, 409));
    await expect(installMcpServer({ name: 'gh', command: 'x' })).rejects.toThrow('HTTP 409');
  });

  it('a 401 unauthenticated throws Error("HTTP 401")', async () => {
    captureFetch(okJSON({ error: 'unauth' }, 401));
    await expect(setMcpServerEnv('gh', [])).rejects.toThrow('HTTP 401');
  });
});

describe('governanceApi skills write layer', () => {
  it('installSkill POSTs the source and returns the RISKY install info', async () => {
    const calls = captureFetch(okJSON(INSTALL_INFO));
    const info = await installSkill('owner/repo');
    expect(info.risk_tier).toBe('RISKY');
    expect(info.checklist).toHaveLength(1);
    expect(first(calls).url).toBe('/api/governance/skills/install');
    expect(JSON.parse(first(calls).init.body as string)).toEqual({ source: 'owner/repo' });
  });

  it('searchSkillCatalog GETs the encoded query and forwards its AbortSignal', async () => {
    const calls = captureFetch(okJSON({ enabled: true, query: 'go test', hits: [] }));
    const controller = new AbortController();
    const res = await searchSkillCatalog('go test', controller.signal);
    expect(res.enabled).toBe(true);
    expect(first(calls).url).toBe('/api/governance/skills/catalog?q=go%20test');
    expect(first(calls).init.signal).toBe(controller.signal);
  });

  it('restoreSkill POSTs /restore and coerces 204', async () => {
    const calls = captureFetch(noContent());
    await expect(restoreSkill('demo')).resolves.toBeUndefined();
    expect(first(calls).url).toBe('/api/governance/skills/demo/restore');
  });

  it('restoreSkill surfaces a 409 colliding restore as Error("HTTP 409")', async () => {
    captureFetch(okJSON({ error: 'active exists' }, 409));
    await expect(restoreSkill('demo')).rejects.toThrow('HTTP 409');
  });

  it('archiveSkill POSTs /archive and coerces 204', async () => {
    const calls = captureFetch(noContent());
    await expect(archiveSkill('demo')).resolves.toBeUndefined();
    expect(first(calls).url).toBe('/api/governance/skills/demo/archive');
  });

  it('an install with an empty source surfaces the backend 400', async () => {
    captureFetch(okJSON({ error: 'empty source' }, 400));
    await expect(installSkill('')).rejects.toThrow('HTTP 400');
  });
});
