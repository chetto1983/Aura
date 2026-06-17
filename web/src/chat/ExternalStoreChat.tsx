import { useCallback, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  AssistantRuntimeProvider,
  AuiIf,
  MessagePrimitive,
  ThreadPrimitive,
  useExternalStoreRuntime,
  type AppendMessage,
  type ThreadMessageLike,
} from '@assistant-ui/react';
import { MarkdownTextPrimitive } from '@assistant-ui/react-markdown';
import { Composer } from './Composer';
import { ReasoningDrawer } from './ReasoningDrawer';
import { ToolActivityCard } from './ToolActivityCard';
import { streamRun, type TurnUsage } from './sseAdapter';

// ExternalStoreChat (CHAT-01): the Core-Value chat lane. It owns the message
// list + isRunning + per-turn usage in React state and feeds them to
// useExternalStoreRuntime. onNew POSTs /agent/run and folds the AG-UI SSE stream
// onto one assistant ThreadMessageLike via the sseAdapter reducer; onCancel
// aborts the in-flight fetch (the Stop affordance, ctx-cancel on the server).
//
// Integration seams left for later plans (do NOT build here):
//   • 25-04 RuntimeFooter — the latest TurnUsage is surfaced via onUsage so the
//     footer can mount alongside without re-plumbing the stream.
//   • 25-07 BranchPicker — onEdit/onReload + branch nav land on this same runtime
//     once the path-aware backend (25-06) exists; capabilities gate them off now.

function appendMessageText(message: AppendMessage): string {
  return message.content
    .filter((p): p is { type: 'text'; text: string } => p.type === 'text')
    .map((p) => p.text)
    .join('');
}

function userMessage(text: string): ThreadMessageLike {
  return { id: crypto.randomUUID(), role: 'user', content: [{ type: 'text', text }] };
}

export interface ExternalStoreChatProps {
  /** Conversation/thread id the run is POSTed against. */
  readonly threadId: string;
  /** 25-04 seam: receives the latest per-turn usage off the SSE STATE_DELTA. */
  readonly onUsage?: (usage: TurnUsage | undefined) => void;
}

export function ExternalStoreChat({ threadId, onUsage }: ExternalStoreChatProps) {
  const { t } = useTranslation();
  const [messages, setMessages] = useState<ThreadMessageLike[]>([]);
  const [isRunning, setIsRunning] = useState(false);
  const abortRef = useRef<AbortController | null>(null);

  const onNew = useCallback(
    async (message: AppendMessage) => {
      const text = appendMessageText(message);
      const user = userMessage(text);
      // Snapshot the assistant message index after appending the user turn so the
      // streaming folder can replace exactly that slot in place.
      let assistantIndex = -1;
      setMessages((prev) => {
        assistantIndex = prev.length + 1;
        return [...prev, user];
      });
      setIsRunning(true);

      const controller = new AbortController();
      abortRef.current = controller;
      try {
        await streamRun({
          threadId,
          userText: text,
          signal: controller.signal,
          onUpdate: (assistant, usage) => {
            onUsage?.(usage);
            setMessages((prev) => {
              const next = prev.slice();
              if (assistantIndex >= 0 && assistantIndex < next.length) {
                next[assistantIndex] = assistant;
              } else {
                next.push(assistant);
              }
              return next;
            });
          },
        });
      } catch {
        // An aborted fetch (Stop) throws AbortError; the partial assistant
        // message already rendered is left as-is (incomplete). Other network
        // failures surface through the reducer's error part where reachable.
      } finally {
        setIsRunning(false);
        abortRef.current = null;
      }
    },
    [threadId, onUsage],
  );

  const onCancel = useCallback(async () => {
    abortRef.current?.abort();
    setIsRunning(false);
    return Promise.resolve();
  }, []);

  // Branch/edit/regenerate are the 25-07 sub-slice (needs the path-aware
  // backend), so onEdit/onReload are deliberately NOT provided — the runtime
  // gates those capabilities off when the handlers are absent. Cancel is enabled
  // by providing onCancel (the Stop affordance). 25-07 wires the rest onto this
  // same runtime once the backend exists.
  const runtime = useExternalStoreRuntime<ThreadMessageLike>({
    messages,
    isRunning,
    convertMessage: (m) => m,
    onNew,
    onCancel,
  });

  return (
    <AssistantRuntimeProvider runtime={runtime}>
      <ThreadPrimitive.Root className="flex h-full min-h-0 flex-col">
        <ThreadPrimitive.Viewport className="flex-1 space-y-4 overflow-y-auto px-3 py-4 sm:px-4">
          <AuiIf condition={(s) => s.thread.isEmpty}>
            <div className="grid h-full place-items-center py-8 text-center">
              <div className="flex flex-col items-center gap-2">
                <h2 className="text-xl font-medium text-text">{t('chat.empty.thread.heading')}</h2>
                <p className="max-w-sm text-sm text-text-muted">{t('chat.empty.thread.body')}</p>
              </div>
            </div>
          </AuiIf>

          <ThreadPrimitive.Messages>
            {({ message }) =>
              message.role === 'user' ? <UserMessage /> : <AssistantMessage />
            }
          </ThreadPrimitive.Messages>
        </ThreadPrimitive.Viewport>

        {/* Running-status row: role="status" announces the active turn politely. */}
        {isRunning ? (
          <p role="status" className="px-3 py-1 text-[0.6875rem] text-text-muted sm:px-4">
            {t('chat.running')}
          </p>
        ) : null}

        <Composer />
      </ThreadPrimitive.Root>
    </AssistantRuntimeProvider>
  );
}

function UserMessage() {
  return (
    <MessagePrimitive.Root className="ml-auto max-w-[80%] rounded-[var(--radius-md)] bg-surface-2 px-3 py-2">
      <MessagePrimitive.Parts
        components={{
          Text: () => (
            <div className="whitespace-pre-wrap text-sm leading-relaxed text-text">
              <MarkdownTextPrimitive />
            </div>
          ),
        }}
      />
    </MessagePrimitive.Root>
  );
}

function AssistantMessage() {
  return (
    <MessagePrimitive.Root className="max-w-[90%] space-y-2">
      <MessagePrimitive.Parts
        components={{
          // Assistant prose → sanitized markdown.
          Text: () => (
            <div className="text-sm leading-relaxed text-text">
              <MarkdownTextPrimitive />
            </div>
          ),
          // CoT → collapsible drawer (D-01). The drawer reads the reasoning text
          // from the part via the reasoning render-fn arg.
          Reasoning: ({ text }) => <ReasoningDrawer text={text} />,
          // Tool activity → raw card (D-02). NEVER typed rendering (Phase 26).
          tools: {
            Fallback: ({ toolName, argsText, result, isError }) => (
              <ToolActivityCard
                toolName={toolName}
                argsText={argsText}
                {...(typeof result === 'string' ? { result } : {})}
                {...(isError !== undefined ? { isError } : {})}
              />
            ),
          },
        }}
      />
      <MessagePrimitive.Error>
        <p role="alert" className="text-sm text-danger">
          {/* The reducer already routes RUN_ERROR into an error text part; this
              is the runtime-level fallback for a hard message error. */}
        </p>
      </MessagePrimitive.Error>
    </MessagePrimitive.Root>
  );
}
