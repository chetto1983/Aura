import { useEffect, useRef, useState, type ReactNode } from 'react';
import {
  AlertCircle,
  Archive,
  MessageSquareText,
  MoreHorizontal,
  Pencil,
  RotateCcw,
  Trash2,
} from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { ariaInvalid } from '../a11y/aria';
import { DeleteConfirmDialog } from './DeleteConfirmDialog';
import {
  displayTitle,
  isArchived,
  useArchiveConversation,
  useConversations,
  useDeleteConversation,
  useRenameConversation,
  useUnarchiveConversation,
  type Conversation,
} from './useConversations';
import { SkeletonBlock } from '@/components/skeleton';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { Checkbox } from '@/components/ui/checkbox';
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';

// ConversationSidebar is the CHAT-02 conversation manager that replaces the
// AppShell left-aside placeholder labels. It lists conversations recent-first
// (the store orders them), marks the selected row aria-current, hides archived
// rows behind an "include archived" toggle, supports inline rename, archives as
// the reversible primary action (D-07), and routes "Delete permanently" through a
// focus-trapped confirm dialog (T-25-14 — Store.Delete is never called blind).
//
// Titles render as React text nodes (auto-escaped) — never dangerouslySetInnerHTML
// (T-25-15). The active conversation drives the chat lane's threadId (AppShell).

export interface ConversationSidebarProps {
  /** The active conversation id (aria-current row); empty when none selected. */
  readonly activeId: string;
  /** Select a conversation → the chat lane POSTs against it. */
  readonly onSelect: (id: string) => void;
}

export function ConversationSidebar({ activeId, onSelect }: ConversationSidebarProps) {
  const { t } = useTranslation();
  const [includeArchived, setIncludeArchived] = useState(false);
  const [pendingDelete, setPendingDelete] = useState<Conversation | null>(null);

  const { data, isPending, isError } = useConversations(includeArchived);
  const rename = useRenameConversation();
  const archive = useArchiveConversation();
  const unarchive = useUnarchiveConversation();
  const remove = useDeleteConversation();

  const conversations = data ?? [];
  const groups = groupByRecency(conversations);

  function confirmDelete() {
    const target = pendingDelete;
    if (!target) return;
    remove.mutate(target.ID);
    setPendingDelete(null);
  }

  return (
    <div className="flex h-full min-h-0 flex-col gap-2 p-3">
      <div className="flex items-center justify-between gap-2">
        <h2 className="text-[0.75rem] font-medium uppercase tracking-wider text-text-faint">
          {t('conversations.heading')}
        </h2>
        <div className="flex items-center gap-2">
          <Checkbox
            id="conversation-include-archived"
            checked={includeArchived}
            onCheckedChange={(checked) => {
              setIncludeArchived(checked === true);
            }}
          />
          <Label
            htmlFor="conversation-include-archived"
            className="cursor-pointer text-[0.75rem] font-normal text-text-muted"
          >
            {t('conversations.includeArchived')}
          </Label>
        </div>
      </div>

      {isPending ? (
        <ConversationListSkeleton label={t('conversations.loading')} />
      ) : isError ? (
        <Alert variant="destructive" className="bg-surface">
          <AlertCircle data-icon aria-hidden="true" className="size-4" />
          <AlertDescription>{t('conversations.loadError')}</AlertDescription>
        </Alert>
      ) : conversations.length === 0 ? (
        <Empty className="flex-none border border-dashed border-border bg-surface-2/40 py-8">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <MessageSquareText data-icon aria-hidden="true" className="size-5" />
            </EmptyMedia>
            <EmptyTitle className="text-sm">{t('conversations.empty.heading')}</EmptyTitle>
            <EmptyDescription className="text-[0.8125rem]">
              {t('conversations.empty.body')}
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <ul className="flex min-h-0 flex-1 flex-col gap-2 overflow-y-auto">
          {groups.map((group) => (
            <li key={group.key} className="flex flex-col gap-1">
              <h3 className="px-2 text-[0.8125rem] font-semibold text-text-faint">
                {t(`conversations.recency.${group.key}`)}
              </h3>
              <ul className="flex flex-col gap-1">
                {group.items.map((conv) => (
                  <ConversationRow
                    key={conv.ID}
                    conv={conv}
                    selected={conv.ID === activeId}
                    onSelect={onSelect}
                    onRename={(title) => {
                      rename.mutate({ id: conv.ID, title });
                    }}
                    onArchive={() => {
                      archive.mutate(conv.ID);
                    }}
                    onUnarchive={() => {
                      unarchive.mutate(conv.ID);
                    }}
                    onRequestDelete={() => {
                      setPendingDelete(conv);
                    }}
                  />
                ))}
              </ul>
            </li>
          ))}
        </ul>
      )}

      <DeleteConfirmDialog
        open={pendingDelete !== null}
        title={pendingDelete ? displayTitle(pendingDelete, t('conversations.untitled')) : ''}
        onConfirm={confirmDelete}
        onCancel={() => {
          setPendingDelete(null);
        }}
      />
    </div>
  );
}

