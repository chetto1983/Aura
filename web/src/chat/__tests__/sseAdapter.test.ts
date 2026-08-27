import { describe, expect, it } from 'vitest';
// The test is driven by the REAL captured translator output — the golden frame
// fixture the Go SSE tests use (internal/agui/testdata/golden-events.json),
// imported as a JSON module so no synthetic inline shapes leak in (no-skip-as-
// green discipline). The fixture is a sibling of web/; vitest.config server.fs.allow
// permits the one-level-up read. The fixture is a map of canonical event-name → the
// exact JSON the AG-UI SSE writer emits for that event type; the tests assemble
// realistic per-turn SEQUENCES from those captured shapes.
import goldenEvents from '../../../../internal/agui/testdata/golden-events.json';
import {
  cacheHitRatio,
  isUsageDelta,
  messageParts,
  newAssistantTurn,
  parseSSEBlock,
  readSSEFrames,
  reduceFrame,
  toThreadMessage,
  toolCallIdFromDelta,
  usageFromStateDelta,
  type AguiFrame,
  type JSONPatchOp,
} from '../sseAdapter';
import { buildAuraRunBody } from '../auraRunBody';

const golden = goldenEvents as Record<string, AguiFrame>;

/** A STATE_DELTA frame narrowed to its delta ops (the fixture's STATE_DELTA). */
function deltaOps(f: AguiFrame): readonly JSONPatchOp[] {
  if (f.type !== 'STATE_DELTA') throw new Error('not a STATE_DELTA frame');
  return f.delta;
}

// The test is driven by the REAL captured translator output — the golden frame
// fixture the Go SSE tests use (internal/agui/testdata/golden-events.json), read
function frame(name: string): AguiFrame {
  const f = golden[name];
  if (f === undefined) throw new Error(`golden fixture missing "${name}"`);
  return f;
}

function frameWith(name: string, patch: Record<string, unknown>): AguiFrame {
  return { ...frame(name), ...patch };
}

/** An aura.artifact CUSTOM frame carrying 37A-02's enriched descriptor. Derives
 *  from the captured golden aura.artifact base (its real path/filename/caption)
 *  and layers the post-37A-02 enrichment (tool_call_id + size_bytes ALWAYS;
 *  asset_id + mime_type on ingest success) — the exact shape the live translator
 *  now emits. The golden base still carries `path`, so these frames also prove the
 *  reducer drops it. */
function artifactFrame(enrichment: Record<string, unknown>): AguiFrame {
  const base = frame('CUSTOM');
  if (base.type !== 'CUSTOM') throw new Error('golden CUSTOM is not a CUSTOM frame');
  return { ...base, value: { ...(base.value as Record<string, unknown>), ...enrichment } };
}

/** A reasoning STATE_DELTA / usage frame is not in the fixture as a usage shape;
 *  the fixture's STATE_DELTA carries only /cost_usd. Build a full usage frame
 *  from the SAME wire shape (op/path/value) the fixture demonstrates. */
function usageStateDelta(ops: readonly JSONPatchOp[]): AguiFrame {
  return { type: 'STATE_DELTA', delta: ops };
}

function fold(frames: readonly AguiFrame[]) {
  const state = newAssistantTurn('a1');
  for (const f of frames) reduceFrame(state, f);
  return state;
}

