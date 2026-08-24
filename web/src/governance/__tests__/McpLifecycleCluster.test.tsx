import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import type { McpServerRow } from '../governanceApi';

// McpLifecycleCluster test (MCPW-02/03) — the inline trust/enable/disable/remove controls on
// the MCP server detail header. It drives: the enable/disable toggle (never color-alone — a
// dot + label), the `Trust & approve` inline form (a reason is REQUIRED — the confirm is
// disabled until a non-empty reason is typed, no value leaks), and the destructive Remove
// confirmation dialog (action-specific labels Remove server / Keep server, the safe Keep
// action default-focused not the destructive one, Escape-dismissable).

const setMcpServerEnabled = vi.fn();
const trustMcpServer = vi.fn();
const removeMcpServer = vi.fn();

vi.mock('../governanceApi', () => ({
  setMcpServerEnabled: (...a: unknown[]) => setMcpServerEnabled(...a) as Promise<unknown>,
  trustMcpServer: (...a: unknown[]) => trustMcpServer(...a) as Promise<unknown>,
  removeMcpServer: (...a: unknown[]) => removeMcpServer(...a) as Promise<void>,
}));

const { McpLifecycleCluster } = await import('../McpLifecycleCluster');

const SERVER: McpServerRow = {
  name: 'github',
  source: 'recipe:github',
  trust: 'trusted',
  riskPolicy: 'trusted',
  runtime: 'stdio',
  startupState: 'configured',
  authStatus: 'ok',
  profiles: [],
  networkAllowlist: [],
  envKeys: [],
};

// Trust & approve exists only for a server that is actually blocked. Installing is the
// authorization everywhere else, so on a runnable row the button changed nothing while
// sitting beside Remove, which changes everything.
const BLOCKED_SERVER: McpServerRow = { ...SERVER, trust: 'blocked', riskPolicy: 'blocked' };

function client() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

function Wrapper({ children, qc }: { children: ReactNode; qc: QueryClient }) {
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

function renderCluster(server: McpServerRow = SERVER, onRemoved = vi.fn()) {
  render(<McpLifecycleCluster server={server} onRemoved={onRemoved} />, {
    wrapper: ({ children }) => <Wrapper qc={client()}>{children}</Wrapper>,
  });
  return onRemoved;
}

describe('McpLifecycleCluster (MCPW-02/03)', () => {
  beforeEach(() => {
    setMcpServerEnabled.mockReset();
    trustMcpServer.mockReset();
    removeMcpServer.mockReset();
  });
  afterEach(() => {
    vi.clearAllMocks();
  });

  it('shows an Enabled state for a trusted server and disables it on toggle', async () => {
    setMcpServerEnabled.mockResolvedValue({ name: 'github' });
    renderCluster();

    // Never color-alone: the toggle carries the "Enabled" text label (not just a dot).
    const toggle = screen.getByRole('button', { pressed: true });
    expect(toggle.textContent).toContain('Enabled');
    fireEvent.click(toggle);
    await waitFor(() => {
      expect(setMcpServerEnabled).toHaveBeenCalledWith('github', false);
    });
  });

  it('reads a blocked server as Disabled and enables it on toggle', async () => {
    setMcpServerEnabled.mockResolvedValue({ name: 'github' });
    renderCluster({ ...SERVER, trust: 'blocked' });

    const toggle = screen.getByRole('button', { pressed: false });
    expect(toggle.textContent).toContain('Disabled');
    fireEvent.click(toggle);
    await waitFor(() => {
      expect(setMcpServerEnabled).toHaveBeenCalledWith('github', true);
    });
  });

  // The trust ceremony is gone from the cockpit entirely, blocked servers included:
  // installing a server IS the authorization, so `Trust & approve` changed nothing on any
  // row an operator would click it on — while sitting next to Remove, which changes a great
  // deal. The two tests that pinned its reason form went with it.
  it('offers no Trust & approve, blocked or not', () => {
    renderCluster();
    expect(screen.queryByRole('button', { name: 'Trust & approve' })).toBeNull();
    renderCluster(BLOCKED_SERVER);
    expect(screen.queryByRole('button', { name: 'Trust & approve' })).toBeNull();
  });

  it('the Remove dialog labels are action-specific, the safe Keep action is default-focused, and Escape dismisses', async () => {
    renderCluster();
    fireEvent.click(screen.getByRole('button', { name: 'Remove server' }));

    const dialog = await screen.findByRole('alertdialog');
    expect(dialog.getAttribute('aria-modal')).toBe('true');
    // Action-specific labels (NOT generic OK/Cancel): Keep server (safe) + Remove server.
    const keep = screen.getByRole('button', { name: 'Keep server' });
    expect(within(dialog).queryByText(/Remove "github"\?/)).toBeTruthy();
    // The SAFE action is default-focused, never the destructive one (NN/g).
    expect(document.activeElement).toBe(keep);

    // Escape dismisses the dialog (no remove call).
    fireEvent.keyDown(document, { key: 'Escape' });
    await waitFor(() => {
      expect(screen.queryByRole('alertdialog')).toBeNull();
    });
    expect(removeMcpServer).not.toHaveBeenCalled();
  });

  it('confirming Remove calls removeMcpServer and notifies onRemoved', async () => {
    removeMcpServer.mockResolvedValue(undefined);
    const onRemoved = renderCluster();

    fireEvent.click(screen.getByRole('button', { name: 'Remove server' }));
    const dialog = await screen.findByRole('alertdialog');
    // The destructive confirm is the in-dialog "Remove server".
    const confirm = within(dialog).getByRole('button', { name: 'Remove server' });
    fireEvent.click(confirm);
    await waitFor(() => {
      expect(removeMcpServer).toHaveBeenCalledWith('github');
    });
    await waitFor(() => {
      expect(onRemoved).toHaveBeenCalled();
    });
  });
});