interface ConversationRowProps {
  readonly conv: Conversation;
  readonly selected: boolean;
  readonly onSelect: (id: string) => void;
  readonly onRename: (title: string) => void;
  readonly onArchive: () => void;
  readonly onUnarchive: () => void;
  readonly onRequestDelete: () => void;
}

function ConversationRow({
  conv,
  selected,
  onSelect,
  onRename,
  onArchive,
  onUnarchive,
  onRequestDelete,
}: ConversationRowProps) {
  const { t } = useTranslation();
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState('');
  const [menuOpen, setMenuOpen] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);
  const rowRef = useRef<HTMLLIElement>(null);

  const archived = isArchived(conv);
  const label = displayTitle(conv, t('conversations.untitled'));
  const menuLabel = t('conversations.actions.more');

  useEffect(() => {
    if (editing) {
      inputRef.current?.focus();
      inputRef.current?.select();
    }
  }, [editing]);

  useEffect(() => {
    if (!menuOpen) return;

    function onPointerDown(event: PointerEvent) {
      if (rowRef.current?.contains(event.target as Node)) return;
      setMenuOpen(false);
    }

    function onKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape') {
        setMenuOpen(false);
      }
    }

    window.addEventListener('pointerdown', onPointerDown);
    window.addEventListener('keydown', onKeyDown);
    return () => {
      window.removeEventListener('pointerdown', onPointerDown);
      window.removeEventListener('keydown', onKeyDown);
    };
  }, [menuOpen]);

  function startEditing() {
    setMenuOpen(false);
    setDraft(label);
    setEditing(true);
  }

  function commit() {
    const next = draft.trim();
    setEditing(false);
    if (next.length > 0 && next !== label) {
      onRename(next);
    }
  }

  const invalid = editing && draft.trim().length === 0;

  return (
    <li ref={rowRef} className="relative">
      <div
        className={`group grid grid-cols-[minmax(0,1fr)_auto] items-center rounded-md transition-colors ${
          selected ? 'bg-surface-2 text-text' : 'text-text-muted hover:bg-surface-2/70'
        }`}
        data-animate="surface"
      >
        {editing ? (
          <Input
            ref={inputRef}
            value={draft}
            onChange={(event) => {
              setDraft(event.target.value);
            }}
            onBlur={commit}
            onKeyDown={(event) => {
              if (event.key === 'Enter') {
                event.preventDefault();
                commit();
              } else if (event.key === 'Escape') {
                event.preventDefault();
                setEditing(false);
              }
            }}
            aria-label={t('conversations.renameLabel')}
            aria-invalid={ariaInvalid(invalid)}
            className="col-span-2 min-h-9 bg-surface text-sm"
          />
        ) : (
          <>
            <Button
              type="button"
              variant="ghost"
              onClick={() => {
                onSelect(conv.ID);
              }}
              onDoubleClick={startEditing}
              aria-current={selected ? 'true' : undefined}
              aria-label={label}
              className="h-9 min-h-9 min-w-0 justify-start rounded-md px-2 py-1.5 text-left text-[13px] font-medium text-text hover:bg-transparent"
            >
              <span className="flex min-w-0 flex-1 items-center gap-2">
                {archived ? (
                  <Badge variant="secondary" className="shrink-0 text-[0.75rem]">
                    {t('conversations.archivedTag')}
                  </Badge>
                ) : null}
                <span className="truncate">{label}</span>
              </span>
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="icon"
              aria-label={menuLabel}
              aria-haspopup="menu"
              aria-expanded={menuOpen}
              data-open={menuOpen ? 'true' : undefined}
              title={menuLabel}
              onClick={() => {
                setMenuOpen((open) => !open);
              }}
              className="h-8 min-h-8 w-8 rounded-md text-text-faint opacity-100 hover:bg-surface-3 hover:text-text focus-visible:opacity-100 data-[open=true]:bg-surface-3 data-[open=true]:text-text md:opacity-0 md:group-hover:opacity-100"
            >
              <MoreHorizontal data-icon="icon" aria-hidden="true" focusable="false" />
            </Button>
          </>
        )}

        {menuOpen && !editing ? (
          <div
            role="menu"
            aria-label={menuLabel}
            className="absolute left-0 top-10 z-50 flex w-56 flex-col gap-1 rounded-xl border border-border bg-surface-2 p-1.5 text-[13px] text-text shadow-[var(--shadow-popover)]"
          >
            <MenuAction
              label={t('conversations.actions.rename')}
              icon={<Pencil data-icon aria-hidden="true" className="size-4" />}
              onClick={startEditing}
            />
            {archived ? (
              <MenuAction
                label={t('conversations.actions.unarchive')}
                icon={<RotateCcw data-icon aria-hidden="true" className="size-4" />}
                onClick={() => {
                  setMenuOpen(false);
                  onUnarchive();
                }}
              />
            ) : (
              <MenuAction
                label={t('conversations.actions.archive')}
                icon={<Archive data-icon aria-hidden="true" className="size-4" />}
                onClick={() => {
                  setMenuOpen(false);
                  onArchive();
                }}
              />
            )}
            <div className="my-1 h-px bg-border" />
            <MenuAction
              label={t('conversations.actions.delete')}
              icon={<Trash2 data-icon aria-hidden="true" className="size-4" />}
              onClick={() => {
                setMenuOpen(false);
                onRequestDelete();
              }}
              danger
            />
          </div>
        ) : null}
      </div>
    </li>
  );
}

