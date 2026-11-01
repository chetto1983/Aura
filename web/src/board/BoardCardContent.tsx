import { useContext } from 'react';
import { Bot, CalendarClock, MessageSquareText, Moon, UserRound } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import type { KanbanCard } from '@svar-ui/react-kanban';
import type { CardSource, WidgetCard } from './boardApi';
import { BoardCardContext } from './boardCardContext';
import { Badge, type BadgeProps } from '@/components/ui/badge';

const sourceIcons = {
  cockpit: UserRound,
  chat: Bot,
  background: Moon,
} satisfies Record<CardSource, typeof UserRound>;

const priorityTone: Record<number, BadgeProps['variant']> = {
  1: 'info',
  2: 'warning',
  3: 'danger',
};
const TAG_TONES = ['info', 'success', 'warning', 'danger'] as const;

function tagTone(tag: string) {
  let hash = 0;
  for (const character of tag) hash = (hash * 31 + (character.codePointAt(0) ?? 0)) >>> 0;
  return TAG_TONES[hash % TAG_TONES.length];
}

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
  const progress = card.progress ?? 0;

  return (
    <div className="flex min-w-0 flex-col gap-2 p-3 text-left">
      <div className="flex flex-wrap items-center gap-2">
        <Badge
          variant={priorityTone[card.priority] ?? 'secondary'}
          className="border-transparent px-1.5 py-0.5 text-[11px] font-medium"
        >
          {t(`board.priority.${String(card.priority)}`)}
        </Badge>
        {card.deadline !== undefined && (
          <span
            className={`inline-flex items-center gap-1 text-[11px] ${overdue ? 'font-semibold text-danger' : 'text-text-muted'}`}
          >
            <CalendarClock aria-hidden="true" className="h-3 w-3" />
            {new Intl.DateTimeFormat(i18n.language, { day: 'numeric', month: 'short' }).format(
              card.deadline,
            )}
            {overdue && <span className="sr-only">{t('board.card.overdue')}</span>}
          </span>
        )}
        <button
          type="button"
          data-action="menu"
          aria-label={t('board.card.menu')}
          className="-mr-1 ml-auto grid h-7 w-7 shrink-0 place-items-center rounded text-text-muted hover:bg-surface-2 focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
        >
          <i className="wx-icon wxi-dots-h" aria-hidden="true" />
        </button>
      </div>
      <p className="min-w-0 text-[14px] leading-snug font-semibold break-words text-text">
        {card.label}
      </p>
      {card.description !== '' && (
        <p className="line-clamp-2 text-[13px] leading-snug text-text-muted">{card.description}</p>
      )}
      {card.tags.length > 0 && (
        <ul className="flex flex-wrap gap-1" aria-label={t('board.card.tags')}>
          {card.tags.map((tag) => (
            <Badge
              key={tag}
              asChild
              variant={tagTone(tag)}
              className="max-w-full rounded-full border-transparent px-2 py-0.5 text-[11px] font-medium break-all whitespace-normal"
            >
              <li>{tag}</li>
            </Badge>
          ))}
        </ul>
      )}
      <div className="flex items-center gap-2">
        <progress
          className="aura-board-progress min-w-0 flex-1"
          aria-label={t('board.card.progress')}
          max={1}
          value={progress}
        />
        <span className="w-9 shrink-0 text-right text-[11px] text-text-muted tabular-nums">
          {Math.round(progress * 100)}%
        </span>
      </div>
      <div className="flex flex-wrap items-center gap-x-2.5 gap-y-1 text-[11px] text-text-muted">
        <span className="inline-flex items-center gap-1">
          <SourceIcon aria-hidden="true" className="h-3 w-3" />
          {t(`board.source.${card.source}`)}
        </span>
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
