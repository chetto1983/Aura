import { describe, expect, it, vi } from 'vitest';
import {
  ARCADE_LABEL_MAX_LENGTH,
  contrastTextColor,
  drawStudioEdgeLabel,
  drawStudioNodeLabel,
  fitCanvasText,
  studioCaption,
  studioNodeRadius,
  wrapCanvasLabel,
} from '../SigmaCanvas_labels';

function recordingContext() {
  const fillText = vi.fn<(text: string, x: number, y: number, maxWidth?: number) => void>();
  const restore = vi.fn();
  const rotate = vi.fn<(angle: number) => void>();
  const save = vi.fn();
  const strokeText = vi.fn<(text: string, x: number, y: number, maxWidth?: number) => void>();
  const translate = vi.fn<(x: number, y: number) => void>();
  const context = {
    fillStyle: '',
    font: '',
    lineJoin: 'miter',
    lineWidth: 1,
    restore,
    rotate,
    save,
    strokeStyle: '',
    textAlign: 'start',
    textBaseline: 'alphabetic',
    translate,
    fillText,
    strokeText,
    measureText: vi.fn((text: string) => ({ width: text.length * 7 })),
  };
  return {
    context: context as unknown as CanvasRenderingContext2D,
    fillText,
    restore,
    rotate,
    save,
    strokeText,
    translate,
  };
}

const SETTINGS = {
  labelColor: { color: '#FFFFFF' },
  labelFont: 'sans-serif',
  labelSize: 13,
  labelWeight: '600',
  edgeLabelColor: { color: '#C7CAD1' },
  edgeLabelFont: 'sans-serif',
  edgeLabelSize: 10,
  edgeLabelWeight: '600',
};

