import { RestDataProvider } from '@svar-ui/react-kanban';
import { getJSON, HttpError, sendJSON } from '@/api/json';

/**
 * The mount the board talks to (internal/agui/board_api.go). It speaks SVAR Kanban's own REST
 * dialect, so the widget's RestDataProvider issues every card write with no request-building
 * code here; this file only adds what the provider leaves out.
 */
export const boardBase = '/api/board';

export const BOARD_QUERY_KEY = ['board'] as const;
export const BOARD_VIEWS_QUERY_KEY = ['board-views'] as const;

export type CardSource = 'cockpit' | 'chat' | 'background';

export interface BoardColumn {
  readonly id: string;
  readonly label: string;
  readonly cardLimit?: number;
}

/** A card on the wire: SVAR's fields plus Aura's. deadline is RFC 3339 here; the widget wants
 * a Date, which withDates supplies. */
export interface BoardCard {
  readonly id: string;
  readonly label: string;
  readonly description: string;
  readonly column: string;
  readonly priority: number;
  readonly tags: readonly string[];
  readonly deadline?: string;
  readonly conversation_id?: string;
  readonly task_id?: string;
  readonly source: CardSource;
  readonly updated_by: 'operator' | 'agent';
  readonly updated_at: string;
}

export interface BoardData {
  readonly name: string;
  readonly columns: readonly BoardColumn[];
  readonly cards: readonly BoardCard[];
}

export interface BoardFilters {
  readonly source?: CardSource;
  readonly priority?: number;
  readonly tag?: string;
  readonly due?: 'overdue' | 'week';
}

export interface BoardView {
  readonly id: string;
  readonly name: string;
  readonly filters: BoardFilters;
  readonly pinned: boolean;
}

/** One query for the columns and the cards together: the widget re-initialises its store from
 * both props whenever either changes, so a column edit must arrive with fresh cards or it
 * would put back every card where it was when the board was last loaded. */
export async function fetchBoard(): Promise<BoardData> {
  const [board, cards] = await Promise.all([
    getJSON<{ name: string; columns: BoardColumn[] }>(boardBase),
    getJSON<BoardCard[]>(`${boardBase}/cards`),
  ]);
  return { name: board.name, columns: board.columns, cards };
}

export function saveColumns(columns: readonly BoardColumn[]): Promise<unknown> {
  return sendJSON('PUT', `${boardBase}/columns`, columns);
}

export function fetchViews(): Promise<BoardView[]> {
  return getJSON<BoardView[]>(`${boardBase}/views`);
}

export function saveView(view: Omit<BoardView, 'id'>): Promise<BoardView> {
  return sendJSON<BoardView>('PUT', `${boardBase}/views`, view);
}

export function deleteView(id: string): Promise<unknown> {
  return sendJSON('DELETE', `${boardBase}/views/${encodeURIComponent(id)}`);
}

/** The widget's card: the wire card with its deadline as the Date the editor's picker needs. */
export type WidgetCard = Omit<BoardCard, 'deadline'> & { readonly deadline?: Date };

export function withDates(cards: readonly BoardCard[]): WidgetCard[] {
  return cards.map(({ deadline, ...card }) =>
    deadline === undefined ? card : { ...card, deadline: new Date(deadline) },
  );
}

/** Why the board must be reloaded: a write the server refused (its `{error}` code), or a
 * duplicate, whose copy the widget can never learn the id of. */
export type Resync = (reason?: string) => void;

/**
 * The provider, with the status check its author left out.
 *
 * Rest.sendRequest is `fetch(...).then(res => res.json())`: a refused write reads as success,
 * so the board would show a move the server never made. This send goes through sendJSON,
 * which throws on a non-2xx with the server's code; the board then says why and reloads, and
 * the send resolves to {} so the widget's queue never hangs on it.
 *
 * A duplicate also reloads. The store makes the copy's temporary id inside its own handler and
 * never writes it back to the event (measured, @svar-ui/kanban-store 2.6.0), so the queue maps
 * nothing and a later edit of the copy would wait forever for an id that never comes.
 */
class CheckedRestDataProvider extends RestDataProvider {
  private readonly resync: Resync;

  constructor(resync: Resync) {
    super(boardBase);
    this.resync = resync;
  }

  // Shadows Rest.send, which the package's types do not declare.
  async send(url: string, method: string, data?: unknown): Promise<unknown> {
    try {
      const res = await sendJSON(method, `${boardBase}/${url}`, data);
      if (url.endsWith('/duplicate')) this.resync();
      return res;
    } catch (err) {
      this.resync(err instanceof HttpError && err.reason !== '' ? err.reason : 'generic');
      return {};
    }
  }
}

export function createBoardProvider(resync: Resync): RestDataProvider {
  return new CheckedRestDataProvider(resync);
}
