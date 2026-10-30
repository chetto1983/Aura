import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import i18n from '../../../i18n/i18n';
import { RemoteAccessPanel } from '../RemoteAccessPanel';
import {
  REMOTE_ACCESS_REASONS,
  type RemoteAccessReason,
  type RemoteAccessStatusDTO,
} from '../remoteAccessApi';
import { reasonAction, reasonStep } from '../remoteAccessSteps';

const firstSetup: RemoteAccessStatusDTO = {
  enabled: false,
  phase: 'disabled',
  api_token_set: false,
  generation: 1,
  nameservers: [],
  reason: '',
  accepted: false,
  reachable: false,
  abandoned: [],
};

const healthy: RemoteAccessStatusDTO = {
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
  generation: 5,
};

interface Sent {
  readonly url: string;
  readonly method: string;
  readonly body: unknown;
}

// A status the test can move on, and every write the wizard sends: the shape of the real API.
function stubApi(initial: RemoteAccessStatusDTO) {
  const api = {
    status: initial,
    sent: [] as Sent[],
    refuse: undefined as { status: number; body: object } | undefined,
    onWrite: (_write: Sent): void => undefined,
  };
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = input instanceof Request ? input.url : input.toString();
      const method = init?.method ?? 'GET';
      if (method === 'GET') return Promise.resolve(new Response(JSON.stringify(api.status)));
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

function renderPanel() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <RemoteAccessPanel />
    </QueryClientProvider>,
  );
  return client;
}

const strip = () => screen.getByRole('list', { name: 'Avanzamento configurazione accesso remoto' });
const states = () => [...strip().querySelectorAll('li')].map((li) => li.getAttribute('data-state'));
const openStep = (name: string) => {
  fireEvent.click(screen.getByRole('button', { name: new RegExp(name) }));
};
const card = (name: string) => screen.findByRole('heading', { level: 3, name });

const CLOUDFLARE = 'Collega il tuo account Cloudflare';
const DOMAIN = 'Scegli il dominio';
const CONNECTION = 'Collegamento con Cloudflare';
const PHONE = 'Prova dal telefono';

const secretKey = 'top-secret';

async function verifyToken(value: string) {
  fireEvent.change(await screen.findByLabelText('Token API Cloudflare'), { target: { value } });
  fireEvent.click(screen.getByRole('button', { name: 'Verifica token' }));
  await screen.findByRole('combobox', { name: 'Account Cloudflare' });
  await waitFor(() => {
    expect(screen.getByRole('button', { name: 'Verifica token' }).hasAttribute('aria-busy')).toBe(
      false,
    );
  });
}

