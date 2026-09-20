import { httpErrorFrom } from '../../api/json';

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

interface RemoteAccessBase {
  readonly enabled: boolean;
  readonly api_token_set: boolean;
  readonly tunnel_token_set: boolean;
  readonly connector: string;
  readonly generation: number;
  readonly public_hostname?: string;
  readonly warp_hostname?: string;
  readonly account_id?: string;
  readonly zone_name?: string;
  readonly nameservers?: readonly string[];
  readonly last_error?: string;
  readonly last_reconciled_at?: string;
  readonly acceptance_required: boolean;
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
  readonly warp_label: string;
  /** Write-only: callers must clear their form state immediately after success. */
  readonly api_token?: string;
}

const ROUTE = '/api/settings/remote-access';

export async function readJSON<T>(res: Response): Promise<T> {
  if (!res.ok) throw await httpErrorFrom(res);
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

/** This request intentionally stays relative: when invoked it must traverse the public tunnel. */
export async function acceptExternalRemoteAccess(generation: number): Promise<void> {
  await mutation('/accept-external', 'POST', { generation });
}
