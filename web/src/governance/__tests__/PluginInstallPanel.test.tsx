import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import { HttpError } from '../../api/json';
import type { PluginReport, PluginReview } from '../pluginApi';

// PluginInstallPanel: the operator reviews a plugin by its owner/repo/plugin name, reads where
// each connector runs and what was skipped, and installs exactly what the review showed.

const reviewPlugin = vi.fn();
const installPlugin = vi.fn();

vi.mock('../pluginApi', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../pluginApi')>()),
  reviewPlugin: (...a: unknown[]) => reviewPlugin(...a) as Promise<PluginReview>,
  installPlugin: (...a: unknown[]) => installPlugin(...a) as Promise<PluginReport>,
}));

const { PluginInstallPanel } = await import('../PluginInstallPanel');

const REF = 'chetto1983/broker-analysis-mcp/broker-analysis';
const COMMIT = '0123456789abcdef0123456789abcdef01234567';

const REVIEW: PluginReview = {
  ref: REF,
  commit: COMMIT,
  digest: 'd'.repeat(64),
  name: 'broker-analysis',
  version: '0.1.1',
  author: 'chetto1983',
  format: 'agent-plugins/1.0.0',
  skills: [{ name: 'broker-report', hash: 'h' }],
  skippedSkills: [{ name: 'memory', reason: 'name is taken by a server from "recipe:memory"' }],
  connectors: [
    {
      name: 'broker-analysis',
      transport: 'stdio',
      placement: 'box',
      trust: 'untrusted',
      oauth: false,
    },
    {
      name: 'broker-cloud',
      transport: 'streamable_http',
      placement: 'remote',
      trust: 'untrusted',
      oauth: true,
    },
  ],
  skippedConnectors: [{ name: 'legacy', reason: 'declares headers' }],
  notes: ['hooks are not run'],
  mcpDisabled: '',
};

function renderPanel(
  props: { installedNames?: readonly string[]; onOpenServer?: (name: string) => void } = {},
) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const Wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
  return render(
    <PluginInstallPanel
      installedNames={props.installedNames ?? []}
      onOpenServer={props.onOpenServer ?? (() => undefined)}
    />,
    { wrapper: Wrapper },
  );
}

async function review(ref = REF) {
  fireEvent.change(screen.getByLabelText('Plugin'), { target: { value: ref } });
  fireEvent.click(screen.getByRole('button', { name: 'Review' }));
  await screen.findByRole('heading', { name: /broker-analysis/ });
}

function connectorItem(name: string) {
  const list = screen.getByRole('list', { name: 'Connectors' });
  const item = within(list)
    .getAllByRole('listitem')
    .find((li) => within(li).queryByText(name) !== null);
  if (item === undefined) throw new Error(`no connector ${name}`);
  return item;
}

