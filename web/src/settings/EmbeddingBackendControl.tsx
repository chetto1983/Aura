import { useEffect, useState } from 'react';
import { Cloud, Cpu, Link2, ScanSearch } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useQueryClient } from '@tanstack/react-query';
import { Spinner } from '../components/Spinner';
import { SettingsFields, type PickerBinding } from './SettingField';
import { RouteToggle, type RouteOption } from './RouteToggle';
import { RestartAuraControl } from './RestartAuraControl';
import { EmbeddingRoutePreviewCard } from './EmbeddingRoutePreview';
import { browserRestartDeps, watchRestart } from './restartAura';
import { EMBEDDING_SETTINGS, type SettingDef, type SettingsKey } from './modelSettingsDefs';
import {
  embeddingBackendChoice,
  embeddingBackendValues,
  embeddingRouteOf,
  normalizeEmbeddingCloudBaseURL,
  sameEmbeddingRoute,
  type EmbeddingBackendChoice,
} from './embeddingBackendState';
import {
  EMBEDDING_SPACE_QUERY_KEY,
  applyEmbeddingRoute,
  previewEmbeddingRoute,
  type EmbeddingRoutePreview,
} from './embeddingSpaceApi';
import type { LoadedState } from './modelSettingsState';
import { Button } from '@/components/ui/button';

interface EmbeddingBackendControlProps {
  readonly loaded: LoadedState;
  readonly onValueChange: (key: SettingsKey, value: string) => void;
  readonly modelPicker: PickerBinding;
  readonly openRouterAvailable: boolean;
}

type ChangeStatus =
  'idle' | 'previewing' | 'applying' | 'restarting' | 'timedOut' | 'restartRequired';

function embeddingSetting(key: SettingsKey): SettingDef {
  const setting = EMBEDDING_SETTINGS.find((def) => def.key === key);
  if (setting === undefined) throw new Error(`missing embedding setting ${key}`);
  return setting;
}

const localBaseURL = embeddingSetting('AURA_EMBED_BASE_URL');
const cloudBaseURL = embeddingSetting('AURA_EMBED_CLOUD_BASE_URL');
const model = embeddingSetting('AURA_EMBED_MODEL');

function messageOf(err: unknown): string {
  return err instanceof Error && err.message.trim() !== '' ? err.message : String(err);
}

