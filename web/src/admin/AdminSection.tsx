// AdminSection is the shared MUSR-01 admin-surface chrome: the kicker/heading/body section
// header plus the identities-roster loading/error/empty guard. AdminAuditView (D-28) and
// CapabilityAdminPanel (D-26) rendered byte-identical header + guard markup, so it lives here
// once. Each surface passes its own aria label id + i18n key prefix and supplies its distinct
// loaded UI as children; the shared markup keeps the same DOM, aria semantics, and i18n keys.

import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { Spinner } from '../components/Spinner';
import type { AdminIdentity } from './adminApi';
import { Alert, AlertDescription } from '@/components/ui/alert';

interface AdminSectionProps {
  /** Shared by the section's aria-labelledby and the heading's id. */
  labelId: string;
  /** i18n key prefix resolving `${keyPrefix}.kicker|heading|body` (e.g. 'admin.audit'). */
  keyPrefix: string;
  /** The roster query — only its loading/error flags gate the content. */
  identitiesQuery: { readonly isLoading: boolean; readonly isError: boolean };
  /** The loaded roster; an empty array renders the shared empty state. */
  identities: readonly AdminIdentity[];
  /** The surface's distinct loaded UI, shown once the roster resolves non-empty. */
  children: ReactNode;
}

export function AdminSection({
  labelId,
  keyPrefix,
  identitiesQuery,
  identities,
  children,
}: AdminSectionProps) {
  const { t } = useTranslation();

  return (
    <section aria-labelledby={labelId} className="flex flex-col gap-4">
      <div className="flex flex-col gap-2">
        <p className="text-xs font-semibold uppercase tracking-wide text-accent-text">
          {t(`${keyPrefix}.kicker`)}
        </p>
        <h2 id={labelId} className="text-[20px] font-semibold text-text">
          {t(`${keyPrefix}.heading`)}
        </h2>
        <p className="max-w-3xl text-[15.5px] leading-relaxed text-text-muted">
          {t(`${keyPrefix}.body`)}
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
        children
      )}
    </section>
  );
}