describe('PluginInstallPanel', () => {
  beforeEach(() => {
    reviewPlugin.mockReset();
    installPlugin.mockReset();
    reviewPlugin.mockResolvedValue(REVIEW);
  });
  afterEach(() => {
    vi.clearAllMocks();
  });

  it('shows where each connector runs, what was skipped and why, and the notes', async () => {
    renderPanel();
    await review(`  ${REF}  `);

    expect(reviewPlugin).toHaveBeenCalledWith(REF);
    expect(within(connectorItem('broker-analysis')).getByText('in the box')).toBeTruthy();
    expect(
      within(connectorItem('broker-analysis')).queryByText('may ask for an authorization'),
    ).toBeNull();
    expect(within(connectorItem('broker-cloud')).getByText('remote')).toBeTruthy();
    expect(
      within(connectorItem('broker-cloud')).getByText('may ask for an authorization'),
    ).toBeTruthy();

    const skipped = screen.getByRole('list', { name: 'Skipped' });
    expect(
      within(skipped).getByText('memory — name is taken by a server from "recipe:memory"'),
    ).toBeTruthy();
    expect(within(skipped).getByText('legacy — declares headers')).toBeTruthy();
    expect(screen.getByText('hooks are not run')).toBeTruthy();
    expect(screen.getByText('broker-report')).toBeTruthy();
    expect(screen.getByText('0123456')).toBeTruthy();
  });

  it('says why the connectors are off', async () => {
    reviewPlugin.mockResolvedValue({
      ...REVIEW,
      connectors: [],
      skippedConnectors: [],
      mcpDisabled: 'mcp.json resolves outside the plugin',
    });
    renderPanel();
    await review();

    expect(
      screen.getByText('Connectors are off: mcp.json resolves outside the plugin'),
    ).toBeTruthy();
  });

  it('installs exactly the reviewed ref, commit and digest', async () => {
    installPlugin.mockResolvedValue({ review: REVIEW, skills: [], connectors: [] });
    renderPanel();
    await review(`  ${REF}  `);

    fireEvent.click(screen.getByRole('button', { name: 'Install' }));
    await waitFor(() => {
      expect(installPlugin).toHaveBeenCalledTimes(1);
    });
    expect(installPlugin).toHaveBeenCalledWith({ ref: REF, commit: COMMIT, digest: REVIEW.digest });
  });

  it('asks for a new review when what it would write changed', async () => {
    installPlugin.mockRejectedValue(new HttpError(409, 'what this install would write changed'));
    renderPanel();
    await review();

    fireEvent.click(screen.getByRole('button', { name: 'Install' }));
    expect(
      await screen.findByText(
        'What this install would write changed since the review. Review it again.',
      ),
    ).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Review again' }));
    await waitFor(() => {
      expect(reviewPlugin).toHaveBeenCalledTimes(2);
    });
    await waitFor(() => {
      expect(screen.queryByText(/changed since the review/)).toBeNull();
    });
    expect(screen.getByRole('button', { name: 'Install' }).hasAttribute('disabled')).toBe(false);
  });

  it('refuses a plugin that is already installed', async () => {
    renderPanel({ installedNames: ['broker-analysis'] });
    await review();

    expect(
      screen.getByText(
        'broker-analysis is already installed. Remove it first to install it again.',
      ),
    ).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Install' }).hasAttribute('disabled')).toBe(true);
  });

  it('shows the server reason when the review fails', async () => {
    reviewPlugin.mockRejectedValue(new HttpError(502, 'fetch chetto1983/nope: exit status 128'));
    renderPanel();
    fireEvent.change(screen.getByLabelText('Plugin'), { target: { value: 'chetto1983/nope/p' } });
    fireEvent.click(screen.getByRole('button', { name: 'Review' }));

    expect(await screen.findByText('fetch chetto1983/nope: exit status 128')).toBeTruthy();
  });

  it('drops the review when the name changes', async () => {
    renderPanel();
    await review();

    fireEvent.change(screen.getByLabelText('Plugin'), { target: { value: 'chetto1983/other/p' } });
    expect(screen.queryByRole('button', { name: 'Install' })).toBeNull();
    expect(screen.queryByRole('list', { name: 'Connectors' })).toBeNull();
  });

  it('reports each step and opens a connector that needs attention on the MCP board', async () => {
    installPlugin.mockResolvedValue({
      review: REVIEW,
      skills: [
        { name: 'broker-report', outcome: 'ok', reason: '' },
        { name: 'memory', outcome: 'skipped', reason: 'name is taken' },
      ],
      connectors: [
        { name: 'broker-analysis', outcome: 'ok', reason: '' },
        { name: 'broker-cloud', outcome: 'attention', reason: '401 Unauthorized' },
      ],
    });
    const onOpenServer = vi.fn();
    renderPanel({ onOpenServer });
    await review();
    fireEvent.click(screen.getByRole('button', { name: 'Install' }));

    const report = await screen.findByRole('list', { name: 'Install report' });
    const steps = within(report).getAllByRole('listitem');
    expect(steps.map((li) => li.textContent)).toEqual([
      expect.stringContaining('broker-report'),
      expect.stringContaining('memory'),
      expect.stringContaining('broker-analysis'),
      expect.stringContaining('broker-cloud'),
    ]);
    const [installedSkill, skippedSkill, , attention] = steps;
    if (installedSkill === undefined || skippedSkill === undefined || attention === undefined) {
      throw new Error('the report lost a step');
    }
    expect(within(installedSkill).getByText('installed')).toBeTruthy();
    expect(within(skippedSkill).getByText('skipped')).toBeTruthy();
    expect(within(skippedSkill).getByText('name is taken')).toBeTruthy();
    expect(within(attention).getByText('needs attention')).toBeTruthy();
    expect(within(attention).getByText('401 Unauthorized')).toBeTruthy();
    expect(within(report).getAllByRole('button', { name: 'Open on the MCP board' })).toHaveLength(
      1,
    );

    fireEvent.click(within(attention).getByRole('button', { name: 'Open on the MCP board' }));
    expect(onOpenServer).toHaveBeenCalledWith('broker-cloud');
  });
});
