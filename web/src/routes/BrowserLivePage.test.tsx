import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import '../i18n/i18n';
import { BrowserLivePage } from './BrowserLivePage';

class FakeEventSource {
  static last: FakeEventSource | null = null;
  readonly listeners = new Map<string, Set<EventListener>>();
  closed = false;
  constructor(readonly url: string) {
    FakeEventSource.last = this;
  }
  addEventListener(name: string, fn: EventListener) {
    const set = this.listeners.get(name) ?? new Set<EventListener>();
    set.add(fn);
    this.listeners.set(name, set);
  }
  removeEventListener(name: string, fn: EventListener) {
    this.listeners.get(name)?.delete(fn);
  }
  close() {
    this.closed = true;
  }
  emit(data: object) {
    act(() => {
      for (const fn of this.listeners.get('message') ?? [])
        fn(new MessageEvent('message', { data: JSON.stringify(data) }));
    });
  }
  fail() {
    act(() => {
      for (const fn of this.listeners.get('error') ?? []) fn(new Event('error'));
    });
  }
}

const frame = { type: 'frame', data: 'AAAA', metadata: { deviceWidth: 1280, deviceHeight: 720 } };
let fetchMock: ReturnType<typeof vi.fn<typeof fetch>>;

function sent(): Record<string, unknown>[] {
  return fetchMock.mock.calls.map(
    ([, init]) => JSON.parse(init?.body as string) as Record<string, unknown>,
  );
}

function renderPage() {
  render(
    <MemoryRouter initialEntries={['/browser/login']}>
      <Routes>
        <Route path="/browser/:session" element={<BrowserLivePage />} />
      </Routes>
    </MemoryRouter>,
  );
  const source = FakeEventSource.last;
  if (!source) throw new Error('the page opened no stream');
  return source;
}

beforeEach(() => {
  vi.stubGlobal('EventSource', FakeEventSource);
  fetchMock = vi.fn<typeof fetch>(() => Promise.resolve(new Response(null, { status: 204 })));
  vi.stubGlobal('fetch', fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
  FakeEventSource.last = null;
});

describe('BrowserLivePage', () => {
  it("streams the caller's own session and shows the page and its URL", () => {
    const source = renderPage();
    expect(source.url).toBe('/api/browser/sessions/login/stream');
    expect(screen.getByRole('status').textContent).toMatch(/connecting/i);

    source.emit(frame);
    source.emit({ type: 'url', url: 'https://portal.test/login' });
    expect(screen.getByRole('img').getAttribute('src')).toBe('data:image/jpeg;base64,AAAA');
    expect(screen.getByText('https://portal.test/login')).toBeTruthy();
    expect(screen.queryByRole('status')).toBeNull();
  });

  it('sends a click at page coordinates and keys with their real keyCode, in order', async () => {
    const source = renderPage();
    source.emit(frame);
    const img = screen.getByRole('img');
    img.getBoundingClientRect = () => ({
      left: 0,
      top: 0,
      width: 640,
      height: 360,
      right: 640,
      bottom: 360,
      x: 0,
      y: 0,
      toJSON: () => ({}),
    });

    fireEvent.mouseDown(img, { clientX: 320, clientY: 180, button: 0 });
    fireEvent.mouseUp(img, { clientX: 320, clientY: 180, button: 0 });
    expect(document.activeElement).toBe(screen.getByLabelText(/live browser page/i));
    const stage = screen.getByLabelText(/live browser page/i);
    fireEvent.keyDown(stage, { key: '.', code: 'Period', keyCode: 190 });
    fireEvent.keyUp(stage, { key: '.', code: 'Period', keyCode: 190 });

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledTimes(4);
    });
    expect(fetchMock.mock.calls[0]?.[0]).toBe('/api/browser/sessions/login/input');
    expect(sent()).toEqual([
      expect.objectContaining({
        type: 'input_mouse',
        eventType: 'mousePressed',
        x: 640,
        y: 360,
        button: 'left',
      }),
      expect.objectContaining({ type: 'input_mouse', eventType: 'mouseReleased', x: 640, y: 360 }),
      expect.objectContaining({
        type: 'input_keyboard',
        eventType: 'keyDown',
        key: '.',
        windowsVirtualKeyCode: 190,
        text: '.',
      }),
      expect.objectContaining({
        type: 'input_keyboard',
        eventType: 'keyUp',
        key: '.',
        windowsVirtualKeyCode: 190,
      }),
    ]);
  });

  it('types soft-keyboard text once, never also as a bubbled key event', async () => {
    const source = renderPage();
    source.emit(frame);
    const soft = screen.getByRole('textbox', { name: /show keyboard/i });
    fireEvent.keyDown(soft, { key: 'x', keyCode: 229 });
    fireEvent.input(soft, { target: { value: 'x' } });
    fireEvent.keyDown(soft, { key: 'Backspace', code: 'Backspace', keyCode: 8 });

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledTimes(3);
    });
    expect(sent().map((e) => [e.eventType, e.key])).toEqual([
      ['keyDown', 'x'],
      ['keyUp', 'x'],
      ['keyDown', 'Backspace'],
    ]);
  });

  it('pastes text as typed characters', async () => {
    const source = renderPage();
    source.emit(frame);
    fireEvent.paste(screen.getByLabelText(/live browser page/i), {
      clipboardData: { getData: () => 'ab' },
    });
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledTimes(4);
    });
    expect(
      sent()
        .filter((e) => e.eventType === 'keyDown')
        .map((e) => e.text),
    ).toEqual(['a', 'b']);
  });

  it('says why the view ended', async () => {
    const source = renderPage();
    source.emit(frame);
    source.emit({ type: 'relay_error', reason: 'no_such_session' });
    expect(source.closed).toBe(true);
    expect(screen.getByRole('status').textContent).toMatch(/no browser open under this name/i);

    const other = renderPage();
    other.fail();
    await waitFor(() => {
      expect(screen.getAllByRole('status').at(-1)?.textContent).toMatch(/connection .* was lost/i);
    });
  });

  it('stops when another window takes the browser over', async () => {
    fetchMock.mockResolvedValue(new Response(null, { status: 409 }));
    const source = renderPage();
    source.emit(frame);
    fireEvent.keyDown(screen.getByLabelText(/live browser page/i), {
      key: 'a',
      code: 'KeyA',
      keyCode: 65,
    });
    await waitFor(() => {
      expect(screen.getByRole('status').textContent).toMatch(/open in another window/i);
    });
    fireEvent.keyDown(screen.getByLabelText(/live browser page/i), {
      key: 'b',
      code: 'KeyB',
      keyCode: 66,
    });
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledTimes(1);
    });
  });

  it('sends nothing before the first frame', () => {
    renderPage();
    fireEvent.keyDown(screen.getByLabelText(/live browser page/i), {
      key: 'a',
      code: 'KeyA',
      keyCode: 65,
    });
    expect(fetchMock).not.toHaveBeenCalled();
  });
});
