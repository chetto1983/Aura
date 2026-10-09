import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Columns3 } from 'lucide-react';
import {
  Editor,
  getEditorItems,
  Kanban,
  Willow,
  WillowDark,
  type CardShape,
  type KanbanCard,
  type KanbanInstanceApi,
} from '@svar-ui/react-kanban';
import { Locale } from '@svar-ui/react-core';
import { useTranslation } from 'react-i18next';
import '@svar-ui/react-kanban/all.css';
import '@/styles/svar.css';
import {
  BOARD_QUERY_KEY,
  createBoardProvider,
  withDates,
  type BoardFilters,
  type BoardView,
} from './boardApi';
import { BoardCardContent } from './BoardCardContent';
import { BoardCardContext } from './boardCardContext';
import { BoardColumnsDialog } from './BoardColumnsDialog';
import { BoardViewsBar } from './BoardViewsBar';
import { boardTags, filterPredicate } from './boardModel';
import { boardWords } from './boardLocale';
import { TagsEditorItem } from './TagsEditorItem';
import {
  useBoardData,
  useBoardViews,
  useDeleteView,
  useSaveColumns,
  useSaveView,
} from './useBoard';
import { fetchSchedulerTasks } from '@/governance/governanceApi';
import { SCHEDULER_QUERY_KEY } from '@/governance/useSchedulerMutations';
import { Button } from '@/components/ui/button';
import { ConfirmDialog } from '@/components/ui/confirm-dialog';
import { HttpError } from '@/api/json';
import { useNow } from '@/lib/useNow';
import { useThemeMode } from '@/theme/useThemeMode';

interface BoardWorkspaceProps {
  readonly mobileMenu?: ReactNode;
  /** Opens the card's conversation, or starts one with draft in the composer when it has none. */
  readonly onDiscuss: (conversationId: string, draft: string) => void;
}

// The card flags that matter with a custom template: the menu, which the template's button
// opens. An identity's board has one user and no cover images, so the rest stay off.
const CARD_SHAPE: CardShape = { menu: true };
const EDITOR_ITEMS: Record<string, unknown>[] = [
  ...(getEditorItems({ description: true, priority: true, deadline: true }) as Record<
    string,
    unknown
  >[]),
  { comp: TagsEditorItem, key: 'tags', label: 'Tags' },
];
const MINUTE_MS = 60_000;

/**
 * The work board IS the component: SVAR React Kanban (MIT) driven by its own RestDataProvider
 * against a mount that speaks its dialect, the way the file manager is. Aura adds the card
 * template, the status check the provider lacks, the confirmation before a delete, the column
 * editor the free edition lacks, and the saved views.
 */
