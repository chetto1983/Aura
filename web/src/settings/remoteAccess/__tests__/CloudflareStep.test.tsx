import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { HttpError } from '../../../api/json';
import i18n from '../../../i18n/i18n';
import { chooseOption, optionNames, shownOption } from '../../../test/chooseOption';
import { CloudflareStep } from '../CloudflareStep';
import {
  RemoteAccessError,
  type CloudflareAccount,
  type RemoteAccessReason,
  type RemoteAccessStatusDTO,
} from '../remoteAccessApi';
import { TOKEN_TEMPLATE_URL } from '../remoteAccessSteps';

const firstSetup: RemoteAccessStatusDTO = {
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

const configured: RemoteAccessStatusDTO = {
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
  generation: 9,
};

const refused = (code: number, reason: RemoteAccessReason) =>
  new RemoteAccessError(new HttpError(code, 'refused'), reason);

function renderStep(
  status: RemoteAccessStatusDTO,
  accounts: readonly CloudflareAccount[] = [{ id: 'one', name: 'Acme' }],
) {
  const props = {
    onVerify: vi.fn(() => Promise.resolve(accounts)),
    onSave: vi.fn(() => Promise.resolve()),
    onDismiss: vi.fn(() => Promise.resolve()),
    onEdit: vi.fn(),
    onNext: vi.fn(),
    onOpenStep: vi.fn(),
  };
  render(<CloudflareStep status={status} {...props} />);
  return props;
}

const token = () => screen.getByLabelText<HTMLInputElement>('Token API Cloudflare');
const avanti = () => screen.getByRole('button', { name: 'Avanti' });
const verify = () => screen.getByRole('button', { name: 'Verifica token' });

async function verifyToken(value = 'cf-token') {
  fireEvent.change(token(), { target: { value } });
  fireEvent.click(verify());
  await screen.findByRole('combobox', { name: 'Account Cloudflare' });
  await waitFor(() => {
    expect(verify().hasAttribute('aria-busy')).toBe(false);
  });
}

describe('CloudflareStep', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('it');
  });
  afterEach(async () => {
    await i18n.changeLanguage('en');
  });

  it('has no Indietro: this is the first step', () => {
    renderStep(firstSetup);
    expect(screen.queryByRole('button', { name: 'Indietro' })).toBeNull();
  });

  it('links to the pre-filled form and names the permission Cloudflare will not pre-fill', () => {
    renderStep(firstSetup);
    const link = screen.getByRole('link', { name: 'Crea il token su Cloudflare' });
    expect(link.getAttribute('href')).toBe(TOKEN_TEMPLATE_URL);
    expect(link.getAttribute('rel')).toContain('noopener');
    expect(link.className).toContain('min-h-[44px]');
    expect(link.className).toContain('text-[15.5px]');
    expect(screen.getByText(/Aggiungi tu.*Cloudflare Tunnel Edit/)).toBeTruthy();
  });

  // The card asks the operator to add a permission by hand, so the list cannot sit behind a control.
  it('always shows every permission under its heading, and no WARP one', () => {
    renderStep(firstSetup);
    expect(document.querySelector('details')).toBeNull();
    const list = within(screen.getByRole('list', { name: 'Permessi che servono al token' }));
    expect(list.getAllByRole('listitem').map((row) => row.textContent)).toEqual([
      'Account Settings Read',
      'Zone Read',
      'Zone Edit',
      'DNS Edit',
      'Cloudflare Tunnel Edit',
      'Access: Apps and Policies Edit',
      'Access: Organizations, Identity Providers, and Groups Edit',
    ]);
    expect(screen.queryByText(/Zero Trust/)).toBeNull();
  });

  it('no token yet: Avanti is disabled and says why beside it', () => {
    renderStep(firstSetup);
    expect(avanti().hasAttribute('disabled')).toBe(true);
    expect(screen.getByText('Incolla il token per continuare')).toBeTruthy();
  });

  it('a running verify spins on its own button, not on Avanti as well', async () => {
    let finish: (accounts: readonly CloudflareAccount[]) => void = () => undefined;
    const onVerify = vi.fn(
      () =>
        new Promise<readonly CloudflareAccount[]>((resolve) => {
          finish = resolve;
        }),
    );
    render(
      <CloudflareStep
        status={firstSetup}
        onVerify={onVerify}
        onSave={vi.fn()}
        onDismiss={vi.fn()}
        onEdit={vi.fn()}
        onNext={vi.fn()}
        onOpenStep={vi.fn()}
      />,
    );
    fireEvent.change(token(), { target: { value: 'cf-token' } });
    fireEvent.click(verify());
    await waitFor(() => {
      expect(verify().getAttribute('aria-busy')).toBe('true');
    });
    expect(avanti().hasAttribute('aria-busy')).toBe(false);
    finish([{ id: 'one', name: 'Acme' }]);
    await screen.findByRole('combobox', { name: 'Account Cloudflare' });
  });

  it('a typed token must be verified first', () => {
    renderStep(firstSetup);
    fireEvent.change(token(), { target: { value: 'cf-token' } });
    expect(avanti().hasAttribute('disabled')).toBe(true);
    expect(screen.getByText('Verifica il token per continuare')).toBeTruthy();
  });

  it('first setup: Avanti sends the token, the account, enabled false and an empty domain', async () => {
    const { onVerify, onSave } = renderStep(firstSetup);
    await verifyToken();
    expect(onVerify).toHaveBeenCalledExactlyOnceWith('cf-token');
    expect(shownOption('Account Cloudflare')).toBe('Acme');
    fireEvent.click(avanti());
    await waitFor(() => {
      expect(onSave).toHaveBeenCalledExactlyOnceWith({
        enabled: false,
        generation: 1,
        account_id: 'one',
        zone_name: '',
        public_label: '',
        api_token: 'cf-token',
      });
    });
  });

  it('several accounts: the choice is the operator’s', async () => {
    const { onSave } = renderStep(firstSetup, [
      { id: 'one', name: 'Acme' },
      { id: 'two', name: 'Globex' },
    ]);
    await verifyToken();
    expect(avanti().hasAttribute('disabled')).toBe(true);
    expect(screen.getByText('Scegli l’account per continuare')).toBeTruthy();
    expect(await optionNames('Account Cloudflare')).toEqual(['Acme', 'Globex']);
    fireEvent.click(screen.getByRole('option', { name: 'Globex' }));
    await waitFor(() => {
      expect(avanti().hasAttribute('disabled')).toBe(false);
    });
    fireEvent.click(avanti());
    await waitFor(() => {
      expect(onSave).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining({ account_id: 'two' }),
      );
    });
  });

  it('a nameless account shows its id rather than a blank row', async () => {
    renderStep(firstSetup, [{ id: 'one', name: '' }]);
    await verifyToken();
    expect(shownOption('Account Cloudflare')).toBe('one');
  });

  it('editing a verified token asks for a new verification', async () => {
    renderStep(firstSetup);
    await verifyToken();
    fireEvent.change(token(), { target: { value: 'another' } });
    expect(avanti().hasAttribute('disabled')).toBe(true);
    expect(screen.getByText('Verifica il token per continuare')).toBeTruthy();
  });

  it('the account select shows account_name after a reload', () => {
    renderStep(configured);
    expect(shownOption('Account Cloudflare')).toBe('Acme');
  });

  it('falls back to the id when Cloudflare could not name the account', () => {
    renderStep({ ...configured, account_name: undefined } as unknown as RemoteAccessStatusDTO);
    expect(shownOption('Account Cloudflare')).toBe('one');
  });

  it('configured install: a new token keeps the address and the enabled state', async () => {
    const { onSave } = renderStep(configured);
    await verifyToken('replacement');
    fireEvent.click(avanti());
    await waitFor(() => {
      expect(onSave).toHaveBeenCalledExactlyOnceWith({
        enabled: true,
        generation: 9,
        account_id: 'one',
        zone_name: 'example.com',
        public_label: 'casa',
        api_token: 'replacement',
      });
    });
    await waitFor(() => {
      expect(token().value).toBe('');
    });
  });

  it('a new token that sees the old account keeps it chosen among several', async () => {
    renderStep(configured, [
      { id: 'two', name: 'Globex' },
      { id: 'one', name: 'Acme' },
    ]);
    await verifyToken();
    expect(shownOption('Account Cloudflare')).toBe('Acme');
    expect(avanti().hasAttribute('disabled')).toBe(false);
  });

  it('a new token can move the install to another account', async () => {
    const { onSave } = renderStep(configured, [
      { id: 'one', name: 'Acme' },
      { id: 'two', name: 'Globex' },
    ]);
    await verifyToken();
    await chooseOption('Account Cloudflare', 'Globex');
    fireEvent.click(avanti());
    await waitFor(() => {
      expect(onSave).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining({ account_id: 'two', zone_name: 'example.com' }),
      );
    });
  });

  it('with nothing typed on a working install, Avanti just walks on', () => {
    const { onNext, onSave } = renderStep(configured);
    fireEvent.click(avanti());
    expect(onNext).toHaveBeenCalledOnce();
    expect(onSave).not.toHaveBeenCalled();
  });

  it.each<RemoteAccessReason>(['token_rejected', 'no_accounts', 'account_not_visible'])(
    '%s on the row: Avanti waits for a new token and the action hands the field back',
    (reason) => {
      renderStep({ ...configured, phase: 'error', reason });
      expect(avanti().hasAttribute('disabled')).toBe(true);
      expect(screen.getByText('Incolla un token nuovo per continuare')).toBeTruthy();
      const notice = screen.getByRole('alert');
      fireEvent.click(within(notice).getByRole('button'));
      expect(document.activeElement).toBe(token());
    },
  );

  it('a verify with no accounts shows its sentence and keeps what was typed', async () => {
    const onVerify = vi.fn(() => Promise.reject(refused(400, 'no_accounts')));
    render(
      <CloudflareStep
        status={firstSetup}
        onVerify={onVerify}
        onSave={vi.fn()}
        onDismiss={vi.fn()}
        onEdit={vi.fn()}
        onNext={vi.fn()}
        onOpenStep={vi.fn()}
      />,
    );
    fireEvent.change(token(), { target: { value: 'cf-token' } });
    fireEvent.click(verify());
    const alert = await screen.findByRole('alert');
    expect(alert.textContent).toContain('Account Settings Read');
    expect(token().value).toBe('cf-token');
    expect(token().getAttribute('aria-invalid')).toBe('true');
  });

  it('a verify Cloudflare could not answer is retried from its notice', async () => {
    const onVerify = vi
      .fn<() => Promise<readonly CloudflareAccount[]>>()
      .mockRejectedValueOnce(refused(502, 'cloudflare_unreachable'))
      .mockResolvedValueOnce([{ id: 'one', name: 'Acme' }]);
    render(
      <CloudflareStep
        status={firstSetup}
        onVerify={onVerify}
        onSave={vi.fn()}
        onDismiss={vi.fn()}
        onEdit={vi.fn()}
        onNext={vi.fn()}
        onOpenStep={vi.fn()}
      />,
    );
    fireEvent.change(token(), { target: { value: 'cf-token' } });
    fireEvent.click(verify());
    fireEvent.click(await screen.findByRole('button', { name: 'Riprova ora' }));
    await screen.findByRole('combobox', { name: 'Account Cloudflare' });
    expect(onVerify).toHaveBeenCalledTimes(2);
  });

  it('a save refused as stale keeps the verified token and shows the refresh message', async () => {
    const { onSave } = renderStep(configured);
    onSave.mockRejectedValueOnce(refused(409, ''));
    await verifyToken('replacement');
    fireEvent.click(avanti());
    expect((await screen.findByRole('alert')).textContent).toContain('cambiata nel frattempo');
    expect(token().value).toBe('replacement');
    expect(avanti().hasAttribute('disabled')).toBe(false);
  });

  it('a blank address can never be sent: it always carries the status’s zone', async () => {
    const { onSave } = renderStep({ ...configured, enabled: false, phase: 'disabled' });
    await verifyToken('replacement');
    fireEvent.click(avanti());
    await waitFor(() => {
      expect(onSave).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining({ enabled: false, zone_name: 'example.com' }),
      );
    });
  });

  it('a keystroke in the token field tells the wizard', () => {
    const { onEdit } = renderStep(firstSetup);
    fireEvent.change(token(), { target: { value: 'c' } });
    expect(onEdit).toHaveBeenCalledOnce();
  });

  describe('the account select', () => {
    const locked = 'L’account cambia con un token che ne vede un altro.';

    it('says why it is locked', () => {
      renderStep(configured);
      expect(screen.getByRole('combobox', { name: 'Account Cloudflare' })).toBeTruthy();
      expect(screen.getByText(locked)).toBeTruthy();
    });

    it('says nothing once a verified token can change it', async () => {
      renderStep(configured);
      await verifyToken();
      expect(screen.queryByText(locked)).toBeNull();
    });

    it('says nothing when there is no select at all', () => {
      renderStep(firstSetup);
      expect(screen.queryByText(locked)).toBeNull();
    });
  });

  describe('resources left on Cloudflare', () => {
    const abandoned = {
      ...configured,
      abandoned: [
        { kind: 'tunnel', name: 'aura-1234' },
        { kind: 'dns', name: 'casa.example.com' },
        { kind: 'access_app', name: 'aura-1234-public' },
        { kind: 'access_policy', name: 'aura-1234-public-members' },
        { kind: 'posture', name: 'aura-1234-gateway' },
      ],
    };

    it('are listed by kind and name', () => {
      renderStep(abandoned);
      const list = within(screen.getByRole('list', { name: /eliminare a mano/ }));
      expect(list.getAllByRole('listitem').map((row) => row.textContent)).toEqual([
        'Tunnelaura-1234',
        'Record DNScasa.example.com',
        'Applicazione Accessaura-1234-public',
        'Regola Accessaura-1234-public-members',
        'Controllo del dispositivoaura-1234-gateway',
      ]);
    });

    // The list also comes from retiring the old access method, with the same token and account.
    it('say what the list is and what to do, and nothing about why', () => {
      renderStep(abandoned);
      const said = screen.getByText(/Queste risorse sono rimaste su Cloudflare/).textContent;
      expect(said).toContain('Eliminale dal dashboard Cloudflare, poi premi Fatto.');
      expect(said).not.toMatch(/token|account/i);
    });

    it('a kind this cockpit does not know still shows, by its own name', () => {
      renderStep({ ...configured, abandoned: [{ kind: 'novelty', name: 'thing' }] });
      expect(screen.getByText('novelty')).toBeTruthy();
    });

    it('Fatto calls dismiss', async () => {
      const { onDismiss } = renderStep(abandoned);
      fireEvent.click(screen.getByRole('button', { name: 'Fatto' }));
      await waitFor(() => {
        expect(onDismiss).toHaveBeenCalledOnce();
      });
    });

    it('a failed dismiss says so', async () => {
      const { onDismiss } = renderStep(abandoned);
      onDismiss.mockRejectedValueOnce(new TypeError('Failed to fetch'));
      fireEvent.click(screen.getByRole('button', { name: 'Fatto' }));
      expect(await screen.findByRole('alert')).toBeTruthy();
    });

    it('show nothing when the list is empty', () => {
      renderStep(configured);
      expect(screen.queryByRole('button', { name: 'Fatto' })).toBeNull();
    });
  });
});
