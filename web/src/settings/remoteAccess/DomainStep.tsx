import { useRef } from 'react';
import { useTranslation } from 'react-i18next';
import { StepFailure } from './ReasonNotice';
import { StepCard, StepFoot } from './StepCard';
import type { RemoteAccessConfiguration, RemoteAccessStatusDTO } from './remoteAccessApi';
import { reasonOn, type DomainDraft, type StepKey } from './remoteAccessSteps';
import { useStepAction } from './useStepAction';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';

interface DomainStepProps {
  readonly status: RemoteAccessStatusDTO;
  readonly draft: DomainDraft;
  readonly onDraft: (draft: DomainDraft) => void;
  readonly onSave: (configuration: RemoteAccessConfiguration) => Promise<void>;
  readonly onBack: () => void;
  readonly onNext: () => void;
  readonly onOpenStep: (step: StepKey) => void;
}

export function DomainStep({
  status,
  draft,
  onDraft,
  onSave,
  onBack,
  onNext,
  onOpenStep,
}: DomainStepProps) {
  const { t } = useTranslation();
  const zone = draft.zone ?? status.zone_name ?? '';
  const label = draft.label ?? status.public_label ?? 'aura';
  const zoneField = useRef<HTMLInputElement>(null);
  const labelField = useRef<HTMLInputElement>(null);
  const { failure, running, run, clear } = useStepAction();
  const cleanZone = zone.trim().toLowerCase();
  // The server takes lowercase only, and a phone keyboard capitalises the first letter.
  const cleanLabel = label.trim().toLowerCase();
  const changed = cleanZone !== status.zone_name || cleanLabel !== status.public_label;
  const rowReason = reasonOn(status, 'domain');
  const mustSave = changed || !status.enabled || rowReason !== '';
  const fault = failure ?? rowReason;
  const labelFault = fault === 'hostname_taken';
  const zoneFault = fault === 'domain_invalid' || fault === 'domain_elsewhere';
  const hasAddress = status.zone_name !== undefined;

  function whyNotNext(): string | null {
    if (cleanZone === '') return t('remoteAccess.domain.needDomain');
    if (cleanLabel === '') return t('remoteAccess.domain.needLabel');
    return !mustSave && status.phase === 'waiting_nameservers'
      ? t('remoteAccess.domain.waitingNext')
      : null;
  }

  async function save() {
    await run(() =>
      onSave({
        enabled: true,
        generation: status.generation,
        account_id: status.account_id ?? '',
        zone_name: cleanZone,
        public_label: cleanLabel,
      }),
    );
  }

  return (
    <StepCard heading={t('remoteAccess.domain.heading')}>
      <p className="text-sm leading-relaxed text-text-muted">{t('remoteAccess.domain.intro')}</p>
      {status.phase === 'disabled' && hasAddress ? (
        <p role="status" className="text-sm leading-relaxed text-text">
          {t('remoteAccess.domain.off')}
        </p>
      ) : null}
      {status.phase === 'deleting' ? (
        <p role="status" className="text-sm leading-relaxed text-text">
          {t('remoteAccess.domain.deleting')}
        </p>
      ) : null}
      <div className="grid gap-3 sm:grid-cols-2">
        <div className="flex min-w-0 flex-col gap-1.5">
          <Label htmlFor="remote-access-domain">{t('remoteAccess.domain.domain')}</Label>
          <Input
            ref={zoneField}
            id="remote-access-domain"
            value={zone}
            onChange={(event) => {
              onDraft({ ...draft, zone: event.target.value });
              clear();
            }}
            aria-invalid={zoneFault || undefined}
            autoCapitalize="none"
            autoComplete="off"
            spellCheck={false}
          />
        </div>
        <div className="flex min-w-0 flex-col gap-1.5">
          <Label htmlFor="remote-access-label">{t('remoteAccess.domain.label')}</Label>
          <Input
            ref={labelField}
            id="remote-access-label"
            value={label}
            onChange={(event) => {
              onDraft({ ...draft, label: event.target.value });
              clear();
            }}
            aria-invalid={labelFault || undefined}
            autoCapitalize="none"
            autoComplete="off"
            spellCheck={false}
          />
        </div>
      </div>
      <div className="rounded-md bg-surface-2 p-3 text-sm text-text-muted">
        <p className="font-medium text-text">{t('remoteAccess.domain.preview')}</p>
        <p className="font-mono break-all">
          https://{cleanZone === '' || cleanLabel === '' ? '…' : `${cleanLabel}.${cleanZone}`}
        </p>
      </div>
      {changed && status.accepted ? (
        <p className="text-sm leading-relaxed text-warning">
          {t('remoteAccess.domain.changeWarning')}
        </p>
      ) : null}
      {status.phase === 'waiting_nameservers' ? (
        <div className="flex flex-col gap-2 rounded-md bg-surface-2 p-3">
          <p className="text-sm font-medium text-text">
            {t('remoteAccess.domain.nameservers.heading')}
          </p>
          {status.nameservers.length === 0 ? (
            <p className="text-sm leading-relaxed text-text-muted">
              {t('remoteAccess.domain.nameservers.unread')}
            </p>
          ) : (
            <>
              <p className="text-sm leading-relaxed text-text-muted">
                {t('remoteAccess.domain.nameservers.body')}
              </p>
              <ul className="grid gap-2 sm:grid-cols-2">
                {status.nameservers.map((name) => (
                  <li
                    key={name}
                    className="rounded-md bg-surface-3 px-3 py-2 font-mono text-sm break-all text-text"
                  >
                    {name}
                  </li>
                ))}
              </ul>
            </>
          )}
          <p role="status" className="text-sm text-text-muted">
            {t('remoteAccess.domain.nameservers.waiting')}
          </p>
        </div>
      ) : null}
      <StepFailure
        status={status}
        step="domain"
        failure={failure}
        onOpenStep={onOpenStep}
        onFix={() => {
          (labelFault ? labelField : zoneField).current?.focus();
        }}
        onRetry={() => void save()}
        retrying={running}
      />
      <StepFoot
        onBack={onBack}
        why={whyNotNext()}
        busy={running}
        onNext={() => {
          if (mustSave) void save();
          else onNext();
        }}
      />
    </StepCard>
  );
}
