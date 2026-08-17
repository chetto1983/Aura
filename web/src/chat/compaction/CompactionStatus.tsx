import { useTranslation } from 'react-i18next';
import type { CompactionFailure } from './api';

// CompactionStatus — what `/compact` says while it works, and when it did not happen.
//
// The composer clears the command as it runs, so silence is indistinguishable from success:
// a compaction that refused would look exactly like one that worked, and the operator would
// keep talking to a conversation they believe is condensed. The summarizer call also takes
// seconds (45s timeout), which is long enough that "nothing visible is happening" is its own
// wrong answer.

export interface CompactionStatusProps {
  readonly running: boolean;
  readonly failure: CompactionFailure | undefined;
  readonly onDismiss: () => void;
}

export function CompactionStatus({ running, failure, onDismiss }: CompactionStatusProps) {
  const { t } = useTranslation();
  if (running) {
    return (
      <p role="status" className="px-3 py-1 text-[0.75rem] text-text-muted sm:px-4">
        {t('chat.compaction.running')}
      </p>
    );
  }
  if (failure === undefined) return null;
  return (
    <p
      role="alert"
      className="flex items-center gap-2 px-3 py-1 text-[0.75rem] text-warning sm:px-4"
    >
      {t(`chat.compaction.${failure}`)}
      <button
        type="button"
        onClick={onDismiss}
        className="underline underline-offset-2 hover:text-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
      >
        {t('chat.compaction.dismiss')}
      </button>
    </p>
  );
}
