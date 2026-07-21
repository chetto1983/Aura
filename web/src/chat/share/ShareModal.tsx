import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Spinner } from '../../components/Spinner';
import { useConversation } from '../../conversations/useConversations';
import { useCopyAction } from '../displays/useCopyAction';
import { RevokeConfirmDialog } from './RevokeConfirmDialog';
import { createShare, revokeShare, updateShareSnapshot, type Tier } from './shareApi';
import type { ShareLink } from './shareTypes';
import {
  DEFAULT_MAX_EXPIRY_DAYS,
  absoluteShareUrl,
  computeStaleCount,
  daysUntil,
  expiryOptionForApi,
  formatShareDate,
  isCustomExpiryInvalid,
  isLinkExpired,
  type ExpiryChip,
} from './shareViewModel';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';

// ShareModal.tsx (plan 37F-15) — the share phase's densest UI, and the place D-01's "public is
// never default" either becomes real or quietly inverts. Modelled on
// web/src/documents/DocumentUploadDialog.tsx's Dialog-with-state-and-async-action template:
// the same guard-clause + try/catch/finally async handler shape, role="status" for progress,
// role="alert" for errors, an outline Cancel + primary action in DialogFooter, and
// onClick={() => void fn()} — never async onClick.
//
// State machine (RESEARCH.md §2): idle -> creating -> shared -> updating -> revoking ->
// revoked. 'revoked' renders the SAME create form as 'idle' (RESEARCH §5: "the modal returns
// to idle" after a successful revoke) — kept as a distinct phase value only so a caller can
// observe the transition sequence; the JSX branch is intentionally shared with 'idle'.
//
// SELF-CONTAINED SESSION, NOT A RESUME SURFACE: this modal does not call listShares() to check
// for a pre-existing share on open — it always starts at the tier-selection form. Resuming an
// already-shared thread across modal (re)opens is plan 37F-17's "Condiviso" section
// (useThreadShares), a separate, already-scoped surface — see 37F-15-SUMMARY.md's Decisions
// for the full reasoning.
export interface ShareModalProps {
  readonly threadId: string;
  readonly onClose: () => void;
}

type Phase = 'idle' | 'creating' | 'shared' | 'updating' | 'revoking' | 'revoked';

const EXPIRY_CHIPS: readonly ExpiryChip[] = ['1d', '7d', '30d', 'custom'];

