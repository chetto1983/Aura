import { useState } from 'react';
import { AlertCircle, MessageSquareText } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { ConversationRow } from './ConversationRow';
import { ConversationBulkActions, ConversationSelectionRow } from './ConversationBulkActions';
import { useConversationSelection } from './useConversationSelection';
import { DeleteConfirmDialog } from './DeleteConfirmDialog';
import {
  displayTitle,
  useArchiveConversation,
  useConversations,
  useDeleteConversation,
  useRenameConversation,
  useUnarchiveConversation,
  type Conversation,
} from './useConversations';
import { SkeletonBlock } from '@/components/skeleton';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Card } from '@/components/ui/card';
import { Checkbox } from '@/components/ui/checkbox';
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty';
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
  /** Fires after a hard delete SUCCEEDS, with the deleted id — AppShell resets
   *  the main pane when the ACTIVE conversation was deleted (operator report:
   *  the pane kept rendering a deleted thread and sends hit thread-not-found). */
  readonly onDeleted?: (id: string) => void;
}

export function ConversationSidebar({ activeId, onSelect, onDeleted }: ConversationSidebarProps) {
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
  const selection = useConversationSelection(conversations, onDeleted);

  function confirmDelete() {
    const target = pendingDelete;
    if (!target) return;
    remove.mutate(target.ID, {
      // Only a CONFIRMED backend delete resets the pane — a failed DELETE keeps
      // the conversation (and the pane) intact.
      onSuccess: () => onDeleted?.(target.ID),
    });
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
            disabled={selection.pending}
            onCheckedChange={(checked) => {
              selection.reset();
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

      <ConversationBulkActions selection={selection} total={conversations.length} />

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
                {group.items.map((conv) =>
                  selection.selecting ? (
                    <ConversationSelectionRow
                      key={conv.ID}
                      conversation={conv}
                      checked={selection.selected.has(conv.ID)}
                      disabled={selection.pending}
                      onToggle={() => {
                        selection.toggle(conv.ID);
                      }}
                    />
                  ) : (
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
                  ),
                )}
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

function ConversationListSkeleton({ label }: { readonly label: string }) {
  return (
    <div role="status" aria-label={label} className="flex flex-col gap-2">
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
