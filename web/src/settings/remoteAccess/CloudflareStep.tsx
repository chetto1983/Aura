import { useRef, useState } from 'react';
import { ShieldCheck } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Spinner } from '../../components/Spinner';
import { FailureLine, StepFailure } from './ReasonNotice';
import { StepCard, StepFoot, StepLink } from './StepCard';
import type {
  CloudflareAccount,
  RemoteAccessConfiguration,
  RemoteAccessStatusDTO,
} from './remoteAccessApi';
import {
  TOKEN_TEMPLATE_URL,
  hasCredentials,
  reasonOn,
  reasonStep,
  type StepKey,
} from './remoteAccessSteps';
import { useStepAction } from './useStepAction';
import { Button } from '@/components/ui/button';
import { Label } from '@/components/ui/label';
import { SecretInput } from '@/components/ui/secret-input';
import { Select } from '@/components/ui/select';

// Cloudflare's own names, as the token form spells them: the same in both locales.
const TOKEN_PERMISSIONS = [
  'Account Settings Read',
  'Zone Read',
  'Zone Edit',
  'DNS Edit',
  'Cloudflare Tunnel Edit',
  'Access: Apps and Policies Edit',
  'Access: Organizations, Identity Providers, and Groups Edit',
];

interface CloudflareStepProps {
  readonly status: RemoteAccessStatusDTO;
  readonly onVerify: (token: string) => Promise<readonly CloudflareAccount[]>;
  readonly onSave: (configuration: RemoteAccessConfiguration) => Promise<void>;
  readonly onDismiss: () => Promise<void>;
  readonly onEdit: () => void;
  readonly onNext: () => void;
  readonly onOpenStep: (step: StepKey) => void;
}

function preferredAccount(found: readonly CloudflareAccount[], current: string | undefined) {
  if (found.some((account) => account.id === current)) return current ?? '';
  return found.length === 1 ? (found.at(0)?.id ?? '') : '';
}

