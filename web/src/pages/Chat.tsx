import { useCallback, useMemo, useState } from 'react';
import {
  AssistantRuntimeProvider,
  AttachmentPrimitive,
  ComposerPrimitive,
  MessagePrimitive,
  MessagePartPrimitive,
  ThreadListItemPrimitive,
  ThreadListPrimitive,
  ThreadPrimitive,
  WebSpeechDictationAdapter,
  useAuiState,
  useMessagePartText,
} from '@assistant-ui/react';
import { ArrowUp, Bot, FileText, MessageCircle, Mic, Paperclip, Plus, X } from 'lucide-react';
import { api } from '@/api';
import { normalizeAudioPlaybackPayload } from '@/components/chat/AudioPlayback.helpers';
import { Markdown } from '@/components/Markdown';
import { AuraToolUIRegistrations } from '@/components/chat/AuraToolUIRegistrations';
import { PendingQuestion } from '@/components/chat/PendingQuestion';
import {
  isPendingQuestionPart,
  normalizePendingQuestionPayload,
  type PendingQuestionPayload,
} from '@/components/chat/PendingQuestion.helpers';
import {
  createSourceAttachmentAdapter,
  createSourceAttachmentRunState,
} from '@/components/chat/SourceAttachmentAdapter';
import { ToolCallComponent } from '@/components/chat/ToolCallComponent';
import { useLocale } from '@/hooks/useLocale';
import { useAuraAssistantRuntime, useAuraChatThreadID } from '@/lib/assistant-runtime';
import { cn } from '@/lib/utils';

export default function ChatPage() {
  const { t } = useLocale();
  const [threadID, startNewThread] = useAuraChatThreadID();
  const [pendingQuestionState, setPendingQuestionState] = useState<{
    threadID: string;
    question: PendingQuestionPayload;
  } | null>(null);
  const [audioState, setAudioState] = useState<{ threadID: string; audioURL: string } | null>(null);
  const attachmentRunState = useMemo(() => createSourceAttachmentRunState(), []);
  const sourceAttachmentAdapter = useMemo(
    () => createSourceAttachmentAdapter({
      upload: api.uploadSource,
      onSend: attachmentRunState.track,
    }),
    [attachmentRunState],
  );
  const dictationAdapter = useMemo(() => (
    WebSpeechDictationAdapter.isSupported()
      ? new WebSpeechDictationAdapter({ language: navigator.language || 'it-IT' })
      : undefined
  ), []);
  const clearPendingAttachmentRefs = useCallback(() => attachmentRunState.clear(), [attachmentRunState]);
  const extraBody = useCallback(() => attachmentRunState.extraBody(), [attachmentRunState]);
  const handleStreamData = useCallback((data: { name: string; data: unknown }) => {
    const audio = normalizeAudioPlaybackPayload(data);
    if (audio) setAudioState({ threadID, audioURL: audio.audioURL });
    const question = normalizePendingQuestionPayload({ name: data.name, data: data.data });
    if (question) setPendingQuestionState({ threadID, question });
  }, [threadID]);
  const runtime = useAuraAssistantRuntime(threadID, handleStreamData, {
    adapters: {
      attachments: sourceAttachmentAdapter,
      dictation: dictationAdapter,
    },
    extraBody,
    onFinish: clearPendingAttachmentRefs,
    onError: clearPendingAttachmentRefs,
    onCancel: clearPendingAttachmentRefs,
  });
  const pendingQuestion = pendingQuestionState?.threadID === threadID
    ? pendingQuestionState.question
    : null;
  const audioURL = audioState?.threadID === threadID ? audioState.audioURL : '';

  return (
    <AssistantRuntimeProvider runtime={runtime}>
      <AuraToolUIRegistrations />
      <div className="flex h-full min-h-0 bg-background">
        <ThreadRail onNewThread={startNewThread} />
        <section className="flex min-w-0 flex-1 flex-col">
          <header className="flex h-14 shrink-0 items-center justify-between border-b px-4">
            <div className="min-w-0">
              <h1 className="text-base font-semibold">{t('chat.title')}</h1>
              <p className="truncate text-xs text-muted-foreground">{threadID}</p>
            </div>
          </header>
          <Thread
            t={t}
            threadID={threadID}
            pendingQuestion={pendingQuestion}
            audioURL={audioURL}
            onAudioURL={(nextAudioURL) => setAudioState({ threadID, audioURL: nextAudioURL })}
            onQuestionAnswered={() => setPendingQuestionState(null)}
          />
        </section>
      </div>
    </AssistantRuntimeProvider>
  );
}

