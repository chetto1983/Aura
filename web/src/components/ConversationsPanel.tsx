import { useCallback, useEffect, useState } from 'react';
import { MessageSquare, Download, Trash2 } from 'lucide-react';
import { toast } from 'sonner';
import { Skeleton } from '@/components/ui/skeleton';
import { ErrorCard } from '@/components/common/ErrorCard';
import { ConversationDrawer } from '@/components/ConversationDrawer';
import { confirm as confirmModal, prompt as promptModal } from '@/lib/confirmModal';
import { api, ApiError } from '@/api';
import { useApi } from '@/hooks/useApi';
import { useLocale } from '@/hooks/useLocale';
import type { ConversationTurn } from '@/types/api';

const ROLE_BADGE: Record<string, string> = {
  user: 'bg-blue-500/15 text-blue-600 dark:text-blue-400',
  assistant: 'bg-cyan-500/15 text-cyan-600 dark:text-cyan-400',
  tool: 'bg-muted text-muted-foreground',
};

export function ConversationsPanel() {
  const { t, formatDate } = useLocale();
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

  useEffect(() => {
    const openHashTurn = () => {
      const match = window.location.hash.match(/^#turn-(\d+)$/);
      if (!match) return;
      const id = Number(match[1]);
      setSelectedId(id);
      window.setTimeout(() => {
        const nodes = document.querySelectorAll<HTMLElement>(`[data-turn-id="${id}"]`);
        const visible = Array.from(nodes).find((node) => node.getClientRects().length > 0);
        visible?.scrollIntoView({ block: 'center' });
      }, 0);
    };
    openHashTurn();
    window.addEventListener('hashchange', openHashTurn);
    return () => window.removeEventListener('hashchange', openHashTurn);
  }, []);

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
      confirmLabel: t('common.delete'),
      destructive: true,
    });
    if (!ok) return;
    setCleaning(true);
    const id = toast.loading(t('conversations.toast.cleaning'));
    try {
      const res = await api.cleanupConversations(sel);
      toast.success(t('conversations.toast.deleted', { deleted: res.deleted }), { id });
      refetch();
      refreshStats();
    } catch (err) {
      const msg = err instanceof ApiError ? err.message : String(err);
      toast.error(t('conversations.toast.cleanupFailed', { msg }), { id });
    } finally {
      setCleaning(false);
    }
  }, [refetch, refreshStats, t]);

  const handleCleanupOlder = useCallback(async () => {
    const ans = await promptModal({
      title: t('conversations.purgeDialog.title'),
      description: t('conversations.purgeDialog.description'),
      label: t('conversations.purgeDialog.label'),
      placeholder: t('conversations.purgeDialog.placeholder'),
      defaultValue: '30',
      inputMode: 'numeric',
      confirmLabel: t('conversations.purgeDialog.confirm'),
      validate: (v) => {
        const n = parseInt(v, 10);
        if (!Number.isFinite(n) || n < 1) return t('conversations.purgeDialog.validate');
        return null;
      },
    });
    if (!ans) return;
    const days = parseInt(ans, 10);
    runCleanup(
      { older_than_days: days },
      days === 1
        ? t('conversations.purgeConfirm.title', { days })
        : t('conversations.purgeConfirm.title_plural', { days }),
      t('conversations.purgeConfirm.description'),
    );
  }, [runCleanup, t]);

  const handleCleanupChat = useCallback(() => {
    if (numericChatId === undefined) {
      toast.error(t('conversations.toast.setChatId'));
      return;
    }
    runCleanup(
      { chat_id: numericChatId },
      t('conversations.wipeChatConfirm.title', { chatId: numericChatId }),
      t('conversations.wipeChatConfirm.description'),
    );
  }, [numericChatId, runCleanup, t]);

  const handleCleanupAll = useCallback(() => {
    runCleanup(
      { all: true },
      t('conversations.wipeAllConfirm.title'),
      t('conversations.wipeAllConfirm.description'),
    );
  }, [runCleanup, t]);

  if (loading && !data) return <PanelSkeleton />;
  if (error && !data) return <ErrorCard error={error} title={t('conversations.errorTitle')} onRetry={refetch} />;

  return (
    <div className="p-6 space-y-4">
      <header className="flex items-start justify-between flex-wrap gap-3">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">{t('conversations.title')}</h1>
          <p className="text-xs text-muted-foreground mt-0.5">
            {t('conversations.subtitle')}
            {stats && stats.total_rows > 0 && (
              <span className="ml-2 inline-flex items-center gap-2">
                ·
                <span className="tabular-nums">{stats.total_rows.toLocaleString()} {t('conversations.rowsLabel')}</span>
                <span>·</span>
                <span className="tabular-nums">{stats.distinct_chats} {stats.distinct_chats === 1 ? t('conversations.chatLabel') : t('conversations.chatsLabel')}</span>
                {stats.oldest_at && (
                  <>
                    <span>·</span>
                    <span>{t('conversations.oldestLabel')} {formatDate(stats.oldest_at, { dateStyle: 'short' })}</span>
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
            title={t('conversations.purgeOlderTitle')}
          >
            <Trash2 size={14} />
            {t('conversations.purgeOlder')}
          </button>
          {numericChatId !== undefined && (
            <button
              type="button"
              onClick={handleCleanupChat}
              disabled={cleaning}
              className="flex min-h-11 items-center gap-1.5 rounded-md border border-amber-500/40 px-3 py-2 text-sm text-amber-600 dark:text-amber-400 hover:bg-amber-500/10 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
              title={t('conversations.wipeChatTitle', { chatId: numericChatId })}
            >
              <Trash2 size={14} />
              {t('conversations.wipeChat')}
            </button>
          )}
          <button
            type="button"
            onClick={handleCleanupAll}
            disabled={cleaning || !stats || stats.total_rows === 0}
            className="flex min-h-11 items-center gap-1.5 rounded-md border border-destructive/40 px-3 py-2 text-sm text-destructive hover:bg-destructive/10 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
            title={t('conversations.wipeAllTitle')}
          >
            <Trash2 size={14} />
            {t('conversations.wipeAll')}
          </button>
          <button
            type="button"
            onClick={handleExport}
            disabled={filtered.length === 0}
            className="flex min-h-11 items-center gap-2 rounded-md border px-3 py-2 text-sm hover:bg-accent/60 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
            title={filtered.length === 0 ? t('conversations.exportHintNoMatch') : t('conversations.exportHintDownload')}
            aria-describedby={filtered.length === 0 ? 'conversation-export-hint' : undefined}
          >
            <Download size={14} />
            {t('conversations.exportJson')}
          </button>
          {filtered.length === 0 && (
            <span id="conversation-export-hint" className="sr-only">
              {t('conversations.srHint')}
            </span>
          )}
        </div>
      </header>

      <div className="flex flex-wrap gap-3">
        <label className="flex flex-col gap-1">
          <span className="text-xs text-muted-foreground">{t('conversations.filterChatId')}</span>
          <input
            type="number"
            value={chatId}
            onChange={(e) => setChatId(e.target.value)}
            placeholder={t('conversations.filterAllChats')}
            className="min-h-11 rounded-md border bg-background px-3 py-2 text-sm w-32 focus:outline-none focus:ring-1 focus:ring-primary/50"
          />
        </label>
        <label className="flex flex-col gap-1">
          <span className="text-xs text-muted-foreground">{t('conversations.filterFrom')}</span>
          <input
            type="date"
            value={dateFrom}
            onChange={(e) => setDateFrom(e.target.value)}
            className="min-h-11 rounded-md border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-primary/50"
          />
        </label>
        <label className="flex flex-col gap-1">
          <span className="text-xs text-muted-foreground">{t('conversations.filterTo')}</span>
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
            {t('conversations.filterHasTools')}
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
              data-turn-id={turn.id}
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
                <span className="tabular-nums">{t('conversations.table.chat')} {turn.chat_id}</span>
                <span>{formatDate(turn.created_at, { dateStyle: 'short', timeStyle: 'medium' })}</span>
              </div>
            </button>
          ))}
        </div>

        <div className="hidden rounded-xl border overflow-hidden md:block">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b bg-muted/30 text-xs text-muted-foreground">
                <th className="px-4 py-2.5 text-left font-medium w-12">{t('conversations.table.id')}</th>
                <th className="px-4 py-2.5 text-left font-medium w-20">{t('conversations.table.chat')}</th>
                <th className="px-4 py-2.5 text-left font-medium w-20">{t('conversations.table.role')}</th>
                <th className="px-4 py-2.5 text-left font-medium">{t('conversations.table.content')}</th>
                <th className="px-4 py-2.5 text-left font-medium w-36">{t('conversations.table.date')}</th>
              </tr>
            </thead>
            <tbody>
              {filtered.map((turn, i) => (
                <tr
                  key={turn.id}
                  data-turn-id={turn.id}
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
                    {formatDate(turn.created_at, { dateStyle: 'short', timeStyle: 'medium' })}
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
  const { t } = useLocale();
  return (
    <div className="flex flex-col items-center justify-center py-16 gap-3 text-center">
      <MessageSquare size={40} className="text-muted-foreground/40" />
      <p className="text-sm font-medium text-muted-foreground">{t('conversations.emptyTitle')}</p>
      <p className="text-xs text-muted-foreground">{t('conversations.emptyHint')}</p>
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
