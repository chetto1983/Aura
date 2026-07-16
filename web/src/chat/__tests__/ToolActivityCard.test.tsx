import { describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen } from '@testing-library/react';
import i18n from '../../i18n/i18n';
import { ToolActivityCard } from '../ToolActivityCard';
import { toolStatus } from '../toolStatus';

function rawPre(text: string): HTMLPreElement {
  const raw = screen.getByText(text);
  expect(raw.tagName.toLowerCase()).toBe('pre');
  return raw as HTMLPreElement;
}

function expectRawHidden(text: string): HTMLPreElement {
  const raw = rawPre(text);
  expect(raw.hidden).toBe(true);
  expect(raw.hasAttribute('hidden')).toBe(true);
  return raw;
}

function expectRawVisible(text: string): HTMLPreElement {
  const raw = rawPre(text);
  expect(raw.hidden).toBe(false);
  expect(raw.hasAttribute('hidden')).toBe(false);
  return raw;
}

describe('ToolActivityCard (D-02 — raw view only)', () => {
  it('renders the tool name + a running status when no result yet', () => {
    render(<ToolActivityCard toolName="web_search" argsText='{"query":"meteo"}' />);
    expect(screen.getByText('web_search')).toBeTruthy();
    expect(screen.getByText('Running')).toBeTruthy();
  });

  it('shows a done status once a result is present', () => {
    render(<ToolActivityCard toolName="web_search" result="the answer" />);
    expect(screen.getByText('Done')).toBeTruthy();
  });

  it('shows an error status when isError is set', () => {
    render(<ToolActivityCard toolName="web_search" result="boom" isError />);
    expect(screen.getByText('Error')).toBeTruthy();
  });

  it('the expander reveals the raw mono result and toggles aria-expanded', () => {
    render(<ToolActivityCard toolName="web_search" result="RAW RESULT BLOB" />);
    const expander = screen.getByRole('button', { name: 'Show raw result' });
    expect(expander.getAttribute('aria-expanded')).toBe('false');
    const raw = expectRawHidden('RAW RESULT BLOB');
    expect(expander.getAttribute('aria-controls')).toBe(raw.id);
    expect(document.getElementById(raw.id)).toBe(raw);

    fireEvent.click(expander);
    expect(expander.getAttribute('aria-expanded')).toBe('true');
    expectRawVisible('RAW RESULT BLOB');
    // It renders as a <pre> mono block — text only.
    expect(raw.className).toContain('font-mono');

    fireEvent.click(screen.getByRole('button', { name: 'Hide raw result' }));
    expectRawHidden('RAW RESULT BLOB');
  });

  it('renders untrusted tool output as TEXT, never as HTML (XSS guard)', () => {
    const payload = '<img src=x onerror="alert(1)"><b>bold</b>';
    render(<ToolActivityCard toolName="evil_tool" result={payload} />);
    fireEvent.click(screen.getByRole('button', { name: 'Show raw result' }));
    const pre = screen.getByText(payload);
    // The literal markup is present as TEXT content (escaped), not parsed: no
    // <img>/<b> elements were injected into the DOM.
    expect(pre.textContent).toBe(payload);
    expect(pre.querySelector('img')).toBeNull();
    expect(pre.querySelector('b')).toBeNull();
  });

  it('does not render an expander when there is no raw content', () => {
    render(<ToolActivityCard toolName="noop_tool" />);
    expect(screen.queryByRole('button')).toBeNull();
  });

  it('toolStatus derives the status from result/isError', () => {
    expect(toolStatus({})).toBe('running');
    expect(toolStatus({ result: 'x' })).toBe('done');
    expect(toolStatus({ result: 'x', isError: true })).toBe('error');
  });

  it('keeps one quiet outer boundary and contains a long tool name beside the disclosure', () => {
    const toolName = 'namespace__a_very_long_tool_name_that_must_wrap_without_overflow';
    const { container } = render(<ToolActivityCard toolName={toolName} result="ok" />);
    const card = container.firstElementChild;
    const name = screen.getByText(toolName);
    const disclosure = screen.getByRole('button', { name: 'Show raw result' });

    expect(card?.className).toContain('border-border');
    expect(card?.className).not.toContain('border-l-2');
    expect(name.className).toContain('min-w-0');
    expect(name.className).toContain('break-words');
    expect(disclosure.className).toContain('min-h-11');
    expect(disclosure.className).toContain('min-w-11');
  });
});

