import type { ToolCallMessagePartProps } from '@assistant-ui/react';
import { CheckCircle2, ChevronDown, CircleAlert, LoaderCircle, Wrench } from 'lucide-react';
import { useLocale } from '@/hooks/useLocale';
import { cn } from '@/lib/utils';
import {
  redactArgsForDisplay,
  resultPreview,
  safeJSONStringify,
  summarizeArgKeys,
  toolStatusTone,
} from './ToolCallComponent.helpers';

export function ToolCallComponent({
  toolName,
  args,
  argsText,
  result,
  status,
}: ToolCallMessagePartProps) {
  const { t } = useLocale();
  const statusType = status?.type;
  const reason = status?.type === 'incomplete' ? status.reason : undefined;
  const tone = toolStatusTone(statusType, reason);
  const argSummary = summarizeArgKeys(args, argsText);
  const redactedArgs = redactArgsForDisplay(args, argsText);
  const hasResult = result !== undefined;

  return (
    <div
      data-testid="tool-call-card"
      data-tool-name={toolName}
      data-status={tone}
      className="my-2 overflow-hidden rounded-md border bg-muted/25 text-sm"
    >
      <div className="flex min-w-0 items-center gap-3 px-3 py-2">
        <StatusIcon tone={tone} />
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            <span className="font-medium">{toolName}</span>
            <span className={cn('rounded-md px-1.5 py-0.5 text-xs', statusClassName(tone))}>
              {statusLabel(t, tone)}
            </span>
          </div>
          <p className="mt-1 truncate text-xs text-muted-foreground">
            {argSummary
              ? t('chat.toolCall.argsSummary', { keys: argSummary })
              : t('chat.toolCall.argsUnavailable')}
          </p>
        </div>
      </div>

      <details className="group border-t border-dashed">
        <summary className="flex cursor-pointer list-none items-center gap-2 px-3 py-2 text-xs font-medium text-muted-foreground [&::-webkit-details-marker]:hidden">
          <ChevronDown className="size-3.5 transition-transform group-open:rotate-180" />
          {t('chat.toolCall.details')}
        </summary>
        <div className="space-y-3 px-3 pb-3">
          <ToolBlock
            label={t('chat.toolCall.args')}
            empty={t('chat.toolCall.noArgKeys')}
            value={Object.keys(redactedArgs).length > 0 ? safeJSONStringify(redactedArgs) : ''}
          />
          <ToolBlock
            label={t('chat.toolCall.result')}
            empty={hasResult ? '' : t('chat.toolCall.noResult')}
            value={hasResult ? safeJSONStringify(result) : ''}
          />
          {hasResult && (
            <p className="text-xs text-muted-foreground">
              {t('chat.toolCall.resultPreview', { preview: resultPreview(result) })}
            </p>
          )}
        </div>
      </details>
    </div>
  );
}

function ToolBlock({ label, value, empty }: { label: string; value: string; empty: string }) {
  return (
    <div>
      <p className="mb-1 text-xs font-semibold text-muted-foreground">{label}</p>
      {value ? (
        <pre className="max-h-56 overflow-auto rounded-md bg-background p-2 text-xs leading-5">
          {value}
        </pre>
      ) : (
        <p className="text-xs text-muted-foreground">{empty}</p>
      )}
    </div>
  );
}

function StatusIcon({ tone }: { tone: ReturnType<typeof toolStatusTone> }) {
  if (tone === 'running') return <LoaderCircle className="size-4 shrink-0 animate-spin text-sky-600" />;
  if (tone === 'error') return <CircleAlert className="size-4 shrink-0 text-destructive" />;
  if (tone === 'action') return <Wrench className="size-4 shrink-0 text-amber-600" />;
  return <CheckCircle2 className="size-4 shrink-0 text-emerald-600" />;
}

function statusClassName(tone: ReturnType<typeof toolStatusTone>): string {
  switch (tone) {
    case 'running':
      return 'bg-sky-500/10 text-sky-700';
    case 'error':
      return 'bg-destructive/10 text-destructive';
    case 'action':
      return 'bg-amber-500/10 text-amber-700';
    default:
      return 'bg-emerald-500/10 text-emerald-700';
  }
}

function statusLabel(t: ReturnType<typeof useLocale>['t'], tone: ReturnType<typeof toolStatusTone>): string {
  switch (tone) {
    case 'running':
      return t('chat.toolCall.status.running');
    case 'error':
      return t('chat.toolCall.status.error');
    case 'action':
      return t('chat.toolCall.status.action');
    default:
      return t('chat.toolCall.status.done');
  }
}
