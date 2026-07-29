import type { SettingKind } from './settingsApi';

export const OPENROUTER_BASE_URL = 'https://openrouter.ai/api/v1';
// The local chat server, the `localllm` compose profile, not the vLLM sidecar this
// used to name:
// `aura-vllm-chat` resolves nowhere in any compose file, so the Local button wrote a
// base URL pointing at a host that does not exist.
export const LOCAL_BASE_URL = 'http://aura-llm:8084/v1';
export const CLOUD_PROVIDER = 'openrouter';
export const LOCAL_PROVIDER = 'llamacpp';

export type SettingsKey =
  | 'AURA_LLM_MODEL'
  | 'AURA_LLM_BASE_URL'
  | 'AURA_LLM_PROVIDER'
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
// Cloud/Local buttons are its editor. It has to be here — settings rows overlay the
// process env at daemon boot and outrank both .env and ~/.aura/llm.json, so a panel
// that moves the base URL without it leaves the previous provider's adapter serving
// the new endpoint (live 2026-07-27: the llama.cpp adapter pointed at OpenRouter).
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
];

export const BACKEND_SETTINGS: readonly SettingDef[] = [
  { key: 'AURA_EMBED_BASE_URL', kind: 'string', labelKey: 'settings.fields.embedBaseUrl' },
  { key: 'AURA_EMBED_MODEL', kind: 'string', labelKey: 'settings.fields.embedModel' },
  { key: 'AURA_EMBED_DIMENSIONS', kind: 'int', labelKey: 'settings.fields.embedDimensions' },
  { key: 'AURA_RERANK_BASE_URL', kind: 'string', labelKey: 'settings.fields.rerankBaseUrl' },
  { key: 'AURA_RERANK_MODEL', kind: 'string', labelKey: 'settings.fields.rerankModel' },
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

// resolveProvider decides which routing button reads as active. The stored provider is
// authoritative because it is what the daemon actually boots with; the URL heuristic is
// only the fallback for a deployment whose row was never written.
export function resolveProvider(storedProvider: string, baseURL: string): 'cloud' | 'local' {
  const normalized = storedProvider.trim().toLowerCase();
  if (normalized === LOCAL_PROVIDER) return 'local';
  if (normalized === CLOUD_PROVIDER) return 'cloud';
  return isLocalBaseURL(baseURL) ? 'local' : 'cloud';
}
