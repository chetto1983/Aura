import { useCallback, useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQueryClient } from '@tanstack/react-query';
import {
  ActionBarPrimitive,
  AssistantRuntimeProvider,
  AuiIf,
  ComposerPrimitive,
  MessagePrimitive,
  ThreadPrimitive,
  useAuiState,
  useExternalStoreRuntime,
  type AppendMessage,
  type ThreadMessageLike,
  type ToolCallMessagePartComponent,
} from '@assistant-ui/react';
import { CONVERSATION_KEY, CONVERSATION_ROT_EVENTS_KEY } from '../conversations/useConversations';
import { BranchPicker } from './BranchPicker';
import { Composer } from './Composer';
import { AttachmentCard } from './attachments/AttachmentCard';
import { useAttachmentUploads } from './attachments/useAttachmentUploads';
import type { Asset } from './attachments/types';
import { DisplayRouter } from './displays/DisplayRouter';
import { isDisplayPayload, type DisplayPayload } from './displays/types';
import { MarkdownText } from './MarkdownText';
import { ReasoningDrawer } from './ReasoningDrawer';
import { ToolActivityCard } from './ToolActivityCard';
import { fetchThreadMessages, streamPost, streamRun, type TurnUsage } from './sseAdapter';

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

function userMessage(text: string, attachments: readonly Asset[] = []): ThreadMessageLike {
  return {
    id: crypto.randomUUID(),
    role: 'user',
    content: [{ type: 'text', text }],
    ...(attachments.length > 0 ? { metadata: { custom: { attachments } } } : {}),
  };
}

function assistantErrorMessage(text: string): ThreadMessageLike {
  return {
    id: crypto.randomUUID(),
    role: 'assistant',
    content: [{ type: 'text', text }],
    status: { type: 'incomplete', reason: 'error' },
  };
}

export interface ExternalStoreChatProps {
  /** Conversation/thread id the run is POSTed against. */
  readonly threadId: string;
  /** Create/select a conversation before the first send when no thread is active. */
  readonly onEnsureThread?: (initialPrompt: string) => Promise<string>;
  /** 25-04 seam: receives the latest per-turn usage off the SSE STATE_DELTA. */
  readonly onUsage?: (usage: TurnUsage | undefined) => void;
  /**
   * Continue-after-resume nonce (D-05): each increment re-drives the run with a
   * no-message POST /agent/run and FOLDS the resumed stream into the chat lane (so the
   * resumed turn renders here, not in a discarded fetch). AppShell bumps it after an
   * inline approval resolves. The initial value is ignored (only changes re-drive).
   */
  readonly resumeNonce?: number;
}