describe('sseAdapter — golden-frame reducer', () => {
  it('imports the captured golden fixture (not synthetic frames)', () => {
    // Sanity: the fixture is the real captured map, keyed by event name. Every
    // shape the reducer tests fold comes from this committed AG-UI golden file.
    expect(frame('TEXT_MESSAGE_START').type).toBe('TEXT_MESSAGE_START');
    expect(frame('STATE_DELTA').type).toBe('STATE_DELTA');
    expect(frame('TOOL_CALL_RESULT').type).toBe('TOOL_CALL_RESULT');
    expect(frame('REASONING_MESSAGE_CONTENT').type).toBe('REASONING_MESSAGE_CONTENT');
  });

  it('TEXT_MESSAGE_START/CONTENT/END → a single text part (no double-print of the final)', () => {
    const state = fold([
      frame('RUN_STARTED'),
      frame('TEXT_MESSAGE_START'),
      frame('TEXT_MESSAGE_CONTENT'), // delta "Ciao"
      frame('TEXT_MESSAGE_END'),
      frame('RUN_FINISHED(success)'),
    ]);
    const msg = toThreadMessage(state);
    const textParts = messageParts(msg).filter((p) => p.type === 'text');
    expect(textParts).toHaveLength(1);
    expect(textParts[0]).toMatchObject({ type: 'text', text: 'Ciao' });
    // The translator treats the final Event as END-only; folding the END does
    // not re-append the delta → "Ciao", never "CiaoCiao".
    expect(state.text).toBe('Ciao');
    expect(state.status).toEqual({ type: 'complete', reason: 'stop' });
  });

  it('REASONING_*  → a reasoning part bound to the pill', () => {
    const state = fold([
      frame('REASONING_START'),
      frame('REASONING_MESSAGE_START'),
      frame('REASONING_MESSAGE_CONTENT'), // delta "thinking…"
      frame('REASONING_MESSAGE_END'),
      frame('REASONING_END'),
    ]);
    expect(state.reasoning).toBe('thinking…');
    const msg = toThreadMessage(state);
    const reasoning = messageParts(msg).filter((p) => p.type === 'reasoning');
    expect(reasoning).toHaveLength(1);
    expect(reasoning[0]).toMatchObject({ type: 'reasoning', text: 'thinking…' });
  });

  it('AC-14: stamps reasoning startedAt/finishedAt from the REASONING_* frame timestamps', () => {
    const state = fold([
      frameWith('REASONING_START', { messageId: 'rsn-1', timestamp: 1000 }),
      frameWith('REASONING_MESSAGE_START', { messageId: 'rsn-1', timestamp: 1200 }),
      frameWith('REASONING_MESSAGE_CONTENT', { messageId: 'rsn-1', delta: 'thinking' }),
      frameWith('REASONING_MESSAGE_END', { messageId: 'rsn-1', timestamp: 4000 }),
      frameWith('REASONING_END', { messageId: 'rsn-1', timestamp: 4600 }),
    ]);
    const reasoning = messageParts(toThreadMessage(state)).filter((p) => p.type === 'reasoning');
    // startedAt is first-wins (the span's opening frame); finishedAt last-wins.
    expect(reasoning[0]).toMatchObject({
      type: 'reasoning',
      text: 'thinking',
      startedAt: 1000,
      finishedAt: 4600,
    });
  });

  it('AC-14: a timestamp-less reasoning frame still ensures the span part (no stamp)', () => {
    const state = fold([
      frameWith('REASONING_MESSAGE_START', { messageId: 'rsn-1', timestamp: undefined }),
      frameWith('REASONING_MESSAGE_CONTENT', { messageId: 'rsn-1', delta: 'x' }),
      frameWith('REASONING_MESSAGE_END', { messageId: 'rsn-1', timestamp: undefined }),
    ]);
    const reasoning = messageParts(toThreadMessage(state)).filter((p) => p.type === 'reasoning');
    expect(reasoning).toHaveLength(1);
    expect(reasoning[0]).toMatchObject({ type: 'reasoning', text: 'x' });
    expect((reasoning[0] as { startedAt?: number }).startedAt).toBeUndefined();
    expect((reasoning[0] as { finishedAt?: number }).finishedAt).toBeUndefined();
  });

  it('TOOL_CALL_START/ARGS/END/RESULT → one tool part with the raw result preview', () => {
    const state = fold([
      frame('TOOL_CALL_START'), // web_search, call-1
      frame('TOOL_CALL_ARGS'), // {"query":"meteo
      frame('TOOL_CALL_END'),
      frame('TOOL_CALL_RESULT'), // content "result preview + footer"
    ]);
    const msg = toThreadMessage(state);
    const tools = messageParts(msg).filter((p) => p.type === 'tool-call');
    expect(tools).toHaveLength(1);
    expect(tools[0]).toMatchObject({
      type: 'tool-call',
      toolCallId: 'call-1',
      toolName: 'web_search',
      argsText: '{"query":"meteo',
      result: 'result preview + footer',
    });
    // The raw result is NEVER folded into the assistant prose.
    expect(state.text).toBe('');
  });

  it('orders completed tool parts before assistant prose', () => {
    const state = fold([
      frame('TOOL_CALL_START'),
      frame('TOOL_CALL_ARGS'),
      frame('TOOL_CALL_END'),
      frame('TOOL_CALL_RESULT'),
      frame('TEXT_MESSAGE_START'),
      frame('TEXT_MESSAGE_CONTENT'),
      frame('TEXT_MESSAGE_END'),
    ]);

    expect(messageParts(toThreadMessage(state)).map((p) => p.type)).toEqual(['tool-call', 'text']);
  });

  it('preserves reasoning/tool/reasoning/text stream order as separate parts', () => {
    const state = fold([
      frameWith('REASONING_MESSAGE_START', { messageId: 'reason-1' }),
      frameWith('REASONING_MESSAGE_CONTENT', { messageId: 'reason-1', delta: 'First thought' }),
      frameWith('REASONING_MESSAGE_END', { messageId: 'reason-1' }),
      frame('TOOL_CALL_START'),
      frame('TOOL_CALL_RESULT'),
      frameWith('REASONING_MESSAGE_START', { messageId: 'reason-2' }),
      frameWith('REASONING_MESSAGE_CONTENT', { messageId: 'reason-2', delta: 'Second thought' }),
      frameWith('REASONING_MESSAGE_END', { messageId: 'reason-2' }),
      frame('TEXT_MESSAGE_START'),
      frame('TEXT_MESSAGE_CONTENT'),
      frame('TEXT_MESSAGE_END'),
    ]);

    const parts = messageParts(toThreadMessage(state));
    expect(parts.map((p) => p.type)).toEqual(['reasoning', 'tool-call', 'reasoning', 'text']);
    expect(parts[0]).toMatchObject({ type: 'reasoning', text: 'First thought' });
    expect(parts[2]).toMatchObject({ type: 'reasoning', text: 'Second thought' });
  });

  it('merges reasoning content when the reasoning messageId is reused', () => {
    const state = fold([
      frameWith('REASONING_MESSAGE_START', { messageId: 'reason-1' }),
      frameWith('REASONING_MESSAGE_CONTENT', { messageId: 'reason-1', delta: 'Part A' }),
      frameWith('REASONING_MESSAGE_END', { messageId: 'reason-1' }),
      frameWith('REASONING_MESSAGE_START', { messageId: 'reason-1' }),
      frameWith('REASONING_MESSAGE_CONTENT', { messageId: 'reason-1', delta: ' Part B' }),
      frameWith('REASONING_MESSAGE_END', { messageId: 'reason-1' }),
    ]);

    const reasoning = messageParts(toThreadMessage(state)).filter((p) => p.type === 'reasoning');
    expect(reasoning).toHaveLength(1);
    expect(reasoning[0]).toMatchObject({ type: 'reasoning', text: 'Part A Part B' });
  });

  it('keeps late reasoning at its arrival position after text has started', () => {
    const state = fold([
      frame('TEXT_MESSAGE_START'),
      frame('TEXT_MESSAGE_CONTENT'),
      frameWith('REASONING_MESSAGE_START', { messageId: 'reason-late' }),
      frameWith('REASONING_MESSAGE_CONTENT', {
        messageId: 'reason-late',
        delta: 'Late reasoning',
      }),
      frameWith('REASONING_MESSAGE_END', { messageId: 'reason-late' }),
    ]);

    const parts = messageParts(toThreadMessage(state));
    expect(parts.map((p) => p.type)).toEqual(['text', 'reasoning']);
    expect(parts[1]).toMatchObject({ type: 'reasoning', text: 'Late reasoning' });
  });

  it('Pitfall 2: a STATE_DELTA carrying tool_call_id → tool part, NEVER a text delta', () => {
    const marker = usageStateDelta([{ op: 'replace', path: '/tool_call_id', value: 'call-9' }]);
    expect(toolCallIdFromDelta(deltaOps(marker))).toBe('call-9');

    const state = fold([
      frame('TEXT_MESSAGE_START'),
      frame('TEXT_MESSAGE_CONTENT'), // "Ciao" assistant prose
      marker, // tool-result correlation marker
    ]);
    // The marker created a tool part …
    const msg = toThreadMessage(state);
    const tools = messageParts(msg).filter((p) => p.type === 'tool-call');
    expect(tools).toHaveLength(1);
    expect(tools[0]).toMatchObject({ type: 'tool-call', toolCallId: 'call-9' });
    // … and did NOT append to the assistant text (still just the real prose).
    expect(state.text).toBe('Ciao');
  });

  it('RUN_FINISHED(interrupt) → requires-action status (inline approval surface)', () => {
    const state = fold([frame('RUN_STARTED'), frame('RUN_FINISHED(interrupt)')]);
    expect(state.status).toEqual({ type: 'requires-action', reason: 'interrupt' });
  });

  it('RUN_ERROR (sanitized) → incomplete status + an error text part', () => {
    const state = fold([frame('RUN_STARTED'), frame('RUN_ERROR')]);
    expect(state.status).toEqual({ type: 'incomplete', reason: 'error' });
    expect(state.error).toBe('LLM 5xx finale');
    const msg = toThreadMessage(state);
    const texts = messageParts(msg).filter((p) => p.type === 'text');
    // empty assistant text part + the error text part
    expect(texts.map((p) => p.text)).toContain('LLM 5xx finale');
  });
});

