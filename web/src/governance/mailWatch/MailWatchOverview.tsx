import { useState } from 'react';
import { SlidersHorizontal } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import type { MailWatchSettings } from '../mailWatchApi';
import { isUnavailable } from './mailWatchLogic';
import { MailWatchDialog } from './MailWatchDialog';
import { useMailWatchStatus } from './useMailWatch';
import { Button } from '@/components/ui/button';

// MailWatchOverview closes the account list: the watch's terms in a line, and the way in to rules,
// labels and times, which belong to the identity rather than to any one account. A deployment
// that cannot watch mail is a fact of that deployment, not a failure, so it reads calmly here and
// nowhere else: not as a red line, and not once per account.

function hourLabel(settings: MailWatchSettings): string {
  return `${String(settings.digest_hour).padStart(2, '0')}:00`;
}

export function MailWatchOverview() {
  const { t } = useTranslation();
  const status = useMailWatchStatus();
  const [open, setOpen] = useState(false);

  if (status.isPending) return null;
  if (status.isError) {
    return (
      <p role="note" className="text-[13px] text-text-muted">
        {t(
          isUnavailable(status.error)
            ? 'governance.mailWatch.overview.unavailable'
            : 'governance.mailWatch.overview.unreadable',
        )}
      </p>
    );
  }
  const { configured, reason, settings } = status.data;
  if (!configured) {
    return (
      <p role="note" className="text-[13px] text-text-muted">
        {t('governance.mailWatch.overview.unavailable')}
        {reason === undefined ? null : <span className="block text-text-faint">{reason}</span>}
      </p>
    );
  }
  const hour = hourLabel(settings);
  return (
    <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1">
      <p className="min-w-0 text-[13px] text-text-muted">
        {settings.daily_alert_cap === 0
          ? t('governance.mailWatch.overview.noAlerts', { hour })
          : t('governance.mailWatch.overview.summary', { hour, cap: settings.daily_alert_cap })}
      </p>
      <Button
        type="button"
        variant="ghost"
        size="sm"
        onClick={() => {
          setOpen(true);
        }}
      >
        <SlidersHorizontal aria-hidden="true" />
        {t('governance.mailWatch.overview.manage')}
      </Button>
      <MailWatchDialog open={open} onOpenChange={setOpen} />
    </div>
  );
}
