import { useState } from 'react';
import { ChevronDown, ChevronRight, Trash2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { AdminSection } from '../admin/AdminSection';
import { IDENTITY_CREATE, hasCapability, type AdminIdentity } from '../admin/adminApi';
import { useAdminIdentities, useCapabilities, useRemoveIdentity } from '../admin/useAdmin';
import { Spinner } from '../components/Spinner';
import { CreditPanel } from './CreditPanel';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { ConfirmDialog } from '@/components/ui/confirm-dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';

// IdentityRoster (RBAC-05/RBAC-06/RBAC-11) replaces CapabilityAdminPanel: RBAC-03 grants every
// non-administrative capability at provisioning and RBAC-06 refuses the administrative pair
// through the API, so there is nothing left for a grant/revoke control to do — this is a
// read-only roster plus one destructive action, not a hidden or disabled version of the old
// control (CLAUDE.md "no asilo nido"). It mounts inside the existing AdminSection chrome
// (loading/error/empty guards reused verbatim, per the UI-SPEC) and its own Card-list body is
// the "populated" state AdminSection delegates to.
//
// Typed-confirmation match semantics (backstop decision, UI-SPEC §Resolved-backstop "Removal
// confirmation dialog · partial", held out rather than assumed upstream): TRIMMED, case-
// SENSITIVE. Trimming forgives the single most common real mistake — a leading/trailing space
// left by copy-paste — while keeping case exact so a careless near-match on a destructive,
// cross-plane, irreversible action does not slip through. Both neighbouring cases (a trailing
// space; a different case) are exercised in IdentityRoster.test.tsx.
function emailConfirms(typed: string, email: string): boolean {
  return typed.trim() === email;
}

/** isAdministrativeIdentity mirrors internal/identity.IsAdministrative client-side: an
 * identity holding identity.create (always granted with identity.delete, D-02) is the
 * bootstrap admin. */
function isAdministrativeIdentity(identity: AdminIdentity): boolean {
  return hasCapability(identity.capabilities, IDENTITY_CREATE);
}

export function IdentityRoster() {
  const { t } = useTranslation();
  const identitiesQuery = useAdminIdentities();
  const { identityId: selfId } = useCapabilities();
  const removeMutation = useRemoveIdentity();
  const [pending, setPending] = useState<AdminIdentity | undefined>(undefined);
  const [confirmText, setConfirmText] = useState('');
  // One row's credit at a time. CreditPanel's own read is disabled on an empty id, so a closed
  // row costs no request -- a roster of twenty identities does not fan out twenty credit GETs.
  const [openCredit, setOpenCredit] = useState('');

  const identities = identitiesQuery.data ?? [];
  // Only the true in-flight window replaces the row's action with the busy indicator. On
  // error the dialog stays open with its own retry affordance (below), so the row goes back
  // to its normal button rather than getting stuck showing "Removing...".
  const removingId = removeMutation.isPending ? removeMutation.variables : undefined;

  function openRemove(identity: AdminIdentity) {
    removeMutation.reset();
    setConfirmText('');
    setPending(identity);
  }

  function handleOpenChange(open: boolean) {
    if (!open) {
      removeMutation.reset();
      setConfirmText('');
      setPending(undefined);
    }
  }

  function confirmRemove() {
    if (pending === undefined) return;
    removeMutation.mutate(pending.id, {
      onSuccess: () => {
        setConfirmText('');
        setPending(undefined);
      },
    });
  }

  const dialogPendingHere =
    pending !== undefined && removeMutation.isPending && removeMutation.variables === pending.id;
  const dialogErrorHere =
    pending !== undefined && removeMutation.isError && removeMutation.variables === pending.id;
  const matches = pending !== undefined && emailConfirms(confirmText, pending.name);

  return (
    <AdminSection
      labelId="admin-roster"
      keyPrefix="admin.roster"
      identitiesQuery={identitiesQuery}
      identities={identities}
    >
      {/* div + explicit role, not ul/li — the pattern McpBoard, SkillsList and PathStrip
          already use here. On a ul the role is redundant and the a11y lint rejects it, and
          Tailwind's preflight strips list-style anyway, which is what drops the implicit
          role in the first place. */}
      <div role="list" className="flex flex-col gap-2">
        {identities.map((identity) => {
          const admin = isAdministrativeIdentity(identity);
          const isSelf = identity.id === selfId;
          const busy = removingId === identity.id;
          const creditOpen = openCredit === identity.id;
          return (
            <div role="listitem" key={identity.id}>
              <div className="flex flex-col gap-3 rounded-lg border border-border bg-surface p-4">
                <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                  <div className="flex min-w-0 flex-col gap-1.5">
                    <span className="break-all font-mono text-[15.5px] text-text">
                      {identity.name}
                      {isSelf ? ` (${t('admin.identity.you')})` : ''}
                    </span>
                    <Badge variant={admin ? 'info' : 'secondary'} className="w-fit">
                      {admin ? t('admin.roster.adminBadge') : t('admin.roster.memberBadge')}
                    </Badge>
                  </div>
                  {busy ? (
                    <div
                      role="status"
                      aria-busy="true"
                      className="flex items-center gap-2 text-[13px] text-text-muted"
                    >
                      <Spinner />
                      {t('admin.removal.inFlight', { name: identity.name })}
                    </div>
                  ) : (
                    <div className="flex flex-col items-start gap-1 sm:items-end">
                      <Button
                        type="button"
                        variant="destructive"
                        size="icon"
                        disabled={isSelf}
                        aria-label={t('admin.roster.removeAriaLabel', { name: identity.name })}
                        onClick={() => {
                          openRemove(identity);
                        }}
                      >
                        <Trash2 aria-hidden="true" />
                      </Button>
                      {isSelf ? (
                        <p className="max-w-[16rem] text-[12px] text-text-muted sm:text-right">
                          {t('admin.roster.cannotRemoveSelf')}
                        </p>
                      ) : null}
                    </div>
                  )}
                </div>
                <div className="border-t border-border pt-3">
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    aria-expanded={creditOpen}
                    aria-controls={`credit-panel-${identity.id}`}
                    // The identity is named in the ACCESSIBLE name only. Repeating it as visible
                    // text would put the same email in the row twice, which reads as noise beside
                    // the name two lines up and makes every getByText on it ambiguous.
                    aria-label={t(
                      creditOpen ? 'admin.credit.toggleHide' : 'admin.credit.toggleShow',
                      { name: identity.name },
                    )}
                    onClick={() => {
                      setOpenCredit(creditOpen ? '' : identity.id);
                    }}
                  >
                    {creditOpen ? (
                      <ChevronDown aria-hidden="true" />
                    ) : (
                      <ChevronRight aria-hidden="true" />
                    )}
                    {t('admin.credit.heading')}
                  </Button>
                  <div id={`credit-panel-${identity.id}`} hidden={!creditOpen} className="pt-3">
                    {creditOpen ? <CreditPanel identityId={identity.id} /> : null}
                  </div>
                </div>
              </div>
            </div>
          );
        })}
      </div>

      <ConfirmDialog
        open={pending !== undefined}
        onOpenChange={handleOpenChange}
        title={pending === undefined ? '' : t('admin.removal.title', { name: pending.name })}
        description={
          pending === undefined ? (
            ''
          ) : (
            <span className="break-all">{t('admin.removal.body', { name: pending.name })}</span>
          )
        }
        cancelLabel={t('admin.removal.cancel')}
        confirmLabel={t('admin.removal.confirm')}
        confirmVariant="destructive"
        confirmDisabled={!matches}
        confirmPending={dialogPendingHere}
        confirmIcon={dialogPendingHere ? <Spinner /> : undefined}
        onConfirm={confirmRemove}
      >
        {pending === undefined ? null : (
          <div className="flex flex-col gap-3">
            <div className="flex flex-col gap-2">
              <Label htmlFor="identity-removal-confirm" className="break-all">
                {t('admin.removal.confirmLabel', { email: pending.name })}
              </Label>
              <Input
                id="identity-removal-confirm"
                value={confirmText}
                autoComplete="off"
                onChange={(event) => {
                  setConfirmText(event.target.value);
                }}
                className="break-all font-mono text-[13px]"
              />
            </div>
            {dialogPendingHere ? (
              <div
                role="status"
                aria-busy="true"
                className="flex items-center gap-2 text-[13px] text-text-muted"
              >
                <Spinner />
                {t('admin.removal.inFlight', { name: pending.name })}
              </div>
            ) : null}
            {dialogErrorHere ? (
              <Alert variant="destructive">
                <AlertDescription>
                  {t('admin.removal.partialFailure', { name: pending.name })}
                </AlertDescription>
              </Alert>
            ) : null}
          </div>
        )}
      </ConfirmDialog>
    </AdminSection>
  );
}
