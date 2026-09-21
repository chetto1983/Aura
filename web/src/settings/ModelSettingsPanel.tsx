import { useMemo } from 'react';
import type { TFunction } from 'i18next';
import { Cloud, Cpu, RefreshCw, Save, Server } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Spinner } from '../components/Spinner';
import { SettingsFields, type PickerBindings } from './SettingField';
import { RestartAuraControl } from './RestartAuraControl';
import { useModelSettings, type SaveOutcome } from './modelSettingsState';
import { useModelCatalog, type ModelCatalogState } from './useModelCatalog';
import { useMediaModelCatalog } from './useMediaModelCatalog';
import type { MediaCatalogModel, ModelRow, VoiceCatalogModel } from './mediaModelCatalog';
import { modelRowMeta, type MediaLabels } from './mediaModelCatalogFormat';
import {
  MODEL_SETTINGS_GROUPS,
  PROVIDER_OPTIONS,
  resolveProvider,
  routeForProvider,
  type ModelSettingsGroup,
  type ProviderChoice,
} from './modelSettingsDefs';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';

// The icon is presentation, so it lives here rather than in the shared route definitions.
const PROVIDER_ICONS: Record<ProviderChoice, typeof Cloud> = {
  cloud: Cloud,
  local: Cpu,
  ollama: Server,
};

// providerIDOf turns the active button back into the provider id the catalogue probe takes.
function providerIDOf(choice: ProviderChoice): string {
  return PROVIDER_OPTIONS.find((option) => option.id === choice)?.provider ?? '';
}

function mediaLabels(t: TFunction): MediaLabels {
  return {
    references: (max) => t('settings.models.references', { count: max }),
    duration: (min, max) =>
      t('settings.models.seconds', {
        range: min === max ? String(min) : `${String(min)}–${String(max)}`,
      }),
    imageToVideo: t('settings.models.imageToVideo'),
    imageTokens: (price) => t('settings.models.imageTokens', { price }),
  };
}

function ttsVoiceCatalog(
  catalog: ModelCatalogState<MediaCatalogModel>,
  modelID: string,
): ModelCatalogState<ModelRow> {
  const model = catalog.models.find(
    (row): row is VoiceCatalogModel => row.kind === 'speech' && row.id === modelID,
  );
  return {
    ...catalog,
    models: (model?.voices ?? []).map((id) => ({ kind: 'speech', id, has_price: false })),
  };
}

interface ModelSettingsPanelProps {
  readonly className?: string;
  /**
   * Which runtime groups this panel renders and may write. The Settings rail mounts one group
   * per pane, so each pane is one form with one save button; the first-run route step mounts
   * the routing group.
   */
  readonly groups: readonly ModelSettingsGroup[];
  readonly onComplete?: (outcome?: SaveOutcome) => void | Promise<void>;
  readonly saveLabel?: string;
  readonly skipLabel?: string;
  /** Whether Skip shows beside Save; the first-run route step hides it while the route is required. */
  readonly skippable?: boolean;
}

