import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useWorkerStatuses } from './useWorkerStatuses';

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

  push(childId: string): void {
    const event = new MessageEvent('message', {
      data: JSON.stringify({
        type: 'CUSTOM',
        name: 'aura.swarm.worker',
        value: {
          child_id: childId,
          status: 'running',
          last_event_at: '2026-08-29T20:00:00Z',
          events: 2,
          duration_sec: 4,
        },
      }),
    });
    for (const listener of this.listeners.get('message') ?? []) listener(event);
  }
}

beforeEach(() => {
  FakeEventSource.instances = [];
  vi.stubGlobal('EventSource', FakeEventSource);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('useWorkerStatuses', () => {
  it('uses exactly one status connection for six workers and closes it on unmount', () => {
    const view = renderHook(() => useWorkerStatuses('conv-1'));
    expect(FakeEventSource.instances).toHaveLength(1);
    expect(FakeEventSource.instances[0]?.url).toBe('/api/conversations/conv-1/swarm/events');

    act(() => {
      for (let index = 0; index < 6; index += 1) {
        FakeEventSource.instances[0]?.push(`worker-${String(index)}`);
      }
    });

    expect(view.result.current).toHaveLength(6);
    expect(view.result.current.get('worker-4')?.duration_sec).toBe(4);
    view.unmount();
    expect(FakeEventSource.instances[0]?.closed).toBe(true);
  });

  it('opens no connection without a conversation id', () => {
    const { result } = renderHook(() => useWorkerStatuses(''));
    expect(FakeEventSource.instances).toHaveLength(0);
    expect(result.current.size).toBe(0);
  });
});
