import { useMutation, useQueryClient } from '@tanstack/react-query';
import {
  approveSchedulerTask,
  cancelSchedulerTask,
  editSchedulerTask,
  runSchedulerTask,
  type SchedulerEditRequest,
} from './governanceApi';

// useSchedulerMutations holds the GOV-03 write hooks (approve / run / cancel / edit). Each
// mirrors the conversations-store mutation shape: a single fetch mutationFn that THROWS
// `Error("HTTP <n>")` on a non-200 (so the row surfaces a visible error, never a silent
// no-op), and an onSuccess that invalidates the board query so the list re-reads. The
// queryKey MUST match SchedulerBoard's useQuery key so an approve/run/cancel re-renders it.

export const SCHEDULER_QUERY_KEY = ['governance', 'scheduler', 'tasks'] as const;

function useInvalidateScheduler(): () => void {
  const queryClient = useQueryClient();
  return () => {
    void queryClient.invalidateQueries({ queryKey: SCHEDULER_QUERY_KEY });
  };
}

export function useApproveTask() {
  const invalidate = useInvalidateScheduler();
  return useMutation({
    mutationFn: (id: string) => approveSchedulerTask(id),
    onSuccess: invalidate,
  });
}

export function useRunTask() {
  const invalidate = useInvalidateScheduler();
  return useMutation({ mutationFn: (id: string) => runSchedulerTask(id), onSuccess: invalidate });
}

export function useCancelTask() {
  const invalidate = useInvalidateScheduler();
  return useMutation({
    mutationFn: (id: string) => cancelSchedulerTask(id),
    onSuccess: invalidate,
  });
}

export function useEditTask() {
  const invalidate = useInvalidateScheduler();
  return useMutation({
    mutationFn: ({ id, req }: { id: string; req: SchedulerEditRequest }) =>
      editSchedulerTask(id, req),
    onSuccess: invalidate,
  });
}
