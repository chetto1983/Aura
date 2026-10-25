import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import { HttpError } from '../../api/json';
import type { PluginRecord, PluginRemoveReport } from '../pluginApi';

// PluginsBoard: the installed plugins, each with what it brought, and a removal that asks once
// and keeps the plugin's data unless the operator ticks the box. The install panel has its own
// suite.

const fetchPlugins = vi.fn();
const removePlugin = vi.fn();

vi.mock('../pluginApi', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../pluginApi')>()),
  fetchPlugins: () => fetchPlugins() as Promise<readonly PluginRecord[]>,
  removePlugin: (...a: unknown[]) => removePlugin(...a) as Promise<PluginRemoveReport>,
}));
vi.mock('../PluginInstallPanel', () => ({
  PluginInstallPanel: ({ installedNames }: { installedNames: readonly string[] }) => (
    <div data-testid="install-panel">{installedNames.join(',')}</div>
  ),
}));

const { PluginsBoard } = await import('../PluginsBoard');

const RECORDS: readonly PluginRecord[] = [
  {
    name: 'broker-analysis',
    version: '0.1.1',
    commit: '0123456789abcdef0123456789abcdef01234567',
    skills: [{ name: 'broker-report', hash: 'h' }],
    connectors: ['broker-analysis'],
    phase: 'installed',
  },
  {
    name: 'half-done',
    version: '',
    commit: 'fedcba9876543210fedcba9876543210fedcba98',
    skills: [],
    connectors: [],
    phase: 'installing',
  },
];

function renderBoard() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const Wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
  return render(<PluginsBoard onOpenServer={() => undefined} />, { wrapper: Wrapper });
}

async function row(name: string) {
  const list = await screen.findByRole('list', { name: 'Installed plugins' });
  const item = within(list)
    .getAllByRole('listitem')
    .find((li) => within(li).queryByRole('heading', { name }) !== null);
  if (item === undefined) throw new Error(`no row ${name}`);
  return item;
}

function dataBox() {
  return screen.getByRole('checkbox', { name: 'Also delete its data (only in your box)' });
}

describe('PluginsBoard', () => {
  beforeEach(() => {
    fetchPlugins.mockReset();
    removePlugin.mockReset();
    fetchPlugins.mockResolvedValue(RECORDS);
    removePlugin.mockResolvedValue({
      name: 'broker-analysis',
      skills: [],
      connectors: [],
      dataDeleted: false,
    });
  });
  afterEach(() => {
    vi.clearAllMocks();
  });

  it('shows each plugin with its version, commit, skills and connectors', async () => {
    renderBoard();
    const broker = await row('broker-analysis');

    expect(within(broker).getByText('0.1.1')).toBeTruthy();
    expect(within(broker).getByText('0123456')).toBeTruthy();
    expect(within(broker).getByText('Skills: broker-report')).toBeTruthy();
    expect(within(broker).getByText('Connectors: broker-analysis')).toBeTruthy();
    expect(within(broker).queryByText('Installing')).toBeNull();

    const half = await row('half-done');
    expect(within(half).getByText('Installing')).toBeTruthy();
    expect(within(half).getByText('Skills: none')).toBeTruthy();
    expect(screen.getByTestId('install-panel').textContent).toBe('broker-analysis,half-done');
  });

  it('removes a plugin and keeps its data by default', async () => {
    renderBoard();
    fireEvent.click(
      within(await row('broker-analysis')).getByRole('button', {
        name: 'Remove broker-analysis',
      }),
    );

    expect(dataBox().getAttribute('aria-checked')).toBe('false');
    fireEvent.click(screen.getByRole('button', { name: 'Remove' }));
    await waitFor(() => {
      expect(removePlugin).toHaveBeenCalledWith('broker-analysis', false);
    });
    expect(await screen.findByText('Removed broker-analysis.')).toBeTruthy();
  });

  it('deletes the data too when the box is ticked', async () => {
    renderBoard();
    fireEvent.click(
      within(await row('broker-analysis')).getByRole('button', {
        name: 'Remove broker-analysis',
      }),
    );

    fireEvent.click(dataBox());
    fireEvent.click(screen.getByRole('button', { name: 'Remove' }));
    await waitFor(() => {
      expect(removePlugin).toHaveBeenCalledWith('broker-analysis', true);
    });
  });

  it('opens the dialog unticked again after a cancel', async () => {
    renderBoard();
    const removeButton = within(await row('broker-analysis')).getByRole('button', {
      name: 'Remove broker-analysis',
    });
    fireEvent.click(removeButton);
    fireEvent.click(dataBox());
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).toBeNull();
    });

    fireEvent.click(removeButton);
    expect(dataBox().getAttribute('aria-checked')).toBe('false');
  });

  it('says which skill stayed, and why', async () => {
    removePlugin.mockResolvedValue({
      name: 'broker-analysis',
      skills: [{ name: 'broker-report', outcome: 'skipped', reason: 'changed since install' }],
      connectors: [{ name: 'broker-analysis', outcome: 'ok', reason: '' }],
      dataDeleted: false,
    });
    renderBoard();
    fireEvent.click(
      within(await row('broker-analysis')).getByRole('button', {
        name: 'Remove broker-analysis',
      }),
    );
    fireEvent.click(screen.getByRole('button', { name: 'Remove' }));

    expect(await screen.findByText('broker-report — changed since install')).toBeTruthy();
    expect(screen.queryByText(/broker-analysis — /)).toBeNull();
  });

  it('shows the server reason when the removal fails', async () => {
    removePlugin.mockRejectedValue(
      new HttpError(502, 'plugin broker-analysis: 1 skill(s) could not be archived'),
    );
    renderBoard();
    fireEvent.click(
      within(await row('broker-analysis')).getByRole('button', {
        name: 'Remove broker-analysis',
      }),
    );
    fireEvent.click(screen.getByRole('button', { name: 'Remove' }));

    expect(
      await screen.findByText('plugin broker-analysis: 1 skill(s) could not be archived'),
    ).toBeTruthy();
  });

  it('says when nothing is installed', async () => {
    fetchPlugins.mockResolvedValue([]);
    renderBoard();

    expect(await screen.findByText('No plugins installed')).toBeTruthy();
    expect(screen.getByTestId('install-panel')).toBeTruthy();
  });
});
