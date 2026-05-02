import { useCallback, useEffect, useState } from 'react';
import { MessageSquare, Download, Trash2 } from 'lucide-react';
import { toast } from 'sonner';
import { Skeleton } from '@/components/ui/skeleton';
import { ErrorCard } from '@/components/common/ErrorCard';
import { ConversationDrawer } from '@/components/ConversationDrawer';
import { confirm as confirmModal, prompt as promptModal } from '@/lib/confirmModal';
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

  const runCleanup = useCallback(async (
    sel: { chat_id?: number; older_than_days?: number; all?: boolean },
    title: string,
    description: string,
  ) => {
    const ok = await confirmModal({
      title,
      description,
      confirmLabel: 'Delete',
      destructive: true,
    });
    if (!ok) return;
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

  const handleCleanupOlder = useCallback(async () => {
    const ans = await promptModal({
      title: 'Purge older turns',
      description: 'Delete every archived turn older than the entered number of days.',
      label: 'Days',
      placeholder: 'e.g. 30',
      defaultValue: '30',
      inputMode: 'numeric',
      confirmLabel: 'Continue',
      validate: (v) => {
        const n = parseInt(v, 10);
        if (!Number.isFinite(n) || n < 1) return 'Enter a positive integer.';
        return null;
      },
    });
    if (!ans) return;
    const days = parseInt(ans, 10);
    runCleanup(
      { older_than_days: days },
      `Delete turns older than ${days} day${days === 1 ? '' : 's'}?`,
      'This permanently removes archived conversation turns and cannot be undone.',
    );
  }, [runCleanup]);

  const handleCleanupChat = useCallback(() => {
    if (numericChatId === undefined) {
      toast.error('Set a chat_id filter first.');
      return;
    }
    runCleanup(
      { chat_id: numericChatId },
      `Wipe all turns for chat ${numericChatId}?`,
      'This permanently removes every archived turn for the selected chat and cannot be undone.',
    );
  }, [numericChatId, runCleanup]);

  const handleCleanupAll = useCallback(() => {
    runCleanup(
      { all: true },
      'Wipe every archived conversation turn?',
      'This permanently deletes all archived turns across every chat and cannot be undone.',
    );
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
            className="flex min-h-11 items-center gap-1.5 rounded-md border px-3 py-2 text-sm hover:bg-accent/60 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
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
              className="flex min-h-11 items-center gap-1.5 rounded-md border border-amber-500/40 px-3 py-2 text-sm text-amber-600 dark:text-amber-400 hover:bg-amber-500/10 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
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
            className="flex min-h-11 items-center gap-1.5 rounded-md border border-destructive/40 px-3 py-2 text-sm text-destructive hover:bg-destructive/10 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
            title="Delete every archived turn"
          >
            <Trash2 size={14} />
            Wipe all
          </button>
          <button
            type="button"
            onClick={handleExport}
            disabled={filtered.length === 0}
            className="flex min-h-11 items-center gap-2 rounded-md border px-3 py-2 text-sm hover:bg-accent/60 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
            title={filtered.length === 0 ? 'No conversation rows match the current filters' : 'Download filtered turns as JSON'}
            aria-describedby={filtered.length === 0 ? 'conversation-export-hint' : undefined}
          >
            <Download size={14} />
            Export JSON
          </button>
          {filtered.length === 0 && (
            <span id="conversation-export-hint" className="sr-only">
              No conversation rows match the current filters.
            </span>
          )}
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
            className="min-h-11 rounded-md border bg-background px-3 py-2 text-sm w-32 focus:outline-none focus:ring-1 focus:ring-primary/50"
          />
        </label>
        <label className="flex flex-col gap-1">
          <span className="text-xs text-muted-foreground">From</span>
          <input
            type="date"
            value={dateFrom}
            onChange={(e) => setDateFrom(e.target.value)}
            className="min-h-11 rounded-md border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-primary/50"
          />
        </label>
        <label className="flex flex-col gap-1">
          <span className="text-xs text-muted-foreground">To</span>
          <input
            type="date"
            value={dateTo}
            onChange={(e) => setDateTo(e.target.value)}
            className="min-h-11 rounded-md border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-primary/50"
          />
        </label>
        <label className="flex min-h-11 items-center gap-2 pb-0">
          <input
            type="checkbox"
            checked={hasTools}
            onChange={(e) => setHasTools(e.target.checked)}
            className="rounded"
            id="has-tools"
          />
          <span className="text-sm select-none cursor-pointer">
            Has tool calls
          </span>
        </label>
      </div>

      {filtered.length === 0 ? (
        <EmptyState />
      ) : (
        <>
        <div className="space-y-2 md:hidden">
          {filtered.map((turn) => (
            <button
              key={turn.id}
              type="button"
              onClick={() => setSelectedId(turn.id)}
              className="w-full rounded-lg border bg-card p-3 text-left hover:bg-accent/30"
            >
              <div className="flex min-h-11 items-center justify-between gap-3">
                <span className="font-mono text-xs tabular-nums text-muted-foreground">#{turn.id}</span>
                <span className={`inline-flex rounded-full px-2 py-1 text-xs font-medium ${ROLE_BADGE[turn.role] ?? 'bg-muted text-muted-foreground'}`}>
                  {turn.role}
                </span>
              </div>
              <p className="mt-2 line-clamp-3 text-sm text-muted-foreground">{turn.content}</p>
              <div className="mt-3 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                <span className="tabular-nums">Chat {turn.chat_id}</span>
                <span>{new Date(turn.created_at).toLocaleString()}</span>
              </div>
            </button>
          ))}
        </div>

        <div className="hidden rounded-xl border overflow-hidden md:block">
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
        </>
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
      <p className="text-xs text-muted-foreground">Start a conversation in Telegram to see turns archived here</p>
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
