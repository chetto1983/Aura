import { useMemo, useState } from 'react';
import { ChevronLeft, ChevronRight, RefreshCw } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import type { AuditEvent } from '../admin/adminApi';
import { AdminSection } from '../admin/AdminSection';
import { useAdminIdentities, useAudit } from '../admin/useAdmin';
import { formatElapsed } from '../chat/durationFormat';
import { Spinner } from '../components/Spinner';
import { pairAuditEvents, type AuditRow, type InvocationOutcome } from './auditPairing';
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

/** Outcome chip tints stay on the accepted blue-palette tokens. */
const OUTCOME_CHIP_CLASS: Record<InvocationOutcome, string> = {
  ok: 'bg-success/15 text-success',
  error: 'bg-danger/15 text-danger',
  running: 'bg-warning/15 text-warning',
};

// AdminAuditView is the D-28 admin per-user activity view: pick an identity, page through its
// MCP/skill/tool activity newest-first. The read is governance.write-gated server-side; this
// view is only mounted inside the admin-gated Governance surface (its Audit tab).
export function AdminAuditView() {
  const { t, i18n } = useTranslation();
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
  // Tool start/end pairs collapse into one compact invocation row (operator
  // directive — a single turn was flooding the feed with two rows per tool).
  const rows = useMemo<readonly AuditRow[]>(() => pairAuditEvents(events), [events]);
  const hasNextPage = events.length === PAGE_SIZE;
  const language = i18n.resolvedLanguage ?? i18n.language;

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
            {rows.map((row, index) =>
              row.kind === 'invocation' ? (
                <li
                  key={`tool-${row.at}-${String(index)}`}
                  data-testid="audit-invocation"
                  className="flex flex-wrap items-center gap-x-3 gap-y-1 py-2.5"
                >
                  <Badge variant={sourceVariant('tool')}>
                    {t('admin.audit.source.tool', 'tool')}
                  </Badge>
                  <span className="font-mono text-[13px] text-text">{row.toolName}</span>
                  <span
                    data-testid="audit-outcome"
                    className={`rounded-[var(--radius-sm)] px-1.5 py-0.5 text-[11px] font-medium ${OUTCOME_CHIP_CLASS[row.outcome]}`}
                  >
                    {t(`admin.audit.outcome.${row.outcome}`)}
                  </span>
                  {row.detail !== undefined ? (
                    <span className="min-w-0 truncate text-[12px] text-text-faint">
                      {row.detail}
                    </span>
                  ) : null}
                  {row.durationMs !== undefined ? (
                    <span
                      data-testid="audit-duration"
                      className="font-mono text-[12px] tabular-nums text-text-faint"
                    >
                      {formatElapsed(row.durationMs, language, t)}
                    </span>
                  ) : null}
                  <time className="ml-auto shrink-0 text-[12px] text-text-faint">
                    {formatTimestamp(row.at)}
                  </time>
                </li>
              ) : (
                <li
                  key={`${row.event.source}-${row.event.created_at}-${String(index)}`}
                  className="flex flex-wrap items-center gap-x-3 gap-y-1 py-2.5"
                >
                  <Badge variant={sourceVariant(row.event.source)}>
                    {t(`admin.audit.source.${row.event.source}`, row.event.source)}
                  </Badge>
                  <span className="font-mono text-[13px] text-text">{row.event.action}</span>
                  <span className="min-w-0 truncate text-[13px] text-text-muted">
                    {row.event.target}
                  </span>
                  {row.event.detail ? (
                    <span className="min-w-0 truncate text-[12px] text-text-faint">
                      {row.event.detail}
                    </span>
                  ) : null}
                  <time className="ml-auto shrink-0 text-[12px] text-text-faint">
                    {formatTimestamp(row.event.created_at)}
                  </time>
                </li>
              ),
            )}
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
