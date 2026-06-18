import { useEffect, useRef, useState } from 'react';
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
        <label className="flex cursor-pointer items-center gap-1.5 text-[0.75rem] text-text-muted">
          <input
            type="checkbox"
            checked={includeArchived}
            onChange={(event) => {
              setIncludeArchived(event.target.checked);
            }}
            className="h-3.5 w-3.5 accent-accent outline-none focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
          />
          {t('conversations.includeArchived')}
        </label>
      </div>

      {isPending ? (
        <p className="text-sm text-text-muted">{t('conversations.loading')}</p>
      ) : isError ? (
        <p role="alert" className="text-sm text-danger">
          {t('conversations.loadError')}
        </p>
      ) : conversations.length === 0 ? (
        <div className="py-4">
          <p className="text-sm font-medium text-text">{t('conversations.empty.heading')}</p>
          <p className="mt-1 text-[0.8125rem] text-text-muted">{t('conversations.empty.body')}</p>
        </div>
      ) : (
        <ul className="flex min-h-0 flex-1 flex-col gap-2 overflow-y-auto">
          {groups.map((group) => (
            <li key={group.key} className="flex flex-col gap-1">
              <h3 className="px-2 text-[0.75rem] font-medium uppercase tracking-wider text-text-faint">
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
  const inputRef = useRef<HTMLInputElement>(null);

  const archived = isArchived(conv);
  const label = displayTitle(conv, t('conversations.untitled'));

  useEffect(() => {
    if (editing) {
      inputRef.current?.focus();
      inputRef.current?.select();
    }
  }, [editing]);

  function startEditing() {
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
    <li
      className={`group rounded-[var(--radius-md)] border px-2 py-1.5 ${
        selected
          ? 'border-l-2 border-l-accent border-border bg-surface-2'
          : 'border-transparent hover:border-border hover:bg-surface-2/60'
      }`}
      data-animate="surface"
    >
      {editing ? (
        <input
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
          className="min-h-[var(--row-h)] w-full rounded-[var(--radius-sm)] border border-border bg-surface px-2 text-sm text-text outline-none focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
        />
      ) : (
        <button
          type="button"
          onClick={() => {
            onSelect(conv.ID);
          }}
          onDoubleClick={startEditing}
          aria-current={selected ? 'true' : undefined}
          className="flex w-full items-center gap-2 truncate text-left text-sm text-text outline-none focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
        >
          {archived ? (
            <span className="shrink-0 text-[0.75rem] uppercase tracking-wide text-text-faint">
              {t('conversations.archivedTag')}
            </span>
          ) : null}
          <span className="truncate">{label}</span>
        </button>
      )}

      {!editing ? (
        <div className="mt-1 flex flex-wrap gap-x-3 gap-y-0.5 text-[0.75rem] text-text-muted opacity-100 transition-opacity md:opacity-0 md:focus-within:opacity-100 md:group-hover:opacity-100">
          <RowAction label={t('conversations.actions.rename')} onClick={startEditing} />
          {archived ? (
            <RowAction label={t('conversations.actions.unarchive')} onClick={onUnarchive} />
          ) : (
            <RowAction label={t('conversations.actions.archive')} onClick={onArchive} />
          )}
          <RowAction label={t('conversations.actions.delete')} onClick={onRequestDelete} danger />
        </div>
      ) : null}
    </li>
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

function RowAction({
  label,
  onClick,
  danger,
}: {
  readonly label: string;
  readonly onClick: () => void;
  readonly danger?: boolean;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`rounded-sm outline-none hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent ${
        danger ? 'text-danger' : 'text-text-muted hover:text-text'
      }`}
    >
      {label}
    </button>
  );
}
