import { useTranslation } from 'react-i18next';
import type { StudioKind, StudioModel } from './studioApi';
import { cheapestOptions, optionsSummary, type StudioOptions } from './studioForm';
import { RatioTiles } from './RatioTiles';
import { StudioField, StudioPopoverShell } from './StudioPopoverShell';
import { Slider } from '@/components/ui/slider';
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group';

// OptionsPopover — the shape, size and length of what is about to be generated, behind one
// pill that already reads the answer. Every control appears only when the model declares its
// values: an aspect ratio a model does not offer is not a choice, and a disabled row that
// never applies is noise in a bar that has to stay small.

interface OptionsPopoverProps {
  readonly kind: StudioKind;
  readonly model: StudioModel;
  readonly options: StudioOptions;
  readonly onChange: (options: StudioOptions) => void;
}

export function OptionsPopover({ kind, model, options, onChange }: OptionsPopoverProps) {
  const { t } = useTranslation();
  const ratios = model.aspect_ratios ?? [];
  const resolutions = model.resolutions ?? [];
  // A slider needs its stops in order; the catalog declares the durations in whatever order
  // the provider listed them.
  const durations = [...(model.durations ?? [])].sort((a, b) => a - b);
  const seconds = (value: number) => t('studio.options.durationValue', { seconds: value });
  // Resolution and duration are axes of POST /api/studio/videos and of nothing else. A model
  // the catalog happens to list under both kinds can declare them and still have them dropped
  // by requestBody on the image route, so what decides whether they are shown is the ROUTE the
  // Generate button will call, not what the row declares.
  const video = kind === 'video';
  // The pill reads what will be SENT, so an image request summarises its ratio alone even
  // when the row it is built from declares the video axes too.
  const summary = optionsSummary(
    video ? options : { ...options, resolution: '', duration: undefined },
    seconds,
  );

  if (ratios.length === 0 && (!video || (resolutions.length === 0 && durations.length === 0))) {
    return null;
  }

  const durationIndex = Math.max(
    0,
    durations.findIndex((value) => value === options.duration),
  );

  return (
    <StudioPopoverShell
      pill={summary === '' ? t('studio.options.open') : summary}
      pillLabel={t('studio.options.open')}
      title={kind === 'video' ? t('studio.options.titleVideo') : t('studio.options.titleImage')}
      onReset={() => {
        // Only the axes this popover owns: the seed and the sound switch live in Advanced,
        // and a Reset that silently changed a control in another popover would be invisible.
        const cheapest = cheapestOptions(model);
        onChange({
          ...options,
          ...(video ? { resolution: cheapest.resolution, duration: cheapest.duration } : {}),
          aspectRatio: cheapest.aspectRatio,
        });
      }}
    >
      {ratios.length > 0 ? (
        <StudioField label={t('studio.options.aspectRatio')}>
          <RatioTiles
            label={t('studio.options.aspectRatio')}
            ratios={ratios}
            value={options.aspectRatio}
            onChange={(aspectRatio) => {
              onChange({ ...options, aspectRatio });
            }}
          />
        </StudioField>
      ) : null}

      {video && resolutions.length > 0 ? (
        <StudioField label={t('studio.options.resolution')}>
          <ToggleGroup
            type="single"
            spacing={2}
            variant="outline"
            value={options.resolution}
            aria-label={t('studio.options.resolution')}
            onValueChange={(next: string) => {
              if (next !== '') onChange({ ...options, resolution: next });
            }}
            className="flex w-full flex-wrap gap-1.5"
          >
            {resolutions.map((resolution) => (
              <ToggleGroupItem
                key={resolution}
                value={resolution}
                className="h-8 rounded-[var(--radius-sm)] px-3 text-xs text-text-muted data-[state=on]:text-accent-text"
              >
                {resolution}
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
        </StudioField>
      ) : null}

      {video && durations.length > 0 ? (
        <StudioField
          label={t('studio.options.duration')}
          hint={seconds(options.duration ?? durations[durationIndex] ?? 0)}
        >
          {/* The stops are the declared durations, not a range: a provider that sells 4 s and
              8 s sells nothing in between, so the slider indexes the list.

              Radix names a lone thumb only from an aria-label passed to the Thumb itself,
              which the vendored Slider does not forward; the root is made a named group
              instead, so the control is announced rather than left as a bare "slider". */}
          <Slider
            role="group"
            min={0}
            max={Math.max(0, durations.length - 1)}
            step={1}
            value={[durationIndex]}
            aria-label={t('studio.options.duration')}
            onValueChange={([next]: number[]) => {
              const picked = durations[next ?? 0];
              if (picked !== undefined) onChange({ ...options, duration: picked });
            }}
          />
        </StudioField>
      ) : null}
    </StudioPopoverShell>
  );
}
