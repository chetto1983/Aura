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

  it('adds the target a tool described as its own line, and only then', async () => {
    const params = { tool: 'board', risk: 'destructive', args: 'action, id' };
    const described = approval({
      presentation: {
        key: 'approval.gateway.mutation',
        params: { ...params, target: 'Fattura fornitore · Fatto' },
      },
    });

    await i18n.changeLanguage('en');
    expect(approvalQuestion(described, i18n.t)).toBe(
      'Approve board (risk=destructive)?\nThis mutating action is WITHHELD until you accept.\nargs: action, id\ntarget: Fattura fornitore · Fatto',
    );
    const undescribed = approval({ presentation: { key: 'approval.gateway.mutation', params } });
    expect(approvalQuestion(undescribed, i18n.t)).not.toContain('target');

    await i18n.changeLanguage('it');
    expect(approvalQuestion(described, i18n.t)).toBe(
      'Approva board (rischio=destructive)?\nQuesta azione mutante resta BLOCCATA finché non accetti.\nargomenti: action, id\noggetto: Fattura fornitore · Fatto',
    );
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

  it('renders a plugin install and a removal in either language', async () => {
    const install = approval({
      question: 'Canonical plugin consent',
      presentation: {
        key: 'approval.plugin.install',
        params: {
          plugin: '"broker-analysis" "0.1.1"',
          source: 'o/r/p@0123456',
          skills: '(23): "a", "b", and 21 more',
          connectors: '(2): "x" (box): "uvx" "srv"; "y" (remote): "https://r.example/mcp"',
          skipped: '(0): none',
          mcp: 'disabled — "mcp.json resolves outside the plugin"',
          notes: '(1): "hooks are not run"',
          digest: 'deadbeef',
        },
      },
    });
    const removal = approval({
      presentation: {
        key: 'approval.plugin.remove',
        params: {
          plugin: '"broker-analysis" "0.1.1"',
          source: 'o/r/p@0123456',
          skills: '(0): none',
          connectors: '(1): "x"',
          data: 'deleted in your box',
          digest: 'deadbeef',
        },
      },
    });

    await i18n.changeLanguage('en');
    expect(approvalQuestion(install, i18n.t)).toBe(
      'Approve plugin install?\nplugin: "broker-analysis" "0.1.1"\nsource: o/r/p@0123456\nskills (23): "a", "b", and 21 more\nconnectors (2): "x" (box): "uvx" "srv"; "y" (remote): "https://r.example/mcp"\nskipped (0): none\nmcp: disabled — "mcp.json resolves outside the plugin"\nnotes (1): "hooks are not run"\nsha256: deadbeef',
    );
    expect(approvalQuestion(removal, i18n.t)).toBe(
      'Approve plugin removal?\nplugin: "broker-analysis" "0.1.1"\nsource: o/r/p@0123456\nskills (0): none\nconnectors (1): "x"\nplugin data: deleted in your box\nsha256: deadbeef',
    );

    await i18n.changeLanguage('it');
    expect(approvalQuestion(install, i18n.t)).toBe(
      'Approva l\'installazione del plugin?\nplugin: "broker-analysis" "0.1.1"\norigine: o/r/p@0123456\ncompetenze (23): "a", "b", and 21 more\nconnettori (2): "x" (box): "uvx" "srv"; "y" (remote): "https://r.example/mcp"\nsaltati (0): nessuno\nmcp: disattivato — "mcp.json resolves outside the plugin"\nnote (1): "hooks are not run"\nsha256: deadbeef',
    );
    expect(approvalQuestion(removal, i18n.t)).toBe(
      'Approva la rimozione del plugin?\nplugin: "broker-analysis" "0.1.1"\norigine: o/r/p@0123456\ncompetenze (0): nessuno\nconnettori (1): "x"\ndati del plugin: eliminati nel tuo box\nsha256: deadbeef',
    );
    const kept = approval({
      presentation: {
        key: 'approval.plugin.remove',
        params: { ...removal.presentation?.params, data: 'kept' },
      },
    });
    expect(approvalQuestion(kept, i18n.t)).toContain('\ndati del plugin: conservati\n');
  });

  it('falls back verbatim for unknown or incomplete metadata', () => {
    for (const presentation of [
      { key: 'approval.forged', params: {} },
      { key: 'approval.shell.command', params: { cwd: '/' } },
      { key: 'approval.plugin.install', params: { plugin: '"p" ""' } },
      { key: 'approval.plugin.remove', params: { plugin: '"p" ""', digest: 'd' } },
    ]) {
      const row = approval({ presentation });
      expect(approvalQuestion(row, i18n.t)).toBe(row.question);
    }
  });
});