export default function BoardWorkspace({ mobileMenu, onDiscuss }: BoardWorkspaceProps) {
  const { t, i18n } = useTranslation();
  const queryClient = useQueryClient();
  const board = useBoardData();
  const views = useBoardViews();
  const saveColumns = useSaveColumns();
  const saveView = useSaveView();
  const deleteView = useDeleteView();
  const [api, setApi] = useState<KanbanInstanceApi | null>(null);
  const [error, setError] = useState('');
  const [filters, setFilters] = useState<BoardFilters>({});
  const [activeViewId, setActiveViewId] = useState('');
  const [columnsOpen, setColumnsOpen] = useState(false);
  const [pendingDelete, setPendingDelete] = useState<string | null>(null);
  const confirmedDelete = useRef<string | null>(null);
  const now = useNow(MINUTE_MS);

  const errorText = useCallback(
    (code: string) =>
      i18n.t(`board.errors.${code}`, { defaultValue: i18n.t('board.errors.generic') }),
    [i18n],
  );

  // One provider for the component's lifetime: it is an event-bus link, and rebuilding it
  // would re-register its handlers. i18n and the query client are stable, so this is too.
  const provider = useMemo(
    () =>
      createBoardProvider((reason) => {
        if (reason !== undefined) setError(errorText(reason));
        void queryClient.invalidateQueries({ queryKey: BOARD_QUERY_KEY });
      }),
    [errorText, queryClient],
  );

  const cards = useMemo(() => withDates(board.data?.cards ?? []), [board.data?.cards]);
  const columns = useMemo(() => [...(board.data?.columns ?? [])], [board.data?.columns]);
  const tags = useMemo(() => boardTags(cards), [cards]);
  const linksTasks = cards.some((card) => card.task_id !== undefined);
  const tasks = useQuery({
    queryKey: SCHEDULER_QUERY_KEY,
    queryFn: fetchSchedulerTasks,
    retry: false,
    enabled: linksTasks,
  });
  const cardContext = useMemo(
    () => ({
      taskStatus: (id: string) => tasks.data?.find((task) => task.ID === id)?.Status,
      now: new Date(now),
    }),
    [tasks.data, now],
  );

  const init = useCallback(
    (kanban: KanbanInstanceApi) => {
      kanban.setNext(provider);
      // The widget adds a card as no more than a column (the "+") or a column and an English
      // label (a double click), and the server never merges its answer back. The card gets
      // here the shape the server gives it, so the template and the filters read a whole card,
      // and a title, which the server refuses to go without.
      kanban.intercept('add-card', (ev) => {
        const label = typeof ev.card.label === 'string' ? ev.card.label.trim() : '';
        ev.card = {
          description: '',
          priority: 2,
          tags: [],
          source: 'cockpit',
          updated_by: 'operator',
          ...ev.card,
          label: label === '' || label === 'New card' ? i18n.t('board.newCard') : label,
        };
      });
      // The editor's button and the card menu both delete through here, so both ask first.
      kanban.intercept('delete-card', (ev) => {
        if (confirmedDelete.current === String(ev.id)) {
          confirmedDelete.current = null;
          return true;
        }
        setPendingDelete(String(ev.id));
        return false;
      });
      setApi(kanban);
    },
    [i18n, provider],
  );

  // The widget re-initialises its store whenever cards or columns change, which drops any
  // filter it was given by action; a view is put back after every such reset. A parent effect
  // runs after the widget's own, so this lands on the store that reset just built.
  useEffect(() => {
    if (api === null) return;
    void api.exec('filter-cards', { filter: filterPredicate(filters, new Date(now)), tag: 'view' });
  }, [api, cards, columns, filters, now]);

  const topBar = useMemo(
    () => ({
      items: [
        { comp: 'icon', icon: 'wxi-close', id: 'close' },
        { comp: 'spacer' },
        { comp: 'button', id: 'discuss', text: t('board.editor.discuss') },
        { comp: 'button', id: 'delete', text: t('board.editor.delete'), type: 'danger' },
        { comp: 'button', id: 'save', text: t('board.editor.save'), type: 'primary' },
      ],
    }),
    [t],
  );

  const editorAction = useCallback(
    ({ item }: { item: { id?: string | number } }) => {
      if (api === null) return;
      const { editorData: card } = api.getState() as { editorData?: KanbanCard | null };
      if (card == null) return;
      if (item.id === 'discuss') {
        const conversation = typeof card.conversation_id === 'string' ? card.conversation_id : '';
        onDiscuss(conversation, t('board.discussDraft', { label: String(card.label ?? '') }));
      } else if (item.id === 'delete') {
        void api.exec('delete-card', { id: card.id });
      } else if (item.id !== 'save') {
        return;
      }
      void api.exec('select-card', { id: null });
    },
    [api, onDiscuss, t],
  );

  function selectView(view: BoardView | null) {
    setActiveViewId(view?.id ?? '');
    setFilters(view?.filters ?? {});
  }

  async function storeColumns(next: Parameters<typeof saveColumns.mutateAsync>[0]) {
    try {
      await saveColumns.mutateAsync(next);
      setColumnsOpen(false);
    } catch {
      // The dialog shows the mutation's error and stays open.
    }
  }

  const Theme = useThemeMode() === 'light' ? Willow : WillowDark;
  const loadError = board.isError
    ? errorText(board.error instanceof HttpError ? board.error.reason || 'generic' : 'generic')
    : '';

  return (
    <section
      aria-label={t('board.title')}
      className="relative flex h-full min-h-0 min-w-0 flex-col bg-bg"
    >
      <header className="flex items-center gap-2 border-b border-border px-3 py-2">
        <span className="md:hidden">{mobileMenu}</span>
        <h1 className="text-[15px] font-semibold text-text">{t('board.title')}</h1>
        <Button
          type="button"
          variant="ghost"
          className="ml-auto"
          disabled={board.data === undefined}
          onClick={() => {
            setColumnsOpen(true);
          }}
        >
          <Columns3 data-icon="inline-start" aria-hidden="true" />
          {t('board.columns.edit')}
        </Button>
      </header>
      <BoardViewsBar
        filters={filters}
        onFiltersChange={(next) => {
          setActiveViewId('');
          setFilters(next);
        }}
        tags={tags}
        views={views.data ?? []}
        activeViewId={activeViewId}
        onSelectView={selectView}
        onSaveView={(name) => {
          void saveView.mutateAsync({ name, filters, pinned: false }).then((view) => {
            setActiveViewId(view.id);
          });
        }}
        onTogglePin={(view) => {
          void saveView.mutateAsync({
            name: view.name,
            filters: view.filters,
            pinned: !view.pinned,
          });
        }}
        onDeleteView={(view) => {
          if (view.id === activeViewId) selectView(null);
          void deleteView.mutateAsync(view.id);
        }}
      />
      {(error !== '' || loadError !== '') && (
        <p role="alert" className="px-3 py-2 text-sm text-danger">
          {error || loadError}
        </p>
      )}
      {board.isPending ? (
        <p role="status" className="grid flex-1 place-items-center text-sm text-text-muted">
          {t('board.loading')}
        </p>
      ) : (
        <div className="min-h-0 flex-1 [&>*]:h-full">
          <Theme fonts={false}>
            <Locale words={boardWords(i18n.language)}>
              <BoardCardContext.Provider value={cardContext}>
                <Kanban
                  cards={cards}
                  columns={columns}
                  card={CARD_SHAPE}
                  cardContent={BoardCardContent}
                  init={init}
                />
              </BoardCardContext.Provider>
              {api !== null && (
                <Editor
                  api={api}
                  placement="modal"
                  autoSave={false}
                  items={EDITOR_ITEMS}
                  topBar={topBar}
                  onAction={editorAction}
                />
              )}
            </Locale>
          </Theme>
        </div>
      )}
      {columnsOpen && board.data !== undefined && (
        <BoardColumnsDialog
          columns={board.data.columns}
          onClose={() => {
            setColumnsOpen(false);
          }}
          onSave={storeColumns}
          saving={saveColumns.isPending}
          error={
            saveColumns.error instanceof HttpError
              ? errorText(saveColumns.error.reason || 'generic')
              : ''
          }
        />
      )}
      <ConfirmDialog
        open={pendingDelete !== null}
        onOpenChange={(open) => {
          if (!open) setPendingDelete(null);
        }}
        title={t('board.delete.title')}
        description={t('board.delete.description')}
        cancelLabel={t('board.delete.cancel')}
        confirmLabel={t('board.delete.confirm')}
        onConfirm={() => {
          if (api !== null && pendingDelete !== null) {
            confirmedDelete.current = pendingDelete;
            void api.exec('delete-card', { id: pendingDelete });
          }
          setPendingDelete(null);
        }}
      />
    </section>
  );
}