function ConversationListSkeleton({ label }: { readonly label: string }) {
  return (
    <div aria-label={label} className="flex flex-col gap-2">
      {Array.from({ length: 4 }).map((_, index) => (
        <Card key={index} className="gap-2 bg-surface-2/40 p-3">
          <SkeletonBlock height="1rem" radius="md" width="75%" />
          <SkeletonBlock height="0.75rem" radius="md" width="50%" />
        </Card>
      ))}
    </div>
  );
}

type RecencyKey = 'today' | 'yesterday' | 'last7' | 'older';

interface RecencyGroup {
  readonly key: RecencyKey;
  readonly items: Conversation[];
}

function groupByRecency(conversations: readonly Conversation[]): RecencyGroup[] {
  const groups: Record<RecencyKey, Conversation[]> = {
    today: [],
    yesterday: [],
    last7: [],
    older: [],
  };
  const now = new Date();
  const today = startOfDay(now).getTime();
  for (const conv of conversations) {
    const created = new Date(conv.CreatedAt);
    const day = Number.isNaN(created.getTime()) ? 0 : startOfDay(created).getTime();
    const ageDays = Math.floor((today - day) / 86_400_000);
    if (ageDays <= 0) groups.today.push(conv);
    else if (ageDays === 1) groups.yesterday.push(conv);
    else if (ageDays <= 7) groups.last7.push(conv);
    else groups.older.push(conv);
  }
  return (Object.keys(groups) as RecencyKey[])
    .map((key) => ({ key, items: groups[key] }))
    .filter((group) => group.items.length > 0);
}

function startOfDay(date: Date): Date {
  return new Date(date.getFullYear(), date.getMonth(), date.getDate());
}

function MenuAction({
  label,
  icon,
  onClick,
  danger,
}: {
  readonly label: string;
  readonly icon: ReactNode;
  readonly onClick: () => void;
  readonly danger?: boolean;
}) {
  return (
    <Button
      type="button"
      variant="ghost"
      size="sm"
      role="menuitem"
      onClick={onClick}
      className={`w-full justify-start rounded-lg px-3 text-[13px] font-medium ${
        danger
          ? 'text-danger hover:bg-danger/15 hover:text-danger'
          : 'text-text hover:bg-surface-3 hover:text-text'
      }`}
    >
      {icon}
      {label}
    </Button>
  );
}
