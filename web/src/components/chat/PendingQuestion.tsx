import { useCallback, useMemo, useState } from 'react';
import { useThreadRuntime } from '@assistant-ui/react';
import { Check, KeyRound, Loader2, Send, X } from 'lucide-react';
import { api, ApiError } from '@/api';
import { Markdown } from '@/components/Markdown';
import { useLocale } from '@/hooks/useLocale';
import { cn } from '@/lib/utils';
import type { ChatReply } from '@/types/api';
import {
  buildAnswerRequest,
  type PendingQuestionPayload,
  requiresTextInput,
} from './PendingQuestion.helpers';

type SubmitState = 'idle' | 'submitting' | 'answered' | 'error';

export function PendingQuestion({
  question,
  threadID,
  onAnswered,
}: {
  question: PendingQuestionPayload;
  threadID: string;
  onAnswered?: () => void;
}) {
  const { t } = useLocale();
  const threadRuntime = useThreadRuntime({ optional: true });
  const [state, setState] = useState<SubmitState>('idle');
  const [text, setText] = useState('');
  const [error, setError] = useState('');
  const [inlineReply, setInlineReply] = useState('');

  const options = useMemo(() => {
    if (question.options.length > 0) return question.options;
    if (question.kind !== 'approval') return [];
    return [
      { label: t('chat.pendingQuestion.approve'), value: 'approve' },
      { label: t('chat.pendingQuestion.deny'), value: 'deny' },
    ];
  }, [question.kind, question.options, t]);

  const showTextInput = requiresTextInput(question.kind, options.length);
  const inputType = question.kind === 'secret_input' ? 'password' : 'text';
  const inputLabel = question.kind === 'secret_input'
    ? t('chat.pendingQuestion.secretLabel')
    : t('chat.pendingQuestion.answerLabel');
  const canSubmitText = text.trim().length > 0 && state !== 'submitting' && state !== 'answered';

  const appendFinalReply = useCallback(
    (reply: ChatReply) => {
      const content = reply.reply?.trim();
      if (!content) return true;
      if (!threadRuntime) return false;
      try {
        threadRuntime.append({
          role: 'assistant',
          content: [{ type: 'text', text: content }],
          startRun: false,
        });
        return true;
      } catch {
        return false;
      }
    },
    [threadRuntime],
  );

  const submitAnswer = useCallback(
    async (answer: string, selectedOptionID?: string) => {
      const trimmed = answer.trim();
      if (!trimmed || state === 'submitting' || state === 'answered') return;
      setState('submitting');
      setError('');
      setInlineReply('');
      try {
        const reply = await api.answerChatQuestion(
          question.id,
          buildAnswerRequest({ answer: trimmed, threadID, selectedOptionID }),
        );
        const appended = appendFinalReply(reply);
        if (!appended && reply.reply?.trim()) {
          setInlineReply(reply.reply.trim());
        } else {
          onAnswered?.();
        }
        setState('answered');
      } catch (err) {
        const message = err instanceof ApiError ? err.message : t('chat.pendingQuestion.submitError');
        setError(message);
        setState('error');
      }
    },
    [appendFinalReply, onAnswered, question.id, state, t, threadID],
  );

  return (
    <section
      className="mt-2 flex min-w-0 flex-col gap-3 rounded-md border bg-muted/30 p-3"
      data-testid="pending-question-card"
      data-kind={question.kind}
    >
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="text-xs font-medium uppercase text-muted-foreground">
            {kindLabel(question.kind, t)}
          </p>
          <p className="mt-1 whitespace-pre-wrap text-sm font-medium">{question.question}</p>
        </div>
        {state === 'submitting' && (
          <Loader2
            className="mt-0.5 size-4 shrink-0 animate-spin text-muted-foreground"
            aria-label={t('chat.pendingQuestion.waiting')}
          />
        )}
      </div>

      {options.length > 0 && (
        <div className="flex flex-wrap gap-2">
          {options.map((option) => (
            <button
              key={option.value}
              type="button"
              disabled={state === 'submitting' || state === 'answered'}
              onClick={() => void submitAnswer(option.value, option.value)}
              className={cn(
                'inline-flex min-h-9 items-center gap-2 rounded-md border bg-background px-3 py-1.5 text-sm font-medium hover:bg-accent disabled:cursor-not-allowed disabled:opacity-60',
                approvalTone(option.value),
              )}
              data-testid="pending-question-option"
            >
              {optionIcon(option.value)}
              <span>{option.label}</span>
            </button>
          ))}
        </div>
      )}

      {showTextInput && (
        <form
          className="flex flex-col gap-2"
          onSubmit={(event) => {
            event.preventDefault();
            void submitAnswer(text);
          }}
        >
          {question.kind === 'secret_input' ? (
            <div className="flex items-center gap-2 rounded-md border bg-background px-3">
              <KeyRound className="size-4 shrink-0 text-muted-foreground" />
              <input
                value={text}
                onChange={(event) => setText(event.target.value)}
                type={inputType}
                autoComplete="off"
                aria-label={inputLabel}
                disabled={state === 'submitting' || state === 'answered'}
                className="h-10 min-w-0 flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground"
                data-testid="pending-question-secret"
              />
            </div>
          ) : (
            <textarea
              value={text}
              onChange={(event) => setText(event.target.value)}
              rows={3}
              aria-label={inputLabel}
              placeholder={t('chat.pendingQuestion.answerPlaceholder')}
              disabled={state === 'submitting' || state === 'answered'}
              className="min-h-20 resize-y rounded-md border bg-background px-3 py-2 text-sm outline-none placeholder:text-muted-foreground disabled:opacity-60"
              data-testid="pending-question-answer"
            />
          )}
          <button
            type="submit"
            disabled={!canSubmitText}
            className="inline-flex h-9 w-fit items-center gap-2 rounded-md bg-primary px-3 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:cursor-not-allowed disabled:opacity-50"
            data-testid="pending-question-submit"
          >
            {state === 'submitting' ? <Loader2 className="size-4 animate-spin" /> : <Send className="size-4" />}
            {state === 'submitting' ? t('chat.pendingQuestion.waiting') : t('chat.pendingQuestion.send')}
          </button>
        </form>
      )}

      {state === 'answered' && (
        <p className="text-xs text-muted-foreground" data-testid="pending-question-answered">
          {t('chat.pendingQuestion.answered')}
        </p>
      )}
      {error && (
        <p className="text-sm text-destructive" data-testid="pending-question-error">
          {error}
        </p>
      )}
      {inlineReply && (
        <div className="border-t pt-3" data-testid="pending-question-final">
          <Markdown content={inlineReply} />
        </div>
      )}
    </section>
  );
}

function kindLabel(kind: string, t: ReturnType<typeof useLocale>['t']): string {
  switch (kind) {
    case 'approval':
      return t('chat.pendingQuestion.kind.approval');
    case 'selection':
      return t('chat.pendingQuestion.kind.selection');
    case 'secret_input':
      return t('chat.pendingQuestion.kind.secretInput');
    default:
      return t('chat.pendingQuestion.kind.clarification');
  }
}

function optionIcon(value: string) {
  const normalized = value.toLowerCase();
  if (normalized.includes('deny') || normalized === 'no' || normalized === 'reject') {
    return <X className="size-4" />;
  }
  if (normalized.includes('approve') || normalized === 'yes' || normalized === 'ok') {
    return <Check className="size-4" />;
  }
  return null;
}

function approvalTone(value: string): string {
  const normalized = value.toLowerCase();
  if (normalized.includes('deny') || normalized === 'no' || normalized === 'reject') {
    return 'text-destructive hover:text-destructive';
  }
  if (normalized.includes('approve') || normalized === 'yes' || normalized === 'ok') {
    return 'text-emerald-700 dark:text-emerald-300';
  }
  return '';
}
