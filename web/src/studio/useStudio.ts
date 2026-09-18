import { useEffect } from 'react';
import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
  type InfiniteData,
} from '@tanstack/react-query';
import {
  STUDIO_HISTORY_LIMIT,
  createStudioImage,
  createStudioVideo,
  fetchStudioModels,
  listStudioHistory,
  listStudioLibrary,
  type StudioImageBody,
  type StudioKind,
  type StudioRecord,
  type StudioVideoBody,
} from './studioApi';
import { isActive } from './studioForm';

// useStudio.ts — the Studio's React Query layer. The page reads models, history and library
// through these and never calls studioApi directly, so the cache keys live in one place.

/** How often the history re-reads itself while something is still generating. A submitted clip
 *  has no push channel: the row only changes when the watcher polls the provider and the page
 *  asks again. */
export const STUDIO_HISTORY_POLL_MS = 5000;

const MODELS_STALE_MS = 5 * 60_000;
const HISTORY_ROOT = ['studio', 'history'] as const;

export const studioKeys = {
  models: (kind: StudioKind) => ['studio', 'models', kind] as const,
  history: (kind: StudioKind | undefined) => [...HISTORY_ROOT, kind ?? 'all'] as const,
  library: () => ['studio', 'library'] as const,
};

/** True while any loaded page still holds a generation that has not finished. */
export function hasActiveRecord(pages: readonly (readonly StudioRecord[])[] | undefined): boolean {
  return (pages ?? []).some((page) => page.some(isActive));
}

export function useStudioModels(kind: StudioKind) {
  return useQuery({
    queryKey: studioKeys.models(kind),
    queryFn: ({ signal }) => fetchStudioModels(kind, signal),
    // The catalog changes when the deployment's key or settings change, not between renders.
    staleTime: MODELS_STALE_MS,
  });
}

type StudioHistoryData = InfiniteData<readonly StudioRecord[], string | undefined>;

/** The identity's generations, newest first, paged on the oldest row of a full page.
 *
 *  While a listed record is active, the FIRST page re-reads itself every 5 s and replaces
 *  itself in the cache. Only the first page: a generation that has not finished is always the
 *  newest row, and the query's own refetchInterval would re-request every page the operator has
 *  scrolled through — history that cannot change, once per tick, for as long as they watch. */
export function useStudioHistory(kind: StudioKind | undefined) {
  const client = useQueryClient();
  const query = useInfiniteQuery({
    queryKey: studioKeys.history(kind),
    queryFn: ({ pageParam, signal }) => listStudioHistory(kind, pageParam, signal),
    initialPageParam: undefined as string | undefined,
    // A page shorter than the server's own size is the last one.
    getNextPageParam: (lastPage: readonly StudioRecord[]) =>
      lastPage.length < STUDIO_HISTORY_LIMIT ? undefined : lastPage.at(-1)?.id,
  });
  const polling = hasActiveRecord(query.data?.pages);
  useEffect(() => {
    if (!polling) return;
    const key = studioKeys.history(kind);
    const controller = new AbortController();
    const tick = async () => {
      let page: readonly StudioRecord[];
      try {
        page = await listStudioHistory(kind, undefined, controller.signal);
      } catch {
        // Explicitly silenced: the next tick is 5 s away, the panel keeps the rows it has, and
        // a failure that is not transient surfaces through the query's own refetch path.
        return;
      }
      client.setQueryData(key, (old: StudioHistoryData | undefined) =>
        old === undefined ? old : { ...old, pages: [page, ...old.pages.slice(1)] },
      );
    };
    const timer = setInterval(() => void tick(), STUDIO_HISTORY_POLL_MS);
    return () => {
      clearInterval(timer);
      controller.abort();
    };
  }, [polling, kind, client]);
  return query;
}

/** Both create routes answer the accepted record, and both make every history list stale — a
 *  new generation belongs to the kind's list and to the unfiltered one. */
function useCreateStudio<B>(create: (body: B) => Promise<StudioRecord>) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: create,
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: HISTORY_ROOT });
    },
  });
}

export function useCreateStudioVideo() {
  return useCreateStudio<StudioVideoBody>(createStudioVideo);
}

export function useCreateStudioImage() {
  return useCreateStudio<StudioImageBody>(createStudioImage);
}

/** The identity's recent images, for the frame and reference pickers. Read only while a picker
 *  is open: the page itself never needs the list. */
export function useStudioLibrary(enabled: boolean) {
  return useQuery({
    queryKey: studioKeys.library(),
    queryFn: ({ signal }) => listStudioLibrary(signal),
    enabled,
  });
}
