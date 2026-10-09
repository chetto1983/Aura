import { afterEach, describe, expect, it, vi } from 'vitest';
import { createBoardProvider, fetchBoard, withDates, type BoardCard } from './boardApi';

// The provider is the one place this board departs from the widget's own code: the status
// check RestDataProvider leaves out, and the reload a duplicate needs. Both are pinned here.

interface ProviderUnderTest {
  send(url: string, method: string, data?: unknown): Promise<unknown>;
}

function respond(status: number, body: unknown) {
  const spy = vi.fn((_input: RequestInfo | URL, _init?: RequestInit) =>
    Promise.resolve(
      new Response(typeof body === 'string' ? body : JSON.stringify(body), { status }),
    ),
  );
  vi.stubGlobal('fetch', spy);
  return spy;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('the board provider', () => {
  it('answers a stored write with the server body, labelled as JSON', async () => {
    const fetchSpy = respond(200, { id: 'c-1', label: 'renew' });
    const resync = vi.fn();
    const provider = createBoardProvider(resync) as unknown as ProviderUnderTest;
    await expect(provider.send('cards', 'POST', { label: 'renew' })).resolves.toEqual({
      id: 'c-1',
      label: 'renew',
    });
    const [url, init] = fetchSpy.mock.calls[0] ?? [];
    expect(url).toBe('/api/board/cards');
    expect(init?.method).toBe('POST');
    expect(new Headers(init?.headers).get('Content-Type')).toBe('application/json');
    expect(resync).not.toHaveBeenCalled();
  });

  it('turns a refused write into a reload with the server code, and never hangs the queue', async () => {
    respond(400, { error: 'invalid_column', message: 'column "later" is not on the board' });
    const resync = vi.fn();
    const provider = createBoardProvider(resync) as unknown as ProviderUnderTest;
    await expect(provider.send('cards/c-1/move', 'PUT', { column: 'later' })).resolves.toEqual({});
    expect(resync).toHaveBeenCalledWith('invalid_column');
  });

  it('names a failure without a code generic', async () => {
    respond(502, '<html>bad gateway</html>');
    const resync = vi.fn();
    const provider = createBoardProvider(resync) as unknown as ProviderUnderTest;
    await expect(provider.send('cards/c-1', 'DELETE')).resolves.toEqual({});
    expect(resync).toHaveBeenCalledWith('generic');
  });

  it('reloads after a duplicate, whose copy the widget cannot learn the id of', async () => {
    respond(200, { id: 'c-copy' });
    const resync = vi.fn();
    const provider = createBoardProvider(resync) as unknown as ProviderUnderTest;
    await provider.send('cards/c-1/duplicate', 'POST');
    expect(resync).toHaveBeenCalledWith();
  });
});

describe('board reads', () => {
  it('loads the columns and the cards in one answer', async () => {
    const spy = vi.fn((input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
      const body = url.endsWith('/cards')
        ? [{ id: 'c-1' }]
        : { name: 'main', columns: [{ id: 'todo', label: 'To do' }] };
      return Promise.resolve(new Response(JSON.stringify(body), { status: 200 }));
    });
    vi.stubGlobal('fetch', spy);
    await expect(fetchBoard()).resolves.toEqual({
      name: 'main',
      columns: [{ id: 'todo', label: 'To do' }],
      cards: [{ id: 'c-1' }],
    });
  });

  it('gives the widget a Date where the wire has a string', () => {
    const wire = { id: 'c', deadline: '2026-10-12T08:00:00Z' } as unknown as BoardCard;
    const [withDeadline, without] = withDates([wire, { id: 'd' } as unknown as BoardCard]);
    expect(withDeadline?.deadline).toEqual(new Date('2026-10-12T08:00:00Z'));
    expect(without !== undefined && 'deadline' in without).toBe(false);
  });
});
