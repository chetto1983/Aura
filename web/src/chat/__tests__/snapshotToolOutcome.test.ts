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
});
