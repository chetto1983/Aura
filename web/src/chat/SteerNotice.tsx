import { useTranslation } from 'react-i18next';
import type { SteerNoticeView } from './ExternalStoreChat_steer';

// SteerNotice — the D-10 visible echo that a submit redirected the live turn instead of doing
// nothing. Follows CompactionMarker's placement idea (a thread-level status line, not a
// message part the runtime could branch from): the steer is already persisted server-side as
// an ordinary user turn (52-04), so a synthetic message part here would be a second
// representation of the same fact. Renders EITHER the redirect echo (notice) or a refusal
// (refusal) — never both, since a refusal never queued anything to echo.

export interface SteerNoticeProps {
  readonly notice: SteerNoticeView | undefined;
  readonly refusal: string | undefined;
}

export function SteerNotice({ notice, refusal }: SteerNoticeProps) {
  const { t } = useTranslation();

  if (refusal !== undefined) {
    return (
      <p role="status" className="px-3 py-1 text-[0.75rem] text-warning sm:px-4">
        {refusal}
      </p>
    );
  }
  if (notice === undefined) return null;

  return (
    <p
      key={notice.id}
      role="status"
      className="animate-in fade-in-0 slide-in-from-bottom-1 fill-mode-backwards px-3 py-1 text-[0.75rem] text-accent-text duration-200 sm:px-4"
    >
      {t(
        notice.kind === 'autoDelivered'
          ? 'chat.steer.notice.autoDelivered'
          : 'chat.steer.notice.redirected',
      )}
    </p>
  );
}
