import { afterEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import '../i18n/i18n'; // side-effect: initialize the i18next singleton so t() resolves keys
import type { UseRuntimeHealthResult } from '../health/useRuntimeHealth';

// Mock the hook so the panel's status->tone mapping + formatLastChecked can be driven
// deterministically (the live-fetch path is covered separately in RuntimeHealthPanel.test).
const { mockHealth } = vi.hoisted(() => ({ mockHealth: vi.fn<() => UseRuntimeHealthResult>() }));
vi.mock('../health/useRuntimeHealth', async (orig) => {
  const actual = await orig<typeof import('../health/useRuntimeHealth')>();
  return { ...actual, useRuntimeHealth: () => mockHealth() };
});

const { RuntimeHealthPanel } = await import('../health/RuntimeHealthPanel');

const base: UseRuntimeHealthResult = {
  healthz: {
    status: 200,
    body: { ok: true, bind_address: '127.0.0.1:9080', build_version: 'dev' },
  },
  readyz: { status: 200, body: { ready: true, deps: { postgres: 'ok', neo4j: 'ok' } } },
  healthzError: false,
  readyzError: false,
  isPending: false,
  isRefetching: false,
  lastChecked: 0,
};

function set(overrides: Partial<UseRuntimeHealthResult>) {
  mockHealth.mockReturnValue({ ...base, ...overrides });
}

// The dot is aria-hidden and colour-only, so its bg-* class is the only carrier of the
// mutated tone — assert it per row to kill tone mutations.
function dotClass(label: string): string {
  const row = screen.getByText(label).parentElement;
  const dot = row?.querySelector('[aria-hidden="true"]');
  return dot?.className ?? '';
}

function statusOf(label: string): string {
  const row = screen.getByText(label).parentElement;
  // The status text is the last span in the value group.
  const spans = row?.querySelectorAll('span') ?? [];
  return spans[spans.length - 1]?.textContent ?? '';
}

describe('RuntimeHealthPanel rendering', () => {
  afterEach(() => {
    vi.useRealTimers();
    mockHealth.mockReset();
  });

  it('shows the loading caption while pending', () => {
    set({ isPending: true });
    render(<RuntimeHealthPanel />);
    expect(screen.getByText('Checking runtime...')).toBeTruthy();
    expect(screen.getByRole('status').textContent).toContain('Checking runtime...');
    expect(screen.queryByText('Liveness')).toBeNull();
  });

  it('keeps loaded rows stable during a background refetch', () => {
    set({ isRefetching: true });
    const { container } = render(<RuntimeHealthPanel />);
    const region = screen.getByRole('region', { name: 'Runtime' });
    expect(region.getAttribute('aria-busy')).toBe('true');
    expect(screen.getByText('Liveness')).toBeTruthy();
    expect(container.querySelector('.skeleton-refetch-bar')).toBeTruthy();
  });

  it('shows a role=alert when every probe is unreachable', () => {
    set({ healthz: undefined, readyz: undefined, healthzError: true, readyzError: true });
    render(<RuntimeHealthPanel />);
    expect(screen.getByRole('alert').textContent).toContain("Can't read runtime status");
  });

  it('maps a healthy stack to success dots + the right status text', () => {
    set({});
    render(<RuntimeHealthPanel />);
    for (const label of ['Liveness', 'Readiness', 'Postgres', 'Neo4j']) {
      expect(dotClass(label)).toContain('bg-success');
    }
    expect(statusOf('Liveness')).toBe('Live');
    expect(statusOf('Readiness')).toBe('Ready');
    expect(statusOf('Postgres')).toBe('Ready');
    expect(statusOf('Bind address')).toBe('127.0.0.1:9080');
    expect(statusOf('Build')).toBe('dev');
  });

  it('maps a not-ok /healthz to a danger Unavailable liveness', () => {
    set({ healthz: { status: 503, body: { ok: false } } });
    render(<RuntimeHealthPanel />);
    expect(statusOf('Liveness')).toBe('Unavailable');
    expect(dotClass('Liveness')).toContain('bg-danger');
  });

  it('maps a not-ready /readyz to a warning Degraded readiness', () => {
    set({ readyz: { status: 503, body: { ready: false, deps: { postgres: 'ok', neo4j: 'ok' } } } });
    render(<RuntimeHealthPanel />);
    expect(statusOf('Readiness')).toBe('Degraded');
    expect(dotClass('Readiness')).toContain('bg-warning');
  });

  it('maps a failing dependency to Degraded and a missing one to Unknown', () => {
    set({
      readyz: { status: 503, body: { ready: false, deps: { postgres: 'dial timeout' } } },
    });
    render(<RuntimeHealthPanel />);
    expect(statusOf('Postgres')).toBe('Degraded');
    expect(dotClass('Postgres')).toContain('bg-warning');
    // neo4j absent from deps → Unknown.
    expect(statusOf('Neo4j')).toBe('Unknown');
    expect(dotClass('Neo4j')).toContain('bg-warning');
  });

  it('renders an em-dash when bind/build are absent from /healthz', () => {
    set({ healthz: { status: 200, body: { ok: true } } });
    render(<RuntimeHealthPanel />);
    expect(statusOf('Bind address')).toBe('-');
    expect(statusOf('Build')).toBe('-');
  });

  it('formats the Last checked caption across the relative-time buckets', () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-06-16T12:00:00Z'));
    const now = Date.now();

    set({ lastChecked: 0 });
    const a = render(<RuntimeHealthPanel />);
    expect(screen.getByText('Last checked never')).toBeTruthy();
    a.unmount();

    set({ lastChecked: now - 2000 });
    const b = render(<RuntimeHealthPanel />);
    expect(screen.getByText('Last checked just now')).toBeTruthy();
    b.unmount();

    set({ lastChecked: now - 30000 });
    const c = render(<RuntimeHealthPanel />);
    expect(screen.getByText('Last checked 30s ago')).toBeTruthy();
    c.unmount();

    set({ lastChecked: now - 120000 });
    render(<RuntimeHealthPanel />);
    expect(screen.getByText('Last checked 2m ago')).toBeTruthy();
  });
});
