// Pure encoding for the live view of an agent-browser session (prd.md §12). The relay in the box
// speaks agent-browser's stream protocol: frames and URL updates come out, mouse and keyboard
// events go in. Everything here is DOM-free so it can be mutation-tested.

/**
 * The page's viewport width in CSS pixels. The frame's height is NOT metadata.deviceHeight:
 * measured 2026-09-26, a 1280x577 viewport streamed 1280x577 JPEGs with deviceHeight 720 (the
 * emulated screen), so mapping y by it put every click about 25% too low.
 */
export interface FrameMeta {
  deviceWidth: number;
}

export type RelayMessage =
  | { kind: 'frame'; src: string; meta: FrameMeta }
  | { kind: 'url'; url: string }
  | { kind: 'error'; reason: string }
  | { kind: 'other' };

/** Parses one relay line. Anything unparseable or unknown is `other`, never an exception. */
export function parseRelayLine(line: string): RelayMessage {
  let msg: unknown;
  try {
    msg = JSON.parse(line);
  } catch {
    return { kind: 'other' };
  }
  if (typeof msg !== 'object' || msg === null) return { kind: 'other' };
  const m = msg as Record<string, unknown>;
  if (m.type === 'frame' && typeof m.data === 'string') {
    const meta = m.metadata as Record<string, unknown> | undefined;
    const deviceWidth = Number(meta?.deviceWidth);
    if (!(deviceWidth > 0)) return { kind: 'other' };
    return { kind: 'frame', src: `data:image/jpeg;base64,${m.data}`, meta: { deviceWidth } };
  }
  if (m.type === 'url' && typeof m.url === 'string') return { kind: 'url', url: m.url };
  if (m.type === 'relay_error' && typeof m.reason === 'string')
    return { kind: 'error', reason: m.reason };
  return { kind: 'other' };
}

export interface Rect {
  left: number;
  top: number;
  width: number;
  height: number;
}

/**
 * Maps a pointer position on the displayed frame to the page's CSS pixels, or null off-frame.
 * The frame keeps its aspect ratio, so one scale, from its width, serves both axes.
 */
export function toViewport(
  clientX: number,
  clientY: number,
  rect: Rect,
  meta: FrameMeta,
): { x: number; y: number } | null {
  if (rect.width <= 0 || rect.height <= 0) return null;
  const dx = clientX - rect.left;
  const dy = clientY - rect.top;
  if (dx < 0 || dy < 0 || dx > rect.width || dy > rect.height) return null;
  const scale = meta.deviceWidth / rect.width;
  return { x: Math.round(dx * scale), y: Math.round(dy * scale) };
}

export interface ModifierState {
  altKey: boolean;
  ctrlKey: boolean;
  metaKey: boolean;
  shiftKey: boolean;
}

/** CDP's modifier bitmask: Alt=1, Ctrl=2, Meta=4, Shift=8. */
export function modifiers(e: ModifierState): number {
  return (e.altKey ? 1 : 0) | (e.ctrlKey ? 2 : 0) | (e.metaKey ? 4 : 0) | (e.shiftKey ? 8 : 0);
}

export interface KeyLike extends ModifierState {
  key: string;
  code: string;
  keyCode: number;
}

/**
 * Encodes a keyboard event. The virtual key code must be the DOM event's own keyCode: a
 * character code in its place turned '.' (46) into VK_DELETE and dropped it (measured
 * 2026-09-26, spikes/agent-browser-auth). Printable text travels only on keyDown, and never
 * while Ctrl or Meta is held, so a shortcut is not also typed. Enter carries "\r": without it
 * CDP moves no caret and submits no form (measured in the live-view E2E the same day).
 */
export function keyEvent(e: KeyLike, eventType: 'keyDown' | 'keyUp') {
  const shortcut = e.ctrlKey || e.metaKey;
  const text = e.key === 'Enter' ? '\r' : e.key.length === 1 && !shortcut ? e.key : undefined;
  return {
    type: 'input_keyboard' as const,
    eventType,
    key: e.key,
    code: e.code,
    windowsVirtualKeyCode: e.keyCode,
    modifiers: modifiers(e),
    ...(eventType === 'keyDown' && text !== undefined ? { text } : {}),
  };
}

/** Encodes text that arrives without key events (paste, a phone's soft keyboard). */
const graphemes = new Intl.Segmenter(undefined, { granularity: 'grapheme' });

export function textEvents(text: string) {
  return Array.from(graphemes.segment(text), (s) => s.segment).flatMap((ch) => [
    {
      type: 'input_keyboard' as const,
      eventType: 'keyDown' as const,
      key: ch,
      code: '',
      windowsVirtualKeyCode: 0,
      modifiers: 0,
      text: ch,
    },
    {
      type: 'input_keyboard' as const,
      eventType: 'keyUp' as const,
      key: ch,
      code: '',
      windowsVirtualKeyCode: 0,
      modifiers: 0,
    },
  ]);
}

const BUTTONS = ['left', 'middle', 'right'] as const;

export function mouseEvent(
  eventType: 'mousePressed' | 'mouseReleased' | 'mouseMoved',
  point: { x: number; y: number },
  button: number,
  mods: ModifierState,
) {
  return {
    type: 'input_mouse' as const,
    eventType,
    x: point.x,
    y: point.y,
    button: eventType === 'mouseMoved' ? 'none' : (BUTTONS[button] ?? 'left'),
    clickCount: eventType === 'mouseMoved' ? 0 : 1,
    modifiers: modifiers(mods),
  };
}

export function wheelEvent(point: { x: number; y: number }, deltaX: number, deltaY: number) {
  return {
    type: 'input_mouse' as const,
    eventType: 'mouseWheel' as const,
    x: point.x,
    y: point.y,
    deltaX,
    deltaY,
  };
}
