import { describe, expect, it } from 'vitest';
import type { DisplayPayload } from '../displays/types';
import { resultMeta, summarizeArgs } from '../toolSummary';

// toolSummary (compact-chat spec §3.2/§3.4) — pure projections, table-driven per
// tool family, incl. partial mid-stream JSON, the caps, and missing keys.

describe('summarizeArgs — per-family table', () => {
  it.each([
    ['web_search', '{"query":"meteo domani Milano"}', 'meteo domani Milano'],
    ['document_search', '{"query":"prd"}', 'prd'],
    ['custom_vector_search', '{"query":"embedding"}', 'embedding'],
    ['fs_read', '{"path":"~/projects/aura/prd.md"}', '~/projects/aura/prd.md'],
    ['fs_write', '{"path":"/tmp/out.txt","content":"x"}', '/tmp/out.txt'],
    ['shell_exec', '{"command":"git log --oneline -5"}', 'git log --oneline -5'],
    ['sandbox_exec', '{"command":"ls -la\\ncat foo"}', 'ls -la'],
    ['memory_recall', '{"query":"docker cache"}', 'recall "docker cache"'],
    ['memory_store', '{"key":"pref"}', 'store "pref"'],
    ['memory_forget', '{}', 'forget'],
    ['send_file', '{"filename":"results.xlsx"}', 'results.xlsx'],
    ['send_file', '{"path":"/workspace/out.docx"}', '/workspace/out.docx'],
    ['calendar.list_events', '{"date":"2026-07-24"}', '2026-07-24'],
    ['generic_tool', '{"first":"value one","second":"two"}', 'value one'],
  ])('%s %s → %s', (tool, args, expected) => {
    expect(summarizeArgs(tool, args)).toBe(expected);
  });

  it('parses partial mid-stream JSON with best-effort closers', () => {
    expect(summarizeArgs('web_search', '{"query":"meteo')).toBe('meteo');
    expect(summarizeArgs('web_search', '{"query":"meteo"')).toBe('meteo');
  });

  it('returns the empty string while nothing is parseable yet', () => {
    expect(summarizeArgs('web_search', '')).toBe('');
    expect(summarizeArgs('web_search', '{"que')).toBe('');
    expect(summarizeArgs('web_search', '[1,2]')).toBe('');
  });

  it('returns the empty string when the family key is missing', () => {
    expect(summarizeArgs('web_search', '{"other":"x"}')).toBe('');
    expect(summarizeArgs('fs_read', '{"query":"not a path"}')).toBe('');
    expect(summarizeArgs('shell_exec', '{}')).toBe('');
    expect(summarizeArgs('generic_tool', '{"n":42}')).toBe('');
  });

  it('caps a pathological value at 120 chars and a command at 64', () => {
    const long = 'x'.repeat(400);
    expect(summarizeArgs('web_search', JSON.stringify({ query: long }))).toHaveLength(121); // 120 + ellipsis
    const command = summarizeArgs('shell_exec', JSON.stringify({ command: long }));
    expect(command).toHaveLength(65);
    expect(command.endsWith('…')).toBe(true);
  });
});

function payload(over: Partial<DisplayPayload> & Pick<DisplayPayload, 'type'>): DisplayPayload {
  return { tool_call_id: 'call-1', ...over };
}

describe('resultMeta — collapsed hint per payload type', () => {
  it('counts web results, table rows, chart points, swarm workers', () => {
    expect(
      resultMeta(
        payload({ type: 'web_result', web_results: [{ title: 't', url: 'u' }] }),
        undefined,
      ),
    ).toEqual({ key: 'chat.tool.meta.results', count: 1 });
    expect(
      resultMeta(payload({ type: 'table', table: { columns: ['a'], rows: [['1'], ['2']] } }), ''),
    ).toEqual({ key: 'display.table.rowCount', count: 2 });
    expect(
      resultMeta(payload({ type: 'chart', chart: { x_labels: ['a'], y_values: [1, 2, 3] } }), ''),
    ).toEqual({ key: 'chat.tool.meta.points', count: 3 });
    expect(
      resultMeta(
        payload({
          type: 'swarm_report',
          swarm: [{ goal_index: 0, child_id: 'c', status: 'ok' }],
        }),
        '',
      ),
    ).toEqual({ key: 'chat.tool.meta.workers', count: 1 });
  });

  it('labels code with the resolved lang prefix + line count', () => {
    expect(
      resultMeta(payload({ type: 'code', code: { body: 'a\nb\nc', lang: 'py' } }), undefined),
    ).toEqual({ key: 'chat.tool.meta.lines', count: 3, prefix: 'python' });
    // Unknown lang → no prefix; empty body → zero lines.
    expect(
      resultMeta(payload({ type: 'code', code: { body: '', lang: 'zz' } }), undefined),
    ).toEqual({ key: 'chat.tool.meta.lines', count: 0 });
  });

  it('uses the document title, degrading to the untitled key', () => {
    expect(
      resultMeta(
        payload({ type: 'document', document: { title: 'The PRD', content_md: '# x' } }),
        undefined,
      ),
    ).toEqual({ text: 'The PRD' });
    expect(
      resultMeta(payload({ type: 'document', document: { content_md: '# x' } }), undefined),
    ).toEqual({ key: 'display.document.untitled' });
  });

  it('falls back to the honest char count for raw results, null when nothing settled', () => {
    expect(resultMeta(undefined, 'four')).toEqual({ key: 'chat.tool.meta.chars', count: 4 });
    expect(resultMeta(undefined, '')).toBeNull();
    expect(resultMeta(undefined, undefined)).toBeNull();
    // Inline-exception types carry no meta contract; the raw fallback applies.
    expect(resultMeta(payload({ type: 'system_event' }), 'xy')).toEqual({
      key: 'chat.tool.meta.chars',
      count: 2,
    });
  });

  it('tolerates a payload missing its per-type slot (zero counts)', () => {
    expect(resultMeta(payload({ type: 'web_result' }), undefined)).toEqual({
      key: 'chat.tool.meta.results',
      count: 0,
    });
  });
});
