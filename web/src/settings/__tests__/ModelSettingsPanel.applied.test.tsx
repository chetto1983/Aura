import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactElement } from 'react';
import '../../i18n/i18n';
import { ModelSettingsPanel } from '../ModelSettingsPanel';

// Amendment #188: every field reports how it reaches the running daemon, the banner
// names the rows waiting for a restart, and the turn budget is saved through the
// hot-profile batch like the route it belongs to.

function renderPanel(
  panel: ReactElement,
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } }),
) {
  return render(<QueryClientProvider client={client}>{panel}</QueryClientProvider>);
}

interface RawItem {
  readonly key: string;
  readonly kind?: 'string' | 'bool' | 'int';
  readonly value?: string;
  readonly overridden?: boolean;
  readonly applied?: 'live' | 'boot' | 'restart';
}

function item(raw: RawItem) {
  return {
    key: raw.key,
    label: raw.key,
    kind: raw.kind ?? 'string',
    secret: false,
    value: raw.value ?? '',
    has_value: (raw.value ?? '') !== '',
    overridden: raw.overridden ?? false,
    applied: raw.applied ?? 'boot',
  };
}

const LIST = {
  restart_required: true,
  restart_keys: ['AURA_VISION_CLOUD'],
  settings: [
    item({ key: 'AURA_LOOP_MAX_STEPS', kind: 'int', value: '25', applied: 'live' }),
    item({ key: 'AURA_LOOP_MAX_WALLCLOCK_SEC', kind: 'int', value: '300', applied: 'live' }),
    item({
      key: 'AURA_VISION_CLOUD',
      kind: 'bool',
      value: 'true',
      overridden: true,
      applied: 'restart',
    }),
    item({ key: 'AURA_EMBED_DIMENSIONS', kind: 'int', value: '768', applied: 'boot' }),
  ],
};

function jsonResponse(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

function urlOf(input: RequestInfo | URL): string {
  return typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
}

function stubFetch() {
  const calls: { readonly method: string; readonly url: string; readonly body: unknown }[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const method = init?.method ?? 'GET';
      const body = typeof init?.body === 'string' ? (JSON.parse(init.body) as unknown) : undefined;
      calls.push({ method, url: urlOf(input), body });
      if (method === 'PUT')
        return Promise.resolve(jsonResponse({ updated: 1, restart_required: false }));
      return Promise.resolve(jsonResponse(LIST));
    }),
  );
  return calls;
}

describe('ModelSettingsPanel — application state and turn budget', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('labels each field with how it applies and names the restart-bound rows in the banner', async () => {
    stubFetch();
    renderPanel(<ModelSettingsPanel />);
    await screen.findByRole('heading', { name: 'Token and turn budget' });

    const stepsField = screen.getByLabelText('Max steps per turn').closest('div.flex.min-h-32');
    expect(stepsField?.querySelector('[data-applied="live"]')?.textContent).toBe(
      'Applies immediately',
    );
    const visionField = screen.getByLabelText('Vision uses cloud').closest('div.flex.min-h-32');
    expect(visionField?.querySelector('[data-applied="restart"]')?.textContent).toBe(
      'Saved — needs a restart',
    );
    const embedField = screen.getByLabelText('Embedding dimensions').closest('div.flex.min-h-32');
    expect(embedField?.querySelector('[data-applied="boot"]')?.textContent).toBe(
      'Applied at start-up',
    );
    expect(screen.getByRole('note').textContent).toContain(
      'Restart Aura to apply: AURA_VISION_CLOUD',
    );
  });

  it('saves the turn budget through the hot-profile batch, not the boot-bound single-key route', async () => {
    const calls = stubFetch();
    renderPanel(<ModelSettingsPanel groups={['tokens']} />);
    const steps = await screen.findByLabelText('Max steps per turn');
    fireEvent.change(steps, { target: { value: '60' } });
    fireEvent.change(screen.getByLabelText('Max seconds per turn'), { target: { value: '1200' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save runtime settings' }));

    await waitFor(() => {
      expect(calls.some((c) => c.method === 'PUT')).toBe(true);
    });
    const puts = calls.filter((c) => c.method === 'PUT');
    expect(puts).toHaveLength(1);
    expect(puts[0]?.url).toBe('/api/settings/llm-profile');
    expect(puts[0]?.body).toEqual({
      settings: { AURA_LOOP_MAX_STEPS: '60', AURA_LOOP_MAX_WALLCLOCK_SEC: '1200' },
    });
  });
});
