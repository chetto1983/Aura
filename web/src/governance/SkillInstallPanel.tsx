import { useId, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { ariaInvalid } from '../a11y/aria';
import { Spinner } from '../components/Spinner';
import { installSkill, searchSkillCatalog, type SkillsInstallInfo } from './governanceApi';

// SkillInstallPanel (SKW-01) — search the skills.sh catalog and install a skill in ONE step.
// Claude-Code parity (operator directive 2026-06-21): no approval ceremony, no "stage for
// approval" two-step, no RISKY framing. Catalog search is on by default; a search hit (or a
// pasted owner/repo, URL, or path in the source field) installs and activates immediately.
// The skill validation (injection blocklist + the write-boundary checks) still runs inside the
// container — intrinsic and invisible, not a gate the operator clicks through.

export interface SkillInstallPanelProps {
  readonly onClose: () => void;
}

export function SkillInstallPanel({ onClose }: SkillInstallPanelProps) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const headingId = useId();
  const sourceId = useId();
  const searchId = useId();

  const [source, setSource] = useState('');
  const [searchQuery, setSearchQuery] = useState('');
  const [emptyError, setEmptyError] = useState(false);

  // Catalog search runs as soon as the operator types — discovery is on by default (the
  // server's `enabled` flag only goes false when a deployment explicitly opts out).
  const catalog = useQuery({
    queryKey: ['governance', 'skills', 'catalog', searchQuery],
    queryFn: () => searchSkillCatalog(searchQuery),
    retry: false,
    enabled: searchQuery.trim() !== '',
  });

  const install = useMutation({
    mutationFn: (src: string) => installSkill(src),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['governance', 'skills'] });
    },
  });

  const installed: SkillsInstallInfo | undefined = install.data;

  function submit() {
    const trimmed = source.trim();
    if (trimmed === '') {
      setEmptyError(true);
      return;
    }
    setEmptyError(false);
    install.mutate(trimmed);
  }

  const catalogDisabled = catalog.data !== undefined && !catalog.data.enabled;
  const hits = catalog.data?.hits ?? [];

  return (
    <section
      aria-labelledby={headingId}
      className="flex h-full min-h-0 flex-col gap-4 overflow-y-auto p-4"
    >
      <header className="flex items-start justify-between gap-2">
        <h3 id={headingId} className="font-display text-[20px] font-semibold text-text">
          {t('governance.skills.install.heading')}
        </h3>
        <button
          type="button"
          onClick={onClose}
          aria-label={t('governance.closeAria')}
          className="min-h-[44px] min-w-[44px] rounded-md text-text-muted hover:text-text focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          ✕
        </button>
      </header>

      {/* Catalog search — primary discovery, on by default. */}
      <div className="flex flex-col gap-2">
        <label htmlFor={searchId} className="text-[13px] font-semibold text-text">
          {t('governance.skills.install.searchLabel')}
        </label>
        <input
          id={searchId}
          type="search"
          value={searchQuery}
          onChange={(event) => {
            setSearchQuery(event.target.value);
          }}
          placeholder={t('governance.skills.install.searchLabel')}
          className="w-full rounded-md border border-border bg-surface-3 px-3 py-2 text-[13px] text-text outline-none focus-visible:ring-2 focus-visible:ring-ring"
        />
        {catalogDisabled ? (
          <p role="note" className="text-[13px] text-text-muted">
            {t('governance.skills.install.externalOffNote')}
          </p>
        ) : hits.length > 0 ? (
          <ul className="flex flex-col gap-1">
            {hits.map((hit) => (
              <li key={hit.source}>
                <button
                  type="button"
                  onClick={() => {
                    setSource(hit.source);
                  }}
                  aria-pressed={source === hit.source}
                  className="w-full rounded-md border border-border bg-surface-2 px-3 py-2 text-left font-mono text-[13px] text-text outline-none hover:border-border-strong focus-visible:ring-2 focus-visible:ring-ring aria-pressed:border-accent"
                >
                  {hit.source}
                </button>
              </li>
            ))}
          </ul>
        ) : null}
      </div>

      {/* Source field — install directly from owner/repo, a URL, or a path. */}
      <div className="flex flex-col gap-1">
        <label htmlFor={sourceId} className="text-[13px] font-semibold text-text">
          {t('governance.skills.install.sourceLabel')}
        </label>
        <input
          id={sourceId}
          type="text"
          value={source}
          onChange={(event) => {
            setSource(event.target.value);
            if (event.target.value.trim() !== '') setEmptyError(false);
          }}
          placeholder={t('governance.skills.install.sourcePlaceholder')}
          aria-invalid={ariaInvalid(emptyError)}
          aria-describedby={emptyError ? `${sourceId}-err` : undefined}
          className="w-full rounded-md border border-border bg-surface-3 px-3 py-2 font-mono text-[13px] text-text outline-none focus-visible:ring-2 focus-visible:ring-ring"
        />
        {emptyError ? (
          <p id={`${sourceId}-err`} role="alert" className="text-[13px] text-danger">
            {t('governance.skills.install.emptySource')}
          </p>
        ) : null}
      </div>

      <p role="note" className="text-[13px] leading-relaxed text-text-muted">
        {t('governance.skills.install.containerNote')}
      </p>

      {/* After install — the active confirmation (source + active destination). */}
      {installed ? (
        <dl className="flex flex-col gap-2 rounded-md bg-surface-3 p-4">
          <InstalledField label={t('governance.skills.install.field.source')} value={installed.source} />
          <InstalledField
            label={t('governance.skills.install.field.destination')}
            value={installed.destination}
          />
          <p role="status" className="text-[13px] text-success">
            {t('governance.skills.install.staged')}
          </p>
        </dl>
      ) : null}

      {install.isError ? (
        <p role="alert" className="text-[13px] text-danger">
          {t('governance.error')}
        </p>
      ) : null}

      <div className="flex flex-wrap items-center gap-2">
        <button
          type="button"
          disabled={install.isPending || installed !== undefined}
          aria-busy={install.isPending}
          onClick={submit}
          className="inline-flex min-h-[44px] items-center justify-center gap-2 rounded-md bg-accent px-4 py-2 text-[13px] font-semibold text-on-accent outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-wait disabled:opacity-60"
        >
          {install.isPending ? <Spinner /> : null}
          {t('governance.skills.install.submit')}
        </button>
        <button
          type="button"
          onClick={onClose}
          className="min-h-[44px] rounded-md border border-border-strong bg-surface-2 px-4 py-2 text-[13px] font-semibold text-text outline-none hover:border-border-strong focus-visible:ring-2 focus-visible:ring-ring"
        >
          {t('governance.skills.install.discard')}
        </button>
      </div>
    </section>
  );
}

function InstalledField({ label, value }: { readonly label: string; readonly value: string }) {
  return (
    <div className="flex flex-col">
      <dt className="text-[13px] font-semibold text-text-muted">{label}</dt>
      <dd className="break-all font-mono text-[13px] text-text">{value}</dd>
    </div>
  );
}
