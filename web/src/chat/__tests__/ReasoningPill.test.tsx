import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import i18n from '../../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys
import { ReasoningPill } from '../ReasoningPill';

// ReasoningPill (compact-chat spec §2.2) — AC-1..AC-5:
//   AC-1 collapsed by default, CoT hidden;
//   AC-2 duration label from timestamps/durationMs, "Reasoning" fallback without;
//   AC-3 streaming shimmer + aria-hidden last-line ticker;
//   AC-4 expand → aria-expanded, region body, max-h-80 scroll cap, focus stays;
//   AC-5 zero localStorage traffic (the aura.chat.reasoning.shown pref is retired).

afterEach(() => {
  vi.restoreAllMocks();
  if (i18n.language !== 'en') void i18n.changeLanguage('en');
});

describe('ReasoningPill (compact-chat §2.2)', () => {
  it('AC-1: renders collapsed by default with the CoT hidden', () => {
    render(<ReasoningPill text="secret chain of thought" durationMs={13900} />);
    const pill = screen.getByTestId('reasoning-pill');
    const trigger = screen.getByRole('button');
    const body = screen.getByTestId('reasoning-body');
    expect(pill).toBeTruthy();
    expect(trigger.getAttribute('aria-expanded')).toBe('false');
    expect(body.hidden).toBe(true);
  });

  it('AC-2: labels "Thought for …" from snapshot durationMs and live timestamps alike', () => {
    // <10s keeps decimals (the shared formatter's precision rule), ≥10s rounds.
    const { unmount } = render(<ReasoningPill text="trace" durationMs={9500} />);
    expect(screen.getByText('Thought for 9.5 s')).toBeTruthy();
    unmount();

    render(<ReasoningPill text="trace" startedAt={1000} finishedAt={47804} />);
    expect(screen.getByText('Thought for 47 s')).toBeTruthy();
  });

  it('AC-2: falls back to the "Reasoning" label when no duration is known — expand identical', () => {
    render(<ReasoningPill text="pre-migration trace" />);
    const trigger = screen.getByRole('button', { name: 'Show reasoning' });
    expect(screen.getByText('Reasoning')).toBeTruthy();
    fireEvent.click(trigger);
    expect(screen.getByTestId('reasoning-body').hidden).toBe(false);
    expect(screen.getByText('pre-migration trace')).toBeTruthy();
  });

  it('AC-2 (it): renders the Italian duration label with the locale decimal comma', async () => {
    await i18n.changeLanguage('it');
    render(<ReasoningPill text="trace" durationMs={9500} />);
    expect(screen.getByText('Ha ragionato per 9,5 s')).toBeTruthy();
  });

  it('AC-3: streaming state shows the shimmer label and an aria-hidden last-line ticker', () => {
    render(<ReasoningPill text={'first line\nnewest line'} isRunning />);
    const label = screen.getByText('Thinking…');
    expect(label.className).toContain('aura-thinking-shimmer');
    const ticker = screen.getByTestId('reasoning-ticker');
    expect(ticker.getAttribute('aria-hidden')).toBe('true');
    expect(ticker.textContent).toBe('newest line');
  });

  it('AC-3: the ticker disappears the instant the span settles', () => {
    const { rerender } = render(<ReasoningPill text="line" isRunning />);
    expect(screen.getByTestId('reasoning-ticker')).toBeTruthy();
    rerender(<ReasoningPill text="line" isRunning finishedAt={2000} startedAt={1000} />);
    expect(screen.queryByTestId('reasoning-ticker')).toBeNull();
    expect(screen.getByText('Thought for 1 s')).toBeTruthy();
  });

  it('AC-4: expanding reveals a region body capped at max-h-80 with its own scrollbar; focus stays on the trigger', () => {
    render(<ReasoningPill text="expandable trace" durationMs={5000} />);
    const trigger = screen.getByRole('button');
    trigger.focus();
    fireEvent.click(trigger);
    expect(trigger.getAttribute('aria-expanded')).toBe('true');
    const body = screen.getByRole('region', { name: 'Model reasoning' });
    expect(body.hidden).toBe(false);
    expect(body.className).toContain('max-h-80');
    expect(body.className).toContain('overflow-y-auto');
    expect(trigger.getAttribute('aria-controls')).toBe(body.id);
    expect(document.activeElement).toBe(trigger);
  });

  it('AC-4: collapsing while focus is inside the body returns focus to the trigger', () => {
    render(<ReasoningPill text="focus trace" durationMs={5000} />);
    const trigger = screen.getByRole('button');
    fireEvent.click(trigger);
    const body = screen.getByTestId('reasoning-body');
    body.tabIndex = -1;
    body.focus();
    expect(document.activeElement).toBe(body);
    fireEvent.click(trigger);
    expect(trigger.getAttribute('aria-expanded')).toBe('false');
    expect(document.activeElement).toBe(trigger);
  });

  it('AC-5: never reads or writes the retired localStorage preference', () => {
    const get = vi.spyOn(Storage.prototype, 'getItem');
    const set = vi.spyOn(Storage.prototype, 'setItem');
    render(<ReasoningPill text="trace" durationMs={100} />);
    fireEvent.click(screen.getByRole('button'));
    fireEvent.click(screen.getByRole('button'));
    expect(get).not.toHaveBeenCalledWith('aura.chat.reasoning.shown');
    expect(set).not.toHaveBeenCalled();
  });

  it('renders nothing for a settled empty span and a pending body for a streaming empty span', () => {
    const { container, rerender } = render(<ReasoningPill text="" />);
    expect(container.firstChild).toBeNull();
    rerender(<ReasoningPill text="" isRunning />);
    fireEvent.click(screen.getByRole('button'));
    expect(screen.getByText('Thinking...')).toBeTruthy(); // pending body copy
  });

  it('skips blank trailing lines when picking the ticker line', () => {
    render(<ReasoningPill text={'kept line\n\n   \n'} isRunning />);
    expect(screen.getByTestId('reasoning-ticker').textContent).toBe('kept line');
  });

  it('follows the streaming tail only while the operator is at the bottom', () => {
    const { rerender } = render(<ReasoningPill text="line one" isRunning />);
    fireEvent.click(screen.getByRole('button'));
    const body = screen.getByTestId('reasoning-body');
    Object.defineProperty(body, 'scrollHeight', { configurable: true, value: 1000 });
    Object.defineProperty(body, 'clientHeight', { configurable: true, value: 100 });

    // Operator scrolled up → the tail-follow disengages; a new delta must NOT yank.
    body.scrollTop = 0;
    fireEvent.scroll(body);
    rerender(<ReasoningPill text={'line one\nline two'} isRunning />);
    expect(body.scrollTop).toBe(0);

    // Back at the bottom → follow re-engages; the next delta pins to the tail.
    body.scrollTop = 900;
    fireEvent.scroll(body);
    rerender(<ReasoningPill text={'line one\nline two\nline three'} isRunning />);
    expect(body.scrollTop).toBe(1000);
  });

  it('keeps expansion per-part (ephemeral): two pills expand independently', () => {
    render(
      <>
        <ReasoningPill text="one" durationMs={100} />
        <ReasoningPill text="two" durationMs={100} />
      </>,
    );
    const [first, second] = screen.getAllByRole('button');
    if (first === undefined || second === undefined) throw new Error('expected two pills');
    fireEvent.click(first);
    expect(first.getAttribute('aria-expanded')).toBe('true');
    expect(second.getAttribute('aria-expanded')).toBe('false');
    const ids = [first, second].map((b) => b.getAttribute('aria-controls'));
    expect(new Set(ids).size).toBe(2);
  });
});
