import { describe, expect, it } from 'vitest';
import { isGroupableToolPart, toolRun, TOOL_GROUP_MIN } from '../toolGrouping';

// toolGrouping (compact-chat spec §3.3) — run detection: threshold, broken runs
// (text/reasoning between), running-tail exclusion, inline-exception exclusion.

function tool(id: string, over: Record<string, unknown> = {}): Record<string, unknown> {
  return { type: 'tool-call', toolCallId: id, toolName: 't', argsText: '', result: 'ok', ...over };
}

describe('toolRun (§3.3)', () => {
  it('detects a contiguous settled run of ≥3 and reports it from any member', () => {
    const content = [tool('a'), tool('b'), tool('c')];
    for (const id of ['a', 'b', 'c']) {
      expect(toolRun(content, id)).toEqual({ startIndex: 0, ids: ['a', 'b', 'c'] });
    }
  });

  it(`returns null under the ${String(TOOL_GROUP_MIN)}-member threshold`, () => {
    expect(toolRun([tool('a'), tool('b')], 'a')).toBeNull();
    expect(toolRun([tool('a')], 'a')).toBeNull();
  });

  it('a non-tool part (text/reasoning) breaks the run', () => {
    const content = [
      tool('a'),
      tool('b'),
      { type: 'text', text: 'interlude' },
      tool('c'),
      tool('d'),
    ];
    expect(toolRun(content, 'a')).toBeNull(); // run of 2
    expect(toolRun(content, 'c')).toBeNull(); // run of 2
    const reasoningBreak = [tool('a'), { type: 'reasoning', text: 'hm' }, tool('b'), tool('c')];
    expect(toolRun(reasoningBreak, 'b')).toBeNull();
  });

  it('the still-running tool is never a member and never anchors a run', () => {
    const running = tool('d', { result: undefined });
    const content = [tool('a'), tool('b'), tool('c'), running];
    expect(toolRun(content, 'd')).toBeNull();
    // The settled prefix still groups without the running tail.
    expect(toolRun(content, 'a')).toEqual({ startIndex: 0, ids: ['a', 'b', 'c'] });
  });

  it('an errored member still counts as settled', () => {
    const content = [tool('a'), tool('b', { result: undefined, isError: true }), tool('c')];
    expect(toolRun(content, 'b')).toEqual({ startIndex: 0, ids: ['a', 'b', 'c'] });
  });

  it('inline display exceptions (system_event/local_artifact) break a run', () => {
    const content = [
      tool('a'),
      tool('b', { display: { type: 'system_event', tool_call_id: 'b' } }),
      tool('c'),
      tool('d'),
    ];
    expect(toolRun(content, 'a')).toBeNull();
    expect(toolRun(content, 'b')).toBeNull();
    expect(toolRun(content, 'c')).toBeNull();
    // Evidence displays (web_result) remain groupable members.
    const evidence = [
      tool('a', { display: { type: 'web_result', tool_call_id: 'a' } }),
      tool('b'),
      tool('c'),
    ];
    expect(toolRun(evidence, 'b')).toEqual({ startIndex: 0, ids: ['a', 'b', 'c'] });
  });

  it('locates a run offset from the start of the content', () => {
    const content = [{ type: 'reasoning', text: 'r' }, tool('a'), tool('b'), tool('c')];
    expect(toolRun(content, 'b')).toEqual({ startIndex: 1, ids: ['a', 'b', 'c'] });
  });

  it('returns null for an unknown id and malformed parts', () => {
    expect(toolRun([tool('a'), tool('b'), tool('c')], 'zz')).toBeNull();
    expect(toolRun([null, undefined, 42], 'a')).toBeNull();
  });
});

describe('isGroupableToolPart', () => {
  it('accepts settled tool parts, rejects running/inline/foreign parts', () => {
    expect(isGroupableToolPart(tool('a'))).toBe(true);
    expect(isGroupableToolPart(tool('a', { result: undefined }))).toBe(false);
    expect(isGroupableToolPart(tool('a', { result: undefined, isError: true }))).toBe(true);
    expect(
      isGroupableToolPart(tool('a', { display: { type: 'local_artifact', tool_call_id: 'a' } })),
    ).toBe(false);
    expect(isGroupableToolPart({ type: 'text', text: 'x' })).toBe(false);
    expect(isGroupableToolPart(null)).toBe(false);
  });
});
