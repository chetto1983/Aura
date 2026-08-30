import { Square, Volume2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import {
  ActionBarPrimitive,
  AuiIf,
  ComposerPrimitive,
  MessagePrimitive,
  useAuiState,
  type ThreadMessageLike,
  type ToolCallMessagePartComponent,
} from '@assistant-ui/react';
import { useVoiceMode } from './voice/voiceModeContext';
import { AttachmentCard } from './attachments/AttachmentCard';
import type { Asset } from './attachments/types';
import { BranchPicker } from './BranchPicker';
import { messageBudgetLimit } from './budgetLimit';
import { BudgetLimitNotice } from './BudgetLimitNotice';
import { DisplayRouter } from './displays/DisplayRouter';
import { aggregateAnswerSources } from './displays/answerSources';
import { useSourceExplorer } from './displays/sourceExplorerControls';
import { SourcesButton } from './displays/SourcesButton';
import { isDisplayPayload, type DisplayPayload } from './displays/types';
import { hasAnswerText } from './ExternalStoreChat_folds';
import { MarkdownText } from './MarkdownText';
import { ReasoningPill } from './ReasoningPill';
import { ToolActivityCard } from './ToolActivityCard';
import { ToolGroup, type ToolGroupMember } from './ToolGroup';
import { toolRun } from './toolGrouping';

// ExternalStoreChat_messages — the presentational message-render components split
// out of ExternalStoreChat.tsx (refactor-on-touch, CLAUDE.md 600-LOC cap). These
// are stateless renderers that read message/part state via assistant-ui hooks; they
// hold no runtime/stream state (that stays in ExternalStoreChat). The Phase-26
// typed-display + Source Explorer seams (ToolFallback → DisplayRouter, AnswerSources
// → SourcesButton) live here.

interface UserMessageProps {
  readonly onAssetRetry: (assetID: string) => void;
  readonly onAssetPromote: (assetID: string) => void;
  readonly onAssetRemove: (assetID: string) => void;
}

const MESSAGE_ACTION_CLASS =
  'inline-flex min-h-[44px] min-w-[44px] items-center justify-center text-[0.75rem] text-text-muted outline-none hover:text-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent';
const MESSAGE_ACTION_ROW_CLASS =
  'flex flex-wrap items-center gap-2 opacity-0 transition-opacity focus-within:opacity-100 hover:opacity-100 [@media(pointer:coarse)]:opacity-100';

function messageAttachments(message: ThreadMessageLike): readonly Asset[] {
  const metadata = message.metadata?.custom as { attachments?: readonly Asset[] } | undefined;
  return metadata?.attachments ?? [];
}

export function UserMessage({ onAssetRetry, onAssetPromote, onAssetRemove }: UserMessageProps) {
  const { t } = useTranslation();
  const message = useAuiState((s) => s.message) as ThreadMessageLike;
  const attachments = messageAttachments(message);
  return (
    <MessagePrimitive.Root className="ml-auto flex min-w-0 max-w-[80%] flex-col items-end gap-1">
      {/* Edit mode: a message-scoped composer whose Send fires onEdit (fork + re-run). */}
      <AuiIf condition={({ composer }) => composer.isEditing}>
        <ComposerPrimitive.Root className="flex w-full flex-col gap-2 rounded-[var(--radius-md)] border border-accent/40 bg-surface-2 px-3 py-2">
          <ComposerPrimitive.Input
            aria-label={t('chat.edit.label')}
            className="w-full resize-none bg-transparent text-sm text-text outline-none"
          />
          <div className="flex items-center justify-end gap-2">
            <ComposerPrimitive.Cancel
              aria-label={t('chat.edit.cancel')}
              className="text-[0.75rem] text-text-muted outline-none hover:text-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
            >
              {t('chat.edit.cancel')}
            </ComposerPrimitive.Cancel>
            <ComposerPrimitive.Send className="rounded-[var(--radius-sm)] bg-accent px-2 py-1 text-[0.75rem] font-medium text-on-accent outline-none focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent">
              {t('chat.edit.save')}
            </ComposerPrimitive.Send>
          </div>
        </ComposerPrimitive.Root>
      </AuiIf>

      {/* Normal view: the rendered user turn + the action bar. */}
      <AuiIf condition={({ composer }) => !composer.isEditing}>
        <div className="min-w-0 max-w-full rounded-3xl bg-surface-2 px-5 py-3">
          <MessagePrimitive.Parts
            components={{
              Text: () => (
                <div className="whitespace-pre-wrap [overflow-wrap:anywhere] text-base leading-relaxed text-text">
                  <MarkdownText />
                </div>
              ),
            }}
          />
        </div>
        {/* Edit a user turn → onEdit forks a branch + re-runs. Copy is the minimum verb. */}
        {attachments.length > 0 ? (
          <div className="flex min-w-0 max-w-full flex-col items-end gap-2">
            {attachments.map((asset) => (
              <AttachmentCard
                key={asset.id}
                asset={asset}
                onRetry={onAssetRetry}
                onPromote={onAssetPromote}
                onRemove={onAssetRemove}
              />
            ))}
          </div>
        ) : null}
        <ActionBarPrimitive.Root data-message-actions className={MESSAGE_ACTION_ROW_CLASS}>
          <ActionBarPrimitive.Edit
            aria-label={t('chat.action.edit')}
            data-required-touch-target
            className={MESSAGE_ACTION_CLASS}
          >
            {t('chat.action.edit')}
          </ActionBarPrimitive.Edit>
          <ActionBarPrimitive.Copy
            aria-label={t('chat.action.copy')}
            data-required-touch-target
            className={MESSAGE_ACTION_CLASS}
          >
            {t('chat.action.copy')}
          </ActionBarPrimitive.Copy>
          <BranchPicker />
        </ActionBarPrimitive.Root>
      </AuiIf>
    </MessagePrimitive.Root>
  );
}

export function AssistantMessage() {
  const { t } = useTranslation();
  const message = useAuiState((s) => s.message) as ThreadMessageLike;
  const attachments = messageAttachments(message);
  return (
    <MessagePrimitive.Root data-message-role="assistant" className="w-full min-w-0 space-y-2">
      {/* §4 spacing rhythm: machinery parts within one turn stack with 8px. */}
      <div data-message-content className="w-full min-w-0 space-y-2 overflow-x-auto">
        <MessagePrimitive.Parts
          components={{
            // Assistant prose → sanitized markdown.
            Text: () => (
              <div className="w-full min-w-0 text-base leading-relaxed text-text">
                <MarkdownText constrainProse />
              </div>
            ),
            // CoT → the compact ReasoningPill (compact-chat spec §2.2). The part
            // wiring reads span timestamps + snapshot duration off the stored part.
            Reasoning: ReasoningPillPart,
            // Tool activity → typed display when a trusted backend normalizer
            // produced an aura.display payload (Phase 26, DISP-02); otherwise the
            // raw escaped card (D-02 / D-FALLBACK). The branch lives in ToolFallback.
            tools: {
              Fallback: ToolFallback,
            },
          }}
        />
      </div>
      {/* Amendment #188: a turn the loop budget cut says so under its answer. */}
      <BudgetLimitNotice limit={messageBudgetLimit(message)} />
      {/* D-15: durable authenticated download chip(s) for agent deliverables folded
          onto THIS assistant turn on saved-conversation load. Uses only asset_id →
          GET /api/assets/{id}/download (the 37A-proven auth path); NEVER an
          object_key / host path (T-37B-14). Renders nothing when the turn has none. */}
      {attachments.length > 0 ? (
        <div className="flex w-full min-w-0 flex-col items-start gap-2">
          {attachments.map((asset) => (
            <a
              key={asset.id}
              href={`/api/assets/${encodeURIComponent(asset.id)}/download`}
              download={asset.file_name}
              aria-label={t('display.artifact.downloadAria', { filename: asset.file_name })}
              data-required-touch-target
              className="group inline-flex min-h-[44px] min-w-[44px] max-w-full items-center gap-2 rounded-[var(--radius-sm)] border border-accent/40 bg-surface-2 px-3 py-1.5 text-sm font-medium text-accent-text transition-colors hover:border-accent hover:bg-surface focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
            >
              <svg
                width="15"
                height="15"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
                strokeLinecap="round"
                strokeLinejoin="round"
                aria-hidden="true"
                className="shrink-0 transition-transform group-hover:translate-y-0.5"
              >
                <path d="M12 3v12" />
                <path d="m7 10 5 5 5-5" />
                <path d="M5 21h14" />
              </svg>
              <span
                className="min-w-0 break-words [overflow-wrap:anywhere] font-mono"
                title={asset.file_name}
              >
                {asset.file_name}
              </span>
            </a>
          ))}
        </div>
      ) : null}
      <MessagePrimitive.Error>
        <p role="alert" className="text-sm text-danger">
          {/* The reducer already routes RUN_ERROR into an error text part; this
              is the runtime-level fallback for a hard message error. */}
        </p>
      </MessagePrimitive.Error>
      {/* Assistant action bar: Copy + Reload (regenerate) + the answer-level
          "Sources (N)" affordance (D-13). The feedback rating group is deferred;
          Reload forks an assistant-turn branch. Rendered ONLY on messages that
          carry a real answer (non-empty text part): tool-card-only and
          reasoning-only turns show no Copy/Regenerate/TTS (operator directive —
          the machinery rows are not copyable answers). */}
      {hasAnswerText(message) ? (
        <ActionBarPrimitive.Root data-message-actions className={MESSAGE_ACTION_ROW_CLASS}>
          <ActionBarPrimitive.Copy
            aria-label={t('chat.action.copy')}
            data-required-touch-target
            className={MESSAGE_ACTION_CLASS}
          >
            {t('chat.action.copy')}
          </ActionBarPrimitive.Copy>
          <ActionBarPrimitive.Reload
            aria-label={t('chat.action.reload')}
            data-required-touch-target
            className={MESSAGE_ACTION_CLASS}
          >
            {t('chat.action.reload')}
          </ActionBarPrimitive.Reload>
          <AssistantSpeakerControl />
          <BranchPicker />
        </ActionBarPrimitive.Root>
      ) : null}
      {/* The "Sources (N)" affordance lives OUTSIDE the hover-only action bar so the
          evidence count is always visible (D-13); hidden when N=0. */}
      <AnswerSources />
    </MessagePrimitive.Root>
  );
}

interface SpeakerSpeech {
  readonly status?: unknown;
}

// A truncated TTS response (the X-Aura-TTS-Truncated header, surfaced by the
// speechAdapter on the utterance + its status object) → the D-05 "message too long"
// hint. Reads the flag off the active speech state for THIS message: the stock runtime
// copies utterance.status into s.message.speech by reference, so a truncated flag stamped
// on the status rides through (top-level `truncated` is also honored for a custom bridge).
function speechIsTruncated(speech: SpeakerSpeech | null | undefined): boolean {
  if (speech == null) return false;
  const flagged = speech as { truncated?: unknown; status?: { truncated?: unknown } };
  return flagged.truncated === true || flagged.status?.truncated === true;
}

/**
 * AssistantSpeakerControl — the caps.tts-gated Speak/StopSpeaking pair (D-04) plus the
 * D-05 truncation hint, dropped into the assistant ActionBar next to Copy/Reload. It
 * renders NOTHING when TTS is unconfigured (useVoiceMode().caps.tts, WEBVOICE-03 degrade).
 * Speak shows while s.message.speech is null, StopSpeaking while this message is being
 * spoken; the runtime cancels any prior utterance so at most one message speaks at a time
 * (RESEARCH Landmine #6). The tooLong hint appears only while the active utterance is
 * truncated — this is what makes the X-Aura-TTS-Truncated header a visible element, not
 * dead code.
 */
export function AssistantSpeakerControl() {
  const { t } = useTranslation();
  const { caps } = useVoiceMode();
  // `s.message.speech` is the RESEARCH-endorsed speaker-state seam (Q2). Its upstream
  // "under active development" deprecation note is not a removal, so the suppression is
  // intentional — there is no non-deprecated substitute for the per-message speech state.
  // eslint-disable-next-line @typescript-eslint/no-deprecated
  const speech = useAuiState((s) => s.message.speech);
  if (!caps.tts) return null;
  return (
    <>
      {/* eslint-disable-next-line @typescript-eslint/no-deprecated */}
      <AuiIf condition={(s) => s.message.speech == null}>
        <ActionBarPrimitive.Speak
          aria-label={t('chat.action.speak')}
          data-required-touch-target
          className={MESSAGE_ACTION_CLASS}
        >
          <Volume2 aria-hidden="true" focusable="false" className="size-3.5" />
        </ActionBarPrimitive.Speak>
      </AuiIf>
      {/* eslint-disable-next-line @typescript-eslint/no-deprecated */}
      <AuiIf condition={(s) => s.message.speech != null}>
        <ActionBarPrimitive.StopSpeaking
          aria-label={t('chat.action.stopSpeaking')}
          data-required-touch-target
          className={MESSAGE_ACTION_CLASS}
        >
          <Square aria-hidden="true" focusable="false" className="size-3.5 fill-current" />
        </ActionBarPrimitive.StopSpeaking>
      </AuiIf>
      {speechIsTruncated(speech) ? (
        <span role="note" className="text-[0.75rem] italic text-text-faint">
          {t('chat.action.tooLong')}
        </span>
      ) : null}
    </>
  );
}

/**
 * AnswerSources mounts the "Sources (N)" affordance at the answer level (D-13). It
 * reads THIS assistant message's tool parts, aggregates the per-turn source
 * registries into one deduped list, and opens the SAME shared Source Explorer the
 * citation chips open (one sheet, two entry points, one registry).
 */
function AnswerSources() {
  const { openSources } = useSourceExplorer();
  const content = useAuiState((s) => s.message.content);
  const sources = aggregateAnswerSources(content);
  return (
    <SourcesButton
      sources={sources}
      onOpen={(srcs) => {
        openSources(srcs);
      }}
    />
  );
}

/**
 * ReasoningPillPart adapts the stored reasoning part to the ReasoningPill (spec
 * §5.1). It mirrors ToolFallback's seam: the external-store runtime passes our
 * ThreadMessageLike part through unchanged (convertMessage: identity), so the
 * span timestamps (live) and the snapshot's durationMs decoration survive on
 * `s.part`; the message-level running status drives the streaming state.
 */
function ReasoningPillPart({ text }: { readonly text: string }) {
  const part = useAuiState((s) => s.part) as {
    durationMs?: unknown;
    startedAt?: unknown;
    finishedAt?: unknown;
  };
  const isRunning = useAuiState((s) => s.message.status?.type === 'running');
  const asMs = (v: unknown): number | undefined => (typeof v === 'number' ? v : undefined);
  return (
    <ReasoningPill
      text={text}
      durationMs={asMs(part.durationMs)}
      startedAt={asMs(part.startedAt)}
      finishedAt={asMs(part.finishedAt)}
      isRunning={isRunning}
    />
  );
}

/** Display types that render INLINE with no disclosure row (compact-chat §3.5):
 *  system_event is safety-relevant one-line status; local_artifact is the small
 *  actionable download chip. Everything else lives behind the compact tool row. */
const INLINE_DISPLAY_TYPES = new Set<string>(['system_event', 'local_artifact']);

/**
 * The tools.Fallback render: the single seam where a tool turn becomes UI. It
 * reads the custom `display` payload off the stored message part (the sseAdapter
 * attaches it by toolCallId, live or on replay) via useAuiState — the
 * external-store runtime passes our ThreadMessageLike part through unchanged
 * (convertMessage: identity), so the field survives.
 *
 * Compact-chat (spec §3): every tool turn renders as the collapsed
 * ToolActivityCard row; the typed display (when attached) becomes the row's
 * EXPANDED body via the card's DisplayRouter dispatch. The two inline
 * exceptions (system_event / local_artifact) keep today's row-less markup.
 *
 * Citation click-through (D-04): when the payload carries a source registry, a
 * chip click opens the SHARED Source Explorer (the same sheet the answer-level
 * "Sources (N)" button opens) over THIS turn's registry, focused on the refId.
 */
export const ToolFallback: ToolCallMessagePartComponent = ({
  toolCallId,
  toolName,
  argsText,
  result,
  isError,
}) => {
  const { openSources } = useSourceExplorer();
  const part = useAuiState((s) => s.part) as {
    display?: unknown;
    startedAt?: unknown;
    finishedAt?: unknown;
  };
  const content = useAuiState((s) => s.message.content) as readonly unknown[];
  const display: DisplayPayload | undefined = isDisplayPayload(part.display)
    ? part.display
    : undefined;
  const resultText = typeof result === 'string' ? result : undefined;
  const startedAt = typeof part.startedAt === 'number' ? part.startedAt : undefined;
  const finishedAt = typeof part.finishedAt === 'number' ? part.finishedAt : undefined;
  const onOpenSource = (refId: string) => {
    openSources(display?.sources ?? [], refId);
  };

  if (display !== undefined && INLINE_DISPLAY_TYPES.has(display.type)) {
    return (
      <DisplayRouter
        payload={display}
        toolName={toolName}
        argsText={argsText}
        onOpenSource={onOpenSource}
        {...(resultText !== undefined ? { result: resultText } : {})}
        {...(isError !== undefined ? { isError } : {})}
      />
    );
  }

  // §3.3 grouping: a run of ≥3 consecutive settled tool parts renders ONE
  // ToolGroup — drawn entirely by the run's FIRST member; later members render
  // nothing. The still-running tool is never a member (falls through below).
  const run = toolRun(content, toolCallId);
  if (run !== null) {
    if (run.ids[0] !== toolCallId) return null;
    const members = run.ids.flatMap((id, offset): ToolGroupMember[] => {
      const raw = content[run.startIndex + offset];
      if (typeof raw !== 'object' || raw === null) return [];
      const p = raw as {
        toolName?: unknown;
        argsText?: unknown;
        result?: unknown;
        isError?: unknown;
        startedAt?: unknown;
        finishedAt?: unknown;
        display?: unknown;
      };
      return [
        {
          toolCallId: id,
          toolName: typeof p.toolName === 'string' ? p.toolName : '',
          argsText: typeof p.argsText === 'string' ? p.argsText : undefined,
          result: typeof p.result === 'string' ? p.result : undefined,
          isError: p.isError === true ? true : undefined,
          startedAt: typeof p.startedAt === 'number' ? p.startedAt : undefined,
          finishedAt: typeof p.finishedAt === 'number' ? p.finishedAt : undefined,
          display: isDisplayPayload(p.display) ? p.display : undefined,
        },
      ];
    });
    return (
      <ToolGroup
        members={members}
        onOpenSource={(memberDisplay, refId) => {
          openSources(memberDisplay.sources ?? [], refId);
        }}
      />
    );
  }

  return (
    <ToolActivityCard
      toolName={toolName}
      argsText={argsText}
      {...(resultText !== undefined ? { result: resultText } : {})}
      {...(isError !== undefined ? { isError } : {})}
      {...(startedAt !== undefined ? { startedAt } : {})}
      {...(finishedAt !== undefined ? { finishedAt } : {})}
      {...(display !== undefined ? { display, onOpenSource } : {})}
    />
  );
};
