import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  BOARD_QUERY_KEY,
  BOARD_VIEWS_QUERY_KEY,
  deleteView,
  fetchBoard,
  fetchViews,
  saveColumns,
  saveView,
  type BoardColumn,
  type BoardView,
} from './boardApi';

/** The board and its cards. Refetched on focus like the scheduler board: a card the agent adds
 * reaches a run's tab, never this one, because the cockpit's surfaces are mutually exclusive. */
export function useBoardData() {
  return useQuery({
    queryKey: BOARD_QUERY_KEY,
    queryFn: fetchBoard,
    retry: false,
    refetchOnWindowFocus: true,
  });
}

export function useSaveColumns() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (columns: readonly BoardColumn[]) => saveColumns(columns),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: BOARD_QUERY_KEY }),
  });
}

export function useBoardViews() {
  return useQuery({ queryKey: BOARD_VIEWS_QUERY_KEY, queryFn: fetchViews, retry: false });
}

export function useSaveView() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (view: Omit<BoardView, 'id'>) => saveView(view),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: BOARD_VIEWS_QUERY_KEY }),
  });
}

export function useDeleteView() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => deleteView(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: BOARD_VIEWS_QUERY_KEY }),
  });
}
