import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { HttpError } from '../../../api/json';
import i18n from '../../../i18n/i18n';
import { PhoneStep } from '../PhoneStep';
import { RemoteAccessError, type RemoteAccessStatusDTO } from '../remoteAccessApi';

const healthy: RemoteAccessStatusDTO = {
  enabled: true,
  phase: 'healthy',
  api_token_set: true,
  account_id: 'account',
  zone_name: 'example.com',
  public_label: 'aura',
  public_hostname: 'aura.example.com',
  generation: 4,
  nameservers: [],
  reason: '',
  accepted: true,
  reachable: true,
  has_resources: true,
  last_checked_at: '2026-10-30T09:15:00Z',
  abandoned: [],
};

const down: RemoteAccessStatusDTO = {
  ...healthy,
  phase: 'degraded',
  reason: 'tunnel_down',
  reachable: false,
};

const waiting: RemoteAccessStatusDTO = {
  ...healthy,
  phase: 'connecting',
  accepted: false,
  reachable: false,
};

function renderStep(status: RemoteAccessStatusDTO) {
  const props = {
    onAccept: vi.fn(() => Promise.resolve()),
    onRetry: vi.fn(() => Promise.resolve()),
    onDisable: vi.fn(() => Promise.resolve()),
    onDelete: vi.fn(() => Promise.resolve()),
    onBack: vi.fn(),
    onOpenStep: vi.fn(),
  };
  render(<PhoneStep status={status} {...props} />);
  return props;
}

const openLink = () => screen.getByRole('link', { name: 'Apri l’indirizzo pubblico' });
const acceptName = 'Conferma l’accesso da fuori';