describe('sseAdapter — usageFromStateDelta', () => {
  it('reads the four usage keys off the JSONPatch ops', () => {
    const usage = usageFromStateDelta([
      { op: 'replace', path: '/prompt_tokens', value: 1200 },
      { op: 'replace', path: '/completion_tokens', value: 340 },
      { op: 'replace', path: '/cache_hit_tokens', value: 1000 },
      { op: 'replace', path: '/cost_usd', value: 0.0042 },
    ]);
    expect(usage).toEqual({
      promptTokens: 1200,
      contextTokens: 1200,
      completionTokens: 340,
      cacheHitTokens: 1000,
      costUsd: 0.0042,
    });
  });

  it('uses the captured /cost_usd shape from the golden STATE_DELTA', () => {
    const goldenDelta = deltaOps(frame('STATE_DELTA'));
    const usage = usageFromStateDelta(goldenDelta);
    expect(usage.costUsd).toBe(0.0042);
    // no token keys present → all zero, never NaN
    expect(usage.promptTokens).toBe(0);
  });

  it('missing cost_usd → costUsd undefined (omit-when-absent)', () => {
    const usage = usageFromStateDelta([{ op: 'replace', path: '/prompt_tokens', value: 10 }]);
    expect(usage.costUsd).toBeUndefined();
    expect('costUsd' in usage).toBe(false);
  });

  it('cacheHitRatio guards /0 (promptTokens=0 → 0, not NaN)', () => {
    expect(
      cacheHitRatio({ promptTokens: 0, contextTokens: 0, completionTokens: 0, cacheHitTokens: 5 }),
    ).toBe(0);
    expect(
      Number.isNaN(
        cacheHitRatio({
          promptTokens: 0,
          contextTokens: 0,
          completionTokens: 0,
          cacheHitTokens: 5,
        }),
      ),
    ).toBe(false);
    expect(
      cacheHitRatio({
        promptTokens: 100,
        contextTokens: 100,
        completionTokens: 0,
        cacheHitTokens: 80,
      }),
    ).toBeCloseTo(0.8);
  });
});

