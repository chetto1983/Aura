import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { openWorkerStream } from './workerStream';

class FakeEventSource {
  static instances: FakeEventSource[] = [];

  readonly url: string;
  closed = false;
  private readonly listeners = new Map<string, Set<EventListener>>();

  constructor(url: string | URL) {
    this.url = String(url);
    FakeEventSource.instances.push(this);
  }

  addEventListener(type: string, listener: EventListener): void {
    const listeners = this.listeners.get(type) ?? new Set<EventListener>();
    listeners.add(listener);
    this.listeners.set(type, listeners);
  }

  removeEventListener(type: string, listener: EventListener): void {
    this.listeners.get(type)?.delete(listener);
  }

  close(): void {
    this.closed = true;
  }

  push(frame: unknown): void {
    const event = new MessageEvent('message', { data: JSON.stringify(frame) });
    for (const listener of this.listeners.get('message') ?? []) listener(event);
  }
}

describe('openWorkerStream', () => {
  beforeEach(() => {
    FakeEventSource.instances = [];
    vi.stubGlobal('EventSource', FakeEventSource);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('folds worker frames through the shipped reducer and encodes both route identifiers', () => {
    const onMessages = vi.fn();
    const stream = openWorkerStream('conv/one', 'child?two', { onMessages, onError: vi.fn() });
    const source = FakeEventSource.instances[0];

    expect(source?.url).toBe('/api/conversations/conv%2Fone/swarm/events?child=child%3Ftwo');
    source?.push({ type: 'TEXT_MESSAGE_START', messageId: 'answer' });
    source?.push({ type: 'TEXT_MESSAGE_CONTENT', messageId: 'answer', delta: 'working' });

    const latest = onMessages.mock.calls.at(-1)?.[0];
    expect(latest).toEqual([
      expect.objectContaining({
        id: 'child?two',
        role: 'assistant',
        content: [{ type: 'text', text: 'working' }],
      }),
    ]);
    stream.close();
  });

  it('removes listeners before closing so later frames cannot emit', () => {
    const onMessages = vi.fn();
    const stream = openWorkerStream('conv', 'child', { onMessages, onError: vi.fn() });
    const source = FakeEventSource.instances[0];

    source?.push({ type: 'TEXT_MESSAGE_START', messageId: 'answer' });
    expect(onMessages).toHaveBeenCalledTimes(1);

    stream.close();
    source?.push({ type: 'TEXT_MESSAGE_CONTENT', messageId: 'answer', delta: 'late' });
    expect(source?.closed).toBe(true);
    expect(onMessages).toHaveBeenCalledTimes(1);
  });
});
