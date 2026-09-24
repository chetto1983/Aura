import { CircleArrowUp } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useUpdateCenter } from './updateCenterContext';
import { Button } from '@/components/ui/button';

// The way back to a dismissed or postponed update decision: quiet by design, it only reopens
// the dialog and never decides anything itself.
export function UpdateIndicator() {
  const { t } = useTranslation();
  const center = useUpdateCenter();
  if (center?.status === undefined || !center.indicatorVisible) return null;
  const failed = center.status.state === 'failed';
  const label = failed ? t('update.indicator.failed') : t('update.indicator.pending');
  return (
    <Button
      type="button"
      variant="outline"
      size="sm"
      data-state={center.status.state}
      aria-label={label}
      title={label}
      onClick={center.openDialog}
      className="update-indicator relative rounded-full text-text-muted hover:text-text"
    >
      <CircleArrowUp aria-hidden="true" />
      <span className="hidden xl:inline">{label}</span>
      <span className="update-indicator__dot" aria-hidden="true" />
    </Button>
  );
}