describe('RemoteAccessWizard', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('it');
  });
  afterEach(async () => {
    vi.unstubAllGlobals();
    await i18n.changeLanguage('en');
  });

  it('is one heading and one strip, with no status page beside it', async () => {
    stubApi(healthy);
    renderPanel();
    await card(PHONE);
    expect(screen.getAllByRole('heading', { level: 2 })).toHaveLength(1);
    const section = screen.getByRole('heading', { level: 2 }).closest('section');
    expect(section?.className).toContain('min-w-0');
    expect(section?.getAttribute('aria-labelledby')).toBe('remote-access-heading');
    expect(strip()).toBeTruthy();
  });

  it('healthy: every step stays green, and the last one is on screen', async () => {
    stubApi(healthy);
    renderPanel();
    await card(PHONE);
    expect(states()).toEqual(['done', 'done', 'done', 'done']);
    expect(screen.getByText('Passo 4 di 4', { exact: false }).textContent).toContain(
      'Prova dal telefono',
    );
  });

  it('healthy: clicking Dominio opens it holding zone_name; Indietro opens Cloudflare', async () => {
    stubApi(healthy);
    renderPanel();
    await card(PHONE);
    openStep('Dominio');
    await card(DOMAIN);
    expect(screen.getByLabelText<HTMLInputElement>('Dominio').value).toBe('example.com');
    fireEvent.click(screen.getByRole('button', { name: 'Indietro' }));
    await card(CLOUDFLARE);
    expect(screen.getByText('Passo 1 di 4', { exact: false })).toBeTruthy();
  });

  it('walks forward with Avanti over steps with nothing to save', async () => {
    const api = stubApi(healthy);
    renderPanel();
    await card(PHONE);
    openStep('Cloudflare');
    await card(CLOUDFLARE);
    fireEvent.click(screen.getByRole('button', { name: 'Avanti' }));
    await card(DOMAIN);
    fireEvent.click(screen.getByRole('button', { name: 'Avanti' }));
    await card(CONNECTION);
    fireEvent.click(screen.getByRole('button', { name: 'Avanti' }));
    await card(PHONE);
    expect(api.sent).toEqual([]);
  });

  it('walks back with Indietro from the last step to the first', async () => {
    stubApi(healthy);
    renderPanel();
    await card(PHONE);
    fireEvent.click(screen.getByRole('button', { name: 'Indietro' }));
    await card(CONNECTION);
    fireEvent.click(screen.getByRole('button', { name: 'Indietro' }));
    await card(DOMAIN);
    fireEvent.click(screen.getByRole('button', { name: 'Indietro' }));
    await card(CLOUDFLARE);
  });

  it('never reopens an upcoming step', async () => {
    stubApi(firstSetup);
    renderPanel();
    await card(CLOUDFLARE);
    expect(screen.queryByRole('button', { name: /Dominio/ })).toBeNull();
  });

  it('follows the status on its own while no step was opened by hand', async () => {
    const api = stubApi({ ...healthy, phase: 'provisioning', accepted: false, reachable: false });
    const client = renderPanel();
    await card(CONNECTION);
    api.status = { ...healthy, phase: 'connecting', accepted: false, reachable: false };
    await client.invalidateQueries();
    await card(PHONE);
  });

  it('a step opened by hand stays put while the status moves on', async () => {
    const api = stubApi({ ...healthy, phase: 'provisioning', accepted: false, reachable: false });
    const client = renderPanel();
    await card(CONNECTION);
    openStep('Dominio');
    await card(DOMAIN);
    api.status = { ...healthy, phase: 'connecting', accepted: false, reachable: false };
    await client.invalidateQueries();
    await screen.findByRole('button', { name: /Collegamento/ });
    expect(screen.getByRole('heading', { level: 3 }).textContent).toBe(DOMAIN);
  });

  it('first setup: Avanti saves the token and the account, then Dominio opens', async () => {
    const api = stubApi(firstSetup);
    api.onWrite = (write) => {
      if (write.method === 'PUT') {
        api.status = { ...firstSetup, api_token_set: true, account_id: 'one', generation: 2 };
      }
    };
    renderPanel();
    await verifyToken('cf-token');
    fireEvent.click(screen.getByRole('button', { name: 'Avanti' }));
    await card(DOMAIN);
    expect(api.sent.at(-1)).toEqual({
      url: '/api/settings/remote-access',
      method: 'PUT',
      body: {
        enabled: false,
        generation: 1,
        account_id: 'one',
        zone_name: '',
        public_label: '',
        api_token: 'cf-token',
      },
    });
  });

  it('configured install: a new token keeps the address, and the wizard stays green', async () => {
    const api = stubApi(healthy);
    renderPanel();
    await card(PHONE);
    openStep('Cloudflare');
    await verifyToken('replacement');
    fireEvent.click(screen.getByRole('button', { name: 'Avanti' }));
    await card(PHONE);
    expect(api.sent.at(-1)?.body).toEqual({
      enabled: true,
      generation: 5,
      account_id: 'one',
      zone_name: 'example.com',
      public_label: 'casa',
      api_token: 'replacement',
    });
    expect(states()).toEqual(['done', 'done', 'done', 'done']);
  });

  it('keeps write-only tokens out of the query and mutation caches', async () => {
    const api = stubApi(firstSetup);
    const client = renderPanel();
    await verifyToken(secretKey);
    fireEvent.click(screen.getByRole('button', { name: 'Avanti' }));
    await waitFor(() => {
      expect(api.sent.some((write) => write.method === 'PUT')).toBe(true);
    });
    expect(JSON.stringify(client.getMutationCache().getAll())).not.toContain(secretKey);
    expect(JSON.stringify(client.getQueryCache().getAll())).not.toContain(secretKey);
  });

  it('a stale save refreshes the status, so the next Avanti carries the new generation', async () => {
    const api = stubApi({ ...healthy, phase: 'disabled', enabled: false, reachable: false });
    renderPanel();
    await card(DOMAIN);
    api.status = { ...api.status, generation: 6 };
    api.refuse = {
      status: 409,
      body: { error: 'configuration changed; refresh and retry', reason: '' },
    };
    fireEvent.change(screen.getByLabelText('Dominio'), { target: { value: 'nuovo.it' } });
    fireEvent.click(screen.getByRole('button', { name: 'Avanti' }));
    expect((await screen.findByRole('alert')).textContent).toContain('cambiata nel frattempo');
    expect(screen.getByLabelText<HTMLInputElement>('Dominio').value).toBe('nuovo.it');
    fireEvent.click(screen.getByRole('button', { name: 'Avanti' }));
    await waitFor(() => {
      expect(api.sent).toHaveLength(2);
    });
    expect(api.sent.at(-1)?.body).toEqual(
      expect.objectContaining({ generation: 6, zone_name: 'nuovo.it' }),
    );
  });

  it('a failed save keeps its card even when the refreshed status has moved on', async () => {
    const api = stubApi({ ...healthy, phase: 'disabled', enabled: false, reachable: false });
    renderPanel();
    await card(DOMAIN);
    api.status = { ...healthy, phase: 'provisioning', accepted: false, reachable: false };
    api.refuse = { status: 409, body: { error: 'changed', reason: '' } };
    fireEvent.change(screen.getByLabelText('Dominio'), { target: { value: 'nuovo.it' } });
    fireEvent.click(screen.getByRole('button', { name: 'Avanti' }));
    await screen.findByRole('alert');
    expect(screen.getByLabelText<HTMLInputElement>('Dominio').value).toBe('nuovo.it');
    expect(screen.getByRole('heading', { level: 3 }).textContent).toBe(DOMAIN);
  });

  it('Elimina leaves a row that opens Dominio with the old address, ready to save again', async () => {
    const left = { ...healthy, enabled: false, phase: 'disabled', accepted: false } as const;
    const api = stubApi({ ...left, reachable: false });
    renderPanel();
    await card(DOMAIN);
    expect(screen.getByLabelText<HTMLInputElement>('Dominio').value).toBe('example.com');
    expect(screen.getByText(/L’accesso remoto è spento/)).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Elimina…' })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Avanti' }));
    await waitFor(() => {
      expect(api.sent.at(-1)?.body).toEqual(
        expect.objectContaining({ enabled: true, zone_name: 'example.com' }),
      );
    });
  });

  it('Disattiva keeps Elimina… within reach on the Dominio card', async () => {
    const api = stubApi({ ...healthy, enabled: false, phase: 'disabled', reachable: false });
    renderPanel();
    await card(DOMAIN);
    expect(screen.queryByRole('button', { name: 'Disattiva' })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Elimina…' }));
    fireEvent.change(screen.getByRole('textbox', { name: /Scrivi casa.example.com/ }), {
      target: { value: 'casa.example.com' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Elimina definitivamente' }));
    await waitFor(() => {
      expect(api.sent.at(-1)).toEqual({
        url: '/api/settings/remote-access',
        method: 'DELETE',
        body: { hostname: 'casa.example.com' },
      });
    });
  });

  it('a save on a deleting row is never stuck: Dominio comes back and asks for one more save', async () => {
    const deleting = { ...healthy, enabled: false, phase: 'deleting', accepted: false } as const;
    const api = stubApi({ ...deleting, reachable: false });
    api.onWrite = () => {
      api.status = { ...api.status, zone_name: 'nuovo.it', generation: 6 };
    };
    const client = renderPanel();
    await card(CONNECTION);
    openStep('Dominio');
    await card(DOMAIN);
    fireEvent.change(screen.getByLabelText('Dominio'), { target: { value: 'nuovo.it' } });
    fireEvent.click(screen.getByRole('button', { name: 'Avanti' }));
    await card(CONNECTION);
    expect(screen.getByRole('status').textContent).toContain('Aura sta eliminando');
    api.status = { ...api.status, phase: 'disabled' };
    await client.invalidateQueries();
    await card(DOMAIN);
    expect(screen.getByLabelText<HTMLInputElement>('Dominio').value).toBe('nuovo.it');
    expect(screen.getByRole('button', { name: 'Avanti' }).hasAttribute('disabled')).toBe(false);
    expect(screen.getByText(/L’accesso remoto è spento/)).toBeTruthy();
  });

  it('a list of abandoned resources brings Cloudflare forward until Fatto', async () => {
    const api = stubApi({ ...healthy, abandoned: [{ kind: 'tunnel', name: 'aura-1234' }] });
    api.onWrite = () => {
      api.status = { ...api.status, abandoned: [] };
    };
    renderPanel();
    await card(CLOUDFLARE);
    expect(states()).toEqual(['done', 'done', 'done', 'done']);
    fireEvent.click(screen.getByRole('button', { name: 'Fatto' }));
    await card(PHONE);
    expect(api.sent.at(-1)).toEqual({
      url: '/api/settings/remote-access/abandoned/dismiss',
      method: 'POST',
      body: {},
    });
  });

  it('the list never traps the strip: the step the status is on still opens', async () => {
    stubApi({ ...healthy, abandoned: [{ kind: 'tunnel', name: 'aura-1234' }] });
    renderPanel();
    await card(CLOUDFLARE);
    openStep('Prova dal telefono');
    await card(PHONE);
    openStep('Cloudflare');
    await card(CLOUDFLARE);
  });

  it('a failed row offers its way out on the step it landed on', async () => {
    stubApi({ ...healthy, phase: 'error', reason: 'not_owned', accepted: false, reachable: false });
    renderPanel();
    await card(CONNECTION);
    expect(screen.getByRole('button', { name: 'Disattiva' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Elimina…' })).toBeTruthy();
  });

  it('a step opened by hand does not repeat that way out', async () => {
    stubApi({ ...healthy, phase: 'degraded', reason: 'tunnel_down', reachable: false });
    renderPanel();
    await card(PHONE);
    openStep('Dominio');
    await card(DOMAIN);
    expect(screen.queryByRole('button', { name: 'Disattiva' })).toBeNull();
  });

  it('Disattiva on the last step sends it, and the wizard follows to Dominio', async () => {
    const api = stubApi(healthy);
    api.onWrite = () => {
      api.status = { ...healthy, enabled: false, phase: 'disabled', reachable: false };
    };
    renderPanel();
    await card(PHONE);
    fireEvent.click(screen.getByRole('button', { name: 'Disattiva' }));
    await card(DOMAIN);
    expect(api.sent).toEqual([
      { url: '/api/settings/remote-access/disable', method: 'POST', body: {} },
    ]);
  });

  it('Riprova sends the status’s generation', async () => {
    const api = stubApi({ ...healthy, phase: 'error', reason: 'unknown', reachable: false });
    renderPanel();
    fireEvent.click(await screen.findByRole('button', { name: 'Riprova' }));
    await waitFor(() => {
      expect(api.sent).toEqual([
        {
          url: '/api/settings/remote-access/reconcile',
          method: 'POST',
          body: { generation: 5 },
        },
      ]);
    });
  });

  it('Rileggi il token ruotato sends the status’s generation', async () => {
    const api = stubApi(healthy);
    renderPanel();
    await card(PHONE);
    openStep('Collegamento');
    fireEvent.click(await screen.findByRole('button', { name: 'Rileggi il token ruotato' }));
    await waitFor(() => {
      expect(api.sent).toEqual([
        {
          url: '/api/settings/remote-access/token/refresh',
          method: 'POST',
          body: { generation: 5 },
        },
      ]);
    });
  });

  it('the phone confirmation goes out relative, with no tunnel marker of its own', async () => {
    vi.stubGlobal('location', new URL('https://casa.example.com/'));
    const api = stubApi({ ...healthy, phase: 'connecting', accepted: false, reachable: false });
    renderPanel();
    fireEvent.click(await screen.findByRole('button', { name: 'Conferma l’accesso da fuori' }));
    await waitFor(() => {
      expect(api.sent).toEqual([
        {
          url: '/api/settings/remote-access/accept-external',
          method: 'POST',
          body: { generation: 5 },
        },
      ]);
    });
  });

  it('shows a loading state, then a retry when the status cannot be read', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response('{"error":"down"}', { status: 503 }))),
    );
    renderPanel();
    expect(screen.getByRole('status').textContent).toContain('Caricamento');
    expect(await screen.findByRole('button', { name: 'Riprova' })).toBeTruthy();
    stubApi(healthy);
    fireEvent.click(screen.getByRole('button', { name: 'Riprova' }));
    await card(PHONE);
  });

  describe('no dead ends: every reason on the step it opens', () => {
    const withReason = (reason: RemoteAccessReason): RemoteAccessStatusDTO => ({
      ...healthy,
      phase: reason === 'tunnel_down' ? 'degraded' : 'error',
      reason,
      accepted: reason === 'tunnel_down',
      reachable: false,
    });

    it.each(REMOTE_ACCESS_REASONS)('%s leads somewhere', async (reason) => {
      const api = stubApi(withReason(reason));
      renderPanel();
      const notice = await screen.findByRole('alert');
      const action = notice.querySelector('button');
      expect(notice.textContent?.length).toBeGreaterThan(20);
      expect(action).not.toBeNull();
      if (action === null) return;
      fireEvent.click(action);
      const home = reasonStep(reason);
      if (reasonAction(reason, home) === 'retry') {
        await waitFor(() => {
          expect(api.sent.at(-1)?.url).toBe('/api/settings/remote-access/reconcile');
        });
        return;
      }
      await waitFor(() => {
        const field =
          reason === 'hostname_taken' ? /Etichetta/ : /^Token API Cloudflare$|^Dominio$/;
        expect(document.activeElement).toBe(screen.getByLabelText(field));
      });
    });

    it('a save refused for a dead token walks the operator to Cloudflare', async () => {
      const api = stubApi({ ...healthy, enabled: false, phase: 'disabled', reachable: false });
      renderPanel();
      await card(DOMAIN);
      api.refuse = { status: 400, body: { error: 'invalid', reason: 'token_rejected' } };
      fireEvent.click(screen.getByRole('button', { name: 'Avanti' }));
      fireEvent.click(await screen.findByRole('button', { name: 'Incolla un token nuovo' }));
      await card(CLOUDFLARE);
    });
  });
});
