import { useState } from 'react';
import { SlidersHorizontal } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import type { StudioModel } from './studioApi';
import type { StudioOptions } from './studioForm';
import { StudioField, StudioPopoverShell } from './StudioPopoverShell';
import { Input } from '@/components/ui/input';
import { Switch } from '@/components/ui/switch';

// AdvancedPopover — the two video knobs that are not about what the clip looks like: the seed
// that makes a run repeatable and the sound the provider can add. Both are hidden unless the
// model declares them, and the pill itself disappears when it declares neither, because an
// empty popover is a promise of a control that is not there.

interface AdvancedPopoverProps {
  readonly model: StudioModel;
  readonly options: StudioOptions;
  readonly onChange: (options: StudioOptions) => void;
}

/** The typed seed, or undefined for "let the provider choose". A partially typed value (an
 *  empty box, a lone "-") is not a number, and sending 0 for it would silently pin every
 *  generation to the same seed. */
function parseSeed(raw: string): number | undefined {
  const trimmed = raw.trim();
  if (trimmed === '') return undefined;
  const value = Number(trimmed);
  return Number.isInteger(value) ? value : undefined;
}

export function AdvancedPopover({ model, options, onChange }: AdvancedPopoverProps) {
  const { t } = useTranslation();
  // The buffer holds ONLY a keystroke that is not a seed — "1.5", a lone "-". A valid seed
  // lives in the draft and nowhere else, so the box can never read a value the body will not
  // carry: an external write (Reuse, Reset) clearing the seed clears the box with it, because
  // there is nothing local left to fall back to.
  const [rejected, setRejected] = useState('');
  const shown = options.seed === undefined ? rejected : String(options.seed);
  const invalid = shown !== '' && parseSeed(shown) === undefined;
  if (!model.seed && !model.audio) return null;

  return (
    <StudioPopoverShell
      pill={<SlidersHorizontal aria-hidden="true" className="size-3.5" />}
      pillLabel={t('studio.advanced.title')}
      title={t('studio.advanced.title')}
      onReset={() => {
        setRejected('');
        onChange({ ...options, audio: false, seed: undefined });
      }}
    >
      {model.seed ? (
        <StudioField label={t('studio.options.seed')}>
          <Input
            type="number"
            step={1}
            inputMode="numeric"
            aria-label={t('studio.options.seed')}
            aria-invalid={invalid}
            placeholder={t('studio.options.seedPlaceholder')}
            className="min-h-9 py-1"
            value={shown}
            onChange={(event) => {
              const seed = parseSeed(event.target.value);
              setRejected(seed === undefined ? event.target.value : '');
              onChange({ ...options, seed });
            }}
          />
          {invalid ? (
            <p role="alert" className="text-[11px] text-danger">
              {t('studio.options.seedInvalid')}
            </p>
          ) : null}
        </StudioField>
      ) : null}

      {model.audio ? (
        <label className="flex items-center justify-between gap-3 text-[13px] text-text">
          {t('studio.options.audio')}
          <Switch
            checked={options.audio}
            onCheckedChange={(audio: boolean) => {
              onChange({ ...options, audio });
            }}
          />
        </label>
      ) : null}
    </StudioPopoverShell>
  );
}
