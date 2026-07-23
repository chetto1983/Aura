import { describe, expect, it } from 'vitest';
import type { AuditEvent } from '../../admin/adminApi';
import { pairAuditEvents } from '../auditPairing';

// auditPairing — exact-first start/end merging of the admin tool feed: the
// server projects `correlation` (tool_call_id) + persisted `duration_ms`
// (server gap CLOSED 2026-07-23); rows without a correlation fall back to the
// conservative per-name stack. Feed order is newest-first, exactly as
// GET /api/admin/audit returns it.

function ev(over: Partial<AuditEvent>): AuditEvent {
  return {
    source: 'tool',
    action: 'end',
    target: 'shell_exec',
    detail: 'ok',
    created_at: '2026-07-23T13:45:01.000Z',
    ...over,
  };
}

describe('pairAuditEvents', () => {
  it('merges an ok start/end pair into ONE invocation row at the end position', () => {
    const rows = pairAuditEvents([
      ev({ action: 'end', detail: 'ok', created_at: '2026-07-23T13:45:01.280Z' }),
      ev({ action: 'start', detail: '', created_at: '2026-07-23T13:45:01.000Z' }),
    ]);
    expect(rows).toEqual([
      {
        kind: 'invocation',
        toolName: 'shell_exec',
        outcome: 'ok',
        at: '2026-07-23T13:45:01.280Z',
        durationMs: 280,
      },
    ]);
  });

  it('marks a non-ok end as an error pair and carries the status detail', () => {
    const rows = pairAuditEvents([
      ev({ action: 'end', detail: 'error: exit 1', created_at: '2026-07-23T13:45:02.000Z' }),
      ev({ action: 'start', detail: '', created_at: '2026-07-23T13:45:01.000Z' }),
    ]);
    expect(rows).toEqual([
      {
        kind: 'invocation',
        toolName: 'shell_exec',
        outcome: 'error',
        at: '2026-07-23T13:45:02.000Z',
        durationMs: 1000,
        detail: 'error: exit 1',
      },
    ]);
  });

  it('renders an orphan start (still running / crashed) as its own running row', () => {
    const rows = pairAuditEvents([ev({ action: 'start', detail: '' })]);
    expect(rows).toEqual([
      {
        kind: 'invocation',
        toolName: 'shell_exec',
        outcome: 'running',
        at: '2026-07-23T13:45:01.000Z',
      },
    ]);
  });

  it('leaves an orphan end as a plain event row (conservative: never invent a pair)', () => {
    const orphanEnd = ev({ action: 'end', target: 'lonely_tool' });
    const rows = pairAuditEvents([orphanEnd]);
    expect(rows).toEqual([{ kind: 'event', event: orphanEnd }]);
  });

  it('pairs interleaved DIFFERENT tools by name (the live feed shape)', () => {
    // Real captured shape: start(todo), start(shell) ... end(todo), end(shell).
    const rows = pairAuditEvents([
      ev({ action: 'end', target: 'shell_exec', created_at: '2026-07-23T13:44:43.796Z' }),
      ev({ action: 'end', target: 'todo_write', created_at: '2026-07-23T13:44:43.794Z' }),
      ev({
        action: 'start',
        target: 'todo_write',
        detail: '',
        created_at: '2026-07-23T13:44:43.777Z',
      }),
      ev({
        action: 'start',
        target: 'shell_exec',
        detail: '',
        created_at: '2026-07-23T13:44:43.777Z',
      }),
    ]);
    expect(rows).toHaveLength(2);
    expect(rows[0]).toMatchObject({ kind: 'invocation', toolName: 'shell_exec', outcome: 'ok' });
    expect(rows[1]).toMatchObject({ kind: 'invocation', toolName: 'todo_write', outcome: 'ok' });
  });

  it('same-name repeats pair each end with its nearest older start', () => {
    const rows = pairAuditEvents([
      ev({ action: 'end', created_at: '2026-07-23T13:00:04.000Z' }),
      ev({ action: 'start', detail: '', created_at: '2026-07-23T13:00:03.000Z' }),
      ev({ action: 'end', created_at: '2026-07-23T13:00:02.000Z' }),
      ev({ action: 'start', detail: '', created_at: '2026-07-23T13:00:01.000Z' }),
    ]);
    expect(rows).toEqual([
      expect.objectContaining({
        kind: 'invocation',
        durationMs: 1000,
        at: '2026-07-23T13:00:04.000Z',
      }),
      expect.objectContaining({
        kind: 'invocation',
        durationMs: 1000,
        at: '2026-07-23T13:00:02.000Z',
      }),
    ]);
  });

  it('passes non-tool events through untouched, in place', () => {
    const mcp = ev({ source: 'mcp', action: 'install', target: 'aura-memory' });
    const skill = ev({ source: 'skill', action: 'create', target: 'my-skill' });
    const rows = pairAuditEvents([
      mcp,
      ev({ action: 'end', created_at: '2026-07-23T13:00:02.000Z' }),
      ev({ action: 'start', detail: '', created_at: '2026-07-23T13:00:01.000Z' }),
      skill,
    ]);
    expect(rows).toEqual([
      { kind: 'event', event: mcp },
      expect.objectContaining({ kind: 'invocation', outcome: 'ok' }),
      { kind: 'event', event: skill },
    ]);
  });

  it('omits the duration when a timestamp is unparseable or negative', () => {
    const rows = pairAuditEvents([
      ev({ action: 'end', created_at: 'not-a-date' }),
      ev({ action: 'start', detail: '', created_at: '2026-07-23T13:00:01.000Z' }),
    ]);
    expect(rows[0]).toMatchObject({ kind: 'invocation', outcome: 'ok', at: 'not-a-date' });
    expect((rows[0] as { durationMs?: number }).durationMs).toBeUndefined();
  });

  it('correlation disambiguates same-name invocations the name stack would mispair', () => {
    // Two shell_exec invocations whose ends arrive OUT of LIFO order: A starts,
    // B starts, then A ends (error), then B ends (ok). The name stack alone
    // would give A's end to B's start; the correlation pairs each exactly.
    const rows = pairAuditEvents([
      ev({
        action: 'end',
        detail: 'ok',
        correlation: 'tc-B',
        created_at: '2026-07-23T13:00:04.000Z',
      }),
      ev({
        action: 'end',
        detail: 'timeout',
        correlation: 'tc-A',
        created_at: '2026-07-23T13:00:03.000Z',
      }),
      ev({
        action: 'start',
        detail: '',
        correlation: 'tc-B',
        created_at: '2026-07-23T13:00:02.000Z',
      }),
      ev({
        action: 'start',
        detail: '',
        correlation: 'tc-A',
        created_at: '2026-07-23T13:00:00.000Z',
      }),
    ]);
    expect(rows).toHaveLength(2);
    expect(rows[0]).toMatchObject({ kind: 'invocation', outcome: 'ok' });
    expect((rows[0] as { durationMs?: number }).durationMs).toBe(2000);
    expect(rows[1]).toMatchObject({ kind: 'invocation', outcome: 'error', detail: 'timeout' });
    expect((rows[1] as { durationMs?: number }).durationMs).toBe(3000);
  });

  it('prefers the server-persisted duration_ms over timestamp arithmetic', () => {
    const rows = pairAuditEvents([
      ev({
        action: 'end',
        correlation: 'tc-1',
        duration_ms: 1234,
        created_at: '2026-07-23T13:00:10.000Z',
      }),
      ev({
        action: 'start',
        detail: '',
        correlation: 'tc-1',
        created_at: '2026-07-23T13:00:00.000Z',
      }),
    ]);
    expect(rows).toHaveLength(1);
    expect((rows[0] as { durationMs?: number }).durationMs).toBe(1234);
  });

  it('a correlated end never claims a foreign name-stack start', () => {
    // The end carries a correlation with NO matching start (its start rotated
    // out of the page): it must fall back to the name stack, and when that is
    // empty too it stays a plain event row.
    const rows = pairAuditEvents([ev({ action: 'end', correlation: 'tc-gone' })]);
    expect(rows[0]).toMatchObject({ kind: 'event' });
  });
});
