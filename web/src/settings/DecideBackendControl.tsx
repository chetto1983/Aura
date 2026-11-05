import { useState } from 'react';
import { Cloud, Cpu, Power } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { SettingsFields, type PickerBinding } from './SettingField';
import { RouteToggle, type RouteOption } from './RouteToggle';
import { DECIDE_MODEL_SETTING, type SettingsKey } from './modelSettingsDefs';
import type { LoadedState } from './modelSettingsState';

interface DecideBackendControlProps {
  readonly loaded: LoadedState;
  readonly onValueChange: (key: SettingsKey, value: string) => void;
  readonly modelPicker: PickerBinding;
}

type DecideChoice = 'off' | 'local' | 'openrouter';

const OFF = 'off';
const LICENCE_URL =
  'https://huggingface.co/LiquidAI/d1-3B-GGUF/blob/bb1e436ea78eb96a3f1acb6da865f70c2fbeb563/LICENSE';

// The route is saved with the pane, like any other row, and the mail watch reads it on its next
// tick: there is nothing to preview, apply or restart. An empty model is the local sidecar, which
// exists only where the installer put one; without it an empty model is the same as off.
export function DecideBackendControl({
  loaded,
  onValueChange,
  modelPicker,
}: DecideBackendControlProps) {
  const { t } = useTranslation();
  const value = (loaded.values.AURA_DECIDE_MODEL ?? '').trim();
  const sidecar = modelPicker.catalog.localSidecar;
  // OpenRouter is chosen before its model is: until then nothing is written.
  const [choosingModel, setChoosingModel] = useState(false);

  const hosted = value !== '' && value !== OFF;
  const choice: DecideChoice =
    hosted || choosingModel ? 'openrouter' : value === OFF || sidecar !== true ? 'off' : 'local';

  const write = (next: string) => {
    onValueChange('AURA_DECIDE_MODEL', next);
  };
  const choose = (next: DecideChoice) => {
    setChoosingModel(next === 'openrouter');
    if (next === 'off') write(OFF);
    if (next === 'local') write('');
  };
  const changeModel = (model: string) => {
    if (model.trim() === '') {
      setChoosingModel(false);
      write(sidecar === true ? '' : OFF);
      return;
    }
    setChoosingModel(false);
    write(model);
  };
  // Clearing the model is a choice of its own: the route it falls back to is named, not guessed.
  const picker: PickerBinding = {
    ...modelPicker,
    onValueChange: changeModel,
    emptyOption: { label: t(sidecar === true ? 'decideRoute.useLocal' : 'decideRoute.useOff') },
  };
  // 'off' is a route, not a model: the picker must not show it as one.
  const pickerLoaded: LoadedState = {
    ...loaded,
    values: { ...loaded.values, AURA_DECIDE_MODEL: hosted ? value : '' },
  };

  const options: readonly RouteOption<DecideChoice>[] = [
    { id: 'off', label: t('decideRoute.off'), icon: Power },
    { id: 'local', label: t('decideRoute.local'), icon: Cpu, disabled: sidecar !== true },
    { id: 'openrouter', label: t('decideRoute.openrouter'), icon: Cloud },
  ];

  return (
    <div className="flex max-w-3xl flex-col gap-5 rounded-[var(--radius-md)] border border-border bg-surface-2 p-5">
      <div className="flex flex-col gap-1">
        <h3 className="text-[15px] font-semibold text-text">{t('decideRoute.heading')}</h3>
        <p className="max-w-2xl text-[13px] leading-relaxed text-text-muted">
          {t('decideRoute.body')}
        </p>
      </div>
      <RouteToggle
        label={t('decideRoute.label')}
        value={choice}
        options={options}
        onChange={choose}
      />
      {sidecar === false ? (
        <p className="text-[13px] leading-relaxed text-text-muted">
          {t('decideRoute.localMissing')}
        </p>
      ) : null}
      {choice === 'local' ? (
        <p className="text-[13px] leading-relaxed text-text-muted">
          {t('decideRoute.localCaption')}{' '}
          <a
            href={LICENCE_URL}
            target="_blank"
            rel="noreferrer noopener"
            className="underline underline-offset-2"
          >
            {t('decideRoute.licence')}
          </a>
        </p>
      ) : null}
      {choice === 'openrouter' ? (
        <>
          <SettingsFields
            variant="inline"
            defs={[DECIDE_MODEL_SETTING]}
            loaded={pickerLoaded}
            resetting={undefined}
            onValueChange={onValueChange}
            pickers={{ AURA_DECIDE_MODEL: picker }}
          />
          <p className="text-[13px] leading-relaxed text-text-muted">
            {t('decideRoute.openrouterCaption')}
          </p>
        </>
      ) : null}
    </div>
  );
}
