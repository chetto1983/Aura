import { useEffect, useId, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Search, X } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { ariaInvalid } from '../a11y/aria';
import { Spinner } from '../components/Spinner';
import { installSkill, searchSkillCatalog, type SkillsInstallInfo } from './governanceApi';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';

// SkillInstallPanel (SKW-01) — search the skills.sh catalog and install a skill in ONE step.
// Claude-Code parity (operator directive 2026-06-21): no approval ceremony, no "stage for
// approval" two-step, no RISKY framing. Catalog search is on by default; a search hit (or a
// pasted owner/repo, URL, or path in the source field) installs and activates immediately.
// The skill validation (injection blocklist + the write-boundary checks) still runs inside the
// container — intrinsic and invisible, not a gate the operator clicks through.

export interface SkillInstallPanelProps {
  readonly onClose: () => void;
}

const catalogSearchDelayMs = 250;

function useDebouncedValue(value: string, delayMs: number): string {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const timer = window.setTimeout(() => {
      setDebounced(value);
    }, delayMs);
    return () => {
      window.clearTimeout(timer);
    };
  }, [delayMs, value]);
  return debounced;
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

  const trimmedSearch = searchQuery.trim();
  const debouncedSearch = useDebouncedValue(trimmedSearch, catalogSearchDelayMs);
  const searchEligible = Array.from(trimmedSearch).length >= 2;
  const debouncedEligible = Array.from(debouncedSearch).length >= 2;
  const searchValueIsCurrent = trimmedSearch === debouncedSearch;
  const searchIsDebouncing = searchEligible && !searchValueIsCurrent;

  // The deployment opt-out stays server-authoritative after the local request discipline.
  const catalog = useQuery({
    queryKey: ['governance', 'skills', 'catalog', debouncedSearch],
    queryFn: ({ signal }) => searchSkillCatalog(debouncedSearch, signal),
    retry: false,
    enabled: debouncedEligible,
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

  const catalogIsCurrent = searchValueIsCurrent && catalog.data?.query === debouncedSearch;
  const catalogDisabled = catalogIsCurrent && catalog.data !== undefined && !catalog.data.enabled;
  const hits = catalogIsCurrent ? (catalog.data?.hits ?? []) : [];
  const showCatalogLoading = searchEligible && (searchIsDebouncing || catalog.isFetching);
  const showCatalogEmpty =
    catalogIsCurrent && catalog.isSuccess && catalog.data.enabled && hits.length === 0;

  return (
    <section
      aria-labelledby={headingId}
      className="flex h-full min-h-0 flex-col gap-4 overflow-y-auto p-4"
    >
      <header className="flex items-start justify-between gap-2">
        <h3 id={headingId} className="font-display text-[20px] font-semibold text-text">
          {t('governance.skills.install.heading')}
        </h3>
        <Button
          type="button"
          variant="ghost"
          size="icon"
          onClick={onClose}
          aria-label={t('governance.closeAria')}
          className="text-text-muted hover:text-text"
        >
          <X data-icon aria-hidden="true" className="size-4" />
        </Button>
      </header>

      {/* Catalog search — primary discovery, on by default. */}
      <div className="flex flex-col gap-2">
        <Label htmlFor={searchId} className="text-[13px] font-semibold text-text">
          {t('governance.skills.install.searchLabel')}
        </Label>
        <div className="relative">
          <Search
            data-icon
            aria-hidden="true"
            className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-text-faint"
          />
          <Input
            id={searchId}
            type="search"
            value={searchQuery}
            onChange={(event) => {
              setSearchQuery(event.target.value);
            }}
            placeholder={t('governance.skills.install.searchLabel')}
            className="pl-9 text-[13px]"
          />
        </div>
        {trimmedSearch.length === 1 ? (
          <p role="note" className="text-[13px] text-text-muted">
            {t('governance.skills.install.searchMinChars')}
          </p>
        ) : showCatalogLoading ? (
          <p role="status" className="flex items-center gap-2 text-[13px] text-text-muted">
            <Spinner />
            {t('governance.skills.install.searching')}
          </p>
        ) : catalog.isError && searchValueIsCurrent ? (
          <Alert variant="destructive">
            <AlertDescription>{t('governance.skills.install.searchError')}</AlertDescription>
          </Alert>
        ) : catalogDisabled ? (
          <p role="note" className="text-[13px] text-text-muted">
            {t('governance.skills.install.externalOffNote')}
          </p>
        ) : hits.length > 0 ? (
          <ul className="flex flex-col gap-1">
            {hits.map((hit) => {
              // The installable spec the `npx skills` CLI expects is `owner/repo@skill`; the
              // catalog parser splits that into source + skill, so re-join it for the install.
              const spec = hit.skill ? `${hit.source}@${hit.skill}` : hit.source;
              return (
                <li key={spec}>
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() => {
                      setSource(spec);
                    }}
                    aria-pressed={source === spec}
                    className="h-auto min-h-11 w-full justify-between whitespace-normal bg-surface-2 text-left font-mono text-[13px] aria-pressed:border-accent"
                  >
                    <span className="truncate">{spec}</span>
                    {hit.installs ? (
                      <span className="shrink-0 tabular-nums text-text-muted">{hit.installs}</span>
                    ) : null}
                  </Button>
                </li>
              );
            })}
          </ul>
        ) : showCatalogEmpty ? (
          <p role="note" className="text-[13px] text-text-muted">
            {t('governance.skills.install.searchEmpty')}
          </p>
        ) : null}
      </div>

      {/* Source field — install directly from owner/repo, a URL, or a path. */}
      <div className="flex flex-col gap-1">
        <Label htmlFor={sourceId} className="text-[13px] font-semibold text-text">
          {t('governance.skills.install.sourceLabel')}
        </Label>
        <Input
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
          className="font-mono text-[13px]"
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
        <Card className="gap-2 bg-surface-3 p-4">
          <dl className="flex flex-col gap-2">
            <InstalledField
              label={t('governance.skills.install.field.source')}
              value={installed.source}
            />
            <InstalledField
              label={t('governance.skills.install.field.destination')}
              value={installed.destination}
            />
          </dl>
          <p role="status" className="text-[13px] text-success">
            {t('governance.skills.install.staged')}
          </p>
        </Card>
      ) : null}

      {install.isError ? (
        <Alert variant="destructive">
          <AlertDescription>{t('governance.error')}</AlertDescription>
        </Alert>
      ) : null}

      <div className="flex flex-wrap items-center gap-2">
        <Button
          type="button"
          disabled={install.isPending || installed !== undefined}
          aria-busy={install.isPending}
          onClick={submit}
        >
          {install.isPending ? <Spinner /> : null}
          {t('governance.skills.install.submit')}
        </Button>
        <Button type="button" variant="outline" onClick={onClose}>
          {t('governance.skills.install.discard')}
        </Button>
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
