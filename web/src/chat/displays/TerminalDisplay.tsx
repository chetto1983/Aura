import { useTranslation } from 'react-i18next';
import { TerminalBlock } from '@/components/terminal-block';
import type { DisplayPayload } from './types';

export function TerminalDisplay({ payload }: { readonly payload: DisplayPayload }) {
  const { t } = useTranslation();
  const terminal = payload.terminal!;
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
