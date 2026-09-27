import { Check } from 'lucide-react';
import { Badge } from '@/components/ui/badge';

// QuestionReceipt is what a question becomes once it closes: Question Flow's read-only receipt
// (assistant-ui/tool-ui@49a8702 question-flow.tsx: QuestionFlowReceipt; © 2025 AgentbaseAI
// Inc., MIT, see THIRD_PARTY_NOTICES.md) as a chip in the outcome's tone, and under an answer
// the value given. Nothing here is interactive.

export type ReceiptTone = 'success' | 'neutral' | 'warning' | 'danger';

const CHIP_TEXT: Record<ReceiptTone, string> = {
  success: 'text-success',
  neutral: 'text-text-muted',
  warning: 'text-warning',
  danger: 'text-danger',
};
const CHIP_DOT: Record<ReceiptTone, string> = {
  success: 'bg-success',
  neutral: 'bg-text-muted',
  warning: 'bg-warning',
  danger: 'bg-danger',
};

export interface ReceiptLine {
  readonly label: string;
  readonly value: string;
}

export interface QuestionReceiptProps {
  readonly tone: ReceiptTone;
  readonly label: string;
  readonly summary?: readonly ReceiptLine[];
  readonly announce?: boolean;
}

export function QuestionReceipt({
  tone,
  label,
  summary = [],
  announce = false,
}: QuestionReceiptProps) {
  return (
    <div className="flex flex-col gap-3">
      <Badge
        variant={tone === 'neutral' ? 'secondary' : tone}
        data-tone={tone}
        {...(announce ? { role: 'status', 'aria-live': 'polite' as const } : {})}
        className={`self-start text-[0.8125rem] ${CHIP_TEXT[tone]}`}
      >
        {tone === 'success' ? (
          <Check aria-hidden="true" className="size-3.5" />
        ) : (
          <span
            aria-hidden="true"
            className={`inline-block h-2 w-2 shrink-0 rounded-sm ${CHIP_DOT[tone]}`}
          />
        )}
        {label}
      </Badge>
      {summary.length > 0 ? (
        <dl className="flex flex-col gap-2 text-sm">
          {summary.map((line) => (
            <div
              key={line.label}
              className="flex flex-col gap-0.5 motion-safe:animate-in motion-safe:fade-in"
            >
              <dt className="text-text-muted">{line.label}</dt>
              <dd className="font-medium break-words whitespace-pre-wrap [overflow-wrap:anywhere]">
                {line.value}
              </dd>
            </div>
          ))}
        </dl>
      ) : null}
    </div>
  );
}
