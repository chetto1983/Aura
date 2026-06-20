import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import '../../i18n/i18n';
import { McpServerDetail } from '../McpServerDetail';
import type { McpProbeResult, McpServerRow } from '../governanceApi';

// McpServerDetail test — renders the detail component directly with props so every field
// label/value, the redacted-chip branch, the probe-loading/resolved/error branches, and the
// lastError branch are asserted (mutation hardening: the rendered copy is the contract).

const SERVER: McpServerRow = {
  name: 'github',
  trust: 'trusted',
  runtime: 'stdio',
  startupState: 'configured',
  authStatus: 'ok',
  envKeys: [
    { key: 'GITHUB_TOKEN', redacted: true },
    { key: 'GITHUB_ORG', redacted: false },
  ],
};

const HEALTHY: McpProbeResult = { name: 'github', ok: true, tool_count: 7, detail: 'ok (7 tools)' };

describe('McpServerDetail', () => {
  it('renders every static field label and value', () => {
    render(<McpServerDetail server={SERVER} probe={HEALTHY} probeLoading={false} onClose={() => undefined} />);

    expect(screen.getByText('Trust')).toBeTruthy();
    expect(screen.getByText('trusted')).toBeTruthy();
    expect(screen.getByText('Runtime')).toBeTruthy();
    expect(screen.getByText('stdio')).toBeTruthy();
    expect(screen.getByText('Startup')).toBeTruthy();
    expect(screen.getByText('configured')).toBeTruthy();
    expect(screen.getByText('Auth status')).toBeTruthy();
    expect(screen.getByText('ok')).toBeTruthy();
  });

  it('renders a redacted chip ONLY for a redacted key (the non-redacted key has no chip suffix)', () => {
    render(<McpServerDetail server={SERVER} probe={HEALTHY} probeLoading={false} onClose={() => undefined} />);

    expect(screen.getByText('GITHUB_TOKEN')).toBeTruthy();
    expect(screen.getByText('GITHUB_ORG')).toBeTruthy();
    // Exactly one redacted suffix (only GITHUB_TOKEN is redacted).
    expect(screen.getAllByText(/redacted/)).toHaveLength(1);
  });

  it('renders the healthy probe tool count + detail in the success tone', () => {
    render(<McpServerDetail server={SERVER} probe={HEALTHY} probeLoading={false} onClose={() => undefined} />);
    expect(screen.getByText('7')).toBeTruthy();
    const detail = screen.getByText('ok (7 tools)');
    expect(detail).toBeTruthy();
    // The healthy detail uses the success tone (kills the ok ? success : danger ternary mutant).
    expect(detail.className).toContain('text-success');
  });

  it('renders a failed probe detail in the danger tone', () => {
    const failed: McpProbeResult = { name: 'github', ok: false, tool_count: 0, detail: 'dial failed' };
    render(<McpServerDetail server={SERVER} probe={failed} probeLoading={false} onClose={() => undefined} />);
    const detail = screen.getByText('dial failed');
    expect(detail.className).toContain('text-danger');
  });

  it('renders Checking… while the probe is loading', () => {
    render(<McpServerDetail server={SERVER} probe={undefined} probeLoading={true} onClose={() => undefined} />);
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
    render(<McpServerDetail server={SERVER} probe={failed} probeLoading={false} onClose={() => undefined} />);
    expect(screen.getByText('dial failed')).toBeTruthy();
    expect(screen.getByText('connection refused')).toBeTruthy();
  });

  it('renders "no env keys" + the static lastError when present, and fires onClose', () => {
    const bare: McpServerRow = {
      name: 'fs',
      trust: 'trusted',
      runtime: 'stdio',
      startupState: 'configured',
      authStatus: 'ok',
      envKeys: [],
      lastError: 'spawn failed',
    };
    const onClose = vi.fn();
    render(<McpServerDetail server={bare} probe={undefined} probeLoading={false} onClose={onClose} />);

    expect(screen.getByText('No environment keys.')).toBeTruthy();
    expect(screen.getByText('spawn failed')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Close' }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
