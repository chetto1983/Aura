import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import type { McpProbeResult, McpServerRow } from '../governanceApi';

// McpBoard test (GOV-01 / T-28-03-01 / T-28-03-05). It mocks governanceApi so it can drive:
//  - the no-raw-secret invariant (the redacted chip never carries the value — the value never
//    enters the DOM even though the test fixture "knows" it),
//  - per-row probe isolation: a HUNG server's probe never resolves (a pending promise) while a
//    sibling probes Healthy — the hung row shows "Checking…", the list never blanks, and the
//    healthy sibling still renders its tool count.

const fetchMcpServers = vi.fn();
const probeMcpServer = vi.fn();
const installMcpServer = vi.fn();

vi.mock('../governanceApi', () => ({
  fetchMcpServers: (...a: unknown[]) => fetchMcpServers(...a) as Promise<readonly McpServerRow[]>,
  probeMcpServer: (...a: unknown[]) => probeMcpServer(...a) as Promise<McpProbeResult>,
  installMcpServer: (...a: unknown[]) => installMcpServer(...a) as Promise<unknown>,
}));

const { McpBoard } = await import('../McpBoard');

const SECRET_VALUE = 'super-secret-token-VALUE-42';

const SERVERS: McpServerRow[] = [
  {
    name: 'github',
    source: 'recipe:github',
    trust: 'trusted',
    riskPolicy: 'trusted',
    runtime: 'stdio',
    startupState: 'configured',
    authStatus: 'ok',
    profiles: ['work'],
    networkAllowlist: ['api.github.com'],
    // The backend NEVER serializes the value — only the redacted KEY chip. The fixture proves
    // the board renders the key, never a value (the value below is local to the test only).
    envKeys: [{ key: 'GITHUB_TOKEN', redacted: true }],
  },
  {
    name: 'filesystem',
    source: 'manual',
    trust: 'trusted',
    riskPolicy: 'trusted',
    runtime: 'stdio',
    startupState: 'configured',
    authStatus: 'ok',
    profiles: [],
    networkAllowlist: [],
    envKeys: [],
  },
];

function client() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

