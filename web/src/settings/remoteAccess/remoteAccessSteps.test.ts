import { describe, expect, it } from 'vitest';
import { HttpError } from '../../api/json';
import {
  REMOTE_ACCESS_REASONS,
  RemoteAccessError,
  type RemoteAccessReason,
  type RemoteAccessStatusDTO,
} from './remoteAccessApi';
import {
  STEP_KEYS,
  TOKEN_TEMPLATE_URL,
  currentStep,
  failureOf,
  hasExit,
  leftDraft,
  reasonAction,
  reasonOn,
  reasonStep,
  stepStates,
  type StepKey,
} from './remoteAccessSteps';

type Reason = Exclude<RemoteAccessReason, ''>;

const fresh: RemoteAccessStatusDTO = {
  enabled: false,
  phase: 'disabled',
  api_token_set: false,
  generation: 1,
  nameservers: [],
  reason: '',
  accepted: false,
  reachable: false,
  has_resources: false,
  abandoned: [],
};

const saved: RemoteAccessStatusDTO = {
  ...fresh,
  api_token_set: true,
  account_id: 'account',
  account_name: 'Acme',
};

const addressed: RemoteAccessStatusDTO = {
  ...saved,
  enabled: true,
  zone_name: 'example.com',
  public_label: 'aura',
  public_hostname: 'aura.example.com',
  has_resources: true,
};

function failed(
  phase: RemoteAccessStatusDTO['phase'],
  reason: RemoteAccessReason,
): RemoteAccessStatusDTO {
  return { ...addressed, phase, reason };
}

const states = (status: RemoteAccessStatusDTO) => STEP_KEYS.map((key) => stepStates(status)[key]);

describe('currentStep', () => {
  it.each<[string, RemoteAccessStatusDTO, StepKey]>([
    ['no token', fresh, 'cloudflare'],
    ['a token with no account behind it', { ...fresh, api_token_set: true }, 'cloudflare'],
    ['token, no zone', saved, 'domain'],
    ['the row Elimina leaves', { ...addressed, enabled: false }, 'domain'],
    ['waiting_nameservers', { ...addressed, phase: 'waiting_nameservers' }, 'domain'],
    ['validating', { ...addressed, phase: 'validating' }, 'connection'],
    ['provisioning', { ...addressed, phase: 'provisioning' }, 'connection'],
    ['deleting', { ...addressed, enabled: false, phase: 'deleting' }, 'connection'],
    ['connecting, not accepted', { ...addressed, phase: 'connecting' }, 'phone'],
    ['healthy', { ...addressed, phase: 'healthy', accepted: true, reachable: true }, 'phone'],
    ['degraded + tunnel_down', failed('degraded', 'tunnel_down'), 'phone'],
    ['error + domain_elsewhere', failed('error', 'domain_elsewhere'), 'domain'],
    ['error + no_accounts', failed('error', 'no_accounts'), 'cloudflare'],
    [
      'degraded + cloudflare_unreachable',
      failed('degraded', 'cloudflare_unreachable'),
      'connection',
    ],
    [
      'deleting + cloudflare_unreachable',
      failed('deleting', 'cloudflare_unreachable'),
      'connection',
    ],
    ['an error row with no code', failed('error', ''), 'connection'],
    ['a degraded row with no code', failed('degraded', ''), 'connection'],
  ])('%s', (_name, status, expected) => {
    expect(currentStep(status)).toBe(expected);
  });

  it('asks for the token first whatever the row says, because nothing else can run without it', () => {
    expect(currentStep({ ...fresh, phase: 'error', reason: 'domain_elsewhere' })).toBe(
      'cloudflare',
    );
  });
});

