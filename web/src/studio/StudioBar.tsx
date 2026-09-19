import { useState } from 'react';
import { Maximize2, Minimize2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { AdvancedPopover } from './AdvancedPopover';
import { OptionsPopover } from './OptionsPopover';
import { StudioFrames } from './StudioFrames';
import { StudioModelPill } from './StudioModelPill';
import type { StudioKind, StudioModel } from './studioApi';
import { estimateCost, type StudioDraft } from './studioForm';
import { formatEstimate } from './studioPrice';
import { Button } from '@/components/ui/button';
import { Kbd, KbdGroup } from '@/components/ui/kbd';
import { Textarea } from '@/components/ui/textarea';
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group';

// StudioBar — the one control surface the Studio is built around: the frames, the prompt, and
// a row of pills that each read their own answer. Everything it needs is passed in; the page
// above owns the draft, so switching model or mode can reconcile it in one place rather than
// leaving each pill to guess what the new model still accepts.

interface StudioBarProps {
  readonly draft: StudioDraft;
  /** The model `draft.model` names — resolved by the page, which also guarantees it is one
   *  of `models`, so there is no "selected model is missing" branch here. */
  readonly model: StudioModel;
  readonly models: readonly StudioModel[];
  readonly submitting: boolean;
  readonly onDraftChange: (draft: StudioDraft) => void;
  readonly onKindChange: (kind: StudioKind) => void;
  readonly onModelChange: (modelId: string) => void;
  readonly onSubmit: () => void;
}

export function StudioBar({
  draft,
  model,
  models,
  submitting,
  onDraftChange,
  onKindChange,
  onModelChange,
  onSubmit,
}: StudioBarProps) {
  const { t } = useTranslation();
  const [expanded, setExpanded] = useState(false);
  const estimate = estimateCost(model, draft.options);
  const ready = draft.prompt.trim() !== '' && !submitting;

  return (
    <form
      className="studio-composer flex w-full max-w-4xl flex-col gap-3 rounded-[var(--radius-lg)] border border-border bg-surface/95 p-3 shadow-[var(--shadow-popover)] backdrop-blur"
      data-expanded={expanded}
      onSubmit={(event) => {
        event.preventDefault();
        if (ready) onSubmit();
      }}
    >
      <Button
        type="button"
        size="icon"
        variant="ghost"
        aria-label={expanded ? t('studio.composer.collapse') : t('studio.composer.expand')}
        aria-pressed={expanded}
        className="studio-composer-expand absolute top-2 right-2 z-10 hidden size-8 min-h-8 rounded-full p-0 text-text-faint hover:bg-surface-2 hover:text-text"
        onClick={() => {
          setExpanded((current) => !current);
        }}
      >
        {expanded ? (
          <Minimize2 aria-hidden="true" className="size-3.5" />
        ) : (
          <Maximize2 aria-hidden="true" className="size-3.5" />
        )}
      </Button>

      <StudioFrames draft={draft} model={model} onChange={onDraftChange} />

      <Textarea
        aria-label={t('studio.prompt.label')}
        placeholder={draft.kind === 'image' ? t('studio.prompt.image') : t('studio.prompt.video')}
        value={draft.prompt}
        rows={2}
        className="studio-prompt max-h-40 resize-none border-0 bg-transparent px-1 text-[14px] text-text shadow-none focus-visible:border-0 focus-visible:ring-0"
        onChange={(event) => {
          onDraftChange({ ...draft, prompt: event.target.value });
        }}
        onKeyDown={(event) => {
          if (event.key !== 'Enter' || !(event.ctrlKey || event.metaKey)) return;
          event.preventDefault();
          if (ready) onSubmit();
        }}
      />

      <div className="studio-controls">
        <div className="studio-controls-rail">
          <ToggleGroup
            type="single"
            spacing={0}
            value={draft.kind}
            aria-label={t('studio.kind.label')}
            onValueChange={(next: string) => {
              if (next === 'image' || next === 'video') onKindChange(next);
            }}
            className="h-8 rounded-[var(--radius-pill)] border border-border bg-surface-2 p-0.5"
          >
            {(['image', 'video'] as const).map((kind) => (
              <ToggleGroupItem
                key={kind}
                value={kind}
                className="h-7 rounded-[var(--radius-pill)] px-3 text-xs text-text-muted data-[state=on]:bg-accent data-[state=on]:text-accent-text"
              >
                {kind === 'image' ? t('studio.kind.image') : t('studio.kind.video')}
              </ToggleGroupItem>
            ))}
          </ToggleGroup>

          <StudioModelPill models={models} value={draft.model} onChange={onModelChange} />

          <OptionsPopover
            kind={draft.kind}
            model={model}
            options={draft.options}
            onChange={(options) => {
              onDraftChange({ ...draft, options });
            }}
          />

          {/* Video only, by route rather than by capability: POST /api/studio/images takes
              neither a seed nor an audio flag, so a row that happens to declare them must not
              offer controls whose values requestBody would silently drop. */}
          {draft.kind === 'video' ? (
            <AdvancedPopover
              model={model}
              options={draft.options}
              onChange={(options) => {
                onDraftChange({ ...draft, options });
              }}
            />
          ) : null}
        </div>

        <div className="studio-actions">
          <span
            data-testid="studio-estimate"
            className="text-[11px] text-text-faint tabular-nums"
            {...(estimate === undefined ? { title: t('studio.generate.costUnknownHint') } : {})}
          >
            {estimate === undefined
              ? t('studio.generate.costUnknown')
              : t('studio.generate.cost', { cost: formatEstimate(estimate) })}
          </span>
          <Button type="submit" size="sm" disabled={!ready} className="min-h-8 gap-2 py-1">
            {submitting ? t('studio.generate.pending') : t('studio.generate.action')}
            <KbdGroup aria-hidden="true" className="max-sm:hidden">
              <Kbd>Ctrl</Kbd>
              <Kbd>⏎</Kbd>
            </KbdGroup>
          </Button>
        </div>
      </div>
    </form>
  );
}
