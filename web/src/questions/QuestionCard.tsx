import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { cn } from '@/lib/utils';

// QuestionCard is the one frame every question the cockpit asks is drawn in: ask_user's three
// kinds and a mounted MCP server's form (spec 2026-09-25). The markup and classes are ported
// from Tool UI's Question Flow (assistant-ui/tool-ui@49a8702 question-flow.tsx: StepContent,
// ProgressBar; © 2025 AgentbaseAI Inc., MIT, see THIRD_PARTY_NOTICES.md), on Aura's tokens and
// with its copy translated where the component hard-codes English.

export type QuestionVariant = 'default' | 'destructive';

export interface QuestionCardProps {
  readonly titleId: string;
  readonly title: ReactNode;
  readonly description?: ReactNode;
  readonly descriptionId?: string;
  readonly icon?: ReactNode;
  /** Above the step label: the MCP form's server chip, tool name and countdown. */
  readonly header?: ReactNode;
  readonly step?: { readonly current: number; readonly total: number };
  readonly variant?: QuestionVariant;
  readonly footer?: ReactNode;
  readonly status?: ReactNode;
  readonly dataAttributes?: Readonly<Record<`data-${string}`, string>>;
  readonly children?: ReactNode;
}

export function QuestionCard({
  titleId,
  title,
  description,
  descriptionId,
  icon,
  header,
  step,
  variant = 'default',
  footer,
  status,
  dataAttributes,
  children,
}: QuestionCardProps) {
  const { t } = useTranslation();
  const described = description !== undefined && descriptionId !== undefined;
  return (
    <div
      role="form"
      aria-labelledby={titleId}
      {...(described ? { 'aria-describedby': descriptionId } : {})}
      data-slot="card"
      data-variant={variant}
      tabIndex={-1}
      {...dataAttributes}
      className={cn(
        'flex w-full flex-col gap-4 rounded-2xl border bg-surface-2 p-5 text-text shadow-xs',
        'motion-safe:animate-in motion-safe:fade-in motion-safe:duration-300',
        variant === 'destructive' ? 'border-danger/60' : 'border-accent/40',
      )}
    >
      {header}
      {step !== undefined && step.total > 1 ? (
        <div className="flex flex-col gap-2">
          <span className="text-xs font-medium tracking-wide text-text-muted uppercase">
            {t('questionCard.step', { current: step.current, total: step.total })}
          </span>
          <StepBar current={step.current} total={step.total} label={t('questionCard.progress')} />
        </div>
      ) : null}
      <div className="flex flex-col gap-1">
        <div className="flex items-center gap-2">
          {icon}
          <h2 id={titleId} className="text-lg leading-tight font-semibold">
            {title}
          </h2>
        </div>
        {description !== undefined ? (
          <p
            id={descriptionId}
            className="overflow-x-auto text-sm leading-relaxed break-words whitespace-pre-wrap text-text-muted [overflow-wrap:anywhere]"
          >
            {description}
          </p>
        ) : null}
      </div>
      {children}
      {footer !== undefined ? (
        <div className="flex flex-wrap items-center justify-between gap-2 pt-2">{footer}</div>
      ) : null}
      {status}
    </div>
  );
}

interface StepBarProps {
  readonly current: number;
  readonly total: number;
  readonly label: string;
}

function StepBar({ current, total, label }: StepBarProps) {
  return (
    <div
      className="flex h-1.5 gap-1"
      role="progressbar"
      aria-label={label}
      aria-valuenow={current}
      aria-valuemin={1}
      aria-valuemax={total}
    >
      {Array.from({ length: total }, (_, index) => (
        <div key={index} className="relative flex-1 overflow-hidden rounded-full bg-surface">
          <div
            className={cn(
              'absolute inset-0 origin-left rounded-full bg-accent',
              'motion-safe:transition-transform motion-safe:duration-300',
              index < current ? 'scale-x-100' : 'scale-x-0',
            )}
          />
        </div>
      ))}
    </div>
  );
}
