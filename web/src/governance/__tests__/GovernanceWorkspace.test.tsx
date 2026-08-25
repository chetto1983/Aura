import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import '../../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys

// GovernanceWorkspace is the lazy rail + pane shell. The four panes are mocked (their data
// hooks are exercised by their own tests) so this suite asserts ONLY the workspace
// obligations: the shared SectionRail renders the four sections, the obsolete read-only
// banner is gone, and selecting a section swaps the mounted board. The rail replaced a
// four-column tablist that truncated every label; the two-layout contract (strip above the
// pane below `lg`, sidebar column from `lg` up) is asserted here and in SectionRail's suite.

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
  it('renders the section rail with the four governance boards', async () => {
    render(<GovernanceWorkspace />);

    const rail = screen.getByRole('navigation', { name: 'Governance sections' });
    // One rail, two layouts: a scrollable strip below `lg`, a sidebar column from `lg` up.
    expect(rail.className).toContain('overflow-x-auto');
    expect(rail.className).toContain('lg:w-[var(--rail-w)]');
    expect(rail.className).toContain('lg:flex-col');
    // The rail is drag-resizable like the chat sidebar — Governance needs it most, the boards
    // beside it are already master + detail.
    expect(screen.getByRole('separator', { name: 'Resize the sections rail' })).toBeTruthy();

    for (const name of ['MCP servers', 'Skills', 'Scheduler', 'Audit']) {
      const item = screen.getByRole('button', { name });
      expect(item.getAttribute('data-slot')).toBe('button');
      // Labels keep their full width on the strip — the old grid-cols-4 tablist clipped them.
      expect(item.className).toContain('whitespace-nowrap');
      expect(item.querySelector('svg[data-icon="inline-start"]')).not.toBeNull();
    }

    // The MCP board is the default-open section.
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

  it('marks the active section aria-current and swaps the board on click', async () => {
    render(<GovernanceWorkspace />);

    const mcp = screen.getByRole('button', { name: 'MCP servers' });
    const skills = screen.getByRole('button', { name: 'Skills' });
    expect(mcp.getAttribute('aria-current')).toBe('page');
    expect(skills.getAttribute('aria-current')).toBeNull();

    fireEvent.click(skills);

    await waitFor(() => {
      expect(screen.getByTestId('skills-board')).toBeTruthy();
    });
    expect(skills.getAttribute('aria-current')).toBe('page');
    expect(mcp.getAttribute('aria-current')).toBeNull();
    // Only the selected board is mounted — the rail is what keeps the other three unmounted.
    expect(screen.queryByTestId('mcp-board')).toBeNull();
  });

  it('opens the scheduler board from the rail', async () => {
    render(<GovernanceWorkspace />);

    fireEvent.click(screen.getByRole('button', { name: 'Scheduler' }));

    await waitFor(() => {
      expect(screen.getByTestId('scheduler-board')).toBeTruthy();
    });
  });

  it('opens the per-identity audit feed on its own section', async () => {
    render(<GovernanceWorkspace />);

    fireEvent.click(screen.getByRole('button', { name: 'Audit' }));

    await waitFor(() => {
      expect(screen.getByTestId('audit-view')).toBeTruthy();
    });
    // The feed is a plain section, so the pane has to supply the scroller the boards own.
    expect(screen.getByTestId('audit-view').closest('.overflow-y-auto')).not.toBeNull();
  });
});
