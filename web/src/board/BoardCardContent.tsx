import { useContext } from 'react';
import { Bot, CalendarClock, MessageSquareText, Moon, UserRound } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import type { KanbanCard } from '@svar-ui/react-kanban';
import type { CardSource, WidgetCard } from './boardApi';
import { BoardCardContext } from './boardCardContext';

const sourceIcons = {
  cockpit: UserRound,
  chat: Bot,
  background: Moon,
} satisfies Record<CardSource, typeof UserRound>;

const priorityTone: Record<number, string> = {
  1: 'bg-info',
  2: 'bg-warning',
  3: 'bg-danger',
};

/**
 * The Aura card, rendered inside the widget's own card shell. Nothing on it is a link: the
 * widget handles clicks on the board element itself, so any click a card does not mark as
 * `data-action="menu"` opens the editor, and the conversation and task live there instead.
 * The menu button keeps the widget's attribute, which is how its context menu finds it.
 */
export function BoardCardContent(props: { readonly card: KanbanCard }) {
  // The widget types a card as an id plus anything; this board's cards are the ones it loaded.
  const card = props.card as WidgetCard;
  const { t, i18n } = useTranslation();
  const { taskStatus, now } = useContext(BoardCardContext);
  const SourceIcon = sourceIcons[card.source];
  const overdue = card.deadline !== undefined && card.deadline.getTime() < now.getTime();
  const status = card.task_id === undefined ? undefined : taskStatus(card.task_id);

  return (
    <div className="flex flex-col gap-1.5 p-2.5 text-left">
      <div className="flex items-start gap-2">
        <span
          role="img"
          aria-label={t(`board.priority.${String(card.priority)}`)}
          className={`mt-1.5 h-2 w-2 shrink-0 rounded-full ${priorityTone[card.priority] ?? 'bg-border'}`}
        />
        <p className="min-w-0 flex-1 text-[14px] leading-snug font-medium break-words text-text">
          {card.label}
        </p>
        <button
          type="button"
          data-action="menu"
          aria-label={t('board.card.menu')}
          className="-mt-0.5 -mr-1 grid h-6 w-6 shrink-0 place-items-center rounded text-text-muted hover:bg-surface-2"
        >
          <i className="wx-icon wxi-dots-h" aria-hidden="true" />
        </button>
      </div>
      {card.description !== '' && (
        <p className="line-clamp-2 text-[13px] leading-snug text-text-muted">{card.description}</p>
      )}
      {card.tags.length > 0 && (
        <ul className="flex flex-wrap gap-1" aria-label={t('board.card.tags')}>
          {card.tags.map((tag) => (
            <li
              key={tag}
              className="rounded bg-surface-2 px-1.5 py-0.5 text-[11px] text-text-muted"
            >
              {tag}
            </li>
          ))}
        </ul>
      )}
      <div className="flex flex-wrap items-center gap-x-2.5 gap-y-1 text-[11px] text-text-muted">
        <span className="inline-flex items-center gap-1">
          <SourceIcon aria-hidden="true" className="h-3 w-3" />
          {t(`board.source.${card.source}`)}
        </span>
        {card.deadline !== undefined && (
          <span
            className={`inline-flex items-center gap-1 ${overdue ? 'font-semibold text-danger' : ''}`}
          >
            <CalendarClock aria-hidden="true" className="h-3 w-3" />
            {new Intl.DateTimeFormat(i18n.language, { day: 'numeric', month: 'short' }).format(
              card.deadline,
            )}
            {overdue && <span className="sr-only">{t('board.card.overdue')}</span>}
          </span>
        )}
        {card.conversation_id !== undefined && (
          <span className="inline-flex items-center gap-1" title={t('board.card.hasConversation')}>
            <MessageSquareText aria-hidden="true" className="h-3 w-3" />
            <span className="sr-only">{t('board.card.hasConversation')}</span>
          </span>
        )}
        {card.task_id !== undefined && (
          <span className="rounded border border-border px-1 py-px">
            {t('board.card.task', { status: status ?? t('board.card.taskUnknown') })}
          </span>
        )}
      </div>
    </div>
  );
}
