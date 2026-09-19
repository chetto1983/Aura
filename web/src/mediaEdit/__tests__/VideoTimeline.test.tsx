import { fireEvent, render, screen } from '@testing-library/react';
import { beforeAll, describe, expect, it, vi } from 'vitest';
import { TimeField } from '../TimeField';
import { VideoTimeline } from '../VideoTimeline';

beforeAll(() => {
  // jsdom implements neither pointer capture nor layout.
  HTMLElement.prototype.setPointerCapture = vi.fn();
  HTMLElement.prototype.hasPointerCapture = vi.fn(() => true);
});

function mount(start = 2, end = 6) {
  const onChange = vi.fn();
  render(
    <VideoTimeline
      duration={10}
      start={start}
      end={end}
      frames={[]}
      onChange={onChange}
      startLabel="Start of the selection"
      endLabel="End of the selection"
    />,
  );
  vi.spyOn(screen.getByTestId('video-timeline'), 'getBoundingClientRect').mockReturnValue({
    left: 0,
    width: 100,
    top: 0,
    height: 56,
    right: 100,
    bottom: 56,
    x: 0,
    y: 0,
    toJSON: () => ({}),
  });
  return onChange;
}

describe('VideoTimeline', () => {
  it('exposes both handles as sliders with the time as text', () => {
    mount();
    const start = screen.getByRole('slider', { name: 'Start of the selection' });
    expect(start.getAttribute('aria-valuenow')).toBe('2');
    expect(start.getAttribute('aria-valuetext')).toBe('00:02.0');
    expect(
      screen.getByRole('slider', { name: 'End of the selection' }).getAttribute('aria-valuemin'),
    ).toBe('2.1');
  });

  it('moves a handle a tenth per arrow and a second with Shift', () => {
    const onChange = mount();
    fireEvent.keyDown(screen.getByRole('slider', { name: 'Start of the selection' }), {
      key: 'ArrowRight',
    });
    expect(onChange).toHaveBeenLastCalledWith(2.1, 6);
    fireEvent.keyDown(screen.getByRole('slider', { name: 'End of the selection' }), {
      key: 'ArrowLeft',
      shiftKey: true,
    });
    expect(onChange).toHaveBeenLastCalledWith(2, 5);
  });

  it('never lets the start reach the end', () => {
    const onChange = mount(5.95, 6);
    fireEvent.keyDown(screen.getByRole('slider', { name: 'Start of the selection' }), {
      key: 'ArrowRight',
      shiftKey: true,
    });
    expect(onChange).toHaveBeenLastCalledWith(5.9, 6);
  });

  it('follows the pointer along the track', () => {
    const onChange = mount();
    const start = screen.getByRole('slider', { name: 'Start of the selection' });
    fireEvent.pointerDown(start, { pointerId: 1, clientX: 25 });
    expect(onChange).toHaveBeenLastCalledWith(2.5, 6);
    fireEvent.pointerMove(start, { pointerId: 1, clientX: 40 });
    expect(onChange).toHaveBeenLastCalledWith(4, 6);
  });

  it('keeps the start a whole step before an end that is off the tenth grid', () => {
    const onChange = mount(7.2, 7.36);
    fireEvent.keyDown(screen.getByRole('slider', { name: 'Start of the selection' }), {
      key: 'ArrowRight',
    });
    expect(onChange).toHaveBeenLastCalledWith(7.2, 7.36);
  });
});

describe('VideoTimeline on a degenerate clip', () => {
  function draw(duration: number, start: number, end: number) {
    const onChange = vi.fn();
    render(
      <VideoTimeline
        duration={duration}
        start={start}
        end={end}
        frames={[]}
        onChange={onChange}
        startLabel="Start"
        endLabel="End"
      />,
    );
    return onChange;
  }

  function expectOrderedBounds(name: string) {
    const handle = screen.getByRole('slider', { name });
    const [min, now, max] = ['aria-valuemin', 'aria-valuenow', 'aria-valuemax'].map((name) =>
      Number(handle.getAttribute(name)),
    );
    expect(min).toBeLessThanOrEqual(now ?? Number.NaN);
    expect(now).toBeLessThanOrEqual(max ?? Number.NaN);
  }

  it.each([0, -1, Number.NaN, Number.POSITIVE_INFINITY])(
    'draws a clip %s seconds long without NaN, Infinity or inverted bounds',
    (duration) => {
      const onChange = draw(duration, 0, 0);
      expect(screen.getByTestId('video-timeline').outerHTML).not.toMatch(/NaN|Infinity/);
      expectOrderedBounds('Start');
      expectOrderedBounds('End');
      fireEvent.keyDown(screen.getByRole('slider', { name: 'Start' }), { key: 'ArrowRight' });
      expect(onChange).toHaveBeenLastCalledWith(0, 0);
    },
  );

  it('keeps a clip shorter than one step whole: start at zero, end at its length', () => {
    const onChange = draw(0.04, 0, 0.04);
    expectOrderedBounds('Start');
    expectOrderedBounds('End');
    fireEvent.keyDown(screen.getByRole('slider', { name: 'Start' }), { key: 'ArrowRight' });
    expect(onChange).toHaveBeenLastCalledWith(0, 0.04);
    fireEvent.keyDown(screen.getByRole('slider', { name: 'End' }), { key: 'ArrowLeft' });
    expect(onChange).toHaveBeenLastCalledWith(0, 0.04);
  });
});

describe('TimeField', () => {
  it('commits on Enter and on no other key', () => {
    const onCommit = vi.fn();
    render(<TimeField label="End" value={4} onCommit={onCommit} />);
    const input = screen.getByLabelText<HTMLInputElement>('End');
    input.focus();
    fireEvent.change(input, { target: { value: '00:05.5' } });
    fireEvent.keyDown(input, { key: 'Tab' });
    expect(onCommit).not.toHaveBeenCalled();
    fireEvent.keyDown(input, { key: 'Enter' });
    expect(onCommit).toHaveBeenCalledExactlyOnceWith(5.5);
  });

  it('commits a valid time on blur and restores an invalid one', () => {
    const onCommit = vi.fn();
    render(<TimeField label="Start" value={2} onCommit={onCommit} />);
    const input = screen.getByLabelText<HTMLInputElement>('Start');
    expect(input.value).toBe('00:02.0');
    fireEvent.change(input, { target: { value: '00:03.5' } });
    fireEvent.blur(input);
    expect(onCommit).toHaveBeenCalledWith(3.5);
    fireEvent.change(input, { target: { value: 'soon' } });
    expect(input.getAttribute('aria-invalid')).toBe('true');
    fireEvent.blur(input);
    expect(input.value).toBe('00:02.0');
  });
});
