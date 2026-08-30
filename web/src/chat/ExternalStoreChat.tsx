import { useCallback, useMemo, useEffect, useLayoutEffect, useRef, useState } from 'react';
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
import { ThreadApprovalCards } from '../approvals/ThreadApprovalCards';
import { useThreadApprovals } from '../approvals/useThreadApprovals';
import type { Approval } from '../approvals/useApprovals';
import { Composer, type ComposerDraftPrompt } from './Composer';
import { EmptyThreadStarters } from './EmptyThreadStarters';
import { listThreadAssets } from './attachments/api';
import { createAuraAttachmentAdapter } from './attachments/auraAttachmentAdapter';
import { useComposerSkills } from './composer/useComposerSkills';
import { resolveGarageMentions } from './composer/garageMentions';
import { resolveSlashSubmission } from './composer/skillTrigger';
import { CompactionMarker } from './compaction/CompactionMarker';
import { CompactionStatus } from './compaction/CompactionStatus';
import { useThreadCompaction } from './compaction/useThreadCompaction';
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
  isAbortError,
  isAbortSignalAborted,
  userMessage,
} from './ExternalStoreChat_folds';
import { useAssetActions } from './ExternalStoreChat_assets';
import { useStreamFolds } from './ExternalStoreChat_streams';
import { useBranchEdits } from './ExternalStoreChat_branches';
import { useLiveRunAttach } from './ExternalStoreChat_liveRun';
import { useSteerSend } from './ExternalStoreChat_steer';
import { SteerNotice } from './SteerNotice';
import { fetchThreadMessages } from './sseAdapter';
import { cancelRun, streamRunResilient } from './sseResume';
import { useRunUsageLifecycle, type RunUsageEvent } from './runUsage';
import { useRunUsageBaseline, type RunSessionBaselineListener } from './useRunUsageBaseline';
import { AutoSpeak } from './voice/AutoSpeak';
import { useVoiceRuntime } from './voice/useVoiceRuntime';
import { useApprovalFocus } from './useApprovalFocus';

const ignoreDraftPrompt = () => undefined;

export interface ExternalStoreChatProps {
  /** Conversation/thread id the run is POSTed against. */
  readonly threadId: string;
  /** Create/select a conversation before the first send when no thread is active. */
  readonly onEnsureThread?: (initialPrompt: string) => Promise<string>;
  readonly onUsage?: (event: RunUsageEvent) => void;
  readonly onUsageBaseline?: RunSessionBaselineListener;
  readonly allocateUsageRunId?: () => number;
  /**
   * 37B seam (mirrors onUsage): fires when a run emits an `aura.artifact` descriptor,
   * carrying its asset_id. AppShell invalidates ['assets', threadId] + drives the
   * one-time Artefatti panel auto-open (D-11). Forwarded into streamRun/streamPost.
   */
  readonly onArtifact?: (assetId: string | undefined) => void;
  readonly draftPrompt?: ComposerDraftPrompt | undefined;
  readonly onDraftPromptConsumed?: (nonce: number) => void;
  readonly onRequestDraftPrompt?: (text: string) => void;
  /** 37D: threads AppShell's startNewConversation to the composer's new-chat quick action. */
}

