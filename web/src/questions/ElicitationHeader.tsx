import { useTranslation } from 'react-i18next';
import { Clock3, Server } from 'lucide-react';
import type { ElicitationQuestion } from '../chat/sseAdapter_elicitation';
import { formatRemaining, useCountdown } from './useCountdown';
import { Badge } from '@/components/ui/badge';

// ElicitationHeader says who is asking. The chip carries the name Aura mounted the server
// under, never one the server gave itself. The countdown is Aura's own bound: a server's
// request timeout can end the form sooner, and the card then shows it cancelled.

export interface ElicitationHeaderProps {
  readonly question: ElicitationQuestion;
  readonly countdown: boolean;
}

export function ElicitationHeader({ question, countdown }: ElicitationHeaderProps) {
  const { t } = useTranslation();
  return (
    <div className="flex flex-wrap items-center gap-2 text-xs text-text-muted">
      <Badge variant="secondary" className="gap-1">
        <Server aria-hidden="true" className="size-3.5" />
        <span className="sr-only">
          {t('questionCard.form.server', { server: question.server })}
        </span>
        <span aria-hidden="true">{question.server}</span>
      </Badge>
      {question.tool !== undefined ? <span className="font-mono">{question.tool}</span> : null}
      {countdown ? <Countdown deadline={question.deadline} /> : null}
    </div>
  );
}

function Countdown({ deadline }: { readonly deadline: string }) {
  const { t } = useTranslation();
  const time = formatRemaining(useCountdown(deadline));
  return (
    <span
      role="timer"
      aria-label={t('questionCard.form.expiresIn', { time })}
      className="ms-auto inline-flex items-center gap-1 tabular-nums"
    >
      <Clock3 aria-hidden="true" className="size-3.5" />
      {time}
    </span>
  );
}
