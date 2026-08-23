import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { RefreshCw, Share2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { RevokeConfirmDialog } from '../chat/share/RevokeConfirmDialog';
import { ShareLinkRow } from '../chat/share/ShareLinkRow';
import { listShares, revokeShare } from '../chat/share/shareApi';
import { selectActiveShares } from '../chat/share/useThreadShares';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { ConfirmDialog } from '@/components/ui/confirm-dialog';
import { Empty, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty';

// SharedLinksSection (D-01/D-14, RESEARCH.md "Shared-links management") — the global Settings
// surface: every share link the owner holds, across every conversation, with per-row and bulk
// revoke. Registered directly in SettingsWorkspace.tsx using the SAME shape
// TelegramSettingsPanel.tsx already establishes for a self-contained top-level panel: one
// <section> owning its own top border-t divider, no new registration mechanism.
//
// Rows are RICHER than open-webui's equivalent list (tier + created + relative expiry;
// open-webui's own shared-chats list has no expiry column at all) — reuses ShareLinkRow and
// selectActiveShares from the "Condiviso" section (web/src/chat/share/) rather than duplicating
// either.
//
// NO BULK ENDPOINT: the already-shipped internal/agui/share_api.go (plan 37F-10) exposes
// exactly four owner routes (create/list/update-snapshot/single revoke) and no bulk-revoke
// route; adding one is a backend change outside this (frontend-only) plan's scope. "Revoke all"
// therefore fans the existing single-delete call out client-side, one request per currently
// listed link, via Promise.allSettled so one failure never blocks the others from clearing.
//
// NO CONVERSATION TITLE: the list response deliberately does not carry a conversation
// identifier (see ShareLinkRow.tsx's own header note), so ShareLinkRow's optional `title` prop
// is simply left unset here — every other richer-than-reference field is unaffected.

const SHARES_KEY = ['shares'];

export function SharedLinksSection() {
  const { t } = useTranslation();
  const client = useQueryClient();
  const query = useQuery({
    queryKey: SHARES_KEY,
    queryFn: ({ signal }) => listShares(undefined, signal),
    select: selectActiveShares,
  });
  const links = query.data ?? [];

  const [pendingRevokeId, setPendingRevokeId] = useState<string | undefined>(undefined);
  const [revokingId, setRevokingId] = useState<string | undefined>(undefined);
  const [confirmRevokeAll, setConfirmRevokeAll] = useState(false);
  const [revokingAll, setRevokingAll] = useState(false);
  const [announcement, setAnnouncement] = useState('');

  const revoke = useMutation({
    mutationFn: (id: string) => revokeShare(id),
    onSettled: () => {
      setRevokingId(undefined);
      void client.invalidateQueries({ queryKey: SHARES_KEY });
    },
    onSuccess: () => {
      setAnnouncement(t('share.revoked.announcement'));
    },
  });

  const revokeAll = useMutation({
    mutationFn: async (ids: readonly string[]) => {
      const results = await Promise.allSettled(ids.map((id) => revokeShare(id)));
      const failed = results.filter((r) => r.status === 'rejected').length;
      if (failed > 0) throw new Error(`${String(failed)} link(s) could not be revoked`);
    },
    onSettled: () => {
      setRevokingAll(false);
      void client.invalidateQueries({ queryKey: SHARES_KEY });
    },
    onSuccess: () => {
      setAnnouncement(t('share.revoked.announcement'));
    },
  });

  function confirmSingleRevoke() {
    if (pendingRevokeId === undefined) return;
    const id = pendingRevokeId;
    setPendingRevokeId(undefined);
    setRevokingId(id);
    revoke.mutate(id);
  }

  function handleRevokeAllConfirmed() {
    setConfirmRevokeAll(false);
    setRevokingAll(true);
    revokeAll.mutate(links.map((link) => link.id));
  }

  const busy = revokingId !== undefined || revokingAll;

  return (
    <section aria-labelledby="settings-shared-links" className="flex flex-col gap-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <h2 id="settings-shared-links" className="text-[20px] font-semibold text-text">
          {t('share.settings.heading')}
        </h2>
        {links.length > 0 ? (
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={busy}
            aria-busy={revokingAll || undefined}
            onClick={() => {
              setConfirmRevokeAll(true);
            }}
          >
            {t('share.settings.revokeAll')}
          </Button>
        ) : null}
      </div>

      {query.isError ? (
        <Alert variant="destructive">
          <AlertDescription>
            {t('share.settings.loadError')}
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

      {!query.isError && !query.isLoading && links.length === 0 ? (
        <Empty className="border border-dashed border-border bg-surface-2/40 py-8">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <Share2 aria-hidden="true" className="size-5" />
            </EmptyMedia>
            <EmptyTitle className="text-sm">{t('share.settings.empty')}</EmptyTitle>
          </EmptyHeader>
        </Empty>
      ) : null}

      {links.length > 0 ? (
        <ul className="flex flex-col gap-1.5">
          {links.map((link) => (
            <li key={link.id}>
              <ShareLinkRow
                link={link}
                revoking={revokingId === link.id || revokingAll}
                onRevokeClick={setPendingRevokeId}
              />
            </li>
          ))}
        </ul>
      ) : null}

      {revoke.isError || revokeAll.isError ? (
        <Alert variant="destructive">
          <AlertDescription>{t('share.settings.revokeError')}</AlertDescription>
        </Alert>
      ) : null}

      <span role="status" className="sr-only">
        {announcement}
      </span>

      <RevokeConfirmDialog
        open={pendingRevokeId !== undefined}
        onConfirm={confirmSingleRevoke}
        onCancel={() => {
          setPendingRevokeId(undefined);
        }}
      />

      <ConfirmDialog
        open={confirmRevokeAll}
        onOpenChange={setConfirmRevokeAll}
        title={t('share.settings.revokeAllConfirm.title')}
        description={t('share.settings.revokeAllConfirm.body')}
        cancelLabel={t('share.modal.cancel')}
        confirmLabel={t('share.settings.revokeAllConfirm.confirm')}
        onConfirm={handleRevokeAllConfirmed}
      />
    </section>
  );
}