export function ModelSettingsPanel({
  className,
  groups,
  onComplete,
  saveLabel,
  skipLabel,
  skippable = true,
}: ModelSettingsPanelProps) {
  const { t } = useTranslation();
  const activeGroups = useMemo(
    () => MODEL_SETTINGS_GROUPS.filter((group) => groups.includes(group.id)),
    [groups],
  );
  const scope = useMemo(
    () => activeGroups.flatMap((group) => [...group.fields, ...group.tracked]),
    [activeGroups],
  );
  const {
    loaded,
    loadStatus,
    routes,
    saving,
    resetting,
    saved,
    saveError,
    dirtyKeys,
    reload,
    setValue,
    save,
    resetSetting,
  } = useModelSettings(scope);
  const formBaseURL = loaded?.values.AURA_LLM_BASE_URL ?? '';
  // Which button reads as active, and which provider the catalogue is probed as. It comes
  // from resolveProvider rather than the raw row so a deployment that never wrote
  // AURA_LLM_PROVIDER still probes as the provider its base URL points at.
  const provider = resolveProvider(loaded?.values.AURA_LLM_PROVIDER ?? '', formBaseURL);
  // The catalogue follows the FORM route, not the saved one, so the model list is the list
  // of the endpoint the operator is currently pointing at.
  const catalog = useModelCatalog(providerIDOf(provider), formBaseURL);
  // The media models are Cloud rows of the routing pane, but the daemon lists them from the
  // SAVED route: they are asked for only while the rows show AND the saved route is Cloud, and
  // a save that changes the saved route asks again.
  const savedProvider = loaded?.initial.AURA_LLM_PROVIDER ?? '';
  const savedBaseURL = loaded?.initial.AURA_LLM_BASE_URL ?? '';
  const savedRoute = `${savedProvider} ${savedBaseURL}`;
  // ONE rule for every picker: the daemon lists from the route it runs on, so a list exists
  // only while the SAVED route is OpenRouter, and only the pane on screen asks for one. The
  // media and backend pickers used to spell this out separately and could therefore disagree.
  const savedRouteIsCloud =
    loaded !== undefined && resolveProvider(savedProvider, savedBaseURL) === 'cloud';
  // The media rows additionally sit under the routing pane's Cloud button, so they follow the
  // FORM provider as well: choosing Local hides them before the choice is saved.
  const mediaEnabled = savedRouteIsCloud && groups.includes('routing') && provider === 'cloud';
  // The cloud STT, TTS and embedding models are rows of the backends pane, and the clients
  // that use them ride the same saved OpenRouter route.
  const backendsEnabled = savedRouteIsCloud && groups.includes('backends');
  const imageCatalog = useMediaModelCatalog('image', mediaEnabled, savedRoute);
  const videoCatalog = useMediaModelCatalog('video', mediaEnabled, savedRoute);
  const transcriptionCatalog = useMediaModelCatalog('transcription', backendsEnabled, savedRoute);
  const speechCatalog = useMediaModelCatalog('speech', backendsEnabled, savedRoute);
  const embeddingCatalog = useMediaModelCatalog('embeddings', backendsEnabled, savedRoute);
  const cloudVoiceCatalog = ttsVoiceCatalog(speechCatalog, loaded?.values.AURA_TTS_MODEL ?? '');

  if (loadStatus === 'loading') {
    return (
      <div
        role="status"
        className={cn('grid min-h-40 place-items-center text-sm text-text-muted', className)}
      >
        {t('settings.loading')}
      </div>
    );
  }

  if (loadStatus === 'error' || loaded === undefined) {
    return (
      <Alert variant="destructive" className={className}>
        <AlertDescription>
          <span>{t('settings.error')}</span>
          <Button type="button" variant="outline" onClick={() => void reload()} className="mt-3">
            <RefreshCw aria-hidden="true" />
            {t('settings.actions.retry')}
          </Button>
        </AlertDescription>
      </Alert>
    );
  }

  const saveButtonLabel =
    onComplete !== undefined && dirtyKeys.length === 0
      ? t('settings.actions.continue')
      : (saveLabel ?? t('settings.actions.save'));
  const labels = mediaLabels(t);
  const formatRow = (row: ModelRow, freeLabel: string) => modelRowMeta(row, freeLabel, labels);
  const localSidecarOption = {
    label: t('settings.models.useLocalSidecar'),
    description: t('settings.models.useLocalSidecarDescription'),
  };
  const selectTTSModel = (modelID: string) => {
    setValue('AURA_TTS_MODEL', modelID);
    if (modelID === '') {
      setValue('AURA_TTS_CLOUD_VOICE', '');
      return;
    }
    const model = speechCatalog.models.find(
      (row): row is VoiceCatalogModel => row.kind === 'speech' && row.id === modelID,
    );
    const voices = model?.voices ?? [];
    const currentVoice = loaded.values.AURA_TTS_CLOUD_VOICE ?? '';
    if (!voices.includes(currentVoice)) setValue('AURA_TTS_CLOUD_VOICE', voices[0] ?? '');
  };
  const pickers: PickerBindings = {
    AURA_LLM_MODEL: { catalog, formatRow },
    AURA_IMAGE_MODEL: { catalog: imageCatalog, formatRow },
    AURA_VIDEO_MODEL: { catalog: videoCatalog, formatRow },
    // Empty is the LOCAL embedding sidecar, exactly as it is for STT and TTS: the model is
    // the whole local-versus-cloud switch, and AURA_EMBED_BASE_URL never names a cloud one.
    AURA_EMBED_MODEL: {
      catalog: embeddingCatalog,
      formatRow,
      emptyOption: localSidecarOption,
    },
    AURA_STT_CLOUD_MODEL: {
      catalog: transcriptionCatalog,
      formatRow,
      emptyOption: localSidecarOption,
    },
    AURA_TTS_MODEL: {
      catalog: speechCatalog,
      formatRow,
      emptyOption: localSidecarOption,
      onValueChange: selectTTSModel,
    },
    AURA_TTS_CLOUD_VOICE: { catalog: cloudVoiceCatalog, formatRow },
  };

  return (
    <div className={cn('flex flex-col gap-7', className)}>
      {activeGroups.map((group, index) => (
        <section
          key={group.id}
          aria-labelledby={group.labelId}
          className={cn('flex flex-col gap-4', index > 0 && 'border-t border-border pt-6')}
        >
          <div className="flex flex-col gap-2">
            <h2 id={group.labelId} className="text-[20px] font-semibold text-text">
              {t(group.headingKey)}
            </h2>
            <p className="max-w-3xl text-[15.5px] leading-relaxed text-text-muted">
              {t(group.bodyKey)}
            </p>
          </div>

          {group.id === 'routing' ? (
            <div
              className="flex flex-wrap items-center gap-2"
              role="group"
              aria-label={t('settings.provider.label')}
            >
              {PROVIDER_OPTIONS.map((option) => {
                const Icon = PROVIDER_ICONS[option.id];
                return (
                  <Button
                    key={option.id}
                    type="button"
                    variant={provider === option.id ? 'default' : 'outline'}
                    aria-pressed={provider === option.id}
                    onClick={() => {
                      // The route comes from what this provider was last saved or booted
                      // with; the compiled-in constant is only reached by a provider this
                      // deployment has never configured.
                      const route = routeForProvider(option, routes, {
                        provider: loaded.initial.AURA_LLM_PROVIDER ?? '',
                        baseURL: loaded.initial.AURA_LLM_BASE_URL ?? '',
                        model: loaded.initial.AURA_LLM_MODEL ?? '',
                      });
                      setValue('AURA_LLM_BASE_URL', route.baseURL);
                      setValue('AURA_LLM_MODEL', route.model);
                      setValue('AURA_LLM_PROVIDER', option.provider);
                    }}
                  >
                    <Icon aria-hidden="true" />
                    {t(option.labelKey)}
                  </Button>
                );
              })}
            </div>
          ) : null}

          <SettingsFields
            defs={group.fields.filter(
              (def) =>
                (def.cloudOnly !== true || provider === 'cloud') &&
                (def.key !== 'AURA_TTS_CLOUD_VOICE' ||
                  (loaded.values.AURA_TTS_MODEL ?? '').trim() !== ''),
            )}
            loaded={loaded}
            resetting={resetting}
            onValueChange={setValue}
            onReset={(key) => void resetSetting(key)}
            pickers={pickers}
          />
        </section>
      ))}

      {loaded.restartRequired ? (
        <div
          role="note"
          className="rounded-md border border-warning bg-warning/10 px-4 py-3 text-[13px] text-warning"
        >
          {loaded.restartKeys.length > 0
            ? t('settings.restartRequiredFor', { keys: loaded.restartKeys.join(', ') })
            : t('settings.restartRequired')}
          {loaded.restartSupported ? <RestartAuraControl /> : null}
        </div>
      ) : null}

      {saved ? (
        <div
          role="status"
          className="rounded-md border border-success bg-success/10 px-4 py-3 text-[13px] text-success"
        >
          {t('settings.saved')}
        </div>
      ) : null}

      {saveError === undefined ? null : (
        // role="alert" rather than "status": a save the operator believes succeeded but did
        // not is worth interrupting for, and it carries the server's own reason so the
        // rejected value can actually be corrected.
        <div
          role="alert"
          className="rounded-md border border-danger bg-danger/10 px-4 py-3 text-[13px] text-danger"
        >
          {t('settings.saveError', { message: saveError })}
        </div>
      )}

      <div className="flex flex-wrap items-center gap-2 border-t border-border pt-5">
        <Button
          type="button"
          disabled={saving}
          aria-busy={saving}
          onClick={() => void save(onComplete)}
        >
          {saving ? <Spinner /> : <Save aria-hidden="true" />}
          {saveButtonLabel}
        </Button>
        {onComplete !== undefined && skippable ? (
          <Button
            type="button"
            variant="outline"
            disabled={saving}
            onClick={() => void onComplete()}
          >
            {skipLabel ?? t('settings.actions.skip')}
          </Button>
        ) : null}
      </div>
    </div>
  );
}
