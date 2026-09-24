import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import {
  EMBEDDING_SPACE_QUERY_KEY,
  fetchEmbeddingSpace,
  type FamilyState,
  type TenantSpaceReport,
} from './embeddingSpaceApi';
import { Badge } from '@/components/ui/badge';

function FamilyTable({ family }: { readonly family: FamilyState }) {
  const { t, i18n } = useTranslation();
  const number = (value: number) => value.toLocaleString(i18n.language);
  const headingId = `embedding-family-${family.family}-${family.space}`;
  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center gap-2">
        <h5 id={headingId} className="text-[13px] font-semibold text-text">
          {t(`embeddingRoute.space.families.${family.family}`, { defaultValue: family.family })}
        </h5>
        <Badge variant={family.open ? 'success' : 'warning'}>
          {t(family.open ? 'embeddingRoute.space.dense' : 'embeddingRoute.space.lexical')}
        </Badge>
      </div>
      <div className="overflow-x-auto">
        <table aria-labelledby={headingId} className="w-full min-w-[28rem] text-left text-[13px]">
          <thead className="text-text-faint">
            <tr>
              <th scope="col" className="py-1 pr-3 font-normal">
                {t('embeddingRoute.space.columns.type')}
              </th>
              <th scope="col" className="py-1 pr-3 text-right font-normal">
                {t('embeddingRoute.space.columns.inSpace')}
              </th>
              <th scope="col" className="py-1 pr-3 text-right font-normal">
                {t('embeddingRoute.space.columns.otherSpace')}
              </th>
              <th scope="col" className="py-1 pr-3 text-right font-normal">
                {t('embeddingRoute.space.columns.noVector')}
              </th>
              <th scope="col" className="py-1 text-right font-normal">
                {t('embeddingRoute.space.columns.rejected')}
              </th>
            </tr>
          </thead>
          <tbody className="tabular-nums text-text">
            {family.types.map((tally) => (
              <tr key={tally.type} className="border-t border-border">
                <th scope="row" className="py-1 pr-3 font-normal">
                  {t(`embeddingRoute.space.types.${tally.type}`, { defaultValue: tally.type })}
                </th>
                <td className="py-1 pr-3 text-right">{number(tally.in_space)}</td>
                <td
                  className={
                    tally.other_space > 0
                      ? 'py-1 pr-3 text-right text-warning'
                      : 'py-1 pr-3 text-right'
                  }
                >
                  {number(tally.other_space)}
                </td>
                <td className="py-1 pr-3 text-right">{number(tally.no_vector)}</td>
                <td
                  className={tally.rejected > 0 ? 'py-1 text-right text-danger' : 'py-1 text-right'}
                >
                  {number(tally.rejected)}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function TenantReport({ report }: { readonly report: TenantSpaceReport }) {
  const { t } = useTranslation();
  return (
    <article className="flex flex-col gap-3 rounded-md border border-border bg-surface p-3">
      <h4 className="font-mono text-[12px] text-text-faint">
        {t('embeddingRoute.space.tenant', { id: report.identity_id })}
      </h4>
      {report.families.map((family) => (
        <FamilyTable key={family.family} family={family} />
      ))}
      {report.stuck_documents.length > 0 ? (
        <div className="flex flex-col gap-1 text-[13px]">
          <p className="font-semibold text-text">{t('embeddingRoute.space.stuck')}</p>
          <ul className="flex flex-col gap-0.5">
            {report.stuck_documents.map((doc) => (
              <li key={doc.source_key} className="break-all text-text-muted">
                {doc.file_name}
              </li>
            ))}
          </ul>
        </div>
      ) : null}
      {report.ingest_errors > 0 ? (
        <p className="text-[13px] text-danger">
          {t('embeddingRoute.space.ingestErrors', { count: report.ingest_errors })}
        </p>
      ) : null}
    </article>
  );
}

// EmbeddingSpacePanel shows, per identity and family, where every stored vector stands against
// the space Aura reads in (spec §10): a family with any vector elsewhere is keyword-only until
// it is re-embedded, and a document that will not re-index is named here.
export function EmbeddingSpacePanel() {
  const { t } = useTranslation();
  const query = useQuery({ queryKey: EMBEDDING_SPACE_QUERY_KEY, queryFn: fetchEmbeddingSpace });

  let body;
  if (query.isPending) {
    body = (
      <p role="status" className="text-[13px] text-text-muted">
        {t('embeddingRoute.space.loading')}
      </p>
    );
  } else if (query.isError) {
    body = (
      <p role="alert" className="text-[13px] text-danger">
        {t('embeddingRoute.space.error')}
      </p>
    );
  } else if (query.data.space_error !== undefined && query.data.space_error !== '') {
    body = (
      <p role="alert" className="text-[13px] text-danger">
        {t('embeddingRoute.space.spaceError', { message: query.data.space_error })}
      </p>
    );
  } else {
    const tenants = query.data.tenants ?? [];
    body = (
      <>
        <p className="text-[13px] text-text">
          <span className="text-text-faint">{t('embeddingRoute.space.current')}: </span>
          <span className="font-mono text-[12px]">{query.data.space}</span> ·{' '}
          {query.data.space_label}
        </p>
        {query.data.floors_calibrated ? null : (
          <p className="text-[13px] text-warning">{t('embeddingRoute.space.uncalibrated')}</p>
        )}
        {tenants.length === 0 ? (
          <p className="text-[13px] text-text-muted">{t('embeddingRoute.space.noTenants')}</p>
        ) : (
          tenants.map((report) => <TenantReport key={report.identity_id} report={report} />)
        )}
      </>
    );
  }

  return (
    <section
      aria-labelledby="embedding-space-heading"
      className="flex max-w-3xl flex-col gap-3 rounded-[var(--radius-md)] border border-border bg-surface-2 p-5"
    >
      <div className="flex flex-col gap-1">
        <h3 id="embedding-space-heading" className="text-[15px] font-semibold text-text">
          {t('embeddingRoute.space.heading')}
        </h3>
        <p className="max-w-2xl text-[13px] leading-relaxed text-text-muted">
          {t('embeddingRoute.space.body')}
        </p>
      </div>
      {body}
    </section>
  );
}
