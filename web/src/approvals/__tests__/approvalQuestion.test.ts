import { afterEach, describe, expect, it } from 'vitest';
import i18n from '../../i18n/i18n';
import { approvalQuestion } from '../approvalQuestion';
import type { Approval } from '../useApprovals';

function approval(overrides: Partial<Approval> = {}): Approval {
  return {
    token: 'token',
    conversation_id: 'conversation',
    kind: 'approval',
    question: 'Canonical fallback',
    priority: 0,
    ...overrides,
  };
}

afterEach(async () => {
  await i18n.changeLanguage('en');
});

describe('approvalQuestion', () => {
  it('renders the same canonical-bound gateway metadata in the active locale', async () => {
    const row = approval({
      presentation: {
        key: 'approval.gateway.mutation',
        params: { tool: 'files.delete', risk: 'high', args: '(none)' },
      },
    });

    await i18n.changeLanguage('en');
    expect(approvalQuestion(row, i18n.t)).toContain('Approve files.delete (risk=high)?');
    expect(approvalQuestion(row, i18n.t)).toContain('args: (none)');

    await i18n.changeLanguage('it');
    expect(approvalQuestion(row, i18n.t)).toContain('Approva files.delete (rischio=high)?');
    expect(approvalQuestion(row, i18n.t)).toContain('argomenti: (nessuno)');
  });

  it('renders shell and scheduled approvals without changing the canonical payload', async () => {
    await i18n.changeLanguage('it');
    const shell = approval({
      question: 'Canonical shell consent',
      presentation: {
        key: 'approval.shell.command',
        params: { cwd: '/srv/aura', command: 'rm example', digest: 'deadbeef' },
      },
    });
    expect(approvalQuestion(shell, i18n.t)).toBe(
      'Approva il comando shell_exec?\ndirectory: /srv/aura\ncomando:\nrm example\nsha256: deadbeef',
    );
    expect(shell.question).toBe('Canonical shell consent');

    const scheduled = approval({
      presentation: {
        key: 'approval.scheduled.task',
        params: { task: '019f9349', kind: 'agent_job', schedule: 'ogni giorno', risk: 'medium' },
      },
    });
    expect(approvalQuestion(scheduled, i18n.t)).toContain(
      "Approva l'attività pianificata agent_job 019f9349 (ogni giorno, rischio=medium)?",
    );
  });

  it('falls back verbatim for unknown or incomplete metadata', () => {
    for (const presentation of [
      { key: 'approval.forged', params: {} },
      { key: 'approval.shell.command', params: { cwd: '/' } },
    ]) {
      const row = approval({ presentation });
      expect(approvalQuestion(row, i18n.t)).toBe(row.question);
    }
  });
});
