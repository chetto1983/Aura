import { useMemo, useState } from 'react';
import { ChevronLeft, ChevronRight, RefreshCw } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import type { AuditEvent } from '../admin/adminApi';
import { AdminSection } from '../admin/AdminSection';
import { useAdminIdentities, useAudit } from '../admin/useAdmin';
import { Spinner } from '../components/Spinner';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Label } from '@/components/ui/label';

const PAGE_SIZE = 25;

type SourceVariant = 'default' | 'secondary' | 'success';

function sourceVariant(source: string): SourceVariant {
  if (source === 'mcp') return 'default';
  if (source === 'skill') return 'success';
  return 'secondary';
}

function formatTimestamp(iso: string): string {
  const date = new Date(iso);
  return Number.isNaN(date.getTime()) ? iso : date.toLocaleString();
}

// AdminAuditView is the D-28 admin per-user activity view: pick an identity, page through its
// MCP/skill/tool activity newest-first. The read is governance.write-gated server-side; this
// view is only mounted inside the admin-gated Settings page.
export function AdminAuditView() {
  const { t } = useTranslation();
  const identitiesQuery = useAdminIdentities();
  const [selectedId, setSelectedId] = useState('');
  const [offset, setOffset] = useState(0);

  const identities = identitiesQuery.data ?? [];
  const activeId = selectedId !== '' ? selectedId : (identities[0]?.id ?? '');
  const auditQuery = useAudit(activeId, PAGE_SIZE, offset);
  const events = useMemo<readonly AuditEvent[]>(
    () => auditQuery.data?.events ?? [],
    [auditQuery.data],
  );
  const hasNextPage = events.length === PAGE_SIZE;

  return (
    <AdminSection
      labelId="admin-audit"
      keyPrefix="admin.audit"
      identitiesQuery={identitiesQuery}
      identities={identities}
    >
      <div className="flex flex-col gap-4 rounded-lg border border-border bg-surface p-4">
        <div className="flex flex-wrap items-end justify-between gap-3">
          <div className="flex min-w-0 flex-col gap-2">
            <Label htmlFor="admin-audit-identity">{t('admin.identity.label')}</Label>
            <select
              id="admin-audit-identity"
              value={activeId}
              onChange={(event) => {
                setSelectedId(event.target.value);
                setOffset(0); // a new identity starts at page one
              }}
              className="min-h-[44px] rounded-md border border-input bg-surface-3 px-3 text-[14px] text-text focus-visible:outline-2 focus-visible:outline-accent"
            >
              {identities.map((identity) => (
                <option key={identity.id} value={identity.id}>
                  {identity.name}
                </option>
              ))}
            </select>
          </div>
          <Button
            type="button"
            variant="outline"
            disabled={auditQuery.isFetching}
            onClick={() => void auditQuery.refetch()}
          >
            <RefreshCw aria-hidden="true" />
            {t('admin.audit.refresh')}
          </Button>
        </div>

        {auditQuery.isLoading ? (
          <div role="status" className="flex items-center gap-2 text-sm text-text-muted">
            <Spinner />
            {t('admin.audit.loading')}
          </div>
        ) : auditQuery.isError ? (
          <Alert variant="destructive">
            <AlertDescription>{t('admin.audit.error')}</AlertDescription>
          </Alert>
        ) : events.length === 0 ? (
          <p className="text-sm text-text-muted">{t('admin.audit.empty')}</p>
        ) : (
          <ul className="flex flex-col divide-y divide-border">
            {events.map((event, index) => (
              <li
                key={`${event.source}-${event.created_at}-${String(index)}`}
                className="flex flex-wrap items-center gap-x-3 gap-y-1 py-2.5"
              >
                <Badge variant={sourceVariant(event.source)}>
                  {t(`admin.audit.source.${event.source}`, event.source)}
                </Badge>
                <span className="font-mono text-[13px] text-text">{event.action}</span>
                <span className="min-w-0 truncate text-[13px] text-text-muted">{event.target}</span>
                {event.detail ? (
                  <span className="min-w-0 truncate text-[12px] text-text-faint">
                    {event.detail}
                  </span>
                ) : null}
                <time className="ml-auto shrink-0 text-[12px] text-text-faint">
                  {formatTimestamp(event.created_at)}
                </time>
              </li>
            ))}
          </ul>
        )}

        <div className="flex items-center justify-between gap-2 border-t border-border pt-3">
          <Button
            type="button"
            variant="ghost"
            size="sm"
            disabled={offset === 0 || auditQuery.isFetching}
            onClick={() => {
              setOffset((value) => Math.max(0, value - PAGE_SIZE));
            }}
          >
            <ChevronLeft aria-hidden="true" />
            {t('admin.audit.prev', 'Previous')}
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            disabled={!hasNextPage || auditQuery.isFetching}
            onClick={() => {
              setOffset((value) => value + PAGE_SIZE);
            }}
          >
            {t('admin.audit.next', 'Next')}
            <ChevronRight aria-hidden="true" />
          </Button>
        </div>
      </div>
    </AdminSection>
  );
}
