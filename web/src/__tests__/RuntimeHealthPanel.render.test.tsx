import { afterEach, describe, expect, it, vi } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import i18n from '../i18n/i18n'; // side-effect: initialize the i18next singleton so t() resolves keys
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
    body: {
      ok: true,
      bind_address: '127.0.0.1:9080',
      build_version: 'dev',
      build_commit: 'e94b2511fdd6f2ffd7b245281a5cf5c1638d948e',
      build_date: '2026-09-21T14:19:00Z',
    },
  },
  readyz: { status: 200, body: { status: 'ready', ready: true, reasons: [] } },
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
    for (const label of ['Liveness', 'Readiness']) {
      expect(dotClass(label)).toContain('bg-success');
    }
    expect(statusOf('Liveness')).toBe('Live');
    expect(statusOf('Readiness')).toBe('Ready');
    expect(screen.queryByRole('list', { name: 'Why' })).toBeNull();
    expect(statusOf('Bind address')).toBe('127.0.0.1:9080');
    expect(statusOf('Build')).toBe('dev');
    expect(statusOf('Commit')).toBe('e94b2511fdd6');
    expect(screen.getByText('e94b2511fdd6').getAttribute('title')).toBe(
      'e94b2511fdd6f2ffd7b245281a5cf5c1638d948e',
    );
    expect(statusOf('Built')).toBe('2026-09-21T14:19:00Z');
  });

  it('maps a not-ok /healthz to a danger Unavailable liveness', () => {
    set({ healthz: { status: 503, body: { ok: false } } });
    render(<RuntimeHealthPanel />);
    expect(statusOf('Liveness')).toBe('Unavailable');
    expect(dotClass('Liveness')).toContain('bg-danger');
  });

  it('maps a not-ready /readyz to a warning Degraded readiness', () => {
    set({
      readyz: {
        status: 503,
        body: { status: 'unavailable', ready: false, reasons: ['memory_unavailable'] },
      },
    });
    render(<RuntimeHealthPanel />);
    expect(statusOf('Readiness')).toBe('Degraded');
    expect(dotClass('Readiness')).toContain('bg-warning');
  });

  it('says why, one line per reason the daemon reports', () => {
    set({
      readyz: {
        status: 503,
        body: {
          status: 'unavailable',
          ready: false,
          reasons: ['memory_unavailable', 'scheduler_stalled'],
        },
      },
    });
    render(<RuntimeHealthPanel />);
    const why = screen.getByRole('list', { name: 'Why' });
    const items = within(why).getAllByRole('listitem');
    expect(items).toHaveLength(2);
    expect(items[0]?.textContent).toContain('Memory is not answering');
    expect(items[0]?.textContent).toContain("can't read its long-term memory");
    expect(items[1]?.textContent).toContain('Scheduler has stalled');
    expect(screen.getByText("The technical detail is in Aura's logs.")).toBeTruthy();
  });

  it('describes every code the daemon can report, in both languages', () => {
    const codes = [
      'postgres_unavailable',
      'memory_unavailable',
      'sandbox_unavailable',
      'listener_unavailable',
      'migration_incompatible',
      'scheduler_stalled',
      'draining',
      'dependency_unavailable',
    ];
    for (const language of ['en', 'it']) {
      for (const code of codes) {
        expect(i18n.exists(`health.reasons.${code}.title`, { lng: language })).toBe(true);
        expect(i18n.exists(`health.reasons.${code}.detail`, { lng: language })).toBe(true);
      }
    }
  });

  it('keeps a code this build cannot describe readable', () => {
    set({
      readyz: {
        status: 503,
        body: { status: 'unavailable', ready: false, reasons: ['flux_capacitor_cold'] },
      },
    });
    render(<RuntimeHealthPanel />);
    const item = within(screen.getByRole('list', { name: 'Why' })).getByRole('listitem');
    expect(item.textContent).toContain('flux_capacitor_cold');
    expect(item.textContent).toContain("this version can't describe");
  });

  it('renders an em-dash for absent bind/build and omits absent provenance', () => {
    set({ healthz: { status: 200, body: { ok: true } } });
    render(<RuntimeHealthPanel />);
    expect(statusOf('Bind address')).toBe('-');
    expect(statusOf('Build')).toBe('-');
    expect(screen.queryByText('Commit')).toBeNull();
    expect(screen.queryByText('Built')).toBeNull();
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
