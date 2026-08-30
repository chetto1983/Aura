import type { SettingKind } from './settingsApi';

export const OPENROUTER_BASE_URL = 'https://openrouter.ai/api/v1';
export const OPENROUTER_MODEL = 'deepseek/deepseek-v4-flash:nitro';
// The local chat server, the `localllm` compose profile, not the vLLM sidecar this
// used to name:
// `aura-vllm-chat` resolves nowhere in any compose file, so the Local button wrote a
// base URL pointing at a host that does not exist.
export const LOCAL_BASE_URL = 'http://aura-llm:8084/v1';
export const LOCAL_MODEL = 'gemma-4-12b';
export const OLLAMA_BASE_URL = 'http://host.docker.internal:11434/v1';
export const OLLAMA_MODEL = 'gemma4:31b-cloud';
export const CLOUD_PROVIDER = 'openrouter';
export const LOCAL_PROVIDER = 'llamacpp';
export const OLLAMA_PROVIDER = 'ollama';

export type SettingsKey =
  | 'AURA_LLM_MODEL'
  | 'AURA_LLM_BASE_URL'
  | 'AURA_LLM_PROVIDER'
  | 'OPENROUTER_API_KEY'
  | 'AURA_LLM_MAX_TOKENS'
  | 'AURA_MODEL_CONTEXT_WINDOW'
  | 'AURA_CONTEXT_COMPACTION_TRIGGER_PERCENT'
  | 'AURA_MODEL_MAX_OUTPUT_TOKENS'
  | 'AURA_EMBED_MODEL'
  | 'AURA_EMBED_BASE_URL'
  | 'AURA_EMBED_DIMENSIONS'
  | 'AURA_TTS_MODEL'
  | 'AURA_STT_CLOUD_MODEL'
  | 'AURA_VISION_CLOUD';

export interface SettingDef {
  readonly key: SettingsKey;
  readonly kind: SettingKind;
  readonly secret?: boolean;
  readonly labelKey: string;
  readonly placeholder?: string;
}

export const PRIMARY_SETTINGS: readonly SettingDef[] = [
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

// AURA_LLM_PROVIDER is loaded and saved like any other row but never rendered: the
// Route buttons are its editor. It has to be here: provider, base URL and model
// form the hot runtime route, so moving the URL without its provider would publish the
// previous adapter against the new endpoint (live 2026-07-27: llama.cpp pointed at
// OpenRouter).
export const ROUTING_SETTINGS: readonly SettingDef[] = [
  { key: 'AURA_LLM_PROVIDER', kind: 'string', labelKey: 'settings.fields.primaryProvider' },
];

export const TOKEN_SETTINGS: readonly SettingDef[] = [
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
  // A percentage, not a token count: it is the share of the window a conversation may
  // spend replaying itself verbatim before the older rounds are condensed. Empty means
  // the backend default (50).
  {
    key: 'AURA_CONTEXT_COMPACTION_TRIGGER_PERCENT',
    kind: 'int',
    labelKey: 'settings.fields.compactionTrigger',
    placeholder: '50',
  },
];

export const BACKEND_SETTINGS: readonly SettingDef[] = [
  { key: 'AURA_EMBED_BASE_URL', kind: 'string', labelKey: 'settings.fields.embedBaseUrl' },
  { key: 'AURA_EMBED_MODEL', kind: 'string', labelKey: 'settings.fields.embedModel' },
  { key: 'AURA_EMBED_DIMENSIONS', kind: 'int', labelKey: 'settings.fields.embedDimensions' },
  { key: 'AURA_STT_CLOUD_MODEL', kind: 'string', labelKey: 'settings.fields.sttCloudModel' },
  { key: 'AURA_TTS_MODEL', kind: 'string', labelKey: 'settings.fields.ttsModel' },
  { key: 'AURA_VISION_CLOUD', kind: 'bool', labelKey: 'settings.fields.visionCloud' },
];

export const ALL_SETTINGS: readonly SettingDef[] = [
  ...PRIMARY_SETTINGS,
  ...ROUTING_SETTINGS,
  ...TOKEN_SETTINGS,
  ...BACKEND_SETTINGS,
];

function isLocalBaseURL(value: string): boolean {
  const normalized = value.trim().toLowerCase();
  return (
    normalized !== '' && normalized !== OPENROUTER_BASE_URL && !normalized.includes('openrouter.ai')
  );
}

function isOllamaBaseURL(value: string): boolean {
  try {
    const parsed = new URL(value.trim());
    return parsed.port === '11434' || parsed.hostname.toLowerCase() === 'ollama';
  } catch {
    return false;
  }
}

// resolveProvider decides which routing button reads as active. The stored provider is
// authoritative because it is what the daemon actually boots with; the URL heuristic is
// only the fallback for a deployment whose row was never written.
export function resolveProvider(
  storedProvider: string,
  baseURL: string,
): 'cloud' | 'local' | 'ollama' {
  const normalized = storedProvider.trim().toLowerCase();
  if (normalized === OLLAMA_PROVIDER) return 'ollama';
  if (normalized === LOCAL_PROVIDER) return 'local';
  if (normalized === CLOUD_PROVIDER) return 'cloud';
  if (isOllamaBaseURL(baseURL)) return 'ollama';
  return isLocalBaseURL(baseURL) ? 'local' : 'cloud';
}

// A group is one settings pane: the fields it renders plus any key it must dirty-track
// without rendering. Route buttons edit AURA_LLM_PROVIDER, so a routing pane that did
// not track it would save a base URL and leave the
// previous provider's adapter serving the new endpoint.
export type ModelSettingsGroup = 'routing' | 'tokens' | 'backends';

export interface ModelSettingsGroupDef {
  readonly id: ModelSettingsGroup;
  readonly labelId: string;
  readonly headingKey: string;
  readonly bodyKey: string;
  readonly fields: readonly SettingDef[];
  readonly tracked: readonly SettingDef[];
}

export const MODEL_SETTINGS_GROUPS: readonly ModelSettingsGroupDef[] = [
  {
    id: 'routing',
    labelId: 'runtime-model-routing',
    headingKey: 'settings.modelRouting.heading',
    bodyKey: 'settings.modelRouting.body',
    fields: PRIMARY_SETTINGS,
    tracked: ROUTING_SETTINGS,
  },
  {
    id: 'tokens',
    labelId: 'runtime-token-budgets',
    headingKey: 'settings.tokens.heading',
    bodyKey: 'settings.tokens.body',
    fields: TOKEN_SETTINGS,
    tracked: [],
  },
  {
    id: 'backends',
    labelId: 'runtime-backends',
    headingKey: 'settings.backends.heading',
    bodyKey: 'settings.backends.body',
    fields: BACKEND_SETTINGS,
    tracked: [],
  },
];

export const ALL_MODEL_GROUPS: readonly ModelSettingsGroup[] = MODEL_SETTINGS_GROUPS.map(
  (group) => group.id,
);
