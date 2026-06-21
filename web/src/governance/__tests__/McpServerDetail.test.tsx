import type { ReactNode } from 'react';
import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import type { McpProbeResult, McpServerRow } from '../governanceApi';

// The WhatsApp "Link device" section drives its own network polling (governanceApi); the detail
// test cares only about the PRESENCE/ABSENCE branch (isWhatsAppServer is the real heuristic from
// governanceApi), so the section component is mocked to a sentinel. Its own status-state behavior
// is covered in WhatsAppConnect.test.tsx.
vi.mock('../WhatsAppConnect', () => ({
  WhatsAppConnect: () => <div data-testid="whatsapp-connect-section" />,
}));

const { McpServerDetail } = await import('../McpServerDetail');

// McpServerDetail test — renders the detail component directly with props so every field
// label/value, the redacted-chip branch, the probe-loading/resolved/error branches, and the
// lastError branch are asserted (mutation hardening: the rendered copy is the contract). The
// detail now embeds the Phase-29 lifecycle cluster + env-edit form (both use TanStack
// mutations), so it is rendered inside a QueryClientProvider.

function client() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
}

function Providers({ children }: { children: ReactNode }) {
  return <QueryClientProvider client={client()}>{children}</QueryClientProvider>;
}

const SERVER: McpServerRow = {
  name: 'github',
  source: 'recipe:github',
  trust: 'trusted',
  riskPolicy: 'trusted_recipe',
  runtime: 'stdio',
  startupState: 'configured',
  authStatus: 'ok',
  profiles: ['work'],
  networkAllowlist: ['api.github.com'],
  envKeys: [
    { key: 'GITHUB_TOKEN', redacted: true },
    { key: 'GITHUB_ORG', redacted: false },
  ],
};

const HEALTHY: McpProbeResult = { name: 'github', ok: true, tool_count: 7, detail: 'ok (7 tools)' };

