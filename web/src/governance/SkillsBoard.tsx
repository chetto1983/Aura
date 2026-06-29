import { useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Download, RotateCcw, Trash2 } from 'lucide-react';
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
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Card } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';

type SkillsTab = 'active' | 'pending' | 'archived' | 'audit';

const SKILL_TABS: readonly SkillsTab[] = ['active', 'pending', 'archived', 'audit'];

const LIFECYCLE_STAGE: Record<Exclude<SkillsTab, 'audit'>, SkillStage> = {
  active: 'active',
  pending: 'pending',
  archived: 'archived',
};

function AuditList({ rows }: { readonly rows: readonly AuditRow[] }) {
  const { t } = useTranslation();
  const ordered = [...rows].sort((a, b) => b.CreatedAt.localeCompare(a.CreatedAt));
  return (
    <ul aria-label={t('governance.skills.stages.audit')} className="flex flex-col gap-2 p-2">
      {ordered.map((row) => (
        <Card key={row.ID} role="listitem" className="gap-2 p-3">
          <span className="flex items-center justify-between gap-2">
            <span className="break-words text-[15.5px] font-semibold text-text">
              {row.SkillName}
            </span>
            <Badge variant="secondary">{row.Action}</Badge>
          </span>
          <span className="flex items-center justify-between gap-2 text-[13px] text-text-muted">
            <span className="font-mono">{row.CreatedAt}</span>
            <span className="break-all">{row.ActorID}</span>
          </span>
        </Card>
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

  function selectRow(name: string, el: HTMLElement | null) {
    restoreFocusRef.current = el;
    setSelected(name);
  }

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

  function openInstall(el: HTMLElement | null) {
    restoreFocusRef.current = el;
    setSelected(undefined);
    setInstalling(true);
  }

  const installLabel = t('governance.skills.installSkill');

  const subTabs = (
    <div className="flex shrink-0 items-center gap-2 border-b border-border bg-surface px-2 py-1">
      <TabsList
        aria-label={t('governance.tabs.skills')}
        className="grid min-w-0 flex-1 grid-cols-4 justify-stretch"
      >
        {SKILL_TABS.map((name, index) => (
          <TabsTrigger
            key={name}
            value={name}
            className="min-w-0 px-2 sm:px-3"
            tabIndex={tab === name ? 0 : -1}
            ref={(el) => {
              tabRefs.current[name] = el;
            }}
            onClick={() => {
              setTab(name);
              setSelected(undefined);
            }}
            onKeyDown={(event) => {
              onTabKeyDown(event, index);
            }}
          >
            <span data-tab-label className="min-w-0 truncate">
              {t(`governance.skills.stages.${name}`)}
            </span>
          </TabsTrigger>
        ))}
      </TabsList>
      <Button
        type="button"
        aria-label={installLabel}
        title={installLabel}
        className="px-3 sm:px-4"
        onClick={(e) => {
          openInstall(e.currentTarget);
        }}
      >
        <Download data-icon="inline-start" aria-hidden="true" focusable="false" />
        <span data-action-label className="hidden sm:inline">
          {installLabel}
        </span>
      </Button>
    </div>
  );

  const master = (
    <div role="list" aria-label={t('governance.tabs.skills')} className="flex flex-col gap-2 p-2">
      {lifecycleRows.map((skill) => {
        const isSelected = selected === skill.name;
        const hasRowAction = tab === 'active' || tab === 'archived';
        const archiveLabel = t('governance.skills.archive');
        const restoreLabel = t('governance.skills.restore');
        const metadataTone = isSelected ? 'text-on-accent/80' : 'text-text-muted';
        const selectedBadgeClass = isSelected
          ? 'border-on-accent/30 bg-on-accent/15 text-on-accent'
          : undefined;

        return (
          <Card
            key={skill.name}
            role="listitem"
            className={`gap-0 overflow-hidden p-0 transition-colors ${
              isSelected ? 'border-accent' : 'hover:border-border-strong'
            }`}
          >
            <div className="flex min-w-0 items-stretch gap-2">
              <Button
                type="button"
                variant={isSelected ? 'default' : 'ghost'}
                aria-pressed={isSelected}
                onClick={(e) => {
                  selectRow(skill.name, e.currentTarget);
                }}
                className={`h-auto min-h-[52px] min-w-0 flex-1 flex-col items-stretch justify-start gap-1 px-3 py-2 text-left ${
                  hasRowAction ? 'rounded-r-sm' : ''
                } ${isSelected ? 'text-on-accent' : 'bg-transparent text-text hover:bg-surface-2'}`}
              >
                <span className="flex min-w-0 items-start justify-between gap-2">
                  <span className="min-w-0 break-words text-[15.5px] font-semibold">
                    {skill.name}
                  </span>
                  <Badge variant="secondary" className={selectedBadgeClass}>
                    {skill.type}
                  </Badge>
                </span>
                {skill.contentHash !== undefined && skill.contentHash !== '' ? (
                  <span
                    className={`break-all font-mono text-[13px] tabular-nums tracking-tight ${metadataTone}`}
                  >
                    {skill.contentHash}
                  </span>
                ) : null}
                {tab === 'pending' ? (
                  <Badge
                    variant="warning"
                    className={
                      isSelected ? 'border-on-accent/30 bg-on-accent/15 text-on-accent' : undefined
                    }
                  >
                    {t('governance.skills.pendingNote')}
                  </Badge>
                ) : null}
              </Button>
              {tab === 'active' ? (
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  aria-label={archiveLabel}
                  title={archiveLabel}
                  disabled={archiveMutation.isPending}
                  onClick={() => {
                    archiveMutation.mutate(skill.name);
                  }}
                  className="m-1 text-danger hover:bg-danger/15 hover:text-danger disabled:cursor-wait"
                >
                  <Trash2 data-icon="icon" aria-hidden="true" focusable="false" />
                </Button>
              ) : null}
              {tab === 'archived' ? (
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  aria-label={restoreLabel}
                  title={restoreLabel}
                  disabled={restoreMutation.isPending}
                  onClick={() => {
                    setCollisionName(undefined);
                    restoreMutation.mutate(skill.name);
                  }}
                  className="m-1 text-accent-text hover:bg-surface-3 hover:text-accent-text disabled:cursor-wait"
                >
                  <RotateCcw data-icon="icon" aria-hidden="true" focusable="false" />
                </Button>
              ) : null}
            </div>
            {tab === 'archived' && collisionName === skill.name ? (
              <p role="alert" className="px-3 pb-2 text-[13px] text-danger">
                {t('governance.skills.collidingRestore', { name: skill.name })}
              </p>
            ) : null}
          </Card>
        );
      })}
    </div>
  );

  return (
    <Tabs
      value={tab}
      onValueChange={(value) => {
        setTab(value as SkillsTab);
        setSelected(undefined);
      }}
      className="flex h-full min-h-0 flex-col gap-0"
    >
      {subTabs}
      <TabsContent value={tab} className="min-h-0 flex-1">
        {installing ? (
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
      </TabsContent>
    </Tabs>
  );
}
