import { describe, expect, it } from 'vitest';
import {
  keyEvent,
  modifiers,
  mouseEvent,
  parseRelayLine,
  textEvents,
  toViewport,
  wheelEvent,
} from './liveInput';

const none = { altKey: false, ctrlKey: false, metaKey: false, shiftKey: false };

describe('parseRelayLine', () => {
  it('turns a frame into a JPEG data URL with its page size', () => {
    const line = JSON.stringify({
      type: 'frame',
      data: 'AAAA',
      metadata: { deviceWidth: 1280, deviceHeight: 720 },
    });
    expect(parseRelayLine(line)).toEqual({
      kind: 'frame',
      src: 'data:image/jpeg;base64,AAAA',
      meta: { deviceWidth: 1280 },
    });
  });

  it('refuses a frame it could not map clicks onto', () => {
    for (const metadata of [
      undefined,
      {},
      { deviceWidth: 0, deviceHeight: 720 },
      { deviceWidth: -1280, deviceHeight: 720 },
      { deviceWidth: 'x', deviceHeight: 720 },
    ]) {
      expect(parseRelayLine(JSON.stringify({ type: 'frame', data: 'AAAA', metadata }))).toEqual({
        kind: 'other',
      });
    }
    expect(
      parseRelayLine(
        JSON.stringify({ type: 'frame', data: 42, metadata: { deviceWidth: 1, deviceHeight: 1 } }),
      ),
    ).toEqual({ kind: 'other' });
  });

  it('reads URL updates and relay errors', () => {
    expect(parseRelayLine('{"type":"url","url":"https://a.test/login"}')).toEqual({
      kind: 'url',
      url: 'https://a.test/login',
    });
    expect(parseRelayLine('{"type":"relay_error","reason":"no_such_session"}')).toEqual({
      kind: 'error',
      reason: 'no_such_session',
    });
    expect(parseRelayLine('{"type":"url","url":7}')).toEqual({ kind: 'other' });
    expect(parseRelayLine('{"type":"relay_error"}')).toEqual({ kind: 'other' });
  });

  it('reads the type, not just the fields a message happens to carry', () => {
    const meta = { deviceWidth: 10, deviceHeight: 10 };
    expect(
      parseRelayLine(JSON.stringify({ type: 'status', data: 'AAAA', metadata: meta })),
    ).toEqual({ kind: 'other' });
    expect(parseRelayLine(JSON.stringify({ type: 'tabs', url: 'https://a.test' }))).toEqual({
      kind: 'other',
    });
    expect(parseRelayLine(JSON.stringify({ type: 'status', reason: 'x' }))).toEqual({
      kind: 'other',
    });
    expect(
      parseRelayLine(
        JSON.stringify({
          type: 'frame',
          data: 'AAAA',
          metadata: { deviceWidth: -10, deviceHeight: 10 },
        }),
      ),
    ).toEqual({ kind: 'other' });
  });

  it('never throws on what it does not understand', () => {
    for (const line of [
      '',
      'not json',
      'null',
      '42',
      '"frame"',
      '{"type":"status","connected":true}',
    ]) {
      expect(parseRelayLine(line)).toEqual({ kind: 'other' });
    }
  });
});

describe('toViewport', () => {
  // A 1280x577 viewport (screen 1280x720) shown at half size: the measured case.
  const rect = { left: 100, top: 50, width: 640, height: 288.5 };
  const meta = { deviceWidth: 1280 };

  it('scales both axes by the frame width, never by the screen height', () => {
    expect(toViewport(100, 50, rect, meta)).toEqual({ x: 0, y: 0 });
    expect(toViewport(740, 338.5, rect, meta)).toEqual({ x: 1280, y: 577 });
    // The email field measured at (134.7, 90.4): a height-720 mapping sent y=113 and missed it.
    expect(toViewport(100 + 67.36, 50 + 45.19, rect, meta)).toEqual({ x: 135, y: 90 });
    expect(toViewport(100.4, 50.4, rect, meta)).toEqual({ x: 1, y: 1 });
  });

  it('ignores points off the frame or a frame with no size', () => {
    expect(toViewport(99, 60, rect, meta)).toBeNull();
    expect(toViewport(200, 49, rect, meta)).toBeNull();
    expect(toViewport(741, 60, rect, meta)).toBeNull();
    expect(toViewport(200, 339, rect, meta)).toBeNull();
    expect(toViewport(0, 0, { left: 0, top: 0, width: 0, height: 10 }, meta)).toBeNull();
    expect(toViewport(0, 0, { left: 0, top: 0, width: 10, height: 0 }, meta)).toBeNull();
  });
});

