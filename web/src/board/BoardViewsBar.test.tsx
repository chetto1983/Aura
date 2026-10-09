import { act, fireEvent, render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { BoardColumn, BoardFilters, BoardView } from './boardApi';
import { BoardColumnsDialog } from './BoardColumnsDialog';
import { BoardViewsBar } from './BoardViewsBar';
import i18n from '@/i18n/i18n';

const VIEWS: BoardView[] = [
  { id: 'v-1', name: 'urgent', filters: { priority: 3 }, pinned: false },
  { id: 'v-2', name: 'from chat', filters: { source: 'chat' }, pinned: true },
];

function renderBar(filters: BoardFilters = {}, activeViewId = '') {
  const handlers = {
    onFiltersChange: vi.fn(),
    onSelectView: vi.fn(),
    onSaveView: vi.fn(),
    onTogglePin: vi.fn(),
    onDeleteView: vi.fn(),
  };
  render(
    <BoardViewsBar
      filters={filters}
      tags={['billing', 'ops']}
      views={VIEWS}
      activeViewId={activeViewId}
      {...handlers}
    />,
  );
  return handlers;
}

beforeEach(async () => {
  await act(() => i18n.changeLanguage('en'));
});

describe('BoardViewsBar', () => {
  it('lists pinned views first and marks the active one', () => {
    renderBar({ source: 'chat' }, 'v-2');
    const [first] = screen.getAllByRole('button', { name: /^(urgent|from chat)$/ });
    expect(first?.textContent).toBe('from chat');
    expect(screen.getByRole('button', { name: 'from chat' }).getAttribute('aria-pressed')).toBe(
      'true',
    );
    expect(screen.getByRole('button', { name: 'All cards' }).getAttribute('aria-pressed')).toBe(
      'false',
    );
  });

  it('turns every control into a filter change', () => {
    const { onFiltersChange } = renderBar({ source: 'chat' });
    fireEvent.change(screen.getByRole('combobox', { name: 'Priority' }), {
      target: { value: '3' },
    });
    expect(onFiltersChange).toHaveBeenLastCalledWith({ source: 'chat', priority: 3 });
    fireEvent.change(screen.getByRole('combobox', { name: 'Due date' }), {
      target: { value: 'overdue' },
    });
    expect(onFiltersChange).toHaveBeenLastCalledWith({ source: 'chat', due: 'overdue' });
    fireEvent.change(screen.getByRole('combobox', { name: 'Tag' }), { target: { value: 'ops' } });
    expect(onFiltersChange).toHaveBeenLastCalledWith({ source: 'chat', tag: 'ops' });
    fireEvent.change(screen.getByRole('combobox', { name: 'Source' }), { target: { value: '' } });
    expect(onFiltersChange).toHaveBeenLastCalledWith({ source: undefined });
    fireEvent.change(screen.getByRole('combobox', { name: 'Priority' }), { target: { value: '' } });
    expect(onFiltersChange).toHaveBeenLastCalledWith({ source: 'chat', priority: undefined });
  });

  it('saves the filters on screen under a name', () => {
    const { onSaveView } = renderBar({ tag: 'ops' });
    fireEvent.click(screen.getByRole('button', { name: 'Save as view' }));
    const save = screen.getByRole('button', { name: 'Save' });
    expect(save).toHaveProperty('disabled', true);
    fireEvent.change(screen.getByRole('textbox', { name: 'View name' }), {
      target: { value: ' ops work ' },
    });
    fireEvent.click(save);
    expect(onSaveView).toHaveBeenCalledWith('ops work');
    expect(screen.queryByRole('textbox', { name: 'View name' })).toBeNull();
  });

  it('offers no save while nothing is filtered, and a naming can be cancelled', () => {
    renderBar();
    expect(screen.queryByRole('button', { name: 'Save as view' })).toBeNull();
    renderBar({ priority: 1 });
    fireEvent.click(screen.getByRole('button', { name: 'Save as view' }));
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(screen.queryByRole('textbox', { name: 'View name' })).toBeNull();
  });

  it('selects, pins and deletes a view', () => {
    const { onSelectView, onTogglePin, onDeleteView } = renderBar();
    fireEvent.click(screen.getByRole('button', { name: 'urgent' }));
    expect(onSelectView).toHaveBeenCalledWith(VIEWS[0]);
    fireEvent.click(screen.getByRole('button', { name: 'All cards' }));
    expect(onSelectView).toHaveBeenLastCalledWith(null);
    fireEvent.click(screen.getByRole('button', { name: 'Pin urgent' }));
    expect(onTogglePin).toHaveBeenCalledWith(VIEWS[0]);
    fireEvent.click(screen.getByRole('button', { name: 'Unpin from chat' }));
    expect(onTogglePin).toHaveBeenLastCalledWith(VIEWS[1]);
    fireEvent.click(screen.getByRole('button', { name: 'Delete the view urgent' }));
    expect(onDeleteView).toHaveBeenCalledWith(VIEWS[0]);
  });
});

describe('BoardColumnsDialog', () => {
  const COLUMNS = [
    { id: 'todo', label: 'To do' },
    { id: 'doing', label: 'Doing', cardLimit: 5 },
  ];

  function renderDialog(error = '') {
    const onSave = vi.fn((_columns: BoardColumn[]) => Promise.resolve());
    const onClose = vi.fn();
    render(
      <BoardColumnsDialog
        columns={COLUMNS}
        onClose={onClose}
        onSave={onSave}
        error={error}
        saving={false}
      />,
    );
    return { onSave, onClose };
  }

  it('reorders, removes and adds columns, and saves the array in order', () => {
    const { onSave } = renderDialog();
    fireEvent.click(screen.getByRole('button', { name: 'Move column 2 up' }));
    fireEvent.click(screen.getByRole('button', { name: 'Add a column' }));
    fireEvent.change(screen.getByRole('textbox', { name: 'Name of column 3' }), {
      target: { value: 'Waiting' },
    });
    fireEvent.change(screen.getByRole('textbox', { name: 'Card limit of column 3' }), {
      target: { value: '2x' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Move column 1 down' }));
    fireEvent.click(screen.getByRole('button', { name: 'Remove column 1' }));
    fireEvent.click(screen.getByRole('button', { name: 'Save columns' }));
    const saved = onSave.mock.calls[0]?.[0] ?? [];
    expect(saved.map((c) => c.label)).toEqual(['Doing', 'Waiting']);
    expect(saved[0]).toEqual({ id: 'doing', label: 'Doing', cardLimit: 5 });
    expect(saved[1]?.cardLimit).toBe(2);
  });

  it('refuses a nameless column and shows what the server said', () => {
    const { onClose } = renderDialog(
      'A column that still holds cards cannot be removed: move them first.',
    );
    expect(screen.getByRole('alert').textContent).toContain('still holds cards');
    fireEvent.change(screen.getByRole('textbox', { name: 'Name of column 1' }), {
      target: { value: '  ' },
    });
    expect(screen.getByRole('button', { name: 'Save columns' })).toHaveProperty('disabled', true);
    expect(screen.getByRole('button', { name: 'Move column 1 up' })).toHaveProperty(
      'disabled',
      true,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(onClose).toHaveBeenCalled();
  });
});
