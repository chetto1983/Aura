import { useCallback } from 'react';
import { useQueryClient, type QueryClient } from '@tanstack/react-query';
import {
  CONVERSATION_KEY,
  fetchConversation,
  type Conversation,
} from '../conversations/useConversations';
import { seedSession } from './footerMetrics';
import type { RunSessionBaselineEvent } from './runSessionUsage';

export type RunSessionBaselineListener = (event: RunSessionBaselineEvent) => void;

export function useRunUsageBaseline(onBaseline?: RunSessionBaselineListener) {
  const queryClient = useQueryClient();
  return useCallback(
    (conversationId: string, runId: number, signal: AbortSignal) =>
      publishRunSessionBaseline(queryClient, conversationId, runId, signal, onBaseline),
    [onBaseline, queryClient],
  );
}

async function publishRunSessionBaseline(
  queryClient: QueryClient,
  conversationId: string,
  runId: number,
  signal: AbortSignal,
  onBaseline: RunSessionBaselineListener | undefined,
): Promise<boolean> {
  const load = queryClient.ensureQueryData({
    queryKey: [CONVERSATION_KEY, conversationId],
    queryFn: () => fetchConversation(conversationId),
    retry: false,
  });
  const outcome = await settleOrAbort(load, signal);
  if (outcome.kind === 'aborted') return false;
  onBaseline?.({
    runId,
    baseline: outcome.kind === 'ready' ? seedSession(outcome.conversation) : null,
  });
  return true;
}

type LoadOutcome =
  | { readonly kind: 'ready'; readonly conversation: Conversation }
  | { readonly kind: 'failed' }
  | { readonly kind: 'aborted' };

function settleOrAbort(load: Promise<Conversation>, signal: AbortSignal): Promise<LoadOutcome> {
  return new Promise((resolve) => {
    let settled = false;
    const finish = (outcome: LoadOutcome) => {
      if (settled) return;
      settled = true;
      signal.removeEventListener('abort', abort);
      resolve(outcome);
    };
    const abort = () => {
      finish({ kind: 'aborted' });
    };
    if (signal.aborted) {
      abort();
      return;
    }
    signal.addEventListener('abort', abort, { once: true });
    void load.then(
      (conversation) => {
        finish({ kind: 'ready', conversation });
      },
      () => {
        finish({ kind: 'failed' });
      },
    );
  });
}
