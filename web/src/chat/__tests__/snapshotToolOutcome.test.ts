import { describe, expect, it } from 'vitest';
import { snapshotToThreadMessages } from '../sseAdapter_snapshot';
import { toolStatus } from '../toolStatus';

describe('persisted tool outcomes', () => {
  it.each([true, false])('keeps the native error flag with a preceding call: %s', (paired) => {
    const result =
      'error: previous result unknown after crash recovery for tool "shell_exec"; verify before re-running this tool call.';
    const messages = snapshotToThreadMessages({
      type: 'MESSAGES_SNAPSHOT',
      messages: [
        ...(paired
          ? [
              {
                role: 'assistant',
                toolCalls: [
                  {
                    id: 'call-1',
                    type: 'function',
                    function: { name: 'shell_exec', arguments: '{}' },
                  },
                ],
              },
            ]
          : []),
        { role: 'tool', toolCallId: 'call-1', content: result, isError: true },
      ],
    });
    const content = messages[0]?.content;
    const part = typeof content === 'string' ? undefined : content?.[0];
    expect(part?.type).toBe('tool-call');
    if (part?.type !== 'tool-call') throw new Error('tool part missing');
    expect(part.result).toBe(result);
    expect(part.isError).toBe(true);
    expect(toolStatus({ result, isError: part.isError })).toBe('error');
  });

  // A turn paused on ask_user has no result for that call yet. The server marks it
  // awaitingInput instead of pairing it with the crash-recovery error, and the card
  // shows it still open rather than failed or interrupted.
  it('shows a call awaiting the person as running, not failed', () => {
    const messages = snapshotToThreadMessages({
      type: 'MESSAGES_SNAPSHOT',
      messages: [
        {
          role: 'assistant',
          toolCalls: [
            {
              id: 'call-ask',
              type: 'function',
              function: { name: 'ask_user', arguments: '{"kind":"choice"}' },
              awaitingInput: true,
            },
          ],
        },
      ],
    });
    const content = messages[0]?.content;
    const part = typeof content === 'string' ? undefined : content?.[0];
    if (part?.type !== 'tool-call') throw new Error('tool part missing');
    const awaitingInput = (part as { awaitingInput?: unknown }).awaitingInput === true;
    expect(awaitingInput).toBe(true);
    const complete = { type: 'complete' as const };
    expect(toolStatus({ awaitingInput, partStatus: complete })).toBe('running');
    expect(toolStatus({ awaitingInput, result: 'Here in this chat', partStatus: complete })).toBe(
      'done',
    );
    expect(toolStatus({ partStatus: complete })).toBe('interrupted');
  });
});