// The route is edited here but never saved with the pane: a new route moves every stored
// vector into another space, so it is previewed, confirmed and applied on its own (spec §4),
// and the daemon restarts on it.
export function EmbeddingBackendControl({
  loaded,
  onValueChange,
  modelPicker,
  openRouterAvailable,
}: EmbeddingBackendControlProps) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
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

  const route = embeddingRouteOf(loaded.values);
  const routeKey = JSON.stringify(route);
  const changed = !sameEmbeddingRoute(route, embeddingRouteOf(loaded.initial));
  const [status, setStatus] = useState<ChangeStatus>('idle');
  const [error, setError] = useState<string | undefined>(undefined);
  // A preview describes one route: editing the route leaves it behind.
  const [preview, setPreview] = useState<{
    readonly key: string;
    readonly value: EmbeddingRoutePreview;
  }>();
  const current = preview?.key === routeKey ? preview.value : undefined;

  useEffect(() => {
    if (status !== 'restarting') return undefined;
    return watchRestart(browserRestartDeps, () => {
      setStatus('timedOut');
    });
  }, [status]);

  const manualURLMissing =
    choice === 'manual' && (loaded.values.AURA_EMBED_CLOUD_BASE_URL ?? '').trim() === '';
  const modelMissing = choice !== 'local' && (loaded.values.AURA_EMBED_MODEL ?? '').trim() === '';
  const valid = !manualURLMissing && !modelMissing;

  const choose = (next: EmbeddingBackendChoice) => {
    setChoice(next);
    for (const [key, value] of Object.entries(embeddingBackendValues(next))) {
      onValueChange(key as SettingsKey, value);
    }
  };
  const changeModel = (value: string) => {
    onValueChange('AURA_EMBED_MODEL', value);
    if (choice === 'openrouter' && value.trim() === '') setChoice('local');
  };
  const picker: PickerBinding = { ...modelPicker, onValueChange: changeModel };

  async function runPreview() {
    setStatus('previewing');
    setError(undefined);
    try {
      setPreview({ key: routeKey, value: await previewEmbeddingRoute(route) });
    } catch (err) {
      setError(t('embeddingRoute.previewFailed', { message: messageOf(err) }));
    } finally {
      setStatus('idle');
    }
  }

  async function apply() {
    if (current === undefined) return;
    setStatus('applying');
    setError(undefined);
    try {
      const applied = await applyEmbeddingRoute(route, current.space);
      await queryClient.invalidateQueries({ queryKey: EMBEDDING_SPACE_QUERY_KEY });
      setStatus(applied.restarting ? 'restarting' : 'restartRequired');
    } catch (err) {
      setError(t('embeddingRoute.applyFailed', { message: messageOf(err) }));
      setStatus('idle');
    }
  }

  const options: readonly RouteOption<EmbeddingBackendChoice>[] = [
    { id: 'local', label: t('settings.embedding.local'), icon: Cpu },
    {
      id: 'openrouter',
      label: t('settings.embedding.openrouter'),
      icon: Cloud,
      disabled: !openRouterAvailable,
    },
    { id: 'manual', label: t('settings.embedding.manual'), icon: Link2 },
  ];

  return (
    // Capped at the width the section's own prose uses: a full-bleed card around a single URL
    // leaves half a row of nothing, and the tiles under it are not full width either.
    <div className="flex max-w-3xl flex-col gap-5 rounded-[var(--radius-md)] border border-border bg-surface-2 p-5">
      <div className="flex flex-col gap-1">
        <h3 className="text-[15px] font-semibold text-text">{t('settings.embedding.heading')}</h3>
        <p className="max-w-2xl text-[13px] leading-relaxed text-text-muted">
          {t('settings.embedding.body')}
        </p>
      </div>
      <div className="flex flex-col gap-2">
        <RouteToggle
          label={t('settings.embedding.label')}
          value={choice}
          options={options}
          onChange={choose}
        />
        {!openRouterAvailable ? (
          <p className="text-[12px] text-text-faint">
            {t('settings.embedding.openrouterUnavailable')}
          </p>
        ) : null}
      </div>
      {choice === 'local' ? (
        <SettingsFields
          variant="inline"
          defs={[localBaseURL]}
          loaded={loaded}
          resetting={undefined}
          onValueChange={onValueChange}
        />
      ) : null}
      {choice === 'manual' ? (
        <SettingsFields
          variant="inline"
          defs={[cloudBaseURL, model]}
          loaded={loaded}
          resetting={undefined}
          onValueChange={(key, value) => {
            onValueChange(
              key,
              key === 'AURA_EMBED_CLOUD_BASE_URL' ? normalizeEmbeddingCloudBaseURL(value) : value,
            );
          }}
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
          variant="inline"
          defs={[model]}
          loaded={loaded}
          resetting={undefined}
          onValueChange={onValueChange}
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
      {changed && status !== 'restarting' && status !== 'restartRequired' ? (
        <div className="flex flex-col gap-3 border-t border-border pt-4">
          <p className="text-[13px] leading-relaxed text-text-muted">
            {t('embeddingRoute.changed')}
          </p>
          <div>
            <Button
              type="button"
              variant="outline"
              disabled={!valid || status !== 'idle'}
              aria-busy={status === 'previewing'}
              onClick={() => void runPreview()}
            >
              {status === 'previewing' ? <Spinner /> : <ScanSearch aria-hidden="true" />}
              {t(status === 'previewing' ? 'embeddingRoute.previewing' : 'embeddingRoute.preview')}
            </Button>
          </div>
          {current === undefined ? null : (
            <EmbeddingRoutePreviewCard
              key={routeKey}
              preview={current}
              applying={status === 'applying'}
              onApply={() => void apply()}
            />
          )}
        </div>
      ) : null}
      {error === undefined ? null : (
        <p role="alert" className="text-[13px] text-danger">
          {error}
        </p>
      )}
      {status === 'restarting' || status === 'timedOut' ? (
        <p role="status" className="text-[13px] text-text">
          {t(
            status === 'restarting'
              ? 'embeddingRoute.restarting'
              : 'embeddingRoute.restartTimedOut',
          )}
        </p>
      ) : null}
      {status === 'restartRequired' ? (
        <div role="status" className="text-[13px] text-warning">
          {t('embeddingRoute.restartRequired')}
          <RestartAuraControl />
        </div>
      ) : null}
    </div>
  );
}
