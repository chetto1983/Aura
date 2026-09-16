import { screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import '../../i18n/i18n';
import { ExternalStoreChat } from '../ExternalStoreChat';
import { messagesSnapshotResponse, renderChat } from '../__tests__/chatTestHarness';
import { REPLAYED_RESULT_MARKER } from './generationState';

// A detached video replayed through the REAL runtime + snapshot rehydration path: the
// frame renders inline exactly once, never as a tool row, and never joins or splits the
// ToolGroup of the completed tools around it the wrong way.

interface Call {
  readonly id: string;
  readonly name: string;
  readonly args: string;
  readonly result: string;
}

function settled(id: string): Call {
  return { id, name: `tool_${id}`, args: '{"query":"q"}', result: `r-${id}` };
}

const DEFERRED: Call = {
  id: 'video-1',
  name: 'video_generate',
  args: '{"prompt":"A moving sea","aspect_ratio":"16:9"}',
  result: `{"status":"in_progress","job_id":"job-1"}${REPLAYED_RESULT_MARKER}`,
};

function stubThread(calls: readonly Call[]): void {
  const messages = [
    { id: 'msg-0', role: 'user', content: 'make a clip' },
    {
      id: 'msg-1',
      role: 'assistant',
      content: '',
      toolCalls: calls.map((call) => ({
        id: call.id,
        type: 'function',
        function: { name: call.name, arguments: call.args },
      })),
    },
    ...calls.map((call, index) => ({
      id: `tool-${String(index)}`,
      role: 'tool',
      toolCallId: call.id,
      content: call.result,
    })),
    { id: 'msg-end', role: 'assistant', content: 'On its way.' },
  ];
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

function looseRows(): HTMLElement[] {
  return screen
    .queryAllByTestId('tool-row')
    .filter((row) => row.closest('[data-testid="tool-group-body"]') === null);
}

describe('generation frames in a replayed thread', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('renders a deferred video inline once, with no tool row of its own', async () => {
    stubThread([DEFERRED]);
    renderChat(<ExternalStoreChat threadId="conv-1" />);
    await screen.findByText('On its way.');
    expect(screen.getAllByTestId('generation-frame')).toHaveLength(1);
    expect(screen.getByText('Arriving in this chat')).toBeTruthy();
    expect(screen.getByText('A moving sea')).toBeTruthy();
    expect(screen.queryByRole('timer')).toBeNull();
    expect(screen.queryAllByTestId('tool-row')).toHaveLength(0);
  });

  it('breaks the run: completed tools on each side group without the video', async () => {
    stubThread([
      settled('c1'),
      settled('c2'),
      settled('c3'),
      DEFERRED,
      settled('c4'),
      settled('c5'),
      settled('c6'),
    ]);
    renderChat(<ExternalStoreChat threadId="conv-1" />);
    await screen.findByText('On its way.');
    expect(screen.getAllByTestId('tool-group')).toHaveLength(2);
    expect(screen.getAllByTestId('generation-frame')).toHaveLength(1);
    for (const body of screen.getAllByTestId('tool-group-body')) {
      expect(body.querySelectorAll('[data-testid="tool-row"]')).toHaveLength(3);
      expect(body.querySelector('[data-testid="generation-frame"]')).toBeNull();
      expect(body.textContent).not.toContain('video_generate');
    }
    expect(looseRows()).toHaveLength(0);
  });

  it('does not let two completed tools on each side form a group through the video', async () => {
    stubThread([settled('c1'), settled('c2'), DEFERRED, settled('c3'), settled('c4')]);
    renderChat(<ExternalStoreChat threadId="conv-1" />);
    await screen.findByText('On its way.');
    expect(screen.queryAllByTestId('tool-group')).toHaveLength(0);
    expect(looseRows()).toHaveLength(4);
    expect(screen.getAllByTestId('generation-frame')).toHaveLength(1);
  });

  it('leaves a failed video job on the ordinary tool card', async () => {
    stubThread([
      {
        ...DEFERRED,
        result: '{"error":"job_failed","message":"The video generation job failed."}',
      },
    ]);
    renderChat(<ExternalStoreChat threadId="conv-1" />);
    await screen.findByText('On its way.');
    expect(screen.queryByTestId('generation-frame')).toBeNull();
    expect(screen.getAllByTestId('tool-row')).toHaveLength(1);
  });
});