describe('sseAdapter — SSE frame parsing', () => {
  it('parseSSEBlock reads a data: line and trusts the JSON type', () => {
    const block = `event: TEXT_MESSAGE_CONTENT\ndata: ${JSON.stringify(golden.TEXT_MESSAGE_CONTENT)}`;
    const ev = parseSSEBlock(block);
    expect(ev?.frame.type).toBe('TEXT_MESSAGE_CONTENT');
    // No id: line (flag-off wire) → no resume cursor.
    expect(ev?.id).toBeUndefined();
  });

  it('parseSSEBlock extracts an INTEGER id: line (1.3B resume wire)', () => {
    const block = `event: TEXT_MESSAGE_CONTENT\nid: 7\ndata: ${JSON.stringify(golden.TEXT_MESSAGE_CONTENT)}`;
    const ev = parseSSEBlock(block);
    expect(ev?.frame.type).toBe('TEXT_MESSAGE_CONTENT');
    expect(ev?.id).toBe(7);
  });

  it('parseSSEBlock treats non-integer ids as "no resume capability" (SDK flag-off shape)', () => {
    const data = `data: ${JSON.stringify(golden.RUN_STARTED)}`;
    // The SDK's <TYPE>_<timestampMs> id, a negative and a float are all rejected.
    expect(parseSSEBlock(`event: RUN_STARTED\nid: RUN_STARTED_1780766525937\n${data}`)?.id).toBe(
      undefined,
    );
    expect(parseSSEBlock(`event: RUN_STARTED\nid: -3\n${data}`)?.id).toBeUndefined();
    expect(parseSSEBlock(`event: RUN_STARTED\nid: 3.5\n${data}`)?.id).toBeUndefined();
  });

  it('parseSSEBlock ignores comments / keep-alives / non-data blocks', () => {
    expect(parseSSEBlock(': keep-alive')).toBeNull();
    expect(parseSSEBlock(':hb')).toBeNull(); // Tier A heartbeat comment
    expect(parseSSEBlock('event: PING')).toBeNull();
    expect(parseSSEBlock('data: {not json')).toBeNull();
    expect(parseSSEBlock('data: 42')).toBeNull(); // no type field
  });

  it('readSSEFrames decodes a chunked SSE byte stream into frames', async () => {
    const enc = new TextEncoder();
    const wire =
      `event: RUN_STARTED\ndata: ${JSON.stringify(golden.RUN_STARTED)}\n\n` +
      `event: TEXT_MESSAGE_CONTENT\ndata: ${JSON.stringify(golden.TEXT_MESSAGE_CONTENT)}\n\n` +
      `event: RUN_FINISHED\ndata: ${JSON.stringify(golden['RUN_FINISHED(success)'])}\n\n`;
    // Split across chunk boundaries to exercise the buffering.
    const mid = Math.floor(wire.length / 2);
    const stream = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(enc.encode(wire.slice(0, mid)));
        controller.enqueue(enc.encode(wire.slice(mid)));
        controller.close();
      },
    });
    const frames: AguiFrame[] = [];
    for await (const { frame } of readSSEFrames(stream)) frames.push(frame);
    expect(frames.map((f) => f.type)).toEqual([
      'RUN_STARTED',
      'TEXT_MESSAGE_CONTENT',
      'RUN_FINISHED',
    ]);
  });

  it('readSSEFrames yields each frame with its integer id and skips heartbeats', async () => {
    const enc = new TextEncoder();
    const wire =
      `event: RUN_STARTED\nid: 1\ndata: ${JSON.stringify(golden.RUN_STARTED)}\n\n` +
      ':hb\n\n' + // heartbeat comment: no frame, never advances the cursor
      `event: TEXT_MESSAGE_CONTENT\nid: 2\ndata: ${JSON.stringify(golden.TEXT_MESSAGE_CONTENT)}\n\n`;
    const stream = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(enc.encode(wire));
        controller.close();
      },
    });
    const events: { type: string; id?: number }[] = [];
    for await (const { frame, id } of readSSEFrames(stream)) {
      events.push({ type: frame.type, ...(id !== undefined ? { id } : {}) });
    }
    expect(events).toEqual([
      { type: 'RUN_STARTED', id: 1 },
      { type: 'TEXT_MESSAGE_CONTENT', id: 2 },
    ]);
  });

  it('readSSEFrames flushes a trailing block with no final blank-line delimiter', async () => {
    const enc = new TextEncoder();
    const wire = `event: TEXT_MESSAGE_CONTENT\ndata: ${JSON.stringify(golden.TEXT_MESSAGE_CONTENT)}`;
    const stream = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(enc.encode(wire));
        controller.close();
      },
    });
    const frames: AguiFrame[] = [];
    for await (const { frame } of readSSEFrames(stream)) frames.push(frame);
    expect(frames.map((f) => f.type)).toEqual(['TEXT_MESSAGE_CONTENT']);
  });
});

