export type SettingKind = 'string' | 'bool' | 'int';

export interface SettingItem {
  readonly key: string;
  readonly label: string;
  readonly kind: SettingKind;
  readonly secret: boolean;
  readonly value: string;
  readonly has_value: boolean;
  readonly overridden: boolean;
  readonly updated_at?: string;
  readonly updated_by?: string;
}

export interface SettingsList {
  readonly settings: readonly SettingItem[];
  readonly restart_required: boolean;
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
    throw new Error(`HTTP ${String(res.status)}`);
  }
  return (await res.json()) as T;
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