describe('modifiers', () => {
  it('uses the CDP bitmask', () => {
    expect(modifiers(none)).toBe(0);
    expect(modifiers({ ...none, altKey: true })).toBe(1);
    expect(modifiers({ ...none, ctrlKey: true })).toBe(2);
    expect(modifiers({ ...none, metaKey: true })).toBe(4);
    expect(modifiers({ ...none, shiftKey: true })).toBe(8);
    expect(modifiers({ altKey: true, ctrlKey: true, metaKey: true, shiftKey: true })).toBe(15);
  });
});

describe('keyEvent', () => {
  it("sends the DOM keyCode, never a character code ('.' is not VK_DELETE)", () => {
    expect(keyEvent({ ...none, key: '.', code: 'Period', keyCode: 190 }, 'keyDown')).toEqual({
      type: 'input_keyboard',
      eventType: 'keyDown',
      key: '.',
      code: 'Period',
      windowsVirtualKeyCode: 190,
      modifiers: 0,
      text: '.',
    });
  });

  it('sends Enter with the carriage return CDP needs to submit a form', () => {
    const enter = { ...none, key: 'Enter', code: 'Enter', keyCode: 13 };
    expect(keyEvent(enter, 'keyDown')).toMatchObject({ text: '\r', windowsVirtualKeyCode: 13 });
    expect(keyEvent(enter, 'keyUp')).not.toHaveProperty('text');
  });

  it('carries text only on keyDown of a printable key without Ctrl or Meta', () => {
    const a = { ...none, key: 'A', code: 'KeyA', keyCode: 65, shiftKey: true };
    expect(keyEvent(a, 'keyDown')).toMatchObject({ text: 'A', modifiers: 8 });
    expect(keyEvent(a, 'keyUp')).not.toHaveProperty('text');
    expect(keyEvent({ ...a, ctrlKey: true }, 'keyDown')).not.toHaveProperty('text');
    expect(keyEvent({ ...a, metaKey: true }, 'keyDown')).not.toHaveProperty('text');
    expect(
      keyEvent({ ...none, key: 'Tab', code: 'Tab', keyCode: 9 }, 'keyDown'),
    ).not.toHaveProperty('text');
  });
});

describe('textEvents', () => {
  it('types each character as a keyDown with text and a keyUp', () => {
    const events = textEvents('é1');
    expect(events).toHaveLength(4);
    expect(events[0]).toEqual({
      type: 'input_keyboard',
      eventType: 'keyDown',
      key: 'é',
      code: '',
      windowsVirtualKeyCode: 0,
      modifiers: 0,
      text: 'é',
    });
    expect(events[1]).toEqual({
      type: 'input_keyboard',
      eventType: 'keyUp',
      key: 'é',
      code: '',
      windowsVirtualKeyCode: 0,
      modifiers: 0,
    });
    expect(events[2]).toMatchObject({ eventType: 'keyDown', text: '1' });
    expect(textEvents('👩‍👩‍👧').filter((e) => e.eventType === 'keyDown')).toHaveLength(1);
    expect(textEvents('')).toEqual([]);
  });
});

describe('mouseEvent and wheelEvent', () => {
  const p = { x: 3, y: 4 };
  it('maps buttons and counts one click', () => {
    expect(mouseEvent('mousePressed', p, 0, none)).toEqual({
      type: 'input_mouse',
      eventType: 'mousePressed',
      x: 3,
      y: 4,
      button: 'left',
      clickCount: 1,
      modifiers: 0,
    });
    expect(mouseEvent('mouseReleased', p, 1, none)).toMatchObject({
      button: 'middle',
      clickCount: 1,
    });
    expect(mouseEvent('mousePressed', p, 2, { ...none, shiftKey: true })).toMatchObject({
      button: 'right',
      modifiers: 8,
    });
    expect(mouseEvent('mousePressed', p, 7, none)).toMatchObject({ button: 'left' });
  });

  it('moves press no button and count no click', () => {
    expect(mouseEvent('mouseMoved', p, 0, none)).toMatchObject({ button: 'none', clickCount: 0 });
  });

  it('scrolls with the wheel deltas', () => {
    expect(wheelEvent(p, 1, -120)).toEqual({
      type: 'input_mouse',
      eventType: 'mouseWheel',
      x: 3,
      y: 4,
      deltaX: 1,
      deltaY: -120,
    });
  });
});