describe('sseAdapter — reducer edge branches', () => {
  it('isUsageDelta distinguishes usage ops from a bare tool_call_id marker', () => {
    expect(isUsageDelta([{ op: 'replace', path: '/completion_tokens', value: 1 }])).toBe(true);
    expect(isUsageDelta([{ op: 'replace', path: '/tool_call_id', value: 'x' }])).toBe(false);
  });

  it('a non-string tool_call_id value is not treated as a marker', () => {
    expect(
      toolCallIdFromDelta([{ op: 'replace', path: '/tool_call_id', value: 42 }]),
    ).toBeUndefined();
  });

  it('TOOL_CALL_ARGS for an unknown call id is a no-op (no phantom part)', () => {
    const state = newAssistantTurn('a1');
    reduceFrame(state, { type: 'TOOL_CALL_ARGS', toolCallId: 'ghost', delta: 'x' });
    expect(state.tools.size).toBe(0);
  });

  it('a TOOL_CALL_RESULT with no prior START still materialises a tool part', () => {
    const state = newAssistantTurn('a1');
    reduceFrame(state, frame('TOOL_CALL_RESULT')); // call-1, content preview
    const tools = messageParts(toThreadMessage(state)).filter((p) => p.type === 'tool-call');
    expect(tools).toHaveLength(1);
    expect(tools[0]).toMatchObject({ toolCallId: 'call-1', result: 'result preview + footer' });
  });

  it('an empty turn still renders a (single empty) text part', () => {
    const msg = toThreadMessage(newAssistantTurn('a1'));
    expect(msg.content).toEqual([{ type: 'text', text: '' }]);
    expect(msg.role).toBe('assistant');
  });
});