describe('McpServerDetail', () => {
  it('renders every static field label and value', () => {
    render(
      <McpServerDetail
        server={SERVER}
        probe={HEALTHY}
        probeLoading={false}
        onClose={() => undefined}
      />,
      { wrapper: Providers },
    );

    expect(screen.getByText('Trust')).toBeTruthy();
    expect(screen.getByText('trusted')).toBeTruthy();
    expect(screen.getByText('Source')).toBeTruthy();
    expect(screen.getByText('recipe:github')).toBeTruthy();
    expect(screen.getByText('Risk policy')).toBeTruthy();
    expect(screen.getByText('trusted_recipe')).toBeTruthy();
    expect(screen.getByText('Profiles')).toBeTruthy();
    expect(screen.getByText('work')).toBeTruthy();
    expect(screen.getByText('Network allowlist')).toBeTruthy();
    expect(screen.getByText('api.github.com')).toBeTruthy();
    expect(screen.getByText('Runtime')).toBeTruthy();
    expect(screen.getByText('stdio')).toBeTruthy();
    expect(screen.getByText('Startup')).toBeTruthy();
    expect(screen.getByText('configured')).toBeTruthy();
    expect(screen.getByText('Auth status')).toBeTruthy();
    expect(screen.getByText('ok')).toBeTruthy();
  });

  it('renders a redacted chip ONLY for a redacted key (the non-redacted key has no chip suffix)', () => {
    render(
      <McpServerDetail
        server={SERVER}
        probe={HEALTHY}
        probeLoading={false}
        onClose={() => undefined}
      />,
      { wrapper: Providers },
    );

    expect(screen.getByText('GITHUB_TOKEN')).toBeTruthy();
    expect(screen.getByText('GITHUB_ORG')).toBeTruthy();
    // Exactly one redacted suffix (only GITHUB_TOKEN is redacted).
    expect(screen.getAllByText(/redacted/)).toHaveLength(1);
  });

  it('renders the healthy probe tool count + detail in the success tone', () => {
    render(
      <McpServerDetail
        server={SERVER}
        probe={HEALTHY}
        probeLoading={false}
        onClose={() => undefined}
      />,
      { wrapper: Providers },
    );
    expect(screen.getByText('7')).toBeTruthy();
    const detail = screen.getByText('ok (7 tools)');
    expect(detail).toBeTruthy();
    // The healthy detail uses the success tone (kills the ok ? success : danger ternary mutant).
    expect(detail.className).toContain('text-success');
  });

  it('renders a failed probe detail in the danger tone', () => {
    const failed: McpProbeResult = {
      name: 'github',
      ok: false,
      tool_count: 0,
      detail: 'dial failed',
    };
    render(
      <McpServerDetail
        server={SERVER}
        probe={failed}
        probeLoading={false}
        onClose={() => undefined}
      />,
      { wrapper: Providers },
    );
    const detail = screen.getByText('dial failed');
    expect(detail.className).toContain('text-danger');
  });

  it('renders Checking… while the probe is loading', () => {
    render(
      <McpServerDetail
        server={SERVER}
        probe={undefined}
        probeLoading={true}
        onClose={() => undefined}
      />,
      { wrapper: Providers },
    );
    expect(screen.getByText('Checking…')).toBeTruthy();
  });

  it('renders the probe err when the probe failed', () => {
    const failed: McpProbeResult = {
      name: 'github',
      ok: false,
      tool_count: 0,
      detail: 'dial failed',
      err: 'connection refused',
    };
    render(
      <McpServerDetail
        server={SERVER}
        probe={failed}
        probeLoading={false}
        onClose={() => undefined}
      />,
      { wrapper: Providers },
    );
    expect(screen.getByText('dial failed')).toBeTruthy();
    expect(screen.getByText('connection refused')).toBeTruthy();
  });

  it('renders "no env keys" + the static lastError when present, and fires onClose', () => {
    const bare: McpServerRow = {
      name: 'fs',
      source: '',
      trust: 'trusted',
      riskPolicy: '',
      runtime: 'stdio',
      startupState: 'configured',
      authStatus: 'ok',
      profiles: [],
      networkAllowlist: [],
      envKeys: [],
      lastError: 'spawn failed',
    };
    const onClose = vi.fn();
    render(
      <McpServerDetail server={bare} probe={undefined} probeLoading={false} onClose={onClose} />,
      { wrapper: Providers },
    );

    expect(screen.getByText('No environment keys.')).toBeTruthy();
    expect(screen.getByText('spawn failed')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Close' }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('toggles the inline env-edit form open and closed (the Edit environment affordance)', () => {
    render(
      <McpServerDetail
        server={SERVER}
        probe={HEALTHY}
        probeLoading={false}
        onClose={() => undefined}
      />,
      { wrapper: Providers },
    );

    // The read view shows the env-keys section + the Edit affordance.
    expect(screen.getByRole('button', { name: 'Edit environment' })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Edit environment' }));

    // The inline McpEnvEditForm renders (its Environment heading + the Save changes control).
    expect(screen.getByText('Environment')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Save changes' })).toBeTruthy();

    // Discarding returns to the read view (the Edit affordance is back).
    fireEvent.click(screen.getByRole('button', { name: /discard|cancel/i }));
    expect(screen.getByRole('button', { name: 'Edit environment' })).toBeTruthy();
  });

  it('renders the WhatsApp connect section for the whatsapp server', () => {
    const whatsapp: McpServerRow = {
      ...SERVER,
      name: 'whatsapp',
      source: 'recipe:whatsapp',
    };
    render(
      <McpServerDetail
        server={whatsapp}
        probe={HEALTHY}
        probeLoading={false}
        onClose={() => undefined}
      />,
      { wrapper: Providers },
    );
    expect(screen.getByTestId('whatsapp-connect-section')).toBeTruthy();
  });

  it('does NOT render the connect section for a non-whatsapp server', () => {
    render(
      <McpServerDetail
        server={SERVER}
        probe={HEALTHY}
        probeLoading={false}
        onClose={() => undefined}
      />,
      { wrapper: Providers },
    );
    expect(screen.queryByTestId('whatsapp-connect-section')).toBeNull();
  });
});
