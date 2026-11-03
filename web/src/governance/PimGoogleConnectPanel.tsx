import { useQuery } from '@tanstack/react-query';
import { ExternalLink } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { pimAccountLinked, type PimGoogleStart } from './pimApi';
import { Button } from '@/components/ui/button';

// PimGoogleConnectPanel renders the Google consent step after the wizard creates a Google account:
// the consent link, then a confirmation once the sidecar stores the token. Registering the redirect
// URI is the admin's job (PimProviderAppsPanel). Consent happens in another tab, so the panel
// polls the account's `linked` state — in the background too, since this tab is not focused while
// the operator is on Google — and turns into a confirmation once the sidecar stores the token.
// Before `linked` existed the panel stayed open until a manual reload (operator, 2026-09-23).
// An account signing in again already reads as linked, so that panel never polls: it keeps the link.

const LINK_POLL_MS = 2000;

export function PimGoogleConnectPanel({
  accountId,
  start,
  signingInAgain = false,
}: {
  readonly accountId: string;
  readonly start: PimGoogleStart;
  readonly signingInAgain?: boolean;
}) {
  const { t } = useTranslation();

  const linked = useQuery({
    queryKey: ['connect', 'pim', 'linked', accountId],
    queryFn: () => pimAccountLinked(accountId),
    enabled: !signingInAgain,
    refetchInterval: (query) => (query.state.data === true ? false : LINK_POLL_MS),
    refetchIntervalInBackground: true,
    retry: false,
  });

  if (!signingInAgain && linked.data === true) {
    return (
      <p role="status" className="text-[13px] text-success">
        {t('governance.mcp.calendar.googleLinked')}
      </p>
    );
  }

  return (
    <div className="flex flex-col gap-2 rounded-md border border-border-strong bg-surface-2 px-3 py-3">
      <Button asChild className="self-start text-[13px]">
        <a href={start.authUrl} target="_blank" rel="noopener noreferrer">
          <ExternalLink data-icon aria-hidden="true" />
          {t('governance.mcp.calendar.connectGoogle')}
        </a>
      </Button>
      <p role="note" className="text-[13px] text-text-muted">
        {t('governance.mcp.calendar.consentNote')}
      </p>
    </div>
  );
}
