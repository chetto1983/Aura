import { useId, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { BoardLayout } from './BoardLayout';
import { BoardStateView, boardStatus } from './governanceView';
import { SkillDetail } from './SkillDetail';
import { SkillInstallPanel } from './SkillInstallPanel';
import {
  archiveSkill,
  fetchSkills,
  fetchSkillsAudit,
  restoreSkill,
  type AuditRow,
  type SkillRow,
  type SkillStage,
} from './governanceApi';

// SkillsBoard (GOV-02) — the skills lifecycle board. Four sub-tabs as a role="tablist":
// active / pending / archived (each a master-list + read-only detail), and audit (the append-only
// ledger, newest-first). The lifecycle stages fetch via fetchSkills(stage); the audit tab fetches
// fetchSkillsAudit. CRITICALLY a PENDING row renders with NO run/activate/install affordance — it
// is selectable only to VIEW its read-only detail (T-28-03-02 / GOV-02): there is no action
// control anywhere on the board. Backend strings render as React-escaped text; data-shaped values
// (content hash) are mono.

type SkillsTab = 'active' | 'pending' | 'archived' | 'audit';

const SKILL_TABS: readonly SkillsTab[] = ['active', 'pending', 'archived', 'audit'];

const LIFECYCLE_STAGE: Record<Exclude<SkillsTab, 'audit'>, SkillStage> = {
  active: 'active',
  pending: 'pending',
  archived: 'archived',
};

/** AuditList renders the append-only ledger newest-first. The backend already returns rows
 * newest-first (CreatedAt DESC); a defensive sort keeps the contract explicit. Every cell is
 * React-escaped text; the content hash + timestamp are mono (data-shaped). */
function AuditList({ rows }: { readonly rows: readonly AuditRow[] }) {
  const { t } = useTranslation();
  const ordered = [...rows].sort((a, b) => b.CreatedAt.localeCompare(a.CreatedAt));
  return (
    <ul aria-label={t('governance.skills.stages.audit')} className="flex flex-col gap-1 p-2">
      {ordered.map((row) => (
        <li
          key={row.ID}
          className="flex flex-col gap-1 rounded-md border border-border bg-surface-2 px-3 py-2"
        >
          <span className="flex items-center justify-between gap-2">
            <span className="break-words text-[15.5px] font-semibold text-text">
              {row.SkillName}
            </span>
            <span className="shrink-0 text-[13px] text-accent-text">{row.Action}</span>
          </span>
          <span className="flex items-center justify-between gap-2 text-[13px] text-text-muted">
            <span className="font-mono">{row.CreatedAt}</span>
            <span className="break-all">{row.ActorID}</span>
          </span>
        </li>
      ))}
    </ul>
  );
}

export function SkillsBoard() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [tab, setTab] = useState<SkillsTab>('active');
  const [selected, setSelected] = useState<string | undefined>(undefined);
  const [installing, setInstalling] = useState(false);
  const [collisionName, setCollisionName] = useState<string | undefined>(undefined);
  const restoreFocusRef = useRef<HTMLElement | null>(null);
  const tabRefs = useRef<Record<SkillsTab, HTMLButtonElement | null>>({
    active: null,
    pending: null,
    archived: null,
    audit: null,
  });
  const tablistId = useId();

  const isLifecycle = tab !== 'audit';
  const stage = isLifecycle ? LIFECYCLE_STAGE[tab] : undefined;

  const lifecycle = useQuery({
    queryKey: ['governance', 'skills', stage ?? 'none'],
    queryFn: () => fetchSkills(stage ?? 'active'),
    retry: false,
    enabled: isLifecycle,
  });
  const audit = useQuery({
    queryKey: ['governance', 'skills', 'audit'],
    queryFn: fetchSkillsAudit,
    retry: false,
    enabled: tab === 'audit',
  });

  function invalidateSkills() {
    void queryClient.invalidateQueries({ queryKey: ['governance', 'skills'] });
  }

  const archiveMutation = useMutation({
    mutationFn: (name: string) => archiveSkill(name),
    onSuccess: invalidateSkills,
  });
  const restoreMutation = useMutation({
    mutationFn: (name: string) => restoreSkill(name),
    onSuccess: () => {
      setCollisionName(undefined);
      invalidateSkills();
    },
    onError: (_err, name) => {
      // A 409 collision (an active skill of the same name) → the inline safe error.
      setCollisionName(name);
    },
  });

  const active = isLifecycle ? lifecycle : audit;
  const lifecycleRows: readonly SkillRow[] = lifecycle.data ?? [];
  const auditRows: readonly AuditRow[] = audit.data ?? [];
  const isEmpty = isLifecycle ? lifecycleRows.length === 0 : auditRows.length === 0;

  const status = boardStatus({
    isLoading: active.isLoading,
    isError: active.isError,
    error: active.error,
    isEmpty,
  });

  const selectedSkill = lifecycleRows.find((s) => s.name === selected);

  function focusTab(next: SkillsTab) {
    setTab(next);
    setSelected(undefined);
    tabRefs.current[next]?.focus();
  }

  function onTabKeyDown(event: React.KeyboardEvent, index: number) {
    if (event.key === 'ArrowRight' || event.key === 'ArrowDown') {
      event.preventDefault();
      focusTab(SKILL_TABS[(index + 1) % SKILL_TABS.length] ?? 'active');
    } else if (event.key === 'ArrowLeft' || event.key === 'ArrowUp') {
      event.preventDefault();
      focusTab(SKILL_TABS[(index - 1 + SKILL_TABS.length) % SKILL_TABS.length] ?? 'active');
    }
  }

  function selectRow(name: string, el: HTMLElement | null) {
    restoreFocusRef.current = el;
    setSelected(name);
  }

  function openInstall(el: HTMLElement | null) {
    restoreFocusRef.current = el;
    setSelected(undefined);
    setInstalling(true);
  }

  const subTabs = (
    <div className="flex shrink-0 items-center justify-between gap-2 border-b border-border bg-surface px-2 py-1">
      <div
        role="tablist"
        aria-label={t('governance.tabs.skills')}
        className="flex items-center gap-1"
      >
        {SKILL_TABS.map((name, index) => {
          const selectedTab = tab === name;
          return (
            <button
              key={name}
              type="button"
              role="tab"
              id={`${tablistId}-tab-${name}`}
              aria-selected={selectedTab}
              tabIndex={selectedTab ? 0 : -1}
              ref={(el) => {
                tabRefs.current[name] = el;
              }}
              onKeyDown={(e) => {
                onTabKeyDown(e, index);
              }}
              onClick={() => {
                setTab(name);
                setSelected(undefined);
              }}
              className={`min-h-[44px] rounded-md px-3 py-2 text-[13px] font-semibold transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${
                selectedTab
                  ? 'bg-accent text-on-accent'
                  : 'text-text-muted hover:bg-surface-2 hover:text-text'
              }`}
            >
              {t(`governance.skills.stages.${name}`)}
            </button>
          );
        })}
      </div>
      {/* The ONE accent CTA on the board — always reachable. */}
      <button
        type="button"
        onClick={(e) => {
          openInstall(e.currentTarget);
        }}
        className="min-h-[44px] rounded-md bg-accent px-4 py-2 text-[13px] font-semibold text-on-accent outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        {t('governance.skills.installSkill')}
      </button>
    </div>
  );

  // active rows gain Archive skill; archived rows gain Restore skill; pending keeps NO
  // run/activate control. The lifecycle button is a SIBLING of the select button (never nested).
  const master = (
    <div role="list" aria-label={t('governance.tabs.skills')} className="flex flex-col gap-1 p-2">
      {lifecycleRows.map((skill) => (
        <div key={skill.name} role="listitem" className="flex flex-col gap-1">
          <button
            type="button"
            aria-pressed={selected === skill.name}
            onClick={(e) => {
              selectRow(skill.name, e.currentTarget);
            }}
            className={`flex min-h-[44px] w-full flex-col gap-1 rounded-md border px-3 py-2 text-left transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${
              selected === skill.name
                ? 'border-accent bg-accent text-on-accent'
                : 'border-border bg-surface-2 text-text hover:border-border-strong'
            }`}
          >
            <span className="flex items-center justify-between gap-2">
              <span className="break-words text-[15.5px] font-semibold">{skill.name}</span>
              <span className="shrink-0 text-[13px] text-text-muted">{skill.type}</span>
            </span>
            {/* Per-row metadata (content hash, mono) — the rest surfaces in the detail. */}
            {skill.contentHash !== undefined && skill.contentHash !== '' ? (
              <span className="break-all font-mono text-[13px] tracking-tight text-text-muted">
                {skill.contentHash}
              </span>
            ) : null}
            {tab === 'pending' ? (
              <span className="text-[13px] text-warning">{t('governance.skills.pendingNote')}</span>
            ) : null}
          </button>
          {tab === 'active' ? (
            <button
              type="button"
              disabled={archiveMutation.isPending}
              onClick={() => {
                archiveMutation.mutate(skill.name);
              }}
              className="min-h-[44px] self-start rounded-md border border-border-strong bg-surface-2 px-3 py-2 text-[13px] font-semibold text-text outline-none hover:border-border-strong focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-60"
            >
              {t('governance.skills.archive')}
            </button>
          ) : null}
          {tab === 'archived' ? (
            <>
              <button
                type="button"
                disabled={restoreMutation.isPending}
                onClick={() => {
                  setCollisionName(undefined);
                  restoreMutation.mutate(skill.name);
                }}
                className="min-h-[44px] self-start rounded-md border border-border-strong bg-surface-2 px-3 py-2 text-[13px] font-semibold text-text outline-none hover:border-border-strong focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-60"
              >
                {t('governance.skills.restore')}
              </button>
              {collisionName === skill.name ? (
                <p role="alert" className="text-[13px] text-danger">
                  {t('governance.skills.collidingRestore', { name: skill.name })}
                </p>
              ) : null}
            </>
          ) : null}
        </div>
      ))}
    </div>
  );

  return (
    <div className="flex h-full min-h-0 flex-col">
      {subTabs}
      <div className="min-h-0 flex-1">
        {installing ? (
          // The install panel is reachable from any tab/state (incl. an empty list); it slots
          // into a BoardLayout detail pane regardless of the fetch status.
          <BoardLayout
            master={master}
            detail={
              <SkillInstallPanel
                onClose={() => {
                  setInstalling(false);
                }}
              />
            }
            detailOpen={true}
            onCloseDetail={() => {
              setInstalling(false);
            }}
            restoreFocusRef={restoreFocusRef}
            detailLabel={t('governance.detailEmpty')}
          />
        ) : (
          <BoardStateView
            status={status}
            emptyHeading={t('governance.skills.emptyHeading')}
            emptyBody={
              tab === 'audit' ? t('governance.skills.auditEmpty') : t('governance.skills.emptyBody')
            }
            onRetry={() => {
              void active.refetch();
            }}
          >
            {tab === 'audit' ? (
              <div className="h-full overflow-y-auto">
                <AuditList rows={auditRows} />
              </div>
            ) : (
              <BoardLayout
                master={master}
                detail={
                  selectedSkill !== undefined ? (
                    <SkillDetail
                      skill={selectedSkill}
                      isPending={tab === 'pending'}
                      onClose={() => {
                        setSelected(undefined);
                      }}
                    />
                  ) : undefined
                }
                detailOpen={selectedSkill !== undefined}
                onCloseDetail={() => {
                  setSelected(undefined);
                }}
                restoreFocusRef={restoreFocusRef}
                detailLabel={t('governance.detailEmpty')}
              />
            )}
          </BoardStateView>
        )}
      </div>
    </div>
  );
}