function ThreadRail({ onNewThread }: { onNewThread: () => string }) {
  const { t } = useLocale();
  return (
    <aside className="hidden w-64 shrink-0 border-r bg-sidebar/65 p-2 md:flex md:flex-col">
      <ThreadListPrimitive.Root className="flex min-h-0 flex-1 flex-col gap-1">
        <ThreadListPrimitive.New asChild>
          <button
            type="button"
            onClick={() => onNewThread()}
            className="flex h-9 items-center gap-2 rounded-md border px-3 text-sm font-medium hover:bg-accent"
          >
            <Plus className="size-4" />
            {t('chat.newThread')}
          </button>
        </ThreadListPrimitive.New>
        <div className="mt-2 min-h-0 flex-1 overflow-y-auto">
          <ThreadListPrimitive.Items>
            {({ threadListItem }) => (
              <ThreadListItemPrimitive.Root
                className={cn(
                  'group flex min-h-9 items-center rounded-md text-sm text-muted-foreground hover:bg-accent hover:text-foreground',
                  threadListItem.status === 'regular' && 'text-foreground',
                )}
              >
                <ThreadListItemPrimitive.Trigger className="flex min-w-0 flex-1 items-center gap-2 px-3 py-2 text-left">
                  <MessageCircle className="size-4 shrink-0" />
                  <span className="truncate">
                    <ThreadListItemPrimitive.Title fallback={t('chat.currentThread')} />
                  </span>
                </ThreadListItemPrimitive.Trigger>
              </ThreadListItemPrimitive.Root>
            )}
          </ThreadListPrimitive.Items>
        </div>
      </ThreadListPrimitive.Root>
    </aside>
  );
}

function Thread({
  t,
  threadID,
  pendingQuestion,
  audioURL,
  onAudioURL,
  onQuestionAnswered,
}: {
  t: ReturnType<typeof useLocale>['t'];
  threadID: string;
  pendingQuestion: PendingQuestionPayload | null;
  audioURL: string;
  onAudioURL: (audioURL: string) => void;
  onQuestionAnswered: () => void;
}) {
  return (
    <ThreadPrimitive.Root className="flex min-h-0 flex-1 flex-col">
      <ThreadPrimitive.Viewport className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto scroll-smooth px-4 py-6">
        <ThreadPrimitive.Empty>
          <div className="mx-auto flex min-h-[45vh] max-w-xl flex-col items-center justify-center gap-3 text-center">
            <div className="flex size-10 items-center justify-center rounded-full bg-primary/10 text-primary">
              <Bot className="size-5" />
            </div>
            <div>
              <p className="text-sm font-medium">{t('chat.emptyTitle')}</p>
              <p className="mt-1 text-sm text-muted-foreground">{t('chat.emptySubtitle')}</p>
            </div>
          </div>
        </ThreadPrimitive.Empty>

        <div className="mx-auto flex w-full max-w-3xl flex-col gap-4">
          <ThreadPrimitive.Messages
            components={{
              UserMessage,
              AssistantMessage: () => <AssistantMessage threadID={threadID} onAudioURL={onAudioURL} />,
            }}
          />
        </div>

        {pendingQuestion && (
          <div className="mx-auto w-full max-w-3xl">
            <PendingQuestion
              question={pendingQuestion}
              threadID={threadID}
              onAudioURL={onAudioURL}
              onAnswered={onQuestionAnswered}
            />
          </div>
        )}

        {audioURL && (
          <div className="mx-auto w-full max-w-3xl">
            <AssistantAudioPlayer audioURL={audioURL} />
          </div>
        )}

        <ThreadPrimitive.ViewportFooter className="sticky bottom-0 mt-auto bg-background/95 pt-2 backdrop-blur">
          <Composer
            placeholder={t('chat.composerPlaceholder')}
            sendLabel={t('chat.send')}
            attachLabel={t('chat.attach')}
            dictateLabel={t('chat.dictate')}
            removeAttachmentLabel={t('chat.removeAttachment')}
          />
        </ThreadPrimitive.ViewportFooter>
      </ThreadPrimitive.Viewport>
    </ThreadPrimitive.Root>
  );
}

function Composer({
  placeholder,
  sendLabel,
  attachLabel,
  dictateLabel,
  removeAttachmentLabel,
}: {
  placeholder: string;
  sendLabel: string;
  attachLabel: string;
  dictateLabel: string;
  removeAttachmentLabel: string;
}) {
  return (
    <ComposerPrimitive.Root className="mx-auto w-full max-w-3xl">
      <ComposerPrimitive.AttachmentDropzone asChild>
        <div className="flex flex-col gap-2 rounded-md border bg-card px-3 py-2 shadow-sm transition-colors data-[dragging=true]:border-dashed data-[dragging=true]:border-primary data-[dragging=true]:bg-primary/5">
          <ComposerAttachmentChips removeLabel={removeAttachmentLabel} />
          <div className="flex items-end gap-2">
            <ComposerPrimitive.Input
              rows={1}
              placeholder={placeholder}
              aria-label={placeholder}
              className="min-h-10 flex-1 resize-none bg-transparent py-2 text-sm leading-6 outline-none placeholder:text-muted-foreground"
            />
            <ComposerPrimitive.AddAttachment asChild>
              <button
                type="button"
                aria-label={attachLabel}
                title={attachLabel}
                className="flex size-9 shrink-0 items-center justify-center rounded-md border text-muted-foreground transition-colors hover:bg-accent hover:text-foreground disabled:opacity-40"
                data-testid="chat-attach"
              >
                <Paperclip className="size-4" />
              </button>
            </ComposerPrimitive.AddAttachment>
            <DictationButton label={dictateLabel} />
            <ComposerPrimitive.Send asChild>
              <button
                type="button"
                aria-label={sendLabel}
                title={sendLabel}
                className="flex size-9 shrink-0 items-center justify-center rounded-md bg-primary text-primary-foreground transition-opacity disabled:opacity-40"
              >
                <ArrowUp className="size-4" />
              </button>
            </ComposerPrimitive.Send>
          </div>
        </div>
      </ComposerPrimitive.AttachmentDropzone>
    </ComposerPrimitive.Root>
  );
}

