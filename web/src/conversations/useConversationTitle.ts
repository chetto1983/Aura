import { useEffect } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { getJSON, HttpError } from '../api/json';
import { CONVERSATION_KEY, CONVERSATIONS_KEY } from './useConversations';

export function useConversationTitle(conversationId: string, enabled: boolean) {
  const client = useQueryClient();
  const title = useQuery({
    queryKey: [CONVERSATION_KEY, conversationId, 'title'],
    queryFn: () =>
      getJSON<{ title: string }>(`/api/conversations/${encodeURIComponent(conversationId)}/title`),
    enabled: enabled && conversationId.length > 0,
    staleTime: Infinity,
    retry: (count, error) => error instanceof HttpError && error.status === 404 && count < 8,
    retryDelay: 5000,
  });
  useEffect(() => {
    if (!title.data?.title) return;
    // Re-read authoritative rows rather than overwrite a concurrent manual rename
    // with the title response. Exact matching avoids invalidating this child query.
    void client.invalidateQueries({ queryKey: [CONVERSATION_KEY, conversationId], exact: true });
    void client.invalidateQueries({ queryKey: [CONVERSATIONS_KEY] });
  }, [client, conversationId, title.data]);
  return title;
}
