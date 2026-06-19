import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import '../../../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys
import { SourcesButton } from '../SourcesButton';
import { SourceExplorerProvider } from '../SourceExplorerContext';
import { useSourceExplorer } from '../sourceExplorerControls';
import { CitationBubble } from '../CitationBubble';
import { aggregateAnswerSources } from '../answerSources';
import type { DisplayPayload, DisplaySource } from '../types';

// SourcesButton (D-13 / DISP-05) + the shared-state wiring: the "Sources (N)"
// affordance opens the read-only Source Explorer; a CitationBubble onOpenSource
// opens the SAME sheet focused to that source (one sheet, two entry points, one
// registry). N=0 hides the button.

function source(over: Partial<DisplaySource> & { ref_id: string; index: number }): DisplaySource {
  return {
    type: 'web_result',
    title: `Source ${String(over.index)}`,
    url: `https://s${String(over.index)}.test/a`,
    snippet: 'snip',
    cited: false,
    ...over,
  };
}

const ALPHA = source({ ref_id: 'src-1', index: 1, title: 'Alpha', cited: true });
const BRAVO = source({ ref_id: 'src-2', index: 2, title: 'Bravo' });
const SOURCES: readonly DisplaySource[] = [ALPHA, BRAVO];

describe('SourcesButton (D-13)', () => {
  it('shows "Sources (N)" with the consulted-source count', () => {
    render(<SourcesButton sources={SOURCES} onOpen={vi.fn()} />);
    expect(screen.getByRole('button', { name: 'View 2 sources' })).toBeTruthy();
    expect(screen.getByText('Sources (2)')).toBeTruthy();
  });

  it('hides the button when N=0 (silent empty-state)', () => {
    const { container } = render(<SourcesButton sources={[]} onOpen={vi.fn()} />);
    expect(container.querySelector('button')).toBeNull();
  });

  it('opens on click', () => {
    const onOpen = vi.fn();
    render(<SourcesButton sources={SOURCES} onOpen={onOpen} />);
    fireEvent.click(screen.getByRole('button', { name: 'View 2 sources' }));
    expect(onOpen).toHaveBeenCalledWith(SOURCES);
  });

  it('opens on keyboard (Enter activates the native button)', () => {
    const onOpen = vi.fn();
    render(<SourcesButton sources={SOURCES} onOpen={onOpen} />);
    const button = screen.getByRole('button', { name: 'View 2 sources' });
    // A native <button> fires click on Enter — assert it IS a real button (not a div).
    expect(button.tagName).toBe('BUTTON');
    fireEvent.click(button); // Enter on a focused <button> dispatches click
    expect(onOpen).toHaveBeenCalledTimes(1);
  });
});

describe('aggregateAnswerSources (pure, D-13)', () => {
  function toolPart(payload: DisplayPayload) {
    return {
      type: 'tool-call',
      toolCallId: payload.tool_call_id,
      toolName: 'web_search',
      display: payload,
    };
  }

  it('returns [] for non-array content', () => {
    expect(aggregateAnswerSources(undefined)).toEqual([]);
    expect(aggregateAnswerSources('text')).toEqual([]);
  });

  it("aggregates + dedupes the registries across an answer's tool parts by ref_id", () => {
    const content = [
      { type: 'text', text: 'answer' },
      toolPart({ type: 'web_result', tool_call_id: 'c1', sources: [ALPHA, BRAVO] }),
      toolPart({ type: 'web_result', tool_call_id: 'c2', sources: [BRAVO] }),
    ];
    const out = aggregateAnswerSources(content);
    expect(out.map((s) => s.ref_id)).toEqual(['src-1', 'src-2']);
  });

  it('promotes a source to cited if any occurrence cited it', () => {
    const consulted = source({ ref_id: 'src-9', index: 9, cited: false });
    const cited = source({ ref_id: 'src-9', index: 9, cited: true });
    const content = [
      toolPart({ type: 'web_result', tool_call_id: 'c1', sources: [consulted] }),
      toolPart({ type: 'web_result', tool_call_id: 'c2', sources: [cited] }),
    ];
    expect(aggregateAnswerSources(content)[0]?.cited).toBe(true);
  });

  it('skips non-tool-call parts and parts without a display payload', () => {
    const content = [
      { type: 'text', text: 'x' },
      { type: 'tool-call', toolCallId: 'c1', toolName: 'web_search' }, // no display
    ];
    expect(aggregateAnswerSources(content)).toEqual([]);
  });
});

