import { useEffect, useRef } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { fetchIngestion, fetchViews } from './workspaceApi';

export function useSavedViews(identity: string) {
  return useQuery({
    queryKey: ['documents-views', identity],
    queryFn: ({ signal }) => fetchViews(signal),
    enabled: !!identity,
  });
}

export function useIngestion(identity: string) {
  const client = useQueryClient();
  const previous = useRef<{ identity: string; signature: string } | null>(null);
  const query = useQuery({
    queryKey: ['documents-ingestion', identity],
    queryFn: ({ signal }) => fetchIngestion(signal),
    enabled: !!identity,
    refetchInterval: 5000,
    retry: false,
  });
  useEffect(() => {
    if (query.data === undefined) return;
    const signature = JSON.stringify(query.data);
    const before = previous.current;
    previous.current = { identity, signature };
    if (before?.identity !== identity || before.signature === signature) return;
    void client.invalidateQueries({ queryKey: ['documents', identity] });
    void client.invalidateQueries({ queryKey: ['document', identity] });
    void client.invalidateQueries({ queryKey: ['documents-content', identity] });
  }, [client, identity, query.data]);
  return query;
}
