import { HttpError } from '../../api/json';
import type { StepState } from '../../components/StepStrip';
import {
  RemoteAccessError,
  type RemoteAccessReason,
  type RemoteAccessStatusDTO,
} from './remoteAccessApi';

// The wizard's PURE logic: which step a status belongs to, how each step reads on the strip, and
// what a reason's button does. No React here, so every branch is directly testable.

export const STEP_KEYS = ['cloudflare', 'domain', 'connection', 'phone'] as const;

export type StepKey = (typeof STEP_KEYS)[number];

/** What went wrong with a press: a reason code, a stale generation, or nothing the server named. */
export type Failure = Exclude<RemoteAccessReason, ''> | 'stale' | 'generic';

// developers.cloudflare.com/fundamentals/api/how-to/account-owned-token-template/ documents these
// keys. It must be the user-token form, because VerifyToken calls /user/tokens/verify; that form
// takes short keys only and silently drops any it cannot match. Cloudflare Tunnel Edit has no
// documented key, so CloudflareStep asks the operator to add it by hand.
const TOKEN_PERMISSIONS = [
  { key: 'account_settings', type: 'read' },
  { key: 'zone', type: 'read' },
  { key: 'zone', type: 'edit' },
  { key: 'dns', type: 'edit' },
  { key: 'access', type: 'edit' },
  { key: 'access_acct', type: 'edit' },
];

export const TOKEN_TEMPLATE_URL = `https://dash.cloudflare.com/profile/api-tokens?permissionGroupKeys=${encodeURIComponent(
  JSON.stringify(TOKEN_PERMISSIONS),
)}&accountId=%2A&zoneId=all&name=${encodeURIComponent('Aura remote access')}`;

const REASON_STEP: Partial<Record<RemoteAccessReason, StepKey>> = {
  token_rejected: 'cloudflare',
  no_accounts: 'cloudflare',
  account_not_visible: 'cloudflare',
  domain_invalid: 'domain',
  domain_elsewhere: 'domain',
  hostname_taken: 'domain',
  tunnel_down: 'phone',
};

export function reasonStep(reason: RemoteAccessReason): StepKey {
  return REASON_STEP[reason] ?? 'connection';
}

/** A failed row with no code still failed: older rows carry none, and its step must say so. */
function rowReason(status: RemoteAccessStatusDTO): RemoteAccessReason {
  if (status.reason !== '') return status.reason;
  return status.phase === 'error' || status.phase === 'degraded' ? 'unknown' : '';
}

/** The row's reason when it belongs to this step, or "". */
export function reasonOn(status: RemoteAccessStatusDTO, step: StepKey): RemoteAccessReason {
  const reason = rowReason(status);
  return reason !== '' && reasonStep(reason) === step ? reason : '';
}

export function hasCredentials(status: RemoteAccessStatusDTO): boolean {
  return status.api_token_set && status.account_id !== undefined;
}

export function currentStep(status: RemoteAccessStatusDTO): StepKey {
  if (!hasCredentials(status)) return 'cloudflare';
  const reason = rowReason(status);
  if (reason !== '') return reasonStep(reason);
  switch (status.phase) {
    case 'disabled':
    case 'waiting_nameservers':
      return 'domain';
    case 'validating':
    case 'provisioning':
    case 'deleting':
      return 'connection';
    default:
      return 'phone';
  }
}

export function stepStates(status: RemoteAccessStatusDTO): Record<StepKey, StepState> {
  const current = currentStep(status);
  const at = STEP_KEYS.indexOf(current);
  const here: StepState =
    current === 'phone' && status.phase === 'healthy'
      ? 'done'
      : reasonOn(status, current) === ''
        ? 'active'
        : 'failed';
  const state = (step: StepKey): StepState => {
    const index = STEP_KEYS.indexOf(step);
    if (index === at) return here;
    return index < at ? 'done' : 'upcoming';
  };
  return {
    cloudflare: state('cloudflare'),
    domain: state('domain'),
    connection: state('connection'),
    phone: state('phone'),
  };
}

/** What a reason's button does on a step: fix the field here, send the work again, or open the
 * step that can fix it. Connection and phone reasons name nothing the operator can retype, so
 * they are retried wherever they show. */
export function reasonAction(
  reason: Exclude<RemoteAccessReason, ''>,
  step: StepKey,
): 'fix' | 'retry' | StepKey {
  const target = reasonStep(reason);
  if (target === 'connection' || target === 'phone') return 'retry';
  return target === step ? 'fix' : target;
}

/** A 409 with no reason is a stale generation. One that names its cause keeps it: Delete answers
 * 409 not_owned, and the refresh message would send the operator round in circles. */
export function failureOf(err: unknown): Failure {
  const code = err instanceof RemoteAccessError ? err.code : '';
  if (code !== '') return code;
  return err instanceof HttpError && err.status === 409 ? 'stale' : 'generic';
}

/** Disattiva and Elimina… live on the last step, which a failed or switched-off row never
 * reaches. Delete clears the acceptance, so a disabled row that keeps it still has resources. */
export function hasExit(status: RemoteAccessStatusDTO): boolean {
  if (status.public_hostname === undefined) return false;
  return (
    status.phase === 'error' ||
    status.phase === 'degraded' ||
    (status.phase === 'disabled' && status.accepted)
  );
}
