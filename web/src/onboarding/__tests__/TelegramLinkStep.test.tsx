import type { ReactNode } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys
import type { OnboardingTelegramStatus } from '../onboardingApi';

// TelegramLinkStep test (ONBD-01b / T-28-06-02). Proves the no-leak posture (the bot token never
// enters the DOM — only the t.me deep-link + the QR <img>) and the link-state machine: the poll
// flips the UI to "Telegram linked" on {linked:true}, an error (expired/404) shows the expired
// copy, and a missing deep-link shows the no-link note.

const fetchTelegramStatus = vi.fn();

vi.mock('../onboardingApi', () => ({
  fetchTelegramStatus: (...a: unknown[]) =>
    fetchTelegramStatus(...a) as Promise<OnboardingTelegramStatus>,
}));

const { TelegramLinkStep } = await import('../TelegramLinkStep');

const DEEP_LINK = 'https://t.me/AuraBot?start=onb-xyz789';
const BOT_TOKEN = '1234567890:AAH-this-is-the-secret-bot-token';
const QR_SVG = '<svg xmlns="http://www.w3.org/2000/svg"><rect width="10" height="10"/></svg>';

function client() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

function renderStep(props: Partial<React.ComponentProps<typeof TelegramLinkStep>> = {}) {
  const merged: React.ComponentProps<typeof TelegramLinkStep> = {
    sessionToken: 'sess-1',
    deepLink: DEEP_LINK,
    qrSvg: QR_SVG,
    polling: true,
    ...props,
  };
  return render(<TelegramLinkStep {...merged} />, {
    wrapper: ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client()}>{children}</QueryClientProvider>
    ),
  });
}

describe('TelegramLinkStep (ONBD-01b)', () => {
  beforeEach(() => {
    fetchTelegramStatus.mockReset();
    fetchTelegramStatus.mockResolvedValue({ linked: false });
  });
  afterEach(() => {
    vi.clearAllMocks();
  });

  it('renders the deep-link button + the QR, and NEVER the bot token (T-28-06-02)', async () => {
    const { container } = renderStep();

    const link = await screen.findByRole('link', { name: 'Open in Telegram' });
    expect(link.getAttribute('href')).toBe(DEEP_LINK);

    // The QR renders as an <img> data: URI (not dangerouslySetInnerHTML) with the scan caption.
    const img = screen.getByRole('img', { name: 'QR code to link Telegram' });
    expect(img.getAttribute('src')).toContain('data:image/svg+xml');
    expect(screen.getByText('Or scan to link Telegram')).toBeTruthy();

    // The bot token must NOT appear anywhere in the rendered DOM — only the deep-link + QR.
    expect(container.innerHTML).not.toContain(BOT_TOKEN);
  });

  it('flips to the linked state when the poll returns {linked:true}', async () => {
    fetchTelegramStatus.mockResolvedValue({ linked: true });
    renderStep();
    await waitFor(() => {
      expect(screen.getByText('Telegram linked')).toBeTruthy();
    });
    // The deep-link button is gone once linked.
    expect(screen.queryByRole('link', { name: 'Open in Telegram' })).toBeNull();
  });

  it('shows the waiting note while not yet linked', async () => {
    renderStep();
    await waitFor(() => {
      expect(screen.getByText('Waiting for the link to be scanned…')).toBeTruthy();
    });
  });

  it('shows the expired copy when the status poll errors (404/expired)', async () => {
    fetchTelegramStatus.mockRejectedValue(new Error('HTTP 404'));
    renderStep();
    await waitFor(() => {
      expect(screen.getByText('This link expired. Generate a new one to continue.')).toBeTruthy();
    });
  });

  it('renders the no-link note when no deep-link was minted', () => {
    renderStep({ deepLink: undefined });
    expect(screen.getByText('No Telegram link was generated for this identity.')).toBeTruthy();
    expect(screen.queryByRole('link')).toBeNull();
  });

  it('does not poll when polling is disabled', () => {
    renderStep({ polling: false });
    expect(fetchTelegramStatus).not.toHaveBeenCalled();
  });
});
