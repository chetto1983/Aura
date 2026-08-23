import { ShieldCheck, X } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import {
  useApprovalGrants,
  useRevokeApprovalGrant,
  type ApprovalGrant,
} from '../approvals/useApprovalGrants';
import { Spinner } from '../components/Spinner';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';

// StandingApprovalsPanel is where an "Always approve" stops being permanent. The operator
// grants it in one click from an approval prompt, so revoking it must be one click too, on
// the surface they already use — a grant that can only be undone from a terminal is a
// one-way door with a hidden handle.
//
// It lists only the AUTHENTICATED principal's own grants: the server scopes the read, and
// there is no identity picker here on purpose. Deciding what ANOTHER identity may do is the
// capability admin's job, one section down; this one is the operator's own standing consent.
export function StandingApprovalsPanel() {
  const { t } = useTranslation();
  const grantsQuery = useApprovalGrants();
  const revoke = useRevokeApprovalGrant();
  const grants = grantsQuery.data ?? [];

  return (
    <section aria-labelledby="settings-standing-approvals" className="flex flex-col gap-4">
      <div className="flex flex-col gap-2">
        <h2 id="settings-standing-approvals" className="text-[20px] font-semibold text-text">
          {t('settings.standingApprovals.heading')}
        </h2>
        <p className="max-w-3xl text-[15.5px] leading-relaxed text-text-muted">
          {t('settings.standingApprovals.body')}
        </p>
      </div>

      {grantsQuery.isPending ? <Spinner /> : null}

      {grantsQuery.isError ? (
        <Alert variant="destructive">
          <AlertDescription>{t('settings.standingApprovals.error')}</AlertDescription>
        </Alert>
      ) : null}

      {revoke.isError ? (
        <Alert variant="destructive">
          <AlertDescription>{t('settings.standingApprovals.revokeError')}</AlertDescription>
        </Alert>
      ) : null}

      {!grantsQuery.isPending && !grantsQuery.isError && grants.length === 0 ? (
        // An empty list is the safe state, not a missing feature: say what it means.
        <p className="text-[15px] text-text-muted">{t('settings.standingApprovals.empty')}</p>
      ) : null}

      {grants.length > 0 ? (
        <ul className="flex flex-col gap-2">
          {grants.map((grant) => (
            <GrantRow
              key={`${grant.tool} ${grant.action}`}
              grant={grant}
              busy={revoke.isPending}
              onRevoke={() => {
                revoke.mutate({ tool: grant.tool, action: grant.action });
              }}
            />
          ))}
        </ul>
      ) : null}
    </section>
  );
}

interface GrantRowProps {
  readonly grant: ApprovalGrant;
  readonly busy: boolean;
  readonly onRevoke: () => void;
}

function GrantRow({ grant, busy, onRevoke }: GrantRowProps) {
  const { t } = useTranslation();
  return (
    <li className="flex items-center justify-between gap-4 rounded-md border border-border bg-surface px-3 py-2.5">
      <span className="flex min-w-0 items-center gap-2.5">
        <ShieldCheck aria-hidden="true" className="size-4 shrink-0 text-accent-text" />
        <span className="flex min-w-0 flex-col">
          <span className="truncate font-mono text-[14px] text-text">{grant.subject}</span>
          <span className="text-[12.5px] text-text-muted">
            {t('settings.standingApprovals.grantedAt', {
              date: new Date(grant.granted_at).toLocaleString(),
            })}
          </span>
        </span>
      </span>
      <Button
        type="button"
        variant="outline"
        size="sm"
        disabled={busy}
        onClick={onRevoke}
        className="shrink-0 text-[0.8125rem]"
      >
        <X aria-hidden="true" />
        {t('settings.standingApprovals.revoke')}
      </Button>
    </li>
  );
}
