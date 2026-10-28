import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { RefreshCw, Unlink } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { fetchTelegramAccounts, unlinkTelegramAccount, type TelegramAccount } from './settingsApi';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { ConfirmDialog } from '@/components/ui/confirm-dialog';

// The Telegram accounts linked to the signed-in identity, oldest first, each with an Unlink
// behind a confirmation (spec 2026-10-27-telegram-account-links, D5). The server scopes both
// calls to the session's identity, so this list never shows another identity's accounts.

const ACCOUNTS_KEY = ['settings', 'telegram', 'accounts'];

function accountName(account: TelegramAccount): string {
  if (account.firstName !== '') return account.firstName;
  if (account.username !== '') return `@${account.username}`;
  return String(account.telegramUserId);
}

export function TelegramLinkedAccounts() {
  const { t, i18n } = useTranslation();
  const client = useQueryClient();
  const query = useQuery({
    queryKey: ACCOUNTS_KEY,
    queryFn: ({ signal }) => fetchTelegramAccounts(signal),
  });
  const [pending, setPending] = useState<TelegramAccount | undefined>(undefined);
  const unlink = useMutation({
    mutationFn: (telegramUserId: number) => unlinkTelegramAccount(telegramUserId),
    onSettled: () => void client.invalidateQueries({ queryKey: ACCOUNTS_KEY }),
  });
  const accounts = query.data ?? [];
  const formatDate = (iso: string) =>
    new Intl.DateTimeFormat(i18n.language, { dateStyle: 'medium' }).format(new Date(iso));

  function confirmUnlink() {
    if (pending === undefined) return;
    const id = pending.telegramUserId;
    setPending(undefined);
    unlink.mutate(id);
  }

  return (
    <section aria-labelledby="settings-telegram-accounts" className="flex flex-col gap-3">
      <h3 id="settings-telegram-accounts" className="text-[15px] font-semibold text-text">
        {t('settings.telegram.accounts.heading')}
      </h3>

      {query.isError ? (
        <Alert variant="destructive">
          <AlertDescription>
            {t('settings.telegram.accounts.error')}
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="mt-2 w-fit"
              onClick={() => void query.refetch()}
            >
              <RefreshCw aria-hidden="true" className="size-3.5" />
              {t('settings.actions.retry')}
            </Button>
          </AlertDescription>
        </Alert>
      ) : null}

      {!query.isError && !query.isLoading && accounts.length === 0 ? (
        <p className="rounded-md border border-dashed border-border px-3 py-4 text-[13px] text-text-muted">
          {t('settings.telegram.accounts.empty')}
        </p>
      ) : null}

      {accounts.length > 0 ? (
        <ul className="flex flex-col gap-1.5">
          {accounts.map((account) => (
            <li
              key={account.telegramUserId}
              className="flex items-center justify-between gap-3 rounded-md border border-border bg-surface px-3 py-2.5"
            >
              <div className="min-w-0">
                <p className="truncate text-[14px] font-medium text-text">
                  {accountName(account)}
                  {account.username !== '' && account.firstName !== '' ? (
                    <span className="font-normal text-text-muted"> @{account.username}</span>
                  ) : null}
                </p>
                <p className="text-[12px] text-text-muted">
                  <span className="font-mono">{account.telegramUserId}</span>
                  {' · '}
                  {t('settings.telegram.accounts.meta', {
                    added: formatDate(account.addedAt),
                    seen:
                      account.lastSeenAt === null
                        ? t('settings.telegram.accounts.neverSeen')
                        : t('settings.telegram.accounts.lastSeen', {
                            date: formatDate(account.lastSeenAt),
                          }),
                  })}
                </p>
              </div>
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={unlink.isPending}
                aria-busy={
                  (unlink.isPending && unlink.variables === account.telegramUserId) || undefined
                }
                onClick={() => {
                  setPending(account);
                }}
              >
                <Unlink aria-hidden="true" className="size-3.5" />
                {t('settings.telegram.accounts.unlink')}
              </Button>
            </li>
          ))}
        </ul>
      ) : null}

      {unlink.isError ? (
        <Alert variant="destructive">
          <AlertDescription>
            {t('settings.telegram.accounts.unlinkError', { reason: unlink.error.message })}
          </AlertDescription>
        </Alert>
      ) : null}

      <ConfirmDialog
        open={pending !== undefined}
        onOpenChange={(open) => {
          if (!open) setPending(undefined);
        }}
        title={t('settings.telegram.accounts.confirmTitle', {
          name: pending === undefined ? '' : accountName(pending),
        })}
        description={t('settings.telegram.accounts.confirmBody')}
        cancelLabel={t('settings.telegram.accounts.cancel')}
        confirmLabel={t('settings.telegram.accounts.unlink')}
        onConfirm={confirmUnlink}
      />
    </section>
  );
}
