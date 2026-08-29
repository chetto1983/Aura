import { useEffect, useMemo, useState } from 'react';
import { X } from 'lucide-react';
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
import { ToolFallback } from '../ExternalStoreChat_messages';
import { MarkdownText } from '../MarkdownText';
import { openWorkerStream } from './workerStream';

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
  const streamKey = `${conversationId}\u0000${childId}`;
  const [streamState, setStreamState] = useState<{
    readonly key: string;
    readonly messages: readonly ThreadMessageLike[];
    readonly failed: boolean;
  }>({ key: streamKey, messages: [], failed: false });
  const current =
    streamState.key === streamKey ? streamState : { key: streamKey, messages: [], failed: false };

  useEffect(() => {
    const stream = openWorkerStream(conversationId, childId, {
      onMessages: (messages) => {
        setStreamState({ key: streamKey, messages, failed: false });
      },
      onError: () => {
        setStreamState((previous) => ({
          key: streamKey,
          messages: previous.key === streamKey ? previous.messages : [],
          failed: true,
        }));
      },
    });
    return stream.close;
  }, [childId, conversationId, streamKey]);

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

      <div className="min-h-0 flex-1 overflow-y-auto">
        {current.failed ? (
          <p role="alert" className="px-1 py-4 text-sm text-danger">
            {t('swarm.pane.error')}
          </p>
        ) : hasContent ? (
          <AssistantRuntimeProvider runtime={scopeRuntime}>
            <ReadonlyThreadProvider messages={readonlyMessages}>
              <ThreadPrimitive.Root className="min-h-0">
                <ThreadPrimitive.Viewport className="space-y-3 py-1">
                  <ThreadPrimitive.Messages>{() => <WorkerMessage />}</ThreadPrimitive.Messages>
                </ThreadPrimitive.Viewport>
              </ThreadPrimitive.Root>
            </ReadonlyThreadProvider>
          </AssistantRuntimeProvider>
        ) : (
          <p role="status" className="px-1 py-4 text-sm text-text-muted">
            {t('swarm.pane.connecting')}
          </p>
        )}
      </div>
    </section>
  );
}
