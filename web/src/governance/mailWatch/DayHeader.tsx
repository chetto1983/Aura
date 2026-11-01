import { useId, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { MAIL_WATCH_TIERS, type MailWatchDecision, type MailWatchTier } from '../mailWatchApi';
import { browserDay, longDay, tierCounts } from './mailWatchLogic';
import { TierMark } from './TierControls';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';

// DayHeader heads the day like a dateline: the day in words, its shape as one strip of tiers, and
// the date input that moves to another day. The strip re-divides as corrections land, which is
// the one place motion says something here: the day's balance has changed.

const STRIP_FILL: Readonly<Record<MailWatchTier, string>> = {
  now: 'bg-warning',
  today: 'bg-info',
  later: 'bg-border-strong',
};

interface DayHeaderProps {
  readonly day: string | null;
  readonly rows: readonly MailWatchDecision[];
  readonly onDayChange: (day: string | null) => void;
}

export function DayHeader({ day, rows, onDayChange }: DayHeaderProps) {
  const { t, i18n } = useTranslation();
  const inputId = useId();
  const [today] = useState(() => browserDay(new Date()));
  const shown = day ?? today;
  const counts = tierCounts(rows);
  return (
    <div className="flex flex-col gap-3 px-4 pb-3 pt-4">
      <div className="flex flex-wrap items-end justify-between gap-x-6 gap-y-2">
        <div className="min-w-0">
          <h3 className="font-display text-[26px] leading-tight text-text first-letter:uppercase">
            {longDay(shown, i18n.language)}
          </h3>
          {rows.length > 0 ? (
            <p className="mt-1 flex flex-wrap gap-x-4 font-mono text-[13px] text-text-muted">
              {MAIL_WATCH_TIERS.map((tier) => (
                <TierMark key={tier} tier={tier}>
                  {counts[tier]} {t(`governance.mailWatch.tier.${tier}`)}
                </TierMark>
              ))}
            </p>
          ) : null}
        </div>
        <div className="flex flex-col gap-1">
          <Label htmlFor={inputId} className="text-text-muted">
            {t('governance.mailWatch.day.dateLabel')}
          </Label>
          <Input
            id={inputId}
            type="date"
            value={shown}
            onChange={(event) => {
              const next = event.currentTarget.value;
              onDayChange(next === '' ? null : next);
            }}
            className="w-44"
          />
        </div>
      </div>
      {rows.length > 0 ? (
        <div
          role="img"
          aria-label={t('governance.mailWatch.day.strip', { ...counts })}
          className="flex h-1.5 gap-0.5 overflow-hidden rounded-full"
        >
          {MAIL_WATCH_TIERS.filter((tier) => counts[tier] > 0).map((tier) => (
            <span
              key={tier}
              className={`${STRIP_FILL[tier]} min-w-1 transition-[flex-grow] duration-(--motion-slow) ease-(--motion-ease-out) motion-reduce:transition-none`}
              style={{ flexGrow: counts[tier] }}
            />
          ))}
        </div>
      ) : null}
    </div>
  );
}
