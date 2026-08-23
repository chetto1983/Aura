import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import '../../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys

// GovernanceWorkspace is the lazy tab-strip shell. The four panes are mocked (their data hooks
// are exercised by their own tests) so this suite asserts ONLY the workspace obligations: the
// shadcn Tabs strip renders (MCP/Skills/Scheduler/Audit), the obsolete read-only banner is gone,
// and switching tabs swaps the active pane. These are the GovernanceWorkspace behavior
// obligations from the plan acceptance criteria.

vi.mock('../McpBoard', () => ({
  McpBoard: () => <div data-testid="mcp-board">mcp</div>,
}));
vi.mock('../SkillsBoard', () => ({
  SkillsBoard: () => <div data-testid="skills-board">skills</div>,
}));
vi.mock('../SchedulerBoard', () => ({
  SchedulerBoard: () => <div data-testid="scheduler-board">scheduler</div>,
}));
vi.mock('../../audit/AdminAuditView', () => ({
  AdminAuditView: () => <div data-testid="audit-view">audit</div>,
}));

const { default: GovernanceWorkspace } = await import('../GovernanceWorkspace');

describe('GovernanceWorkspace (boards mocked)', () => {
  it('renders the role=tablist tab strip with the four governance tabs', async () => {
    render(<GovernanceWorkspace />);

    const tablist = screen.getByRole('tablist', { name: 'Governance' });
    expect(tablist).toBeTruthy();
    expect(tablist.getAttribute('data-slot')).toBe('tabs-list');
    expect(tablist.className).toContain('grid-cols-4');

    const mcpTab = screen.getByRole('tab', { name: 'MCP servers' });
    expect(mcpTab.getAttribute('data-slot')).toBe('tabs-trigger');
    expect(mcpTab.className).toContain('min-w-0');
    expect(mcpTab.querySelector('svg[data-icon="inline-start"]')).not.toBeNull();
    expect(mcpTab.querySelector('[data-tab-label]')?.className).toContain('truncate');
    expect(screen.getByRole('tab', { name: 'Skills' }).getAttribute('data-slot')).toBe(
      'tabs-trigger',
    );
    expect(screen.getByRole('tab', { name: 'Scheduler' }).getAttribute('data-slot')).toBe(
      'tabs-trigger',
    );
    expect(screen.getByRole('tab', { name: 'Audit' }).getAttribute('data-slot')).toBe(
      'tabs-trigger',
    );

    // The MCP board is the default-open tab.
    await waitFor(() => {
      expect(screen.getByTestId('mcp-board')).toBeTruthy();
    });
  });

  it('does not render the obsolete read-only banner', () => {
    render(<GovernanceWorkspace />);
    expect(
      screen.queryByText('Read-only — viewing only. Changes arrive in a later phase.'),
    ).toBeNull();
  });

  it('marks the active tab with aria-selected and swaps the board on tab click', async () => {
    render(<GovernanceWorkspace />);

    const mcpTab = screen.getByRole('tab', { name: 'MCP servers' });
    const skillsTab = screen.getByRole('tab', { name: 'Skills' });
    expect(mcpTab.getAttribute('aria-selected')).toBe('true');
    expect(skillsTab.getAttribute('aria-selected')).toBe('false');

    // Roving tabindex: the active tab is 0, inactive are -1 (kills the tabIndex ternary mutant).
    expect(mcpTab.getAttribute('tabindex')).toBe('0');
    expect(skillsTab.getAttribute('tabindex')).toBe('-1');

    fireEvent.click(skillsTab);

    await waitFor(() => {
      expect(screen.getByTestId('skills-board')).toBeTruthy();
    });
    expect(skillsTab.getAttribute('aria-selected')).toBe('true');
    expect(mcpTab.getAttribute('aria-selected')).toBe('false');
    expect(skillsTab.getAttribute('tabindex')).toBe('0');
    expect(mcpTab.getAttribute('tabindex')).toBe('-1');
  });

  it('opens the per-identity audit feed on its own tab', async () => {
    render(<GovernanceWorkspace />);

    fireEvent.click(screen.getByRole('tab', { name: 'Audit' }));

    await waitFor(() => {
      expect(screen.getByTestId('audit-view')).toBeTruthy();
    });
    // The feed is a plain section, so the pane has to supply the scroller the boards own.
    expect(screen.getByTestId('audit-view').closest('.overflow-y-auto')).not.toBeNull();
  });

  it('roams tabs with the arrow keys (roving tabindex)', async () => {
    render(<GovernanceWorkspace />);
    const mcpTab = screen.getByRole('tab', { name: 'MCP servers' });
    const auditTab = screen.getByRole('tab', { name: 'Audit' });

    // ArrowLeft from the first tab wraps to the last (Audit).
    fireEvent.keyDown(mcpTab, { key: 'ArrowLeft' });
    await waitFor(() => {
      expect(screen.getByTestId('audit-view')).toBeTruthy();
    });
    expect(auditTab.getAttribute('aria-selected')).toBe('true');

    // ArrowRight wraps back to the first tab.
    fireEvent.keyDown(auditTab, { key: 'ArrowRight' });
    await waitFor(() => {
      expect(screen.getByTestId('mcp-board')).toBeTruthy();
    });
  });

  it('Home selects the first tab and End selects the last', async () => {
    render(<GovernanceWorkspace />);
    const mcpTab = screen.getByRole('tab', { name: 'MCP servers' });
    const auditTab = screen.getByRole('tab', { name: 'Audit' });

    fireEvent.keyDown(mcpTab, { key: 'End' });
    await waitFor(() => {
      expect(auditTab.getAttribute('aria-selected')).toBe('true');
    });

    fireEvent.keyDown(auditTab, { key: 'Home' });
    await waitFor(() => {
      expect(mcpTab.getAttribute('aria-selected')).toBe('true');
    });
  });
});
