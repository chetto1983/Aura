import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, screen, waitFor } from '@testing-library/react';
import i18n from '../../../i18n/i18n';
import type { RemoteAccessReason } from '../remoteAccessApi';
import {
  CLOUDFLARE,
  CONNECTION,
  DOMAIN,
  LOAD_ERROR,
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
  type,
  verifyToken,
} from './wizardHarness';

const off = { ...healthy, enabled: false, phase: 'disabled', reachable: false } as const;

function confirmDelete() {
  const trigger = screen.getByRole('button', { name: 'Elimina…' });
  trigger.focus();
  fireEvent.click(trigger);
  fireEvent.change(screen.getByRole('textbox', { name: /Scrivi casa.example.com/ }), {
    target: { value: 'casa.example.com' },
  });
  fireEvent.click(screen.getByRole('button', { name: 'Elimina definitivamente' }));
}

// What a failure, a poll or walking the steps must never take from the operator.
describe('RemoteAccessWizard, under failures and polls', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('it');
  });
  afterEach(async () => {
    vi.unstubAllGlobals();
    await i18n.changeLanguage('en');
  });

  describe('a refresh that fails', () => {
    const off = { ...healthy, phase: 'disabled', enabled: false, reachable: false } as const;

    it('during a save keeps the card, what was typed and the failure’s sentence', async () => {
      const api = stubApi(off);
      renderPanel();
      await card(DOMAIN);
      type('Dominio', 'nuovo.it');
      type(/Etichetta/, 'tana');
      api.readsFail = true;
      api.refuse = { status: 503, body: { error: 'down' } };
      fireEvent.click(screen.getByRole('button', { name: 'Avanti' }));
      expect((await screen.findByRole('alert')).textContent).toContain('non è andata a buon fine');
      expect(shownCard()).toBe(DOMAIN);
      expect(fieldValue('Dominio')).toBe('nuovo.it');
      expect(fieldValue(/Etichetta/)).toBe('tana');
      expect(screen.queryByText(LOAD_ERROR)).toBeNull();
    });

    it('during a save on Cloudflare keeps the token that was typed', async () => {
      const api = stubApi(firstSetup);
      renderPanel();
      await verifyToken('cf-token');
      api.readsFail = true;
      api.refuse = { status: 502, body: { error: 'down', reason: 'cloudflare_unreachable' } };
      fireEvent.click(screen.getByRole('button', { name: 'Avanti' }));
      expect((await screen.findByRole('alert')).textContent).toContain('Cloudflare non risponde');
      expect(shownCard()).toBe(CLOUDFLARE);
      expect(fieldValue('Token API Cloudflare')).toBe('cf-token');
    });

    it('during a poll keeps the card and what was typed, through the next good poll too', async () => {
      const api = stubApi({ ...healthy, phase: 'waiting_nameservers', accepted: false });
      const client = renderPanel();
      await card(DOMAIN);
      type('Dominio', 'nuovo.it');
      api.readsFail = true;
      await poll(client);
      expect(screen.queryByText(LOAD_ERROR)).toBeNull();
      expect(shownCard()).toBe(DOMAIN);
      expect(fieldValue('Dominio')).toBe('nuovo.it');
      api.readsFail = false;
      await poll(client);
      expect(shownCard()).toBe(DOMAIN);
      expect(fieldValue('Dominio')).toBe('nuovo.it');
    });

    it('during a poll that was following the status keeps the step on screen', async () => {
      const api = stubApi(healthy);
      const client = renderPanel();
      await card(PHONE);
      api.readsFail = true;
      await poll(client);
      expect(shownCard()).toBe(PHONE);
      expect(screen.queryByText(LOAD_ERROR)).toBeNull();
    });
  });

  describe('the first load that fails', () => {
    it('still shows the load error and its retry', async () => {
      const api = stubApi(healthy);
      api.readsFail = true;
      renderPanel();
      expect(await screen.findByText(LOAD_ERROR)).toBeTruthy();
      api.readsFail = false;
      fireEvent.click(screen.getByRole('button', { name: 'Riprova' }));
      await card(PHONE);
    });
  });

  describe('one column', () => {
    it('holds the strip, the card and its way out, at the width of the nearby cards', async () => {
      stubApi({ ...healthy, phase: 'error', reason: 'not_owned', reachable: false });
      renderPanel();
      const heading = await card(CONNECTION);
      const column = heading.closest('.max-w-3xl');
      expect(column?.contains(strip())).toBe(true);
      expect(column?.contains(screen.getByRole('button', { name: 'Elimina…' }))).toBe(true);
      expect(document.querySelector('.max-w-xl')).toBeNull();
    });
  });

  describe('focus follows the step', () => {
    it('takes nothing on the first render', async () => {
      stubApi(healthy);
      renderPanel();
      await card(PHONE);
      expect(document.activeElement).toBe(document.body);
    });

    it('goes to the new card’s heading, so a phone brings it into view', async () => {
      stubApi(healthy);
      renderPanel();
      await card(PHONE);
      fireEvent.click(screen.getByRole('button', { name: 'Indietro' }));
      const heading = await card(CONNECTION);
      await waitFor(() => {
        expect(document.activeElement).toBe(heading);
      });
      expect(heading.getAttribute('tabindex')).toBe('-1');
    });

    it('never takes focus from where the operator is', async () => {
      const api = stubApi({ ...healthy, phase: 'provisioning', accepted: false, reachable: false });
      const client = renderPanel();
      await card(CONNECTION);
      const kept = screen.getByRole('button', { name: /Dominio/ });
      kept.focus();
      api.status = { ...healthy, phase: 'connecting', accepted: false, reachable: false };
      await client.invalidateQueries();
      await card(PHONE);
      expect(document.activeElement).toBe(kept);
    });

    it('after a delete goes to the step that follows, whose trigger is gone', async () => {
      const api = stubApi(healthy);
      api.onWrite = () => {
        api.status = {
          ...healthy,
          enabled: false,
          phase: 'disabled',
          accepted: false,
          reachable: false,
          has_resources: false,
        };
      };
      renderPanel();
      await card(PHONE);
      confirmDelete();
      const heading = await card(DOMAIN);
      await waitFor(() => {
        expect(document.activeElement).toBe(heading);
      });
    });

    it('after a delete that stays on the same step goes to its heading', async () => {
      const api = stubApi(off);
      api.onWrite = () => {
        api.status = { ...off, accepted: false, has_resources: false };
      };
      renderPanel();
      const heading = await card(DOMAIN);
      confirmDelete();
      await waitFor(() => {
        expect(screen.queryByRole('button', { name: 'Elimina…' })).toBeNull();
      });
      await waitFor(() => {
        expect(document.activeElement).toBe(heading);
      });
    });

    it('after Disattiva on the same step goes to its heading, whose button is gone', async () => {
      const waiting = { ...healthy, phase: 'waiting_nameservers', accepted: false } as const;
      const api = stubApi(waiting);
      api.onWrite = () => {
        api.status = { ...waiting, enabled: false, phase: 'disabled' };
      };
      renderPanel();
      const heading = await card(DOMAIN);
      const disable = screen.getByRole('button', { name: 'Disattiva' });
      disable.focus();
      fireEvent.click(disable);
      await waitFor(() => {
        expect(document.activeElement).toBe(heading);
      });
    });
  });

  describe('a keystroke pins the card', () => {
    it('on Dominio: a poll that moves the followed step cannot replace it', async () => {
      const waiting = { ...healthy, accepted: false, reachable: false } as const;
      const api = stubApi({ ...waiting, phase: 'waiting_nameservers' });
      const client = renderPanel();
      await card(DOMAIN);
      type('Dominio', 'giusto.it');
      api.status = { ...waiting, phase: 'provisioning' };
      await client.invalidateQueries();
      await waitFor(() => {
        expect(states()).toEqual(['done', 'done', 'active', 'upcoming']);
      });
      expect(shownCard()).toBe(DOMAIN);
      expect(fieldValue('Dominio')).toBe('giusto.it');
    });

    it('on Cloudflare: a poll that moves the followed step cannot replace it', async () => {
      const api = stubApi(firstSetup);
      const client = renderPanel();
      await card(CLOUDFLARE);
      type('Token API Cloudflare', 'cf-token');
      api.status = { ...firstSetup, api_token_set: true, account_id: 'one', generation: 2 };
      await client.invalidateQueries();
      await waitFor(() => {
        expect(states()).toEqual(['done', 'active', 'upcoming', 'upcoming']);
      });
      expect(shownCard()).toBe(CLOUDFLARE);
      expect(fieldValue('Token API Cloudflare')).toBe('cf-token');
    });

    it('with nothing typed the card still follows the status', async () => {
      const waiting = { ...healthy, accepted: false, reachable: false } as const;
      const api = stubApi({ ...waiting, phase: 'waiting_nameservers' });
      const client = renderPanel();
      await card(DOMAIN);
      api.status = { ...waiting, phase: 'provisioning' };
      await client.invalidateQueries();
      await card(CONNECTION);
    });
  });

  describe('going back keeps the domain', () => {
    const saved = { ...firstSetup, api_token_set: true, account_id: 'one', generation: 2 };
    const back = () => {
      fireEvent.click(screen.getByRole('button', { name: 'Indietro' }));
    };
    const next = () => {
      fireEvent.click(screen.getByRole('button', { name: 'Avanti' }));
    };

    it('type, Indietro, Avanti: the values are there', async () => {
      stubApi(saved);
      renderPanel();
      await card(DOMAIN);
      type('Dominio', 'mio.it');
      type(/Etichetta/, 'tana');
      back();
      await card(CLOUDFLARE);
      next();
      await card(DOMAIN);
      expect(fieldValue('Dominio')).toBe('mio.it');
      expect(fieldValue(/Etichetta/)).toBe('tana');
    });

    it('a saved token keeps the draft too: the Cloudflare card never touches it', async () => {
      const api = stubApi(saved);
      api.onWrite = (write) => {
        if (write.method === 'PUT') api.status = { ...saved, generation: 3 };
      };
      renderPanel();
      await card(DOMAIN);
      type('Dominio', 'mio.it');
      back();
      await verifyToken('replacement');
      next();
      await card(DOMAIN);
      expect(api.sent.at(-1)?.body).toEqual(expect.objectContaining({ zone_name: '' }));
      expect(fieldValue('Dominio')).toBe('mio.it');
    });

    it('a saved address wins over a draft the operator emptied', async () => {
      const api = stubApi(saved);
      const client = renderPanel();
      await card(DOMAIN);
      type('Dominio', 'mio.it');
      type('Dominio', '');
      back();
      await card(CLOUDFLARE);
      api.status = healthy;
      await poll(client);
      openStep('Dominio');
      await card(DOMAIN);
      expect(fieldValue('Dominio')).toBe('example.com');
    });

    it('a field emptied on the card stays empty until the operator leaves it', async () => {
      stubApi({ ...healthy, enabled: false, phase: 'disabled', reachable: false });
      renderPanel();
      await card(DOMAIN);
      type('Dominio', '');
      expect(fieldValue('Dominio')).toBe('');
    });

    it('a configured row: walking away drops the edit, and Avanti sends nothing', async () => {
      const api = stubApi(healthy);
      renderPanel();
      await card(PHONE);
      openStep('Dominio');
      await card(DOMAIN);
      type('Dominio', 'altro.it');
      type(/Etichetta/, 'tana');
      back();
      await card(CLOUDFLARE);
      next();
      await card(DOMAIN);
      expect(fieldValue('Dominio')).toBe('example.com');
      expect(fieldValue(/Etichetta/)).toBe('casa');
      next();
      await card(CONNECTION);
      expect(api.sent).toEqual([]);
    });

    it('a first setup’s draft gives way once the status brings an address', async () => {
      const api = stubApi(saved);
      const client = renderPanel();
      await card(DOMAIN);
      type('Dominio', 'mio.it');
      back();
      await card(CLOUDFLARE);
      api.status = healthy;
      await poll(client);
      openStep('Dominio');
      await card(DOMAIN);
      expect(fieldValue('Dominio')).toBe('example.com');
    });

    it('after Elimina, a draft does not outrank the kept address', async () => {
      const api = stubApi(off);
      api.onWrite = () => {
        api.status = { ...off, accepted: false, has_resources: false };
      };
      renderPanel();
      await card(DOMAIN);
      type('Dominio', 'altro.it');
      confirmDelete();
      await waitFor(() => {
        expect(screen.queryByRole('button', { name: 'Elimina…' })).toBeNull();
      });
      await waitFor(() => {
        expect(fieldValue('Dominio')).toBe('example.com');
      });
    });

    it('a saved domain leaves no draft behind to outrank the status', async () => {
      const api = stubApi(saved);
      api.onWrite = (write) => {
        if (write.method === 'PUT') {
          api.status = { ...healthy, phase: 'provisioning', accepted: false, reachable: false };
        }
      };
      const client = renderPanel();
      await card(DOMAIN);
      type('Dominio', 'mio.it');
      next();
      await card(CONNECTION);
      api.status = { ...api.status, zone_name: 'altro.it' };
      await poll(client);
      back();
      await card(DOMAIN);
      expect(fieldValue('Dominio')).toBe('altro.it');
    });
  });

  it('a code this build does not know reads as unknown, never as a raw key', async () => {
    stubApi({
      ...healthy,
      phase: 'error',
      reason: 'brand_new_code' as RemoteAccessReason,
      reachable: false,
    });
    renderPanel();
    const notice = (await screen.findByRole('alert')).textContent;
    expect(notice).toContain('il dettaglio è nei log del server');
    expect(notice).not.toContain('brand_new_code');
    expect(notice).not.toContain('remoteAccess.');
  });
});
