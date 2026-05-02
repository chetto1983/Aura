import { useCallback, useEffect } from 'react';
import { X } from 'lucide-react';
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
} from '@/components/ui/sheet';
import { Skeleton } from '@/components/ui/skeleton';
import { api } from '@/api';
import { useApi } from '@/hooks/useApi';
import type { ConversationDetail } from '@/types/api';

interface Props {
  turnId: number | null;
  onClose: () => void;
}

const ROLE_COLOR: Record<string, string> = {
  user: 'border-blue-500/40 bg-blue-500/5',
  assistant: 'border-cyan-500/40 bg-cyan-500/5',
  tool: 'border-muted bg-muted/30',
};

const ROLE_LABEL_COLOR: Record<string, string> = {
  user: 'text-blue-500',
  assistant: 'text-cyan-500',
  tool: 'text-muted-foreground',
};

export function ConversationDrawer({ turnId, onClose }: Props) {
  const fetcher = useCallback(
    () => (turnId !== null ? api.conversation(turnId) : Promise.reject(new Error('no id'))),
    [turnId],
  );
  const { data, loading } = useApi<ConversationDetail>(fetcher);

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [onClose]);

  return (
    <Sheet open={turnId !== null} onOpenChange={(open) => { if (!open) onClose(); }}>
      <SheetContent side="right" className="w-full sm:max-w-xl overflow-y-auto" showCloseButton={false}>
        <SheetHeader className="pr-10">
          <div className="flex items-center justify-between">
            <SheetTitle>Turn detail</SheetTitle>
            <button
              type="button"
              onClick={onClose}
              className="rounded-md p-1 hover:bg-accent/50 text-muted-foreground"
              aria-label="Close"
            >
              <X size={16} />
            </button>
          </div>
          {data && (
            <SheetDescription>
              Chat {data.chat_id} · turn #{data.turn_index} · {new Date(data.created_at).toLocaleString()}
            </SheetDescription>
          )}
        </SheetHeader>

        <div className="px-4 pb-6 space-y-4">
          {loading && !data && <DrawerSkeleton />}
          {data && <TurnDetail turn={data} />}
        </div>
      </SheetContent>
    </Sheet>
  );
}

function TurnDetail({ turn }: { turn: ConversationDetail }) {
  const borderClass = ROLE_COLOR[turn.role] ?? 'border-muted bg-muted/30';
  const labelClass = ROLE_LABEL_COLOR[turn.role] ?? 'text-muted-foreground';

  let parsedToolCalls: unknown[] | null = null;
  if (turn.tool_calls) {
    try {
      parsedToolCalls = JSON.parse(turn.tool_calls) as unknown[];
    } catch {
      // show raw string below if parse fails
    }
  }

  return (
    <div className={`rounded-lg border p-4 space-y-3 ${borderClass}`}>
      <div className="flex items-center justify-between text-xs">
        <span className={`font-semibold uppercase tracking-wider ${labelClass}`}>{turn.role}</span>
        <span className="text-muted-foreground">id {turn.id}</span>
      </div>

      <p className="text-sm whitespace-pre-wrap break-words leading-relaxed">{turn.content}</p>

      {turn.tool_calls && (
        <div className="space-y-1">
          <p className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">Tool calls</p>
          {parsedToolCalls ? (
            <pre className="text-xs bg-muted/50 rounded p-3 overflow-x-auto whitespace-pre-wrap break-words">
              {JSON.stringify(parsedToolCalls, null, 2)}
            </pre>
          ) : (
            <pre className="text-xs bg-muted/50 rounded p-3 overflow-x-auto whitespace-pre-wrap break-words">
              {turn.tool_calls}
            </pre>
          )}
        </div>
      )}

      <dl className="grid grid-cols-2 gap-x-4 gap-y-1 text-xs text-muted-foreground">
        {turn.llm_calls > 0 && <><dt>LLM calls</dt><dd className="tabular-nums">{turn.llm_calls}</dd></>}
        {turn.tool_calls_count > 0 && <><dt>Tool calls</dt><dd className="tabular-nums">{turn.tool_calls_count}</dd></>}
        {turn.elapsed_ms > 0 && <><dt>Elapsed</dt><dd className="tabular-nums">{turn.elapsed_ms}ms</dd></>}
        {turn.tokens_in > 0 && <><dt>Tokens in</dt><dd className="tabular-nums">{turn.tokens_in}</dd></>}
        {turn.tokens_out > 0 && <><dt>Tokens out</dt><dd className="tabular-nums">{turn.tokens_out}</dd></>}
      </dl>
    </div>
  );
}

function DrawerSkeleton() {
  return (
    <div className="space-y-3">
      <Skeleton className="h-4 w-24" />
      <Skeleton className="h-20 w-full" />
      <Skeleton className="h-4 w-32" />
    </div>
  );
}
