import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import i18n from '../../../i18n/i18n';
import { ConnectionStep } from '../ConnectionStep';
import type { RemoteAccessReason, RemoteAccessStatusDTO } from '../remoteAccessApi';

const running: RemoteAccessStatusDTO = {
  enabled: true,
  phase: 'provisioning',
  api_token_set: true,
  account_id: 'account',
  zone_name: 'example.com',
  public_label: 'aura',
  public_hostname: 'aura.example.com',
  generation: 4,
  nameservers: [],
  reason: '',
  accepted: false,
  reachable: false,
  abandoned: [],
};

function renderStep(status: RemoteAccessStatusDTO) {
  const props = {
    onRetry: vi.fn(() => Promise.resolve()),
    onRefreshToken: vi.fn(() => Promise.resolve()),
    onBack: vi.fn(),
    onNext: vi.fn(),
    onOpenStep: vi.fn(),
  };
  render(<ConnectionStep status={status} {...props} />);
  return props;
}

const avanti = () => screen.getByRole('button', { name: 'Avanti' });

describe('ConnectionStep', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('it');
  });
  afterEach(async () => {
    await i18n.changeLanguage('en');
  });

  it.each(['validating', 'provisioning'] as const)(
    '%s: it says what is happening, and why Avanti waits',
    (phase) => {
      renderStep({ ...running, phase });
      expect(screen.getByRole('status').textContent).toContain('Aura sta creando');
      expect(avanti().hasAttribute('disabled')).toBe(true);
      expect(screen.getByText(/si apre da solo/)).toBeTruthy();
    },
  );

  it('deleting: it says so, and points at what comes next', () => {
    renderStep({ ...running, enabled: false, phase: 'deleting' });
    expect(screen.getByRole('status').textContent).toContain('Aura sta eliminando');
    expect(screen.getByRole('status').textContent).toContain('Dominio');
    expect(avanti().hasAttribute('disabled')).toBe(true);
  });

  it.each<RemoteAccessReason>([
    'not_owned',
    'no_administrator',
    'cloudflare_unreachable',
    'old_address_present',
    'unknown',
  ])('%s: its notice retries, and Avanti says the problem comes first', async (reason) => {
    const { onRetry } = renderStep({ ...running, phase: 'error', reason });
    expect(avanti().hasAttribute('disabled')).toBe(true);
    expect(screen.getByText(/Risolvi il problema qui sopra/)).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: /Riprova/ }));
    await waitFor(() => {
      expect(onRetry).toHaveBeenCalledOnce();
    });
  });

  it('a failed deletion keeps saying what it is doing', () => {
    renderStep({ ...running, enabled: false, phase: 'deleting', reason: 'cloudflare_unreachable' });
    expect(screen.getByRole('status').textContent).toContain('Aura sta eliminando');
    expect(screen.getByRole('alert')).toBeTruthy();
  });

  it('a retry that fails says why and can be pressed again', async () => {
    const { onRetry } = renderStep({ ...running, phase: 'error', reason: 'unknown' });
    onRetry.mockRejectedValueOnce(new TypeError('Failed to fetch'));
    fireEvent.click(screen.getByRole('button', { name: /Riprova/ }));
    expect(await screen.findByText(/non è andata a buon fine/)).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: /Riprova/ }));
    await waitFor(() => {
      expect(onRetry).toHaveBeenCalledTimes(2);
    });
  });

  it('once the connection stands, Avanti walks on', () => {
    const { onNext } = renderStep({ ...running, phase: 'connecting' });
    expect(screen.getByRole('status').textContent).toContain('pronto');
    fireEvent.click(avanti());
    expect(onNext).toHaveBeenCalledOnce();
  });

  it('Indietro walks back, even from a failure', () => {
    const { onBack } = renderStep({ ...running, phase: 'error', reason: 'unknown' });
    fireEvent.click(screen.getByRole('button', { name: 'Indietro' }));
    expect(onBack).toHaveBeenCalledOnce();
  });

  describe('a tunnel token rotated in the Cloudflare dashboard', () => {
    const live = { ...running, phase: 'healthy', accepted: true, reachable: true } as const;

    it('can be read again once the tunnel exists', async () => {
      const { onRefreshToken } = renderStep(live);
      expect(screen.getByRole('link', { name: 'Come ruotare il token del tunnel' })).toBeTruthy();
      fireEvent.click(screen.getByRole('button', { name: 'Rileggi il token ruotato' }));
      await waitFor(() => {
        expect(onRefreshToken).toHaveBeenCalledOnce();
      });
    });

    it('a refused read says so', async () => {
      const { onRefreshToken } = renderStep(live);
      onRefreshToken.mockRejectedValueOnce(new TypeError('Failed to fetch'));
      fireEvent.click(screen.getByRole('button', { name: 'Rileggi il token ruotato' }));
      expect(await screen.findByRole('alert')).toBeTruthy();
    });

    it('is not offered before there is a tunnel', () => {
      renderStep(running);
      expect(screen.queryByRole('button', { name: 'Rileggi il token ruotato' })).toBeNull();
    });

    it('is not offered on a switched-off row, where the server would refuse it', () => {
      renderStep({ ...running, enabled: false, phase: 'deleting' });
      expect(screen.queryByRole('button', { name: 'Rileggi il token ruotato' })).toBeNull();
    });
  });
});
