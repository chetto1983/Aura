import { documentsBase } from './documentsApi';
import { getJSON, sendJSON } from '@/api/json';

export type DocumentDisplay = 'table' | 'compact' | 'cards';
export interface SavedView {
  readonly id: string;
  readonly name: string;
  readonly version: 1;
  readonly query: string;
  readonly display: DocumentDisplay;
  readonly pinned: boolean;
  readonly dashboard: boolean;
}

export async function fetchViews(signal?: AbortSignal): Promise<readonly SavedView[]> {
  return (await getJSON<{ views: SavedView[] }>(`${documentsBase}/views`, signal)).views;
}

export async function saveView(view: Omit<SavedView, 'id'>, id?: string): Promise<SavedView> {
  return (
    await sendJSON<{ view: SavedView }>(
      id ? 'PUT' : 'POST',
      `${documentsBase}/views${id ? `/${encodeURIComponent(id)}` : ''}`,
      view,
    )
  ).view;
}

export async function deleteView(id: string): Promise<void> {
  await sendJSON('DELETE', `${documentsBase}/views/${encodeURIComponent(id)}`);
}

export interface IngestionState {
  readonly status: string;
  readonly observed_at?: string;
  readonly in_progress: number;
  readonly finished: number;
  readonly errors: number;
}

export async function fetchIngestion(signal?: AbortSignal): Promise<IngestionState | null> {
  return (await getJSON<{ indexing: IngestionState | null }>(`${documentsBase}/ingestion`, signal))
    .indexing;
}
