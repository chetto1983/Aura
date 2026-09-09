import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import { CreditPanel } from '../CreditPanel';

// CreditPanel (CRED-03/CRED-06/CRED-09). The suite pins the three things this surface can get
// wrong in the direction that LOOKS fine: a sub-cent spend rounded away, a non-billing backend
// shown a zero balance, and the admin's typed cap displayed instead of the one the server
// applied. Every figure comes off the GET response, which reads Aura's own ledger (D-08).

const ID = 'id-alice';

function urlOf(input: RequestInfo | URL): string {
  if (typeof input === 'string') return input;
  if (input instanceof URL) return input.href;
  return input.url;
}

function stubFetch(opts: {
  readonly credit: unknown;
  readonly post?: unknown;
  readonly postStatus?: number;
}) {
  const spy = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = urlOf(input);
    const method = init?.method ?? 'GET';
    if (url.includes('/credit') && method === 'POST') {
      const status = opts.postStatus ?? 200;
      return Promise.resolve(
        new Response(JSON.stringify(opts.post ?? { error: 'nope' }), { status }),
      );
    }
    return Promise.resolve(new Response(JSON.stringify(opts.credit), { status: 200 }));
  });
  vi.stubGlobal('fetch', spy);
  return spy;
}

function renderPanel() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <CreditPanel identityId={ID} />
    </QueryClientProvider>,
  );
}

const BILLING = {
  identity_id: ID,
  exempt: false,
  cap: 5,
  reset_interval: 'monthly',
  spend: 1.25,
  remaining: 3.75,
  percent_used: 25,
};

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe('CreditPanel — the billing surface', () => {
  it('renders cap, reset interval and the spend gauge from the ledger response', async () => {
    stubFetch({ credit: BILLING });
    renderPanel();

    const cap = await screen.findByLabelText<HTMLInputElement>('Spending cap');
    expect(cap.value).toBe('5.00');
    expect(cap.getAttribute('inputmode')).toBe('decimal');
    expect(cap.getAttribute('step')).toBe('0.01');
    expect(cap.getAttribute('min')).toBe('0');
    expect(cap.className).toContain('font-mono');

    const select = screen.getByLabelText<HTMLSelectElement>('Reset interval');
    expect(select.value).toBe('monthly');
    expect([...select.options].map((o) => o.value)).toEqual(['daily', 'weekly', 'monthly']);

    const bar = screen.getByRole('progressbar');
    expect(bar.getAttribute('aria-valuenow')).toBe('25');
    expect(screen.getByText('$1.25 / $5.00 · 25%')).toBeTruthy();
    expect(screen.getByText('Remaining: $3.75')).toBeTruthy();
  });

  // The three tiers are ContextBudgetGauge's own, imported by name — 70 and 90, not new numbers.
  it.each([
    { percent: 69, fill: 'bg-accent' },
    { percent: 70, fill: 'bg-warning' },
    { percent: 89, fill: 'bg-warning' },
    { percent: 90, fill: 'bg-danger' },
  ])('renders the $fill tier at $percent%', async ({ percent, fill }) => {
    stubFetch({ credit: { ...BILLING, percent_used: percent } });
    renderPanel();
    const bar = await screen.findByRole('progressbar');
    expect(bar.getAttribute('aria-valuenow')).toBe(String(percent));
    expect(bar.firstElementChild?.className).toContain(fill);
  });

  // Exhaustion is decided server-side by COMPARISON, not by a float ratio that can land a hair
  // under 100 at the boundary. The panel renders what the server decided, either way.
  it('renders 100% and the danger tier when spend equals cap, and under 100 one cent below', async () => {
    stubFetch({
      credit: { ...BILLING, spend: 5, remaining: 0, percent_used: 100 },
    });
    const { unmount } = renderPanel();
    const bar = await screen.findByRole('progressbar');
    expect(bar.getAttribute('aria-valuenow')).toBe('100');
    expect(bar.firstElementChild?.className).toContain('bg-danger');
    expect(screen.getByText('$5.00 / $5.00 · 100%')).toBeTruthy();
    unmount();

    stubFetch({ credit: { ...BILLING, spend: 4.99, remaining: 0.01, percent_used: 99 } });
    renderPanel();
    const nearly = await screen.findByRole('progressbar');
    expect(nearly.getAttribute('aria-valuenow')).toBe('99');
  });

  // Migration 0124 widened cost_usd to numeric(24,12) and 61c30e4a6 stopped the server rounding
  // a spend to cents. A client that rendered two decimals would undo both, one layer up.
  it('renders a sub-cent spend truthfully rather than collapsing it to two decimals', async () => {
    stubFetch({
      credit: { ...BILLING, spend: 0.000004158, remaining: 4.999995842, percent_used: 0 },
    });
    renderPanel();
    await screen.findByRole('progressbar');
    expect(screen.getByText(/\$0\.00000416 \/ \$5\.00 · 0%/)).toBeTruthy();
    expect(screen.queryByText('$0.00 / $5.00 · 0%')).toBeNull();
  });

  // Backstop decision (UI-SPEC §Resolved-backstop "Credit panel · partial"): the wire cannot
  // tell "no ledger rows yet" from "a genuine zero" — credit_api.go's creditGetResponse reports
  // spend 0 for both — so the panel renders 0%, and does NOT invent an em-dash distinction it
  // has no evidence for.
  it('renders 0% for a fresh identity with a cap and no spend, not a placeholder', async () => {
    stubFetch({ credit: { ...BILLING, spend: 0, remaining: 5, percent_used: 0 } });
    renderPanel();
    const bar = await screen.findByRole('progressbar');
    expect(bar.getAttribute('aria-valuenow')).toBe('0');
    expect(bar.firstElementChild?.className).toContain('bg-accent');
    expect(screen.getByText('$0.00 / $5.00 · 0%')).toBeTruthy();
    expect(screen.queryByText(/—/)).toBeNull();
  });
});

