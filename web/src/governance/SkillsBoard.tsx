import { lazy, Suspense, useMemo, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { BoardLayout } from './BoardLayout';
import { BoardStateView, boardStatus } from './governanceView';
import { SkillDeleteDialog } from './SkillDeleteDialog';
import { SkillDetail } from './SkillDetail';
import { SkillInstallPanel } from './SkillInstallPanel';
import { SkillsAuditList } from './SkillsAuditList';
import { SkillsList, type StagedSkill } from './SkillsList';
import { SkillsToolbar, type SkillsFilter } from './SkillsToolbar';
import {
  archiveSkill,
  deleteSkill,
  fetchSkills,
  fetchSkillsAudit,
  restoreSkill,
  type AuditRow,
  type SkillDraft,
  type SkillRow,
  type SkillStage,
} from './governanceApi';

// Tiptap and its ProseMirror core are the heaviest thing the cockpit can load, and most
// visits to this board never author anything — so the editor is a chunk of its own,
// fetched when the operator actually opens it.
const SkillEditor = lazy(() => import('./SkillEditor').then((m) => ({ default: m.SkillEditor })));

// SkillsBoard (GOV-02, rebuilt for amendment #97) — ONE list of every skill, filtered by
// state, with the audit ledger behind a toggle.
//
// It used to be four lifecycle tabs. 'Pending' went away with the approval stage: there is
// no longer a state in which a skill exists but does not work, so there is nothing to
// stage, review, or wait for. That left 'Active' and 'Archived', which are not places to
// navigate to but a property of a row — so they became filters over one list, and the
// counts stay visible instead of hiding behind a click.
//
// The two lifecycle stages are fetched together (the counts need both anyway), so
// filtering and searching are local: no refetch, no spinner, no stale-tab flicker.
//
// The queries DO refetch on window focus, against the SPA-wide default. This board is a
// view of state other actors change — the agent installs a skill mid-turn, the operator
// runs `aura skills create` in a terminal — and a board left open on another tab showed
// a snapshot from whenever it mounted. Coming back to the tab is exactly the moment the
// answer is expected to be current.

export function SkillsBoard() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [filter, setFilter] = useState<SkillsFilter>('all');
  const [query, setQuery] = useState('');
  const [selected, setSelected] = useState<string | undefined>(undefined);
  const [installing, setInstalling] = useState(false);
  const [auditOpen, setAuditOpen] = useState(false);
  const [collisionName, setCollisionName] = useState<string | undefined>(undefined);
  const [pendingDelete, setPendingDelete] = useState<string | undefined>(undefined);
  // undefined = not editing; 'new' = authoring; a draft = editing that skill.
  const [editing, setEditing] = useState<SkillDraft | 'new' | undefined>(undefined);
  const restoreFocusRef = useRef<HTMLElement | null>(null);

  const active = useQuery({
    queryKey: ['governance', 'skills', 'active'],
    queryFn: () => fetchSkills('active'),
    retry: false,
    refetchOnWindowFocus: true,
  });
  const archived = useQuery({
    queryKey: ['governance', 'skills', 'archived'],
    queryFn: () => fetchSkills('archived'),
    retry: false,
    refetchOnWindowFocus: true,
  });
  const audit = useQuery({
    queryKey: ['governance', 'skills', 'audit'],
    queryFn: fetchSkillsAudit,
    retry: false,
    refetchOnWindowFocus: true,
    enabled: auditOpen,
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
  const deleteMutation = useMutation({
    mutationFn: (name: string) => deleteSkill(name),
    onSuccess: (_data, name) => {
      setPendingDelete(undefined);
      setSelected((current) => (current === name ? undefined : current));
      invalidateSkills();
    },
  });

  // The merged list: active first, then archived, each row tagged with the stage it came
  // from so the row and the detail can tell them apart without a second lookup.
  const allRows: readonly StagedSkill[] = useMemo(() => {
    const toStaged = (rows: readonly SkillRow[], stage: SkillStage): StagedSkill[] =>
      rows.map((row) => ({ ...row, stage }));
    return [...toStaged(active.data ?? [], 'active'), ...toStaged(archived.data ?? [], 'archived')];
  }, [active.data, archived.data]);

  const counts = useMemo(
    () => ({
      all: allRows.length,
      active: allRows.filter((r) => r.stage === 'active').length,
      archived: allRows.filter((r) => r.stage === 'archived').length,
    }),
    [allRows],
  );

  const rows = useMemo(() => {
    const needle = query.trim().toLowerCase();
    return allRows.filter((row) => {
      if (filter !== 'all' && row.stage !== filter) {
        return false;
      }
      if (needle === '') {
        return true;
      }
      return (
        row.name.toLowerCase().includes(needle) || row.description.toLowerCase().includes(needle)
      );
    });
  }, [allRows, filter, query]);

  const auditRows: readonly AuditRow[] = audit.data ?? [];
  const listQuery = active.isError ? active : archived;
  const status = boardStatus({
    isLoading: auditOpen ? audit.isLoading : active.isLoading || archived.isLoading,
    isError: auditOpen ? audit.isError : active.isError || archived.isError,
    error: auditOpen ? audit.error : listQuery.error,
    isEmpty: auditOpen ? auditRows.length === 0 : rows.length === 0,
  });

  const selectedSkill = rows.find((s) => s.name === selected);

  function closeOverlays() {
    setInstalling(false);
    setSelected(undefined);
  }

  const master = (
    <SkillsList
      rows={rows}
      selected={selected}
      onSelect={(name, el) => {
        restoreFocusRef.current = el;
        setInstalling(false);
        setSelected(name);
      }}
      onArchive={(name) => {
        archiveMutation.mutate(name);
      }}
      onRestore={(name) => {
        setCollisionName(undefined);
        restoreMutation.mutate(name);
      }}
      onDelete={setPendingDelete}
      archivePending={archiveMutation.isPending}
      restorePending={restoreMutation.isPending}
      collisionName={collisionName}
    />
  );

  // The editor takes over the whole board rather than sharing it with the list: authoring
  // a skill is a task, not a lookup, and the split view has nothing useful to show beside
  // it.
  if (editing !== undefined) {
    return (
      <Suspense
        fallback={
          <p className="p-6 text-[14px] text-text-muted">{t('governance.skills.editor.loading')}</p>
        }
      >
        <SkillEditor
          {...(editing === 'new' ? {} : { initial: editing })}
          onClose={() => {
            setEditing(undefined);
          }}
          onSaved={(name) => {
            setEditing(undefined);
            invalidateSkills();
            setSelected(name);
          }}
        />
      </Suspense>
    );
  }

  return (
    <div className="flex h-full min-h-0 flex-col">
      <SkillsToolbar
        query={query}
        onQueryChange={setQuery}
        filter={filter}
        onFilterChange={(next) => {
          setFilter(next);
          setSelected(undefined);
        }}
        counts={counts}
        auditOpen={auditOpen}
        onToggleAudit={(el) => {
          restoreFocusRef.current = el;
          closeOverlays();
          setAuditOpen((open) => !open);
        }}
        onNew={() => {
          setEditing('new');
        }}
        onInstall={(el) => {
          restoreFocusRef.current = el;
          setAuditOpen(false);
          setSelected(undefined);
          setInstalling(true);
        }}
      />

      <div className="min-h-0 flex-1">
        <BoardStateView
          status={status}
          emptyHeading={t('governance.skills.emptyHeading')}
          emptyBody={
            auditOpen ? t('governance.skills.auditEmpty') : t('governance.skills.emptyBody')
          }
          onRetry={() => {
            void (auditOpen
              ? audit.refetch()
              : Promise.all([active.refetch(), archived.refetch()]));
          }}
        >
          {auditOpen ? (
            <div className="h-full overflow-y-auto">
              <SkillsAuditList rows={auditRows} />
            </div>
          ) : (
            <BoardLayout
              master={master}
              detail={
                installing ? (
                  <SkillInstallPanel
                    onClose={() => {
                      setInstalling(false);
                    }}
                  />
                ) : selectedSkill !== undefined ? (
                  <SkillDetail
                    skill={selectedSkill}
                    stage={selectedSkill.stage}
                    onClose={() => {
                      setSelected(undefined);
                    }}
                    onDelete={setPendingDelete}
                    onEdit={setEditing}
                  />
                ) : undefined
              }
              detailOpen={installing || selectedSkill !== undefined}
              onCloseDetail={closeOverlays}
              restoreFocusRef={restoreFocusRef}
              detailLabel={t('governance.detailEmpty')}
            />
          )}
        </BoardStateView>
      </div>

      <SkillDeleteDialog
        open={pendingDelete !== undefined}
        name={pendingDelete ?? ''}
        pending={deleteMutation.isPending}
        onCancel={() => {
          setPendingDelete(undefined);
        }}
        onConfirm={() => {
          if (pendingDelete !== undefined) {
            deleteMutation.mutate(pendingDelete);
          }
        }}
      />
    </div>
  );
}
