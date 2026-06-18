import { describe, expect, it } from 'vitest';
import { messageParts } from '../../sseAdapter';
import { snapshotToMessages } from '../snapshotToMessages';
import { isDisplayPayload, type DisplayPayload } from '../types';

// snapshotToMessages projects a persisted MESSAGES_SNAPSHOT (the GET
// /threads/{id}/messages body) onto the visible chat model for replay (D-06).
// The fixtures mirror the real backend wire shape: projectMessages emits each
// turn as { id, role, content, toolCalls?, toolCallId? }, where a tool turn's
// re-derived display rides the tool-call entry's `display` field.

const webDisplay: DisplayPayload = {
  type: 'web_result',
  tool_call_id: 'call-1',
  title: 'Web results',
  web_results: [
    { title: 'Meteo Roma', url: 'https://example.com/meteo', snippet: 'sunny 25C', ref_id: 's1' },
  ],
  sources: [{ ref_id: 's1', index: 1, type: 'web_result', url: 'https://example.com/meteo', cited: true }],
};

describe('snapshotToMessages — D-06 replay projection', () => {
  it('maps user / assistant text / tool turns in arrival order', () => {
    const messages = snapshotToMessages({
      type: 'MESSAGES_SNAPSHOT',
      messages: [
        { id: 'msg-1', role: 'system', content: 'hidden system prompt' },
        { id: 'msg-2', role: 'user', content: 'che tempo fa a Roma?' },
        {
          id: 'msg-3',
          role: 'assistant',
          content: '',
          toolCalls: [
            { id: 'call-1', type: 'function', function: { name: 'web_search', arguments: '{"q":"meteo"}' } },
          ],
        },
        { id: 'msg-4', role: 'tool', toolCallId: 'call-1', content: 'sunny 25C' },
        { id: 'msg-5', role: 'assistant', content: 'A Roma c’è il sole, 25°C.' },
      ],
    });

    // System turns stay out of the UI; the visible order is user → tool-bearing
    // assistant → final assistant.
    expect(messages.map((m) => m.role)).toEqual(['user', 'assistant', 'assistant']);
    expect(messages[0]).toMatchObject({
      role: 'user',
      content: [{ type: 'text', text: 'che tempo fa a Roma?' }],
    });

    const toolAssistant = messages[1];
    if (toolAssistant === undefined) throw new Error('expected the tool-bearing assistant turn');
    const toolParts = messageParts(toolAssistant).filter((p) => p.type === 'tool-call');
    expect(toolParts).toHaveLength(1);
    expect(toolParts[0]).toMatchObject({
      toolCallId: 'call-1',
      toolName: 'web_search',
      argsText: '{"q":"meteo"}',
      result: 'sunny 25C',
    });
  });

  it('attaches a re-derived display payload to the tool part (forward-compatible)', () => {
    const messages = snapshotToMessages({
      type: 'MESSAGES_SNAPSHOT',
      messages: [
        { id: 'msg-1', role: 'user', content: 'meteo' },
        {
          id: 'msg-2',
          role: 'assistant',
          content: '',
          toolCalls: [
            {
              id: 'call-1',
              type: 'function',
              function: { name: 'web_search', arguments: '{}' },
              display: webDisplay,
            },
          ],
        },
        { id: 'msg-3', role: 'tool', toolCallId: 'call-1', content: '[1] Meteo Roma' },
      ],
    });

    const toolAssistant = messages[1];
    if (toolAssistant === undefined) throw new Error('expected the assistant tool turn');
    const toolPart = messageParts(toolAssistant).find((p) => p.type === 'tool-call');
    if (toolPart?.type !== 'tool-call') {
      throw new Error('expected a tool-call part');
    }
    expect(toolPart.display).toEqual(webDisplay);
    expect(toolPart.display?.type).toBe('web_result');
  });

  it('tolerates a tool turn WITHOUT a display payload (no .display key)', () => {
    const messages = snapshotToMessages({
      type: 'MESSAGES_SNAPSHOT',
      messages: [
        { id: 'msg-1', role: 'user', content: 'hi' },
        {
          id: 'msg-2',
          role: 'assistant',
          content: '',
          toolCalls: [{ id: 'call-9', type: 'function', function: { name: 'noop_tool', arguments: '{}' } }],
        },
      ],
    });

    const toolAssistant = messages[1];
    if (toolAssistant === undefined) throw new Error('expected the assistant tool turn');
    const toolPart = messageParts(toolAssistant).find((p) => p.type === 'tool-call');
    if (toolPart?.type !== 'tool-call') {
      throw new Error('expected a tool-call part');
    }
    expect(toolPart.display).toBeUndefined();
  });

  it('ignores a malformed display value (not a DisplayPayload) — no corruption', () => {
    const messages = snapshotToMessages({
      type: 'MESSAGES_SNAPSHOT',
      messages: [
        { id: 'msg-1', role: 'user', content: 'hi' },
        {
          id: 'msg-2',
          role: 'assistant',
          content: '',
          toolCalls: [
            {
              id: 'call-1',
              type: 'function',
              function: { name: 'web_search', arguments: '{}' },
              display: { not: 'a payload' },
            },
          ],
        },
      ],
    });

    const toolAssistant = messages[1];
    if (toolAssistant === undefined) throw new Error('expected the assistant tool turn');
    const toolPart = messageParts(toolAssistant).find((p) => p.type === 'tool-call');
    if (toolPart?.type !== 'tool-call') {
      throw new Error('expected a tool-call part');
    }
    expect(toolPart.display).toBeUndefined();
  });

  it('returns an empty list for a non-snapshot body', () => {
    expect(snapshotToMessages({ type: 'NOT_A_SNAPSHOT' })).toEqual([]);
    expect(snapshotToMessages(null)).toEqual([]);
  });

  it('synthesizes a standalone assistant tool card when a tool row has no preceding tool-call part', () => {
    // A tool result row with no preceding assistant tool-call to merge into
    // (ordering/partial snapshot) must still materialise — the raw blob is never
    // lost on replay (D-06).
    const messages = snapshotToMessages({
      type: 'MESSAGES_SNAPSHOT',
      messages: [
        { id: 'msg-1', role: 'user', content: 'meteo' },
        { id: 'msg-2', role: 'tool', toolCallId: 'orphan-call', content: 'sunny 25C' },
      ],
    });

    expect(messages.map((m) => m.role)).toEqual(['user', 'assistant']);
    const toolAssistant = messages[1];
    if (toolAssistant === undefined) throw new Error('expected a synthesized assistant turn');
    const toolPart = messageParts(toolAssistant).find((p) => p.type === 'tool-call');
    if (toolPart?.type !== 'tool-call') throw new Error('expected a tool-call part');
    expect(toolPart).toMatchObject({ toolCallId: 'orphan-call', result: 'sunny 25C' });
  });
});

describe('isDisplayPayload', () => {
  it('accepts a value with a string type + string tool_call_id', () => {
    expect(isDisplayPayload(webDisplay)).toBe(true);
  });

  it('rejects null, primitives, and objects missing the discriminant keys', () => {
    expect(isDisplayPayload(null)).toBe(false);
    expect(isDisplayPayload('web_result')).toBe(false);
    expect(isDisplayPayload(42)).toBe(false);
    expect(isDisplayPayload({})).toBe(false);
    expect(isDisplayPayload({ type: 'web_result' })).toBe(false); // no tool_call_id
    expect(isDisplayPayload({ type: 1, tool_call_id: 'x' })).toBe(false); // non-string type
  });
});