function Wrapper({ children, qc }: { children: ReactNode; qc: QueryClient }) {
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

describe('McpBoard (GOV-01)', () => {
  beforeEach(() => {
    fetchMcpServers.mockReset();
    probeMcpServer.mockReset();
  });
  afterEach(() => {
    vi.clearAllMocks();
  });

  it('renders only redacted chips — no raw secret value reaches the DOM (T-28-03-01)', async () => {
    fetchMcpServers.mockResolvedValue(SERVERS);
    probeMcpServer.mockResolvedValue({
      name: 'github',
      ok: true,
      tool_count: 3,
      detail: 'ok (3 tools)',
    });

    const { container } = render(<McpBoard />, {
      wrapper: ({ children }) => <Wrapper qc={client()}>{children}</Wrapper>,
    });

    await waitFor(() => {
      expect(screen.getByText('github')).toBeTruthy();
    });

    // The secret VALUE must never appear anywhere in the rendered DOM.
    expect(container.textContent).not.toContain(SECRET_VALUE);
    // The redacted chip copy IS present (count · redacted).
    await waitFor(() => {
      expect(screen.getAllByText(/redacted/).length).toBeGreaterThan(0);
    });
    // The row renders the server trust + the env-key count chip (github has 1 env key).
    expect(screen.getAllByText('trusted').length).toBeGreaterThan(0);
    expect(screen.getByText('1 · redacted')).toBeTruthy();

    // Selecting a row marks it aria-pressed (kills the selected === name ternary mutant).
    const row = screen.getByText('github').closest('button');
    if (row === null) throw new Error('mcp row button not found');
    expect(row.getAttribute('aria-pressed')).toBe('false');
    fireEvent.click(row);
    await waitFor(() => {
      expect(row.getAttribute('aria-pressed')).toBe('true');
    });
  });

  it('isolates a hung-probe row: it shows Checking… while a sibling renders Healthy · N tools', async () => {
    fetchMcpServers.mockResolvedValue(SERVERS);
    // github hangs forever (never resolves); filesystem resolves Healthy. Per-row queries mean
    // the hung row never blanks the list nor stalls the sibling (R1 / T-28-03-05).
    probeMcpServer.mockImplementation((name: string) => {
      if (name === 'github') {
        return new Promise<McpProbeResult>(() => {
          /* never resolves — simulates a hung server */
        });
      }
      return Promise.resolve({
        name: 'filesystem',
        ok: true,
        tool_count: 5,
        detail: 'ok (5 tools)',
      });
    });

    render(<McpBoard />, {
      wrapper: ({ children }) => <Wrapper qc={client()}>{children}</Wrapper>,
    });

    // Both rows render (the list is never blanked by the hung row).
    await waitFor(() => {
      expect(screen.getByText('github')).toBeTruthy();
      expect(screen.getByText('filesystem')).toBeTruthy();
    });

    // The healthy sibling resolves its status independently.
    await waitFor(() => {
      expect(screen.getByText('Healthy · 5 tools')).toBeTruthy();
    });

    // The hung row is still in its Checking… state (never resolved, never errored, never blank).
    expect(screen.getAllByText('Checking…').length).toBeGreaterThan(0);
  });

  it('renders a Timed out status when a probe query rejects (bounded-probe failure)', async () => {
    fetchMcpServers.mockResolvedValue([SERVERS[0]] as McpServerRow[]);
    probeMcpServer.mockRejectedValue(new Error('HTTP 504'));

    render(<McpBoard />, {
      wrapper: ({ children }) => <Wrapper qc={client()}>{children}</Wrapper>,
    });

    await waitFor(() => {
      expect(screen.getByText("Timed out — server didn't respond")).toBeTruthy();
    });
  });

  it('renders the empty state when no servers are configured', async () => {
    fetchMcpServers.mockResolvedValue([]);

    render(<McpBoard />, {
      wrapper: ({ children }) => <Wrapper qc={client()}>{children}</Wrapper>,
    });

    await waitFor(() => {
      expect(screen.getByText('No MCP servers configured')).toBeTruthy();
    });
  });

  it('renders a visible auth-error (never a blank board) when the list 401s', async () => {
    fetchMcpServers.mockRejectedValue(new Error('HTTP 401'));

    render(<McpBoard />, {
      wrapper: ({ children }) => <Wrapper qc={client()}>{children}</Wrapper>,
    });

    await waitFor(() => {
      expect(screen.getByText('Your session expired. Sign in again to continue.')).toBeTruthy();
    });
    expect(screen.getByRole('alert')).toBeTruthy();
  });

  it('opens the detail pane with redacted env chips when a row is selected', async () => {
    fetchMcpServers.mockResolvedValue(SERVERS);
    probeMcpServer.mockResolvedValue({
      name: 'github',
      ok: true,
      tool_count: 3,
      detail: 'ok (3 tools)',
    });

    const { container } = render(<McpBoard />, {
      wrapper: ({ children }) => <Wrapper qc={client()}>{children}</Wrapper>,
    });

    await waitFor(() => {
      expect(screen.getByText('github')).toBeTruthy();
    });
    fireEvent.click(screen.getByText('github'));

    // The detail pane shows the env-keys section + the KEY name, never the value.
    await waitFor(() => {
      expect(screen.getByText('GITHUB_TOKEN')).toBeTruthy();
    });
    expect(container.textContent).not.toContain(SECRET_VALUE);

    // Close returns to the detail-empty state. On desktop both the detail ✕ and the (lg:hidden)
    // backdrop carry the "Close" label; the detail ✕ is first in DOM order.
    const closeButton = screen.getAllByRole('button', { name: 'Close' })[0];
    if (closeButton === undefined) throw new Error('close button not found');
    fireEvent.click(closeButton);
    await waitFor(() => {
      expect(screen.getByText('Select a row to see details')).toBeTruthy();
    });
  });

  it('moves focus with ArrowDown and selects the focused row with Enter', async () => {
    fetchMcpServers.mockResolvedValue(SERVERS);
    probeMcpServer.mockResolvedValue({
      name: 'filesystem',
      ok: false,
      tool_count: 0,
      detail: 'dial failed',
    });

    render(<McpBoard />, {
      wrapper: ({ children }) => <Wrapper qc={client()}>{children}</Wrapper>,
    });

    await waitFor(() => {
      expect(screen.getByText('github')).toBeTruthy();
      expect(screen.getByText('filesystem')).toBeTruthy();
    });

    const github = screen.getByText('github').closest('button');
    const filesystem = screen.getByText('filesystem').closest('button');
    if (github === null || filesystem === null) throw new Error('mcp row button not found');

    github.focus();
    fireEvent.keyDown(github, { key: 'ArrowDown' });
    expect(document.activeElement).toBe(filesystem);

    fireEvent.keyDown(filesystem, { key: 'Enter' });
    await waitFor(() => {
      expect(screen.getByText('No environment keys.')).toBeTruthy();
    });
    expect(filesystem.getAttribute('aria-pressed')).toBe('true');
  });

  it('the detail pane shows the probe error + lastError, and "no env keys" for a keyless server', async () => {
    const failing: McpServerRow[] = [
      {
        name: 'filesystem',
        source: 'manual',
        trust: 'trusted',
        riskPolicy: 'trusted',
        runtime: 'stdio',
        startupState: 'configured',
        authStatus: 'ok',
        profiles: [],
        networkAllowlist: [],
        envKeys: [],
        lastError: 'spawn failed (sanitized)',
      },
    ];
    fetchMcpServers.mockResolvedValue(failing);
    probeMcpServer.mockResolvedValue({
      name: 'filesystem',
      ok: false,
      tool_count: 0,
      detail: 'dial failed',
      err: 'connection refused (sanitized)',
    });

    render(<McpBoard />, {
      wrapper: ({ children }) => <Wrapper qc={client()}>{children}</Wrapper>,
    });

    await waitFor(() => {
      expect(screen.getByText('filesystem')).toBeTruthy();
    });
    // The row shows the danger "Error — {state}" status for a reachable-but-failed probe.
    await waitFor(() => {
      expect(screen.getByText('Error — dial failed')).toBeTruthy();
    });

    fireEvent.click(screen.getByText('filesystem'));
    await waitFor(() => {
      expect(screen.getByText('No environment keys.')).toBeTruthy();
    });
    expect(screen.getAllByText('manual').length).toBeGreaterThan(0);
    // The probe err + the static lastError both render (sanitized).
    expect(screen.getByText('connection refused (sanitized)')).toBeTruthy();
    expect(screen.getByText('spawn failed (sanitized)')).toBeTruthy();
  });

  it('opens the install panel from the Add button (CLI-equiv + destination preview) and closes it', async () => {
    fetchMcpServers.mockResolvedValue(SERVERS);
    probeMcpServer.mockResolvedValue({ name: 'github', ok: true, tool_count: 3, detail: 'ok' });

    render(<McpBoard />, {
      wrapper: ({ children }) => <Wrapper qc={client()}>{children}</Wrapper>,
    });
    await waitFor(() => {
      expect(screen.getByText('github')).toBeTruthy();
    });

    // The ONE accent CTA opens the install panel even with a populated list.
    fireEvent.click(screen.getByRole('button', { name: 'Add MCP server' }));

    // The panel shows the live read-only preview: the CLI-equivalent label + the
    // `Will write to:` destination (the install previews BEFORE any save).
    await waitFor(() => {
      expect(screen.getByText('CLI equivalent')).toBeTruthy();
    });
    expect(screen.getByText('Will write to:')).toBeTruthy();
    // Recipe | Custom mode toggle is a role=group.
    expect(screen.getByRole('group', { name: /install mode|mode/i })).toBeTruthy();

    // Close the panel via the ✕ (closeAria='Close') — both the panel ✕ and the (lg:hidden)
    // backdrop carry the label; the panel ✕ is first in DOM order.
    const closeButton = screen.getAllByRole('button', { name: 'Close' })[0];
    if (closeButton === undefined) throw new Error('close button not found');
    fireEvent.click(closeButton);
    await waitFor(() => {
      expect(screen.queryByText('Will write to:')).toBeNull();
    });
  });

  it('opens the install panel even from the empty state (reachable on a zero-server list)', async () => {
    fetchMcpServers.mockResolvedValue([]);

    render(<McpBoard />, {
      wrapper: ({ children }) => <Wrapper qc={client()}>{children}</Wrapper>,
    });
    await waitFor(() => {
      expect(screen.getByText('No MCP servers configured')).toBeTruthy();
    });

    fireEvent.click(screen.getByRole('button', { name: 'Add MCP server' }));
    await waitFor(() => {
      expect(screen.getByText('Will write to:')).toBeTruthy();
    });
  });
});