export function CloudflareStep({
  status,
  onVerify,
  onSave,
  onDismiss,
  onEdit,
  onNext,
  onOpenStep,
}: CloudflareStepProps) {
  const { t } = useTranslation();
  const [token, setToken] = useState('');
  const [accounts, setAccounts] = useState<readonly CloudflareAccount[] | null>(null);
  const [chosen, setChosen] = useState('');
  const tokenField = useRef<HTMLInputElement>(null);
  const { failure, running, run, clear } = useStepAction();
  const typed = token.trim();
  const verifying = running && accounts === null;
  const account = accounts === null ? (status.account_id ?? '') : chosen;
  const rowReason = reasonOn(status, 'cloudflare');
  const named = failure === 'stale' || failure === 'generic' ? null : failure;
  const tokenFault = rowReason !== '' || (named !== null && reasonStep(named) === 'cloudflare');

  const known = status.account_id
    ? [{ id: status.account_id, name: status.account_name ?? '' }]
    : [];
  const options = (accounts ?? known).map((item) => ({
    value: item.id,
    label: item.name === '' ? item.id : item.name,
  }));

  function whyNotNext(): string | null {
    if (typed !== '') {
      if (accounts === null) return t('remoteAccess.cloudflare.needVerify');
      return account === '' ? t('remoteAccess.cloudflare.needAccount') : null;
    }
    if (rowReason !== '') return t('remoteAccess.cloudflare.needNewToken');
    return hasCredentials(status) ? null : t('remoteAccess.cloudflare.needToken');
  }

  async function verify() {
    await run(async () => {
      const found = await onVerify(typed);
      setAccounts(found);
      setChosen(preferredAccount(found, status.account_id));
    });
  }

  async function save() {
    const saved = await run(() =>
      onSave({
        enabled: status.enabled,
        generation: status.generation,
        account_id: account,
        zone_name: status.zone_name ?? '',
        public_label: status.public_label ?? '',
        api_token: typed,
      }),
    );
    if (saved) {
      setToken('');
      setAccounts(null);
    }
  }

  return (
    <StepCard heading={t('remoteAccess.cloudflare.heading')}>
      {status.abandoned.length > 0 ? (
        <AbandonedResources status={status} onDismiss={onDismiss} />
      ) : null}
      <p className="text-sm leading-relaxed text-text-muted">
        {t('remoteAccess.cloudflare.intro')}
      </p>
      <div className="flex flex-col items-start gap-2">
        <StepLink href={TOKEN_TEMPLATE_URL} external>
          {t('remoteAccess.cloudflare.createToken')}
        </StepLink>
        <p className="text-[13px] leading-relaxed text-text-muted">
          {t('remoteAccess.cloudflare.tokenHint')}
        </p>
      </div>
      <div className="flex flex-col gap-2 rounded-md bg-surface-2 p-3">
        <p id="remote-access-permissions" className="text-sm font-medium text-text">
          {t('remoteAccess.cloudflare.permissions')}
        </p>
        <ul aria-labelledby="remote-access-permissions" className="flex flex-col gap-1">
          {TOKEN_PERMISSIONS.map((name) => (
            <li key={name} className="font-mono text-[13px] break-words text-text-muted">
              {name}
            </li>
          ))}
        </ul>
      </div>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="remote-access-token">{t('remoteAccess.cloudflare.tokenLabel')}</Label>
        <SecretInput
          ref={tokenField}
          id="remote-access-token"
          value={token}
          onChange={(event) => {
            setToken(event.target.value);
            setAccounts(null);
            clear();
            onEdit();
          }}
          showLabel={t('secret.show', { label: t('remoteAccess.cloudflare.tokenLabel') })}
          hideLabel={t('secret.hide', { label: t('remoteAccess.cloudflare.tokenLabel') })}
          aria-invalid={tokenFault || undefined}
          autoComplete="off"
        />
      </div>
      <Button
        type="button"
        variant="outline"
        className="w-fit"
        disabled={running || typed === ''}
        aria-busy={verifying ? 'true' : undefined}
        onClick={() => void verify()}
      >
        {verifying ? <Spinner /> : <ShieldCheck aria-hidden="true" />}
        {t('remoteAccess.cloudflare.verify')}
      </Button>
      {options.length > 0 ? (
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="remote-access-account">{t('remoteAccess.cloudflare.account')}</Label>
          <Select
            id="remote-access-account"
            value={account}
            onValueChange={setChosen}
            disabled={accounts === null}
            placeholder={t('remoteAccess.cloudflare.accountPlaceholder')}
            options={options}
          />
          {accounts === null ? (
            <p className="text-[13px] leading-relaxed text-text-muted">
              {t('remoteAccess.cloudflare.accountLocked')}
            </p>
          ) : null}
        </div>
      ) : null}
      <StepFailure
        status={status}
        step="cloudflare"
        failure={failure}
        onOpenStep={onOpenStep}
        onFix={() => tokenField.current?.focus()}
        onRetry={() => void (accounts === null ? verify() : save())}
        retrying={running}
      />
      <StepFoot
        why={whyNotNext()}
        busy={running && !verifying}
        onNext={() => {
          if (typed === '') onNext();
          else void save();
        }}
      />
    </StepCard>
  );
}

function AbandonedResources({
  status,
  onDismiss,
}: {
  readonly status: RemoteAccessStatusDTO;
  readonly onDismiss: () => Promise<void>;
}) {
  const { t } = useTranslation();
  const dismiss = useStepAction();
  return (
    <div className="flex flex-col items-start gap-3 rounded-md border border-warning/40 bg-warning/10 p-3">
      <p id="remote-access-abandoned" className="text-sm font-semibold text-text">
        {t('remoteAccess.cloudflare.abandoned.heading')}
      </p>
      <p className="text-sm leading-relaxed text-text-muted">
        {t('remoteAccess.cloudflare.abandoned.body')}
      </p>
      <ul aria-labelledby="remote-access-abandoned" className="flex w-full flex-col gap-2">
        {status.abandoned.map((resource) => (
          <li key={`${resource.kind}:${resource.name}`} className="flex min-w-0 flex-col">
            <span className="text-[13px] text-text-muted">
              {t(`remoteAccess.cloudflare.abandoned.kinds.${resource.kind}`, {
                defaultValue: resource.kind,
              })}
            </span>
            <span className="font-mono text-sm break-all text-text">{resource.name}</span>
          </li>
        ))}
      </ul>
      {dismiss.failure === null ? null : <FailureLine failure={dismiss.failure} />}
      <Button
        type="button"
        variant="outline"
        disabled={dismiss.running}
        onClick={() => void dismiss.run(onDismiss)}
      >
        {dismiss.running ? <Spinner /> : null}
        {t('remoteAccess.cloudflare.abandoned.done')}
      </Button>
    </div>
  );
}
