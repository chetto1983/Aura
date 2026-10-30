import { createElement } from 'react';
import { expect, vi } from 'vitest';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { RemoteAccessPanel } from '../RemoteAccessPanel';
import type { RemoteAccessStatusDTO } from '../remoteAccessApi';

// The whole panel over a stubbed API, shared by the wizard's two suites. The status can be moved by
// a test, and every write the wizard sends is kept.

export const firstSetup: RemoteAccessStatusDTO = {
  enabled: false,
  phase: 'disabled',
  api_token_set: false,
  generation: 1,
  nameservers: [],
  reason: '',
  accepted: false,
  reachable: false,
  has_resources: false,
  abandoned: [],
};

export const healthy: RemoteAccessStatusDTO = {
  ...firstSetup,
  enabled: true,
  phase: 'healthy',
  api_token_set: true,
  account_id: 'one',
  account_name: 'Acme',
  zone_name: 'example.com',
  public_label: 'casa',
  public_hostname: 'casa.example.com',
  accepted: true,
  reachable: true,
  has_resources: true,
  generation: 5,
};

interface Sent {
  readonly url: string;
  readonly method: string;
  readonly body: unknown;
}

// A status the test can move on, and every write the wizard sends: the shape of the real API.
export function stubApi(initial: RemoteAccessStatusDTO) {
  const api = {
    status: initial,
    sent: [] as Sent[],
    refuse: undefined as { status: number; body: object } | undefined,
    readsFail: false,
    onWrite: (_write: Sent): void => undefined,
  };
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = input instanceof Request ? input.url : input.toString();
      const method = init?.method ?? 'GET';
      if (method === 'GET') {
        return Promise.resolve(
          api.readsFail
            ? new Response('{"error":"down"}', { status: 503 })
            : new Response(JSON.stringify(api.status)),
        );
      }
      const write = {
        url,
        method,
        body: typeof init?.body === 'string' ? (JSON.parse(init.body) as unknown) : undefined,
      };
      api.sent.push(write);
      if (api.refuse !== undefined) {
        const { status, body } = api.refuse;
        api.refuse = undefined;
        return Promise.resolve(new Response(JSON.stringify(body), { status }));
      }
      api.onWrite(write);
      if (url.endsWith('/token/verify')) {
        return Promise.resolve(
          new Response(JSON.stringify({ accounts: [{ id: 'one', name: 'Acme' }] })),
        );
      }
      return Promise.resolve(new Response('{"status":"accepted"}', { status: 202 }));
    }),
  );
  return api;
}

export function renderPanel() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(createElement(QueryClientProvider, { client }, createElement(RemoteAccessPanel)));
  return client;
}

export const strip = () =>
  screen.getByRole('list', { name: 'Avanzamento configurazione accesso remoto' });
export const states = () =>
  [...strip().querySelectorAll('li')].map((li) => li.getAttribute('data-state'));
export const openStep = (name: string) => {
  fireEvent.click(screen.getByRole('button', { name: new RegExp(name) }));
};
export const card = (name: string) => screen.findByRole('heading', { level: 3, name });
export const shownCard = () => screen.getByRole('heading', { level: 3 }).textContent;
export const fieldValue = (label: string | RegExp) =>
  screen.getByLabelText<HTMLInputElement>(label).value;
export const type = (label: string | RegExp, value: string) => {
  fireEvent.change(screen.getByLabelText(label), { target: { value } });
};

// A poll, up to the render it causes: Query notifies its observers on a timer of its own.
export const poll = (client: QueryClient) =>
  act(async () => {
    await client.refetchQueries();
    await new Promise((resolve) => {
      setTimeout(resolve, 0);
    });
  });

export const CLOUDFLARE = 'Collega il tuo account Cloudflare';
export const DOMAIN = 'Scegli il dominio';
export const CONNECTION = 'Collegamento con Cloudflare';
export const PHONE = 'Prova dal telefono';
export const LOAD_ERROR = /Impossibile caricare/;

export async function verifyToken(value: string) {
  fireEvent.change(await screen.findByLabelText('Token API Cloudflare'), { target: { value } });
  fireEvent.click(screen.getByRole('button', { name: 'Verifica token' }));
  await screen.findByRole('combobox', { name: 'Account Cloudflare' });
  await waitFor(() => {
    expect(screen.getByRole('button', { name: 'Verifica token' }).hasAttribute('aria-busy')).toBe(
      false,
    );
  });
}
