import { useCallback, useEffect, useMemo, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { REASONING_CAPABILITIES_QUERY_KEY } from '../chat/composer/useReasoningCapabilities';
import {
  deleteSetting,
  fetchSettings,
  putLLMProfile,
  putSetting,
  type SettingItem,
} from './settingsApi';
import { ALL_SETTINGS, type SettingDef, type SettingsKey } from './modelSettingsDefs';

export interface LoadedState {
  readonly rows: Record<string, SettingItem>;
  readonly values: Record<string, string>;
  readonly initial: Record<string, string>;
  readonly restartRequired: boolean;
  /** The keys behind restartRequired (amendment #188); empty on an older daemon. */
  readonly restartKeys: readonly string[];
}

export type LoadStatus = 'loading' | 'ready' | 'error';

// Mirrors the daemon's hotLLMProfileKeys (internal/agui/settings_api.go): these rows go
// through PUT /api/settings/llm-profile in one prepare→persist→publish batch.
const HOT_LLM_PROFILE_KEYS = new Set<SettingsKey>([
  'AURA_LLM_PROVIDER',
  'AURA_LLM_BASE_URL',
  'AURA_LLM_MODEL',
  'AURA_LLM_MAX_TOKENS',
  'AURA_MODEL_CONTEXT_WINDOW',
  'AURA_MODEL_MAX_OUTPUT_TOKENS',
  'AURA_CONTEXT_COMPACTION_TRIGGER_PERCENT',
  'OPENROUTER_API_KEY',
  'AURA_LOOP_MAX_STEPS',
  'AURA_LOOP_MAX_WALLCLOCK_SEC',
]);

function emptyItem(def: SettingDef): SettingItem {
  return {
    key: def.key,
    label: def.key,
    kind: def.kind,
    secret: def.secret === true,
    value: '',
    has_value: false,
    overridden: false,
    applied: HOT_LLM_PROFILE_KEYS.has(def.key) ? 'live' : 'boot',
  };
}

export function settingRow(loaded: LoadedState, def: SettingDef): SettingItem {
  return loaded.rows[def.key] ?? emptyItem(def);
}

// The state always holds every key, not just the rendered subset: a pane scoped to one group
// still needs the whole row set so `resolveProvider` can read the base URL the routing pane
// owns, and so a reload after a reset repopulates untouched panes consistently.
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
  return {
    rows,
    values,
    initial,
    restartRequired: list.restart_required,
    restartKeys: list.restart_keys ?? [],
  };
}

// errorMessage pulls the human part out of whatever the API layer threw. settingsApi
// already unwraps the server's {"error": "..."} body, so for a rejected write this is the
// validation reason itself ("must be an int") rather than a status code.
function errorMessage(err: unknown): string {
  if (err instanceof Error && err.message.trim() !== '') return err.message;
  return String(err);
}

export interface ModelSettingsState {
  readonly loaded: LoadedState | undefined;
  readonly loadStatus: LoadStatus;
  readonly saving: boolean;
  readonly resetting: string | undefined;
  readonly saved: boolean;
  readonly saveError: string | undefined;
  readonly dirtyKeys: readonly SettingsKey[];
  readonly reload: () => Promise<void>;
  readonly setValue: (key: SettingsKey, value: string) => void;
  readonly save: (onComplete?: () => void | Promise<void>) => Promise<void>;
  readonly resetSetting: (key: SettingsKey) => Promise<void>;
}

// useModelSettings owns the load/dirty/save/reset machinery for one settings pane. `scope` is
// the set of keys THIS pane may write: a pane that showed only the token budgets must not push
// the backend rows it happens to be holding in state.
export function useModelSettings(scope: readonly SettingDef[]): ModelSettingsState {
  const queryClient = useQueryClient();
  const [loaded, setLoaded] = useState<LoadedState | undefined>(undefined);
  const [loadStatus, setLoadStatus] = useState<LoadStatus>('loading');
  const [saving, setSaving] = useState(false);
  const [resetting, setResetting] = useState<string | undefined>(undefined);
  const [saved, setSaved] = useState(false);
  // Separate from loadStatus on purpose — a save that fails is not a load that failed.
  const [saveError, setSaveError] = useState<string | undefined>(undefined);

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
    return scope
      .filter((def) => {
        const value = loaded.values[def.key] ?? '';
        // A secret never round-trips its stored value, so "unchanged" is indistinguishable
        // from "empty": only a non-empty box counts as an edit.
        if (def.secret) return value.trim() !== '';
        return value !== (loaded.initial[def.key] ?? '');
      })
      .map((def) => def.key);
  }, [loaded, scope]);

  const setValue = useCallback((key: SettingsKey, value: string) => {
    setLoaded((prev) =>
      prev === undefined ? prev : { ...prev, values: { ...prev.values, [key]: value } },
    );
    setSaved(false);
    setSaveError(undefined);
  }, []);

  const save = useCallback(
    async (onComplete?: () => void | Promise<void>) => {
      if (loaded === undefined || saving) return;
      if (dirtyKeys.length === 0) {
        await onComplete?.();
        return;
      }
      setSaving(true);
      setSaved(false);
      setSaveError(undefined);
      try {
        const profileKeys = dirtyKeys.filter((key) => HOT_LLM_PROFILE_KEYS.has(key));
        if (profileKeys.length > 0) {
          await putLLMProfile(
            Object.fromEntries(profileKeys.map((key) => [key, loaded.values[key] ?? ''])),
          );
          await Promise.all([
            queryClient.invalidateQueries({ queryKey: ['me'] }),
            queryClient.invalidateQueries({ queryKey: REASONING_CAPABILITIES_QUERY_KEY }),
          ]);
        }
        for (const key of dirtyKeys.filter((key) => !HOT_LLM_PROFILE_KEYS.has(key))) {
          await putSetting(key, loaded.values[key] ?? '');
        }
        const settings = await fetchSettings();
        setLoaded(buildState(settings));
        setSaved(true);
        await onComplete?.();
      } catch (err) {
        // Deliberately NOT setLoadStatus('error'): that swaps the whole panel for the
        // "couldn't LOAD settings" alert, which throws away everything the operator just
        // typed and blames the wrong operation. A failed save leaves the form standing, with
        // the edits intact and the server's reason next to the button that failed.
        setSaveError(errorMessage(err));
      } finally {
        setSaving(false);
      }
    },
    [dirtyKeys, loaded, queryClient, saving],
  );

  const resetSetting = useCallback(
    async (key: SettingsKey) => {
      if (resetting !== undefined) return;
      setResetting(key);
      setSaveError(undefined);
      try {
        await deleteSetting(key);
        if (HOT_LLM_PROFILE_KEYS.has(key)) {
          await Promise.all([
            queryClient.invalidateQueries({ queryKey: ['me'] }),
            queryClient.invalidateQueries({ queryKey: REASONING_CAPABILITIES_QUERY_KEY }),
          ]);
        }
        await reload();
      } catch (err) {
        // Reset had no error handling at all: a rejected delete only cleared the spinner, so
        // the row went back to looking normal while still holding the old override.
        setSaveError(errorMessage(err));
      } finally {
        setResetting(undefined);
      }
    },
    [queryClient, reload, resetting],
  );

  return {
    loaded,
    loadStatus,
    saving,
    resetting,
    saved,
    saveError,
    dirtyKeys,
    reload,
    setValue,
    save,
    resetSetting,
  };
}