describe('stepStates', () => {
  it('healthy: every step done', () => {
    const healthy = { ...addressed, phase: 'healthy', accepted: true, reachable: true } as const;
    expect(states(healthy)).toEqual(['done', 'done', 'done', 'done']);
  });

  it('a healthy row the phone test never accepted is not green yet', () => {
    expect(states({ ...addressed, phase: 'healthy', reachable: true })).toEqual([
      'done',
      'done',
      'done',
      'active',
    ]);
  });

  it('first setup: Cloudflare active, the rest upcoming', () => {
    expect(states(fresh)).toEqual(['active', 'upcoming', 'upcoming', 'upcoming']);
  });

  it('a saved token: Cloudflare done, Dominio active', () => {
    expect(states(saved)).toEqual(['done', 'active', 'upcoming', 'upcoming']);
  });

  it('provisioning: the connection is the active step', () => {
    expect(states({ ...addressed, phase: 'provisioning' })).toEqual([
      'done',
      'done',
      'active',
      'upcoming',
    ]);
  });

  it('connecting: the phone test is active, never done', () => {
    expect(states({ ...addressed, phase: 'connecting' })).toEqual([
      'done',
      'done',
      'done',
      'active',
    ]);
  });

  it('degraded + tunnel_down: phone failed', () => {
    expect(states(failed('degraded', 'tunnel_down'))).toEqual(['done', 'done', 'done', 'failed']);
  });

  it('error + domain_elsewhere: domain failed', () => {
    expect(states(failed('error', 'domain_elsewhere'))).toEqual([
      'done',
      'failed',
      'upcoming',
      'upcoming',
    ]);
  });

  it('error + no_accounts: cloudflare failed', () => {
    expect(states(failed('error', 'no_accounts'))).toEqual([
      'failed',
      'upcoming',
      'upcoming',
      'upcoming',
    ]);
  });

  it('an error row with no code still reads failed', () => {
    expect(stepStates(failed('error', '')).connection).toBe('failed');
  });

  it('a reason for a later step does not paint the step that blocks it', () => {
    expect(stepStates({ ...fresh, phase: 'error', reason: 'domain_elsewhere' }).cloudflare).toBe(
      'active',
    );
  });

  it('a healthy phase never greens a step the token still blocks', () => {
    expect(states({ ...fresh, phase: 'healthy' })).toEqual([
      'active',
      'upcoming',
      'upcoming',
      'upcoming',
    ]);
  });
});

describe('reasonStep', () => {
  it.each<[RemoteAccessReason, StepKey]>([
    ['token_rejected', 'cloudflare'],
    ['no_accounts', 'cloudflare'],
    ['account_not_visible', 'cloudflare'],
    ['domain_invalid', 'domain'],
    ['domain_elsewhere', 'domain'],
    ['hostname_taken', 'domain'],
    ['tunnel_down', 'phone'],
    ['not_owned', 'connection'],
    ['no_administrator', 'connection'],
    ['cloudflare_unreachable', 'connection'],
    ['old_address_present', 'connection'],
    ['unknown', 'connection'],
    ['', 'connection'],
  ])('%s → %s', (reason, step) => {
    expect(reasonStep(reason)).toBe(step);
  });
});

describe('reasonOn', () => {
  it('names the reason on its own step only', () => {
    const status = failed('error', 'hostname_taken');
    expect(reasonOn(status, 'domain')).toBe('hostname_taken');
    expect(reasonOn(status, 'cloudflare')).toBe('');
    expect(reasonOn(status, 'connection')).toBe('');
  });

  it('calls a failed row with no code unknown, so its step has something to say', () => {
    expect(reasonOn(failed('error', ''), 'connection')).toBe('unknown');
    expect(reasonOn(failed('degraded', ''), 'connection')).toBe('unknown');
  });

  it('has nothing to say about a row that is working', () => {
    expect(reasonOn({ ...addressed, phase: 'provisioning' }, 'connection')).toBe('');
  });

  // A tab left open across an update can hold a code this build has never heard of.
  it('calls a code this build does not know unknown', () => {
    const status = failed('error', 'brand_new_code' as RemoteAccessReason);
    expect(reasonOn(status, 'connection')).toBe('unknown');
    expect(currentStep(status)).toBe('connection');
    expect(stepStates(status).connection).toBe('failed');
  });
});

describe('reasonAction', () => {
  const FIXED_ON: readonly (readonly [Reason, StepKey])[] = [
    ['token_rejected', 'cloudflare'],
    ['no_accounts', 'cloudflare'],
    ['account_not_visible', 'cloudflare'],
    ['domain_invalid', 'domain'],
    ['domain_elsewhere', 'domain'],
    ['hostname_taken', 'domain'],
  ];
  const RETRIED: readonly Reason[] = [
    'not_owned',
    'no_administrator',
    'cloudflare_unreachable',
    'old_address_present',
    'unknown',
    'tunnel_down',
  ];

  // The ruling: a code that lands on a step with nothing to press is a defect, so a new code has to
  // land in one of the two tables below before this passes.
  it('every reason is in exactly one table', () => {
    const covered = [...FIXED_ON.map(([reason]) => reason), ...RETRIED];
    expect(covered.toSorted()).toEqual([...REMOTE_ACCESS_REASONS].toSorted());
  });

  it.each(FIXED_ON)(
    '%s is fixed in place on %s, and opens it from anywhere else',
    (reason, target) => {
      expect(reasonAction(reason, target)).toBe('fix');
      for (const step of STEP_KEYS.filter((key) => key !== target)) {
        expect(reasonAction(reason, step)).toBe(target);
      }
    },
  );

  it.each(RETRIED)('%s is retried wherever it shows', (reason) => {
    for (const step of STEP_KEYS) expect(reasonAction(reason, step)).toBe('retry');
  });
});