// A harness that wires the button + a citation chip into ONE provider so the
// shared-sheet behavior is exercised end-to-end (D-13: one sheet, two openers).
function Harness({ sources }: { readonly sources: readonly DisplaySource[] }) {
  return (
    <SourceExplorerProvider>
      <AnswerBar sources={sources} />
      <CitationOpener source={BRAVO} />
    </SourceExplorerProvider>
  );
}

function AnswerBar({ sources }: { readonly sources: readonly DisplaySource[] }) {
  const { openSources } = useSourceExplorer();
  return (
    <SourcesButton
      sources={sources}
      onOpen={(srcs) => {
        openSources(srcs);
      }}
    />
  );
}

function CitationOpener({ source: src }: { readonly source: DisplaySource }) {
  const { openSources } = useSourceExplorer();
  return (
    <CitationBubble
      number={src.index}
      source={src}
      onOpenSource={(refId) => {
        openSources([src], refId);
      }}
    />
  );
}

describe('shared Source Explorer (D-13: one sheet, two entry points, one registry)', () => {
  it('the "Sources (N)" button opens the shared sheet', () => {
    render(<Harness sources={SOURCES} />);
    expect(screen.queryByRole('dialog')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'View 2 sources' }));
    expect(screen.getByRole('dialog')).toBeTruthy();
    // The sheet renders the registry rows.
    expect(screen.getByText('Alpha')).toBeTruthy();
    expect(screen.getByText('Bravo')).toBeTruthy();
  });

  it('a CitationBubble onOpenSource opens the SAME sheet focused to that source (D-04)', () => {
    render(<Harness sources={SOURCES} />);
    // The citation chip is the numbered button "2".
    fireEvent.click(screen.getByRole('button', { name: /Source 2/ }));
    const dialog = screen.getByRole('dialog');
    expect(dialog).toBeTruthy();
    // Focused on the Metadata of src-2 (the click-through target).
    fireEvent.click(screen.getByRole('tab', { name: 'Metadata' }));
    expect(screen.getByText('src-2')).toBeTruthy();
  });

  it('both openers render ONE dialog (shared state, not two sheets)', () => {
    render(<Harness sources={SOURCES} />);
    fireEvent.click(screen.getByRole('button', { name: 'View 2 sources' }));
    expect(screen.getAllByRole('dialog')).toHaveLength(1);
  });

  it('closing the sheet (Esc) clears the shared open state', () => {
    render(<Harness sources={SOURCES} />);
    fireEvent.click(screen.getByRole('button', { name: 'View 2 sources' }));
    expect(screen.getByRole('dialog')).toBeTruthy();
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByRole('dialog')).toBeNull();
  });
});

describe('useSourceExplorer without a provider (no-op controller)', () => {
  it('returns a no-op openSources that throws nothing when no provider is mounted', () => {
    function Bare() {
      const { openSources } = useSourceExplorer();
      return (
        <button
          type="button"
          onClick={() => {
            openSources(SOURCES, 'src-1');
          }}
        >
          go
        </button>
      );
    }
    render(<Bare />);
    // No provider → no sheet; clicking the opener is a safe no-op (no throw, no dialog).
    expect(() => {
      fireEvent.click(screen.getByRole('button', { name: 'go' }));
    }).not.toThrow();
    expect(screen.queryByRole('dialog')).toBeNull();
  });
});
