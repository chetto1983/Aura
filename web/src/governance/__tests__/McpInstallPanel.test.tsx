import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import type { McpWriteResponse } from '../governanceApi';

// McpInstallPanel test (MCPW-01). It mocks installMcpServer so it can assert:
//  - the live PREVIEW shows the CLI-equivalent + the `Will write to:` destination,
//  - the CLI-equivalent updates live as the operator switches Recipe ↔ Custom + types,
//  - a duplicate name disables Install with an aria-invalid field error AND fires no request.

const installMcpServer = vi.fn();

vi.mock('../governanceApi', () => ({
  installMcpServer: (...a: unknown[]) => installMcpServer(...a) as Promise<McpWriteResponse>,
}));

const { McpInstallPanel } = await import('../McpInstallPanel');

function client() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
}

function renderPanel(props: { existingNames?: readonly string[]; onClose?: () => void }) {
  const qc = client();
  const Wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
  return render(
    <McpInstallPanel
      existingNames={props.existingNames ?? []}
      onClose={props.onClose ?? (() => undefined)}
    />,
    { wrapper: Wrapper },
  );
}

describe('McpInstallPanel (MCPW-01)', () => {
  beforeEach(() => {
    installMcpServer.mockReset();
    installMcpServer.mockResolvedValue({ name: 'calculator', envKeys: [] });
  });
  afterEach(() => {
    vi.clearAllMocks();
  });

  it('shows the live preview: the CLI-equivalent + the Will write to: destination', () => {
    renderPanel({});
    // Recipe mode default → `aura mcp install <recipe>`.
    expect(screen.getByText('CLI equivalent')).toBeTruthy();
    expect(screen.getByText(/aura mcp install calculator/)).toBeTruthy();
    expect(screen.getByText('Will write to:')).toBeTruthy();
    expect(screen.getByText('~/.aura/mcp/servers.json')).toBeTruthy();
  });

  it('updates the CLI-equivalent live when switching to Custom and typing a command', () => {
    renderPanel({});
    fireEvent.click(screen.getByRole('button', { name: 'Custom (stdio)', pressed: false }));
    fireEvent.change(screen.getByLabelText('Server name'), { target: { value: 'mytool' } });
    fireEvent.change(screen.getByLabelText('Command'), { target: { value: 'mytool-bin' } });
    expect(screen.getByText('aura mcp add mytool -- mytool-bin')).toBeTruthy();
  });

  it('disables Install + shows an aria-invalid error on a duplicate name, no request fires', () => {
    renderPanel({ existingNames: ['calculator'] });
    // Recipe default = calculator → collides with the existing name.
    const install = screen.getByRole('button', { name: 'Install server' });
    expect(install.hasAttribute('disabled')).toBe(true);

    const nameField = screen.getByLabelText('Server name');
    expect(nameField.getAttribute('aria-invalid')).toBe('true');
    expect(screen.getByRole('alert').textContent).toContain('already exists');

    fireEvent.click(install);
    expect(installMcpServer).not.toHaveBeenCalled();
  });

  it('enables Install for a unique recipe name and fires the recipe install request', async () => {
    const onClose = vi.fn();
    renderPanel({ existingNames: ['memory'], onClose });
    // Default recipe = calculator, not in existingNames → installable.
    const install = screen.getByRole('button', { name: 'Install server' });
    expect(install.hasAttribute('disabled')).toBe(false);
    fireEvent.click(install);
    await waitFor(() => {
      expect(installMcpServer).toHaveBeenCalledTimes(1);
    });
    expect(installMcpServer).toHaveBeenCalledWith(
      expect.objectContaining({ name: 'calculator', recipe: 'calculator' }),
    );
    await waitFor(() => {
      expect(onClose).toHaveBeenCalled();
    });
  });

  it('Discard install calls onClose without a request', () => {
    const onClose = vi.fn();
    renderPanel({ onClose });
    fireEvent.click(screen.getByRole('button', { name: 'Discard install' }));
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(installMcpServer).not.toHaveBeenCalled();
  });

  it('updates the CLI-equivalent live when a different recipe is selected', () => {
    renderPanel({});
    fireEvent.change(screen.getByLabelText('Recipe'), { target: { value: 'whatsapp' } });
    expect(screen.getByText('aura mcp install whatsapp')).toBeTruthy();
  });

  it('fires a custom stdio install request with the command + trimmed args', async () => {
    const onClose = vi.fn();
    renderPanel({ onClose });
    fireEvent.click(screen.getByRole('button', { name: 'Custom (stdio)', pressed: false }));
    fireEvent.change(screen.getByLabelText('Server name'), { target: { value: 'mytool' } });
    fireEvent.change(screen.getByLabelText('Command'), { target: { value: '  mytool-bin  ' } });
    fireEvent.change(screen.getByLabelText('Argument 1'), { target: { value: '  --flag  ' } });
    const install = screen.getByRole('button', { name: 'Install server' });
    expect(install.hasAttribute('disabled')).toBe(false);
    fireEvent.click(install);
    await waitFor(() => {
      expect(installMcpServer).toHaveBeenCalledTimes(1);
    });
    expect(installMcpServer).toHaveBeenCalledWith({
      name: 'mytool',
      command: 'mytool-bin',
      args: ['--flag'],
    });
    await waitFor(() => {
      expect(onClose).toHaveBeenCalled();
    });
  });

  it('adds a new argument row and edits each argument independently', () => {
    renderPanel({});
    fireEvent.click(screen.getByRole('button', { name: 'Custom (stdio)', pressed: false }));
    fireEvent.change(screen.getByLabelText('Argument 1'), { target: { value: 'first' } });
    fireEvent.click(screen.getByRole('button', { name: 'Add argument' }));
    const second = screen.getByLabelText<HTMLInputElement>('Argument 2');
    fireEvent.change(second, { target: { value: 'second' } });
    expect(screen.getByLabelText<HTMLInputElement>('Argument 1').value).toBe('first');
    expect(second.value).toBe('second');
  });

  it('renders a spinner and marks Install aria-busy while the request is in flight', async () => {
    const onClose = vi.fn();
    let resolve: (value: McpWriteResponse) => void = () => undefined;
    installMcpServer.mockReturnValue(
      new Promise<McpWriteResponse>((res) => {
        resolve = res;
      }),
    );
    renderPanel({ existingNames: ['memory'], onClose });
    const install = screen.getByRole('button', { name: 'Install server' });
    fireEvent.click(install);
    await waitFor(() => {
      expect(install.getAttribute('aria-busy')).toBe('true');
    });
    expect(screen.getByTestId('spinner')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Discard install' }).hasAttribute('disabled')).toBe(
      true,
    );
    resolve({ name: 'calculator', envKeys: [] });
    await waitFor(() => {
      expect(onClose).toHaveBeenCalledTimes(1);
    });
  });

  it('shows the destructive error alert when the install request fails', async () => {
    installMcpServer.mockRejectedValueOnce(new Error('boom'));
    renderPanel({ existingNames: ['memory'] });
    fireEvent.click(screen.getByRole('button', { name: 'Install server' }));
    await waitFor(() => {
      expect(screen.getByText(/load this board/)).toBeTruthy();
    });
    // The failed request still fired exactly once (no retry).
    expect(installMcpServer).toHaveBeenCalledTimes(1);
  });
});
