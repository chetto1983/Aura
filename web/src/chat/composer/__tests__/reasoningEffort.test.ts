import { act, renderHook, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { buildAuraRunBody } from '../../auraRunBody';
import type { StreamRunOptions } from '../../sseAdapter';
import {
  fetchReasoningCapabilities,
  REASONING_CAPABILITIES_FLOOR,
  REASONING_CAPABILITIES_PATH,
} from '../api';
import { useReasoningEffort } from '../useReasoningEffort';

// reasoningEffort.test.ts is the WEBMODEL-01/03 state+wire unit gate: the run-body effort fold
// (auto OMITTED, the six fixed levels folded), the capability fetch degrade-to-floor (D-09), and
// the per-conversation hydrate + clamp-unsupported→auto (D-13). It has NO backend dependency —
// fetch is stubbed and the hook is driven headless — mirroring the sseAdapter + useConversations
// unit precedent.

function opts(over: Partial<StreamRunOptions> = {}): StreamRunOptions {
  return {
    threadId: 'conv-1',
    userText: 'hi',
    signal: new AbortController().signal,
    onUpdate: () => undefined,
    ...over,
  };
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

describe('buildAuraRunBody effort fold', () => {
  it('folds a fixed effort into aura.effort', () => {
    const body = buildAuraRunBody('m1', opts({ effort: 'high' })) as { aura?: { effort?: string } };
    expect(body.aura?.effort).toBe('high');
  });

  it('folds off (a fixed level) into aura.effort', () => {
    const body = buildAuraRunBody('m1', opts({ effort: 'off' })) as { aura?: { effort?: string } };
    expect(body.aura?.effort).toBe('off');
  });

  it('OMITS auto — no aura key when nothing else is set', () => {
    const body = buildAuraRunBody('m1', opts({ effort: 'auto' }));
    expect(body).not.toHaveProperty('aura');
  });

  it('OMITS effort when undefined', () => {
    const body = buildAuraRunBody('m1', opts());
    expect(body).not.toHaveProperty('aura');
  });

  it('coexists with a pinned skill in the same aura envelope', () => {
    const body = buildAuraRunBody('m1', opts({ effort: 'mid', skill: 'skill-creator' })) as {
      aura?: Record<string, string>;
    };
    expect(body.aura).toEqual({ skill: 'skill-creator', effort: 'mid' });
  });
});

describe('fetchReasoningCapabilities', () => {
  afterEach(() => vi.unstubAllGlobals());

  it('targets the composer reasoning-capabilities route', () => {
    expect(REASONING_CAPABILITIES_PATH).toBe('/api/composer/reasoning-capabilities');
  });

  it('returns the parsed capability payload on success', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          jsonResponse({ levels: ['auto', 'off', 'high'], default: 'auto', detected: true }),
        ),
      ),
    );
    const caps = await fetchReasoningCapabilities();
    expect(caps).toEqual({ levels: ['auto', 'off', 'high'], default: 'auto', detected: true });
  });

  it('degrades to the safe floor {auto,off} on any throw', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.reject(new Error('network'))),
    );
    const caps = await fetchReasoningCapabilities();
    expect(caps).toEqual(REASONING_CAPABILITIES_FLOOR);
    expect(caps.detected).toBe(false);
  });

  it('degrades to the safe floor on a non-ok response', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(jsonResponse({}, 503))),
    );
    const caps = await fetchReasoningCapabilities();
    expect(caps.levels).toEqual(['auto', 'off']);
    expect(caps.detected).toBe(false);
  });

  it('degrades to the floor when the body omits levels', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(jsonResponse({ default: 'auto', detected: true }))),
    );
    const caps = await fetchReasoningCapabilities();
    expect(caps.levels).toEqual(['auto', 'off']);
  });
});

describe('useReasoningEffort', () => {
  it('hydrates the persisted per-conversation value on thread open', async () => {
    const { result } = renderHook(() =>
      useReasoningEffort('conv-1', 'high', ['auto', 'off', 'low', 'high']),
    );
    await waitFor(() => {
      expect(result.current.effort).toBe('high');
    });
  });

  it('hydrates auto when the stored value is empty/absent', async () => {
    const { result } = renderHook(() => useReasoningEffort('conv-1', '', ['auto', 'off']));
    await waitFor(() => {
      expect(result.current.effort).toBe('auto');
    });
  });

  it('clamps a stored value the active model does not advertise back to auto (D-13)', async () => {
    const { result } = renderHook(() => useReasoningEffort('conv-1', 'high', ['auto', 'off']));
    await waitFor(() => {
      expect(result.current.effort).toBe('auto');
    });
  });

  it('keeps a manual pick — there is NO send-time reset (unlike the pinned skill)', () => {
    const { result } = renderHook(() =>
      useReasoningEffort('conv-1', 'auto', ['auto', 'off', 'high']),
    );
    act(() => {
      result.current.setEffort('high');
    });
    expect(result.current.effort).toBe('high');
  });

  it('re-hydrates when a different thread opens', async () => {
    const { result, rerender } = renderHook(
      ({ id, eff }: { id: string; eff: string }) =>
        useReasoningEffort(id, eff, ['auto', 'off', 'high']),
      { initialProps: { id: 'conv-1', eff: 'high' } },
    );
    await waitFor(() => {
      expect(result.current.effort).toBe('high');
    });
    rerender({ id: 'conv-2', eff: 'off' });
    await waitFor(() => {
      expect(result.current.effort).toBe('off');
    });
  });
});
