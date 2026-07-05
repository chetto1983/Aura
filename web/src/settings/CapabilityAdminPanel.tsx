import { useMemo, useState } from 'react';
import { KeyRound, Plus, X } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { WILDCARD } from '../admin/adminApi';
import {
  useAdminIdentities,
  useCapabilities,
  useGrantCapability,
  useRevokeCapability,
} from '../admin/useAdmin';
import { Spinner } from '../components/Spinner';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';

// CapabilityAdminPanel is the D-26 admin control: pick an identity, see its grants, and
// grant/revoke a capability through the validated identity.Store seam. The routes are
// governance.write-gated server-side; this control is only ever mounted inside the
// admin-gated Settings page.
export function CapabilityAdminPanel() {
  const { t } = useTranslation();
  const identitiesQuery = useAdminIdentities();
  const { identityId: selfId } = useCapabilities();
  const grant = useGrantCapability();
  const revoke = useRevokeCapability();
  const [selectedId, setSelectedId] = useState('');
  const [draft, setDraft] = useState('');

  const identities = useMemo(() => identitiesQuery.data ?? [], [identitiesQuery.data]);
  const selected = useMemo(
    () => identities.find((identity) => identity.id === selectedId),
    [identities, selectedId],
  );

  const activeId = selected?.id ?? identities[0]?.id ?? '';
  const active = selected ?? identities[0];
  const mutating = grant.isPending || revoke.isPending;

  function submitGrant() {
    const capability = draft.trim();
    if (capability === '' || activeId === '' || mutating) return;
    grant.mutate(
      { identityId: activeId, capability },
      {
        onSuccess: () => {
          setDraft('');
        },
      },
    );
  }

  return (
    <section aria-labelledby="admin-access" className="flex flex-col gap-4">
      <div className="flex flex-col gap-2">
        <p className="text-xs font-semibold uppercase tracking-wide text-accent-text">
          {t('admin.access.kicker')}
        </p>
        <h2 id="admin-access" className="text-[20px] font-semibold text-text">
          {t('admin.access.heading')}
        </h2>
        <p className="max-w-3xl text-[15.5px] leading-relaxed text-text-muted">
          {t('admin.access.body')}
        </p>
      </div>

      {identitiesQuery.isLoading ? (
        <div role="status" className="flex items-center gap-2 text-sm text-text-muted">
          <Spinner />
          {t('admin.identity.loading')}
        </div>
      ) : identitiesQuery.isError ? (
        <Alert variant="destructive">
          <AlertDescription>{t('admin.identity.error')}</AlertDescription>
        </Alert>
      ) : identities.length === 0 ? (
        <p className="text-sm text-text-muted">{t('admin.identity.empty')}</p>
      ) : (
        <div className="flex flex-col gap-4 rounded-lg border border-border bg-surface p-4">
          <div className="flex flex-col gap-2">
            <Label htmlFor="admin-access-identity">{t('admin.identity.label')}</Label>
            <select
              id="admin-access-identity"
              value={activeId}
              onChange={(event) => {
                setSelectedId(event.target.value);
              }}
              className="min-h-[44px] rounded-md border border-input bg-surface-3 px-3 text-[14px] text-text focus-visible:outline-2 focus-visible:outline-accent"
            >
              {identities.map((identity) => (
                <option key={identity.id} value={identity.id}>
                  {identity.name}
                  {identity.id === selfId ? ` (${t('admin.identity.you')})` : ''}
                </option>
              ))}
            </select>
          </div>

          <div className="flex flex-col gap-2">
            <p className="text-[13px] font-semibold text-text">{t('admin.access.capabilities')}</p>
            {active && active.capabilities.length > 0 ? (
              <ul className="flex flex-wrap gap-2">
                {active.capabilities.map((capability) => (
                  <li key={capability}>
                    {capability === WILDCARD ? (
                      <Badge variant="secondary" className="gap-1">
                        <KeyRound aria-hidden="true" className="size-3" />
                        {t('admin.access.wildcard')}
                      </Badge>
                    ) : (
                      <Badge variant="success" className="gap-1 pr-1">
                        {capability}
                        <button
                          type="button"
                          aria-label={t('admin.access.revoke', { capability })}
                          disabled={mutating}
                          onClick={() => {
                            revoke.mutate({ identityId: activeId, capability });
                          }}
                          className="rounded-full p-0.5 hover:bg-surface-2 focus-visible:outline-2 focus-visible:outline-accent disabled:opacity-50"
                        >
                          <X aria-hidden="true" className="size-3" />
                        </button>
                      </Badge>
                    )}
                  </li>
                ))}
              </ul>
            ) : (
              <p className="text-sm text-text-muted">{t('admin.access.none')}</p>
            )}
          </div>

          <div className="flex flex-wrap items-end gap-2 border-t border-border pt-4">
            <div className="flex min-w-0 flex-1 flex-col gap-2">
              <Label htmlFor="admin-access-grant">{t('admin.access.grant')}</Label>
              <Input
                id="admin-access-grant"
                value={draft}
                placeholder={t('admin.access.grantPlaceholder')}
                className="font-mono text-[13px]"
                onChange={(event) => {
                  setDraft(event.target.value);
                }}
                onKeyDown={(event) => {
                  if (event.key === 'Enter') {
                    event.preventDefault();
                    submitGrant();
                  }
                }}
              />
            </div>
            <Button
              type="button"
              disabled={mutating || draft.trim() === ''}
              aria-busy={grant.isPending}
              onClick={submitGrant}
            >
              {grant.isPending ? <Spinner /> : <Plus aria-hidden="true" />}
              {t('admin.access.grant')}
            </Button>
          </div>

          {grant.isError ? (
            <Alert variant="destructive">
              <AlertDescription>{t('admin.access.grantError')}</AlertDescription>
            </Alert>
          ) : null}
          {revoke.isError ? (
            <Alert variant="destructive">
              <AlertDescription>{t('admin.access.revokeError')}</AlertDescription>
            </Alert>
          ) : null}
        </div>
      )}
    </section>
  );
}
