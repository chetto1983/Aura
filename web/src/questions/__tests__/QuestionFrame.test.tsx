import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import '../../i18n/i18n';
import { CancelControl } from '../CancelControl';
import { QuestionCard } from '../QuestionCard';
import { QuestionOptions, type QuestionOption } from '../QuestionOptions';
import { QuestionReceipt } from '../QuestionReceipt';

const OPTIONS: QuestionOption[] = [
  { id: 'rome', label: 'Rome' },
  { id: 'milan', label: 'Milan', description: 'the north' },
  { id: 'turin', label: 'Turin' },
];

const LABELS = {
  cancel: 'Cancel run',
  confirm: 'Stop this run?',
  yes: 'Stop run',
  no: 'Keep running',
};

describe('QuestionCard', () => {
  it('is a form named by its title, with the description kept as plain, wrapped text', () => {
    render(
      <QuestionCard
        titleId="t"
        title="Pick a city"
        descriptionId="d"
        description={'line one\n<b>two</b>'}
      >
        <span>body</span>
      </QuestionCard>,
    );
    const form = screen.getByRole('form', { name: 'Pick a city' });
    expect(form.getAttribute('data-slot')).toBe('card');
    expect(form.getAttribute('data-variant')).toBe('default');
    expect(form.getAttribute('aria-describedby')).toBe('d');
    const description = screen.getByText((_, el) => el?.textContent === 'line one\n<b>two</b>');
    expect(description.className).toMatch(/whitespace-pre-wrap/);
    expect(form.querySelector('b')).toBeNull();
  });

  it('shows the step label and the segmented bar only on a form of more than one step', () => {
    const { rerender } = render(
      <QuestionCard titleId="t" title="One" step={{ current: 1, total: 1 }} />,
    );
    expect(screen.queryByRole('progressbar')).toBeNull();
    rerender(<QuestionCard titleId="t" title="Two" step={{ current: 2, total: 3 }} />);
    expect(screen.getByText('Step 2 of 3')).toBeTruthy();
    const bar = screen.getByRole('progressbar');
    expect(bar.getAttribute('aria-valuenow')).toBe('2');
    expect(bar.getAttribute('aria-valuemax')).toBe('3');
    expect(bar.querySelectorAll('.scale-x-100')).toHaveLength(2);
  });

  it('carries its variant and the data attributes an adapter asks for', () => {
    render(
      <QuestionCard
        titleId="t"
        title="Risky"
        variant="destructive"
        dataAttributes={{ 'data-approval-token': 'tok' }}
      />,
    );
    const form = screen.getByRole('form', { name: 'Risky' });
    expect(form.getAttribute('data-variant')).toBe('destructive');
    expect(form.getAttribute('data-approval-token')).toBe('tok');
  });
});

