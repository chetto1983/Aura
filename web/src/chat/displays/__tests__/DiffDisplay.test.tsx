import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import '../../../i18n/i18n';
import { DiffDisplay } from '../DiffDisplay';
import type { DisplayDiff } from '../types';

const diff: DisplayDiff = {
  filename: 'a.txt',
  additions: 1,
  deletions: 1,
  lines: [
    { kind: 'removed', text: 'before' },
    { kind: 'added', text: '+++ header-like content' },
    { kind: 'context', text: '<img src=x>' },
  ],
};

describe('DiffDisplay', () => {
  it('shows filename, counts, line kinds, and inert content', () => {
    render(<DiffDisplay diff={diff} />);
    expect(screen.getByText('a.txt')).toBeTruthy();
    expect(screen.getByText('+1')).toBeTruthy();
    expect(screen.getByText('−1')).toBeTruthy();
    expect(screen.getByText('+++ header-like content')).toBeTruthy();
    expect(screen.getByText('<img src=x>')).toBeTruthy();
    expect(document.querySelector('img')).toBeNull();
  });

  it('copies the unified diff', () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    vi.stubGlobal('navigator', { clipboard: { writeText } });
    render(<DiffDisplay diff={diff} />);
    fireEvent.click(screen.getByRole('button', { name: 'Copy diff' }));
    expect(writeText).toHaveBeenCalledWith(expect.stringContaining('++++ header-like content'));
    vi.unstubAllGlobals();
  });
});
