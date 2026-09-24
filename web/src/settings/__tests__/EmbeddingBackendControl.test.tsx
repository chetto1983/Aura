import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { useState } from 'react';
import '../../i18n/i18n';
import { EmbeddingBackendControl } from '../EmbeddingBackendControl';
import type { LoadedState } from '../modelSettingsState';
import type { PickerBinding } from '../SettingField';

// The restart itself is restartAura's to prove (restartAura.test.ts); here only its outcome.
let restartTimeout: (() => void) | undefined;
vi.mock('../restartAura', () => ({
  browserRestartDeps: {},
  requestRestart: vi.fn(),
  watchRestart: vi.fn((_deps: unknown, onTimeout: () => void) => {
    restartTimeout = onTimeout;
    return () => undefined;
  }),
}));

const SAVED = {
  AURA_EMBED_BASE_URL: 'http://aura-llama-embed:8081',
  AURA_EMBED_MODEL: '',
  AURA_EMBED_CLOUD_BASE_URL: '',
};

const picker: PickerBinding = {
  catalog: { models: [], status: 'ready', error: undefined, reload: () => undefined },
  formatRow: (row) => row.id,
};

function Harness() {
  const [values, setValues] = useState<Record<string, string>>({ ...SAVED });
  const loaded: LoadedState = {
    rows: {},
    values,
    initial: { ...SAVED },
    restartRequired: false,
    restartKeys: [],
    restartSupported: true,
  };
  return (
    <EmbeddingBackendControl
      loaded={loaded}
      onValueChange={(key, value) => {
        setValues((prev) => ({ ...prev, [key]: value }));
      }}
      modelPicker={picker}
      openRouterAvailable
    />
  );
}

const PREVIEW = {
  space: 'es1-local-new',
  space_label: 'local other.gguf, 768d, recipe 1',
  memory_space: 'es1-local-new',
  native_width: 768,
  dimensions: 768,
  width_warning: false,
  chars_per_second: 400,
  input_limit: 2048,
  work: { types: [{ type: 'FACT', rows: 3, chars: 300 }], passages_over_limit: 0 },
  tokens: 100,
  cost_usd: 0,
  local: true,
  duration_seconds: 1,
  floors_calibrated: true,
  refusals: [],
};

function stubRoute(apply: { status: number; body: unknown }, previewStatus = 200) {
  vi.stubGlobal(
    'fetch',
    vi.fn((url: string) => {
      if (url.endsWith('/preview')) {
        return Promise.resolve(
          new Response(
            JSON.stringify(previewStatus === 200 ? PREVIEW : { error: 'probe exploded' }),
            {
              status: previewStatus,
            },
          ),
        );
      }
      if (url === '/api/settings/embedding-route') {
        return Promise.resolve(new Response(JSON.stringify(apply.body), { status: apply.status }));
      }
      return Promise.resolve(new Response('{}', { status: 200 }));
    }),
  );
}

function renderControl() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <Harness />
    </QueryClientProvider>,
  );
}

async function previewANewLocalBase() {
  fireEvent.change(screen.getByLabelText('Embedding base URL'), {
    target: { value: 'http://other-embed:8081' },
  });
  fireEvent.click(screen.getByRole('button', { name: 'Preview change' }));
  return screen.findByRole('region', { name: 'What this change does' });
}

function confirmAndApply(card: HTMLElement) {
  fireEvent.click(within(card).getByRole('checkbox'));
  fireEvent.click(within(card).getByRole('button', { name: 'Apply and restart' }));
}

describe('EmbeddingBackendControl route change', () => {
  beforeEach(() => {
    restartTimeout = undefined;
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('offers no preview while the route is the saved one', () => {
    stubRoute({ status: 200, body: {} });
    renderControl();
    expect(screen.queryByRole('button', { name: 'Preview change' })).toBeNull();
  });

  it('follows the daemon while it restarts, and says so when it does not come back', async () => {
    stubRoute({
      status: 200,
      body: { space: 'es1-local-new', restarting: true, restart_required: false },
    });
    renderControl();
    confirmAndApply(await previewANewLocalBase());
    expect(
      await screen.findByText(
        'Aura is restarting on the new route. This page reloads when it is back.',
      ),
    ).toBeTruthy();
    expect(screen.queryByRole('region', { name: 'What this change does' })).toBeNull();
    await waitFor(() => {
      expect(restartTimeout).toBeDefined();
    });
    restartTimeout?.();
    expect(
      await screen.findByText('Aura has not come back yet. Reload the page in a moment.'),
    ).toBeTruthy();
  });

  // A 409 means the preview on screen names a space the daemon no longer computes: the card
  // goes, so its tick cannot be re-sent, and the operator is told to preview again.
  it('drops a preview whose space moved before the apply, and says to preview again', async () => {
    stubRoute({ status: 409, body: { error: 'space_changed', space: 'es1-other' } });
    renderControl();
    confirmAndApply(await previewANewLocalBase());
    expect((await screen.findByRole('alert')).textContent).toBe(
      'The route’s space changed since the preview. Preview it again.',
    );
    expect(screen.queryByRole('region', { name: 'What this change does' })).toBeNull();
    expect(screen.getByRole('button', { name: 'Preview change' }).hasAttribute('disabled')).toBe(
      false,
    );
  });

  it('says why the apply’s own probe refused the route', async () => {
    stubRoute({
      status: 422,
      body: { error: 'route_refused', refusals: [{ code: 'key_missing' }] },
    });
    renderControl();
    confirmAndApply(await previewANewLocalBase());
    expect((await screen.findByRole('alert')).textContent).toBe(
      'The change was not applied: This cloud route needs an OpenRouter key, and none is configured.',
    );
    expect(screen.queryByRole('region', { name: 'What this change does' })).toBeNull();
  });

  it('asks for the confirmation again on every new preview', async () => {
    stubRoute({ status: 200, body: {} });
    renderControl();
    fireEvent.click(within(await previewANewLocalBase()).getByRole('checkbox'));
    fireEvent.click(screen.getByRole('button', { name: 'Preview change' }));
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Preview change' }).hasAttribute('disabled')).toBe(
        false,
      );
    });
    const card = await screen.findByRole('region', { name: 'What this change does' });
    expect(within(card).getByRole('checkbox')).toHaveProperty('checked', false);
  });

  it('shows a failed preview', async () => {
    stubRoute({ status: 200, body: {} }, 502);
    renderControl();
    fireEvent.change(screen.getByLabelText('Embedding base URL'), {
      target: { value: 'http://other-embed:8081' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Preview change' }));
    expect((await screen.findByRole('alert')).textContent).toBe(
      'The preview failed: probe exploded',
    );
  });

  // A preview describes one route: an edit after it must not carry its confirmation over.
  it('drops a preview once the route is edited again', async () => {
    stubRoute({ status: 200, body: {} });
    renderControl();
    await previewANewLocalBase();
    fireEvent.change(screen.getByLabelText('Embedding base URL'), {
      target: { value: 'http://third-embed:8081' },
    });
    expect(screen.queryByRole('region', { name: 'What this change does' })).toBeNull();
  });
});
