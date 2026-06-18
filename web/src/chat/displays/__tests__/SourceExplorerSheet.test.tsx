import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, within } from '@testing-library/react';
import '../../../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys
import { SourceExplorerSheet } from '../SourceExplorerSheet';
import {
  anyIncomplete,
  filterAndSortSources,
  nextExplorerSort,
  sourceIncomplete,
} from '../sourceExplorerData';
import type { DisplaySource } from '../types';

// SourceExplorerSheet (D-03 / DISP-05): the READ-ONLY evidence dossier. Tests cover
// the three view-only tabs, the Table sort/search/paginate, the read-only NEGATIVE
// (no PATCH/destructive control in Metadata/Configuration — T-26-19), the warning
// banner for an incomplete source, and the sheet a11y (role=dialog, Esc, title).

interface SourceOverride {
  ref_id: string;
  index: number;
  title?: string;
  url?: string | undefined;
  cited?: boolean;
  type?: DisplaySource['type'];
  snippet?: string;
  confidence?: number;
}

function src(over: SourceOverride): DisplaySource {
  const base: { -readonly [K in keyof DisplaySource]: DisplaySource[K] } = {
    ref_id: over.ref_id,
    index: over.index,
    type: over.type ?? 'web_result',
    title: over.title ?? `Source ${String(over.index)}`,
    url: 'url' in over ? over.url ?? '' : `https://example${String(over.index)}.com/a`,
    snippet: over.snippet ?? 'snippet',
    cited: over.cited ?? false,
    ...(over.confidence !== undefined ? { confidence: over.confidence } : {}),
  };
  // Drop an empty url so "incomplete" tests see a missing url, not an empty string
  // that still counts as present in some checks (sourceIncomplete treats '' as missing).
  return base;
}

const SOURCES: readonly DisplaySource[] = [
  src({ ref_id: 'src-1', index: 1, title: 'Alpha', cited: true, url: 'https://alpha.test/x' }),
  src({ ref_id: 'src-2', index: 2, title: 'Bravo', cited: false, url: 'https://bravo.test/y' }),
  src({ ref_id: 'src-3', index: 3, title: 'Charlie', cited: false, url: 'https://charlie.test/z' }),
];

describe('filterAndSortSources (pure)', () => {
  it('returns all sources when the query is empty', () => {
    expect(filterAndSortSources(SOURCES, '', null)).toHaveLength(3);
  });

  it('filters by title/url/snippet/type/ref_id (case-insensitive)', () => {
    expect(filterAndSortSources(SOURCES, 'bravo', null).map((s) => s.ref_id)).toEqual(['src-2']);
    expect(filterAndSortSources(SOURCES, 'ALPHA.TEST', null).map((s) => s.ref_id)).toEqual(['src-1']);
    expect(filterAndSortSources(SOURCES, 'src-3', null).map((s) => s.ref_id)).toEqual(['src-3']);
  });

  it('sorts ascending and descending by title', () => {
    const asc = filterAndSortSources(SOURCES, '', { key: 'title', dir: 'asc' });
    expect(asc.map((s) => s.title)).toEqual(['Alpha', 'Bravo', 'Charlie']);
    const desc = filterAndSortSources(SOURCES, '', { key: 'title', dir: 'desc' });
    expect(desc.map((s) => s.title)).toEqual(['Charlie', 'Bravo', 'Alpha']);
  });

  it('sorts by status (cited first when ascending)', () => {
    const asc = filterAndSortSources(SOURCES, '', { key: 'status', dir: 'asc' });
    expect(asc[0]?.cited).toBe(true);
  });
});

describe('nextExplorerSort (pure)', () => {
  it('starts ascending on a new column, toggles asc↔desc on the same column', () => {
    expect(nextExplorerSort(null, 'title')).toEqual({ key: 'title', dir: 'asc' });
    expect(nextExplorerSort({ key: 'title', dir: 'asc' }, 'title')).toEqual({
      key: 'title',
      dir: 'desc',
    });
    expect(nextExplorerSort({ key: 'title', dir: 'desc' }, 'index')).toEqual({
      key: 'index',
      dir: 'asc',
    });
  });
});

describe('sourceIncomplete / anyIncomplete (pure)', () => {
  it('flags a source missing a title or url as incomplete', () => {
    expect(sourceIncomplete(src({ ref_id: 'a', index: 1, title: '' }))).toBe(true);
    expect(sourceIncomplete(src({ ref_id: 'b', index: 2, url: undefined }))).toBe(true);
    expect(sourceIncomplete(src({ ref_id: 'c', index: 3, title: 'Full', url: 'https://x.test' }))).toBe(
      false,
    );
  });

  it('anyIncomplete is true when at least one source is incomplete', () => {
    expect(anyIncomplete(SOURCES)).toBe(false);
    expect(anyIncomplete([...SOURCES, src({ ref_id: 'x', index: 4, url: undefined })])).toBe(true);
  });
});