export function ExternalStoreChat({
  threadId,
  onEnsureThread,
  onUsage,
  resumeNonce = 0,
}: ExternalStoreChatProps) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [messages, setMessages] = useState<ThreadMessageLike[]>([]);
  const [isRunning, setIsRunning] = useState(false);
  const abortRef = useRef<AbortController | null>(null);
  const historyAbortRef = useRef<AbortController | null>(null);
  const historyRequestRef = useRef(0);
  const isRunningRef = useRef(false);
  const uploads = useAttachmentUploads(threadId);

  const invalidateRuntimeReads = useCallback(
    (id = threadId) => {
      if (id.length === 0) return;
      void queryClient.invalidateQueries({ queryKey: [CONVERSATION_KEY, id] });
      void queryClient.invalidateQueries({ queryKey: [CONVERSATION_ROT_EVENTS_KEY, id] });
    },
    [queryClient, threadId],
  );

  const onNew = useCallback(
    async (message: AppendMessage) => {
      if (uploads.hasBlockingUploads) return;
      historyRequestRef.current += 1;
      historyAbortRef.current?.abort();
      historyAbortRef.current = null;
      const text = appendMessageText(message);
      const readyAttachmentIds = uploads.readyAssetIds;
      const readyAttachments = uploads.items.flatMap((item) =>
        item.status === 'ready' && item.asset !== undefined ? [item.asset] : [],
      );
      const user = userMessage(text, readyAttachments);
      // Snapshot the assistant message index after appending the user turn so the
      // streaming folder can replace exactly that slot in place.
      let assistantIndex = -1;
      setMessages((prev) => {
        assistantIndex = prev.length + 1;
        return [...prev, user];
      });
      isRunningRef.current = true;
      setIsRunning(true);

      let runThreadId = threadId;
      try {
        if (runThreadId.length === 0 && onEnsureThread !== undefined) {
          runThreadId = await onEnsureThread(text);
        }
        if (runThreadId.length === 0) {
          setMessages((prev) => {
            const next = prev.slice();
            const assistant = assistantErrorMessage(t('chat.error.createConversation'));
            if (assistantIndex >= 0 && assistantIndex < next.length) {
              next[assistantIndex] = assistant;
            } else {
              next.push(assistant);
            }
            return next;
          });
          return;
        }

        const controller = new AbortController();
        abortRef.current = controller;
        await streamRun({
          threadId: runThreadId,
          userText: text,
          attachmentIds: readyAttachmentIds,
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
        if (readyAttachmentIds.length > 0) uploads.clearReady();
      } catch (err) {
        // An aborted fetch (Stop) throws AbortError; the partial assistant
        // message already rendered is left as-is (incomplete). Other network
        // failures surface through the reducer's error part where reachable.
        if (!(err instanceof DOMException && err.name === 'AbortError')) {
          setMessages((prev) => {
            const next = prev.slice();
            const assistant = assistantErrorMessage(t('chat.error.stream'));
            if (assistantIndex >= 0 && assistantIndex < next.length) {
              next[assistantIndex] = assistant;
            } else {
              next.push(assistant);
            }
            return next;
          });
        }
      } finally {
        isRunningRef.current = false;
        setIsRunning(false);
        abortRef.current = null;
        invalidateRuntimeReads(runThreadId);
      }
    },
    [threadId, onEnsureThread, onUsage, invalidateRuntimeReads, t, uploads],
  );

  const onCancel = useCallback(async () => {
    abortRef.current?.abort();
    isRunningRef.current = false;
    setIsRunning(false);
    return Promise.resolve();
  }, []);

  useEffect(() => {
    historyRequestRef.current += 1;
    const request = historyRequestRef.current;
    const clearLoadedMessages = () => {
      queueMicrotask(() => {
        if (request !== historyRequestRef.current) return;
        setMessages([]);
        onUsage?.(undefined);
      });
    };
    historyAbortRef.current?.abort();
    if (threadId.length === 0) {
      historyAbortRef.current = null;
      clearLoadedMessages();
      return;
    }
    if (isRunningRef.current) {
      historyAbortRef.current = null;
      return;
    }

    const controller = new AbortController();
    historyAbortRef.current = controller;
    clearLoadedMessages();

    void fetchThreadMessages(threadId, controller.signal)
      .then((loaded) => {
        if (controller.signal.aborted || request !== historyRequestRef.current) return;
        setMessages(loaded);
      })
      .catch((err: unknown) => {
        if (
          controller.signal.aborted ||
          request !== historyRequestRef.current ||
          (err instanceof DOMException && err.name === 'AbortError')
        ) {
          return;
        }
        setMessages([assistantErrorMessage(t('chat.error.loadHistory'))]);
      })
      .finally(() => {
        if (request === historyRequestRef.current) historyAbortRef.current = null;
      });

    return () => {
      controller.abort();
    };
  }, [threadId, onUsage, t]);

  // foldReRun streams a backend branch re-run (edit / regenerate) onto the supplied base
  // message list, folding the AG-UI reply onto ONE freshly-id'd assistant message appended
  // after the base. The base already reflects the forked turn (the edited user message, or
  // the truncation before a regenerate), so the external-store runtime keeps the PRIOR
  // version as a SIBLING branch automatically — the BranchPicker lights up without a
  // hand-rolled state machine (RESEARCH "Don't Hand-Roll"). It does NOT re-implement the
  // walk — the server drives LoadManagedHistoryForBranch over the forked path (Task-1).
  const foldReRun = useCallback(
    async (url: string, body: unknown, base: ThreadMessageLike[]) => {
      historyRequestRef.current += 1;
      historyAbortRef.current?.abort();
      historyAbortRef.current = null;
      isRunningRef.current = true;
      setIsRunning(true);
      const controller = new AbortController();
      abortRef.current = controller;
      try {
        await streamPost({
          url,
          body,
          signal: controller.signal,
          onUpdate: (assistant, usage) => {
            onUsage?.(usage);
            setMessages([...base, assistant]);
          },
        });
      } catch {
        // Aborted (Stop) or network failure — the partial/last message is left as-is.
      } finally {
        isRunningRef.current = false;
        setIsRunning(false);
        abortRef.current = null;
        invalidateRuntimeReads();
      }
    },
    [onUsage, invalidateRuntimeReads],
  );

  // Continue-after-resume (D-05): when resumeNonce changes (AppShell bumps it after an
  // inline approval resolves), re-drive the run with a no-message POST /agent/run and FOLD
  // the resumed stream into THIS lane's message list, so the resumed turn renders in-thread
  // (not in a discarded fetch). Initial mount only skips nonce=0; if the chat chunk loaded
  // after an approval resolved, a non-zero nonce must still replay.
  const lastResumeNonce = useRef(0);
  useEffect(() => {
    if (resumeNonce === lastResumeNonce.current) return;
    lastResumeNonce.current = resumeNonce;
    if (threadId.length === 0) return;
    historyRequestRef.current += 1;
    historyAbortRef.current?.abort();
    historyAbortRef.current = null;
    const controller = new AbortController();
    abortRef.current = controller;
    // The resumed turn is folded onto a fresh assistant slot appended after whatever the
    // lane currently shows. Each streamed frame replaces that one slot (never N copies):
    // the first onUpdate appends, the rest replace the last element while running.
    const drive = async () => {
      isRunningRef.current = true;
      setIsRunning(true);
      let appended = false;
      try {
        await streamPost({
          url: '/agent/run',
          body: { threadId, messages: [] },
          signal: controller.signal,
          onUpdate: (assistant, usage) => {
            onUsage?.(usage);
            setMessages((prev) => {
              if (!appended) {
                appended = true;
                return [...prev, assistant];
              }
              const next = prev.slice();
              next[next.length - 1] = assistant;
              return next;
            });
          },
        });
      } catch {
        // Aborted / network error — the partial resumed turn is left as rendered.
      } finally {
        isRunningRef.current = false;
        setIsRunning(false);
        abortRef.current = null;
        invalidateRuntimeReads();
      }
    };
    void drive();
  }, [resumeNonce, threadId, onUsage, invalidateRuntimeReads]);

  // backendSeqAt maps a visible message to the backend turn seq it diverges from. Rehydrated
  // snapshots carry metadata.custom.backendSeq from the GET /threads/{id}/messages ids
  // (msg-1, msg-2, ...). Fresh in-memory turns fall back to the visible index + 1 because
  // normal Aura conversations start at the first user turn (no persisted system row).
  const backendSeqAt = useCallback(
    (index: number): number => {
      if (index < 0) return 0;
      const seq = messages[index]?.metadata?.custom?.backendSeq;
      return typeof seq === 'number' && Number.isFinite(seq) && seq > 0 ? seq : index + 1;
    },
    [messages],
  );

  // onEdit (D-09): edit a USER turn → slice to the parent, append the edited user turn (a
  // fresh id), then POST /edit (fork a sibling branch off the diverging turn's parent +
  // re-run). The runtime tracks the prior user turn + its answer as a sibling branch.
  const onEdit = useCallback(
    async (message: AppendMessage) => {
      const text = appendMessageText(message);
      const sourceId = message.sourceId ?? message.parentId;
      const sourceIndex = messages.findIndex((m) => m.id === sourceId);
      const seq = backendSeqAt(sourceIndex);
      // The edited user turn replaces the old one (a fresh id); everything after the
      // edited source is dropped from THIS branch (the runtime keeps it as the sibling).
      const base: ThreadMessageLike[] = [...messages.slice(0, sourceIndex), userMessage(text)];
      await foldReRun(
        `/api/conversations/${threadId}/edit`,
        { diverge_seq: seq, role: 'user', content: text },
        base,
      );
    },
    [threadId, messages, backendSeqAt, foldReRun],
  );

  // onReload (D-09): regenerate an ASSISTANT turn → slice to the parent user turn, then
  // POST /edit (role assistant, no content) so the agent produces a fresh assistant turn on
  // a new sibling branch. parentId is the user turn the assistant answered.
  const onReload = useCallback(
    async (parentId: string | null) => {
      const parentIndex = messages.findIndex((m) => m.id === parentId);
      // WR-02: an assistant turn's parent (the user turn it answered) is ALWAYS in the
      // visible list, so a not-found parent here is a stale/unknown id — never the legitimate
      // first-turn case (unlike onEdit, whose parent can be the invisible system/root). An
      // unknown parent would fork at seq 1 (the system turn) and replace the whole visible
      // thread (base = slice(0,0)) — a silent destructive re-run. Bail before it.
      if (parentIndex < 0) return;
      const seq = backendSeqAt(parentIndex) + 1; // the assistant turn after its user parent
      const base: ThreadMessageLike[] = messages.slice(0, parentIndex + 1);
      await foldReRun(
        `/api/conversations/${threadId}/edit`,
        { diverge_seq: seq, role: 'assistant', content: '' },
        base,
      );
    },
    [threadId, messages, backendSeqAt, foldReRun],
  );

  // 25-07: edit/regenerate + branch nav now ride this same runtime over the path-aware
  // backend (plan 25-06). onEdit forks a user-turn branch; onReload forks an assistant
  // branch; the runtime models the tree from setMessages so BranchPickerPrimitive
  // (BranchPicker.tsx) navigates siblings. Copy/Edit/Reload are the ONLY action-bar verbs
  // (UI-SPEC — the feedback rating group is Phase 26).
  const runtime = useExternalStoreRuntime<ThreadMessageLike>({
    messages,
    isRunning,
    convertMessage: (m) => m,
    onNew,
    onEdit,
    onReload,
    onCancel,
  });

  return (
    <AssistantRuntimeProvider runtime={runtime}>
      <ThreadPrimitive.Root className="flex h-full min-h-0 flex-col">
        <ThreadPrimitive.Viewport className="flex-1 space-y-4 overflow-y-auto px-3 py-4 sm:px-4">
          <AuiIf condition={(s) => s.thread.isEmpty}>
            <div className="grid h-full place-items-center py-8 text-center">
              <div className="flex flex-col items-center gap-3 px-6">
                <h2 className="font-display text-4xl font-medium text-text sm:text-5xl">
                  {t('chat.empty.thread.heading')}
                </h2>
                <p className="max-w-sm text-sm text-text-muted">{t('chat.empty.thread.body')}</p>
              </div>
            </div>
          </AuiIf>

          <ThreadPrimitive.Messages>
            {({ message }) => (message.role === 'user' ? <UserMessage /> : <AssistantMessage />)}
          </ThreadPrimitive.Messages>
        </ThreadPrimitive.Viewport>

        {/* Running-status row: role="status" announces the active turn politely. */}
        {isRunning ? (
          <p role="status" className="px-3 py-1 text-[0.75rem] text-text-muted sm:px-4">
            {t('chat.running')}
          </p>
        ) : null}

        <Composer uploads={uploads} />
      </ThreadPrimitive.Root>
    </AssistantRuntimeProvider>
  );
}

function UserMessage() {
  const { t } = useTranslation();
  const message = useAuiState((s) => s.message) as ThreadMessageLike;
  const metadata = message.metadata?.custom as { attachments?: readonly Asset[] } | undefined;
  const attachments = metadata?.attachments ?? [];
  return (
    <MessagePrimitive.Root className="ml-auto flex max-w-[80%] flex-col items-end gap-1">
      {/* Edit mode: a message-scoped composer whose Send fires onEdit (fork + re-run). */}
      <AuiIf condition={({ composer }) => composer.isEditing}>
        <ComposerPrimitive.Root className="flex w-full flex-col gap-2 rounded-[var(--radius-md)] border border-accent/40 bg-surface-2 px-3 py-2">
          <ComposerPrimitive.Input
            aria-label={t('chat.edit.label')}
            className="w-full resize-none bg-transparent text-sm text-text outline-none"
          />
          <div className="flex items-center justify-end gap-2">
            <ComposerPrimitive.Cancel
              aria-label={t('chat.edit.cancel')}
              className="text-[0.75rem] text-text-muted outline-none hover:text-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
            >
              {t('chat.edit.cancel')}
            </ComposerPrimitive.Cancel>
            <ComposerPrimitive.Send className="rounded-[var(--radius-sm)] bg-accent px-2 py-1 text-[0.75rem] font-medium text-on-accent outline-none focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent">
              {t('chat.edit.save')}
            </ComposerPrimitive.Send>
          </div>
        </ComposerPrimitive.Root>
      </AuiIf>

      {/* Normal view: the rendered user turn + the action bar. */}
      <AuiIf condition={({ composer }) => !composer.isEditing}>
        <div className="rounded-3xl bg-surface-2 px-5 py-3">
          <MessagePrimitive.Parts
            components={{
              Text: () => (
                <div className="whitespace-pre-wrap text-base leading-relaxed text-text">
                  <MarkdownText />
                </div>
              ),
            }}
          />
        </div>
        {attachments.length > 0 ? (
          <div className="flex flex-col items-end gap-2">
            {attachments.map((asset) => (
              <AttachmentCard key={asset.id} asset={asset} />
            ))}
          </div>
        ) : null}
        {/* Edit a user turn → onEdit forks a branch + re-runs. Copy is the minimum verb. */}
        <ActionBarPrimitive.Root className="flex items-center gap-2 opacity-0 transition-opacity focus-within:opacity-100 hover:opacity-100">
          <ActionBarPrimitive.Edit
            aria-label={t('chat.action.edit')}
            className="text-[0.75rem] text-text-muted outline-none hover:text-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
          >
            {t('chat.action.edit')}
          </ActionBarPrimitive.Edit>
          <ActionBarPrimitive.Copy
            aria-label={t('chat.action.copy')}
            className="text-[0.75rem] text-text-muted outline-none hover:text-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
          >
            {t('chat.action.copy')}
          </ActionBarPrimitive.Copy>
          <BranchPicker />
        </ActionBarPrimitive.Root>
      </AuiIf>
    </MessagePrimitive.Root>
  );
}

function AssistantMessage() {
  const { t } = useTranslation();
  return (
    <MessagePrimitive.Root className="max-w-[90%] space-y-2">
      <MessagePrimitive.Parts
        components={{
          // Assistant prose → sanitized markdown.
          Text: () => (
            <div className="text-base leading-relaxed text-text">
              <MarkdownText />
            </div>
          ),
          // CoT → collapsible drawer (D-01). The drawer reads the reasoning text
          // from the part via the reasoning render-fn arg.
          Reasoning: ({ text }) => <ReasoningDrawer text={text} />,
          // Tool activity → typed display when a trusted backend normalizer
          // produced an aura.display payload (Phase 26, DISP-02); otherwise the
          // raw escaped card (D-02 / D-FALLBACK). The branch lives in ToolFallback.
          tools: {
            Fallback: ToolFallback,
          },
        }}
      />
      <MessagePrimitive.Error>
        <p role="alert" className="text-sm text-danger">
          {/* The reducer already routes RUN_ERROR into an error text part; this
              is the runtime-level fallback for a hard message error. */}
        </p>
      </MessagePrimitive.Error>
      {/* Assistant action bar: Copy + Reload (regenerate) ONLY — the feedback rating
          group is Phase 26 (UI-SPEC). Reload forks an assistant-turn branch. */}
      <ActionBarPrimitive.Root className="flex items-center gap-2 opacity-0 transition-opacity focus-within:opacity-100 hover:opacity-100">
        <ActionBarPrimitive.Copy
          aria-label={t('chat.action.copy')}
          className="text-[0.75rem] text-text-muted outline-none hover:text-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
        >
          {t('chat.action.copy')}
        </ActionBarPrimitive.Copy>
        <ActionBarPrimitive.Reload
          aria-label={t('chat.action.reload')}
          className="text-[0.75rem] text-text-muted outline-none hover:text-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
        >
          {t('chat.action.reload')}
        </ActionBarPrimitive.Reload>
        <BranchPicker />
      </ActionBarPrimitive.Root>
    </MessagePrimitive.Root>
  );
}

/**
 * The tools.Fallback render: the single seam where a tool turn becomes a typed
 * display. It reads the custom `display` payload off the stored message part
 * (the sseAdapter attaches it by toolCallId, live or on replay) via
 * useMessagePart — the external-store runtime passes our ThreadMessageLike part
 * through unchanged (convertMessage: identity), so the field survives.
 *
 * D-15 progressive swap: while a tool runs, no payload exists yet → the raw
 * running card stays; the typed display replaces it only once the aura.display
 * payload arrives on completion. D-FALLBACK: no/unknown payload → the raw card.
 */
const ToolFallback: ToolCallMessagePartComponent = ({ toolName, argsText, result, isError }) => {
  const part = useAuiState((s) => s.part) as { display?: unknown };
  const display: DisplayPayload | undefined = isDisplayPayload(part.display)
    ? part.display
    : undefined;
  const resultText = typeof result === 'string' ? result : undefined;

  if (display !== undefined) {
    return (
      <DisplayRouter
        payload={display}
        toolName={toolName}
        argsText={argsText}
        {...(resultText !== undefined ? { result: resultText } : {})}
        {...(isError !== undefined ? { isError } : {})}
      />
    );
  }
  return (
    <ToolActivityCard
      toolName={toolName}
      argsText={argsText}
      {...(resultText !== undefined ? { result: resultText } : {})}
      {...(isError !== undefined ? { isError } : {})}
    />
  );
};
