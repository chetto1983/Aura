import { afterEach, describe, expect, it, vi } from 'vitest';
import { installMutationIdempotency } from './idempotency';

// bind: capturing the bare method trips typescript-eslint's unbound-method; the
// bound copy restores identical behaviour (fetch is called through window anyway).
const originalFetch = window.fetch.bind(window);

afterEach(() => {
  window.fetch = originalFetch;
  vi.restoreAllMocks();
});

describe('installMutationIdempotency', () => {
  it('adds one stable key to a same-origin mutation request', async () => {
    const nativeFetch = vi.fn().mockResolvedValue(new Response(null, { status: 204 }));
    window.fetch = nativeFetch;
    vi.spyOn(crypto, 'randomUUID').mockReturnValue('11111111-1111-4111-8111-111111111111');
    installMutationIdempotency();

    await window.fetch('/api/conversations', { method: 'POST', body: '{}' });

    const init = nativeFetch.mock.calls[0]?.[1] as RequestInit;
    expect(new Headers(init.headers).get('Idempotency-Key')).toBe(
      '11111111-1111-4111-8111-111111111111',
    );
  });

  it('preserves an explicit logical-operation key', async () => {
    const nativeFetch = vi.fn().mockResolvedValue(new Response(null, { status: 204 }));
    window.fetch = nativeFetch;
    installMutationIdempotency();

    await window.fetch('/api/conversations', {
      method: 'DELETE',
      headers: { 'Idempotency-Key': 'owned-retry-key' },
    });

    const init = nativeFetch.mock.calls[0]?.[1] as RequestInit;
    expect(new Headers(init.headers).get('Idempotency-Key')).toBe('owned-retry-key');
  });

  it('does not attach Aura keys to external object-store uploads', async () => {
    const nativeFetch = vi.fn().mockResolvedValue(new Response(null, { status: 204 }));
    window.fetch = nativeFetch;
    installMutationIdempotency();

    await window.fetch('https://objects.example/upload', { method: 'PUT' });

    expect(nativeFetch).toHaveBeenCalledWith('https://objects.example/upload', { method: 'PUT' });
  });
});