describe('ToolActivityCard — nested subagent / child rows (§3.5)', () => {
  it('renders each child tool entry as its own indented status row', () => {
    render(
      <ToolActivityCard
        toolName="swarm_coordinator"
        result="parent done"
        childActivity={[
          { toolName: 'web_search', result: 'child one done' },
          { toolName: 'web_fetch' }, // running (no result)
          { toolName: 'sandbox_exec', result: 'boom', isError: true },
        ]}
      />,
    );
    // Each child's tool name is present.
    expect(screen.getByText('web_search')).toBeTruthy();
    expect(screen.getByText('web_fetch')).toBeTruthy();
    expect(screen.getByText('sandbox_exec')).toBeTruthy();
    // Each child carries its own marker and text status.
    expect(screen.getAllByText('Done').length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText('Running')).toBeTruthy();
    expect(screen.getByText('Error')).toBeTruthy();
    // The children live in an indented, left-ruled container (ps-4 border-l border-border).
    const childList = screen.getByTestId('tool-children');
    expect(childList.className).toContain('ps-4');
    expect(childList.className).toContain('border-l');
    expect(childList.className).toContain('border-border');
  });

  it('child raw output renders as escaped TEXT, never HTML (XSS guard on children)', () => {
    const payload = '<img src=x onerror="alert(1)"><b>bold</b>';
    render(
      <ToolActivityCard
        toolName="swarm_coordinator"
        result="parent"
        childActivity={[{ toolName: 'evil_child', result: payload }]}
      />,
    );
    // Expand the child's raw blob via its own expander.
    const childExpander = screen
      .getAllByRole('button', { name: 'Show raw result' })
      .find((b) => b.closest('[data-testid="tool-children"]') !== null);
    if (childExpander === undefined) throw new Error('expected a child raw-result expander');
    fireEvent.click(childExpander);
    const pre = screen.getByText(payload);
    expect(pre.tagName.toLowerCase()).toBe('pre');
    expect(pre.textContent).toBe(payload);
    expect(pre.querySelector('img')).toBeNull();
    expect(pre.querySelector('b')).toBeNull();
  });

  it('graceful absence: with no children the card is exactly the single-row shape', () => {
    render(<ToolActivityCard toolName="web_search" result="ok" />);
    expect(screen.queryByTestId('tool-children')).toBeNull();
  });

  it('graceful absence: an empty children array renders no children container', () => {
    render(<ToolActivityCard toolName="web_search" result="ok" childActivity={[]} />);
    expect(screen.queryByTestId('tool-children')).toBeNull();
  });
});

