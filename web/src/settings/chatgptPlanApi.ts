import { readJSON } from './settingsApi';

export interface ChatGPTPlanStatus {
  readonly connected: boolean;
  readonly email: string;
  readonly plan_enabled: boolean;
  readonly status: string;
  readonly error?: string;
}

function requestSignal(signal: AbortSignal): AbortSignal {
  return AbortSignal.any([signal, AbortSignal.timeout(15_000)]);
}

export async function fetchChatGPTPlanStatus(signal: AbortSignal): Promise<ChatGPTPlanStatus> {
  return readJSON<ChatGPTPlanStatus>(
    await fetch('/api/settings/chatgpt/status', {
      headers: { Accept: 'application/json' },
      credentials: 'same-origin',
      signal: requestSignal(signal),
    }),
  );
}

export async function startChatGPTPlanLogin(): Promise<{
  readonly auth_url: string;
  readonly status: string;
}> {
  return readJSON(
    await fetch('/api/settings/chatgpt/login', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      credentials: 'same-origin',
      // Retain the response after unmount: its exact route selects cleanup.
      signal: AbortSignal.timeout(60_000),
    }),
  );
}

export function isChatGPTPlanLoginRoute(route: unknown): route is string {
  return (
    typeof route === 'string' && /^\/browser\/chatgpt-[a-f0-9]{24}$/.exec(route)?.[0] === route
  );
}

export async function cancelChatGPTPlanLogin(authURL: string): Promise<void> {
  if (!isChatGPTPlanLoginRoute(authURL)) throw new Error('Invalid ChatGPT login route');
  await readJSON(
    await fetch('/api/settings/chatgpt/login', {
      method: 'DELETE',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      credentials: 'same-origin',
      body: JSON.stringify({ auth_url: authURL }),
      keepalive: true,
      signal: AbortSignal.timeout(15_000),
    }),
  );
}

export async function disconnectChatGPTPlan(signal: AbortSignal): Promise<void> {
  await readJSON(
    await fetch('/api/settings/chatgpt', {
      method: 'DELETE',
      headers: { Accept: 'application/json' },
      credentials: 'same-origin',
      signal: requestSignal(signal),
    }),
  );
}
