import { useQuery } from '@tanstack/react-query';
import { ExternalLink } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { pimAccountLinked, type PimGoogleStart } from './pimApi';
import { Button } from '@/components/ui/button';

// PimGoogleConnectPanel renders the Google web-redirect step after the wizard creates a Google
// account and calls pimGoogleStart: the one redirect URI to register (the shared aura-connect relay,
// identical for every install) and the consent link. Consent happens in another tab, so the panel
// polls the account's `linked` state — in the background too, since this tab is not focused while
// the operator is on Google — and turns into a confirmation once the sidecar stores the token.
// Before `linked` existed the panel stayed open until a manual reload (operator, 2026-09-23).

const LINK_POLL_MS = 2000;

export function PimGoogleConnectPanel({
  accountId,
  start,
}: {
  readonly accountId: string;
  readonly start: PimGoogleStart;
}) {
  const { t } = useTranslation();

  const linked = useQuery({
    queryKey: ['connect', 'pim', 'linked', accountId],
    queryFn: () => pimAccountLinked(accountId),
    refetchInterval: (query) => (query.state.data === true ? false : LINK_POLL_MS),
    refetchIntervalInBackground: true,
    retry: false,
  });

  if (linked.data === true) {
    return (
      <p role="status" className="text-[13px] text-success">
        {t('governance.mcp.calendar.googleLinked')}
      </p>
    );
  }

  return (
    <div className="flex flex-col gap-2 rounded-md border border-border-strong bg-surface-2 px-3 py-3">
      <p className="text-[13px] font-semibold text-text">
        {t('governance.mcp.calendar.redirectHeading')}
      </p>
      <p className="break-all font-mono text-[13px] text-text">{start.redirectUri}</p>
      <p className="text-[13px] text-text-muted">{t('governance.mcp.calendar.redirectHint')}</p>
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
