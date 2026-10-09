import { act, fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import type { KanbanCard } from '@svar-ui/react-kanban';
import { BoardCardContent } from './BoardCardContent';
import { BoardCardContext } from './boardCardContext';
import { TagsEditorItem } from './TagsEditorItem';
import i18n from '@/i18n/i18n';

const NOW = new Date('2026-10-09T12:00:00Z');

function renderCard(
  card: Partial<KanbanCard>,
  taskStatus: (id: string) => string | undefined = () => undefined,
) {
  return render(
    <BoardCardContext.Provider value={{ taskStatus, now: NOW }}>
      <BoardCardContent
        card={{
          id: 'c-1',
          label: 'Pay the invoice',
          description: '',
          column: 'todo',
          priority: 3,
          tags: [],
          source: 'chat',
          updated_by: 'agent',
          updated_at: '2026-10-09T10:00:00Z',
          ...card,
        }}
      />
    </BoardCardContext.Provider>,
  );
}

describe('BoardCardContent', () => {
  it('says where the card came from, and keeps the widget menu reachable', async () => {
    await act(() => i18n.changeLanguage('en'));
    const { container } = renderCard({});
    expect(screen.getByText('Pay the invoice')).toBeTruthy();
    expect(screen.getByText('Aura in chat')).toBeTruthy();
    expect(screen.getByRole('img', { name: 'High priority' })).toBeTruthy();
    // The widget's context menu finds its trigger by this attribute, nothing else.
    expect(container.querySelector('[data-action="menu"]')?.getAttribute('aria-label')).toBe(
      'Card menu',
    );
  });

  it('marks a passed due date, the conversation and the task status', async () => {
    await act(() => i18n.changeLanguage('en'));
    renderCard(
      {
        deadline: new Date('2026-10-08T09:00:00Z'),
        conversation_id: 'conv-1',
        task_id: 'task-1',
        tags: ['ops'],
        description: 'before Friday',
      },
      (id) => (id === 'task-1' ? 'active' : undefined),
    );
    expect(screen.getByText('overdue')).toBeTruthy();
    expect(screen.getAllByText('Linked to a conversation').length).toBeGreaterThan(0);
    expect(screen.getByText('Task: active')).toBeTruthy();
    expect(screen.getByText('ops')).toBeTruthy();
    expect(screen.getByText('before Friday')).toBeTruthy();
  });

  it('reads a task it cannot see as scheduled, not as missing', async () => {
    await act(() => i18n.changeLanguage('en'));
    renderCard({ task_id: 'task-9' });
    expect(screen.getByText('Task: scheduled')).toBeTruthy();
    expect(screen.queryByText('overdue')).toBeNull();
  });
});

describe('TagsEditorItem', () => {
  it('keeps the text as typed and hands over the parsed tags', () => {
    const onChange = vi.fn();
    render(<TagsEditorItem value={['ops']} onChange={onChange} />);
    const input = screen.getByRole('textbox');
    expect((input as HTMLInputElement).value).toBe('ops');
    fireEvent.change(input, { target: { value: 'ops, billing,' } });
    expect((input as HTMLInputElement).value).toBe('ops, billing,');
    expect(onChange).toHaveBeenLastCalledWith({ value: ['ops', 'billing'] });
  });

  it('shows the tags of the card the editor hands over after mounting, and of the next one', () => {
    const onChange = vi.fn();
    const { rerender } = render(<TagsEditorItem onChange={onChange} />);
    const input = screen.getByRole('textbox') as HTMLInputElement;
    expect(input.value).toBe('');
    rerender(<TagsEditorItem value={['billing', 'admin']} onChange={onChange} />);
    expect(input.value).toBe('billing, admin');
    fireEvent.change(input, { target: { value: 'billing, admin, ' } });
    rerender(<TagsEditorItem value={['billing', 'admin']} onChange={onChange} />);
    expect(input.value).toBe('billing, admin, ');
    rerender(<TagsEditorItem value={['ops']} onChange={onChange} />);
    expect(input.value).toBe('ops');
  });
});