function ComposerAttachmentChips({ removeLabel }: { removeLabel: string }) {
  return (
    <div className="flex flex-wrap gap-2 empty:hidden">
      <ComposerPrimitive.Attachments>
        {() => <SourceAttachmentChip removeLabel={removeLabel} />}
      </ComposerPrimitive.Attachments>
    </div>
  );
}

function SourceAttachmentChip({ removeLabel }: { removeLabel: string }) {
  return (
    <AttachmentPrimitive.Root
      className="inline-flex min-w-0 max-w-full items-center gap-2 rounded-md border bg-background px-2 py-1 text-xs"
      data-testid="source-attachment-chip"
    >
      <FileText className="size-4 shrink-0 text-muted-foreground" />
      <AttachmentPrimitive.Name className="min-w-0 truncate" />
      <AttachmentPrimitive.Remove asChild>
        <button
          type="button"
          aria-label={removeLabel}
          title={removeLabel}
          className="flex size-5 shrink-0 items-center justify-center rounded-sm text-muted-foreground hover:bg-accent hover:text-foreground"
        >
          <X className="size-3" />
        </button>
      </AttachmentPrimitive.Remove>
    </AttachmentPrimitive.Root>
  );
}

function DictationButton({ label }: { label: string }) {
  const active = useAuiState((state) => state.composer.dictation?.status.type === 'starting'
    || state.composer.dictation?.status.type === 'running');
  return (
    <ComposerPrimitive.Dictate asChild>
      <button
        type="button"
        aria-label={label}
        title={label}
        className={cn(
          'flex size-9 shrink-0 items-center justify-center rounded-md border text-muted-foreground transition-colors hover:bg-accent hover:text-foreground disabled:opacity-40',
          active && 'border-primary bg-primary/10 text-primary',
        )}
        data-testid="chat-dictate"
      >
        <Mic className="size-4" />
      </button>
    </ComposerPrimitive.Dictate>
  );
}

function AssistantAudioPlayer({ audioURL }: { audioURL: string }) {
  return (
    <div className="rounded-md border bg-card px-3 py-2">
      <audio controls src={audioURL} className="w-full" data-testid="assistant-audio" />
    </div>
  );
}

function UserMessage() {
  return (
    <MessagePrimitive.Root className="flex justify-end">
      <div className="max-w-[80%] rounded-md bg-primary px-4 py-2.5 text-sm text-primary-foreground">
        <MessagePrimitive.Parts components={{ Text: UserText }} />
      </div>
    </MessagePrimitive.Root>
  );
}

function AssistantMessage({ threadID, onAudioURL }: { threadID: string; onAudioURL: (audioURL: string) => void }) {
  const { t } = useLocale();
  return (
    <MessagePrimitive.Root className="flex items-start gap-3">
      <div className="mt-1 flex size-7 shrink-0 items-center justify-center rounded-md bg-primary/10 text-primary">
        <Bot className="size-4" />
      </div>
      <div className="min-w-0 max-w-[80%] rounded-md border bg-card px-4 py-2.5 text-sm">
        <MessagePrimitive.Parts>
          {({ part }) => {
            if (part.type === 'text') return <AssistantText />;
            if (part.type === 'tool-call') return part.toolUI ?? <ToolCallComponent {...part} />;
            if (isPendingQuestionPart(part)) {
              const pendingQuestion = normalizePendingQuestionPayload(part);
              if (pendingQuestion) {
                return <PendingQuestion question={pendingQuestion} threadID={threadID} onAudioURL={onAudioURL} />;
              }
            }
            const audio = normalizeAudioPlaybackPayload(part);
            if (audio) return <AssistantAudioPlayer audioURL={audio.audioURL} />;
            return null;
          }}
        </MessagePrimitive.Parts>
        <MessagePrimitive.Error>
          <div className="mt-2 text-destructive">{t('chat.error')}</div>
        </MessagePrimitive.Error>
      </div>
    </MessagePrimitive.Root>
  );
}

function UserText() {
  return (
    <p className="whitespace-pre-wrap">
      <MessagePartPrimitive.Text />
    </p>
  );
}

function AssistantText() {
  const part = useMessagePartText();
  return <Markdown content={part.text} />;
}
