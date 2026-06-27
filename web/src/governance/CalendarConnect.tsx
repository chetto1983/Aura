import { useId, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Spinner } from '../components/Spinner';
import { PimDeviceCodePanel } from './PimDeviceCodePanel';
import {
  AdvancedSection,
  Field,
  GoogleStartPanel,
  ProviderConfigFields,
  ProviderSelect,
  StartFailedPanel,
} from './CalendarConnectFields';
import {
  pimInitialValues,
  pimMissingRequired,
  pimProviderById,
  pimSubmitConfig,
  type PimAuthFlow,
} from './pimProviders';
import {
  createPimAccount,
  deletePimAccount,
  listPimAccounts,
  pimDeviceStart,
  pimGoogleStart,
  type PimAccount,
  type PimCreateAccountRequest,
  type PimDeviceStart,
  type PimGoogleStart,
  type PimProviderId,
} from './pimApi';

// CalendarConnect — the inline calendar/PIM connect section the MCP server detail renders for the
// aura-pim-mcp server. It lists the configured accounts (~10s poll) each with a Disconnect button,
// and an "Add account" form driven by pimProviders.ts so the operator can configure EVERY provider
// the sidecar exposes (operator directive 2026-06-27: "configure all variable on frontend not just
// google") — not only Google's clientId/clientSecret. The provider picker swaps the visible config
// fields; on create the wizard routes the connect step by the provider's authFlow: Google opens the
// web-redirect consent (GoogleStartPanel), Microsoft/Outlook render the device-code grant
// (PimDeviceCodePanel), and credential/URL providers (IMAP/ICS/JSON) are ready immediately. All copy
// via governance.mcp.calendar.* (en + it); a 503 (sidecar unconfigured) → a calm offline note.

const ACCOUNTS_POLL_MS = 10000;

export function CalendarConnect() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const headingId = useId();

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
          <li
            key={acct.id}
            className="flex items-center justify-between gap-2 rounded-md border border-border bg-surface-2 px-3 py-2"
          >
            <span className="flex min-w-0 flex-col">
              <span className="break-words text-[15.5px] text-text">
                {acct.displayName || acct.id}
              </span>
              <span className="break-all font-mono text-[13px] text-text-muted">
                {acct.provider ? `${acct.provider} · ${acct.id}` : acct.id}
              </span>
            </span>
            <button
              type="button"
              disabled={remove.isPending}
              aria-busy={remove.isPending}
              onClick={() => {
                remove.mutate(acct.id);
              }}
              className="inline-flex min-h-[44px] shrink-0 items-center justify-center gap-2 rounded-md border border-border-strong bg-surface-2 px-3 py-2 text-[13px] font-semibold text-text outline-none hover:border-border-strong focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-60"
            >
              {remove.isPending ? <Spinner /> : null}
              {t('governance.mcp.calendar.disconnect')}
            </button>
          </li>
        ))}
      </ul>
      {remove.isError ? (
        <p role="alert" className="text-[13px] text-danger">
          {t('governance.mcp.calendar.disconnectError')}
        </p>
      ) : null}
    </div>
  );
}

// CreateResult carries the connect outcome. `device` snapshots the submitted `id` so the poll
// target never tracks the live Account ID field, and `startFailed` keeps the (already-created)
// account's id + flow so the operator can retry sign-in WITHOUT re-creating the account (which
// would hit the sidecar's 409-duplicate).
type CreateResult =
  | { readonly kind: 'google'; readonly start: PimGoogleStart }
  | { readonly kind: 'device'; readonly id: string; readonly start: PimDeviceStart }
  | { readonly kind: 'none' }
  | { readonly kind: 'startFailed'; readonly id: string; readonly authFlow: PimAuthFlow };

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

// startConnect runs ONLY the post-create connect step for an already-created account, mapping a
// failure to a `startFailed` result (NOT a thrown error) so a created account whose sign-in didn't
// start is reported as a recoverable state, not as a total create failure.
async function startConnect(id: string, flow: PimAuthFlow): Promise<CreateResult> {
  if (flow === 'none') return { kind: 'none' };
  try {
    if (flow === 'google') return { kind: 'google', start: await pimGoogleStart(id) };
    return { kind: 'device', id, start: await pimDeviceStart(id) };
  } catch {
    return { kind: 'startFailed', id, authFlow: flow };
  }
}

function AddAccountForm({ onCreated }: { readonly onCreated: () => void }) {
  const { t } = useTranslation();
  const ids = { provider: useId(), accountId: useId(), displayName: useId() };

  const [providerId, setProviderId] = useState<PimProviderId>('google');
  const def = pimProviderById(providerId);

  const [accountId, setAccountId] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [values, setValues] = useState<Record<string, string>>(() => pimInitialValues(def));
  const [domains, setDomains] = useState('');
  const [priority, setPriority] = useState('');
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [submitted, setSubmitted] = useState(false);
  const [result, setResult] = useState<CreateResult | null>(null);

  const accountIdEmpty = accountId.trim() === '';
  const displayNameEmpty = displayName.trim() === '';
  const missingConfig = new Set(pimMissingRequired(def, values));
  const hasEmpty = accountIdEmpty || displayNameEmpty || missingConfig.size > 0;

  const create = useMutation({
    mutationFn: async (): Promise<CreateResult> => {
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
      await createPimAccount(body);
      onCreated();
      // create.isError now means ONLY the account creation failed; a failed connect step is a
      // `startFailed` result so the operator isn't told creation failed when it didn't.
      return startConnect(id, def.authFlow);
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
    if (hasEmpty) return;
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
        onChange={setAccountId}
        invalid={submitted && accountIdEmpty}
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

      <button
        type="submit"
        disabled={create.isPending}
        aria-busy={create.isPending}
        className="inline-flex min-h-[44px] items-center justify-center gap-2 self-start rounded-md bg-accent px-4 py-2 text-[13px] font-semibold text-on-accent outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-wait disabled:opacity-60"
      >
        {create.isPending ? <Spinner /> : null}
        {t('governance.mcp.calendar.submit')}
      </button>

      {create.isError ? (
        <p role="alert" className="text-[13px] text-danger">
          {t('governance.error')}
        </p>
      ) : null}

      {result?.kind === 'google' ? <GoogleStartPanel start={result.start} /> : null}
      {result?.kind === 'device' ? (
        <PimDeviceCodePanel accountId={result.id} start={result.start} />
      ) : null}
      {result?.kind === 'startFailed' ? (
        <StartFailedPanel
          pending={retryStart.isPending}
          onRetry={() => {
            retryStart.mutate({ id: result.id, flow: result.authFlow });
          }}
        />
      ) : null}
      {result?.kind === 'none' ? (
        <p role="status" className="text-[13px] text-success">
          {t('governance.mcp.calendar.noAuthCreated')}
        </p>
      ) : null}
    </form>
  );
}
