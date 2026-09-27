import { afterEach, describe, expect, it, vi } from 'vitest';
import { fetchOpenElicitations, postElicitationAnswer } from '../elicitationApi';

function respond(response: Response) {
  const fetchStub = vi.fn((_input: RequestInfo | URL, _init?: RequestInit) =>
    Promise.resolve(response),
  );
  vi.stubGlobal('fetch', fetchStub);
  return fetchStub;
}

const OPEN = {
  run_id: 'run-1',
  id: 'q-1',
  server: 'forms',
  message: 'm',
  fields: [{ name: 'name', kind: 'string', required: true }],
  deadline: '2026-09-25T10:05:00Z',
};

describe('postElicitationAnswer', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('posts the answer once, owner-cookied and keyed', async () => {
    const fetchStub = respond(new Response('{"status":"delivered"}', { status: 202 }));
    const body = { action: 'accept', content: { name: 'Ada' } } as const;
    await expect(postElicitationAnswer('run-1', 'q/1', body, 'key-1')).resolves.toEqual({
      kind: 'delivered',
    });
    const [url, init] = fetchStub.mock.calls[0] ?? [];
    expect(url).toBe('/agent/runs/run-1/elicitations/q%2F1');
    expect(init?.method).toBe('POST');
    expect(init?.credentials).toBe('same-origin');
    expect(new Headers(init?.headers).get('Idempotency-Key')).toBe('key-1');
    expect(JSON.parse(init?.body as string)).toEqual(body);
  });

  it('classifies every refusal the route has, keeping only string codes', async () => {
    const codes = { email: 'format', '': 'not_asked', bad: 3 };
    respond(new Response(JSON.stringify({ errors: codes }), { status: 422 }));
    await expect(postElicitationAnswer('r', 'q', { action: 'accept' }, 'k')).resolves.toEqual({
      kind: 'invalid',
      errors: { email: 'format', '': 'not_asked' },
    });
    respond(new Response('{"error":"question already resolved"}', { status: 409 }));
    await expect(postElicitationAnswer('r', 'q', { action: 'decline' }, 'k')).resolves.toEqual({
      kind: 'closed',
    });
    respond(new Response('run has ended', { status: 410 }));
    await expect(postElicitationAnswer('r', 'q', { action: 'decline' }, 'k')).resolves.toEqual({
      kind: 'gone',
    });
    respond(new Response('question not found', { status: 404 }));
    await expect(postElicitationAnswer('r', 'q', { action: 'decline' }, 'k')).rejects.toThrow(
      'question not found',
    );
  });

  it('reads a 422 with no usable body as no field errors', async () => {
    respond(new Response('<html>', { status: 422 }));
    await expect(postElicitationAnswer('r', 'q', { action: 'accept' }, 'k')).resolves.toEqual({
      kind: 'invalid',
      errors: {},
    });
  });
});

describe('fetchOpenElicitations', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("lists the run's open forms and drops an entry it cannot trust", async () => {
    const body = { questions: [OPEN, { ...OPEN, id: 9 }] };
    const fetchStub = respond(new Response(JSON.stringify(body), { status: 200 }));
    await expect(fetchOpenElicitations('run-1')).resolves.toEqual([OPEN]);
    const [url, init] = fetchStub.mock.calls[0] ?? [];
    expect(url).toBe('/agent/runs/run-1/elicitations');
    expect(init?.credentials).toBe('same-origin');
  });

  it('reads a body with no list as no forms, and a refusal as an error', async () => {
    respond(new Response('{}', { status: 200 }));
    await expect(fetchOpenElicitations('run-1')).resolves.toEqual([]);
    respond(new Response('run not found', { status: 404 }));
    await expect(fetchOpenElicitations('run-1')).rejects.toThrow('run not found');
  });
});
