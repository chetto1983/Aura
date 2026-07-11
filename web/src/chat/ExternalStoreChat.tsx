import { useCallback, useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQueryClient } from '@tanstack/react-query';
import {
  AssistantRuntimeProvider,
  AuiIf,
  ThreadPrimitive,
  useExternalStoreRuntime,
  type AppendMessage,
  type ThreadMessageLike,
} from '@assistant-ui/react';
import {
  CONVERSATION_KEY,
  CONVERSATION_ROT_EVENTS_KEY,
  useConversation,
} from '../conversations/useConversations';
import { Composer, type ComposerDraftPrompt } from './Composer';
import { deleteAsset, listThreadAssets, promoteAsset, retryAsset } from './attachments/api';
import { useAttachmentUploads } from './attachments/useAttachmentUploads';
import { useComposerSkills } from './composer/useComposerSkills';
import { usePinnedSkill } from './composer/usePinnedSkill';
import { useReasoningCapabilities } from './composer/useReasoningCapabilities';
import { useReasoningEffort } from './composer/useReasoningEffort';
import type { Asset } from './attachments/types';
import { SourceExplorerProvider } from './displays/SourceExplorerContext';
import { AssistantMessage, UserMessage } from './ExternalStoreChat_messages';
import {
  appendMessageText,
  assistantErrorMessage,
  attachAssetsToUserMessages,
  foldAgentOntoAssistant,
  isAbortSignalAborted,
  removeAssetFromMessages,
  replaceAssetInMessages,
  userMessage,
} from './ExternalStoreChat_folds';
import { fetchThreadMessages, streamPost, streamRun, type TurnUsage } from './sseAdapter';
import { AutoSpeak } from './voice/AutoSpeak';
import { useVoiceRuntime } from './voice/useVoiceRuntime';

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
// Pure message/asset-fold helpers live in ./ExternalStoreChat_folds (refactor-on-touch to
// keep this file under the 600-LOC cap); the presentational rows live in _messages.

export interface ExternalStoreChatProps {
  /** Conversation/thread id the run is POSTed against. */
  readonly threadId: string;
  /** Create/select a conversation before the first send when no thread is active. */
  readonly onEnsureThread?: (initialPrompt: string) => Promise<string>;
  /** 25-04 seam: receives the latest per-turn usage off the SSE STATE_DELTA. */
  readonly onUsage?: (usage: TurnUsage | undefined) => void;
  /**
   * 37B seam (mirrors onUsage): fires when a run emits an `aura.artifact` descriptor,
   * carrying its asset_id. AppShell invalidates ['assets', threadId] + drives the
   * one-time Artefatti panel auto-open (D-11). Forwarded into streamRun/streamPost.
   */
  readonly onArtifact?: (assetId: string | undefined) => void;
  /**
   * Continue-after-resume nonce (D-05): each increment re-drives the run with a
   * no-message POST /agent/run and FOLDS the resumed stream into the chat lane (so the
   * resumed turn renders here, not in a discarded fetch). AppShell bumps it after an
   * inline approval resolves. The initial value is ignored (only changes re-drive).
   */
  readonly resumeNonce?: number;
  readonly draftPrompt?: ComposerDraftPrompt | undefined;
  /** 37D: threads AppShell's startNewConversation to the composer's new-chat quick action. */
  readonly onNewChat?: () => void | Promise<void>;
}

