import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { HostnamesStep } from './HostnamesStep';
import { TokenStep } from './TokenStep';
import { VerifyStep } from './VerifyStep';
import { ZoneStep } from './ZoneStep';
import type {
  CloudflareAccount,
  RemoteAccessConfiguration,
  RemoteAccessStatusDTO,
} from './remoteAccessApi';

interface RemoteAccessWizardProps {
  readonly status: RemoteAccessStatusDTO;
  readonly onVerifyToken: (token: string) => Promise<readonly CloudflareAccount[]>;
  readonly onConfigure: (configuration: RemoteAccessConfiguration) => Promise<void>;
  readonly accepting: boolean;
  readonly onAcceptExternal: () => Promise<void>;
}

const steps = ['account', 'domain', 'nameservers', 'tunnel', 'access', 'warp', 'verify'] as const;

export function RemoteAccessWizard({
  status,
  onVerifyToken,
  onConfigure,
  accepting,
  onAcceptExternal,
}: RemoteAccessWizardProps) {
  const { t } = useTranslation();
  const [candidate, setCandidate] = useState<string | undefined>(undefined);
  const candidateRef = useRef<string | undefined>(undefined);
  const [accounts, setAccounts] = useState<readonly CloudflareAccount[]>([]);
  const current = currentStep(status, candidate !== undefined);
  async function verify(token: string) {
    const found = await onVerifyToken(token);
    candidateRef.current = token;
    setCandidate(token);
    setAccounts(found);
    return found;
  }
  useEffect(
    () => () => {
      candidateRef.current = undefined;
    },
    [],
  );
  async function configure(configuration: RemoteAccessConfiguration) {
    await onConfigure(configuration);
    candidateRef.current = undefined;
    setCandidate(undefined);
  }
  const availableAccounts =
    accounts.length > 0
      ? accounts
      : status.account_id
        ? [{ id: status.account_id, name: status.account_id }]
        : [];
  return (
    <section aria-labelledby="remote-access-heading" className="flex min-w-0 flex-col gap-4">
      <div className="flex flex-col gap-2">
        <h2 id="remote-access-heading" className="text-[20px] font-semibold text-text">
          {t('remoteAccess.heading')}
        </h2>
        <p className="max-w-3xl text-[15.5px] leading-relaxed text-text-muted">
          {t('remoteAccess.body')}
        </p>
      </div>
      <ol
        aria-label={t('remoteAccess.steps.label')}
        className="grid gap-2 sm:grid-cols-2 lg:grid-cols-4"
      >
        {steps.map((step, index) => (
          <li
            key={step}
            aria-current={step === current ? 'step' : undefined}
            className={`min-w-0 rounded-md border px-3 py-2 text-sm ${index <= steps.indexOf(current) ? 'border-accent bg-accent/10 text-text' : 'border-border bg-surface text-text-muted'}`}
          >
            <span className="mr-2 font-mono text-xs">{index + 1}</span>
            {t(`remoteAccess.steps.${step}`)}
          </li>
        ))}
      </ol>
      {!status.api_token_set && candidate === undefined ? <TokenStep onVerified={verify} /> : null}
      {candidate !== undefined ||
      (status.api_token_set &&
        status.phase !== 'waiting_nameservers' &&
        status.phase !== 'connecting' &&
        status.phase !== 'provisioning') ? (
        <ZoneStep
          accounts={availableAccounts}
          generation={status.generation}
          token={candidate}
          initialAccount={status.account_id}
          initialDomain={status.zone_name}
          initialPublicLabel={hostnameLabel(status.public_hostname, status.zone_name, 'aura')}
          initialWarpLabel={hostnameLabel(status.warp_hostname, status.zone_name, 'aura-warp')}
          onSave={configure}
        />
      ) : null}
      {status.phase === 'waiting_nameservers' ? <HostnamesStep status={status} /> : null}
      {status.phase === 'provisioning' ? (
        <div
          role="status"
          className="rounded-md border border-border bg-surface p-4 text-sm text-text-muted"
        >
          {t('remoteAccess.status.phases.provisioning')}
        </div>
      ) : null}
      {status.phase === 'connecting' ? (
        <VerifyStep status={status} busy={accepting} accepted={false} onAccept={onAcceptExternal} />
      ) : null}
    </section>
  );
}

function hostnameLabel(
  hostname: string | undefined,
  zone: string | undefined,
  fallback: string,
): string {
  if (hostname === undefined || zone === undefined) return fallback;
  const suffix = `.${zone}`;
  return hostname.endsWith(suffix) ? hostname.slice(0, -suffix.length) : fallback;
}

function currentStep(status: RemoteAccessStatusDTO, hasCandidate: boolean): (typeof steps)[number] {
  if (!status.api_token_set && !hasCandidate) return 'account';
  if (status.phase === 'waiting_nameservers') return 'nameservers';
  if (status.phase === 'provisioning') return 'tunnel';
  if (status.phase === 'connecting') return 'verify';
  return 'domain';
}
