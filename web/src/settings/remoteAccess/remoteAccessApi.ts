import { HttpError, httpErrorFrom } from '../../api/json';

export type RemoteAccessPhase =
  | 'disabled'
  | 'validating'
  | 'waiting_nameservers'
  | 'provisioning'
  | 'connecting'
  | 'healthy'
  | 'degraded'
  | 'error'
  | 'deleting';

/** Go remotetunnel.Reason and the SQL CHECK, as a list: the wizard iterates every code. */
export const REMOTE_ACCESS_REASONS = [
  'token_rejected',
  'no_accounts',
  'account_not_visible',
  'domain_invalid',
  'domain_elsewhere',
  'hostname_taken',
  'not_owned',
  'no_administrator',
  'cloudflare_unreachable',
  'tunnel_down',
  'old_address_present',
  'unknown',
] as const;

/** "" means no reason. */
export type RemoteAccessReason = (typeof REMOTE_ACCESS_REASONS)[number] | '';

export interface AbandonedResource {
  readonly kind: string;
  readonly name: string;
}

interface RemoteAccessBase {
  readonly enabled: boolean;
  readonly api_token_set: boolean;
  readonly generation: number;
  readonly public_hostname?: string;
  readonly account_id?: string;
  readonly account_name?: string;
  readonly zone_name?: string;
  readonly public_label?: string;
  readonly nameservers: readonly string[];
  readonly reason: RemoteAccessReason;
  /** The phone test passed for this address. */
  readonly accepted: boolean;
  /** The tunnel is serving now. */
  readonly reachable: boolean;
  readonly last_checked_at?: string;
  readonly abandoned: readonly AbandonedResource[];
}

type PhaseStatus<P extends RemoteAccessPhase> = RemoteAccessBase & { readonly phase: P };

export type RemoteAccessStatusDTO =
  | PhaseStatus<'disabled'>
  | PhaseStatus<'validating'>
  | PhaseStatus<'waiting_nameservers'>
  | PhaseStatus<'provisioning'>
  | PhaseStatus<'connecting'>
  | PhaseStatus<'healthy'>
  | PhaseStatus<'degraded'>
  | PhaseStatus<'error'>
  | PhaseStatus<'deleting'>;

export interface CloudflareAccount {
  readonly id: string;
  readonly name: string;
}

export interface RemoteAccessConfiguration {
  readonly enabled: boolean;
  readonly generation: number;
  readonly account_id: string;
  readonly zone_name: string;
  readonly public_label: string;
  /** Write-only: callers must clear their form state immediately after success. */
  readonly api_token?: string;
}

/** A refusal with the code the wizard acts on. `code` is "" when the server named no cause: a
 * stale generation, or a body the API's error writer did not write. */
export class RemoteAccessError extends HttpError {
  readonly code: RemoteAccessReason;

  constructor(error: HttpError, code: RemoteAccessReason) {
    super(error.status, error.reason, error.detail);
    this.code = code;
  }
}

const ROUTE = '/api/settings/remote-access';

function isReason(value: unknown): value is RemoteAccessReason {
  return REMOTE_ACCESS_REASONS.some((reason) => reason === value);
}

async function bodyReason(res: Response): Promise<RemoteAccessReason> {
  try {
    const body = (await res.json()) as { readonly reason?: unknown } | null;
    return isReason(body?.reason) ? body.reason : '';
  } catch {
    return '';
  }
}

export async function readJSON<T>(res: Response): Promise<T> {
  if (!res.ok) {
    const code = await bodyReason(res.clone());
    throw new RemoteAccessError(await httpErrorFrom(res), code);
  }
  return (await res.json()) as T;
}

function mutationHeaders(): HeadersInit {
  return {
    Accept: 'application/json',
    'Content-Type': 'application/json',
    'Idempotency-Key': crypto.randomUUID(),
  };
}

async function mutation<T>(
  path: string,
  method: 'POST' | 'PUT' | 'DELETE',
  body: unknown,
): Promise<T> {
  const res = await fetch(`${ROUTE}${path}`, {
    method,
    headers: mutationHeaders(),
    credentials: 'same-origin',
    body: JSON.stringify(body),
  });
  return readJSON<T>(res);
}

export async function fetchRemoteAccess(signal?: AbortSignal): Promise<RemoteAccessStatusDTO> {
  const res = await fetch(ROUTE, {
    headers: { Accept: 'application/json' },
    credentials: 'same-origin',
    ...(signal === undefined ? {} : { signal }),
  });
  return readJSON<RemoteAccessStatusDTO>(res);
}

export async function verifyRemoteAccessToken(
  token: string,
): Promise<readonly CloudflareAccount[]> {
  const data = await mutation<{ readonly accounts?: readonly CloudflareAccount[] }>(
    '/token/verify',
    'POST',
    {
      api_token: token,
    },
  );
  return data.accounts ?? [];
}

export async function configureRemoteAccess(
  configuration: RemoteAccessConfiguration,
): Promise<void> {
  await mutation('', 'PUT', configuration);
}

export async function reconcileRemoteAccess(generation: number): Promise<void> {
  await mutation('/reconcile', 'POST', { generation });
}

export async function refreshRemoteAccessToken(generation: number): Promise<void> {
  await mutation('/token/refresh', 'POST', { generation });
}

export async function disableRemoteAccess(): Promise<void> {
  await mutation('/disable', 'POST', {});
}

export async function deleteRemoteAccess(hostname: string): Promise<void> {
  await mutation('', 'DELETE', { hostname });
}

export async function dismissAbandonedRemoteAccess(): Promise<void> {
  await mutation('/abandoned/dismiss', 'POST', {});
}

/** This request intentionally stays relative: when invoked it must traverse the public tunnel. */
export async function acceptExternalRemoteAccess(generation: number): Promise<void> {
  await mutation('/accept-external', 'POST', { generation });
}
