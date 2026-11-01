import { useId, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { MailWatchSettings } from '../mailWatchApi';
import { dayAndTime, errorSentence } from './mailWatchLogic';
import { PanelHeading, PanelPending } from './PanelParts';
import { useMailWatchStatus, useSaveSettings } from './useMailWatch';
import { Spinner } from '@/components/Spinner';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select } from '@/components/ui/select';

// SettingsPanel holds the two numbers a person sets: the digest's hour and the daily cap. The
// check interval is the task's, set at enable, so it is said and not offered.

/** daily_alert_cap's CHECK in migration 0150. */
const CAP_MAX = 100;

const HOURS = Array.from({ length: 24 }, (_, hour) => ({
  value: String(hour),
  label: `${String(hour).padStart(2, '0')}:00`,
}));

export function SettingsPanel() {
  const { t } = useTranslation();
  const status = useMailWatchStatus();
  if (status.isPending) {
    return (
      <div className="px-4 py-4">
        <PanelPending />
      </div>
    );
  }
  if (status.isError) {
    return (
      <p role="alert" className="px-4 py-4 text-[13px] text-danger">
        {errorSentence(status.error, t)}
      </p>
    );
  }
  return <SettingsForm settings={status.data.settings} />;
}

function parseCap(raw: string): number | null {
  if (!/^\d+$/.test(raw.trim())) return null;
  const cap = Number.parseInt(raw, 10);
  return cap <= CAP_MAX ? cap : null;
}

function SettingsForm({ settings }: { readonly settings: MailWatchSettings }) {
  const { t, i18n } = useTranslation();
  const ids = { hour: useId(), cap: useId(), capHint: useId() };
  const [hour, setHour] = useState(String(settings.digest_hour));
  const [cap, setCap] = useState(String(settings.daily_alert_cap));
  const [submitted, setSubmitted] = useState(false);
  const save = useSaveSettings();
  const parsedCap = parseCap(cap);
  const capInvalid = submitted && parsedCap === null;
  const lastDigest = settings.last_digest_at;

  return (
    <form
      noValidate
      className="flex max-w-md flex-col gap-4 px-4 py-4"
      onSubmit={(event) => {
        event.preventDefault();
        setSubmitted(true);
        if (parsedCap === null) return;
        save.mutate({ digest_hour: Number.parseInt(hour, 10), daily_alert_cap: parsedCap });
      }}
    >
      <PanelHeading title={t('governance.mailWatch.settings.heading')} />
      <div className="flex flex-col gap-1">
        <Label htmlFor={ids.hour}>{t('governance.mailWatch.settings.digestHour')}</Label>
        <Select id={ids.hour} value={hour} onValueChange={setHour} options={HOURS} />
        <p className="text-[13px] text-text-muted">
          {t('governance.mailWatch.settings.digestHint')}
        </p>
      </div>
      <div className="flex flex-col gap-1">
        <Label htmlFor={ids.cap}>{t('governance.mailWatch.settings.cap')}</Label>
        <Input
          id={ids.cap}
          type="number"
          inputMode="numeric"
          min={0}
          max={CAP_MAX}
          value={cap}
          aria-invalid={capInvalid || undefined}
          aria-describedby={ids.capHint}
          onChange={(event) => {
            setCap(event.currentTarget.value);
          }}
        />
        <p
          id={ids.capHint}
          className={capInvalid ? 'text-[13px] text-danger' : 'text-[13px] text-text-muted'}
        >
          {t(
            capInvalid
              ? 'governance.mailWatch.settings.capInvalid'
              : 'governance.mailWatch.settings.capHint',
          )}
        </p>
      </div>
      <p className="text-[13px] text-text-muted">
        {t('governance.mailWatch.settings.interval', { count: settings.interval_minutes })}{' '}
        {lastDigest === null
          ? t('governance.mailWatch.settings.noDigest')
          : t('governance.mailWatch.settings.lastDigest', {
              time: dayAndTime(lastDigest, i18n.language),
            })}
      </p>
      <div className="flex flex-wrap items-center gap-3">
        <Button type="submit" disabled={save.isPending} aria-busy={save.isPending || undefined}>
          {save.isPending ? <Spinner /> : null}
          {t('governance.mailWatch.settings.save')}
        </Button>
        <p role="status" className="text-[13px] text-text-muted empty:hidden">
          {save.isSuccess ? t('governance.mailWatch.settings.saved') : null}
        </p>
        {save.isError ? (
          <p role="alert" className="min-w-0 text-[13px] text-danger">
            {errorSentence(save.error, t)}
          </p>
        ) : null}
      </div>
    </form>
  );
}
