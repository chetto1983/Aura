import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, screen, waitFor } from '@testing-library/react';
import i18n from '../../../i18n/i18n';
import {
  REMOTE_ACCESS_REASONS,
  type RemoteAccessReason,
  type RemoteAccessStatusDTO,
} from '../remoteAccessApi';
import { reasonAction, reasonStep } from '../remoteAccessSteps';
import {
  CLOUDFLARE,
  CONNECTION,
  DOMAIN,
  PHONE,
  card,
  fieldValue,
  firstSetup,
  healthy,
  openStep,
  poll,
  renderPanel,
  shownCard,
  states,
  strip,
  stubApi,
  verifyToken,
} from './wizardHarness';

const secretKey = 'top-secret';

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

  it('configured install: a new token walks the row back to green, and keeps the address', async () => {
    const api = stubApi(healthy);
    api.onWrite = (write) => {
      if (write.method === 'PUT') {
        api.status = { ...healthy, phase: 'validating', reachable: false, generation: 6 };
      }
    };
    const client = renderPanel();
    await card(PHONE);
    openStep('Cloudflare');
    await verifyToken('replacement');
    fireEvent.click(screen.getByRole('button', { name: 'Avanti' }));

    await card(CONNECTION);
    expect(api.sent.at(-1)?.body).toEqual({
      enabled: true,
      generation: 5,
      account_id: 'one',
      zone_name: 'example.com',
      public_label: 'casa',
      api_token: 'replacement',
    });
    expect(screen.getByRole('status').textContent).toContain('Aura sta controllando');
    expect(screen.getByTestId('spinner')).toBeTruthy();
    expect(states()).toEqual(['done', 'done', 'active', 'upcoming']);

    api.status = { ...api.status, phase: 'provisioning' };
    await poll(client);
    expect(shownCard()).toBe(CONNECTION);
    expect(screen.getByTestId('spinner')).toBeTruthy();
    expect(states()).toEqual(['done', 'done', 'active', 'upcoming']);

    api.status = { ...healthy, generation: 7 };
    await client.invalidateQueries();
    await card(PHONE);
    expect(states()).toEqual(['done', 'done', 'done', 'done']);
    expect(screen.getByRole('status').textContent).toContain('Raggiungibile da fuori casa');
    expect(screen.queryByText(/con i dati mobili/)).toBeNull();
    expect(screen.queryByRole('button', { name: 'Conferma l’accesso da fuori' })).toBeNull();
    expect(screen.getByText('https://casa.example.com')).toBeTruthy();

    openStep('Cloudflare');
    await card(CLOUDFLARE);
    expect(fieldValue('Token API Cloudflare')).toBe('');
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

  it('Elimina… is quiet, and the filled form stays on the dialog’s confirm', async () => {
    stubApi(healthy);
    renderPanel();
    await card(PHONE);
    const trigger = screen.getByRole('button', { name: 'Elimina…' });
    expect(trigger.className.split(' ')).toEqual(
      expect.arrayContaining(['border-danger', 'text-danger']),
    );
    expect(trigger.className).not.toContain('bg-destructive');
    fireEvent.click(trigger);
    expect(screen.getByRole('button', { name: 'Elimina definitivamente' }).className).toContain(
      'bg-destructive',
    );
  });

  it('a row switched off before its first acceptance still offers Elimina…', async () => {
    stubApi({ ...healthy, enabled: false, phase: 'disabled', accepted: false, reachable: false });
    renderPanel();
    await card(DOMAIN);
    expect(screen.getByRole('button', { name: 'Elimina…' })).toBeTruthy();
  });

  it('a failed row with nothing on Cloudflare has nothing to delete', async () => {
    stubApi({
      ...healthy,
      phase: 'error',
      reason: 'not_owned',
      accepted: false,
      reachable: false,
      has_resources: false,
    });
    renderPanel();
    await card(CONNECTION);
    expect(screen.getByRole('button', { name: 'Disattiva' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Elimina…' })).toBeNull();
  });

  it('Elimina leaves a row that opens Dominio with the old address, ready to save again', async () => {
    const left = {
      ...healthy,
      enabled: false,
      phase: 'disabled',
      accepted: false,
      has_resources: false,
    } as const;
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

  describe('waiting for the nameservers, for as long as the registrar takes', () => {
    const waiting = {
      ...healthy,
      phase: 'waiting_nameservers',
      accepted: false,
      reachable: false,
      nameservers: ['ada.ns.cloudflare.com', 'bob.ns.cloudflare.com'],
    } as const;

    it('the card with the nameservers offers both ways out', async () => {
      stubApi(waiting);
      renderPanel();
      await card(DOMAIN);
      expect(screen.getByText('ada.ns.cloudflare.com')).toBeTruthy();
      expect(screen.getByRole('button', { name: 'Disattiva' })).toBeTruthy();
      expect(screen.getByRole('button', { name: 'Elimina…' })).toBeTruthy();
    });

    it('Elimina… still follows what is on Cloudflare', async () => {
      stubApi({ ...waiting, has_resources: false });
      renderPanel();
      await card(DOMAIN);
      expect(screen.getByRole('button', { name: 'Disattiva' })).toBeTruthy();
      expect(screen.queryByRole('button', { name: 'Elimina…' })).toBeNull();
    });

    it('Disattiva sends it, and the row stays on Dominio, switched off', async () => {
      const api = stubApi(waiting);
      api.onWrite = () => {
        api.status = { ...waiting, enabled: false, phase: 'disabled' };
      };
      renderPanel();
      await card(DOMAIN);
      fireEvent.click(screen.getByRole('button', { name: 'Disattiva' }));
      expect(await screen.findByText(/L’accesso remoto è spento/)).toBeTruthy();
      expect(api.sent).toEqual([
        { url: '/api/settings/remote-access/disable', method: 'POST', body: {} },
      ]);
      expect(shownCard()).toBe(DOMAIN);
    });
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