describe('PhoneStep', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('it');
  });
  afterEach(async () => {
    vi.unstubAllGlobals();
    await i18n.changeLanguage('en');
  });

  it('healthy: reachable, when it was checked, the link, Disattiva and Elimina…', () => {
    renderStep(healthy);
    const line = screen.getByRole('status');
    expect(line.textContent).toContain('Raggiungibile da fuori casa');
    expect(line.querySelector('svg')).not.toBeNull();
    expect(screen.getByText(/Ultimo controllo/).textContent).toContain('2026');
    expect(openLink().getAttribute('href')).toBe(
      'https://aura.example.com/?settings=remote-access',
    );
    expect(screen.getByRole('button', { name: 'Disattiva' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Elimina…' })).toBeTruthy();
  });

  it('shows no check time before the first check', () => {
    renderStep({ ...healthy, last_checked_at: undefined } as unknown as RemoteAccessStatusDTO);
    expect(screen.queryByText(/Ultimo controllo/)).toBeNull();
  });

  it('tunnel_down: not reachable, with no phone test to redo', () => {
    renderStep(down);
    expect(screen.getByRole('status').textContent).toContain('Non raggiungibile');
    expect(screen.queryByRole('button', { name: acceptName })).toBeNull();
    expect(screen.queryByText(/disponibile solo/)).toBeNull();
  });

  it('tunnel_down never offers the test, even if the row lost its acceptance', () => {
    renderStep({ ...down, accepted: false });
    expect(screen.queryByRole('button', { name: acceptName })).toBeNull();
    expect(screen.queryByText(/disponibile solo/)).toBeNull();
    expect(screen.getByRole('status').textContent).toContain('Non raggiungibile');
  });

  it('tunnel_down: the action checks now', async () => {
    const { onRetry } = renderStep(down);
    fireEvent.click(screen.getByRole('button', { name: 'Controlla ora' }));
    await waitFor(() => {
      expect(onRetry).toHaveBeenCalledOnce();
    });
  });

  it('reachable is what it shows, whatever the phase says', () => {
    renderStep({ ...healthy, reachable: false });
    expect(screen.getByRole('status').textContent).toContain('Non raggiungibile');
  });

  it('not accepted, away from the public hostname: the link and where to confirm', () => {
    renderStep(waiting);
    expect(openLink()).toBeTruthy();
    expect(screen.getByText(/disponibile solo/)).toBeTruthy();
    expect(screen.queryByRole('button', { name: acceptName })).toBeNull();
    expect(screen.queryByText('Non raggiungibile')).toBeNull();
    expect(screen.queryByText('Raggiungibile da fuori casa')).toBeNull();
  });

  it('on the public hostname the confirmation is there, and sends it', async () => {
    vi.stubGlobal('location', new URL('https://AURA.example.com/'));
    const { onAccept } = renderStep(waiting);
    expect(screen.queryByText(/disponibile solo/)).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: acceptName }));
    await waitFor(() => {
      expect(onAccept).toHaveBeenCalledOnce();
    });
  });

  it('a refused confirmation says why', async () => {
    vi.stubGlobal('location', new URL('https://aura.example.com/'));
    const { onAccept } = renderStep(waiting);
    onAccept.mockRejectedValueOnce(new RemoteAccessError(new HttpError(409, 'changed'), ''));
    fireEvent.click(screen.getByRole('button', { name: acceptName }));
    expect((await screen.findByRole('alert')).textContent).toContain('cambiata nel frattempo');
  });

  it('Disattiva calls it', async () => {
    const { onDisable } = renderStep(healthy);
    fireEvent.click(screen.getByRole('button', { name: 'Disattiva' }));
    await waitFor(() => {
      expect(onDisable).toHaveBeenCalledOnce();
    });
  });

  it('a refused Disattiva says so', async () => {
    const { onDisable } = renderStep(healthy);
    onDisable.mockRejectedValueOnce(new TypeError('Failed to fetch'));
    fireEvent.click(screen.getByRole('button', { name: 'Disattiva' }));
    expect(await screen.findByRole('alert')).toBeTruthy();
  });

  it('Elimina… asks for the exact hostname and returns focus to its trigger', async () => {
    const { onDelete } = renderStep(healthy);
    const trigger = screen.getByRole('button', { name: 'Elimina…' });
    fireEvent.click(trigger);
    const confirm = screen.getByRole('button', { name: 'Elimina definitivamente' });
    expect(confirm.hasAttribute('disabled')).toBe(true);
    const field = screen.getByLabelText('Scrivi aura.example.com per confermare');
    fireEvent.change(field, { target: { value: 'wrong.example.com' } });
    expect(confirm.hasAttribute('disabled')).toBe(true);
    fireEvent.change(field, { target: { value: 'aura.example.com' } });
    fireEvent.click(confirm);
    await waitFor(() => {
      expect(onDelete).toHaveBeenCalledExactlyOnceWith('aura.example.com');
    });
    await waitFor(() => {
      expect(document.activeElement).toBe(trigger);
    });
  });

  it('a refused delete stays inside its open dialog', async () => {
    const { onDelete } = renderStep(healthy);
    onDelete.mockRejectedValueOnce(
      new RemoteAccessError(new HttpError(409, 'conflict'), 'not_owned'),
    );
    fireEvent.click(screen.getByRole('button', { name: 'Elimina…' }));
    const dialog = screen.getByRole('alertdialog');
    fireEvent.change(within(dialog).getByRole('textbox'), {
      target: { value: 'aura.example.com' },
    });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Elimina definitivamente' }));
    expect((await within(dialog).findByRole('alert')).textContent).toContain('non ha creato');
  });

  it('closing the dialog forgets what was typed', () => {
    renderStep(healthy);
    fireEvent.click(screen.getByRole('button', { name: 'Elimina…' }));
    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'aura.example.com' } });
    fireEvent.click(screen.getByRole('button', { name: 'Annulla' }));
    fireEvent.click(screen.getByRole('button', { name: 'Elimina…' }));
    expect(screen.getByRole<HTMLInputElement>('textbox').value).toBe('');
  });

  it('Indietro walks back, and there is no Avanti on the last step', () => {
    const { onBack } = renderStep(healthy);
    fireEvent.click(screen.getByRole('button', { name: 'Indietro' }));
    expect(onBack).toHaveBeenCalledOnce();
    expect(screen.queryByRole('button', { name: 'Avanti' })).toBeNull();
  });

  it('every action is at least 44px tall', () => {
    renderStep(healthy);
    for (const control of [...screen.getAllByRole('button'), openLink()]) {
      expect(control.className).toContain('min-h-[44px]');
    }
  });
});