export function ExternalStoreChat({
  threadId,
  onEnsureThread,
  onUsage,
  onUsageBaseline,
  allocateUsageRunId,
  onArtifact,
  draftPrompt,
  onDraftPromptConsumed,
  onRequestDraftPrompt = ignoreDraftPrompt,
}: ExternalStoreChatProps) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [messages, setMessages] = useState<ThreadMessageLike[]>([]);
  const [historyReadiness, setHistoryReadiness] = useState<{
    readonly threadId: string;
    readonly status: 'loading' | 'ready' | 'error';
  }>({ threadId, status: threadId.length === 0 ? 'ready' : 'loading' });
  const historyReadinessRef = useRef(historyReadiness);
  useEffect(() => {
    historyReadinessRef.current = historyReadiness;
  }, [historyReadiness]);
  const [isRunning, setIsRunning] = useState(false);
  const abortRef = useRef<AbortController | null>(null);
  const historyAbortRef = useRef<AbortController | null>(null);
  const historyRequestRef = useRef(0);
  const isRunningRef = useRef(false);
  /** The live runId of the stream this tab drives/watches — the §4.4 cancel key. */
  const activeRunIdRef = useRef<string | null>(null);
  const usageThreadRef = useRef<string | null>(null);
  const usageLifecycle = useRunUsageLifecycle(onUsage, allocateUsageRunId);
  const prepareUsageBaseline = useRunUsageBaseline(onUsageBaseline);
  const composerInputRef = useRef<HTMLTextAreaElement | null>(null);
  const [composerInput, setComposerInput] = useState<HTMLTextAreaElement | null>(null);
  const approvalFocusRef = useRef<(next: Approval | undefined) => void>(() => undefined);
  const dispatchApprovalFocus = useCallback((next: Approval | undefined) => {
    approvalFocusRef.current(next);
  }, []);
  // The upload lifecycle is the runtime's now (assistant-ui AttachmentAdapter), not a hook
  // holding component state beside it; the composer renders progress straight off it.
  const attachments = useMemo(() => createAuraAttachmentAdapter({ threadId }), [threadId]);
  const skills = useComposerSkills();
  const compaction = useThreadCompaction(threadId, messages);
  const reasoningCaps = useReasoningCapabilities();
  const conversation = useConversation(threadId).data;
  const hydratedEffort = conversation?.ReasoningEffort;
  /** RS-07 §4.2: a set live_run_id means a detached run is in flight for this thread. */
  const liveRunId = conversation?.live_run_id;
  const steer = useSteerSend({ threadId, liveRunId, activeRunIdRef, isRunning, setMessages });
  const { effort, setEffort } = useReasoningEffort(
    threadId,
    hydratedEffort,
    reasoningCaps.levels,
    reasoningCaps.settled,
  );
  // assistant-ui publishes adapter callbacks in a passive effect; an older onNew callback must
  // still observe a selection committed by the current render.
  const effortRef = useRef(effort);
  useLayoutEffect(() => {
    effortRef.current = effort;
  }, [effort]);

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
      historyRequestRef.current += 1;
      historyAbortRef.current?.abort();
      historyAbortRef.current = null;
      // The '/'-command is part of the message the operator typed, so the skill is read
      // back out of it here rather than held in a separate pinned-chip state.
      const { skill, command, text } = resolveSlashSubmission(appendMessageText(message), skills);
      const documentScope = resolveGarageMentions(text);
      // A command is not a message. The menu normally executes it on selection, but the
      // text can reach a send whole — typed out, menu dismissed — and a '/compact' that
      // silently became a question to the agent would be the worst of both.
      if (command !== null) {
        compaction.runCommand(command);
        return;
      }
      // D-10: a live run for this thread means this submit redirects it instead of starting a
      // new one. Gated on NO attachments — the steer route is text-only (internal/steer.Push),
      // so a file attached while live still falls through to the normal (and still-locked) path
      // rather than silently dropping it.
      if ((message.attachments?.length ?? 0) === 0 && (await steer.trySend(text))) return;
      // The runtime only hands over attachments it has already sent (adapter.send ran), so
      // there is no separate "is anything still uploading" gate to consult here.
      // The attachment id is the adapter's stable client id, so the ASSET id the run
      // envelope needs comes from the adapter's map rather than from the attachment itself.
      const readyAttachments = (message.attachments ?? []).flatMap((a) => {
        const asset = attachments.assetFor(a.id);
        return asset === undefined ? [] : [asset];
      });
      const readyAttachmentIds = readyAttachments.map((asset) => asset.id);
      const user = userMessage(text, readyAttachments);
      let assistantIndex = -1;
      setMessages((prev) => {
        assistantIndex = prev.length + 1;
        return [...prev, user];
      });
      isRunningRef.current = true;
      setIsRunning(true);

      const controller = new AbortController();
      abortRef.current = controller;
      let runThreadId = threadId;
      usageThreadRef.current = runThreadId;
      const usageRunId = usageLifecycle.start();
      try {
        if (runThreadId.length === 0 && onEnsureThread !== undefined) {
          runThreadId = await onEnsureThread(text);
          usageThreadRef.current = runThreadId;
        }
        if (controller.signal.aborted) return;
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
        if (!(await prepareUsageBaseline(runThreadId, usageRunId, controller.signal))) return;
        await streamRunResilient({
          threadId: runThreadId,
          userText: text,
          attachmentIds: readyAttachmentIds,
          ...(skill !== null ? { skill } : {}),
          ...(documentScope.length > 0 ? { documentScope } : {}),
          effort: effortRef.current,
          signal: controller.signal,
          connectionLostNote: t('chat.error.connectionLost'),
          onRunId: (runId) => {
            activeRunIdRef.current = runId;
          },
          onSnapshotReplace: setMessages,
          ...(onArtifact !== undefined ? { onArtifact } : {}),
          onSteer: steer.onFrame,
          onUpdate: (assistant, usage) => {
            usageLifecycle.update(usageRunId, usage);
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
      } catch (err) {
        if (!isAbortError(err)) {
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
        usageLifecycle.settle(usageRunId);
        if (abortRef.current === controller) {
          isRunningRef.current = false;
          setIsRunning(false);
          abortRef.current = null;
          activeRunIdRef.current = null;
        }
        invalidateRuntimeReads(runThreadId);
      }
    },
    [
      threadId,
      onEnsureThread,
      usageLifecycle,
      onArtifact,
      invalidateRuntimeReads,
      t,
      attachments,
      skills,
      prepareUsageBaseline,
      compaction,
      steer,
    ],
  );

  const onCancel = useCallback(() => {
    abortRef.current?.abort();
    const runId = activeRunIdRef.current;
    activeRunIdRef.current = null;
    if (runId !== null) {
      // §4.4: with run-detach on, abort only detaches this viewer — the cancel
      // POST is what actually stops the agent. Fire-and-forget; the composer is
      // already released, so a failure is only logged (the run stays bounded by
      // the server's Budget/wallclock caps either way).
      void cancelRun(runId).catch((err: unknown) => {
        console.error('aura: run cancel failed', err);
      });
    }
    return Promise.resolve();
  }, []);

  useEffect(
    () => () => {
      const controller = abortRef.current;
      abortRef.current = null;
      controller?.abort();
    },
    [],
  );

  useEffect(() => {
    const isFirstThreadAdoption = usageThreadRef.current === threadId;
    if (isFirstThreadAdoption) {
      usageThreadRef.current = null;
    } else {
      usageLifecycle.clear();
      if (isRunningRef.current) {
        const previousViewer = abortRef.current;
        abortRef.current = null;
        activeRunIdRef.current = null;
        isRunningRef.current = false;
        setIsRunning(false);
        previousViewer?.abort();
      }
    }
    historyRequestRef.current += 1;
    const request = historyRequestRef.current;
    const nextHistoryStatus = threadId.length === 0 ? 'ready' : 'loading';
    queueMicrotask(() => {
      if (request !== historyRequestRef.current) return;
      const current = historyReadinessRef.current;
      if (current.threadId === threadId && current.status === nextHistoryStatus) return;
      const next = { threadId, status: nextHistoryStatus } as const;
      historyReadinessRef.current = next;
      setHistoryReadiness(next);
    });
    const clearLoadedMessages = () => {
      queueMicrotask(() => {
        if (request !== historyRequestRef.current) return;
        setMessages([]);
      });
    };
    historyAbortRef.current?.abort();
    if (threadId.length === 0) {
      historyAbortRef.current = null;
      clearLoadedMessages();
      return;
    }
    if (isFirstThreadAdoption && isRunningRef.current) {
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
        setHistoryReadiness({ threadId, status: 'ready' });
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
        setHistoryReadiness({ threadId, status: 'error' });
      })
      .finally(() => {
        if (request === historyRequestRef.current) historyAbortRef.current = null;
      });

    return () => {
      controller.abort();
    };
  }, [threadId, usageLifecycle, t]);

  // Attachment-chip actions live in ./ExternalStoreChat_assets (600-LOC split).
  const { handleAssetRetry, handleAssetPromote, handleAssetRemove } = useAssetActions(setMessages);

  // The stream scaffolding the three non-primary streams share (re-run from a branch point,
  // HITL resume, live-run attach) lives in ./ExternalStoreChat_streams — 600-LOC split.
  const { foldReRun, foldAppendedStream, foldResumeRun } = useStreamFolds({
    threadId,
    historyRequestRef,
    historyAbortRef,
    abortRef,
    isRunningRef,
    activeRunIdRef,
    usageThreadRef,
    setIsRunning,
    setMessages,
    usageLifecycle,
    prepareUsageBaseline,
    invalidateRuntimeReads,
    onArtifact,
    streamErrorText: t('chat.error.stream'),
  });

  const resumeRun = useCallback(async () => {
    if (threadId.length === 0) return;
    await foldResumeRun(threadId);
  }, [foldResumeRun, threadId]);

  // RS-07 §4.2 reload-attach lives in ./ExternalStoreChat_liveRun (600-LOC
  // split): discovery via live_run_id + the read-only full-replay attach.
  useLiveRunAttach({
    threadId,
    liveRunId,
    historyReadiness,
    isRunningRef,
    activeRunIdRef,
    foldAppendedStream,
    setMessages,
    onArtifact,
    onSteer: steer.onFrame,
  });

  const threadApprovals = useThreadApprovals(threadId, resumeRun, dispatchApprovalFocus);
  const requestApprovalFocus = useApprovalFocus(
    threadId,
    threadApprovals.isPending,
    isRunning,
    composerInput,
  );
  useLayoutEffect(() => {
    approvalFocusRef.current = requestApprovalFocus;
    return () => {
      if (approvalFocusRef.current === requestApprovalFocus) {
        approvalFocusRef.current = () => undefined;
      }
    };
  }, [requestApprovalFocus]);

  // The D-09 branch-edit callbacks (onEdit/onReload) live in ./ExternalStoreChat_branches
  // (600-LOC split); the stream fold stays here.
  const { onEdit, onReload } = useBranchEdits({ threadId, messages, foldReRun });

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
    adapters: { ...voiceAdapters, attachments },
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
                  {historyReadiness.threadId === threadId && historyReadiness.status === 'ready' ? (
                    <EmptyThreadStarters onRequestDraftPrompt={onRequestDraftPrompt} />
                  ) : null}
                </div>
              </div>
            </AuiIf>

            <ThreadPrimitive.Messages>
              {({ message }) => (
                <>
                  {message.role === 'user' ? (
                    <UserMessage
                      onAssetRetry={handleAssetRetry}
                      onAssetPromote={handleAssetPromote}
                      onAssetRemove={handleAssetRemove}
                    />
                  ) : (
                    <AssistantMessage />
                  )}
                  {/* The marker belongs BETWEEN two turns, so it is drawn after the last
                      message the summary speaks for rather than as a message of its own —
                      a synthetic entry in the list would be one the runtime could branch
                      from, edit, or hand to a re-run. */}
                  {message.id === compaction.anchorId ? (
                    <CompactionMarker state={compaction.state} />
                  ) : null}
                </>
              )}
            </ThreadPrimitive.Messages>
          </ThreadPrimitive.Viewport>

          {/* Running-status row: role="status" announces the active turn politely. */}
          {isRunning ? (
            <p role="status" className="px-3 py-1 text-[0.75rem] text-text-muted sm:px-4">
              {t('chat.running')}
            </p>
          ) : null}

          <CompactionStatus
            running={compaction.running}
            failure={compaction.failure}
            onDismiss={compaction.dismissFailure}
          />

          {/* D-10: a live detached run no longer blocks Send — this hint just orients the
              operator toward the redirect control Composer now renders. */}
          {liveRunId !== undefined && liveRunId.length > 0 ? (
            <p role="status" className="px-3 py-1 text-[0.75rem] text-text-muted sm:px-4">
              {t('chat.liveRun.hint')}
            </p>
          ) : null}
          <SteerNotice notice={steer.notice} refusal={steer.refusalText} />

          <ThreadApprovalCards
            approvals={threadApprovals.approvals}
            isStreaming={isRunning}
            onResolutionStarted={threadApprovals.onResolutionStarted}
            onResolutionFailed={threadApprovals.onResolutionFailed}
            onResolved={threadApprovals.onResolved}
          />
          <Composer
            inputRef={composerInputRef}
            onInputAvailable={setComposerInput}
            approvalLocked={threadApprovals.isPending}
            sendBlocked={false} // D-10: a live run redirects (steerAvailable) instead of blocking.
            steerAvailable={steer.available}
            onSteerSubmit={(steerText) => void steer.trySend(steerText)}
            draftPrompt={draftPrompt}
            onDraftPromptConsumed={onDraftPromptConsumed}
            skills={skills}
            onCommand={compaction.runCommand}
            effort={effort}
            effortLevels={reasoningCaps.levels}
            onEffortChange={setEffort}
          />
        </ThreadPrimitive.Root>
      </SourceExplorerProvider>
    </AssistantRuntimeProvider>
  );
}
