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
import { AttachmentCard } from './attachments/AttachmentCard';
import type { Asset } from './attachments/types';
import { BranchPicker } from './BranchPicker';
import { DisplayRouter } from './displays/DisplayRouter';
import { aggregateAnswerSources } from './displays/answerSources';
import { useSourceExplorer } from './displays/sourceExplorerControls';
import { SourcesButton } from './displays/SourcesButton';
import { isDisplayPayload, type DisplayPayload } from './displays/types';
import { MarkdownText } from './MarkdownText';
import { ReasoningDrawer } from './ReasoningDrawer';
import { ToolActivityCard } from './ToolActivityCard';

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

function messageAttachments(message: ThreadMessageLike): readonly Asset[] {
  const metadata = message.metadata?.custom as { attachments?: readonly Asset[] } | undefined;
  return metadata?.attachments ?? [];
}

export function UserMessage({ onAssetRetry, onAssetPromote, onAssetRemove }: UserMessageProps) {
  const { t } = useTranslation();
  const message = useAuiState((s) => s.message) as ThreadMessageLike;
  const attachments = messageAttachments(message);
  return (
    <MessagePrimitive.Root className="ml-auto flex max-w-[80%] flex-col items-end gap-1">
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
        <div className="rounded-3xl bg-surface-2 px-5 py-3">
          <MessagePrimitive.Parts
            components={{
              Text: () => (
                <div className="whitespace-pre-wrap text-base leading-relaxed text-text">
                  <MarkdownText />
                </div>
              ),
            }}
          />
        </div>
        {/* Edit a user turn → onEdit forks a branch + re-runs. Copy is the minimum verb. */}
        {attachments.length > 0 ? (
          <div className="flex flex-col items-end gap-2">
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
        <ActionBarPrimitive.Root className="flex items-center gap-2 opacity-0 transition-opacity focus-within:opacity-100 hover:opacity-100">
          <ActionBarPrimitive.Edit
            aria-label={t('chat.action.edit')}
            className="text-[0.75rem] text-text-muted outline-none hover:text-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
          >
            {t('chat.action.edit')}
          </ActionBarPrimitive.Edit>
          <ActionBarPrimitive.Copy
            aria-label={t('chat.action.copy')}
            className="text-[0.75rem] text-text-muted outline-none hover:text-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
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
  return (
    <MessagePrimitive.Root className="max-w-[90%] space-y-2">
      <MessagePrimitive.Parts
        components={{
          // Assistant prose → sanitized markdown.
          Text: () => (
            <div className="text-base leading-relaxed text-text">
              <MarkdownText />
            </div>
          ),
          // CoT → collapsible drawer (D-01). The drawer reads the reasoning text
          // from the part via the reasoning render-fn arg.
          Reasoning: ({ text }) => <ReasoningDrawer text={text} />,
          // Tool activity → typed display when a trusted backend normalizer
          // produced an aura.display payload (Phase 26, DISP-02); otherwise the
          // raw escaped card (D-02 / D-FALLBACK). The branch lives in ToolFallback.
          tools: {
            Fallback: ToolFallback,
          },
        }}
      />
      <MessagePrimitive.Error>
        <p role="alert" className="text-sm text-danger">
          {/* The reducer already routes RUN_ERROR into an error text part; this
              is the runtime-level fallback for a hard message error. */}
        </p>
      </MessagePrimitive.Error>
      {/* Assistant action bar: Copy + Reload (regenerate) + the answer-level
          "Sources (N)" affordance (D-13). The feedback rating group is deferred;
          Reload forks an assistant-turn branch. */}
      <ActionBarPrimitive.Root className="flex items-center gap-2 opacity-0 transition-opacity focus-within:opacity-100 hover:opacity-100">
        <ActionBarPrimitive.Copy
          aria-label={t('chat.action.copy')}
          className="text-[0.75rem] text-text-muted outline-none hover:text-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
        >
          {t('chat.action.copy')}
        </ActionBarPrimitive.Copy>
        <ActionBarPrimitive.Reload
          aria-label={t('chat.action.reload')}
          className="text-[0.75rem] text-text-muted outline-none hover:text-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
        >
          {t('chat.action.reload')}
        </ActionBarPrimitive.Reload>
        <BranchPicker />
      </ActionBarPrimitive.Root>
      {/* The "Sources (N)" affordance lives OUTSIDE the hover-only action bar so the
          evidence count is always visible (D-13); hidden when N=0. */}
      <AnswerSources />
    </MessagePrimitive.Root>
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
 * The tools.Fallback render: the single seam where a tool turn becomes a typed
 * display. It reads the custom `display` payload off the stored message part
 * (the sseAdapter attaches it by toolCallId, live or on replay) via useAuiState —
 * the external-store runtime passes our ThreadMessageLike part through unchanged
 * (convertMessage: identity), so the field survives.
 *
 * D-15 progressive swap: while a tool runs, no payload exists yet → the raw
 * running card stays; the typed display replaces it only once the aura.display
 * payload arrives on completion. D-FALLBACK: no/unknown payload → the raw card.
 *
 * Citation click-through (D-04): when the payload carries a source registry, a
 * chip click opens the SHARED Source Explorer (the same sheet the answer-level
 * "Sources (N)" button opens) over THIS turn's registry, focused on the refId.
 */
const ToolFallback: ToolCallMessagePartComponent = ({ toolName, argsText, result, isError }) => {
  const { openSources } = useSourceExplorer();
  const part = useAuiState((s) => s.part) as { display?: unknown };
  const display: DisplayPayload | undefined = isDisplayPayload(part.display)
    ? part.display
    : undefined;
  const resultText = typeof result === 'string' ? result : undefined;

  if (display !== undefined) {
    const onOpenSource = (refId: string) => {
      openSources(display.sources ?? [], refId);
    };
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
  return (
    <ToolActivityCard
      toolName={toolName}
      argsText={argsText}
      {...(resultText !== undefined ? { result: resultText } : {})}
      {...(isError !== undefined ? { isError } : {})}
    />
  );
};