describe('SourceExplorerSheet (render, D-03 / DISP-05)', () => {
  it('renders nothing when closed', () => {
    const { container } = render(
      <SourceExplorerSheet open={false} sources={SOURCES} onClose={vi.fn()} />,
    );
    expect(container.querySelector('[role="dialog"]')).toBeNull();
  });

  it('renders the three read-only tabs — Table / Metadata / Configuration', () => {
    render(<SourceExplorerSheet open sources={SOURCES} onClose={vi.fn()} />);
    expect(screen.getByRole('tab', { name: 'Table' })).toBeTruthy();
    expect(screen.getByRole('tab', { name: 'Metadata' })).toBeTruthy();
    expect(screen.getByRole('tab', { name: 'Configuration' })).toBeTruthy();
  });

  it('is a role=dialog with aria-modal and the 18px Fraunces title', () => {
    render(<SourceExplorerSheet open sources={SOURCES} onClose={vi.fn()} />);
    const dialog = screen.getByRole('dialog');
    expect(dialog.getAttribute('aria-modal')).toBe('true');
    const heading = within(dialog).getByRole('heading', { level: 2 });
    expect(heading.className).toContain('font-display');
    expect(heading.className).toContain('text-lg');
  });

  it('Table view renders every source row with a Cited/Consulted status tag', () => {
    render(<SourceExplorerSheet open sources={SOURCES} onClose={vi.fn()} />);
    expect(screen.getByText('Alpha')).toBeTruthy();
    expect(screen.getByText('Bravo')).toBeTruthy();
    expect(screen.getByText('Cited')).toBeTruthy();
    expect(screen.getAllByText('Consulted').length).toBeGreaterThan(0);
  });

  it('Table search narrows the visible rows', () => {
    render(<SourceExplorerSheet open sources={SOURCES} onClose={vi.fn()} />);
    const search = screen.getByPlaceholderText('Search sources');
    fireEvent.change(search, { target: { value: 'bravo' } });
    expect(screen.getByText('Bravo')).toBeTruthy();
    expect(screen.queryByText('Alpha')).toBeNull();
  });

  it('Table sort header toggles aria-sort', () => {
    render(<SourceExplorerSheet open sources={SOURCES} onClose={vi.fn()} />);
    const titleHeader = screen.getByRole('button', { name: 'Sort by Title' });
    fireEvent.click(titleHeader);
    const th = titleHeader.closest('th');
    expect(th?.getAttribute('aria-sort')).toBe('ascending');
    fireEvent.click(titleHeader);
    expect(th?.getAttribute('aria-sort')).toBe('descending');
  });

  it('Table paginates beyond 9 sources', () => {
    const many: DisplaySource[] = Array.from({ length: 12 }, (_, i) =>
      src({ ref_id: `s-${String(i)}`, index: i + 1, title: `Row ${String(i + 1)}` }),
    );
    render(<SourceExplorerSheet open sources={many} onClose={vi.fn()} />);
    expect(screen.getByText('Row 1')).toBeTruthy();
    expect(screen.queryByText('Row 12')).toBeNull(); // page 1 only shows 9
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }));
    expect(screen.getByText('Row 12')).toBeTruthy();
  });

  it('clicking a source row opens its Metadata (read-only fields)', () => {
    render(<SourceExplorerSheet open sources={SOURCES} onClose={vi.fn()} />);
    fireEvent.click(screen.getByRole('button', { name: 'Show details for source 1' }));
    // Metadata view shows the ref id + url, read-only.
    expect(screen.getByText('src-1')).toBeTruthy();
    expect(screen.getByText('https://alpha.test/x')).toBeTruthy();
  });

  it('Metadata and Configuration contain NO PATCH/destructive control (D-03 read-only, T-26-19)', () => {
    render(<SourceExplorerSheet open sources={SOURCES} onClose={vi.fn()} />);
    fireEvent.click(screen.getByRole('tab', { name: 'Metadata' }));
    fireEvent.click(screen.getByRole('tab', { name: 'Configuration' }));
    const forbidden = [/re-?analyze/i, /\bclear\b/i, /\bsave\b/i, /\bdelete\b/i, /\bedit\b/i, /\bapply\b/i];
    const buttons = screen.getAllByRole('button');
    for (const button of buttons) {
      const name = button.textContent ?? '';
      const label = button.getAttribute('aria-label') ?? '';
      for (const re of forbidden) {
        expect(re.test(name)).toBe(false);
        expect(re.test(label)).toBe(false);
      }
    }
    // No textbox/form input in the read-only Configuration view.
    fireEvent.click(screen.getByRole('tab', { name: 'Configuration' }));
    expect(screen.queryByRole('textbox')).toBeNull();
  });

  it('shows the warning banner when a source is incomplete', () => {
    const withIncomplete = [...SOURCES, src({ ref_id: 'inc', index: 4, url: undefined })];
    render(<SourceExplorerSheet open sources={withIncomplete} onClose={vi.fn()} />);
    expect(
      screen.getByText('Some sources are incomplete or were not fully processed.'),
    ).toBeTruthy();
  });

  it('hides the warning banner when every source is complete', () => {
    render(<SourceExplorerSheet open sources={SOURCES} onClose={vi.fn()} />);
    expect(
      screen.queryByText('Some sources are incomplete or were not fully processed.'),
    ).toBeNull();
  });

  it('renders the empty state when there are no sources', () => {
    render(<SourceExplorerSheet open sources={[]} onClose={vi.fn()} />);
    expect(screen.getByText('No sources')).toBeTruthy();
    expect(screen.getByText("This answer didn't consult any external sources.")).toBeTruthy();
  });

  it('Esc closes the sheet', () => {
    const onClose = vi.fn();
    render(<SourceExplorerSheet open sources={SOURCES} onClose={onClose} />);
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('traps Tab focus inside the dialog (wraps last→first and first→last)', () => {
    render(<SourceExplorerSheet open sources={SOURCES} onClose={vi.fn()} />);
    const dialog = screen.getByRole('dialog');
    const focusables = within(dialog).getAllByRole('button');
    const first = focusables[0];
    const last = focusables[focusables.length - 1];
    expect(first).toBeTruthy();
    expect(last).toBeTruthy();
    // Forward wrap: Tab from the last focusable returns to the first.
    last?.focus();
    fireEvent.keyDown(document, { key: 'Tab' });
    expect(document.activeElement).toBe(first);
    // Backward wrap: Shift+Tab from the first focusable jumps to the last.
    first?.focus();
    fireEvent.keyDown(document, { key: 'Tab', shiftKey: true });
    expect(document.activeElement).toBe(last);
  });

  it('a non-trap key (e.g. Tab in the middle) does not change activeElement', () => {
    render(<SourceExplorerSheet open sources={SOURCES} onClose={vi.fn()} />);
    const buttons = within(screen.getByRole('dialog')).getAllByRole('button');
    const middle = buttons[1];
    middle?.focus();
    fireEvent.keyDown(document, { key: 'Tab' });
    // Browser default Tab movement is not simulated by jsdom; the trap only acts at
    // the boundaries, so a mid-list Tab leaves focus on the same element.
    expect(document.activeElement).toBe(middle);
  });

  it('a descending sort renders the ↓ arrow indicator', () => {
    render(<SourceExplorerSheet open sources={SOURCES} onClose={vi.fn()} />);
    const titleHeader = screen.getByRole('button', { name: 'Sort by Title' });
    fireEvent.click(titleHeader); // asc
    expect(within(titleHeader).getByText('↑')).toBeTruthy();
    fireEvent.click(titleHeader); // desc
    expect(within(titleHeader).getByText('↓')).toBeTruthy();
  });

  it('the close controls (backdrop + header button) both fire onClose', () => {
    const onClose = vi.fn();
    render(<SourceExplorerSheet open sources={SOURCES} onClose={onClose} />);
    // Two controls carry the "Close sources" label: the backdrop overlay and the
    // header close button. Both must dismiss the read-only sheet.
    const closers = screen.getAllByRole('button', { name: 'Close sources' });
    expect(closers.length).toBe(2);
    for (const closer of closers) fireEvent.click(closer);
    expect(onClose).toHaveBeenCalledTimes(2);
  });

  it('opening with a focusRefId selects that source in Metadata', () => {
    render(<SourceExplorerSheet open sources={SOURCES} focusRefId="src-2" onClose={vi.fn()} />);
    fireEvent.click(screen.getByRole('tab', { name: 'Metadata' }));
    expect(screen.getByText('src-2')).toBeTruthy();
  });

  it('a focusRefId not in the registry focuses nothing (T-26-21)', () => {
    render(<SourceExplorerSheet open sources={SOURCES} focusRefId="ghost" onClose={vi.fn()} />);
    fireEvent.click(screen.getByRole('tab', { name: 'Metadata' }));
    // The "select a source" prompt shows — no fabricated target rendered.
    expect(
      screen.getByText('Select a source in the Table view to inspect its metadata.'),
    ).toBeTruthy();
  });

  it('Configuration view reports cited/consulted counts (read-only)', () => {
    render(<SourceExplorerSheet open sources={SOURCES} onClose={vi.fn()} />);
    fireEvent.click(screen.getByRole('tab', { name: 'Configuration' }));
    expect(screen.getByText('Sources consulted')).toBeTruthy();
    expect(screen.getByText('Sources cited')).toBeTruthy();
  });
});
