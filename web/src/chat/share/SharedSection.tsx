import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Share2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { RevokeConfirmDialog } from './RevokeConfirmDialog';
import { ShareLinkRow } from './ShareLinkRow';
import { revokeShare } from './shareApi';
import { useThreadShares } from './useThreadShares';
import { Alert, AlertDescription } from '@/components/ui/alert';

// SharedSection (D-05) — the "Condiviso" section 37B explicitly deferred to this phase. Mounted
// by ArtifactsPanel.tsx below the artifacts list; it derives everything from `threadId` via its
// own hook (useThreadShares) instead of a new panel prop, so ArtifactsPanel's props contract
// stays exactly { threadId, onClose } (stated as a contract in that file's own header).
//
// Reuses the panel's own idioms rather than inventing new ones: the uppercase/tracking-wide
// section-header type treatment, the staggered <li> reveal, and the glyph-plate empty-state
// shape — so this section reads as part of the SAME panel, not a bolted-on second component.
//
// A query error degrades exactly like the panel's own artifacts query does (ArtifactsPanel has
// no dedicated load-error UI either): `data ?? []` renders the empty state either way. This
// also keeps mounting unconditional here safe for the EXISTING, unedited
// ArtifactsPanel.test.tsx, which never mocks the share client — a relative-URL fetch call has
// no base to resolve against under this project's test runtime and rejects near-instantly,
// which React Query swallows into an ordinary error state, never an unhandled rejection.
export interface SharedSectionProps {
  readonly threadId: string;
}

export function SharedSection({ threadId }: SharedSectionProps) {
  const { t } = useTranslation();
  const client = useQueryClient();
  const { data, isLoading } = useThreadShares(threadId);
  const links = data ?? [];

  const [pendingRevokeId, setPendingRevokeId] = useState<string | undefined>(undefined);
  const [revokingId, setRevokingId] = useState<string | undefined>(undefined);
  const [announcement, setAnnouncement] = useState('');

  const revoke = useMutation({
    mutationFn: (id: string) => revokeShare(id),
    onSettled: () => {
      setRevokingId(undefined);
      void client.invalidateQueries({ queryKey: ['shares'] });
    },
    onSuccess: () => {
      setAnnouncement(t('share.revoked.announcement'));
    },
  });

  function confirmRevoke() {
    if (pendingRevokeId === undefined) return;
    const id = pendingRevokeId;
    setPendingRevokeId(undefined);
    setRevokingId(id);
    revoke.mutate(id);
  }

  return (
    <div className="flex flex-col gap-2 border-t border-border pt-3">
      <div className="flex items-center gap-2">
        <Share2 aria-hidden="true" className="size-4 text-accent-text" strokeWidth={1.75} />
        <h3 className="flex-1 truncate text-[0.75rem] font-semibold uppercase tracking-[0.14em] text-text-faint">
          {t('share.section.heading')}
        </h3>
      </div>

      {links.length === 0 ? (
        isLoading ? null : (
          <SharedSectionEmptyState />
        )
      ) : (
        <ul className="flex flex-col gap-1.5">
          {links.map((link, index) => (
            <li
              key={link.id}
              className="animate-in fade-in-0 slide-in-from-bottom-1 fill-mode-backwards"
              style={{ animationDelay: `${String(Math.min(index, 8) * 40)}ms` }}
            >
              <ShareLinkRow
                link={link}
                revoking={revokingId === link.id}
                onRevokeClick={setPendingRevokeId}
              />
            </li>
          ))}
        </ul>
      )}

      {revoke.isError ? (
        <Alert variant="destructive">
          <AlertDescription>{t('share.settings.revokeError')}</AlertDescription>
        </Alert>
      ) : null}

      <span role="status" className="sr-only">
        {announcement}
      </span>

      <RevokeConfirmDialog
        open={pendingRevokeId !== undefined}
        onConfirm={confirmRevoke}
        onCancel={() => {
          setPendingRevokeId(undefined);
        }}
      />
    </div>
  );
}

function SharedSectionEmptyState() {
  const { t } = useTranslation();
  return (
    <div className="flex flex-col items-center justify-center gap-2 px-2 py-5 text-center">
      <span className="grid size-10 place-items-center rounded-2xl bg-surface-2/60 text-text-faint ring-1 ring-border">
        <Share2 className="size-5" strokeWidth={1.5} />
      </span>
      <p className="text-[0.75rem] text-text-faint">{t('share.section.empty')}</p>
    </div>
  );
}