describe('QuestionOptions', () => {
  function renderOptions(mode: 'single' | 'multi', selected: ReadonlySet<string> = new Set()) {
    const onToggle = vi.fn();
    const onSubmit = vi.fn();
    render(
      <>
        <span id="lbl">Cities</span>
        <span id="hint">Pick one</span>
        <QuestionOptions
          labelledBy="lbl"
          describedBy="hint"
          options={OPTIONS}
          mode={mode}
          selected={selected}
          onToggle={onToggle}
          onSubmit={onSubmit}
        />
      </>,
    );
    return { onToggle, onSubmit };
  }

  it('is a listbox of options that marks the selected row', () => {
    renderOptions('multi', new Set(['milan']));
    const list = screen.getByRole('listbox', { name: 'Cities' });
    expect(list.getAttribute('aria-multiselectable')).toBe('true');
    expect(list.getAttribute('aria-describedby')).toBe('hint');
    expect(screen.getByRole('option', { name: /Milan/ }).getAttribute('aria-selected')).toBe(
      'true',
    );
    expect(screen.getByRole('option', { name: 'Rome' }).getAttribute('aria-selected')).toBe(
      'false',
    );
    expect(screen.getByText('the north')).toBeTruthy();
  });

  it('moves with the arrow keys, Home and End, and wraps', () => {
    renderOptions('single');
    const list = screen.getByRole('listbox');
    const [rome, milan, turin] = screen.getAllByRole('option');
    fireEvent.keyDown(list, { key: 'ArrowDown' });
    expect(document.activeElement).toBe(milan);
    fireEvent.keyDown(list, { key: 'End' });
    expect(document.activeElement).toBe(turin);
    fireEvent.keyDown(list, { key: 'ArrowDown' });
    expect(document.activeElement).toBe(rome);
    fireEvent.keyDown(list, { key: 'ArrowUp' });
    expect(document.activeElement).toBe(turin);
    fireEvent.keyDown(list, { key: 'Home' });
    expect(document.activeElement).toBe(rome);
  });

  it('selects with Space or Enter, and Enter on the chosen single row submits', () => {
    const { onToggle, onSubmit } = renderOptions('single', new Set(['rome']));
    const list = screen.getByRole('listbox');
    fireEvent.keyDown(list, { key: 'Enter' });
    expect(onSubmit).toHaveBeenCalledTimes(1);
    expect(onToggle).not.toHaveBeenCalled();
    fireEvent.keyDown(list, { key: 'ArrowDown' });
    fireEvent.keyDown(list, { key: ' ' });
    expect(onToggle).toHaveBeenCalledWith('milan');
    fireEvent.click(screen.getByRole('option', { name: 'Turin' }));
    expect(onToggle).toHaveBeenLastCalledWith('turin');
  });

  it('never submits from a multi-choice list', () => {
    const { onToggle, onSubmit } = renderOptions('multi', new Set(['rome']));
    fireEvent.keyDown(screen.getByRole('listbox'), { key: 'Enter' });
    expect(onToggle).toHaveBeenCalledWith('rome');
    expect(onSubmit).not.toHaveBeenCalled();
  });
});

describe('QuestionReceipt', () => {
  it('shows the chip in its tone and the answer given', () => {
    render(
      <QuestionReceipt
        tone="success"
        label="Answered."
        summary={[{ label: 'Your answer', value: 'Milan' }]}
      />,
    );
    expect(screen.getByText('Answered.').closest('[data-tone]')?.getAttribute('data-tone')).toBe(
      'success',
    );
    expect(screen.getByText('Your answer')).toBeTruthy();
    expect(screen.getByText('Milan')).toBeTruthy();
  });

  it('announces itself only when asked to', () => {
    const { rerender } = render(<QuestionReceipt tone="warning" label="Expired." />);
    expect(screen.queryByRole('status')).toBeNull();
    rerender(<QuestionReceipt tone="warning" label="Expired." announce />);
    expect(screen.getByRole('status').textContent).toContain('Expired.');
  });
});

describe('CancelControl', () => {
  it('cancels at once while idle', () => {
    const onCancel = vi.fn();
    render(
      <CancelControl isStreaming={false} disabled={false} labels={LABELS} onCancel={onCancel} />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Cancel run' }));
    expect(onCancel).toHaveBeenCalledTimes(1);
  });

  it('asks first while streaming, and Escape keeps the run going', async () => {
    const onCancel = vi.fn();
    render(<CancelControl isStreaming disabled={false} labels={LABELS} onCancel={onCancel} />);
    fireEvent.click(screen.getByRole('button', { name: 'Cancel run' }));
    const keep = screen.getByRole('button', { name: 'Keep running' });
    await waitFor(() => {
      expect(document.activeElement).toBe(keep);
    });
    expect(keep.className).toContain('min-h-11');
    fireEvent.keyDown(keep, { key: 'Escape' });
    await waitFor(() => {
      expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Cancel run' }));
    });
    expect(onCancel).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: 'Cancel run' }));
    fireEvent.click(screen.getByRole('button', { name: 'Stop run' }));
    expect(onCancel).toHaveBeenCalledTimes(1);
  });
});
