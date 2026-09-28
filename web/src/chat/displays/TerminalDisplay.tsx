import { useTranslation } from 'react-i18next';
import type { DisplayTerminal } from './types';
import { TerminalBlock } from '@/components/terminal-block';

export function TerminalDisplay({ terminal }: { readonly terminal: DisplayTerminal }) {
  const { t } = useTranslation();
  const lines = terminal.output ? terminal.output.split('\n') : [];
  return (
    <TerminalBlock
      command={terminal.command}
      lines={lines}
      visibleCount={lines.length}
      done
      exitCode={terminal.exit_code}
      {...(terminal.cwd ? { cwd: terminal.cwd } : {})}
      {...(terminal.duration_ms !== undefined ? { durationMs: terminal.duration_ms } : {})}
      truncated={terminal.truncated === true}
      maxCollapsedLines={12}
      showAllLabel={t('display.terminal.showAll')}
      showLessLabel={t('display.terminal.showLess')}
      truncatedLabel={t('display.terminal.truncated')}
    />
  );
}
