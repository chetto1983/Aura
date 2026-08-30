export type SettingKind = 'string' | 'bool' | 'int';

// How a row reaches the running daemon (amendment #188): a hot profile key is `live`
// as soon as it is saved; a boot-bound key is `boot` while its row matches what the
// process started with and `restart` once a save has outrun the running process.
export type SettingApplied = 'live' | 'boot' | 'restart';

export interface SettingItem {
  readonly key: string;
  readonly label: string;
  readonly kind: SettingKind;
  readonly secret: boolean;
  readonly value: string;
  readonly has_value: boolean;
  readonly overridden: boolean;
  readonly applied: SettingApplied;
  readonly updated_at?: string;
  readonly updated_by?: string;
}

export interface SettingsList {
  readonly settings: readonly SettingItem[];
  readonly restart_required: boolean;
  /** The rows behind restart_required, so the banner can name them. */
  readonly restart_keys?: readonly string[];
}

export interface TelegramAvailability {
  readonly configured: boolean;
  readonly available: boolean;
  readonly botUsername?: string;
  readonly requiresRestart: boolean;
  readonly error?: string;
}

export interface TelegramLink {
  readonly sessionToken: string;
  readonly deepLink?: string;
  readonly qrSvg?: string;
}

export interface TelegramLinkStatus {
  readonly linked: boolean;
}

async function readJSON<T>(res: Response): Promise<T> {
  if (!res.ok) {
    throw new Error(await readErrorMessage(res));
  }
  return (await res.json()) as T;
}

// The API answers a rejected write with {"error": "<why>"}, and for a validation failure
// that string is the only thing that says WHICH value was wrong and how ("must be an int",
// "unknown setting key"). Collapsing it to "HTTP 400" turned an actionable message into a
// shrug, and the panel then had nothing worth showing the operator.
async function readErrorMessage(res: Response): Promise<string> {
  try {
    const body: unknown = await res.json();
    if (body !== null && typeof body === 'object' && 'error' in body) {
      const message = (body as { readonly error?: unknown }).error;
      if (typeof message === 'string' && message.trim() !== '') return message;
    }
  } catch {
    // Not JSON at all — a proxy error page, or an empty body. The status is all there is.
  }
  return `HTTP ${String(res.status)}`;
}

export async function fetchSettings(): Promise<SettingsList> {
  const res = await fetch('/api/settings', {
    headers: { Accept: 'application/json' },
    credentials: 'same-origin',
  });
  return readJSON<SettingsList>(res);
}

export async function putSetting(key: string, value: string): Promise<SettingItem> {
  const res = await fetch(`/api/settings/${encodeURIComponent(key)}`, {
    method: 'PUT',
    headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
    credentials: 'same-origin',
    body: JSON.stringify({ value }),
  });
  return readJSON<SettingItem>(res);
}

export async function putLLMProfile(settings: Readonly<Record<string, string>>): Promise<void> {
  const res = await fetch('/api/settings/llm-profile', {
    method: 'PUT',
    headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
    credentials: 'same-origin',
    body: JSON.stringify({ settings }),
  });
  await readJSON<unknown>(res);
}

export async function deleteSetting(key: string): Promise<{
  readonly key: string;
  readonly deleted: boolean;
  readonly restart_required: boolean;
}> {
  const res = await fetch(`/api/settings/${encodeURIComponent(key)}`, {
    method: 'DELETE',
    headers: { Accept: 'application/json' },
    credentials: 'same-origin',
  });
  return readJSON(res);
}

export async function checkTelegramAvailability(token?: string): Promise<TelegramAvailability> {
  const body = token === undefined || token.trim() === '' ? {} : { token };
  const res = await fetch('/api/settings/telegram/check', {
    method: 'POST',
    headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
    credentials: 'same-origin',
    body: JSON.stringify(body),
  });
  return readJSON<TelegramAvailability>(res);
}

export async function createTelegramLink(): Promise<TelegramLink> {
  const res = await fetch('/api/settings/telegram/link', {
    method: 'POST',
    headers: { Accept: 'application/json' },
    credentials: 'same-origin',
  });
  return readJSON<TelegramLink>(res);
}

export async function fetchTelegramLinkStatus(sessionToken: string): Promise<TelegramLinkStatus> {
  const res = await fetch(`/api/settings/telegram/${encodeURIComponent(sessionToken)}/status`, {
    headers: { Accept: 'application/json' },
    credentials: 'same-origin',
  });
  return readJSON<TelegramLinkStatus>(res);
}
