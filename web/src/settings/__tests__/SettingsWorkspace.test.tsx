import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import SettingsWorkspace from '../SettingsWorkspace';

// The admin sub-panels have their own dedicated tests + own network calls; mock them here so
// this suite focuses on the SettingsWorkspace capability gate + structure.
vi.mock('../CapabilityAdminPanel', () => ({
  CapabilityAdminPanel: () => <div>Capabilities panel</div>,
}));
vi.mock('../../audit/AdminAuditView', () => ({
  AdminAuditView: () => <div>Activity view</div>,
}));

function stubFetch(capabilities: readonly string[]) {
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
      if (url.includes('/api/me')) {
        return Promise.resolve(
          new Response(JSON.stringify({ identity_id: 'local', capabilities }), { status: 200 }),
        );
      }
      return Promise.resolve(
        new Response(JSON.stringify({ restart_required: false, settings: [] }), { status: 200 }),
      );
    }),
  );
}

function renderWorkspace(onCreateIdentity = vi.fn()) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <SettingsWorkspace onCreateIdentity={onCreateIdentity} />
    </QueryClientProvider>,
  );
}

describe('SettingsWorkspace', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('mounts the runtime model settings workspace for an admin', async () => {
    stubFetch(['*']);
    renderWorkspace();

    expect(await screen.findByRole('heading', { name: 'Runtime settings' })).toBeTruthy();
    expect(screen.getByRole('heading', { name: 'Model routing' })).toBeTruthy();
  });

  it('exposes identity creation as a settings action for an admin', async () => {
    const onCreateIdentity = vi.fn();
    stubFetch(['*']);
    renderWorkspace(onCreateIdentity);

    fireEvent.click(await screen.findByRole('button', { name: 'Create identity' }));

    expect(onCreateIdentity).toHaveBeenCalledOnce();
  });

  it('hides the admin controls behind a not-authorized fallback for a non-admin', async () => {
    stubFetch(['agent.run']);
    renderWorkspace();

    expect(await screen.findByRole('heading', { name: 'Admin access required' })).toBeTruthy();
    // The model settings + admin panels must NOT render for a non-admin identity (D-03).
    expect(screen.queryByRole('heading', { name: 'Runtime settings' })).toBeNull();
    expect(screen.queryByText('Capabilities panel')).toBeNull();
    expect(screen.queryByText('Activity view')).toBeNull();
  });
});