describe('sseAdapter — CUSTOM/aura.display frame (DISP-02)', () => {
  it('attaches the typed payload to the matching tool part by toolCallId', () => {
    // The display frame's value.tool_call_id is "call-1", the same id the
    // TOOL_CALL_START/RESULT golden frames carry — the payload upgrades that part.
    const state = fold([
      frame('TOOL_CALL_START'), // web_search, call-1
      frame('TOOL_CALL_ARGS'),
      frame('TOOL_CALL_END'),
      frame('TOOL_CALL_RESULT'), // call-1, raw preview
      frame('CUSTOM_DISPLAY'), // aura.display for call-1
    ]);
    const part = state.tools.get('call-1');
    if (part === undefined) throw new Error('expected the call-1 tool part');
    // The raw result is preserved AND the typed display is attached.
    expect(part.result).toBe('result preview + footer');
    expect(part.display).toBeDefined();
    expect(part.display?.type).toBe('web_result');
    expect(part.display?.tool_call_id).toBe('call-1');
    expect(part.display?.web_results?.[0]).toMatchObject({ title: 'Forecast' });
    // Exactly one tool part — the display merged onto it, never a phantom part.
    expect(state.tools.size).toBe(1);
  });

  it('creates the tool part on demand when the display arrives before any START', () => {
    // Tolerant like TOOL_CALL_RESULT: a display can land for a call we never saw a
    // START for (snapshot/ordering), and the payload is not lost.
    const state = fold([frame('CUSTOM_DISPLAY')]);
    const part = state.tools.get('call-1');
    if (part === undefined) throw new Error('expected an ensured call-1 tool part');
    expect(part.display?.type).toBe('web_result');
    expect(state.toolOrder).toEqual(['call-1']);
  });

  // 37A (WEBART-04): aura.artifact is now CONSUMED, not dropped. These three cases
  // REPLACE the pre-37A "aura.artifact is a no-op" test, which encoded the OLD drop
  // contract — the legitimate CLAUDE.md exception (the test asserted the exact
  // behavior we intentionally change). The reducer synthesizes a local_artifact
  // display attached by tool_call_id and NEVER copies the descriptor's raw
  // host/container path into the payload (either branch, D-13).
  it('aura.artifact (asset_id present) attaches a local_artifact card by tool_call_id, no path', () => {
    const state = fold([
      frame('TEXT_MESSAGE_START'),
      frame('TEXT_MESSAGE_CONTENT'), // "Ciao"
      artifactFrame({
        tool_call_id: 'call-1',
        size_bytes: 4096,
        asset_id: 'asset-xyz',
        mime_type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
      }),
    ]);
    const part = state.tools.get('call-1');
    if (part === undefined) throw new Error('expected the call-1 tool part');
    expect(part.display?.type).toBe('local_artifact');
    expect(part.display?.tool_call_id).toBe('call-1');
    const artifact = part.display?.artifact;
    expect(artifact?.filename).toBe('results.xlsx'); // from the captured golden base
    expect(artifact?.size_bytes).toBe(4096);
    expect(artifact?.asset_id).toBe('asset-xyz');
    expect(artifact?.mime_type).toBe(
      'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
    );
    // The raw descriptor path is NEVER copied into the synthesized payload.
    expect(artifact).not.toHaveProperty('path');
    // Prose untouched; exactly one tool part (the card merged onto it).
    expect(state.text).toBe('Ciao');
    expect(state.tools.size).toBe(1);
  });

  it('aura.artifact degrade (no asset_id, descriptor has path) → render-only card, no path, no asset_id', () => {
    // The golden aura.artifact base rides a real path (/abs/results.xlsx). On the
    // D-02/D-05 degrade the descriptor still carries path + tool_call_id +
    // size_bytes but NO asset_id; the reducer attaches the card, drops path, and
    // omits asset_id — so the "delivery unavailable" render-only card is reachable.
    const degraded = artifactFrame({ tool_call_id: 'call-1', size_bytes: 4096 });
    expect((degraded as { value: { path?: string } }).value.path).toBe('/abs/results.xlsx');
    const part = fold([degraded]).tools.get('call-1');
    if (part === undefined) throw new Error('expected the call-1 tool part');
    expect(part.display?.type).toBe('local_artifact');
    const artifact = part.display?.artifact;
    expect(artifact?.filename).toBe('results.xlsx');
    expect(artifact?.size_bytes).toBe(4096);
    expect(artifact).not.toHaveProperty('path');
    expect(artifact).not.toHaveProperty('asset_id');
  });

  it('aura.artifact without a tool_call_id (pre-enrichment / malformed) is ignored — no card', () => {
    // The isArtifactDescriptor guard requires the tool_call_id correlation key; a
    // descriptor without it (the captured pre-37A-02 golden shape) attaches
    // nothing — additive safety, never corruption.
    const state = fold([
      frame('TEXT_MESSAGE_START'),
      frame('TEXT_MESSAGE_CONTENT'), // "Ciao"
      frame('CUSTOM'), // golden base: {path, filename, caption} — no tool_call_id
    ]);
    expect(state.tools.size).toBe(0);
    expect(state.text).toBe('Ciao');
  });

  it('an aura.display frame with a non-DisplayPayload value is a no-op', () => {
    const state = newAssistantTurn('a1');
    reduceFrame(state, { type: 'CUSTOM', name: 'aura.display', value: { no: 'tool_call_id' } });
    expect(state.tools.size).toBe(0);
  });
});

