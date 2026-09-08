import { useId } from 'react';
import { Archive, ListChecks, RotateCcw, Trash2, X } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { displayTitle, type Conversation } from './useConversations';
import type { useConversationSelection } from './useConversationSelection';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import { ConfirmDialog } from '@/components/ui/confirm-dialog';
import { Label } from '@/components/ui/label';

export function ConversationBulkActions({
  selection,
  total,
}: {
  readonly selection: ReturnType<typeof useConversationSelection>;
  readonly total: number;
}) {
  const { t } = useTranslation();
  const selectAllId = useId();
  const count = selection.ids.length;
  const result = selection.result;
  return (
    <div className="flex flex-col gap-2">
      {selection.selecting ? (
        <div
          className="rounded-md border border-border bg-surface-2 p-2"
          aria-busy={selection.pending}
        >
          <div className="flex items-center gap-1">
            <Checkbox
              id={selectAllId}
              checked={count > 0 && count === total ? true : count > 0 ? 'indeterminate' : false}
              disabled={selection.pending || total === 0}
              onCheckedChange={selection.toggleAll}
              aria-label={t('conversations.bulk.selectAll')}
              className="data-[state=indeterminate]:bg-primary"
            />
            <Label htmlFor={selectAllId} className="flex-1 text-xs font-normal">
              {t('conversations.bulk.selected', { count })}
            </Label>
            <Button
              variant="ghost"
              size="icon"
              disabled={selection.pending}
              onClick={selection.reset}
              aria-label={t('conversations.bulk.cancel')}
            >
              <X aria-hidden="true" />
            </Button>
          </div>
          <div className="flex flex-wrap gap-1">
            <Button
              variant="ghost"
              size="sm"
              disabled={selection.pending || selection.activeIds.length === 0}
              onClick={() => {
                void selection.apply('archive', selection.activeIds);
              }}
            >
              <Archive aria-hidden="true" />
              {t('conversations.actions.archive')}
            </Button>
            {selection.archivedIds.length > 0 ? (
              <Button
                variant="ghost"
                size="sm"
                disabled={selection.pending}
                onClick={() => {
                  void selection.apply('unarchive', selection.archivedIds);
                }}
              >
                <RotateCcw aria-hidden="true" />
                {t('conversations.actions.unarchive')}
              </Button>
            ) : null}
            <Button
              variant="ghost"
              size="sm"
              className="text-danger hover:text-danger"
              disabled={selection.pending || count === 0}
              onClick={selection.requestDelete}
            >
              <Trash2 aria-hidden="true" />
              {t('conversations.bulk.delete')}
            </Button>
          </div>
        </div>
      ) : total > 0 ? (
        <Button
          variant="ghost"
          size="sm"
          className="self-start text-text-muted"
          onClick={selection.start}
        >
          <ListChecks aria-hidden="true" />
          {t('conversations.bulk.select')}
        </Button>
      ) : null}
      {selection.pending ? (
        <p role="status" className="px-2 text-xs text-text-muted">
          {t('conversations.bulk.pending')}
        </p>
      ) : null}
      {result && !selection.pending ? (
        <p
          role={result.failed.length > 0 ? 'alert' : 'status'}
          className={`px-2 text-xs ${result.failed.length > 0 ? 'text-danger' : 'text-text-muted'}`}
        >
          {t(
            result.action === 'archive'
              ? 'conversations.bulk.archived'
              : result.action === 'unarchive'
                ? 'conversations.bulk.restored'
                : 'conversations.bulk.deleted',
            { count: result.succeeded.length },
          )}
          {result.failed.length > 0
            ? ` ${t('conversations.bulk.failed', { count: result.failed.length })}`
            : ''}
        </p>
      ) : null}
      <ConfirmDialog
        open={selection.deleteTargets !== null}
        onOpenChange={(open) => {
          if (!open) selection.cancelDelete();
        }}
        title={t('conversations.bulk.deleteTitle', { count: selection.deleteTargets?.length ?? 0 })}
        description={t('conversations.bulk.deleteBody', {
          count: selection.deleteTargets?.length ?? 0,
        })}
        cancelLabel={t('conversations.bulk.keep')}
        confirmLabel={t('conversations.delete.confirm')}
        confirmPending={selection.pending}
        onConfirm={() => selection.apply('delete', selection.deleteTargets ?? [])}
      />
    </div>
  );
}

export function ConversationSelectionRow({
  conversation,
  checked,
  disabled,
  onToggle,
}: {
  readonly conversation: Conversation;
  readonly checked: boolean;
  readonly disabled: boolean;
  readonly onToggle: () => void;
}) {
  const { t } = useTranslation();
  const title = displayTitle(conversation, t('conversations.untitled'));
  const id = useId();
  return (
    <li className={`rounded-md ${checked ? 'bg-surface-2' : 'hover:bg-surface-2/70'}`}>
      <Label
        htmlFor={id}
        className="flex min-h-11 cursor-pointer items-center gap-1 px-1 text-[13px]"
      >
        <Checkbox
          id={id}
          checked={checked}
          disabled={disabled}
          onCheckedChange={onToggle}
          aria-label={t('conversations.bulk.selectConversation', { title })}
        />
        <span className="min-w-0 truncate" title={title}>
          {title}
        </span>
      </Label>
    </li>
  );
}
