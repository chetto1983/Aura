import type { ReactElement } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import { ModelSettingsPanel } from '../ModelSettingsPanel';

// What a save tells whoever asked for it: every reconciler run its writes triggered, in write
// order, so the first-run route step can show the keys minted and what OpenRouter refused. And the
// one control that step needs from the pane: no Skip while the route is required.

const ROUTING_LIST = {
  restart_required: false,
  settings: [
    { key: 'AURA_LLM_MODEL', value: 'deepseek/deepseek-v4-flash:nitro', secret: false },
    { key: 'AURA_LLM_BASE_URL', value: 'https://openrouter.ai/api/v1', secret: false },
  ].map((row) => ({ ...row, label: row.key, kind: 'string', has_value: true, overridden: true })),
};

// The cap's PUT runs the reconciler before any management key is stored, so it only waits.
const WAITING = { skipped: 'management_key_unset', identities_minted: [], limits_aligned: [] };
const MINTED = {
  services_label: 'sk-or-v1-srv...ce1',
  identities_minted: ['id-admin'],
  minted_labels: { 'id-admin': 'sk-or-v1-adm...in1' },
  limits_aligned: [],
};

function stubSettingsAPI() {
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
      const payload =
        init?.method === 'PUT'
          ? {
              key: url.split('/').pop(),
              openrouter_keys: url.endsWith('/AURA_OPENROUTER_MANAGEMENT_KEY') ? MINTED : WAITING,
            }
          : ROUTING_LIST;
      return Promise.resolve(
        new Response(JSON.stringify(payload), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      );
    }),
  );
}

function mount(panel: ReactElement) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}>{panel}</QueryClientProvider>);
  return screen.findByRole('heading', { name: 'Model routing' });
}

describe('ModelSettingsPanel save outcome', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('hands onComplete every reconciler run the save triggered, in write order', async () => {
    stubSettingsAPI();
    const onComplete = vi.fn();
    await mount(<ModelSettingsPanel groups={['routing']} onComplete={onComplete} />);

    fireEvent.change(screen.getByLabelText('Services key monthly cap (USD)'), {
      target: { value: '10' },
    });
    fireEvent.change(screen.getByLabelText('OpenRouter management key'), {
      target: { value: 'sk-or-v1-mgmt' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Save runtime settings' }));

    await waitFor(() => {
      expect(onComplete).toHaveBeenCalledTimes(1);
    });
    expect(onComplete).toHaveBeenCalledWith({ openRouterKeys: [WAITING, MINTED] });
  });

  it('hands an empty outcome when nothing changed', async () => {
    stubSettingsAPI();
    const onComplete = vi.fn();
    await mount(<ModelSettingsPanel groups={['routing']} onComplete={onComplete} />);

    fireEvent.click(screen.getByRole('button', { name: 'Continue' }));

    await waitFor(() => {
      expect(onComplete).toHaveBeenCalledWith({ openRouterKeys: [] });
    });
  });

  it('hides Skip while the step may not be skipped', async () => {
    stubSettingsAPI();
    await mount(<ModelSettingsPanel groups={['routing']} onComplete={vi.fn()} skippable={false} />);

    expect(screen.queryByRole('button', { name: 'Skip runtime setup' })).toBeNull();
    expect(screen.getByRole('button', { name: 'Continue' })).toBeTruthy();
  });
});
