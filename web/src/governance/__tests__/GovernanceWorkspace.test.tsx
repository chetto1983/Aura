import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import '../../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys

// GovernanceWorkspace is the lazy tab-strip shell. The three boards are mocked (their data hooks
// are exercised by their own board tests) so this suite asserts ONLY the workspace obligations:
// the role=tablist tab strip renders (MCP/Skills/Scheduler), the read-only banner is present, and
// switching tabs swaps the active board. These are the GovernanceWorkspace behavior obligations
// from the plan acceptance criteria.

vi.mock('../McpBoard', () => ({
  McpBoard: () => <div data-testid="mcp-board">mcp</div>,
}));
vi.mock('../SkillsBoard', () => ({
  SkillsBoard: () => <div data-testid="skills-board">skills</div>,
}));
vi.mock('../SchedulerBoard', () => ({
  SchedulerBoard: () => <div data-testid="scheduler-board">scheduler</div>,
}));

const { default: GovernanceWorkspace } = await import('../GovernanceWorkspace');

describe('GovernanceWorkspace (boards mocked)', () => {
  it('renders the role=tablist tab strip with the three governance tabs', async () => {
    render(<GovernanceWorkspace />);

    const tablist = screen.getByRole('tablist', { name: 'Governance' });
    expect(tablist).toBeTruthy();

    expect(screen.getByRole('tab', { name: 'MCP servers' })).toBeTruthy();
    expect(screen.getByRole('tab', { name: 'Skills' })).toBeTruthy();
    expect(screen.getByRole('tab', { name: 'Scheduler' })).toBeTruthy();

    // The MCP board is the default-open tab.
    await waitFor(() => {
      expect(screen.getByTestId('mcp-board')).toBeTruthy();
    });
  });

  it('renders the persistent read-only banner', () => {
    render(<GovernanceWorkspace />);
    expect(
      screen.getByText('Read-only — viewing only. Changes arrive in a later phase.'),
    ).toBeTruthy();
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

  it('roams tabs with the arrow keys (roving tabindex)', async () => {
    render(<GovernanceWorkspace />);
    const mcpTab = screen.getByRole('tab', { name: 'MCP servers' });
    const schedulerTab = screen.getByRole('tab', { name: 'Scheduler' });

    // ArrowLeft from the first tab wraps to the last (Scheduler).
    fireEvent.keyDown(mcpTab, { key: 'ArrowLeft' });
    await waitFor(() => {
      expect(screen.getByTestId('scheduler-board')).toBeTruthy();
    });
    expect(schedulerTab.getAttribute('aria-selected')).toBe('true');

    // ArrowRight wraps back to the first tab.
    fireEvent.keyDown(schedulerTab, { key: 'ArrowRight' });
    await waitFor(() => {
      expect(screen.getByTestId('mcp-board')).toBeTruthy();
    });
  });

  it('Home selects the first tab and End selects the last', async () => {
    render(<GovernanceWorkspace />);
    const mcpTab = screen.getByRole('tab', { name: 'MCP servers' });
    const schedulerTab = screen.getByRole('tab', { name: 'Scheduler' });

    fireEvent.keyDown(mcpTab, { key: 'End' });
    await waitFor(() => {
      expect(schedulerTab.getAttribute('aria-selected')).toBe('true');
    });

    fireEvent.keyDown(schedulerTab, { key: 'Home' });
    await waitFor(() => {
      expect(mcpTab.getAttribute('aria-selected')).toBe('true');
    });
  });
});
