import { useId, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { ariaInvalid } from '../a11y/aria';
import {
  installSkill,
  searchSkillCatalog,
  type SkillsCheckItem,
  type SkillsInstallInfo,
} from './governanceApi';

// SkillInstallPanel (SKW-01) — the RISKY-HONEST install panel in the Skills detail slot. The
// install is ALWAYS rendered RISKY supply-chain input (never "safe"): a prominent RISKY badge,
// the FIVE validation-checklist items (post-D-09: NO --ignore-scripts), and a neutral
// container-isolation note. A source field (owner/repo, URL, path) OR a skills.sh search behind
// the `External discovery (skills.sh)` toggle (OFF by default, disabled with a note when off,
// reflecting AURA_SKILLS_EXTERNAL_DISCOVERY). The submit is `Stage for approval` (NEVER
// Install/Activate) — it stages to pending + mints the operator approval pause (D-13);
// activation is the operator resume only. Abandon = Discard install.

// The FIVE checklist item labels (the contract — rendered BEFORE staging; after staging the
// resolved pass/fail from the install response replaces them). No --ignore-scripts item (D-09).
const CHECKLIST_KEYS: readonly string[] = [
  'sanitizedEnv',
  'skillMdParse',
  'bodyCap',
  'injectionBlocklist',
  'sanitizedNamePath',
];

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
  const [externalDiscovery, setExternalDiscovery] = useState(false);
  const [searchQuery, setSearchQuery] = useState('');
  const [emptyError, setEmptyError] = useState(false);

  // The catalog search is flag-gated: it only runs when the operator turns on external
  // discovery AND has typed a non-empty query. The server is the authority — its result's
  // `enabled` flag reflects AURA_SKILLS_EXTERNAL_DISCOVERY (the UI toggle is the operator gate).
  const catalog = useQuery({
    queryKey: ['governance', 'skills', 'catalog', searchQuery],
    queryFn: () => searchSkillCatalog(searchQuery),
    retry: false,
    enabled: externalDiscovery && searchQuery.trim() !== '',
  });

  const install = useMutation({
    mutationFn: (src: string) => installSkill(src),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['governance', 'skills'] });
      void queryClient.invalidateQueries({ queryKey: ['approvals'] });
    },
  });

  const staged: SkillsInstallInfo | undefined = install.data;

  function submit() {
    const trimmed = source.trim();
    if (trimmed === '') {
      setEmptyError(true);
      return;
    }
    setEmptyError(false);
    install.mutate(trimmed);
  }

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

      {/* RISKY badge — install is ALWAYS RISKY supply-chain input, never "safe". */}
      <div
        role="note"
        className="flex flex-col gap-1 rounded-md border border-warning bg-warning/15 px-3 py-2"
      >
        <span className="flex items-center gap-1.5 text-[13px] font-semibold text-warning">
          <span
            aria-hidden="true"
            className="inline-block h-2 w-2 shrink-0 rounded-sm bg-warning"
          />
          {t('governance.skills.install.riskBadge')}
        </span>
        <span className="text-[15.5px] leading-relaxed text-text">
          {t('governance.skills.install.riskBanner')}
        </span>
      </div>

      {/* Source field. */}
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

      {/* External-discovery toggle + flag-gated search. */}
      <div className="flex flex-col gap-2">
        <button
          type="button"
          aria-pressed={externalDiscovery}
          onClick={() => {
            setExternalDiscovery((prev) => !prev);
          }}
          className="flex min-h-[44px] items-center gap-2 self-start rounded-md border border-border-strong bg-surface-2 px-3 py-2 text-[13px] font-semibold text-text outline-none hover:border-border-strong focus-visible:ring-2 focus-visible:ring-ring"
        >
          <span
            aria-hidden="true"
            className={`inline-block h-2 w-2 shrink-0 rounded-sm ${
              externalDiscovery ? 'bg-success' : 'bg-text-muted'
            }`}
          />
          {t('governance.skills.install.externalToggle')}
        </button>
        <label htmlFor={searchId} className="sr-only">
          {t('governance.skills.install.searchLabel')}
        </label>
        <input
          id={searchId}
          type="search"
          value={searchQuery}
          disabled={!externalDiscovery}
          onChange={(event) => {
            setSearchQuery(event.target.value);
          }}
          placeholder={t('governance.skills.install.searchLabel')}
          className="w-full rounded-md border border-border bg-surface-3 px-3 py-2 text-[13px] text-text outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-60"
        />
        {!externalDiscovery ? (
          <p role="note" className="text-[13px] text-text-muted">
            {t('governance.skills.install.externalOffNote')}
          </p>
        ) : catalog.data?.enabled ? (
          <ul className="flex flex-col gap-1">
            {catalog.data.hits.map((hit) => (
              <li key={hit.source}>
                <button
                  type="button"
                  onClick={() => {
                    setSource(hit.source);
                  }}
                  className="w-full rounded-md border border-border bg-surface-2 px-3 py-2 text-left font-mono text-[13px] text-text outline-none hover:border-border-strong focus-visible:ring-2 focus-visible:ring-ring"
                >
                  {hit.source}
                </button>
              </li>
            ))}
          </ul>
        ) : null}
      </div>

      {/* The FIVE-item validation checklist (always rendered — the contract). After staging the
          resolved pass/fail from the install response replaces the static labels. */}
      <section className="flex flex-col gap-1">
        <h4 className="text-[13px] font-semibold uppercase tracking-wide text-text-muted">
          {t('governance.skills.install.checklistHeading')}
        </h4>
        <ul className="flex flex-col gap-1">
          {(staged?.checklist ?? CHECKLIST_KEYS.map((key) => ({ label: key, passed: false }))).map(
            (item: SkillsCheckItem, index) => {
              const label = staged
                ? item.label
                : t(`governance.skills.install.checklist.${CHECKLIST_KEYS[index] ?? item.label}`);
              return (
                <li
                  key={staged ? item.label : (CHECKLIST_KEYS[index] ?? String(index))}
                  className={`flex items-center gap-2 text-[13px] ${
                    staged ? (item.passed ? 'text-success' : 'text-danger') : 'text-text-muted'
                  }`}
                >
                  <span aria-hidden="true">{staged ? (item.passed ? '✓' : '✗') : '•'}</span>
                  <span className="break-words">{label}</span>
                </li>
              );
            },
          )}
        </ul>
        <p role="note" className="text-[13px] leading-relaxed text-text-muted">
          {t('governance.skills.install.containerNote')}
        </p>
      </section>

      {/* After staging — the pre-activation surface (source/hash/preview/destination + queued). */}
      {staged ? (
        <dl className="flex flex-col gap-2 rounded-md bg-surface-3 p-4">
          <StagedField label={t('governance.skills.install.field.source')} value={staged.source} />
          <StagedField
            label={t('governance.skills.install.field.hash')}
            value={staged.content_hash}
          />
          <StagedField
            label={t('governance.skills.install.field.destination')}
            value={staged.destination}
          />
          <div className="flex flex-col">
            <dt className="text-[13px] font-semibold text-text-muted">
              {t('governance.skills.install.field.preview')}
            </dt>
            <dd className="max-h-40 overflow-y-auto whitespace-pre-wrap break-words font-mono text-[13px] text-text">
              {staged.preview}
            </dd>
          </div>
          <p role="status" className="text-[13px] text-success">
            {t('governance.skills.install.staged')}
          </p>
        </dl>
      ) : null}

      {install.isError ? (
        <p role="alert" className="text-[13px] text-danger">
          {t('governance.skills.install.emptySource')}
        </p>
      ) : null}

      <div className="flex flex-wrap items-center gap-2">
        <button
          type="button"
          disabled={install.isPending || staged !== undefined}
          onClick={submit}
          className="min-h-[44px] rounded-md bg-accent px-4 py-2 text-[13px] font-semibold text-on-accent outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-60"
        >
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

function StagedField({ label, value }: { readonly label: string; readonly value: string }) {
  return (
    <div className="flex flex-col">
      <dt className="text-[13px] font-semibold text-text-muted">{label}</dt>
      <dd className="break-all font-mono text-[13px] text-text">{value}</dd>
    </div>
  );
}
