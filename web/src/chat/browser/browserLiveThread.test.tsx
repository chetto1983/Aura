import { screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import '../../i18n/i18n';
import { ExternalStoreChat } from '../ExternalStoreChat';
import { messagesSnapshotResponse, renderChat } from '../__tests__/chatTestHarness';

// The live view under the agent's turn, through the REAL runtime and snapshot rehydration: the
// chat opens the browser the turn left open, so the user can sign in without the model having to
// write a link (the VM turn of 2026-09-27 wrote none).

class FakeEventSource {
  static opened: FakeEventSource[] = [];
  readonly events = new Set<string>();
  closed = false;
  constructor(readonly url: string) {
    FakeEventSource.opened.push(this);
  }
  addEventListener(name: string) {
    this.events.add(name);
  }
  removeEventListener(name: string) {
    this.events.delete(name);
  }
  close() {
    this.closed = true;
  }
}

function openCall(id: string, args: object) {
  return {
    id: `a-${id}`,
    role: 'assistant',
    content: '',
    toolCalls: [
      {
        id,
        type: 'function',
        function: { name: 'browser__agent_browser_open', arguments: JSON.stringify(args) },
      },
    ],
  };
}

function stubThread(messages: readonly Record<string, unknown>[]): void {
  vi.stubGlobal(
    'fetch',
    vi.fn((url: unknown) =>
      Promise.resolve(
        url === '/threads/conv-1/messages'
          ? messagesSnapshotResponse(messages)
          : new Response('[]', { status: 200 }),
      ),
    ),
  );
}

describe('browser live view in the thread', () => {
  beforeEach(() => {
    FakeEventSource.opened = [];
    vi.stubGlobal('EventSource', FakeEventSource);
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("opens the browser the latest turn left open, under that turn's answer", async () => {
    stubThread([
      { id: 'u-1', role: 'user', content: 'open youtube, I will log in' },
      openCall('c1', { url: 'https://www.youtube.com/' }),
      { id: 't-1', role: 'tool', toolCallId: 'c1', content: 'YouTube' },
      { id: 'a-end', role: 'assistant', content: 'Open. Sign in when you are ready.' },
    ]);
    renderChat(<ExternalStoreChat threadId="conv-1" />);
    await screen.findByText('Open. Sign in when you are ready.');
    expect(screen.getByRole('region', { name: 'Live browser' })).toBeTruthy();
    const [stream, ...others] = FakeEventSource.opened;
    expect(others).toEqual([]);
    expect(stream?.url).toBe('/api/browser/sessions/default/stream');
    expect(stream?.closed).toBe(false);
    expect([...(stream?.events ?? [])].sort()).toEqual(['error', 'message']);
  });

  it('leaves an earlier turn closed once the user has answered it', async () => {
    stubThread([
      { id: 'u-1', role: 'user', content: 'open the bank' },
      openCall('c1', { url: 'https://bank.example', session: 'bank' }),
      { id: 't-1', role: 'tool', toolCallId: 'c1', content: 'Bank' },
      { id: 'a-end', role: 'assistant', content: 'Sign in here.' },
      { id: 'u-2', role: 'user', content: 'thanks, that is all' },
      { id: 'a-2', role: 'assistant', content: 'You are welcome.' },
    ]);
    renderChat(<ExternalStoreChat threadId="conv-1" />);
    await screen.findByText('You are welcome.');
    expect(screen.queryByRole('region', { name: 'Live browser' })).toBeNull();
    expect(FakeEventSource.opened).toEqual([]);
  });
});