export function ShareModal({ threadId, onClose }: ShareModalProps) {
  const { t } = useTranslation();
  const { data: conversation } = useConversation(threadId);
  const { copied, copy } = useCopyAction();

  const [phase, setPhase] = useState<Phase>('idle');
  const [tier, setTier] = useState<Tier>('internal');
  const [expiryChip, setExpiryChip] = useState<ExpiryChip>('7d');
  const [customDays, setCustomDays] = useState('7');
  const [link, setLink] = useState<ShareLink | undefined>(undefined);
  const [error, setError] = useState('');
  const [confirmRevokeOpen, setConfirmRevokeOpen] = useState(false);

  const customDaysNumber = Number.parseInt(customDays, 10);
  const customInvalid = isCustomExpiryInvalid(
    expiryChip,
    Number.isNaN(customDaysNumber) ? 0 : customDaysNumber,
    DEFAULT_MAX_EXPIRY_DAYS,
  );

  async function handleCreate() {
    if (phase !== 'idle' && phase !== 'revoked') return;
    setPhase('creating');
    setError('');
    try {
      const expiryOption =
        tier === 'public' ? expiryOptionForApi(expiryChip, customDaysNumber) : undefined;
      const created = await createShare(threadId, tier, expiryOption);
      setLink(created);
      setPhase('shared');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'create failed');
      setPhase('idle');
    }
  }

  async function handleUpdate() {
    if (link === undefined || phase !== 'shared') return;
    setPhase('updating');
    setError('');
    try {
      const updated = await updateShareSnapshot(link.id);
      setLink(updated);
      setPhase('shared');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'update failed');
      setPhase('shared');
    }
  }

  async function handleRevokeConfirmed() {
    if (link === undefined) return;
    setConfirmRevokeOpen(false);
    setPhase('revoking');
    setError('');
    try {
      await revokeShare(link.id);
      setLink(undefined);
      setPhase('revoked');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'revoke failed');
      setPhase('shared');
    }
  }

  function copyUrl() {
    if (link?.url === undefined) return;
    copy(absoluteShareUrl(link.url));
  }

  const now = new Date();
  const staleCount = computeStaleCount(conversation, link);

  const showCreateForm = phase === 'idle' || phase === 'creating' || phase === 'revoked';
  const sharedPhase = phase === 'shared' || phase === 'updating' || phase === 'revoking';
  // A single derived, properly-narrowed value rather than a boolean flag re-checked against
  // `link` a second time — `sharedLink` is either a real ShareLink or undefined, never both a
  // flag AND a redundant membership check split across two expressions.
  const sharedLink: ShareLink | undefined = sharedPhase ? link : undefined;
  const expired = sharedLink !== undefined && isLinkExpired(sharedLink, now);

  return (
    <Dialog
      open
      onOpenChange={(next) => {
        if (!next) onClose();
      }}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('share.modal.title')}</DialogTitle>
        </DialogHeader>

        {showCreateForm ? (
          <>
            <fieldset className="flex flex-col gap-2">
              <legend className="text-[13px] font-semibold text-text">
                {t('share.modal.tier.legend')}
              </legend>
              <label className="flex items-start gap-2">
                <input
                  type="radio"
                  name="share-tier"
                  value="internal"
                  checked={tier === 'internal'}
                  onChange={() => {
                    setTier('internal');
                  }}
                  aria-describedby="share-tier-internal-description"
                  className="mt-1"
                />
                <span className="text-[13px] font-medium text-text">
                  {t('share.modal.tier.internal.label')}
                </span>
              </label>
              <span
                id="share-tier-internal-description"
                className="pl-6 text-[12px] text-text-muted"
              >
                {t('share.modal.tier.internal.description')}
              </span>

              <label className="flex items-start gap-2">
                <input
                  type="radio"
                  name="share-tier"
                  value="public"
                  checked={tier === 'public'}
                  onChange={() => {
                    setTier('public');
                  }}
                  aria-describedby={
                    tier === 'public'
                      ? 'share-tier-public-description share-public-warning'
                      : 'share-tier-public-description'
                  }
                  className="mt-1"
                />
                <span className="text-[13px] font-medium text-text">
                  {t('share.modal.tier.public.label')}
                </span>
              </label>
              <span id="share-tier-public-description" className="pl-6 text-[12px] text-text-muted">
                {t('share.modal.tier.public.description')}
              </span>
            </fieldset>

            {tier === 'public' ? (
              <div
                id="share-public-warning"
                role="note"
                className="animate-in fade-in-0 slide-in-from-top-1 fill-mode-backwards rounded-md border border-warning/40 bg-warning/10 p-2 text-[12px] text-warning"
              >
                {t('share.modal.publicWarning')}
              </div>
            ) : null}

            {tier === 'public' ? (
              <fieldset
                className="animate-in fade-in-0 slide-in-from-top-1 fill-mode-backwards flex flex-col gap-2"
                style={{ animationDelay: '40ms' }}
              >
                <legend className="text-[13px] font-semibold text-text">
                  {t('share.modal.expiry.legend')}
                </legend>
                <div className="flex flex-wrap gap-1.5">
                  {EXPIRY_CHIPS.map((chip) => (
                    <Button
                      key={chip}
                      type="button"
                      variant="outline"
                      size="sm"
                      aria-pressed={expiryChip === chip}
                      data-active={expiryChip === chip || undefined}
                      onClick={() => {
                        setExpiryChip(chip);
                      }}
                    >
                      {t(`share.modal.expiry.${chip}`)}
                    </Button>
                  ))}
                </div>
                {expiryChip === 'custom' ? (
                  <Input
                    type="number"
                    min={1}
                    aria-label={t('share.modal.expiry.customDaysLabel')}
                    value={customDays}
                    onChange={(event) => {
                      setCustomDays(event.target.value);
                    }}
                    aria-invalid={customInvalid || undefined}
                  />
                ) : null}
              </fieldset>
            ) : null}

            <p className="text-[12px] text-text-muted">{t('share.modal.frozenNote')}</p>

            {error.length > 0 ? (
              <div role="alert" className="text-[13px] text-danger">
                {error}
              </div>
            ) : null}

            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                onClick={() => {
                  onClose();
                }}
              >
                {t('share.modal.cancel')}
              </Button>
              <Button
                type="button"
                disabled={phase === 'creating' || (tier === 'public' && customInvalid)}
                aria-busy={phase === 'creating' || undefined}
                onClick={() => void handleCreate()}
              >
                {phase === 'creating' ? <Spinner /> : null}
                {t('share.modal.create')}
              </Button>
            </DialogFooter>
            {phase === 'creating' ? (
              <span role="status" className="sr-only">
                {t('share.modal.create')}
              </span>
            ) : null}
          </>
        ) : null}

        {sharedLink !== undefined ? (
          <>
            <div className="flex items-center gap-2">
              <Input
                readOnly
                aria-label={t('share.shared.urlLabel')}
                value={absoluteShareUrl(sharedLink.url ?? '')}
              />
              <Button
                type="button"
                variant="outline"
                disabled={expired}
                aria-live="polite"
                onClick={() => {
                  copyUrl();
                }}
              >
                {copied ? t('share.shared.copied') : t('share.shared.copy')}
              </Button>
            </div>

            <p
              className="text-[12px] text-text-muted"
              title={
                sharedLink.expires_at !== undefined
                  ? formatShareDate(sharedLink.expires_at)
                  : undefined
              }
            >
              {sharedLink.expires_at === undefined
                ? t('share.shared.metaNoExpiry', {
                    tier: t('share.settings.tier.' + sharedLink.tier),
                    created: formatShareDate(sharedLink.created_at),
                  })
                : t('share.shared.meta', {
                    tier: t('share.settings.tier.' + sharedLink.tier),
                    expiry: expired
                      ? t('share.settings.expired')
                      : t('share.shared.expiresInDays', {
                          count: daysUntil(sharedLink.expires_at, now),
                        }),
                    created: formatShareDate(sharedLink.created_at),
                  })}
            </p>

            {!expired && staleCount > 0 ? (
              <div className="flex items-center gap-2 text-[12px] text-text-muted">
                <span>{t('share.shared.stale', { count: staleCount })}</span>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  disabled={phase === 'updating'}
                  aria-busy={phase === 'updating' || undefined}
                  onClick={() => void handleUpdate()}
                >
                  {phase === 'updating' ? <Spinner /> : null}
                  {t('share.shared.update')}
                </Button>
              </div>
            ) : null}
            {phase === 'updating' ? (
              <span role="status" className="sr-only">
                {t('share.shared.update')}
              </span>
            ) : null}

            {error.length > 0 ? (
              <div role="alert" className="text-[13px] text-danger">
                {error}
              </div>
            ) : null}

            <DialogFooter>
              <Button
                type="button"
                variant="destructive"
                disabled={phase === 'revoking'}
                aria-busy={phase === 'revoking' || undefined}
                onClick={() => {
                  setConfirmRevokeOpen(true);
                }}
              >
                {phase === 'revoking' ? <Spinner /> : null}
                {t('share.shared.revoke')}
              </Button>
            </DialogFooter>
          </>
        ) : null}
      </DialogContent>

      <RevokeConfirmDialog
        open={confirmRevokeOpen}
        onConfirm={() => void handleRevokeConfirmed()}
        onCancel={() => {
          setConfirmRevokeOpen(false);
        }}
      />
    </Dialog>
  );
}
