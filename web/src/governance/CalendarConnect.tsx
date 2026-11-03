import { useId, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Spinner } from '../components/Spinner';
import { HttpError } from '../api/json';
import { useCapabilities } from '../admin/useAdmin';
import { MailWatchOverview } from './mailWatch/MailWatchOverview';
import { PimAccountRow } from './PimAccountRow';
import { PimConnectPanel } from './PimConnectPanel';
import { PimProviderAppsPanel } from './PimProviderAppsPanel';
import {
  AdvancedSection,
  Field,
  ProviderConfigFields,
  ProviderSelect,
} from './CalendarConnectFields';
import { startConnect, type PimConnectResult } from './pimConnect';
import {
  normalizePimAccountId,
  pimAccountIdError,
  pimInitialValues,
  pimIsManaged,
  pimMissingRequired,
  pimProviderById,
  pimSubmitConfig,
  type PimAuthFlow,
} from './pimProviders';
import {
  createPimAccount,
  deletePimAccount,
  listPimAccounts,
  listPimProviderApps,
  PIM_PROVIDER_NOT_CONFIGURED,
  PIM_PROVIDERS_KEY,
  type PimAccount,
  type PimCreateAccountRequest,
  type PimProviderId,
} from './pimApi';
import { Button } from '@/components/ui/button';

// CalendarConnect — the inline calendar/PIM connect section the MCP server detail renders for the
// aura-pim-mcp server. It lists the configured accounts (~10s poll) each with a Disconnect button,
// and an "Add account" form driven by pimProviders.ts so the operator can configure EVERY provider
// the sidecar exposes (operator directive 2026-06-27: "configure all variable on frontend not just
// google") — not only Google's clientId/clientSecret. The provider picker swaps the visible config
// fields; on create the wizard routes the connect step by the provider's authFlow: Google opens the
// web-redirect consent (PimGoogleConnectPanel), Microsoft/Outlook render the device-code grant
// (PimDeviceCodePanel), and credential/URL providers (IMAP/ICS/JSON) are ready immediately. All copy
// via governance.mcp.calendar.* (en + it); a 503 (sidecar unconfigured) → a calm offline note. Each
// account row says whether its sign-in finished and carries the mail and calendar watch's switch
// (PimAccountRow).

const ACCOUNTS_POLL_MS = 10000;

export function CalendarConnect() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const headingId = useId();
  const { isAdmin } = useCapabilities();

  const accounts = useQuery({
    queryKey: ['connect', 'pim', 'accounts'],
    queryFn: listPimAccounts,
    refetchInterval: ACCOUNTS_POLL_MS,
    retry: false,
  });

  function refresh() {
    void queryClient.invalidateQueries({ queryKey: ['connect', 'pim', 'accounts'] });
  }

  return (
    <section aria-labelledby={headingId} className="flex flex-col gap-3">
      <h4
        id={headingId}
        className="text-[13px] font-semibold uppercase tracking-wide text-text-muted"
      >
        {t('governance.mcp.calendar.heading')}
      </h4>

      {accounts.isLoading ? (
        <p role="status" className="text-[15.5px] text-warning">
          {t('governance.mcp.calendar.loading')}
        </p>
      ) : accounts.isError ? (
        <p role="note" className="text-[15.5px] text-warning">
          {t('governance.mcp.calendar.offline')}
        </p>
      ) : (
        <AccountList accounts={accounts.data?.accounts ?? []} onChanged={refresh} />
      )}

      {isAdmin ? <PimProviderAppsPanel /> : null}
      <AddAccountForm onCreated={refresh} />
    </section>
  );
}

function AccountList({
  accounts,
  onChanged,
}: {
  readonly accounts: readonly PimAccount[];
  readonly onChanged: () => void;
}) {
  const { t } = useTranslation();
  const remove = useMutation({
    mutationFn: (id: string) => deletePimAccount(id),
    onSuccess: onChanged,
  });

  if (accounts.length === 0) {
    return <p className="text-[15.5px] text-text-muted">{t('governance.mcp.calendar.empty')}</p>;
  }

  return (
    <div className="flex flex-col gap-2">
      <p className="text-[13px] font-semibold text-text-muted">
        {t('governance.mcp.calendar.accountsHeading')}
      </p>
      <ul className="flex flex-col gap-2">
        {accounts.map((acct) => (
          <PimAccountRow
            key={acct.id}
            account={acct}
            removing={remove.isPending}
            onRemove={() => {
              remove.mutate(acct.id);
            }}
          />
        ))}
      </ul>
      {remove.isError ? (
        <p role="alert" className="text-[13px] text-danger">
          {t('governance.mcp.calendar.removeError')}
        </p>
      ) : null}
      <MailWatchOverview />
    </div>
  );
}

function parseDomains(raw: string): readonly string[] {
  return raw
    .split(',')
    .map((d) => d.trim())
    .filter((d) => d !== '');
}

function parsePriority(raw: string): number | undefined {
  const trimmed = raw.trim();
  if (trimmed === '') return undefined;
  const n = Number.parseInt(trimmed, 10);
  return Number.isNaN(n) ? undefined : n;
}

/** serverReason pulls the sidecar's own sentence out of a failed mutation. A validation refusal
 * names the exact field to fix, which the generic board error cannot; anything else (a transport
 * failure, a 502) has no sentence and falls back to it. */
function serverReason(error: unknown): string | null {
  if (error instanceof HttpError && error.reason !== '') return error.reason;
  return null;
}

