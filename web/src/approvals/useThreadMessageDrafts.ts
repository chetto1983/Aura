import { useCallback, useEffect } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

export type MessageDraftStatus =
  'pending' | 'dispatching' | 'sent' | 'failed' | 'declined' | 'uncertain' | 'expired';

export interface MessageDraft {
  readonly id: string;
  readonly conversation_id: string;
  readonly channel: 'email' | 'whatsapp';
  readonly status: MessageDraftStatus;
  readonly arguments: Record<string, unknown>;
  readonly effective_arguments?: Record<string, unknown>;
  readonly expires_at: string;
}

export interface MessageDraftResolution {
  readonly status: MessageDraftStatus;
  readonly outcome: 'continue' | 'pending' | 'approved' | 'rejected' | 'terminated';
  readonly remaining: number;
}

export interface ResolveMessageDraft {
  readonly id: string;
  readonly action: 'send' | 'decline';
  readonly overrides?: Readonly<Record<string, string | readonly string[]>>;
}

export const MESSAGE_DRAFTS_KEY = 'message-drafts';

async function fetchMessageDrafts(threadId: string): Promise<MessageDraft[]> {
  const response = await fetch(
    `/api/message-drafts?conversation_id=${encodeURIComponent(threadId)}`,
    { credentials: 'same-origin', headers: { Accept: 'application/json' } },
  );
  if (!response.ok) throw new Error(`HTTP ${String(response.status)}`);
  const data: unknown = await response.json();
  return Array.isArray(data) ? (data as MessageDraft[]) : [];
}

async function postMessageDraftResolution(
  input: ResolveMessageDraft,
): Promise<MessageDraftResolution> {
  const response = await fetch(`/api/message-drafts/${encodeURIComponent(input.id)}/resolve`, {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(
      input.action === 'send'
        ? { action: 'send', overrides: input.overrides ?? {} }
        : { action: 'decline' },
    ),
  });
  if (!response.ok) throw new Error(`HTTP ${String(response.status)}`);
  return (await response.json()) as MessageDraftResolution;
}

export function useThreadMessageDrafts(
  threadId: string,
  isRunning: boolean,
  resumeRun: () => Promise<void>,
) {
  const queryClient = useQueryClient();
  const query = useQuery({
    queryKey: [MESSAGE_DRAFTS_KEY, threadId],
    queryFn: () => fetchMessageDrafts(threadId),
    enabled: threadId.length > 0,
    refetchInterval: 3000,
    retry: false,
  });
  useEffect(() => {
    if (!isRunning && threadId.length > 0) {
      void queryClient.invalidateQueries({ queryKey: [MESSAGE_DRAFTS_KEY, threadId] });
    }
  }, [isRunning, queryClient, threadId]);
  const mutation = useMutation({
    mutationFn: postMessageDraftResolution,
    onSuccess: async (resolution) => {
      await queryClient.invalidateQueries({ queryKey: [MESSAGE_DRAFTS_KEY, threadId] });
      if (resolution.outcome === 'continue') await resumeRun();
    },
  });
  const resolve = useCallback(
    (input: ResolveMessageDraft) => mutation.mutateAsync(input),
    [mutation],
  );
  const drafts = query.data ?? [];
  return {
    drafts,
    isPending: drafts.length > 0,
    isResolving: mutation.isPending,
    error: mutation.error ?? query.error,
    resolve,
  };
}

export type ThreadMessageDrafts = ReturnType<typeof useThreadMessageDrafts>;
