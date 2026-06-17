import { useCallback, useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  ActionBarPrimitive,
  AssistantRuntimeProvider,
  AuiIf,
  ComposerPrimitive,
  MessagePrimitive,
  ThreadPrimitive,
  useExternalStoreRuntime,
  type AppendMessage,
  type ThreadMessageLike,
} from '@assistant-ui/react';
import { MarkdownTextPrimitive } from '@assistant-ui/react-markdown';
import { BranchPicker } from './BranchPicker';
import { Composer } from './Composer';
import { ReasoningDrawer } from './ReasoningDrawer';
import { ToolActivityCard } from './ToolActivityCard';
import { streamPost, streamRun, type TurnUsage } from './sseAdapter';

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
  /**
   * Continue-after-resume nonce (D-05): each increment re-drives the run with a
   * no-message POST /agent/run and FOLDS the resumed stream into the chat lane (so the
   * resumed turn renders here, not in a discarded fetch). AppShell bumps it after an
   * inline approval resolves. The initial value is ignored (only changes re-drive).
   */
  readonly resumeNonce?: number;
}

export function ExternalStoreChat({ threadId, onUsage, resumeNonce = 0 }: ExternalStoreChatProps) {
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

  // foldReRun streams a backend branch re-run (edit / regenerate) onto the supplied base
  // message list, folding the AG-UI reply onto ONE freshly-id'd assistant message appended
  // after the base. The base already reflects the forked turn (the edited user message, or
  // the truncation before a regenerate), so the external-store runtime keeps the PRIOR
  // version as a SIBLING branch automatically — the BranchPicker lights up without a
  // hand-rolled state machine (RESEARCH "Don't Hand-Roll"). It does NOT re-implement the
  // walk — the server drives LoadManagedHistoryForBranch over the forked path (Task-1).
  const foldReRun = useCallback(
    async (url: string, body: unknown, base: ThreadMessageLike[]) => {
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
        setIsRunning(false);
        abortRef.current = null;
      }
    },
    [onUsage],
  );

  // Continue-after-resume (D-05): when resumeNonce changes (AppShell bumps it after an
  // inline approval resolves), re-drive the run with a no-message POST /agent/run and FOLD
  // the resumed stream into THIS lane's message list, so the resumed turn renders in-thread
  // (not in a discarded fetch). The initial mount (nonce unchanged) is skipped.
  const lastResumeNonce = useRef(resumeNonce);
  useEffect(() => {
    if (resumeNonce === lastResumeNonce.current) return;
    lastResumeNonce.current = resumeNonce;
    if (threadId.length === 0) return;
    const controller = new AbortController();
    abortRef.current = controller;
    // The resumed turn is folded onto a fresh assistant slot appended after whatever the
    // lane currently shows. Each streamed frame replaces that one slot (never N copies):
    // the first onUpdate appends, the rest replace the last element while running.
    const drive = async () => {
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
        setIsRunning(false);
        abortRef.current = null;
      }
    };
    void drive();
  }, [resumeNonce, threadId, onUsage]);

  // divergeSeq maps a frontend message INDEX to the backend turn seq it diverges from. The
  // persisted history is seq=1 (system), then user/assistant turns from seq=2; the visible
  // list (user+assistant only, no system) at index i is backend seq i+2. This is the
  // edit/regenerate divergence point the server forks a sibling branch off of.
  const divergeSeqAt = useCallback((index: number): number => (index < 0 ? 0 : index + 2), []);

  // onEdit (D-09): edit a USER turn → slice to the parent, append the edited user turn (a
  // fresh id), then POST /edit (fork a sibling branch off the diverging turn's parent +
  // re-run). The runtime tracks the prior user turn + its answer as a sibling branch.
  const onEdit = useCallback(
    async (message: AppendMessage) => {
      const text = appendMessageText(message);
      const parentIndex = messages.findIndex((m) => m.id === message.parentId);
      const seq = divergeSeqAt(parentIndex);
      // The edited user turn replaces the old one (a fresh id); everything after the parent
      // is dropped from THIS branch (the runtime keeps it as the sibling).
      const base: ThreadMessageLike[] = [...messages.slice(0, parentIndex), userMessage(text)];
      await foldReRun(
        `/api/conversations/${threadId}/edit`,
        { diverge_seq: seq, role: 'user', content: text },
        base,
      );
    },
    [threadId, messages, divergeSeqAt, foldReRun],
  );

  // onReload (D-09): regenerate an ASSISTANT turn → slice to the parent user turn, then
  // POST /edit (role assistant, no content) so the agent produces a fresh assistant turn on
  // a new sibling branch. parentId is the user turn the assistant answered.
  const onReload = useCallback(
    async (parentId: string | null) => {
      const parentIndex = messages.findIndex((m) => m.id === parentId);
      const seq = divergeSeqAt(parentIndex) + 1; // the assistant turn after its user parent
      const base: ThreadMessageLike[] = messages.slice(0, parentIndex + 1);
      await foldReRun(
        `/api/conversations/${threadId}/edit`,
        { diverge_seq: seq, role: 'assistant', content: '' },
        base,
      );
    },
    [threadId, messages, divergeSeqAt, foldReRun],
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
              <div className="flex flex-col items-center gap-2">
                <h2 className="text-xl font-medium text-text">{t('chat.empty.thread.heading')}</h2>
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
  const { t } = useTranslation();
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
              className="text-[0.6875rem] text-text-muted outline-none hover:text-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
            >
              {t('chat.edit.cancel')}
            </ComposerPrimitive.Cancel>
            <ComposerPrimitive.Send className="rounded-[var(--radius-sm)] bg-accent px-2 py-1 text-[0.6875rem] font-medium text-[#0B0E14] outline-none focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent">
              {t('chat.edit.save')}
            </ComposerPrimitive.Send>
          </div>
        </ComposerPrimitive.Root>
      </AuiIf>

      {/* Normal view: the rendered user turn + the action bar. */}
      <AuiIf condition={({ composer }) => !composer.isEditing}>
        <div className="rounded-[var(--radius-md)] bg-surface-2 px-3 py-2">
          <MessagePrimitive.Parts
            components={{
              Text: () => (
                <div className="whitespace-pre-wrap text-sm leading-relaxed text-text">
                  <MarkdownTextPrimitive />
                </div>
              ),
            }}
          />
        </div>
        {/* Edit a user turn → onEdit forks a branch + re-runs. Copy is the minimum verb. */}
        <ActionBarPrimitive.Root className="flex items-center gap-2 opacity-0 transition-opacity focus-within:opacity-100 hover:opacity-100">
          <ActionBarPrimitive.Edit
            aria-label={t('chat.action.edit')}
            className="text-[0.6875rem] text-text-muted outline-none hover:text-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
          >
            {t('chat.action.edit')}
          </ActionBarPrimitive.Edit>
          <ActionBarPrimitive.Copy
            aria-label={t('chat.action.copy')}
            className="text-[0.6875rem] text-text-muted outline-none hover:text-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
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
      {/* Assistant action bar: Copy + Reload (regenerate) ONLY — the feedback rating
          group is Phase 26 (UI-SPEC). Reload forks an assistant-turn branch. */}
      <ActionBarPrimitive.Root className="flex items-center gap-2 opacity-0 transition-opacity focus-within:opacity-100 hover:opacity-100">
        <ActionBarPrimitive.Copy
          aria-label={t('chat.action.copy')}
          className="text-[0.6875rem] text-text-muted outline-none hover:text-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
        >
          {t('chat.action.copy')}
        </ActionBarPrimitive.Copy>
        <ActionBarPrimitive.Reload
          aria-label={t('chat.action.reload')}
          className="text-[0.6875rem] text-text-muted outline-none hover:text-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
        >
          {t('chat.action.reload')}
        </ActionBarPrimitive.Reload>
        <BranchPicker />
      </ActionBarPrimitive.Root>
    </MessagePrimitive.Root>
  );
}
