import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import '../../i18n/i18n';
import { SkillDetail } from '../SkillDetail';
import type { SkillRow } from '../governanceApi';

// SkillDetail test — renders the detail component directly so every field label/value, the
// dash fallback for missing fields, the pending-note branch, and onClose are asserted (mutation
// hardening: the rendered copy + the present-vs-dash branches are the contract).

const FULL: SkillRow = {
  name: 'rich-skill',
  description: 'fully described',
  type: 'executable',
  language: 'go',
  contentHash: 'deadbeef',
};

describe('SkillDetail', () => {
  it('renders every field label and value for a fully-populated skill', () => {
    render(<SkillDetail skill={FULL} isPending={false} onClose={() => undefined} />);

    expect(screen.getByText('rich-skill')).toBeTruthy();
    expect(screen.getByText('Type')).toBeTruthy();
    expect(screen.getByText('executable')).toBeTruthy();
    expect(screen.getByText('Language')).toBeTruthy();
    expect(screen.getByText('go')).toBeTruthy();
    expect(screen.getByText('Content hash')).toBeTruthy();
    expect(screen.getByText('deadbeef')).toBeTruthy();
    expect(screen.getByText('Description')).toBeTruthy();
    expect(screen.getByText('fully described')).toBeTruthy();
    // No dash placeholder when every field is present.
    expect(screen.queryByText('—')).toBeNull();
  });

  it('renders the pending note only when isPending', () => {
    const { rerender } = render(<SkillDetail skill={FULL} isPending={false} onClose={() => undefined} />);
    expect(screen.queryByText('Pending — inactive and cannot be run.')).toBeNull();

    rerender(<SkillDetail skill={FULL} isPending={true} onClose={() => undefined} />);
    expect(screen.getByText('Pending — inactive and cannot be run.')).toBeTruthy();
  });

  it('renders dashes for missing type / language / content-hash / description', () => {
    const bare: SkillRow = { name: 'sparse', description: '', type: '' };
    render(<SkillDetail skill={bare} isPending={false} onClose={() => undefined} />);
    // Four fields all missing → four dash placeholders.
    expect(screen.getAllByText('—')).toHaveLength(4);
  });

  it('fires onClose when the close button is clicked', () => {
    const onClose = vi.fn();
    render(<SkillDetail skill={FULL} isPending={false} onClose={onClose} />);
    fireEvent.click(screen.getByRole('button', { name: 'Close' }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
