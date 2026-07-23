import { useId, useState } from 'react';
import { ChevronDown } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import type { DisplayPayload } from './displays/types';
import { formatElapsed } from './durationFormat';
import { ToolActivityCard } from './ToolActivityCard';
import { Button } from '@/components/ui/button';

// ToolGroup (compact-chat spec §3.3): one 44px header for a run of ≥3
// consecutive settled tool parts — deep-research turns fire 5-15 sequential
// searches; the stack turns N rows into one. Rollup dot (danger when any
// member errored), "N tools" count, wall-clock span (first member startedAt →
// last member finishedAt), chevron. Expanded: the member rows stack (each
// still individually expandable — two-level disclosure). Collapse state lives
// in this group-leader component (ephemeral, never persisted).

export interface ToolGroupMember {
  readonly toolCallId: string;
  readonly toolName: string;
  readonly argsText?: string | undefined;
  readonly result?: string | undefined;
  readonly isError?: boolean | undefined;
  readonly startedAt?: number | undefined;
  readonly finishedAt?: number | undefined;
  readonly display?: DisplayPayload | undefined;
}

export interface ToolGroupProps {
  readonly members: readonly ToolGroupMember[];
  /** Citation click-through forwarded to each member's expanded display. */
  readonly onOpenSource?: ((display: DisplayPayload, refId: string) => void) | undefined;
}

export function ToolGroup({ members, onOpenSource }: ToolGroupProps) {
  const { t, i18n } = useTranslation();
  const bodyId = useId();
  const [expanded, setExpanded] = useState(false);

  const anyError = members.some((m) => m.isError === true);
  const startedAt = members[0]?.startedAt;
  const finishedAt = members[members.length - 1]?.finishedAt;
  const span =
    startedAt !== undefined && finishedAt !== undefined && finishedAt > startedAt
      ? formatElapsed(finishedAt - startedAt, i18n.resolvedLanguage ?? i18n.language, t)
      : null;

  return (
    <div
      data-testid="tool-group"
      className={`rounded-[var(--radius-md)] border ${anyError ? 'border-danger/40' : 'border-border'}`}
    >
      <Button
        type="button"
        variant="ghost"
        onClick={() => {
          setExpanded((v) => !v);
        }}
        aria-expanded={expanded}
        aria-controls={bodyId}
        aria-label={t('chat.tool.groupAria')}
        className="min-h-11 w-full justify-between gap-2 px-3 text-xs"
      >
        <span className="flex min-w-0 items-center gap-2">
          <span
            aria-hidden="true"
            className={`inline-block h-2 w-2 shrink-0 rounded-sm ${anyError ? 'bg-danger' : 'bg-success'}`}
          />
          <span className="text-xs text-text">
            {t('chat.tool.group', { count: members.length })}
          </span>
        </span>
        <span className="flex shrink-0 items-center gap-2">
          {span !== null ? (
            <span
              data-testid="tool-group-elapsed"
              className="font-mono text-[0.75rem] tabular-nums text-text-faint"
            >
              {span}
            </span>
          ) : null}
          <ChevronDown
            data-icon
            aria-hidden="true"
            className={`transition-transform motion-reduce:transition-none ${expanded ? 'rotate-180' : ''}`}
          />
        </span>
      </Button>
      <div
        id={bodyId}
        hidden={!expanded}
        data-testid="tool-group-body"
        className="aura-part-reveal space-y-1 border-t border-border p-2"
      >
        {members.map((member) => (
          <ToolActivityCard
            key={member.toolCallId}
            toolName={member.toolName}
            {...(member.argsText !== undefined ? { argsText: member.argsText } : {})}
            {...(member.result !== undefined ? { result: member.result } : {})}
            {...(member.isError !== undefined ? { isError: member.isError } : {})}
            {...(member.startedAt !== undefined ? { startedAt: member.startedAt } : {})}
            {...(member.finishedAt !== undefined ? { finishedAt: member.finishedAt } : {})}
            {...(member.display !== undefined
              ? {
                  display: member.display,
                  ...(onOpenSource !== undefined
                    ? {
                        onOpenSource: (refId: string) => {
                          if (member.display !== undefined) onOpenSource(member.display, refId);
                        },
                      }
                    : {}),
                }
              : {})}
          />
        ))}
      </div>
    </div>
  );
}
