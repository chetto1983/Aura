import { useEffect, useMemo, useRef, useState } from 'react';
import { ArrowDown, X } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import {
  AssistantRuntimeProvider,
  fromThreadMessageLike,
  MessagePrimitive,
  ReadonlyThreadProvider,
  ThreadPrimitive,
  useExternalStoreRuntime,
  type ThreadMessage,
  type ThreadMessageLike,
} from '@assistant-ui/react';
import { ReasoningPillPart, ToolFallback } from '../ExternalStoreChat_messages';
import { statusLabelKey } from '../displays/swarmRow';
import { MarkdownText } from '../MarkdownText';
import { openWorkerStream } from './workerStream';
import { WorkerPicker } from './WorkerPicker';
import { useWatchWorker } from './workerWatchControls';
import { WorkerControls } from './WorkerControls';

export interface WorkerPaneProps {
  readonly conversationId: string;
  readonly childId: string;
  readonly onClose: () => void;
}

function toReadonlyMessage(message: ThreadMessageLike, childId: string): ThreadMessage {
  // eslint-disable-next-line @typescript-eslint/no-deprecated -- required by the pinned 0.15 ReadonlyThreadProvider API
  return fromThreadMessageLike(
    message,
    message.id ?? childId,
    message.status ?? { type: 'running' },
  );
}

function WorkerMessage() {
  return (
    <MessagePrimitive.Root className="w-full min-w-0">
      <div className="w-full min-w-0 space-y-2 overflow-x-auto">
        <MessagePrimitive.Parts
          components={{
            Reasoning: ReasoningPillPart,
            Text: () => (
              <div className="w-full min-w-0 text-sm leading-relaxed text-text">
                <MarkdownText constrainProse />
              </div>
            ),
            tools: { Fallback: ToolFallback },
          }}
        />
      </div>
    </MessagePrimitive.Root>
  );
}

