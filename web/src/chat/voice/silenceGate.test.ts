import { describe, expect, it } from 'vitest';
import {
  createSilenceGate,
  DEFAULT_MAX_UTTERANCE_MS,
  DEFAULT_SILENCE_MS,
  DEFAULT_SPEECH_LEVEL,
} from './silenceGate';

// Every case feeds levels with explicit timestamps: the gate owns no clock, so the
// whole endpointing policy is decided here rather than by how fast the suite runs.

describe('createSilenceGate', () => {
  it('never ends an utterance before speech was heard', () => {
    const gate = createSilenceGate({ silenceMs: 100 });
    for (let at = 0; at <= 5000; at += 50) {
      expect(gate.push(0, at)).toBe(false);
    }
    expect(gate.heardSpeech()).toBe(false);
  });

  it('ends after a continuous quiet stretch once speech was heard', () => {
    const gate = createSilenceGate({ speechLevel: 0.2, silenceMs: 300 });
    expect(gate.push(0.5, 0)).toBe(false);
    expect(gate.heardSpeech()).toBe(true);
    expect(gate.push(0.01, 100)).toBe(false);
    expect(gate.push(0.01, 300)).toBe(false); // 200ms of quiet — not yet
    expect(gate.push(0.01, 400)).toBe(true); // 300ms of quiet
  });

  it('a single loud frame resets the quiet stretch (a pause for breath is not the end)', () => {
    const gate = createSilenceGate({ speechLevel: 0.2, silenceMs: 300 });
    gate.push(0.5, 0);
    gate.push(0.01, 100);
    gate.push(0.5, 250); // still talking
    // Quiet is measured from the FIRST quiet sample after the reset (450), not from
    // the last loud one, so 560 is 110ms in and 760 is 310ms in.
    expect(gate.push(0.01, 450)).toBe(false);
    expect(gate.push(0.01, 560)).toBe(false);
    expect(gate.push(0.01, 760)).toBe(true);
  });

  it('fires exactly once — later samples are inert until reset', () => {
    const gate = createSilenceGate({ speechLevel: 0.2, silenceMs: 100 });
    gate.push(0.5, 0);
    expect(gate.push(0, 200)).toBe(false);
    expect(gate.push(0, 320)).toBe(true);
    expect(gate.push(0, 500)).toBe(false);
    expect(gate.push(0.9, 600)).toBe(false);
    gate.reset();
    expect(gate.heardSpeech()).toBe(false);
    expect(gate.push(0.5, 700)).toBe(false);
    expect(gate.push(0, 800)).toBe(false);
    expect(gate.push(0, 950)).toBe(true);
  });

  it('caps an utterance that never goes quiet (a fan, a TV) at maxUtteranceMs', () => {
    const gate = createSilenceGate({ speechLevel: 0.2, silenceMs: 300, maxUtteranceMs: 1000 });
    expect(gate.push(0.8, 0)).toBe(false);
    expect(gate.push(0.8, 500)).toBe(false);
    expect(gate.push(0.8, 1000)).toBe(true);
  });

  it('a level exactly at the threshold counts as speech', () => {
    const gate = createSilenceGate({ speechLevel: 0.3, silenceMs: 100 });
    expect(gate.push(0.3, 0)).toBe(false);
    expect(gate.heardSpeech()).toBe(true);
  });

  it('ships usable defaults', () => {
    expect(DEFAULT_SPEECH_LEVEL).toBeGreaterThan(0);
    expect(DEFAULT_SPEECH_LEVEL).toBeLessThan(1);
    expect(DEFAULT_SILENCE_MS).toBeGreaterThan(300);
    expect(DEFAULT_MAX_UTTERANCE_MS).toBeGreaterThan(DEFAULT_SILENCE_MS);
    const gate = createSilenceGate();
    expect(gate.push(0.9, 0)).toBe(false);
    expect(gate.push(0, 10)).toBe(false); // the first quiet sample starts the stretch
    expect(gate.push(0, 10 + DEFAULT_SILENCE_MS)).toBe(true);
  });
});
