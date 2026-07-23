import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, within } from '@testing-library/react';
import '../../i18n/i18n';
import { ToolGroup, type ToolGroupMember } from '../ToolGroup';

// ToolGroup (compact-chat spec §3.3 / AC-10): one header for a settled run —
// rollup dot, "N tools" count, wall-clock span, two-level disclosure, a11y.

function member(id: string, over: Partial<ToolGroupMember> = {}): ToolGroupMember {
  return {
    toolCallId: id,
    toolName: `tool_${id}`,
    argsText: '{"query":"x"}',
    result: `result ${id}`,
    ...over,
  };
}

describe('ToolGroup (AC-10)', () => {
  it('renders ONE collapsed header with the member count and no individual rows', () => {
    render(<ToolGroup members={[member('a'), member('b'), member('c')]} />);
    const group = screen.getByTestId('tool-group');
    expect(within(group).getByText('3 tools')).toBeTruthy();
    expect(screen.getByTestId('tool-group-body').hidden).toBe(true);
    // Member rows exist only inside the hidden body — nothing renders as a
    // sibling row while collapsed.
    const header = screen.getByRole('button', { name: 'Grouped tool activity' });
    expect(header.getAttribute('aria-expanded')).toBe('false');
    expect(header.getAttribute('aria-controls')).toBe(screen.getByTestId('tool-group-body').id);
  });

  it('shows the wall-clock span from first member start to last member finish', () => {
    render(
      <ToolGroup
        members={[member('a', { startedAt: 1000 }), member('b'), member('c', { finishedAt: 8500 })]}
      />,
    );
    expect(screen.getByTestId('tool-group-elapsed').textContent).toBe('7.5 s');
  });

  it('omits the span when timestamps are missing', () => {
    render(<ToolGroup members={[member('a'), member('b'), member('c')]} />);
    expect(screen.queryByTestId('tool-group-elapsed')).toBeNull();
  });

  it('rolls up the dot to danger when ANY member errored', () => {
    const { container, rerender } = render(
      <ToolGroup members={[member('a'), member('b'), member('c')]} />,
    );
    expect(container.querySelector('.bg-success')).not.toBeNull();
    rerender(<ToolGroup members={[member('a'), member('b', { isError: true }), member('c')]} />);
    const group = screen.getByTestId('tool-group');
    expect(group.className).toContain('border-danger/40');
    expect(group.querySelector(':scope > button .bg-danger')).not.toBeNull();
  });

  it('forwards a member display citation click to onOpenSource with that display', () => {
    const onOpenSource = vi.fn();
    const display = {
      type: 'document' as const,
      tool_call_id: 'b',
      document: { content_md: 'A claim [1].' },
      sources: [
        {
          ref_id: 'src-1',
          index: 1,
          type: 'document' as const,
          title: 'Cited',
          url: 'https://x.test',
          cited: true,
        },
      ],
    };
    render(
      <ToolGroup
        members={[member('a'), member('b', { display }), member('c')]}
        onOpenSource={onOpenSource}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Grouped tool activity' }));
    fireEvent.click(screen.getByRole('button', { name: 'tool_b activity' }));
    fireEvent.click(screen.getByRole('button', { name: /Source 1/ }));
    expect(onOpenSource).toHaveBeenCalledWith(display, 'src-1');
  });

  it('two-level disclosure: expanding the group reveals member rows, each individually expandable', () => {
    render(<ToolGroup members={[member('a'), member('b'), member('c')]} />);
    fireEvent.click(screen.getByRole('button', { name: 'Grouped tool activity' }));
    const body = screen.getByTestId('tool-group-body');
    expect(body.hidden).toBe(false);
    expect(body.className).toContain('space-y-1'); // §4 tight member rhythm
    const rows = within(body).getAllByTestId('tool-row');
    expect(rows).toHaveLength(3);
    // Second level: expand one member → its Request/Result panel appears.
    fireEvent.click(within(body).getByRole('button', { name: 'tool_b activity' }));
    expect(within(body).getByText('result b')).toBeTruthy();
  });
});
