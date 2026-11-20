import { useId } from 'react';
import { Mail } from 'lucide-react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { MailWatchOverview } from '../../governance/mailWatch/MailWatchOverview';
import { PimAccountRow } from '../../governance/PimAccountRow';
import { deletePimAccount, listPimAccounts, type PimAccount } from '../../governance/pimApi';
import { AddMailAccount } from './AddMailAccount';

// MailAccountsSection is the identity's «Posta e calendario» pane in Settings: its accounts (~10s
// poll), each row saying whether its sign-in finished and carrying the mail and calendar watch's
// switch (PimAccountRow), the watch's terms under the list, and the add flow (AddMailAccount). It is
// a member's own pane: the routes it calls are gated by governance.write, which every identity
// holds. A 503 (sidecar unconfigured) reads as a calm offline note.

const ACCOUNTS_KEY = ['connect', 'pim', 'accounts'] as const;
const ACCOUNTS_POLL_MS = 10000;

export function MailAccountsSection() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const headingId = useId();

  const accounts = useQuery({
    queryKey: ACCOUNTS_KEY,
    queryFn: listPimAccounts,
    refetchInterval: ACCOUNTS_POLL_MS,
    retry: false,
  });

  function refresh() {
    void queryClient.invalidateQueries({ queryKey: ACCOUNTS_KEY });
  }

  return (
    <section aria-labelledby={headingId} className="flex flex-col gap-4">
      <h2 id={headingId} className="flex items-center gap-2 text-[20px] font-semibold text-text">
        <Mail aria-hidden="true" className="size-5 text-accent-text" />
        {t('settings.mail.heading')}
      </h2>

      {accounts.isLoading ? (
        <p role="status" className="text-[15.5px] text-warning">
          {t('settings.mail.loading')}
        </p>
      ) : accounts.isError ? (
        <p role="note" className="text-[15.5px] text-warning">
          {t('settings.mail.offline')}
        </p>
      ) : (
        <AccountList accounts={accounts.data?.accounts ?? []} onChanged={refresh} />
      )}

      <AddMailAccount onCreated={refresh} />
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
    return <p className="text-[15.5px] text-text-muted">{t('settings.mail.empty')}</p>;
  }

  return (
    <div className="flex flex-col gap-2">
      <p className="text-[13px] font-semibold text-text-muted">
        {t('settings.mail.accountsHeading')}
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
          {t('settings.mail.removeError')}
        </p>
      ) : null}
      <MailWatchOverview />
    </div>
  );
}
