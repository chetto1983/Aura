import { useTranslation } from 'react-i18next';
import { Link2 } from 'lucide-react';
import { useCopyAction } from '../displays/useCopyAction';
import type { ShareLink } from './shareTypes';
import { absoluteShareUrl, daysUntil, formatShareDate, isLinkExpired } from './shareViewModel';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';

// ShareLinkRow — the row BOTH share-management surfaces render: the per-thread "Condiviso"
// section (SharedSection.tsx) and the global Settings list (SharedLinksSection.tsx). Built once
// here and imported by both, rather than copy-pasted, per 37F-17 Task 2's explicit instruction
// to reuse Task 1's row (CLAUDE.md: never duplicate; extract a helper).
//
// `title` is optional and, today, always left unset by SharedLinksSection: the already-shipped
// GET /api/shares response (internal/agui/share_api.go, plan 37F-10) deliberately does not
// carry a conversation identifier on the wire — its own doc comment lists exactly which
// share.Link fields are excluded and states plainly that none of them belong on the wire. There
// is therefore no id here to resolve a title from without extending that Go response, which is
// a backend change out of this (frontend-only) plan's scope. The prop stays so a future plan
// can wire a real title through with a one-line change at this component's call site.
//
// The internal/public asymmetry below is the security design (PRD item 17): an internal row's
// link is ALWAYS derived directly from `link.id` (never from a server-sent field, so the
// guarantee holds independent of what any response happens to include) — it carries no secret.
// A public row's one-time credential is handed back only at mint and is never listed again, so
// a public row here never offers a copy affordance — there is nothing to copy.
export interface ShareLinkRowProps {
  readonly link: ShareLink;
  /** The conversation title, when the caller can resolve one (see the header note above). */
  readonly title?: string;
  /** Injectable clock for deterministic expiry rendering in tests; defaults to the real time. */
  readonly now?: Date;
  readonly onRevokeClick: (id: string) => void;
  /** Disables the revoke control while this row's revoke is in flight. */
  readonly revoking?: boolean;
}

export function ShareLinkRow({
  link,
  title,
  now,
  onRevokeClick,
  revoking = false,
}: ShareLinkRowProps) {
  const { t } = useTranslation();
  const { copied, copy } = useCopyAction();
  const clock = now ?? new Date();
  const expired = isLinkExpired(link, clock);
  const copyHref = link.tier === 'internal' ? `/shared/${link.id}` : undefined;

  return (
    <div
      className="group/row flex items-center gap-3 rounded-lg border border-transparent bg-surface-2/40 px-2.5 py-2 transition-colors hover:border-border hover:bg-surface-2 data-[expired=true]:opacity-70"
      data-expired={expired}
    >
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        {title !== undefined && (
          <span className="truncate text-[0.8125rem] font-medium text-text">{title}</span>
        )}
        <span className="flex flex-wrap items-center gap-1.5 text-[0.75rem] text-text-faint">
          <Badge
            variant={link.tier === 'public' ? 'warning' : 'secondary'}
            className="text-[0.75rem]"
          >
            {t(`share.settings.tier.${link.tier}`)}
          </Badge>
          <span>{formatShareDate(link.created_at)}</span>
          {link.expires_at !== undefined && (
            <span title={formatShareDate(link.expires_at)}>
              {expired
                ? t('share.settings.expired')
                : t('share.settings.expiresIn', { days: daysUntil(link.expires_at, clock) })}
            </span>
          )}
        </span>
      </div>

      {copyHref !== undefined && !expired ? (
        <button
          type="button"
          onClick={() => {
            copy(absoluteShareUrl(copyHref));
          }}
          aria-label={copied ? t('share.shared.copied') : t('share.shared.copy')}
          className="grid size-8 shrink-0 place-items-center rounded-full border border-transparent text-text-faint transition-colors hover:border-accent hover:bg-surface hover:text-accent-text focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
        >
          <Link2 aria-hidden="true" className="size-4" />
        </button>
      ) : null}

      <Button
        type="button"
        variant="outline"
        size="sm"
        disabled={revoking}
        aria-busy={revoking || undefined}
        onClick={() => {
          onRevokeClick(link.id);
        }}
      >
        {t('share.settings.revoke')}
      </Button>
    </div>
  );
}
