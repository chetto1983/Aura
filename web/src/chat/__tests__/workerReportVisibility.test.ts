import { describe, expect, it } from 'vitest';
import { snapshotToThreadMessages } from '../sseAdapter_snapshot';

describe('internal worker report visibility', () => {
  it('keeps the coordinator answer and ordinary checklists while hiding a marked report', () => {
    const messages = snapshotToThreadMessages({
      type: 'MESSAGES_SNAPSHOT',
      messages: [
        { id: 'msg-1', role: 'user', content: 'Run the task' },
        { id: 'msg-2', role: 'assistant', content: '✅ ordinary answer' },
        {
          id: 'msg-3',
          role: 'assistant',
          workerReport: true,
          content: 'Internal worker instructions and raw report',
        },
        { id: 'msg-4', role: 'assistant', content: 'Consolidated final answer' },
      ],
    });
    expect(messages.map((message) => message.id)).toEqual(['msg-1', 'msg-2', 'msg-4']);
    expect(JSON.stringify(messages)).not.toContain('Internal worker instructions');
  });
});
