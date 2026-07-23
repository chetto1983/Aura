import { describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen, within } from '@testing-library/react';
import i18n from '../../i18n/i18n';
import type { DisplayPayload } from '../displays/types';
import { ToolActivityCard } from '../ToolActivityCard';
import { toolStatus } from '../toolStatus';

// ToolActivityCard — compact disclosure row (compact-chat spec §3): AC-6 running
// rows NEVER auto-expand; AC-7 settled raw rows expand to the Request/Result
// panel; AC-8 typed displays render behind the row with a collapsed meta hint;
// AC-11 errors stay collapsed with a visible danger "Error" word. The XSS guard
// and the elapsed-duration suites are ported from the pre-compact card.

function row(): HTMLElement {
  return screen.getByTestId('tool-row');
}
function body(): HTMLElement {
  return screen.getByTestId('tool-body');
}
function trigger(): HTMLElement {
  return screen.getByRole('button', { name: /activity/ });
}

describe('ToolActivityCard — compact row (AC-6/AC-7/AC-11)', () => {
  it('AC-6: a running tool renders a COLLAPSED row with pulsing dot and args summary', () => {
    render(<ToolActivityCard toolName="web_search" argsText='{"query":"meteo domani"}' />);
    expect(body().hidden).toBe(true); // the C2 auto-expand is gone
    expect(trigger().getAttribute('aria-expanded')).toBe('false');
    expect(screen.getByText('meteo domani')).toBeTruthy(); // §3.2 query summary
    const dot = row().querySelector('.aura-dot-pulse');
    expect(dot).not.toBeNull();
    expect(dot?.className).toContain('bg-warning');
    // Status word is sr-only — AT hears it, the row stays quiet visually.
    const status = within(row()).getByText('Running');
    expect(status.className).toContain('sr-only');
  });

  it('AC-6: delayed streamed args never auto-expand the row', () => {
    const { rerender } = render(<ToolActivityCard toolName="delayed" />);
    rerender(<ToolActivityCard toolName="delayed" argsText='{"query":"late"}' />);
    expect(body().hidden).toBe(true);
    expect(trigger().getAttribute('aria-expanded')).toBe('false');
  });

  it('AC-7: a settled raw row stays collapsed; expanding reveals Request+Result in a region', () => {
    render(
      <ToolActivityCard
        toolName="web_search"
        argsText='{"query":"meteo"}'
        result='{"answer":42}'
      />,
    );
    expect(body().hidden).toBe(true);
    fireEvent.click(trigger());
    expect(trigger().getAttribute('aria-expanded')).toBe('true');
    const region = screen.getByRole('region', { name: 'web_search result' });
    expect(region.hidden).toBe(false);
    expect(trigger().getAttribute('aria-controls')).toBe(region.id);
    expect(within(region).getByText('Request')).toBeTruthy();
    expect(within(region).getByText('Result')).toBeTruthy();
    // The panel scroll container caps the payload height (§3.5/§4).
    expect(region.querySelector('.max-h-80.overflow-y-auto')).not.toBeNull();
  });

  it('AC-7: the raw result meta shows the honest char count when settled', () => {
    render(<ToolActivityCard toolName="web_search" result="0123456789" />);
    expect(screen.getByTestId('tool-meta').textContent).toBe('10 chars');
  });

  it('AC-11: an error row shows a visible danger Error word, stays collapsed + collapsible', () => {
    render(<ToolActivityCard toolName="web_search" result="boom" isError />);
    expect(body().hidden).toBe(true);
    expect(row().className).toContain('border-danger/40'); // danger tint (OQ-2)
    const words = within(row()).getAllByText('Error');
    const visible = words.find((w) => !w.className.includes('sr-only'));
    expect(visible?.className).toContain('text-danger');
    // sr-only status is present too.
    expect(words.some((w) => w.className.includes('sr-only'))).toBe(true);
    fireEvent.click(trigger());
    expect(body().hidden).toBe(false);
  });

  it('renders untrusted tool output as TEXT, never as HTML (XSS guard)', () => {
    const payload = '<img src=x onerror="alert(1)"><b>bold</b>';
    render(<ToolActivityCard toolName="evil_tool" result={payload} />);
    fireEvent.click(trigger());
    const pre = screen.getByText(payload);
    expect(pre.tagName.toLowerCase()).toBe('pre');
    expect(pre.textContent).toBe(payload);
    expect(pre.querySelector('img')).toBeNull();
    expect(pre.querySelector('b')).toBeNull();
  });

  it('collapsing while focus is inside the body returns focus to the row trigger', () => {
    render(<ToolActivityCard toolName="web_search" result="raw" />);
    fireEvent.click(trigger());
    const region = body();
    region.tabIndex = -1;
    region.focus();
    fireEvent.click(trigger());
    expect(trigger().getAttribute('aria-expanded')).toBe('false');
    expect(document.activeElement).toBe(trigger());
  });

  it('toolStatus derives the status from result/isError', () => {
    expect(toolStatus({})).toBe('running');
    expect(toolStatus({ result: 'x' })).toBe('done');
    expect(toolStatus({ result: 'x', isError: true })).toBe('error');
  });
});