// buildAuraRunBody is the single body assembler streamRun delegates to (body:
// JSON.stringify(buildAuraRunBody(id, opts))), so these three cases exercise every branch of
// the aura fold that a real /agent/run POST carries — attachments+skill, skill-only, neither.
describe('buildAuraRunBody — the aura run envelope fold (WEBSKILL-02)', () => {
  const base = {
    threadId: 'thread-1',
    userText: 'do the thing',
    signal: new AbortController().signal,
    onUpdate: () => undefined,
  };

  it('folds attachment ids AND a pinned skill into ONE aura object', () => {
    const body = buildAuraRunBody('m1', { ...base, attachmentIds: ['a1'], skill: 'skill-creator' });
    expect(body.aura).toEqual({ attachment_ids: ['a1'], skill: 'skill-creator' });
    // Exactly one aura key — never two.
    expect(Object.keys(body).filter((key) => key === 'aura')).toHaveLength(1);
  });

  it('carries the skill alone when there are no attachments', () => {
    const body = buildAuraRunBody('m1', { ...base, skill: 'skill-creator' });
    expect(body.aura).toEqual({ skill: 'skill-creator' });
  });

  it('folds Garage mentions into structured scope without rewriting visible user text', () => {
    const body = buildAuraRunBody('m1', {
      ...base,
      userText: 'Review @folder:"/finance/2026"',
      documentScope: [{ kind: 'folder', path: 'finance/2026' }],
    });
    expect(body).toEqual({
      threadId: 'thread-1',
      messages: [{ id: 'm1', role: 'user', content: 'Review @folder:"/finance/2026"' }],
      aura: { document_scope: [{ kind: 'folder', path: 'finance/2026' }] },
    });
  });

  it('emits NO aura key when neither a skill nor attachments are set (byte-identical to today)', () => {
    const body = buildAuraRunBody('m1', base);
    expect(body).not.toHaveProperty('aura');
    expect(body).toEqual({
      threadId: 'thread-1',
      messages: [{ id: 'm1', role: 'user', content: 'do the thing' }],
    });
  });
});
