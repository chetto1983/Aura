import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import '../../../i18n/i18n';
import { CompactionMarker } from '../CompactionMarker';
import type { CompactionState } from '../api';

// The marker exists because the boundary it draws was invisible: the ladder has been
// compacting at half the window since 2026-08-16, and the operator saw a full transcript with
// no way to tell which part of it the model could still read.

const COMPACTED: CompactionState = {
  covers_through_seq: 12,
  source_turns: 9,
  summary: 'The operator asked about skills; Aura listed them.',
  tokens_before: 41000,
  tokens_after: 2600,
};

describe('CompactionMarker', () => {
  it('says what the compaction did', () => {
    render(<CompactionMarker state={COMPACTED} />);

    expect(screen.getByText('Context compacted')).toBeTruthy();
    expect(screen.getByText(/9 earlier turns/)).toBeTruthy();
    expect(screen.getByText(/41k → 2.6k tokens/)).toBeTruthy();
  });

  // Machine-facing text, disclosed on demand: a page of summary rendered inline would bury
  // the turns the marker sits between.
  it('keeps the summary behind a disclosure', () => {
    render(<CompactionMarker state={COMPACTED} />);
    const toggle = screen.getByRole('button');

    expect(toggle.getAttribute('aria-expanded')).toBe('false');
    expect(screen.queryByText(COMPACTED.summary)).toBeNull();

    fireEvent.click(toggle);

    expect(toggle.getAttribute('aria-expanded')).toBe('true');
    expect(screen.getByText(COMPACTED.summary)).toBeTruthy();
    // The one thing an operator must not have to guess about a "compacted" conversation.
    expect(screen.getByText(/Every turn is still stored/)).toBeTruthy();

    fireEvent.click(toggle);
    expect(screen.queryByText(COMPACTED.summary)).toBeNull();
  });

  // The stored row does not remember the token counts, so a marker restored from a page
  // reload shows the boundary without inventing numbers for it.
  it('omits the token counts a re-read cannot know', () => {
    render(
      <CompactionMarker
        state={{ covers_through_seq: 12, source_turns: 9, summary: 'condensed' }}
      />,
    );

    expect(screen.queryByText(/tokens/)).toBeNull();
    expect(screen.getByText(/9 earlier turns/)).toBeTruthy();
  });

  it('offers no disclosure when there is no summary text to disclose', () => {
    render(<CompactionMarker state={{ covers_through_seq: 4, source_turns: 2, summary: '' }} />);

    expect(screen.getByRole('button').hasAttribute('disabled')).toBe(true);
  });
});