describe('leftDraft', () => {
  it('forgets a field the operator emptied, so a saved address can come back', () => {
    expect(leftDraft({ zone: '', label: '' })).toEqual({ zone: null, label: null });
  });

  it('keeps what was typed', () => {
    expect(leftDraft({ zone: 'nuovo.it', label: 'casa' })).toEqual({
      zone: 'nuovo.it',
      label: 'casa',
    });
  });

  it('treats each field on its own', () => {
    expect(leftDraft({ zone: 'nuovo.it', label: '' })).toEqual({ zone: 'nuovo.it', label: null });
    expect(leftDraft({ zone: null, label: 'casa' })).toEqual({ zone: null, label: 'casa' });
  });
});

describe('failureOf', () => {
  const error = (status: number, code: RemoteAccessReason) =>
    new RemoteAccessError(new HttpError(status, 'configuration changed; refresh and retry'), code);

  it('a 409 with no reason is the refresh message, whatever the sentence says', () => {
    expect(failureOf(error(409, ''))).toBe('stale');
  });

  it('a 409 that names its cause keeps it', () => {
    expect(failureOf(error(409, 'not_owned'))).toBe('not_owned');
  });

  it('a reason is the failure', () => {
    expect(failureOf(error(400, 'domain_invalid'))).toBe('domain_invalid');
    expect(failureOf(error(502, 'cloudflare_unreachable'))).toBe('cloudflare_unreachable');
  });

  it('a body with no reason is no reason, never a failure code', () => {
    expect(failureOf(error(503, ''))).toBe('generic');
    expect(failureOf(error(403, ''))).toBe('generic');
  });

  it('a request that never got an answer is generic', () => {
    expect(failureOf(new TypeError('Failed to fetch'))).toBe('generic');
  });

  it('a bare HttpError carries no code', () => {
    expect(failureOf(new HttpError(409))).toBe('stale');
    expect(failureOf(new HttpError(500))).toBe('generic');
  });
});

describe('hasExit', () => {
  it.each<[string, RemoteAccessStatusDTO, boolean]>([
    ['an error row', failed('error', 'not_owned'), true],
    ['a degraded row', failed('degraded', 'cloudflare_unreachable'), true],
    [
      'an install switched off with Disattiva',
      { ...addressed, enabled: false, accepted: true },
      true,
    ],
    ['a row switched off before it was ever accepted', { ...addressed, enabled: false }, true],
    ['the row Elimina leaves', { ...addressed, enabled: false, has_resources: false }, false],
    [
      'a healthy install, which has them on the last step',
      { ...addressed, phase: 'healthy' },
      false,
    ],
    ['a row that is already deleting', { ...addressed, enabled: false, phase: 'deleting' }, false],
    ['a failed row with no address', { ...saved, phase: 'error', reason: 'unknown' }, false],
    ['a switched-off row with no address', { ...saved, has_resources: true }, false],
  ])('%s', (_name, status, expected) => {
    expect(hasExit(status)).toBe(expected);
  });
});

describe('TOKEN_TEMPLATE_URL', () => {
  const url = new URL(TOKEN_TEMPLATE_URL);

  it('opens the user token form: Aura verifies at /user/tokens/verify', () => {
    expect(`${url.origin}${url.pathname}`).toBe('https://dash.cloudflare.com/profile/api-tokens');
    expect(url.searchParams.get('accountId')).toBe('*');
    expect(url.searchParams.get('zoneId')).toBe('all');
    expect(url.searchParams.get('name')).toBe('Aura remote access');
  });

  it('pre-fills every permission with a documented key, and no WARP permission', () => {
    expect(JSON.parse(url.searchParams.get('permissionGroupKeys') ?? '[]')).toEqual([
      { key: 'account_settings', type: 'read' },
      { key: 'zone', type: 'read' },
      { key: 'zone', type: 'edit' },
      { key: 'dns', type: 'edit' },
      { key: 'access', type: 'edit' },
      { key: 'access_acct', type: 'edit' },
    ]);
  });
});