export function WorkerPane({ conversationId, childId, onClose }: WorkerPaneProps) {
  const { t } = useTranslation();
  const { workers, statuses, watchWorker } = useWatchWorker();
  const [openedConversationId] = useState(conversationId);
  // The scoped endpoint validates ownership. A nested worker's source card can
  // unmount when we open it, and restored workers need no mounted source card.
  const workerSelected =
    conversationId.length > 0 && childId.length > 0 && openedConversationId === conversationId;
  const workerStatus = statuses.get(childId);
  const lifecycleStatus = workerStatus?.status;
  const executionId = workerStatus?.run_id;
  const streamKey = `${conversationId}\u0000${childId}`;
  const subscription = useRef<{
    key: string;
    runId: string | undefined;
    status: string | undefined;
    revision: number;
    close: () => void;
  } | null>(null);
  const [streamState, setStreamState] = useState<{
    readonly key: string;
    readonly messages: readonly ThreadMessageLike[];
    readonly failed: boolean;
    readonly revision: number;
  }>({ key: streamKey, messages: [], failed: false, revision: 0 });
  const current =
    streamState.key === streamKey
      ? streamState
      : { key: streamKey, messages: [], failed: false, revision: 0 };

  useEffect(() => {
    if (!workerSelected) {
      subscription.current?.close();
      subscription.current = null;
      return;
    }
    const previous = subscription.current;
    const newExecution =
      executionId !== undefined && previous?.runId !== undefined && executionId !== previous.runId;
    const legacyResume =
      executionId === undefined &&
      previous?.runId === undefined &&
      previous?.status !== undefined &&
      previous.status !== 'running' &&
      lifecycleStatus === 'running';
    if (previous?.key === streamKey && !newExecution && !legacyResume) {
      if (executionId !== undefined) previous.runId = executionId;
      previous.status = lifecycleStatus;
      return;
    }
    previous?.close();
    const revision = (previous?.revision ?? 0) + 1;
    let active = true;
    setStreamState({ key: streamKey, messages: [], failed: false, revision });
    const stream = openWorkerStream(conversationId, childId, {
      onMessages: (messages) => {
        if (active) setStreamState({ key: streamKey, messages, failed: false, revision });
      },
      onError: () => {
        if (!active) return;
        setStreamState((previous) => ({
          key: streamKey,
          messages: previous.key === streamKey ? previous.messages : [],
          failed: true,
          revision,
        }));
      },
    });
    subscription.current = {
      key: streamKey,
      runId: executionId,
      status: lifecycleStatus,
      revision,
      close: () => {
        active = false;
        stream.close();
      },
    };
  }, [childId, conversationId, streamKey, workerSelected, lifecycleStatus, executionId]);

  useEffect(
    () => () => {
      subscription.current?.close();
      subscription.current = null;
    },
    [],
  );

  useEffect(() => {
    if (!workerSelected) onClose();
  }, [onClose, workerSelected]);

  const readonlyMessages = useMemo(
    () => current.messages.map((message) => toReadonlyMessage(message, childId)),
    [childId, current.messages],
  );
  const hasContent = current.messages.some(
    (message) => Array.isArray(message.content) && message.content.length > 0,
  );
  const scopeRuntime = useExternalStoreRuntime<ThreadMessageLike>({
    messages: [],
    isRunning: false,
    convertMessage: (message) => message,
    onNew: () => Promise.resolve(),
  });
  const pickerWorkers = useMemo(() => {
    if (workers.some((worker) => worker.child_id === childId)) return workers;
    return [
      ...workers,
      {
        goal_index: workers.length,
        child_id: childId,
        status: statuses.get(childId)?.status ?? ('running' as const),
        goal: childId,
      },
    ];
  }, [childId, statuses, workers]);

  if (!workerSelected) return null;

  const selectedWorker = pickerWorkers.find((worker) => worker.child_id === childId);
  const selectedStatus = statuses.get(childId)?.status ?? selectedWorker?.status ?? 'running';

  return (
    <section className="flex h-full min-h-0 flex-col px-3 pb-3 pt-3">
      <div className="mb-3 flex min-h-[44px] items-center justify-between gap-2 border-b border-border pb-3">
        <div className="min-w-0">
          <h2 className="font-display text-lg font-medium text-text">{t('swarm.pane.title')}</h2>
          <p className="truncate font-mono text-[0.75rem] text-text-faint" title={childId}>
            {childId}
          </p>
        </div>
        <button
          type="button"
          onClick={onClose}
          aria-label={t('swarm.pane.close')}
          data-required-touch-target
          className="grid min-h-[44px] min-w-[44px] shrink-0 place-items-center rounded-full text-text-muted transition-colors hover:bg-surface-2 hover:text-text focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
        >
          <X className="size-4" aria-hidden="true" />
        </button>
      </div>

      <WorkerPicker
        key={childId}
        workers={pickerWorkers}
        statuses={statuses}
        watchedChildId={childId}
        onSelect={(nextChildId) => {
          watchWorker(nextChildId, pickerWorkers);
        }}
      />

      <div className="mb-3 space-y-2 border-b border-border pb-3">
        {workerStatus?.parent_child_id ? (
          <button
            type="button"
            onClick={() => {
              watchWorker(workerStatus.parent_child_id ?? '');
            }}
            className="min-h-11 text-xs text-text-muted hover:text-text"
          >
            {t('swarm.controls.parent')}
          </button>
        ) : null}
        <p
          className="line-clamp-3 break-words text-sm font-medium leading-relaxed text-text"
          title={selectedWorker?.goal ?? childId}
        >
          {selectedWorker?.goal ?? childId}
        </p>
        <p className="text-xs text-text-muted">{t(statusLabelKey(selectedStatus))}</p>
      </div>

      <div className="relative flex min-h-0 flex-1 flex-col">
        <AssistantRuntimeProvider runtime={scopeRuntime}>
          <ReadonlyThreadProvider
            key={`${streamKey}:${String(current.revision)}`}
            messages={readonlyMessages}
          >
            <ThreadPrimitive.Root className="relative flex min-h-0 flex-1 flex-col">
              <ThreadPrimitive.Viewport className="min-h-0 flex-1 space-y-3 overflow-y-auto py-1">
                {current.failed ? (
                  <p role="alert" className="px-1 py-4 text-sm text-danger">
                    {t('swarm.pane.error')}
                  </p>
                ) : !hasContent ? (
                  <p role="status" className="px-1 py-4 text-sm text-text-muted">
                    {t('swarm.pane.connecting')}
                  </p>
                ) : null}
                <ThreadPrimitive.Messages>{() => <WorkerMessage />}</ThreadPrimitive.Messages>
              </ThreadPrimitive.Viewport>
              <ThreadPrimitive.ScrollToBottom
                aria-label={t('swarm.latestActivity')}
                className="absolute bottom-3 right-3 grid size-11 place-items-center rounded-full border border-border bg-surface shadow-md disabled:hidden"
              >
                <ArrowDown className="size-4" aria-hidden="true" />
              </ThreadPrimitive.ScrollToBottom>
            </ThreadPrimitive.Root>
          </ReadonlyThreadProvider>
        </AssistantRuntimeProvider>
      </div>
      {workerStatus !== undefined ? (
        <WorkerControls
          key={`${conversationId}:${childId}`}
          conversationId={conversationId}
          childId={childId}
          status={workerStatus}
        />
      ) : null}
    </section>
  );
}