describe('ArcadeDB Studio-style graph labels', () => {
  it("uses Studio's 25 + 6×length diameter rule with a 200 px cap", () => {
    expect(studioNodeRadius('', 4)).toBe(12.5);
    expect(studioNodeRadius('5889', 4)).toBe(24.5);
    expect(studioNodeRadius('x'.repeat(100), 4)).toBe(100);
    expect(studioNodeRadius('x', 20)).toBe(36);
  });

  it('preserves human captions through 45 characters and then adds an ellipsis', () => {
    expect(studioCaption('  Aura memory  ')).toBe('Aura memory');
    expect(studioCaption('x'.repeat(ARCADE_LABEL_MAX_LENGTH))).toBe(
      'x'.repeat(ARCADE_LABEL_MAX_LENGTH),
    );
    expect(studioCaption('x'.repeat(ARCADE_LABEL_MAX_LENGTH + 1))).toBe(
      `${'x'.repeat(ARCADE_LABEL_MAX_LENGTH)}…`,
    );
  });

  it('chooses the higher-contrast text color for light and dark node fills', () => {
    expect(contrastTextColor('#AECBFA')).toBe('#101114');
    expect(contrastTextColor('#3C4043')).toBe('#FFFFFF');
    expect(contrastTextColor('#000000')).toBe('#FFFFFF');
    expect(contrastTextColor('#FFFFFF')).toBe('#101114');
    expect(contrastTextColor('#757575')).toBe('#FFFFFF');
    expect(contrastTextColor('#767676')).toBe('#101114');
    expect(contrastTextColor('#00FF00')).toBe('#101114');
    expect(contrastTextColor('not-a-color')).toBe('#FFFFFF');
    expect(contrastTextColor('x#AECBFA')).toBe('#FFFFFF');
    expect(contrastTextColor('#AECBFAx')).toBe('#FFFFFF');
  });

  it('fits long text with an ellipsis at exact canvas-width boundaries', () => {
    const { context } = recordingContext();
    expect(fitCanvasText(context, 'abcd', 28)).toBe('abcd');
    expect(fitCanvasText(context, 'abcd', 27)).toBe('ab…');
    expect(fitCanvasText(context, 'abcd', 7)).toBe('…');
    expect(fitCanvasText(context, 'abcd', 6)).toBe('');
  });

  it('handles whitespace, exact-width words and the maximum line count', () => {
    const { context } = recordingContext();
    expect(wrapCanvasLabel(context, '   ', 49, 2)).toEqual([]);
    expect(wrapCanvasLabel(context, 'one two', 49, 0)).toEqual([]);
    expect(wrapCanvasLabel(context, '  one   two  ', 49, 2)).toEqual(['one two']);
    expect(wrapCanvasLabel(context, 'one two three four', 28, 2)).toEqual(['one', 'two…']);
    expect(wrapCanvasLabel(context, 'abcdef', 21, 1)).toEqual(['ab…']);
  });

  it('wraps a caption into bounded lines and centres those lines inside the node', () => {
    const { context, fillText, restore, save } = recordingContext();
    expect(wrapCanvasLabel(context, 'Arcade graph memory', 50, 3)).toEqual([
      'Arcade',
      'graph',
      'memory',
    ]);

    drawStudioNodeLabel(
      context,
      { x: 60, y: 40, size: 44, label: 'Arcade graph memory', color: '#AECBFA' },
      SETTINGS as never,
    );
    const calls = fillText.mock.calls;
    expect(calls).toHaveLength(3);
    expect(calls.every(([, x]) => x === 60)).toBe(true);
    for (const [, , , maxWidth] of calls) expect(maxWidth).toBeCloseTo(69.52);
    const yPositions = calls.map(([, , y]) => y);
    expect(yPositions[0]).toBeCloseTo(25.96);
    expect(yPositions[1]).toBe(40);
    expect(yPositions[2]).toBeCloseTo(54.04);
    expect(context.font).toBe('600 13px sans-serif');
    expect(context.textAlign).toBe('center');
    expect(context.textBaseline).toBe('middle');
    expect(context.fillStyle).toBe('#101114');
    expect(save).toHaveBeenCalledTimes(1);
    expect(restore).toHaveBeenCalledTimes(1);
  });

  it('uses the minimum node font geometry and skips an empty caption', () => {
    const small = recordingContext();
    drawStudioNodeLabel(
      small.context,
      { x: 10, y: 20, size: 8, label: 'A', color: '#3C4043' },
      SETTINGS as never,
    );
    expect(small.context.font).toBe('600 10px sans-serif');
    expect(small.context.fillStyle).toBe('#FFFFFF');
    expect(small.fillText).toHaveBeenCalledWith('A', 10, 20, 16);

    const empty = recordingContext();
    drawStudioNodeLabel(
      empty.context,
      { x: 0, y: 0, size: 20, label: '', color: '#AECBFA' },
      SETTINGS as never,
    );
    expect(empty.save).not.toHaveBeenCalled();
    expect(empty.fillText).not.toHaveBeenCalled();
  });

  it('rotates the relationship caption with the edge and outlines it before filling', () => {
    const { context, fillText, restore, rotate, save, strokeText, translate } = recordingContext();
    drawStudioEdgeLabel(
      context,
      { label: 'knows', color: '#C7CAD1', size: 1.4 },
      { x: 0, y: 0, size: 10 },
      { x: 120, y: 0, size: 10 },
      SETTINGS as never,
    );

    expect(translate).toHaveBeenCalledWith(60, 0);
    expect(rotate).toHaveBeenCalledWith(0);
    expect(context.strokeStyle).toBe('#131314');
    expect(context.lineWidth).toBe(5);
    expect(context.font).toBe('600 10px sans-serif');
    expect(context.textAlign).toBe('center');
    expect(context.textBaseline).toBe('bottom');
    expect(context.lineJoin).toBe('round');
    expect(context.fillStyle).toBe('#C7CAD1');
    expect(strokeText).toHaveBeenCalledWith('knows', 0, -4.4, 100);
    expect(fillText).toHaveBeenCalledWith('knows', 0, -4.4, 100);
    expect(save).toHaveBeenCalledTimes(1);
    expect(restore).toHaveBeenCalledTimes(1);
  });

  it('keeps reverse diagonal edge text upright and centred after node clipping', () => {
    const { context, rotate, translate } = recordingContext();
    drawStudioEdgeLabel(
      context,
      { label: 'relates', color: '#C7CAD1', size: 1 },
      { x: 120, y: 80, size: 10 },
      { x: 0, y: 0, size: 10 },
      SETTINGS as never,
    );
    expect(translate).toHaveBeenCalledWith(60, 40);
    expect(rotate.mock.calls[0]?.[0]).toBeCloseTo(Math.atan2(80, 120));
  });

  it('does not draw missing, overlapping or too-short edge captions', () => {
    const missing = recordingContext();
    drawStudioEdgeLabel(
      missing.context,
      { label: '', color: '#C7CAD1', size: 1 },
      { x: 0, y: 0, size: 10 },
      { x: 120, y: 0, size: 10 },
      SETTINGS as never,
    );
    expect(missing.save).not.toHaveBeenCalled();

    const overlapping = recordingContext();
    drawStudioEdgeLabel(
      overlapping.context,
      { label: 'knows', color: '#C7CAD1', size: 1 },
      { x: 0, y: 0, size: 10 },
      { x: 20, y: 0, size: 10 },
      SETTINGS as never,
    );
    expect(overlapping.save).not.toHaveBeenCalled();

    const short = recordingContext();
    drawStudioEdgeLabel(
      short.context,
      { label: 'knows', color: '#C7CAD1', size: 1 },
      { x: 0, y: 0, size: 10 },
      { x: 21, y: 0, size: 10 },
      SETTINGS as never,
    );
    expect(short.save).toHaveBeenCalledTimes(1);
    expect(short.restore).toHaveBeenCalledTimes(1);
    expect(short.fillText).not.toHaveBeenCalled();
  });
});
