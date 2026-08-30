import { describe, expect, it } from 'vitest';
import goldenEvents from '../../../../internal/agui/testdata/golden-events.json';
import {
  budgetLimitFromStateDelta,
  newAssistantTurn,
  reduceFrame,
  toThreadMessage,
  type AguiFrame,
} from '../sseAdapter';

// Amendment #188: the terminal STATE_DELTA's limit_hit / steps_consumed — the exact
// keys internal/agent/llm_agent_events.go's terminalBudgetEvent and
// llm_agent_finalize.go's finalizeEvent put on the wire — are projected onto the
// assistant turn instead of being dropped.

const golden = goldenEvents as Record<string, AguiFrame>;

function stateDelta(ops: readonly { op: string; path: string; value: unknown }[]): AguiFrame {
  const base = golden.STATE_DELTA;
  if (base === undefined) throw new Error('golden fixture missing STATE_DELTA');
  return { ...base, delta: ops } as AguiFrame;
}

describe('budget trip projection', () => {
  it('reads limit_hit + steps_consumed off a STATE_DELTA and ignores one without them', () => {
    expect(
      budgetLimitFromStateDelta([
        { op: 'replace', path: '/limit_hit', value: 'max_steps' },
        { op: 'replace', path: '/steps_consumed', value: 25 },
        { op: 'replace', path: '/termination_reason', value: 'budget_exhausted' },
      ]),
    ).toEqual({ reason: 'max_steps', stepsConsumed: 25 });
    expect(budgetLimitFromStateDelta([{ op: 'replace', path: '/cost_usd', value: 0.1 }])).toBe(
      undefined,
    );
  });

  it('keeps the trip on the turn and exposes it as message metadata, beside the usage', () => {
    let state = newAssistantTurn('m1');
    // The finalize path folds usage AND trip keys into ONE terminal delta.
    state = reduceFrame(
      state,
      stateDelta([
        { op: 'replace', path: '/prompt_tokens', value: 1200 },
        { op: 'replace', path: '/completion_tokens', value: 80 },
        { op: 'replace', path: '/limit_hit', value: 'wallclock' },
        { op: 'replace', path: '/steps_consumed', value: 9 },
      ]),
    );
    expect(state.usage?.promptTokens).toBe(1200);
    expect(state.limit).toEqual({ reason: 'wallclock', stepsConsumed: 9 });
    const message = toThreadMessage(state);
    expect(message.metadata?.custom).toEqual({
      budgetLimit: { reason: 'wallclock', stepsConsumed: 9 },
    });
  });

  it('leaves a turn that ended on its own without metadata', () => {
    const state = reduceFrame(
      newAssistantTurn('m2'),
      stateDelta([{ op: 'replace', path: '/prompt_tokens', value: 10 }]),
    );
    expect(state.limit).toBe(undefined);
    expect(toThreadMessage(state).metadata).toBe(undefined);
  });
});
