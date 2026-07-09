import { afterEach, describe, expect, it, vi } from 'vitest';
import { COMPOSER_SKILLS_PATH, fetchComposerSkills } from './api';

// api.test — the composer skills client is the degrade-to-no-op source (D-09): it resolves
// the {name,description,type} rows on a 200, and resolves [] (never throws) when getJSON
// throws (401/500/503) or the body omits `skills`, so the '/' picker never opens on an
// empty/unreachable list.

function okJSON(body: unknown) {
  return vi.fn(() => Promise.resolve(new Response(JSON.stringify(body), { status: 200 })));
}

function errStatus(code: number) {
  return vi.fn(() => Promise.resolve(new Response('', { status: code })));
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('fetchComposerSkills', () => {
  it('resolves the {name,description,type} rows and hits the same-origin path', async () => {
    const fetchMock = okJSON({
      skills: [{ name: 'skill-creator', description: 'Create a skill', type: 'instruction' }],
    });
    vi.stubGlobal('fetch', fetchMock);

    const rows = await fetchComposerSkills();

    expect(rows).toEqual([
      { name: 'skill-creator', description: 'Create a skill', type: 'instruction' },
    ]);
    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe(COMPOSER_SKILLS_PATH);
    expect(url).toBe('/api/composer/skills');
    expect(init.credentials).toBe('same-origin');
    expect((init.headers as Record<string, string>).Accept).toBe('application/json');
  });

  it('resolves [] when the body omits skills (degrade source, D-09)', async () => {
    vi.stubGlobal('fetch', okJSON({}));
    expect(await fetchComposerSkills()).toEqual([]);
  });

  it('resolves [] when getJSON throws on a 401 (expired session ⇒ menu never opens)', async () => {
    vi.stubGlobal('fetch', errStatus(401));
    expect(await fetchComposerSkills()).toEqual([]);
  });

  it('resolves [] when getJSON throws on a 503 (nil provider / unreachable ⇒ no-op)', async () => {
    vi.stubGlobal('fetch', errStatus(503));
    expect(await fetchComposerSkills()).toEqual([]);
  });
});
