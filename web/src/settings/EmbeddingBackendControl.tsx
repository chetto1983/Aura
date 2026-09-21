import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { SettingsFields, type PickerBinding } from './SettingField';
import { EMBEDDING_SETTINGS, type SettingDef, type SettingsKey } from './modelSettingsDefs';
import {
  embeddingBackendChoice,
  embeddingBackendValues,
  normalizeEmbeddingCloudBaseURL,
  type EmbeddingBackendChoice,
} from './embeddingBackendState';
import type { LoadedState } from './modelSettingsState';
import { Button } from '@/components/ui/button';

interface EmbeddingBackendControlProps {
  readonly loaded: LoadedState;
  readonly resetting: string | undefined;
  readonly onValueChange: (key: SettingsKey, value: string) => void;
  readonly onReset: (key: SettingsKey) => void;
  readonly modelPicker: PickerBinding;
  readonly openRouterAvailable: boolean;
  readonly onRouteValidityChange: (valid: boolean) => void;
}

function embeddingSetting(key: SettingsKey): SettingDef {
  const setting = EMBEDDING_SETTINGS.find((def) => def.key === key);
  if (setting === undefined) throw new Error(`missing embedding setting ${key}`);
  return setting;
}

const localBaseURL = embeddingSetting('AURA_EMBED_BASE_URL');
const cloudBaseURL = embeddingSetting('AURA_EMBED_CLOUD_BASE_URL');
const model = embeddingSetting('AURA_EMBED_MODEL');

export function EmbeddingBackendControl({
  loaded,
  resetting,
  onValueChange,
  onReset,
  modelPicker,
  openRouterAvailable,
  onRouteValidityChange,
}: EmbeddingBackendControlProps) {
  const { t } = useTranslation();
  const configuredChoice = embeddingBackendChoice(
    loaded.values.AURA_EMBED_MODEL ?? '',
    loaded.values.AURA_EMBED_CLOUD_BASE_URL ?? '',
  );
  const persisted = JSON.stringify([
    loaded.initial.AURA_EMBED_MODEL ?? '',
    loaded.initial.AURA_EMBED_CLOUD_BASE_URL ?? '',
  ]);
  const [choice, setChoice] = useState<EmbeddingBackendChoice>(configuredChoice);
  // Re-derived during render rather than in an effect: a save rewrites the persisted route,
  // and setState in an effect would cascade a second render to show it.
  const [syncedFrom, setSyncedFrom] = useState(persisted);
  if (syncedFrom !== persisted) {
    setSyncedFrom(persisted);
    setChoice(configuredChoice);
  }

  const manualURLMissing =
    choice === 'manual' && (loaded.values.AURA_EMBED_CLOUD_BASE_URL ?? '').trim() === '';
  const modelMissing = choice !== 'local' && (loaded.values.AURA_EMBED_MODEL ?? '').trim() === '';
  useEffect(() => {
    onRouteValidityChange(!manualURLMissing && !modelMissing);
  }, [manualURLMissing, modelMissing, onRouteValidityChange]);

  const choose = (next: EmbeddingBackendChoice) => {
    setChoice(next);
    onRouteValidityChange(next === 'local' || (next === 'openrouter' && !modelMissing));
    for (const [key, value] of Object.entries(embeddingBackendValues(next))) {
      onValueChange(key as SettingsKey, value);
    }
  };
  const changeModel = (value: string) => {
    onValueChange('AURA_EMBED_MODEL', value);
    if (choice === 'openrouter' && value.trim() === '') {
      setChoice('local');
      onRouteValidityChange(true);
    }
  };
  const picker: PickerBinding = { ...modelPicker, onValueChange: changeModel };

  return (
    <div className="flex flex-col gap-4 rounded-md border border-border bg-surface-2 p-4">
      <div className="flex flex-col gap-1">
        <h3 className="text-[15px] font-semibold text-text">{t('settings.embedding.heading')}</h3>
        <p className="text-[13px] leading-relaxed text-text-muted">
          {t('settings.embedding.body')}
        </p>
      </div>
      <div role="group" aria-label={t('settings.embedding.label')} className="flex flex-wrap gap-2">
        {(['local', 'openrouter', 'manual'] as const).map((option) => {
          const unavailable = option === 'openrouter' && !openRouterAvailable;
          return (
            <Button
              key={option}
              type="button"
              variant={choice === option ? 'default' : 'outline'}
              aria-pressed={choice === option}
              disabled={unavailable}
              onClick={() => {
                choose(option);
              }}
            >
              {t(`settings.embedding.${option}`)}
            </Button>
          );
        })}
      </div>
      {!openRouterAvailable ? (
        <p className="text-[12px] text-text-muted">
          {t('settings.embedding.openrouterUnavailable')}
        </p>
      ) : null}
      {choice === 'local' ? (
        <SettingsFields
          defs={[localBaseURL]}
          loaded={loaded}
          resetting={resetting}
          onValueChange={onValueChange}
          onReset={onReset}
        />
      ) : null}
      {choice === 'manual' ? (
        <SettingsFields
          defs={[cloudBaseURL, model]}
          loaded={loaded}
          resetting={resetting}
          onValueChange={(key, value) => {
            onValueChange(
              key,
              key === 'AURA_EMBED_CLOUD_BASE_URL' ? normalizeEmbeddingCloudBaseURL(value) : value,
            );
          }}
          onReset={onReset}
          pickers={{ AURA_EMBED_MODEL: picker }}
          invalidKeys={manualURLMissing ? new Set(['AURA_EMBED_CLOUD_BASE_URL']) : undefined}
          describedBy={
            manualURLMissing
              ? { AURA_EMBED_CLOUD_BASE_URL: 'embedding-manual-url-error' }
              : undefined
          }
        />
      ) : null}
      {choice === 'openrouter' ? (
        <SettingsFields
          defs={[model]}
          loaded={loaded}
          resetting={resetting}
          onValueChange={onValueChange}
          onReset={onReset}
          pickers={{ AURA_EMBED_MODEL: picker }}
        />
      ) : null}
      {manualURLMissing || modelMissing ? (
        <p
          {...(manualURLMissing ? { id: 'embedding-manual-url-error' } : {})}
          role="alert"
          className="text-[13px] text-danger"
        >
          {t(
            manualURLMissing
              ? 'settings.embedding.manualURLRequired'
              : 'settings.embedding.modelRequired',
          )}
        </p>
      ) : null}
    </div>
  );
}
