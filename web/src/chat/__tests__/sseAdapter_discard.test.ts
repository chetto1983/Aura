import { describe, expect, it } from 'vitest';
import {
  messageParts,
  newAssistantTurn,
  reduceFrame,
  toThreadMessage,
  type AguiFrame,
} from '../sseAdapter';

// aura.discard (amendment #191): the shape measured live on 2026-08-30 — a draft the
// completion gate vetoed had been streamed, and the real answer followed it. The
// reducer must drop the repudiated text part and keep every other part (and the
// index maps that address them) intact.

const discard = (messageId: string): AguiFrame => ({
  type: 'CUSTOM',
  name: 'aura.discard',
  value: { message_id: messageId },
});

function text(messageId: string, delta: string): readonly AguiFrame[] {
  return [
    { type: 'TEXT_MESSAGE_START', messageId, role: 'assistant' } as AguiFrame,
    { type: 'TEXT_MESSAGE_CONTENT', messageId, delta },
    { type: 'TEXT_MESSAGE_END', messageId },
  ];
}

describe('sseAdapter — CUSTOM/aura.discard (amendment #191)', () => {
  it('drops the repudiated draft so only the answer that followed renders', () => {
    const state = newAssistantTurn('a1');
    for (const f of [
      ...text('m1', 'Il budget è esaurito, bozza uno.'),
      discard('m1'),
      ...text('m2', 'Ecco l’output, bozza due.'),
      discard('m2'),
      ...text('m3', 'Ecco i risultati ottenuti.'),
    ])
      reduceFrame(state, f);
    const parts = messageParts(toThreadMessage(state)).filter((p) => p.type === 'text');
    expect(parts).toEqual([{ type: 'text', text: 'Ecco i risultati ottenuti.' }]);
    expect(state.text).toBe('Ecco i risultati ottenuti.');
  });

  it('keeps the parts after the dropped draft addressable', () => {
    const state = newAssistantTurn('a1');
    for (const f of [
      ...text('m1', 'draft'),
      { type: 'TOOL_CALL_START', toolCallId: 't1', toolCallName: 'shell_exec' } as AguiFrame,
      discard('m1'),
      { type: 'TOOL_CALL_ARGS', toolCallId: 't1', delta: '{"cmd":"date"}' } as AguiFrame,
      ...text('m2', 'done'),
    ])
      reduceFrame(state, f);
    const parts = messageParts(toThreadMessage(state));
    expect(parts.map((p) => p.type)).toEqual(['tool-call', 'text']);
    expect(state.tools.get('t1')?.argsText).toBe('{"cmd":"date"}');
    expect(parts[0]).toMatchObject({ type: 'tool-call', argsText: '{"cmd":"date"}' });
    expect(parts[1]).toEqual({ type: 'text', text: 'done' });
  });

  it('ignores a discard for an unknown message or a malformed payload', () => {
    const state = newAssistantTurn('a1');
    for (const f of [
      ...text('m1', 'kept'),
      discard('nope'),
      { type: 'CUSTOM', name: 'aura.discard', value: { message_id: 7 } } as AguiFrame,
    ])
      reduceFrame(state, f);
    expect(state.text).toBe('kept');
    expect(messageParts(toThreadMessage(state))).toEqual([{ type: 'text', text: 'kept' }]);
  });

  it('a turn that ends with only a discarded draft renders empty, not the draft', () => {
    const state = newAssistantTurn('a1');
    for (const f of [...text('m1', 'draft'), discard('m1')]) reduceFrame(state, f);
    expect(state.text).toBe('');
    expect(messageParts(toThreadMessage(state))).toEqual([{ type: 'text', text: '' }]);
  });
});
