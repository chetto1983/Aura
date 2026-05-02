import { useCallback, useEffect, useState } from 'react';
import { MessageSquare, Download, Trash2 } from 'lucide-react';
import { toast } from 'sonner';
import { Skeleton } from '@/components/ui/skeleton';
import { ErrorCard } from '@/components/common/ErrorCard';
import { ConversationDrawer } from '@/components/ConversationDrawer';
import { api, ApiError } from '@/api';
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

  const { data, error, loading, refetch } = useApi<ConversationTurn[]>(fetcher);

  const turns = data ?? [];

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

  const [stats, setStats] = useState<{ total_rows: number; oldest_at?: string; distinct_chats: number } | null>(null);
  const [cleaning, setCleaning] = useState(false);
  const refreshStats = useCallback(() => {
    api.conversationStats()
      .then((s) => setStats({ total_rows: s.total_rows, oldest_at: s.oldest_at, distinct_chats: s.distinct_chats }))
      .catch(() => setStats(null));
  }, []);
  useEffect(() => { refreshStats(); }, [refreshStats]);

  const runCleanup = useCallback(async (sel: { chat_id?: number; older_than_days?: number; all?: boolean }, label: string) => {
    if (!window.confirm(`${label}\n\nThis cannot be undone. Continue?`)) return;
    setCleaning(true);
    const id = toast.loading('Cleaning…');
    try {
      const res = await api.cleanupConversations(sel);
      toast.success(`Deleted ${res.deleted} turns.`, { id });
      refetch();
      refreshStats();
    } catch (err) {
      const msg = err instanceof ApiError ? err.message : String(err);
      toast.error(`Cleanup failed: ${msg}`, { id });
    } finally {
      setCleaning(false);
    }
  }, [refetch, refreshStats]);

  const handleCleanupOlder = useCallback(() => {
    const ans = window.prompt('Delete turns older than how many days? (e.g. 30)', '30');
    if (!ans) return;
    const days = parseInt(ans, 10);
    if (!Number.isFinite(days) || days < 1) {
      toast.error('Enter a positive integer.');
      return;
    }
    runCleanup({ older_than_days: days }, `Delete every turn older than ${days} day${days === 1 ? '' : 's'}.`);
  }, [runCleanup]);

  const handleCleanupChat = useCallback(() => {
    if (numericChatId === undefined) {
      toast.error('Set a chat_id filter first.');
      return;
    }
    runCleanup({ chat_id: numericChatId }, `Delete every archived turn for chat ${numericChatId}.`);
  }, [numericChatId, runCleanup]);

  const handleCleanupAll = useCallback(() => {
    runCleanup({ all: true }, 'Delete every archived conversation turn (all chats).');
  }, [runCleanup]);

  if (loading && !data) return <PanelSkeleton />;
  if (error && !data) return <ErrorCard error={error} title="Failed to load conversations" onRetry={refetch} />;

  return (
    <div className="p-6 space-y-4">
      <header className="flex items-start justify-between flex-wrap gap-3">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Conversations</h1>
          <p className="text-xs text-muted-foreground mt-0.5">
            Archived Telegram conversation turns
            {stats && stats.total_rows > 0 && (
              <span className="ml-2 inline-flex items-center gap-2">
                ·
                <span className="tabular-nums">{stats.total_rows.toLocaleString()} rows</span>
                <span>·</span>
                <span className="tabular-nums">{stats.distinct_chats} chat{stats.distinct_chats === 1 ? '' : 's'}</span>
                {stats.oldest_at && (
                  <>
                    <span>·</span>
                    <span>oldest {new Date(stats.oldest_at).toLocaleDateString()}</span>
                  </>
                )}
              </span>
            )}
          </p>
        </div>
        <div className="flex items-center gap-2 flex-wrap">
          <button
            type="button"
            onClick={handleCleanupOlder}
            disabled={cleaning || !stats || stats.total_rows === 0}
            className="flex items-center gap-1.5 rounded-md border px-3 py-1.5 text-sm hover:bg-accent/60 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
            title="Delete turns older than N days"
          >
            <Trash2 size={14} />
            Purge older than…
          </button>
          {numericChatId !== undefined && (
            <button
              type="button"
              onClick={handleCleanupChat}
              disabled={cleaning}
              className="flex items-center gap-1.5 rounded-md border border-amber-500/40 px-3 py-1.5 text-sm text-amber-600 dark:text-amber-400 hover:bg-amber-500/10 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
              title={`Delete all turns for chat ${numericChatId}`}
            >
              <Trash2 size={14} />
              Wipe this chat
            </button>
          )}
          <button
            type="button"
            onClick={handleCleanupAll}
            disabled={cleaning || !stats || stats.total_rows === 0}
            className="flex items-center gap-1.5 rounded-md border border-destructive/40 px-3 py-1.5 text-sm text-destructive hover:bg-destructive/10 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
            title="Delete every archived turn"
          >
            <Trash2 size={14} />
            Wipe all
          </button>
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
        </div>
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
