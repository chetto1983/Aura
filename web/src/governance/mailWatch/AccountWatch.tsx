import { useId, useState } from 'react';
import { ArrowRight, Pause, TriangleAlert } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import type { PimAccount } from '../pimApi';
import type { MailWatchAccount, MailWatchResource } from '../mailWatchApi';
import {
  canResume,
  clockTime,
  errorSentence,
  failingSince,
  failureSentence,
  pausedSentence,
} from './mailWatchLogic';
import { MailWatchDialog } from './MailWatchDialog';
import { useMailWatchStatus, useWatchToggle } from './useMailWatch';
import { Spinner } from '@/components/Spinner';
import { Button } from '@/components/ui/button';
import { Label } from '@/components/ui/label';
import { Switch } from '@/components/ui/switch';

// AccountWatch is the watch on an account's row: the switch, then what the watch is doing in a
// sentence. An account the status does not list has no watch. With the deployment unable to watch
// mail (`configured: false`) a new watch cannot start, so the row draws nothing and the overview
// says why, once; a watch that already exists keeps its switch, because turning off still works.

export function AccountWatch({ account }: { readonly account: PimAccount }) {
  const { t } = useTranslation();
  const switchId = useId();
  const stateId = useId();
  const status = useMailWatchStatus();
  const toggle = useWatchToggle(account.id);
  const [dayOpen, setDayOpen] = useState(false);

  if (status.data === undefined) return null;
  const { configured, settings, accounts } = status.data;
  const watch = accounts.find((entry) => entry.account_id === account.id);
  if (!configured && watch === undefined) return null;
  const name = account.displayName || account.id;
  const checked = toggle.isPending ? toggle.variables : watch !== undefined;

  return (
    <div className="flex flex-col border-t border-border pb-1">
      <div className="flex items-center justify-between gap-3">
        <Label htmlFor={switchId} className="min-h-11 min-w-0 flex-1 cursor-pointer gap-3">
          <Switch
            id={switchId}
            checked={checked}
            disabled={toggle.isPending}
            aria-label={t('governance.mailWatch.switch.labelFor', { account: name })}
            aria-describedby={stateId}
            onCheckedChange={(next) => {
              toggle.mutate(next);
            }}
            className="data-[state=checked]:bg-info"
          />
          <span className="min-w-0">{t('governance.mailWatch.switch.label')}</span>
          {toggle.isPending ? <Spinner /> : null}
        </Label>
        {watch === undefined ? null : (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={() => {
              setDayOpen(true);
            }}
            className="shrink-0"
          >
            {t('governance.mailWatch.switch.open')}
            <ArrowRight aria-hidden="true" />
          </Button>
        )}
      </div>
      <div id={stateId} className="flex flex-col gap-1">
        {watch === undefined ? (
          <p className="text-[13px] text-text-muted">{t('governance.mailWatch.switch.pitch')}</p>
        ) : (
          <WatchState
            watch={watch}
            cap={settings.daily_alert_cap}
            resuming={toggle.isPending}
            onResume={() => {
              toggle.mutate(true);
            }}
          />
        )}
        <p className="text-[13px] text-text-muted">{t('governance.mailWatch.switch.scope')}</p>
      </div>
      {toggle.isError ? (
        <p role="alert" className="text-[13px] text-danger">
          {errorSentence(toggle.error, t)}
        </p>
      ) : null}
      {watch === undefined ? null : (
        <MailWatchDialog
          open={dayOpen}
          onOpenChange={setDayOpen}
          account={{ id: account.id, name }}
        />
      )}
    </div>
  );
}

interface WatchStateProps {
  readonly watch: MailWatchAccount;
  readonly cap: number;
  readonly resuming: boolean;
  readonly onResume: () => void;
}

function WatchState({ watch, cap, resuming, onResume }: WatchStateProps) {
  const { t } = useTranslation();
  const failing = watch.resources.filter((entry) => entry.status === 'failing');
  const unsupported = watch.resources.filter((entry) => entry.status === 'unsupported');
  const refused = unsupported.filter((entry) => entry.last_error === 'permission_denied');
  const notOffered = unsupported.filter((entry) => !refused.includes(entry));
  return (
    <>
      {watch.paused ? (
        <div className="flex flex-wrap items-center gap-x-3">
          <p className="flex items-center gap-1.5 text-[13px] font-semibold text-warning">
            <Pause aria-hidden="true" className="size-3.5 shrink-0" />
            {pausedSentence(watch.paused_reason, t)}
          </p>
          {canResume(watch.paused_reason) ? (
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={resuming}
              onClick={onResume}
            >
              {t('governance.mailWatch.state.resume')}
            </Button>
          ) : null}
        </div>
      ) : (
        <RunningLine watch={watch} cap={cap} />
      )}
      {[...failing, ...refused].map((entry) => (
        <FailingLine key={entry.resource} entry={entry} />
      ))}
      {notOffered.map((entry) => (
        <p key={entry.resource} className="text-[13px] text-text-muted">
          {t('governance.mailWatch.state.unsupported', {
            resource: t(`governance.mailWatch.resource.${entry.resource}`),
          })}
        </p>
      ))}
    </>
  );
}

/** A cap of 0 means no alerts at all, so there is no count to hold it against. */
function RunningLine({ watch, cap }: { readonly watch: MailWatchAccount; readonly cap: number }) {
  const { t, i18n } = useTranslation();
  const used = watch.alerts_today;
  const parts = [
    ...(cap === 0 ? [] : [t('governance.mailWatch.state.alerts', { used, cap })]),
    ...(cap > 0 && used >= cap ? [t('governance.mailWatch.state.capReached')] : []),
    ...(watch.next_run_at === null
      ? []
      : [
          t('governance.mailWatch.state.next', {
            time: clockTime(watch.next_run_at, i18n.language),
          }),
        ]),
  ];
  return (
    <p className="flex flex-wrap items-center gap-x-1.5 text-[13px] text-text-muted">
      <span aria-hidden="true" className="size-2 shrink-0 rounded-full bg-success" />
      <span className="font-semibold text-text">{t('governance.mailWatch.state.watching')}</span>
      {parts.length === 0 ? null : <span>· {parts.join(' · ')}</span>}
    </p>
  );
}

function FailingLine({ entry }: { readonly entry: MailWatchResource }) {
  const { t, i18n } = useTranslation();
  const name = t(`governance.mailWatch.resource.${entry.resource}`);
  const since = entry.failing_since;
  const [now] = useState(() => new Date());
  return (
    <p className="flex items-start gap-1.5 text-[13px] text-warning">
      <TriangleAlert aria-hidden="true" className="mt-0.5 size-3.5 shrink-0" />
      <span>
        <strong className="font-semibold">
          {since === null ? name : `${name}, ${failingSince(since, now, i18n.language, t)}`}:
        </strong>{' '}
        {failureSentence(entry.last_error ?? '', t)}
      </span>
    </p>
  );
}
