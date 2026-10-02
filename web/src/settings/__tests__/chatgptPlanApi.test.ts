import { afterEach, describe, expect, it, vi } from 'vitest';
import { installMutationIdempotency } from '../../api/idempotency';
import {
  cancelChatGPTPlanLogin,
  disconnectChatGPTPlan,
  fetchChatGPTPlanStatus,
  startChatGPTPlanLogin,
} from '../chatgptPlanApi';

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('ChatGPT plan API mutation contract', () => {
  it.each([
    '/browser/chatgpt-0123456789abcdef01234567?other=flow',
    '/browser/chatgpt-0123456789abcdef01234567\n',
  ])('rejects cancelling an invalid selector before sending a request: %s', async (authURL) => {
    const fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);
    await expect(cancelChatGPTPlanLogin(authURL)).rejects.toThrow('Invalid ChatGPT login route');
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('uses distinct idempotency keys and the exact flow selector for each account mutation', async () => {
    const nativeFetch = vi.fn((_input: RequestInfo | URL, _init?: RequestInit) =>
      Promise.resolve(new Response('{}', { status: 200 })),
    );
    vi.stubGlobal('fetch', nativeFetch);
    installMutationIdempotency();
    vi.stubGlobal('fetch', window.fetch.bind(window));
    const controller = new AbortController();
    await startChatGPTPlanLogin();
    await cancelChatGPTPlanLogin('/browser/chatgpt-0123456789abcdef01234567');
    await disconnectChatGPTPlan(controller.signal);
    await fetchChatGPTPlanStatus(controller.signal);
    const login = nativeFetch.mock.calls[0]?.[1];
    const cancel = nativeFetch.mock.calls[1]?.[1];
    const disconnect = nativeFetch.mock.calls[2]?.[1];
    const status = nativeFetch.mock.calls[3]?.[1];
    if (
      login === undefined ||
      cancel === undefined ||
      disconnect === undefined ||
      status === undefined
    )
      throw new Error('Expected login, disconnect and status requests');
    const loginKey = new Headers(login.headers).get('Idempotency-Key');
    const disconnectKey = new Headers(disconnect.headers).get('Idempotency-Key');
    expect(loginKey).toMatch(/^[\da-f-]{36}$/);
    expect(disconnectKey).toMatch(/^[\da-f-]{36}$/);
    expect(disconnectKey).not.toBe(loginKey);
    expect(new Headers(cancel.headers).get('Idempotency-Key')).toMatch(/^[\da-f-]{36}$/);
    expect(new Headers(cancel.headers).get('Idempotency-Key')).not.toBe(loginKey);
    expect(cancel.method).toBe('DELETE');
    expect(cancel.keepalive).toBe(true);
    expect(JSON.parse(cancel.body as string)).toEqual({
      auth_url: '/browser/chatgpt-0123456789abcdef01234567',
    });
    expect(new Headers(status.headers).has('Idempotency-Key')).toBe(false);
    expect(login.credentials).toBe('same-origin');
    expect(disconnect.credentials).toBe('same-origin');
  });
});
