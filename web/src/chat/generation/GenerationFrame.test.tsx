import { act, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import i18n from '../../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys
import { GenerationFrame } from './GenerationFrame';

// GenerationFrame (spec §5 "While running", R22/R23): the frame for a running
// image_generate/video_generate call and for a detached video job. Running shows an m:ss
// counter from mount, ticking once a second; the deferred frame is static, and neither
// ever renders the model's prompt as markup.

afterEach(() => {
  vi.useRealTimers();
  if (i18n.language !== 'en') void i18n.changeLanguage('en');
});

function frame(): HTMLElement {
  return screen.getByTestId('generation-frame');
}

function visual(): HTMLElement {
  const node = frame().querySelector<HTMLElement>('[data-slot="generation-visual"]');
  if (node === null) throw new Error('expected the frame visual');
  return node;
}

describe('GenerationFrame', () => {
  it('a replayed detached video has a static frame', () => {
    vi.useFakeTimers();
    render(
      <GenerationFrame
        kind="video"
        prompt="A moving sea"
        aspectRatio="16 / 9"
        generating={false}
      />,
    );
    expect(screen.getByText('Arriving in this chat')).toBeTruthy();
    expect(frame().getAttribute('data-generating')).toBe('false');
    expect(screen.getByText('A moving sea')).toBeTruthy();
    expect(screen.queryByRole('timer')).toBeNull();
    expect(vi.getTimerCount()).toBe(0);
    expect(frame().querySelector('.animate-pulse')).toBeNull();
    expect(frame().querySelector('.aura-thinking-shimmer')).toBeNull();
  });

  it('counts elapsed time from mount as m:ss, once a second', () => {
    vi.useFakeTimers();
    render(<GenerationFrame kind="video" prompt="A moving sea" aspectRatio="16 / 9" generating />);
    expect(screen.getByText('Generating video')).toBeTruthy();
    expect(frame().getAttribute('data-generating')).toBe('true');
    const timer = screen.getByRole('timer');
    expect(timer.textContent).toBe('0:00');
    act(() => {
      vi.advanceTimersByTime(999);
    });
    expect(timer.textContent).toBe('0:00');
    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(timer.textContent).toBe('0:01');
    act(() => {
      vi.advanceTimersByTime(64_000);
    });
    expect(timer.textContent).toBe('1:05');
    expect(timer.getAttribute('aria-label')).toBe('Elapsed time 1:05');
    expect(vi.getTimerCount()).toBe(1);
  });

  it('counts from the job start time it is given, not from mount', () => {
    vi.useFakeTimers();
    const startedAt = Date.now() - 65_000;
    const { unmount } = render(
      <GenerationFrame
        kind="video"
        prompt="A moving sea"
        aspectRatio="16 / 9"
        generating
        startedAt={startedAt}
      />,
    );
    expect(screen.getByRole('timer').textContent).toBe('1:05');
    act(() => {
      vi.advanceTimersByTime(1_000);
    });
    expect(screen.getByRole('timer').textContent).toBe('1:06');
    unmount();

    // The point of the prop: a reload re-mounts the card on the same job, and the clock has
    // to keep reading the job's age rather than restarting at zero.
    render(
      <GenerationFrame
        kind="video"
        prompt="A moving sea"
        aspectRatio="16 / 9"
        generating
        startedAt={startedAt}
      />,
    );
    expect(screen.getByRole('timer').textContent).toBe('1:06');
  });

  it('never shows a negative clock for a start time in the future', () => {
    vi.useFakeTimers();
    render(
      <GenerationFrame
        kind="video"
        prompt="p"
        aspectRatio="16 / 9"
        generating
        startedAt={Date.now() + 30_000}
      />,
    );
    expect(screen.getByRole('timer').textContent).toBe('0:00');
  });

  it('keeps the ticking number out of any live region', () => {
    vi.useFakeTimers();
    render(<GenerationFrame kind="image" prompt="p" aspectRatio="1 / 1" generating />);
    const timer = screen.getByRole('timer');
    expect(timer.closest('[role="status"]')).toBeNull();
    expect(timer.closest('[aria-live="polite"]')).toBeNull();
    expect(screen.getByRole('status').textContent).toBe('Generating image');
  });

  it('stops the counter and its interval when generation becomes a detached job', () => {
    vi.useFakeTimers();
    const { rerender } = render(
      <GenerationFrame kind="video" prompt="p" aspectRatio="9 / 16" generating />,
    );
    act(() => {
      vi.advanceTimersByTime(3_000);
    });
    rerender(<GenerationFrame kind="video" prompt="p" aspectRatio="9 / 16" generating={false} />);
    expect(screen.queryByRole('timer')).toBeNull();
    expect(screen.getByText('Arriving in this chat')).toBeTruthy();
    expect(vi.getTimerCount()).toBe(0);
  });

  it('leaves no timer behind after unmount', () => {
    vi.useFakeTimers();
    const { unmount } = render(
      <GenerationFrame kind="image" prompt="p" aspectRatio="1 / 1" generating />,
    );
    expect(vi.getTimerCount()).toBe(1);
    unmount();
    expect(vi.getTimerCount()).toBe(0);
  });

  it('sizes the visual to the requested aspect ratio', () => {
    render(<GenerationFrame kind="video" prompt="p" aspectRatio="9 / 21" generating={false} />);
    expect(visual().style.aspectRatio).toBe('9 / 21');
    expect(visual().className).not.toContain('aspect-square');
    expect(frame().textContent).not.toContain('1024');
  });

  it('renders the prompt as text, never as markup', () => {
    render(
      <GenerationFrame
        kind="image"
        prompt="<img src=x onerror=alert(1)>"
        aspectRatio="1 / 1"
        generating
      />,
    );
    expect(screen.getByText('<img src=x onerror=alert(1)>')).toBeTruthy();
    expect(frame().querySelector('img')).toBeNull();
  });

  it('animates the running dots and label only where motion is allowed', () => {
    render(<GenerationFrame kind="image" prompt="p" aspectRatio="1 / 1" generating />);
    const dots = frame().querySelectorAll('.animate-pulse');
    expect(dots.length).toBeGreaterThan(0);
    for (const dot of dots) expect(dot.className).toContain('motion-reduce:animate-none');
    expect(screen.getByText('Generating image').className).toContain('aura-thinking-shimmer');
  });

  it('has no inert regenerate button', () => {
    render(<GenerationFrame kind="image" prompt="p" aspectRatio="1 / 1" generating={false} />);
    expect(screen.queryByRole('button')).toBeNull();
  });

  it('localizes the frame labels in Italian', async () => {
    await i18n.changeLanguage('it');
    const { rerender, unmount } = render(
      <GenerationFrame kind="image" prompt="p" aspectRatio="1 / 1" generating />,
    );
    expect(screen.getByText('Generazione immagine')).toBeTruthy();
    expect(screen.getByRole('timer').getAttribute('aria-label')).toBe('Tempo trascorso 0:00');
    rerender(<GenerationFrame kind="video" prompt="p" aspectRatio="1 / 1" generating />);
    expect(screen.getByText('Generazione video')).toBeTruthy();
    rerender(<GenerationFrame kind="video" prompt="p" aspectRatio="1 / 1" generating={false} />);
    expect(screen.getByText('In arrivo in questa chat')).toBeTruthy();
    unmount();
  });
});
