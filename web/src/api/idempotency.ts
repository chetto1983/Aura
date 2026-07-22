const MUTATION_METHODS = new Set(['POST', 'PUT', 'PATCH', 'DELETE']);

/**
 * Installs the browser ingress half of the durable mutation contract. One UUID
 * is attached to the concrete fetch request and is therefore retained by
 * Request cloning, the service worker, and transport retries. Explicit caller
 * keys are preserved for workflows that own a longer-lived logical operation.
 */
export function installMutationIdempotency(): void {
  const nativeFetch = window.fetch.bind(window);
  window.fetch = (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const request = input instanceof Request ? input : undefined;
    const method = (init?.method ?? request?.method ?? 'GET').toUpperCase();
    const rawURL = input instanceof Request ? input.url : input instanceof URL ? input.href : input;
    const url = new URL(rawURL, window.location.href);
    if (!MUTATION_METHODS.has(method) || url.origin !== window.location.origin) {
      return nativeFetch(input, init);
    }

    const headers = new Headers(request?.headers);
    new Headers(init?.headers).forEach((value, name) => {
      headers.set(name, value);
    });
    if (!headers.has('Idempotency-Key')) {
      headers.set('Idempotency-Key', crypto.randomUUID());
    }
    return nativeFetch(input, { ...init, headers });
  };
}
