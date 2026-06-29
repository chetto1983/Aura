import { useCallback, useEffect, useMemo, useState } from 'react';
import type { ReactNode } from 'react';
import { Cloud, Cpu, RefreshCw, RotateCcw, Save } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Spinner } from '../components/Spinner';
import {
  deleteSetting,
  fetchSettings,
  putSetting,
  type SettingItem,
  type SettingKind,
} from './settingsApi';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { SecretInput } from '@/components/ui/secret-input';
import { cn } from '@/lib/utils';

const OPENROUTER_BASE_URL = 'https://openrouter.ai/api/v1';
const LOCAL_BASE_URL = 'http://aura-vllm-chat:8000/v1';

type SettingsKey =
  | 'AURA_LLM_MODEL'
  | 'AURA_LLM_BASE_URL'
  | 'OPENROUTER_API_KEY'
  | 'AURA_LLM_MAX_TOKENS'
  | 'AURA_MODEL_CONTEXT_WINDOW'
  | 'AURA_MODEL_MAX_OUTPUT_TOKENS'
  | 'AURA_EMBED_MODEL'
  | 'AURA_EMBED_BASE_URL'
  | 'AURA_EMBED_DIMENSIONS'
  | 'AURA_RERANK_MODEL'
  | 'AURA_RERANK_BASE_URL'
  | 'AURA_TTS_MODEL'
  | 'AURA_STT_CLOUD_MODEL'
  | 'AURA_VISION_CLOUD'
  | 'AURA_MEMORY_EMBED_BASE_URL'
  | 'AURA_MEMORY_EMBED_API_KEY';

interface SettingDef {
  readonly key: SettingsKey;
  readonly kind: SettingKind;
  readonly secret?: boolean;
  readonly labelKey: string;
  readonly placeholder?: string;
}

const PRIMARY_SETTINGS: readonly SettingDef[] = [
  {
    key: 'AURA_LLM_MODEL',
    kind: 'string',
    labelKey: 'settings.fields.primaryModel',
    placeholder: 'deepseek/deepseek-v4-flash:nitro',
  },
  {
    key: 'AURA_LLM_BASE_URL',
    kind: 'string',
    labelKey: 'settings.fields.primaryBaseUrl',
    placeholder: OPENROUTER_BASE_URL,
  },
  {
    key: 'OPENROUTER_API_KEY',
    kind: 'string',
    secret: true,
    labelKey: 'settings.fields.openRouterKey',
    placeholder: 'sk-or-...',
  },
];

const TOKEN_SETTINGS: readonly SettingDef[] = [
  {
    key: 'AURA_LLM_MAX_TOKENS',
    kind: 'int',
    labelKey: 'settings.fields.maxTokens',
    placeholder: '4096',
  },
  {
    key: 'AURA_MODEL_CONTEXT_WINDOW',
    kind: 'int',
    labelKey: 'settings.fields.contextWindow',
    placeholder: '1000000',
  },
  {
    key: 'AURA_MODEL_MAX_OUTPUT_TOKENS',
    kind: 'int',
    labelKey: 'settings.fields.maxOutputTokens',
    placeholder: '32768',
  },
];

const BACKEND_SETTINGS: readonly SettingDef[] = [
  { key: 'AURA_EMBED_BASE_URL', kind: 'string', labelKey: 'settings.fields.embedBaseUrl' },
  { key: 'AURA_EMBED_MODEL', kind: 'string', labelKey: 'settings.fields.embedModel' },
  { key: 'AURA_EMBED_DIMENSIONS', kind: 'int', labelKey: 'settings.fields.embedDimensions' },
  { key: 'AURA_RERANK_BASE_URL', kind: 'string', labelKey: 'settings.fields.rerankBaseUrl' },
  { key: 'AURA_RERANK_MODEL', kind: 'string', labelKey: 'settings.fields.rerankModel' },
  { key: 'AURA_STT_CLOUD_MODEL', kind: 'string', labelKey: 'settings.fields.sttCloudModel' },
  { key: 'AURA_TTS_MODEL', kind: 'string', labelKey: 'settings.fields.ttsModel' },
  { key: 'AURA_VISION_CLOUD', kind: 'bool', labelKey: 'settings.fields.visionCloud' },
  {
    key: 'AURA_MEMORY_EMBED_BASE_URL',
    kind: 'string',
    labelKey: 'settings.fields.memoryEmbedBaseUrl',
  },
  {
    key: 'AURA_MEMORY_EMBED_API_KEY',
    kind: 'string',
    secret: true,
    labelKey: 'settings.fields.memoryEmbedKey',
  },
];

