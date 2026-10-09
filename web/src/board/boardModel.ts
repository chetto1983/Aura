import type { KanbanCard } from '@svar-ui/react-kanban';
import type { BoardFilters, BoardView, WidgetCard } from './boardApi';

const DAY_MS = 24 * 60 * 60 * 1000;

/** A view's filters as the widget's predicate, or null when the view filters nothing. Every
 * filter that is set must hold; due compares against now, so a view re-applied tomorrow moves
 * the cards whose date has passed into "overdue" without anyone saving it again. */
export function filterPredicate(
  filters: BoardFilters,
  now: Date,
): ((card: KanbanCard) => boolean) | null {
  const { source, priority, tag, due } = filters;
  if (source === undefined && priority === undefined && tag === undefined && due === undefined) {
    return null;
  }
  const weekEnd = now.getTime() + 7 * DAY_MS;
  return (widgetCard) => {
    const card = widgetCard as WidgetCard;
    if (source !== undefined && card.source !== source) return false;
    if (priority !== undefined && card.priority !== priority) return false;
    if (tag !== undefined && !card.tags.includes(tag)) return false;
    if (due === undefined) return true;
    if (card.deadline === undefined) return false;
    const at = card.deadline.getTime();
    return due === 'overdue' ? at < now.getTime() : at >= now.getTime() && at <= weekEnd;
  };
}

/** Every tag on the board, sorted: the tag filter's options. */
export function boardTags(cards: readonly WidgetCard[]): string[] {
  return [...new Set(cards.flatMap((card) => card.tags))].sort((a, b) => a.localeCompare(b));
}

/** A comma-separated tag line as the card's tags: trimmed, without blanks or repeats. */
export function parseTags(text: string): string[] {
  return [
    ...new Set(
      text
        .split(',')
        .map((tag) => tag.trim())
        .filter((tag) => tag !== ''),
    ),
  ];
}

/** Pinned views first, then by name: the order the operator reaches for them. */
export function orderViews(views: readonly BoardView[]): BoardView[] {
  return [...views].sort(
    (a, b) => Number(b.pinned) - Number(a.pinned) || a.name.localeCompare(b.name),
  );
}