function AddAccountForm({ onCreated }: { readonly onCreated: () => void }) {
  const { t } = useTranslation();
  const ids = { provider: useId(), accountId: useId(), displayName: useId() };

  const [providerId, setProviderId] = useState<PimProviderId>('google');
  const def = pimProviderById(providerId);
  const apps = useQuery({
    queryKey: PIM_PROVIDERS_KEY,
    queryFn: listPimProviderApps,
    retry: false,
  });
  const managed = pimIsManaged(def);
  const appConfigured =
    !managed || apps.data?.some((a) => a.provider === providerId && a.configured) === true;
  const providerLabel = t(def.labelKey);

  const [accountId, setAccountId] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [values, setValues] = useState<Record<string, string>>(() => pimInitialValues(def));
  const [domains, setDomains] = useState('');
  const [priority, setPriority] = useState('');
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [submitted, setSubmitted] = useState(false);
  const [result, setResult] = useState<PimConnectResult | null>(null);

  const accountIdError = pimAccountIdError(accountId);
  const displayNameEmpty = displayName.trim() === '';
  const missingConfig = new Set(pimMissingRequired(def, values));
  const invalidForm =
    accountIdError !== null || displayNameEmpty || missingConfig.size > 0 || !appConfigured;

  function createErrorText(error: unknown): string | null {
    if (error instanceof HttpError && error.reason === PIM_PROVIDER_NOT_CONFIGURED) {
      return t('governance.mcp.calendar.providerNotConfigured', { provider: providerLabel });
    }
    return serverReason(error);
  }

  const create = useMutation({
    mutationFn: async (): Promise<PimConnectResult> => {
      const id = accountId.trim();
      const domainList = parseDomains(domains);
      const prio = parsePriority(priority);
      const body: PimCreateAccountRequest = {
        id,
        displayName: displayName.trim(),
        provider: providerId,
        providerConfig: pimSubmitConfig(def, values),
        ...(domainList.length > 0 ? { domains: domainList } : {}),
        ...(prio !== undefined ? { priority: prio } : {}),
      };
      const created = await createPimAccount(body);
      onCreated();
      // create.isError now means ONLY the account creation failed; a failed connect step is a
      // `startFailed` result so the operator isn't told creation failed when it didn't.
      return startConnect(created.id, def.authFlow);
    },
    onSuccess: setResult,
  });

  // retryStart re-runs ONLY the connect step against the already-created account, so a transient
  // auth-start failure doesn't dead-end on the sidecar's 409-duplicate when re-submitting.
  const retryStart = useMutation({
    mutationFn: ({ id, flow }: { id: string; flow: PimAuthFlow }) => startConnect(id, flow),
    onSuccess: setResult,
  });

  function selectProvider(next: PimProviderId) {
    const nextDef = pimProviderById(next);
    setProviderId(next);
    setValues(pimInitialValues(nextDef));
    setResult(null);
    setSubmitted(false);
    create.reset();
    retryStart.reset();
  }

  function submit() {
    setSubmitted(true);
    if (invalidForm) return;
    create.mutate();
  }

  return (
    <form
      className="flex flex-col gap-3 rounded-md border border-border bg-surface px-3 py-3"
      onSubmit={(event) => {
        event.preventDefault();
        submit();
      }}
    >
      <p className="text-[13px] font-semibold text-text-muted">
        {t('governance.mcp.calendar.addHeading')}
      </p>

      <ProviderSelect id={ids.provider} value={providerId} onChange={selectProvider} />

      <Field
        id={ids.accountId}
        label={t('governance.mcp.calendar.accountIdLabel')}
        value={accountId}
        onChange={(next) => {
          setAccountId(normalizePimAccountId(next));
        }}
        invalid={submitted && accountIdError !== null}
        error={accountIdError === 'slug' ? t('governance.mcp.calendar.accountIdSlug') : undefined}
        hint={t('governance.mcp.calendar.accountIdHint')}
      />
      <Field
        id={ids.displayName}
        label={t('governance.mcp.calendar.displayNameLabel')}
        value={displayName}
        onChange={setDisplayName}
        invalid={submitted && displayNameEmpty}
      />

      <ProviderConfigFields
        def={def}
        values={values}
        onChange={(key, v) => {
          setValues((prev) => ({ ...prev, [key]: v }));
        }}
        submitted={submitted}
        missing={missingConfig}
      />

      {managed && apps.isSuccess && !appConfigured ? (
        <p role="note" className="text-[13px] text-warning">
          {t('governance.mcp.calendar.providerNotConfigured', { provider: providerLabel })}
        </p>
      ) : null}

      <AdvancedSection
        open={showAdvanced}
        onToggle={() => {
          setShowAdvanced((o) => !o);
        }}
        domains={domains}
        onDomains={setDomains}
        priority={priority}
        onPriority={setPriority}
      />

      <Button
        type="submit"
        disabled={create.isPending || !appConfigured}
        aria-busy={create.isPending}
        className="self-start text-[13px] disabled:cursor-wait"
      >
        {create.isPending ? <Spinner /> : null}
        {t('governance.mcp.calendar.submit')}
      </Button>

      {create.isError ? (
        <p role="alert" className="text-[13px] text-danger">
          {createErrorText(create.error) ?? t('governance.error')}
        </p>
      ) : null}

      {result === null ? null : (
        <PimConnectPanel
          result={result}
          retrying={retryStart.isPending}
          onRetry={() => {
            if (result.kind === 'startFailed') {
              retryStart.mutate({ id: result.id, flow: result.authFlow });
            }
          }}
        />
      )}
    </form>
  );
}