describe('ToolActivityCard — auto-collapse on settle (§3.5)', () => {
  it('expands when raw content arrives during a running call before manual intent', () => {
    const { rerender } = render(<ToolActivityCard toolName="delayed" />);
    expect(screen.queryByRole('button')).toBeNull();

    rerender(<ToolActivityCard toolName="delayed" argsText="late args" />);

    expect(screen.getByText('late args')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Hide raw result' })).toBeTruthy();
  });

  it('does not auto-expand delayed raw after the user has expressed intent', () => {
    const { rerender } = render(<ToolActivityCard toolName="manual" argsText="first" />);
    fireEvent.click(screen.getByRole('button', { name: 'Hide raw result' }));
    expectRawHidden('first');

    rerender(<ToolActivityCard toolName="manual" />);
    expect(screen.queryByRole('button')).toBeNull();

    rerender(<ToolActivityCard toolName="manual" argsText="late replacement" />);

    const raw = expectRawHidden('late replacement');
    const disclosure = screen.getByRole('button', { name: 'Show raw result' });
    expect(disclosure.getAttribute('aria-expanded')).toBe('false');
    expect(disclosure.getAttribute('aria-controls')).toBe(raw.id);
    expect(document.getElementById(raw.id)).toBe(raw);
  });

  it('auto-collapses ONCE to the one-line summary when running → done', () => {
    const { rerender } = render(
      <ToolActivityCard toolName="slow_tool" argsText="PARTIAL ARGS BLOB" />,
    );
    // Running with raw → auto-expanded, blob visible.
    expectRawVisible('PARTIAL ARGS BLOB');
    // The call settles (a result arrives) → auto-collapse to one line.
    rerender(<ToolActivityCard toolName="slow_tool" result="FINAL RESULT BLOB" />);
    expectRawHidden('FINAL RESULT BLOB');
    // The expander reflects the collapsed state.
    expect(
      screen.getByRole('button', { name: 'Show raw result' }).getAttribute('aria-expanded'),
    ).toBe('false');
  });

  it('auto-collapses when running → error too', () => {
    const { rerender } = render(<ToolActivityCard toolName="slow_tool" argsText="ARGS" />);
    expectRawVisible('ARGS');
    rerender(<ToolActivityCard toolName="slow_tool" result="boom" isError />);
    expectRawHidden('boom');
  });

  it('a manual toggle wins: a user-collapsed running card stays collapsed on settle', () => {
    const { rerender } = render(<ToolActivityCard toolName="slow_tool" argsText="ARGS BLOB" />);
    // Auto-expanded while running; the operator manually collapses it.
    expectRawVisible('ARGS BLOB');
    fireEvent.click(screen.getByRole('button', { name: 'Hide raw result' }));
    expectRawHidden('ARGS BLOB');
    // Settle: auto-collapse must NOT fight the user — the intent-guard means status
    // transitions no longer drive expansion.
    rerender(<ToolActivityCard toolName="slow_tool" result="RESULT BLOB" />);
    expectRawHidden('RESULT BLOB');
  });

  it('a user who manually keeps it expanded stays expanded across a settle (manual wins over auto-collapse)', () => {
    const { rerender } = render(<ToolActivityCard toolName="slow_tool" argsText="ARGS BLOB" />);
    // Operator toggles: collapse then expand — now userToggled, final state = expanded.
    fireEvent.click(screen.getByRole('button', { name: 'Hide raw result' }));
    fireEvent.click(screen.getByRole('button', { name: 'Show raw result' }));
    expectRawVisible('ARGS BLOB');
    // Settle: the auto-collapse-once is suppressed by the user-intent guard.
    rerender(<ToolActivityCard toolName="slow_tool" result="RESULT BLOB" />);
    expectRawVisible('RESULT BLOB');
    expect(
      screen.getByRole('button', { name: 'Hide raw result' }).getAttribute('aria-expanded'),
    ).toBe('true');
  });

  it('a card that mounts already-settled stays collapsed (no regression)', () => {
    render(<ToolActivityCard toolName="web_search" result="DONE BLOB" />);
    expectRawHidden('DONE BLOB');
  });

  it('collapses exactly ONCE: a settled card stays collapsed even if a later frame flickers status back to running', () => {
    // running → done auto-collapses once.
    const { rerender } = render(<ToolActivityCard toolName="flicky" argsText="ARGS" />);
    expectRawVisible('ARGS');
    rerender(<ToolActivityCard toolName="flicky" result="RES" />);
    expectRawHidden('RES');
    // A spurious late frame reports running again (result undefined). The ONCE semantics
    // mean the card must NOT auto-re-expand from a status re-seed — the collapse already
    // fired once and no manual intent re-opened it.
    rerender(<ToolActivityCard toolName="flicky" argsText="ARGS AGAIN" />);
    expectRawHidden('ARGS AGAIN');
  });
});

describe('ToolActivityCard — elapsed time (§3.5)', () => {
  it('renders NOTHING when no timing is present (graceful absence — no 0s placeholder)', () => {
    render(<ToolActivityCard toolName="web_search" result="ok" />);
    // No elapsed readout element at all.
    expect(screen.queryByTestId('tool-elapsed')).toBeNull();
    // And no spurious zero-second text leaked anywhere.
    expect(screen.queryByText(/0\s*s/)).toBeNull();
  });

  it('shows a frozen final duration as ordinary non-live assistive text', () => {
    render(
      <ToolActivityCard toolName="web_search" result="ok" startedAt={1000} finishedAt={3500} />,
    );
    const elapsed = screen.getByTestId('tool-elapsed');
    // (3500 - 1000) / 1000 = 2.5s
    expect(elapsed.textContent).toBe('2.5 s');
    expect(elapsed.hasAttribute('aria-hidden')).toBe(false);
    expect(elapsed.hasAttribute('aria-live')).toBe(false);
    // Mono tabular-nums faint readout.
    expect(elapsed.className).toContain('font-mono');
    expect(elapsed.className).toContain('tabular-nums');
    expect(elapsed.className).toContain('text-text-faint');
  });

  it('formats a settled decimal duration for the active Italian locale', async () => {
    await act(async () => {
      await i18n.changeLanguage('it');
    });
    try {
      render(
        <ToolActivityCard toolName="ricerca" result="ok" startedAt={1000} finishedAt={3500} />,
      );
      expect(screen.getByTestId('tool-elapsed').textContent).toBe('2,5 s');
    } finally {
      await act(async () => {
        await i18n.changeLanguage('en');
      });
    }
  });

  it('ticks while running (no finishedAt) and freezes once a finishedAt arrives', () => {
    vi.useFakeTimers();
    try {
      // advanceTimersByTime advances the mocked Date.now() clock too, so the ticking
      // readout = Date.now() - startedAt. Anchor startedAt to the current fake clock.
      const t0 = Date.now();
      const { rerender } = render(
        <ToolActivityCard toolName="slow_tool" argsText="{}" startedAt={t0} />,
      );
      // Running ticks are hidden from assistive technology so each timer update is quiet.
      expect(screen.getByTestId('tool-elapsed').textContent).toBe('0 s');
      expect(screen.getByTestId('tool-elapsed').getAttribute('aria-hidden')).toBe('true');
      // Advance 2s of wall clock — the ticker re-renders against the advanced clock.
      act(() => {
        vi.advanceTimersByTime(2000);
      });
      expect(screen.getByTestId('tool-elapsed').textContent).toBe('2 s');
      // A result + finishedAt arrives: the readout freezes at the exact final duration.
      rerender(
        <ToolActivityCard
          toolName="slow_tool"
          result="done"
          startedAt={t0}
          finishedAt={t0 + 3750}
        />,
      );
      expect(screen.getByTestId('tool-elapsed').textContent).toBe('3.75 s');
      expect(screen.getByTestId('tool-elapsed').hasAttribute('aria-hidden')).toBe(false);
      expect(screen.getByTestId('tool-elapsed').hasAttribute('aria-live')).toBe(false);
      // Further wall-clock advance does NOT move a frozen readout.
      act(() => {
        vi.advanceTimersByTime(5000);
      });
      expect(screen.getByTestId('tool-elapsed').textContent).toBe('3.75 s');
    } finally {
      vi.useRealTimers();
    }
  });

  it('clears the ticking interval on unmount — NO timer leak', () => {
    vi.useFakeTimers();
    const clearSpy = vi.spyOn(globalThis, 'clearInterval');
    try {
      vi.setSystemTime(1000);
      const { unmount } = render(
        <ToolActivityCard toolName="slow_tool" argsText="{}" startedAt={1000} />,
      );
      // A ticking interval was registered while running.
      expect(vi.getTimerCount()).toBeGreaterThan(0);
      unmount();
      // The interval was cleared on unmount: none pending, and clearInterval ran.
      expect(clearSpy).toHaveBeenCalled();
      expect(vi.getTimerCount()).toBe(0);
    } finally {
      clearSpy.mockRestore();
      vi.useRealTimers();
    }
  });

  it('keeps END-only lifecycle running until RESULT settles it, while honoring END timing', () => {
    vi.useFakeTimers();
    try {
      const t0 = 1000;
      vi.setSystemTime(t0);
      const { rerender } = render(
        <ToolActivityCard toolName="split_events" argsText="{}" startedAt={t0} />,
      );
      expect(screen.getByText('Running')).toBeTruthy();
      expect(vi.getTimerCount()).toBeGreaterThan(0);

      rerender(
        <ToolActivityCard
          toolName="split_events"
          argsText="{}"
          startedAt={t0}
          finishedAt={t0 + 1500}
        />,
      );
      const endedOnlyElapsed = screen.getByTestId('tool-elapsed');
      expect(screen.getByText('Running')).toBeTruthy();
      expect(screen.queryByText('Done')).toBeNull();
      expect(endedOnlyElapsed.textContent).toBe('1.5 s');
      expect(endedOnlyElapsed.getAttribute('aria-hidden')).toBe('true');
      expect(vi.getTimerCount()).toBeGreaterThan(0);

      act(() => {
        vi.advanceTimersByTime(5000);
      });
      expect(screen.getByTestId('tool-elapsed').textContent).toBe('1.5 s');

      rerender(
        <ToolActivityCard
          toolName="split_events"
          result="done"
          startedAt={t0}
          finishedAt={t0 + 1500}
        />,
      );
      const settledElapsed = screen.getByTestId('tool-elapsed');
      expect(screen.getByText('Done')).toBeTruthy();
      expect(settledElapsed.textContent).toBe('1.5 s');
      expect(settledElapsed.hasAttribute('aria-hidden')).toBe(false);
      expect(vi.getTimerCount()).toBe(0);
    } finally {
      vi.useRealTimers();
    }
  });

  it.each([
    { label: 'result', settledProps: { result: 'done' }, statusText: 'Done' },
    { label: 'error', settledProps: { isError: true }, statusText: 'Error' },
  ])(
    'stops and exposes the frozen duration when $label settles without finishedAt',
    async ({ settledProps, statusText }) => {
      vi.useFakeTimers();
      try {
        const t0 = 1000;
        vi.setSystemTime(t0);
        const { rerender } = render(
          <ToolActivityCard toolName="logical_settle" argsText="{}" startedAt={t0} />,
        );
        expect(vi.getTimerCount()).toBeGreaterThan(0);

        act(() => {
          vi.advanceTimersByTime(2000);
        });
        expect(screen.getByTestId('tool-elapsed').textContent).toBe('2 s');
        vi.setSystemTime(t0 + 2500);

        await act(async () => {
          rerender(
            <ToolActivityCard
              toolName="logical_settle"
              argsText="{}"
              startedAt={t0}
              {...settledProps}
            />,
          );
          await Promise.resolve();
        });
        const settledElapsed = screen.getByTestId('tool-elapsed');
        expect(screen.getByText(statusText)).toBeTruthy();
        expect(settledElapsed.textContent).toBe('2.5 s');
        expect(settledElapsed.hasAttribute('aria-hidden')).toBe(false);
        expect(settledElapsed.hasAttribute('aria-live')).toBe(false);
        expect(vi.getTimerCount()).toBe(0);

        act(() => {
          vi.advanceTimersByTime(5000);
        });
        expect(screen.getByTestId('tool-elapsed').textContent).toBe('2.5 s');
      } finally {
        vi.useRealTimers();
      }
    },
  );

  it('does not register a ticking interval once settled (no leak after freeze)', () => {
    vi.useFakeTimers();
    try {
      vi.setSystemTime(2500);
      render(<ToolActivityCard toolName="web_search" result="ok" startedAt={1000} />);
      // Settled card: no live ticker.
      expect(screen.getByTestId('tool-elapsed').textContent).toBe('1.5 s');
      expect(screen.getByTestId('tool-elapsed').hasAttribute('aria-hidden')).toBe(false);
      expect(vi.getTimerCount()).toBe(0);
    } finally {
      vi.useRealTimers();
    }
  });
});
