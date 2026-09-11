import { afterEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import { IdentityRoster } from '../IdentityRoster';
import { SpendOverview } from '../SpendOverview';

// SpendOverview (RBAC-11/CRED-06, plan 02-09) — the account-wide reconciliation dashboard.
// This suite pins the things this surface can get wrong in the direction that looks fine: a
// naive "up is green" delta on a rising spend figure, a $0.11 collapsed by compact notation,
// a reconciliation outage that takes the roster down with it, and inert tab chrome for the
// three explicitly out-of-scope OpenRouter tabs.

const ADMIN_IDENTITY = {
  id: 'id-admin',
  name: 'admin@aura.local',
  kind: 'user',
  capabilities: ['identity.create', 'identity.delete', 'agent.run'],
};
const MEMBER_IDENTITY = {
  id: 'id-alice',
  name: 'alice@example.com',
  kind: 'user',
  capabilities: ['agent.run', 'governance.write'],
};

function urlOf(input: RequestInfo | URL): string {
  if (typeof input === 'string') return input;
  if (input instanceof URL) return input.href;
  return input.url;
}

interface TileFixture {
  value: number;
  delta: number | null;
}

function fiveTiles(overrides?: Record<string, TileFixture>) {
  const base: Record<string, TileFixture> = {
    total_usage: { value: 5.52, delta: 20 },
    request_count: { value: 3000, delta: 20 },
    tokens_total: { value: 52_100_000, delta: 20 },
    cache_hit_rate: { value: 0.515, delta: 20 },
    blended_cost_per_million_tokens: { value: 0.11, delta: 20 },
  };
  const merged: Record<string, TileFixture> = { ...base, ...overrides };
  return Object.entries(merged).map(([metric, v]) => ({
    metric,
    value: v.value,
    delta_percent: v.delta,
    series: Array.from({ length: 12 }, (_, i) => i),
  }));
}

function overviewPayload(overrides?: {
  kpis?: ReturnType<typeof fiveTiles>;
  topIdentities?: unknown[];
  overAllocation?: { triggered: boolean; sum_caps: number; available: number };
}) {
  return {
    kpis: overrides?.kpis ?? fiveTiles(),
    top_identities: overrides?.topIdentities ?? [
      {
        identity_id: 'id-alice',
        name: 'alice@example.com',
        masked_label: 'sk-or-v1-caa...61c',
        lifetime_spend: 5.52,
      },
    ],
    over_allocation: overrides?.overAllocation ?? { triggered: false, sum_caps: 10, available: 20 },
  };
}

function stubFetch(opts: {
  readonly overview: unknown;
  readonly overviewStatus?: number;
  readonly identities?: readonly (typeof ADMIN_IDENTITY)[];
  readonly selfId?: string;
}) {
  const spy = vi.fn((input: RequestInfo | URL) => {
    const url = urlOf(input);
    if (url.includes('/api/admin/spend/overview')) {
      const status = opts.overviewStatus ?? 200;
      if (status !== 200) {
        return Promise.resolve(new Response('nope', { status }));
      }
      return Promise.resolve(new Response(JSON.stringify(opts.overview), { status: 200 }));
    }
    if (url.includes('/api/me')) {
      return Promise.resolve(
        new Response(JSON.stringify({ identity_id: opts.selfId ?? 'id-admin', capabilities: [] }), {
          status: 200,
        }),
      );
    }
    return Promise.resolve(
      new Response(
        JSON.stringify({ identities: opts.identities ?? [ADMIN_IDENTITY, MEMBER_IDENTITY] }),
        {
          status: 200,
        },
      ),
    );
  });
  vi.stubGlobal('fetch', spy);
  return spy;
}

function renderOverview(children = <SpendOverview />) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={client}>{children}</QueryClientProvider>);
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('SpendOverview — populated', () => {
  it('renders exactly five tiles with a compact value, a delta and a 12-point sparkline', async () => {
    stubFetch({ overview: overviewPayload() });
    renderOverview();

    expect(await screen.findByText('$5.52')).toBeTruthy();
    expect(screen.getByText('3K')).toBeTruthy();
    expect(screen.getByText('52.1M')).toBeTruthy();
    expect(screen.getByText('51.5%')).toBeTruthy();
    expect(screen.getByText('$0.11')).toBeTruthy();

    const polylines = document.querySelectorAll('polyline');
    expect(polylines.length).toBe(5);
    for (const p of polylines) {
      expect(p.getAttribute('points')?.split(' ').length).toBe(12);
    }
  });

  it('colours total spend delta muted and cache hit rate delta success, both up 20%', async () => {
    stubFetch({ overview: overviewPayload() });
    renderOverview();

    await screen.findByText('$5.52');
    // Three deltas share this exact text (total_usage, request_count and tokens_total all
    // have delta 20 in the fixture) — assert at least one muted instance exists.
    const mutedDeltas = screen
      .getAllByText(/vs prev period/)
      .filter((el) => el.className.includes('text-text-muted'));
    expect(mutedDeltas.length).toBeGreaterThan(0);

    const successDeltas = screen
      .getAllByText(/vs prev period/)
      .filter((el) => el.className.includes('text-success'));
    expect(successDeltas.length).toBe(1);
  });

  it('colours blended $/1M delta warning when rising (cost-per-token rising is the one worth flagging)', async () => {
    stubFetch({
      overview: overviewPayload({
        kpis: fiveTiles({ blended_cost_per_million_tokens: { value: 0.11, delta: 15 } }),
      }),
    });
    renderOverview();

    await screen.findByText('$5.52');
    const warningDeltas = screen
      .getAllByText(/vs prev period/)
      .filter((el) => el.className.includes('text-warning'));
    expect(warningDeltas.length).toBe(1);
  });

  it('renders the ranked list with a role badge, masked label, and Lifetime spend', async () => {
    stubFetch({ overview: overviewPayload() });
    renderOverview();

    expect(await screen.findByText('alice@example.com')).toBeTruthy();
    expect(screen.getByText('sk-or-v1-caa...61c')).toBeTruthy();
    expect(screen.getByText('Member')).toBeTruthy();
    expect(screen.getByText('See full roster below')).toBeTruthy();
  });

  it('renders the over-allocation banner when the server says so, and nothing when it does not', async () => {
    stubFetch({
      overview: overviewPayload({
        overAllocation: { triggered: true, sum_caps: 25, available: 20 },
      }),
    });
    renderOverview();
    expect(
      await screen.findByText(/Assigned caps total more than this account's available/),
    ).toBeTruthy();
  });

  it('does not render the over-allocation banner when not triggered', async () => {
    stubFetch({
      overview: overviewPayload({
        overAllocation: { triggered: false, sum_caps: 5, available: 20 },
      }),
    });
    renderOverview();
    await screen.findByText('$5.52');
    expect(screen.queryByText(/Assigned caps total more than/)).toBeNull();
  });

  // Keys with no limit (an admin's own) add nothing to the sum of caps yet draw on the same
  // credit, so the banner says how many there are.
  it('reports the keys with no limit inside the over-allocation banner', async () => {
    stubFetch({
      overview: {
        ...overviewPayload(),
        over_allocation: { triggered: true, sum_caps: 25, available: 20, uncapped_keys: 1 },
      },
    });
    renderOverview();
    expect(
      await screen.findByText(/Keys with no limit, which draw on the same credit: 1\./),
    ).toBeTruthy();
  });
});

describe('SpendOverview — empty', () => {
  it('renders the empty copy and no $0.00 tiles when the account has no billed requests', async () => {
    stubFetch({
      overview: overviewPayload({
        kpis: fiveTiles({
          total_usage: { value: 0, delta: null },
          request_count: { value: 0, delta: null },
          tokens_total: { value: 0, delta: null },
          cache_hit_rate: { value: 0, delta: null },
          blended_cost_per_million_tokens: { value: 0, delta: null },
        }),
        topIdentities: [],
      }),
    });
    renderOverview();

    expect(
      await screen.findByText("No spend yet — this account hasn't made a billed request."),
    ).toBeTruthy();
    expect(screen.queryAllByText('$0.00')).toHaveLength(0);
  });
});

describe('SpendOverview — loading and error guards', () => {
  it('renders the shared spinner guard while the overview read is in flight', () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => new Promise(() => undefined)),
    );
    renderOverview();
    expect(screen.getByRole('status').textContent).toContain('Loading spend overview');
  });

  it('renders the destructive alert on failure, and a roster row in the same tree is unaffected', async () => {
    stubFetch({ overview: {}, overviewStatus: 502 });
    renderOverview(
      <>
        <SpendOverview />
        <IdentityRoster />
      </>,
    );

    expect(
      await screen.findByText("Couldn't load the spend overview. Try refreshing."),
    ).toBeTruthy();
    // Substring match: the roster's own row appends " (you)" to the caller's identity, so
    // its exact textContent is "admin@aura.local (you)", not the bare email.
    expect(await screen.findByText(/admin@aura\.local/)).toBeTruthy();
  });
});

describe('SpendOverview — scope fence', () => {
  it('renders no tab bar and no inert Trends/Explore/Guardrails chrome', async () => {
    stubFetch({ overview: overviewPayload() });
    renderOverview();
    await screen.findByText('$5.52');
    expect(screen.queryByRole('tablist')).toBeNull();
    expect(screen.queryByText(/Trends/)).toBeNull();
    expect(screen.queryByText(/Explore/)).toBeNull();
    expect(screen.queryByText(/Guardrails/)).toBeNull();
  });
});
