import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { AssistantRuntime } from '@assistant-ui/react';
import { useDataStreamRuntime } from '@assistant-ui/react-data-stream';
import { clearToken, getToken } from '@/lib/auth';

const THREAD_QUERY_PARAM = 'thread_id';

function createThreadID(): string {
  const random =
    typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function'
      ? crypto.randomUUID()
      : `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
  return `web_${random}`;
}

function readThreadIDFromURL(): string | null {
  if (typeof window === 'undefined') return null;
  return new URL(window.location.href).searchParams.get(THREAD_QUERY_PARAM);
}

function writeThreadIDToURL(threadID: string) {
  if (typeof window === 'undefined') return;
  const url = new URL(window.location.href);
  if (url.searchParams.get(THREAD_QUERY_PARAM) === threadID) return;
  url.searchParams.set(THREAD_QUERY_PARAM, threadID);
  window.history.replaceState(null, '', `${url.pathname}${url.search}${url.hash}`);
}

async function auraStreamHeaders(): Promise<Record<string, string>> {
  const token = getToken();
  return token ? { Authorization: `Bearer ${token}` } : {};
}

async function handleStreamResponse(response: Response) {
  if (response.status !== 401) return;
  clearToken();
  window.location.assign('/login');
}

export function useAuraChatThreadID(): [string, () => string] {
  const [threadID, setThreadID] = useState(() => readThreadIDFromURL() ?? createThreadID());

  useEffect(() => {
    writeThreadIDToURL(threadID);
  }, [threadID]);

  const startNewThread = useCallback(() => {
    const next = createThreadID();
    writeThreadIDToURL(next);
    setThreadID(next);
    return next;
  }, []);

  return [threadID, startNewThread];
}

export function useAuraAssistantRuntime(threadID: string): AssistantRuntime {
  const threadIDRef = useRef(threadID);

  useEffect(() => {
    threadIDRef.current = threadID;
  }, [threadID]);

  const options = useMemo(
    () => ({
      api: '/api/chat/stream',
      protocol: 'ui-message-stream' as const,
      headers: auraStreamHeaders,
      onResponse: handleStreamResponse,
      body: async () => ({ thread_id: threadIDRef.current }),
    }),
    [],
  );

  return useDataStreamRuntime(options);
}
