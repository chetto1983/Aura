import { describe, expect, it } from 'vitest';
import { statusLabel } from '../McpBoard';
import { scheduleText } from '../SchedulerBoard';
import { boardStatus, isAuthError } from '../governanceView';
import type { SchedulerTask } from '../governanceApi';

// Pure-helper unit tests — they exercise every branch + the returned tone/text/status values
// directly (mutation hardening: the branch ORDER, the equality operators, the tone strings, and
// the status mapping are all asserted without going through the DOM).

// A minimal t() that echoes the key + interpolation so the exact branch is observable. statusLabel
// only ever calls t(key) / t(key, {count|state}); the branded i18next TFunction signature is cast
// here since this stub fulfils the only shape statusLabel uses.
type StatusLabelT = Parameters<typeof statusLabel>[0];
const t = ((key: string, opts?: Record<string, unknown>): string => {
  if (opts === undefined) return key;
  const args = Object.entries(opts)
    .map(([k, v]) => `${k}=${String(v)}`)
    .join(',');
  return `${key}{${args}}`;
}) as unknown as StatusLabelT;

describe('statusLabel (MCP probe → row status)', () => {
  it('loading → Checking… (warning), taking priority over every other input', () => {
    const r = statusLabel(t, { ok: true, tool_count: 9, detail: 'x' }, true, false);
    expect(r.text).toBe('governance.mcp.status.checking');
    expect(r.tone).toBe('text-warning');
  });

  it('error → Timed out (warning)', () => {
    const r = statusLabel(t, undefined, false, true);
    expect(r.text).toBe('governance.mcp.status.timedOut');
    expect(r.tone).toBe('text-warning');
  });

  it('no probe yet (not loading, not error) → Checking… (warning)', () => {
    const r = statusLabel(t, undefined, false, false);
    expect(r.text).toBe('governance.mcp.status.checking');
    expect(r.tone).toBe('text-warning');
  });

  it('ok probe → Healthy · N tools (success) with the live count', () => {
    const r = statusLabel(t, { ok: true, tool_count: 4, detail: 'ok' }, false, false);
    expect(r.text).toBe('governance.mcp.status.healthy{count=4}');
    expect(r.tone).toBe('text-success');
  });

  it('failed probe → Error — {state} (danger) with the detail as the state', () => {
    const r = statusLabel(t, { ok: false, tool_count: 0, detail: 'dial failed' }, false, false);
    expect(r.text).toBe('governance.mcp.status.error{state=dial failed}');
    expect(r.tone).toBe('text-danger');
  });
});

describe('scheduleText (scheduler row schedule cell)', () => {
  function task(over: Partial<SchedulerTask>): SchedulerTask {
    return {
      ID: 't',
      Kind: 'reminder',
      ScheduleKind: 'cron',
      CronExpr: '',
      EveryMinutes: 0,
      RunAt: '0001-01-01T00:00:00Z',
      TZ: 'UTC',
      StepBudget: 0,
      Status: 'active',
      NextRunAt: '',
      NotifyRoute: '',
      IdentityID: 'local',
      OriginConversationID: '',
      CreatedAt: '',
      UpdatedAt: '',
      ...over,
    };
  }

  it('prefers the cron expression', () => {
    expect(scheduleText(task({ CronExpr: '0 9 * * *' }), '—')).toBe('0 9 * * *');
  });

  it('falls back to every-N-minutes when there is no cron', () => {
    expect(scheduleText(task({ EveryMinutes: 15 }), '—')).toBe('every 15m');
  });

  it('falls back to a one-shot RunAt when neither cron nor interval is set', () => {
    expect(scheduleText(task({ RunAt: '2026-07-01T00:00:00Z' }), '—')).toBe('2026-07-01T00:00:00Z');
  });

  it('returns the dash for the zero-value RunAt (no schedule at all)', () => {
    expect(scheduleText(task({}), '—')).toBe('—');
  });
});

describe('boardStatus + isAuthError', () => {
  it('isAuthError matches only the HTTP 401 sentinel', () => {
    expect(isAuthError(new Error('HTTP 401'))).toBe(true);
    expect(isAuthError(new Error('HTTP 500'))).toBe(false);
    expect(isAuthError('HTTP 401')).toBe(false);
    expect(isAuthError(undefined)).toBe(false);
  });

  it('maps an auth error to error-auth and any other error to error', () => {
    expect(
      boardStatus({ isLoading: false, isError: true, error: new Error('HTTP 401'), isEmpty: false }),
    ).toBe('error-auth');
    expect(
      boardStatus({ isLoading: false, isError: true, error: new Error('HTTP 502'), isEmpty: true }),
    ).toBe('error');
  });

  it('error takes priority over loading', () => {
    expect(
      boardStatus({ isLoading: true, isError: true, error: new Error('HTTP 503'), isEmpty: false }),
    ).toBe('error');
  });

  it('loading before data, then empty vs populated', () => {
    expect(boardStatus({ isLoading: true, isError: false, error: null, isEmpty: true })).toBe(
      'loading',
    );
    expect(boardStatus({ isLoading: false, isError: false, error: null, isEmpty: true })).toBe(
      'empty',
    );
    expect(boardStatus({ isLoading: false, isError: false, error: null, isEmpty: false })).toBe(
      'populated',
    );
  });
});
