import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ThreadMessageLike } from '@assistant-ui/react';
import { decodeWorkerStatus, openWorkerStatusStream, openWorkerStream } from './workerStream';

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
    const type =
      typeof frame === 'object' &&
      frame !== null &&
      typeof (frame as { type?: unknown }).type === 'string'
        ? (frame as { type: string }).type
        : 'message';
    const event = new MessageEvent(type, { data: JSON.stringify(frame) });
    for (const listener of this.listeners.get(type) ?? []) listener(event);
  }

  emit(type: string, event: Event): void {
    for (const listener of this.listeners.get(type) ?? []) listener(event);
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
    const onMessages = vi.fn<(messages: readonly ThreadMessageLike[]) => void>();
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
    const onMessages = vi.fn<(messages: readonly ThreadMessageLike[]) => void>();
    const stream = openWorkerStream('conv', 'child', { onMessages, onError: vi.fn() });
    const source = FakeEventSource.instances[0];

    source?.push({ type: 'TEXT_MESSAGE_START', messageId: 'answer' });
    expect(onMessages).toHaveBeenCalledTimes(1);

    stream.close();
    source?.push({ type: 'TEXT_MESSAGE_CONTENT', messageId: 'answer', delta: 'late' });
    expect(source?.closed).toBe(true);
    expect(onMessages).toHaveBeenCalledTimes(1);
  });

  it('ignores non-message events and reports malformed frames and transport errors', () => {
    const onMessages = vi.fn<(messages: readonly ThreadMessageLike[]) => void>();
    const onError = vi.fn();
    openWorkerStream('conv', 'child', { onMessages, onError });
    const source = FakeEventSource.instances[0];

    source?.emit('message', new Event('message'));
    source?.emit('message', new MessageEvent('message', { data: '{' }));
    source?.emit('error', new Event('error'));

    expect(onMessages).not.toHaveBeenCalled();
    expect(onError).toHaveBeenCalledTimes(2);
  });
});

describe('worker status stream', () => {
  beforeEach(() => {
    FakeEventSource.instances = [];
    vi.stubGlobal('EventSource', FakeEventSource);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('decodes only complete worker status payloads', () => {
    expect(decodeWorkerStatus(null)).toBeNull();
    expect(decodeWorkerStatus({ child_id: 'child', status: 'awaiting_input' })).toBeNull();
    expect(
      decodeWorkerStatus({
        child_id: 'child',
        status: 'stalled',
        last_event_at: '2026-08-29T20:00:00Z',
        events: 3,
        duration_sec: 8,
      }),
    ).toEqual(expect.objectContaining({ child_id: 'child', status: 'stalled', events: 3 }));
  });

  it('filters unrelated and invalid frames while surfacing stream errors', () => {
    const onStatus = vi.fn();
    const onError = vi.fn();
    const stream = openWorkerStatusStream('conv/one', { onStatus, onError });
    const source = FakeEventSource.instances[0];

    expect(source?.url).toBe('/api/conversations/conv%2Fone/swarm/events');
    source?.emit('message', new Event('message'));
    source?.push({ type: 'CUSTOM', name: 'other', value: {} });
    source?.push({ type: 'CUSTOM', name: 'aura.swarm.worker', value: {} });
    source?.push({
      type: 'CUSTOM',
      name: 'aura.swarm.worker',
      value: {
        child_id: 'child',
        status: 'running',
        last_event_at: '2026-08-29T20:00:00Z',
        events: 2,
        duration_sec: 4,
      },
    });
    source?.emit('message', new MessageEvent('message', { data: '{' }));
    source?.emit('error', new Event('error'));

    expect(onStatus).toHaveBeenCalledOnce();
    expect(onStatus).toHaveBeenCalledWith(expect.objectContaining({ child_id: 'child' }));
    expect(onError).toHaveBeenCalledTimes(2);
    stream.close();
    expect(source?.closed).toBe(true);
  });
});