const ALL_SETTINGS = [...PRIMARY_SETTINGS, ...TOKEN_SETTINGS, ...BACKEND_SETTINGS] as const;

interface ModelSettingsPanelProps {
  readonly className?: string;
  readonly onComplete?: () => void | Promise<void>;
  readonly saveLabel?: string;
  readonly skipLabel?: string;
}

interface LoadedState {
  readonly rows: Record<string, SettingItem>;
  readonly values: Record<string, string>;
  readonly initial: Record<string, string>;
  readonly restartRequired: boolean;
}

function emptyItem(def: SettingDef): SettingItem {
  return {
    key: def.key,
    label: def.key,
    kind: def.kind,
    secret: def.secret === true,
    value: '',
    has_value: false,
    overridden: false,
  };
}

function buildState(list: Awaited<ReturnType<typeof fetchSettings>>): LoadedState {
  const byKey = Object.fromEntries(list.settings.map((item) => [item.key, item]));
  const rows: Record<string, SettingItem> = {};
  const values: Record<string, string> = {};
  const initial: Record<string, string> = {};
  for (const def of ALL_SETTINGS) {
    const item = byKey[def.key] ?? emptyItem(def);
    rows[def.key] = item;
    const value = item.secret ? '' : item.value;
    values[def.key] = value;
    initial[def.key] = value;
  }
  return { rows, values, initial, restartRequired: list.restart_required };
}

function isLocalBaseURL(value: string): boolean {
  const normalized = value.trim().toLowerCase();
  return (
    normalized !== '' && normalized !== OPENROUTER_BASE_URL && !normalized.includes('openrouter.ai')
  );
}

function SettingsFields({
  defs,
  loaded,
  onReset,
  onValueChange,
  resetting,
}: {
  readonly defs: readonly SettingDef[];
  readonly loaded: LoadedState;
  readonly resetting: string | undefined;
  readonly onValueChange: (key: SettingsKey, value: string) => void;
  readonly onReset: (key: SettingsKey) => void;
}) {
  return (
    <SettingsGrid>
      {defs.map((def) => (
        <SettingField
          key={def.key}
          def={def}
          item={loaded.rows[def.key] ?? emptyItem(def)}
          value={loaded.values[def.key] ?? ''}
          onChange={(value) => {
            onValueChange(def.key, value);
          }}
          onReset={() => {
            onReset(def.key);
          }}
          resetting={resetting === def.key}
        />
      ))}
    </SettingsGrid>
  );
}

