import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useCopyAction } from './useCopyAction';
import type { DisplayDiff } from './types';
import { CodeDiff } from '@/components/code-diff';
import { Button } from '@/components/ui/button';

export function DiffDisplay({
  diff,
  rawResult,
}: {
  readonly diff: DisplayDiff;
  readonly rawResult?: string;
}) {
  const { t } = useTranslation();
  const { copied, copy } = useCopyAction();
  const [showRaw, setShowRaw] = useState(false);
  const copyText =
    rawResult ??
    diff.lines
      .map(
        (line) =>
          `${line.kind === 'added' ? '+' : line.kind === 'removed' ? '-' : ' '}${line.text}`,
      )
      .join('\n');
  return (
    <div className="min-w-0 space-y-2">
      <CodeDiff
        filename={diff.filename}
        additions={diff.additions}
        deletions={diff.deletions}
        lines={diff.lines}
        cycle={0}
      />
      <div className="flex flex-wrap gap-2">
        <Button
          type="button"
          variant="outline"
          onClick={() => {
            copy(copyText);
          }}
          aria-label={t('display.diff.copy')}
        >
          {copied ? t('display.diff.copied') : t('display.diff.copy')}
        </Button>
        {rawResult !== undefined ? (
          <Button
            type="button"
            variant="outline"
            onClick={() => {
              setShowRaw((value) => !value);
            }}
            aria-expanded={showRaw}
          >
            {showRaw ? t('display.diff.hideRaw') : t('display.diff.showRaw')}
          </Button>
        ) : null}
      </div>
      {showRaw && rawResult !== undefined ? (
        <pre className="max-w-full overflow-x-auto whitespace-pre rounded-[var(--radius-md)] border border-border bg-surface p-3 text-xs text-text-muted">
          {rawResult}
        </pre>
      ) : null}
    </div>
  );
}