describe('CreditPanel — saving a cap', () => {
  it('renders the applied cap after a precision round, never the typed value', async () => {
    stubFetch({
      credit: BILLING,
      post: {
        identity_id: ID,
        cap: 5.13,
        reset_interval: 'monthly',
        store_applied: true,
        provider_applied: true,
      },
    });
    renderPanel();

    const cap = await screen.findByLabelText<HTMLInputElement>('Spending cap');
    fireEvent.change(cap, { target: { value: '5.126' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save cap' }));

    await waitFor(() => {
      expect(cap.value).toBe('5.13');
    });
  });

  // Two strings, not one generic "a moment": the measured latencies differ by 5x (M-06 raising,
  // M-05 lowering) and a single copy would make the slower of them look broken.
  it.each([
    { applied: 9, copy: 'Takes about 25 seconds to apply.' },
    { applied: 2, copy: 'Takes about 5 seconds to apply.' },
  ])('shows the $copy advisory for the direction of the change', async ({ applied, copy }) => {
    stubFetch({
      credit: BILLING,
      post: {
        identity_id: ID,
        cap: applied,
        reset_interval: 'monthly',
        store_applied: true,
        provider_applied: true,
      },
    });
    renderPanel();

    const cap = await screen.findByLabelText<HTMLInputElement>('Spending cap');
    fireEvent.change(cap, { target: { value: String(applied) } });
    fireEvent.click(screen.getByRole('button', { name: 'Save cap' }));

    expect(await screen.findByText(copy)).toBeTruthy();
  });

  it('renders the save error through the destructive alert', async () => {
    stubFetch({ credit: BILLING, postStatus: 502 });
    renderPanel();

    const cap = await screen.findByLabelText<HTMLInputElement>('Spending cap');
    fireEvent.change(cap, { target: { value: '9.00' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save cap' }));

    expect(
      await screen.findByText("Couldn't update the spending cap. Check the amount and try again."),
    ).toBeTruthy();
  });
});

describe('CreditPanel — the CRED-09 exemption', () => {
  // An exemption and an exhaustion are different facts. A deployment that bills nothing gets the
  // Empty composition, never the $0.00 LibreChat's tokenCredits.toFixed(2) default produces.
  it('renders the exempt empty state and no zero balance on a non-billing backend', async () => {
    stubFetch({ credit: { identity_id: ID, exempt: true } });
    renderPanel();

    expect(await screen.findByText('No spending cap to show')).toBeTruthy();
    expect(
      screen.getByText(
        "This deployment runs on a local model backend, which doesn't bill — there's no cap or spend to show.",
      ),
    ).toBeTruthy();
    expect(screen.queryByText('$0.00')).toBeNull();
    expect(screen.queryByRole('progressbar')).toBeNull();
    expect(screen.queryByRole('button', { name: 'Save cap' })).toBeNull();
  });
});

describe('CreditPanel — the load guards', () => {
  it('renders the shared spinner guard while the credit read is in flight', () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => new Promise(() => undefined)),
    );
    renderPanel();
    expect(screen.getByRole('status').textContent).toContain('Loading credit');
  });

  it('renders the destructive alert when the credit read fails', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response('nope', { status: 502 }))),
    );
    renderPanel();
    expect(
      await screen.findByText("Couldn't load this identity's credit. Try again."),
    ).toBeTruthy();
  });
});