describe('ToolActivityCard — typed display behind the row (AC-8)', () => {
  const webDisplay: DisplayPayload = {
    type: 'web_result',
    tool_call_id: 'call-1',
    web_results: [
      { title: 'Result One', url: 'https://example.com/1' },
      { title: 'Result Two', url: 'https://example.com/2' },
    ],
  };

  it('shows the collapsed meta hint and renders the typed display only when expanded', () => {
    render(
      <ToolActivityCard
        toolName="web_search"
        argsText='{"query":"q"}'
        result="settled"
        display={webDisplay}
      />,
    );
    expect(screen.getByTestId('tool-meta').textContent).toBe('2 results');
    expect(body().hidden).toBe(true);
    fireEvent.click(trigger());
    expect(body().hidden).toBe(false);
    expect(screen.getByText('Result One')).toBeTruthy();
    expect(screen.getByText('Result Two')).toBeTruthy();
  });

  it('pluralizes the meta hint for a single result', () => {
    render(
      <ToolActivityCard
        toolName="web_search"
        result="settled"
        display={{ ...webDisplay, web_results: [{ title: 'Only', url: 'https://e.c' }] }}
      />,
    );
    expect(screen.getByTestId('tool-meta').textContent).toBe('1 result');
  });
});

describe('ToolActivityCard — nested subagent / child rows', () => {
  it('renders child rows inside the expanded body, each with its own disclosure', () => {
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
    fireEvent.click(trigger());
    const childList = screen.getByTestId('tool-children');
    expect(childList.className).toContain('ps-4');
    expect(childList.className).toContain('border-l');
    expect(within(childList).getByText('web_search')).toBeTruthy();
    expect(within(childList).getByText('web_fetch')).toBeTruthy();
    expect(within(childList).getByText('sandbox_exec')).toBeTruthy();
    // Each child is its own disclosure row.
    expect(within(childList).getAllByTestId('tool-row')).toHaveLength(3);
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
    fireEvent.click(screen.getByRole('button', { name: 'swarm_coordinator activity' }));
    fireEvent.click(screen.getByRole('button', { name: 'evil_child activity' }));
    const pre = screen.getByText(payload);
    expect(pre.tagName.toLowerCase()).toBe('pre');
    expect(pre.querySelector('img')).toBeNull();
    expect(pre.querySelector('b')).toBeNull();
  });

  it('graceful absence: no children container without childActivity', () => {
    render(<ToolActivityCard toolName="web_search" result="ok" />);
    expect(screen.queryByTestId('tool-children')).toBeNull();
    render(<ToolActivityCard toolName="web_search" result="ok" childActivity={[]} />);
    expect(screen.queryByTestId('tool-children')).toBeNull();
  });
});

describe('ToolActivityCard — elapsed time', () => {
  it('renders NOTHING when no timing is present (graceful absence — no 0s placeholder)', () => {
    render(<ToolActivityCard toolName="web_search" result="ok" />);
    expect(screen.queryByTestId('tool-elapsed')).toBeNull();
    expect(screen.queryByText(/0\s*s/)).toBeNull();
  });

  it('shows a frozen final duration as ordinary non-live assistive text', () => {
    render(
      <ToolActivityCard toolName="web_search" result="ok" startedAt={1000} finishedAt={3500} />,
    );
    const elapsed = screen.getByTestId('tool-elapsed');
    expect(elapsed.textContent).toBe('2.5 s');
    expect(elapsed.hasAttribute('aria-hidden')).toBe(false);
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

  it('ticks while running (aria-hidden) and freezes once a finishedAt arrives', () => {
    vi.useFakeTimers();
    try {
      const t0 = Date.now();
      const { rerender } = render(
        <ToolActivityCard toolName="slow_tool" argsText="{}" startedAt={t0} />,
      );
      expect(screen.getByTestId('tool-elapsed').textContent).toBe('0 s');
      expect(screen.getByTestId('tool-elapsed').getAttribute('aria-hidden')).toBe('true');
      act(() => {
        vi.advanceTimersByTime(2000);
      });
      expect(screen.getByTestId('tool-elapsed').textContent).toBe('2 s');
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
      expect(vi.getTimerCount()).toBeGreaterThan(0);
      unmount();
      expect(clearSpy).toHaveBeenCalled();
      expect(vi.getTimerCount()).toBe(0);
    } finally {
      clearSpy.mockRestore();
      vi.useRealTimers();
    }
  });

  it.each([
    { label: 'result', settledProps: { result: 'done' } },
    { label: 'error', settledProps: { isError: true } },
  ])('freezes the duration when $label settles without finishedAt', async ({ settledProps }) => {
    vi.useFakeTimers();
    try {
      const t0 = 1000;
      vi.setSystemTime(t0);
      const { rerender } = render(
        <ToolActivityCard toolName="logical_settle" argsText="{}" startedAt={t0} />,
      );
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
      expect(screen.getByTestId('tool-elapsed').textContent).toBe('2.5 s');
      expect(screen.getByTestId('tool-elapsed').hasAttribute('aria-hidden')).toBe(false);
      expect(vi.getTimerCount()).toBe(0);
      act(() => {
        vi.advanceTimersByTime(5000);
      });
      expect(screen.getByTestId('tool-elapsed').textContent).toBe('2.5 s');
    } finally {
      vi.useRealTimers();
    }
  });
});
