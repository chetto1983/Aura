import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { postJSON } from '../api/json';

export type CompactionKind = 'compaction' | 'l1_offload' | 'l2_5_loss';

export interface CompactionEntry {
  readonly checkpoint_id: string;
  readonly kind: CompactionKind;
  readonly trigger: string;
  readonly reason: string;
  readonly token_delta: number;
  readonly quality: 'passed' | 'degraded' | 'corrupt' | 'quarantined';
  readonly model?: string;
  readonly manifest?: readonly number[];
}

export interface CompactionOutcome {
  readonly operationId: string;
  readonly checkpointId?: string;
  readonly priorCheckpointId?: string;
  readonly summary?: string;
  readonly status: string;
  readonly activated: boolean;
  readonly entries?: readonly CompactionEntry[];
}

export const COMPACTION_KEY = 'compaction-history';

function operationId(): string {
  return globalThis.crypto.randomUUID();
}

export function useCompactionHistory(conversationId: string) {
  return useQuery({
    queryKey: [COMPACTION_KEY, conversationId],
    queryFn: () =>
      postJSON<CompactionOutcome>(
        `/api/conversations/${encodeURIComponent(conversationId)}/compact/history`,
        { operationId: operationId() },
      ),
    enabled: conversationId.length > 0,
    retry: false,
  });
}

export function usePreviewCompaction(conversationId: string) {
  return useMutation({
    mutationFn: (checkpointId: string) =>
      postJSON<CompactionOutcome>(
        `/api/conversations/${encodeURIComponent(conversationId)}/compact/preview`,
        { operationId: operationId(), checkpointId },
      ),
  });
}

export function useRestoreCompaction(conversationId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (checkpointId: string) =>
      postJSON<CompactionOutcome>(
        `/api/conversations/${encodeURIComponent(conversationId)}/compact/restore`,
        { operationId: operationId(), checkpointId, safePoint: true },
      ),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: [COMPACTION_KEY, conversationId] });
    },
  });
}
