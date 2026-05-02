import { useCallback, useState } from 'react';
import { MessageSquare, Download } from 'lucide-react';
import { Skeleton } from '@/components/ui/skeleton';
import { ErrorCard } from '@/components/common/ErrorCard';
import { ConversationDrawer } from '@/components/ConversationDrawer';
import { api } from '@/api';
import { useApi } from '@/hooks/useApi';
import type { ConversationTurn } from '@/types/api';

const ROLE_BADGE: Record<string, string> = {
  user: 'bg-blue-500/15 text-blue-600 dark:text-blue-400',
  assistant: 'bg-cyan-500/15 text-cyan-600 dark:text-cyan-400',
  tool: 'bg-muted text-muted-foreground',
};

export function ConversationsPanel() {
  const [chatId, setChatId] = useState('');
  const [dateFrom, setDateFrom] = useState('');
  const [dateTo, setDateTo] = useState('');
  const [hasTools, setHasTools] = useState(false);
  const [selectedId, setSelectedId] = useState<number | null>(null);

  const numericChatId = chatId.trim() !== '' ? parseInt(chatId, 10) : undefined;

  const fetcher = useCallback(
    () => api.conversations(numericChatId, 100, hasTools || undefined),
    [numericChatId, hasTools],
  );

  const { data, error, loading, refetch } = useApi<{ turns: ConversationTurn[] }>(fetcher);

  const turns = data?.turns ?? [];

  const filtered = turns.filter((t) => {
    if (dateFrom) {
      const d = new Date(t.created_at);
      if (d < new Date(dateFrom)) return false;
    }
    if (dateTo) {
      const d = new Date(t.created_at);
      const to = new Date(dateTo);
      to.setDate(to.getDate() + 1);
      if (d >= to) return false;
    }
    return true;
  });

  const handleExport = () => {
    const blob = new Blob([JSON.stringify(filtered, null, 2)], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `conversations-${Date.now()}.json`;
    a.click();
    URL.revokeObjectURL(url);
  };

  if (loading && !data) return <PanelSkeleton />;
  if (error && !data) return <ErrorCard error={error} title="Failed to load conversations" onRetry={refetch} />;

  return (
    <div className="p-6 space-y-4">
      <header className="flex items-center justify-between flex-wrap gap-3">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Conversations</h1>
          <p className="text-xs text-muted-foreground mt-0.5">Archived Telegram conversation turns</p>
        </div>
        <button
          type="button"
          onClick={handleExport}
          disabled={filtered.length === 0}
          className="flex items-center gap-2 rounded-md border px-3 py-1.5 text-sm hover:bg-accent/60 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
          title="Download filtered turns as JSON"
        >
          <Download size={14} />
          Export JSON
        </button>
      </header>

      <div className="flex flex-wrap gap-3">
        <label className="flex flex-col gap-1">
          <span className="text-xs text-muted-foreground">Chat ID</span>
          <input
            type="number"
            value={chatId}
            onChange={(e) => setChatId(e.target.value)}
            placeholder="All chats"
            className="rounded-md border bg-background px-3 py-1.5 text-sm w-32 focus:outline-none focus:ring-1 focus:ring-primary/50"
          />
        </label>
        <label className="flex flex-col gap-1">
          <span className="text-xs text-muted-foreground">From</span>
          <input
            type="date"
            value={dateFrom}
            onChange={(e) => setDateFrom(e.target.value)}
            className="rounded-md border bg-background px-3 py-1.5 text-sm focus:outline-none focus:ring-1 focus:ring-primary/50"
          />
        </label>
        <label className="flex flex-col gap-1">
          <span className="text-xs text-muted-foreground">To</span>
          <input
            type="date"
            value={dateTo}
            onChange={(e) => setDateTo(e.target.value)}
            className="rounded-md border bg-background px-3 py-1.5 text-sm focus:outline-none focus:ring-1 focus:ring-primary/50"
          />
        </label>
        <label className="flex items-end gap-2 pb-1.5">
          <input
            type="checkbox"
            checked={hasTools}
            onChange={(e) => setHasTools(e.target.checked)}
            className="rounded"
            id="has-tools"
          />
          <span className="text-sm select-none cursor-pointer" onClick={() => setHasTools((v) => !v)}>
            Has tool calls
          </span>
        </label>
      </div>

      {filtered.length === 0 ? (
        <EmptyState />
      ) : (
        <div className="rounded-xl border overflow-hidden">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b bg-muted/30 text-xs text-muted-foreground">
                <th className="px-4 py-2.5 text-left font-medium w-12">ID</th>
                <th className="px-4 py-2.5 text-left font-medium w-20">Chat</th>
                <th className="px-4 py-2.5 text-left font-medium w-20">Role</th>
                <th className="px-4 py-2.5 text-left font-medium">Content</th>
                <th className="px-4 py-2.5 text-left font-medium w-36">Date</th>
              </tr>
            </thead>
            <tbody>
              {filtered.map((turn, i) => (
                <tr
                  key={turn.id}
                  onClick={() => setSelectedId(turn.id)}
                  className={`cursor-pointer transition-colors hover:bg-accent/40 ${
                    i % 2 === 0 ? '' : 'bg-muted/10'
                  }`}
                >
                  <td className="px-4 py-2.5 tabular-nums text-muted-foreground">{turn.id}</td>
                  <td className="px-4 py-2.5 tabular-nums">{turn.chat_id}</td>
                  <td className="px-4 py-2.5">
                    <span className={`inline-flex rounded-full px-2 py-0.5 text-xs font-medium ${ROLE_BADGE[turn.role] ?? 'bg-muted text-muted-foreground'}`}>
                      {turn.role}
                    </span>
                  </td>
                  <td className="px-4 py-2.5 max-w-xs truncate text-muted-foreground">{turn.content}</td>
                  <td className="px-4 py-2.5 text-xs text-muted-foreground tabular-nums whitespace-nowrap">
                    {new Date(turn.created_at).toLocaleString()}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <ConversationDrawer turnId={selectedId} onClose={() => setSelectedId(null)} />
    </div>
  );
}

function EmptyState() {
  return (
    <div className="flex flex-col items-center justify-center py-16 gap-3 text-center">
      <MessageSquare size={40} className="text-muted-foreground/40" />
      <p className="text-sm font-medium text-muted-foreground">No conversations found</p>
      <p className="text-xs text-muted-foreground/70">Start a conversation in Telegram to see turns archived here</p>
    </div>
  );
}

function PanelSkeleton() {
  return (
    <div className="p-6 space-y-4">
      <Skeleton className="h-8 w-48" />
      <div className="flex gap-3">
        <Skeleton className="h-9 w-32" />
        <Skeleton className="h-9 w-32" />
        <Skeleton className="h-9 w-32" />
      </div>
      <div className="rounded-xl border overflow-hidden space-y-0">
        {[0, 1, 2, 3, 4].map((i) => (
          <div key={i} className="flex gap-4 px-4 py-3 border-b last:border-b-0">
            <Skeleton className="h-4 w-8" />
            <Skeleton className="h-4 w-16" />
            <Skeleton className="h-4 w-20" />
            <Skeleton className="h-4 flex-1" />
            <Skeleton className="h-4 w-28" />
          </div>
        ))}
      </div>
    </div>
  );
}