export function ExternalStoreChat({
  threadId,
  onEnsureThread,
  onUsage,
  onArtifact,
  resumeNonce = 0,
  draftPrompt,
  onNewChat,
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
  const skills = useComposerSkills();
  const { pinnedSkill, setPinnedSkill } = usePinnedSkill();
  // 37E reasoning-effort selector: the advertised levels (D-13, degrades to {auto,off}) and the
  // per-conversation effort hydrated from the conversation DTO's persisted ReasoningEffort (37E-03),
  // restored on reopen and NEVER cleared after send.
  const reasoningCaps = useReasoningCapabilities();
  const hydratedEffort = useConversation(threadId).data?.ReasoningEffort;
  const { effort, setEffort } = useReasoningEffort(threadId, hydratedEffort, reasoningCaps.levels);

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
          ...(pinnedSkill !== null ? { skill: pinnedSkill.name } : {}),
          // The effort rides every send (buildAuraRunBody omits 'auto'); unlike the pinned skill
          // it is a per-conversation preference and is NOT cleared after the turn.
          effort,
          signal: controller.signal,
          ...(onArtifact !== undefined ? { onArtifact } : {}),
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
        // The pinned skill applies to exactly one turn (mirrors uploads.clearReady).
        if (pinnedSkill !== null) setPinnedSkill(null);
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
    [
      threadId,
      onEnsureThread,
      onUsage,
      onArtifact,
      invalidateRuntimeReads,
      t,
      uploads,
      pinnedSkill,
      setPinnedSkill,
      effort,
    ],
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
      .then(async (loaded) => {
        if (isAbortSignalAborted(controller.signal) || request !== historyRequestRef.current)
          return;
        let assets: Asset[] = [];
        try {
          assets = await listThreadAssets(threadId, controller.signal);
        } catch (err) {
          if (err instanceof DOMException && err.name === 'AbortError') return;
        }
        if (isAbortSignalAborted(controller.signal) || request !== historyRequestRef.current)
          return;
        // D-15: split by source_kind BEFORE folding. Uploads (web/telegram/cli) keep
        // the existing user-turn fold; agent deliverables rehydrate onto assistant
        // turns (their download chip survives saved-conversation open with no reload).
        const uploads = assets.filter((asset) => asset.source_kind !== 'agent');
        const agent = assets.filter((asset) => asset.source_kind === 'agent');
        setMessages(foldAgentOntoAssistant(attachAssetsToUserMessages(loaded, uploads), agent));
      })
      .catch((err: unknown) => {
        if (
          isAbortSignalAborted(controller.signal) ||
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

  const handleAssetRetry = useCallback((assetID: string) => {
    void retryAsset(assetID)
      .then((asset) => {
        setMessages((prev) => replaceAssetInMessages(prev, asset));
      })
      .catch(() => undefined);
  }, []);

  const handleAssetPromote = useCallback((assetID: string) => {
    void promoteAsset(assetID)
      .then((asset) => {
        setMessages((prev) => replaceAssetInMessages(prev, asset));
      })
      .catch(() => undefined);
  }, []);

  const handleAssetRemove = useCallback((assetID: string) => {
    void deleteAsset(assetID)
      .then(() => {
        setMessages((prev) => removeAssetFromMessages(prev, assetID));
      })
      .catch(() => undefined);
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
          ...(onArtifact !== undefined ? { onArtifact } : {}),
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
    [onUsage, onArtifact, invalidateRuntimeReads],
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
          ...(onArtifact !== undefined ? { onArtifact } : {}),
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
  }, [resumeNonce, threadId, onUsage, onArtifact, invalidateRuntimeReads]);

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

  // 25-07: edit/regenerate + branch nav ride this runtime; onEdit/onReload fork sibling
  // branches the runtime models from setMessages so BranchPicker navigates them.
  // Voice (37C-05): caps-gated speech + dictation adapters attach here (undefined ⇒ native
  // degrade); useVoiceRuntime revokes the speech adapter URLs via dispose() on unmount.
  const voiceAdapters = useVoiceRuntime();
  const runtime = useExternalStoreRuntime<ThreadMessageLike>({
    messages,
    isRunning,
    convertMessage: (m) => m,
    onNew,
    onEdit,
    onReload,
    onCancel,
    adapters: voiceAdapters,
  });

  return (
    <AssistantRuntimeProvider runtime={runtime}>
      <AutoSpeak />
      {/* The shared read-only Source Explorer (D-13): one sheet, two entry points
          (the "Sources (N)" button + the citation click-through), one registry. */}
      <SourceExplorerProvider>
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
              {({ message }) =>
                message.role === 'user' ? (
                  <UserMessage
                    onAssetRetry={handleAssetRetry}
                    onAssetPromote={handleAssetPromote}
                    onAssetRemove={handleAssetRemove}
                  />
                ) : (
                  <AssistantMessage />
                )
              }
            </ThreadPrimitive.Messages>
          </ThreadPrimitive.Viewport>

          {/* Running-status row: role="status" announces the active turn politely. */}
          {isRunning ? (
            <p role="status" className="px-3 py-1 text-[0.75rem] text-text-muted sm:px-4">
              {t('chat.running')}
            </p>
          ) : null}

          <Composer
            uploads={uploads}
            draftPrompt={draftPrompt}
            skills={skills}
            pinnedSkill={pinnedSkill}
            onPinSkill={setPinnedSkill}
            onNewChat={onNewChat}
            effort={effort}
            effortLevels={reasoningCaps.levels}
            onEffortChange={setEffort}
          />
        </ThreadPrimitive.Root>
      </SourceExplorerProvider>
    </AssistantRuntimeProvider>
  );
}
