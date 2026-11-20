import { useId } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { useCapabilities } from '../admin/useAdmin';
import { OtherAccountForm } from '../settings/mail/OtherAccountForm';
import { MailWatchOverview } from './mailWatch/MailWatchOverview';
import { PimAccountRow } from './PimAccountRow';
import { PimProviderAppsPanel } from './PimProviderAppsPanel';
import { deletePimAccount, listPimAccounts, type PimAccount } from './pimApi';

// CalendarConnect — the inline calendar/PIM connect section the MCP server detail renders for the
// aura-pim-mcp server. It lists the configured accounts (~10s poll) each with a Disconnect button,
// and the full account form (OtherAccountForm), which configures EVERY provider the sidecar exposes
// (operator directive 2026-06-27: "configure all variable on frontend not just google"). All copy via
// governance.mcp.calendar.* (en + it); a 503 (sidecar unconfigured) → a calm offline note. Each
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
      <OtherAccountForm onCreated={refresh} />
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