export function ModelSettingsPanel({
  className,
  onComplete,
  saveLabel,
  skipLabel,
}: ModelSettingsPanelProps) {
  const { t } = useTranslation();
  const [loaded, setLoaded] = useState<LoadedState | undefined>(undefined);
  const [loadStatus, setLoadStatus] = useState<'loading' | 'ready' | 'error'>('loading');
  const [saving, setSaving] = useState(false);
  const [resetting, setResetting] = useState<string | undefined>(undefined);
  const [saved, setSaved] = useState(false);

  const reload = useCallback(async () => {
    setLoadStatus('loading');
    try {
      const settings = await fetchSettings();
      setLoaded(buildState(settings));
      setLoadStatus('ready');
    } catch {
      setLoadStatus('error');
    }
  }, []);

  useEffect(() => {
    let cancelled = false;
    void fetchSettings()
      .then((settings) => {
        if (cancelled) return;
        setLoaded(buildState(settings));
        setLoadStatus('ready');
      })
      .catch(() => {
        if (cancelled) return;
        setLoadStatus('error');
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const dirtyKeys = useMemo(() => {
    if (loaded === undefined) return [];
    return ALL_SETTINGS.filter((def) => {
      const value = loaded.values[def.key] ?? '';
      if (def.secret) return value.trim() !== '';
      return value !== (loaded.initial[def.key] ?? '');
    }).map((def) => def.key);
  }, [loaded]);

  const provider = isLocalBaseURL(loaded?.values.AURA_LLM_BASE_URL ?? '') ? 'local' : 'cloud';

  function setValue(key: SettingsKey, value: string) {
    setLoaded((prev) =>
      prev === undefined
        ? prev
        : {
            ...prev,
            values: { ...prev.values, [key]: value },
          },
    );
    setSaved(false);
  }

  async function saveChanges() {
    if (loaded === undefined || saving) return;
    if (dirtyKeys.length === 0) {
      await onComplete?.();
      return;
    }
    setSaving(true);
    setSaved(false);
    try {
      for (const key of dirtyKeys) {
        await putSetting(key, loaded.values[key] ?? '');
      }
      const settings = await fetchSettings();
      setLoaded(buildState(settings));
      setSaved(true);
      await onComplete?.();
    } catch {
      setLoadStatus('error');
    } finally {
      setSaving(false);
    }
  }

  async function resetSetting(key: SettingsKey) {
    if (resetting !== undefined) return;
    setResetting(key);
    try {
      await deleteSetting(key);
      await reload();
    } finally {
      setResetting(undefined);
    }
  }

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

  return (
    <div className={cn('flex flex-col gap-7', className)}>
      <section aria-labelledby="runtime-model-routing" className="flex flex-col gap-4">
        <div className="flex flex-col gap-2">
          <h2 id="runtime-model-routing" className="text-[20px] font-semibold text-text">
            {t('settings.modelRouting.heading')}
          </h2>
          <p className="max-w-3xl text-[15.5px] leading-relaxed text-text-muted">
            {t('settings.modelRouting.body')}
          </p>
        </div>

        <div
          className="flex flex-wrap items-center gap-2"
          role="group"
          aria-label={t('settings.provider.label')}
        >
          <Button
            type="button"
            variant={provider === 'cloud' ? 'default' : 'outline'}
            aria-pressed={provider === 'cloud'}
            onClick={() => {
              setValue('AURA_LLM_BASE_URL', OPENROUTER_BASE_URL);
            }}
          >
            <Cloud aria-hidden="true" />
            {t('settings.provider.cloud')}
          </Button>
          <Button
            type="button"
            variant={provider === 'local' ? 'default' : 'outline'}
            aria-pressed={provider === 'local'}
            onClick={() => {
              setValue('AURA_LLM_BASE_URL', LOCAL_BASE_URL);
            }}
          >
            <Cpu aria-hidden="true" />
            {t('settings.provider.local')}
          </Button>
        </div>

        <SettingsFields
          defs={PRIMARY_SETTINGS}
          loaded={loaded}
          resetting={resetting}
          onValueChange={setValue}
          onReset={(key) => void resetSetting(key)}
        />
      </section>

      <section
        aria-labelledby="runtime-token-budgets"
        className="flex flex-col gap-4 border-t border-border pt-6"
      >
        <div className="flex flex-col gap-2">
          <h2 id="runtime-token-budgets" className="text-[18px] font-semibold text-text">
            {t('settings.tokens.heading')}
          </h2>
          <p className="max-w-3xl text-[15.5px] leading-relaxed text-text-muted">
            {t('settings.tokens.body')}
          </p>
        </div>
        <SettingsFields
          defs={TOKEN_SETTINGS}
          loaded={loaded}
          resetting={resetting}
          onValueChange={setValue}
          onReset={(key) => void resetSetting(key)}
        />
      </section>

      <section
        aria-labelledby="runtime-backends"
        className="flex flex-col gap-4 border-t border-border pt-6"
      >
        <div className="flex flex-col gap-2">
          <h2 id="runtime-backends" className="text-[18px] font-semibold text-text">
            {t('settings.backends.heading')}
          </h2>
          <p className="max-w-3xl text-[15.5px] leading-relaxed text-text-muted">
            {t('settings.backends.body')}
          </p>
        </div>
        <SettingsFields
          defs={BACKEND_SETTINGS}
          loaded={loaded}
          resetting={resetting}
          onValueChange={setValue}
          onReset={(key) => void resetSetting(key)}
        />
      </section>

      {loaded.restartRequired ? (
        <div
          role="note"
          className="rounded-md border border-warning bg-warning/10 px-4 py-3 text-[13px] text-warning"
        >
          {t('settings.restartRequired')}
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

      <div className="flex flex-wrap items-center gap-2 border-t border-border pt-5">
        <Button
          type="button"
          disabled={saving}
          aria-busy={saving}
          onClick={() => void saveChanges()}
        >
          {saving ? <Spinner /> : <Save aria-hidden="true" />}
          {saveButtonLabel}
        </Button>
        {onComplete !== undefined ? (
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

function SettingsGrid({ children }: { readonly children: ReactNode }) {
  return <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">{children}</div>;
}

function SettingField({
  def,
  item,
  onChange,
  onReset,
  resetting,
  value,
}: {
  readonly def: SettingDef;
  readonly item: SettingItem;
  readonly value: string;
  readonly onChange: (value: string) => void;
  readonly onReset: () => void;
  readonly resetting: boolean;
}) {
  const { t } = useTranslation();
  const inputId = `setting-${def.key}`;
  const label = t(def.labelKey);
  const status = item.has_value
    ? t('settings.status.configured')
    : t('settings.status.notConfigured');

  return (
    <div className="flex min-h-32 flex-col gap-2 rounded-md border border-border bg-surface px-3 py-3">
      <div className="flex min-w-0 items-start justify-between gap-2">
        <Label htmlFor={inputId} className="min-w-0 break-words text-[13px]">
          {label}
        </Label>
        <Badge variant={item.has_value ? 'success' : 'secondary'}>{status}</Badge>
      </div>
      {def.kind === 'bool' ? (
        <label className="flex min-h-[44px] items-center gap-3 rounded-md border border-input bg-surface-3 px-3 text-[13px] text-text">
          <input
            id={inputId}
            type="checkbox"
            checked={value === 'true'}
            onChange={(event) => {
              onChange(event.target.checked ? 'true' : 'false');
            }}
            className="size-4 accent-accent"
          />
          {t('settings.fields.enabled')}
        </label>
      ) : def.secret ? (
        <SecretInput
          id={inputId}
          value={value}
          placeholder={def.placeholder ?? t('settings.secretPlaceholder')}
          showLabel={t('secret.show', { label })}
          hideLabel={t('secret.hide', { label })}
          onChange={(event) => {
            onChange(event.target.value);
          }}
          className="font-mono text-[13px]"
        />
      ) : (
        <Input
          id={inputId}
          type={def.kind === 'int' ? 'number' : 'text'}
          inputMode={def.kind === 'int' ? 'numeric' : undefined}
          value={value}
          placeholder={def.placeholder}
          onChange={(event) => {
            onChange(event.target.value);
          }}
          className="font-mono text-[13px]"
        />
      )}
      <div className="mt-auto flex items-center justify-between gap-2">
        <code className="min-w-0 break-all text-[12px] text-text-faint">{def.key}</code>
        {item.overridden ? (
          <Button type="button" variant="ghost" size="sm" disabled={resetting} onClick={onReset}>
            {resetting ? <Spinner /> : <RotateCcw aria-hidden="true" />}
            {t('settings.actions.reset')}
          </Button>
        ) : null}
      </div>
    </div>
  );
}
