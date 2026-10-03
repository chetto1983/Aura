import { describe, expect, it } from 'vitest';
import {
  PIM_PROVIDERS,
  normalizePimAccountId,
  pimAccountIdError,
  pimInitialValues,
  pimMissingRequired,
  pimProviderById,
  pimIsManaged,
  pimSubmitConfig,
} from '../pimProviders';

// pimProviders unit tests — the provider/field schema is the contract the wizard submits to the
// aura-pim-mcp sidecar, so its validation/submit helpers get direct ground-truth coverage
// independent of the React render path.

describe('pimProviderById', () => {
  it('returns the matching provider', () => {
    expect(pimProviderById('imap').id).toBe('imap');
    expect(pimProviderById('outlook.com').id).toBe('outlook.com');
  });
  it('defaults to Google for an unknown id', () => {
    expect(pimProviderById('nope').id).toBe('google');
  });
});

describe('schema keys mirror the sidecar', () => {
  it('uses the exact provider-config keys each provider service reads', () => {
    const keys = (id: string) => pimProviderById(id).fields.map((f) => f.key);
    const appKeys = (id: string) => pimProviderById(id).appFields.map((f) => f.key);
    expect(keys('google')).toEqual([]);
    expect(appKeys('google')).toEqual(['clientId', 'clientSecret']);
    expect(appKeys('microsoft365')).toEqual(['tenantId', 'clientId']);
    expect(appKeys('outlook.com')).toEqual(['tenantId', 'clientId']);
    expect(appKeys('imap')).toEqual([]);
    expect(keys('imap')).toEqual([
      'imapHost',
      'imapPort',
      'smtpHost',
      'smtpPort',
      'username',
      'password',
    ]);
    expect(keys('ics')).toEqual(['icsUrl']);
    expect(keys('json')).toEqual(['source', 'oneDrivePath', 'authAccountId']);
  });
  it('covers exactly the sidecar KnownProviders set', () => {
    expect(PIM_PROVIDERS.map((p) => p.id).sort()).toEqual(
      ['google', 'ics', 'imap', 'json', 'microsoft365', 'outlook.com'].sort(),
    );
  });
});

describe('pimInitialValues', () => {
  it('seeds selects to their first option and text fields to empty', () => {
    const json = pimProviderById('json');
    const values = pimInitialValues(json);
    expect(values.source).toBe('onedrive');
    expect(values.oneDrivePath).toBe('');
  });
});

// The sidecar refuses, through /admin, a JSON account that reads its own disk (aura-pim-mcp
// Admin/TenantProviderConfig, measured 2026-10-03 against aura-pim-mcp:latest): offering `local`
// would hand every member a form that can only end in HTTP 400.
describe('json source', () => {
  it('offers OneDrive only, never a local file', () => {
    const json = pimProviderById('json');
    const source = json.fields.find((f) => f.key === 'source');
    expect(source?.options?.map((o) => o.value)).toEqual(['onedrive']);
    expect(json.fields.some((f) => f.key === 'filePath')).toBe(false);
  });
});

describe('pimSubmitConfig', () => {
  it('keeps only non-empty, trimmed values', () => {
    const imap = pimProviderById('imap');
    const out = pimSubmitConfig(imap, {
      imapHost: '  imap.example.com ',
      imapPort: '',
      smtpHost: 'smtp.example.com',
      smtpPort: '587',
      username: 'me',
      password: 'pw',
    });
    expect(out).toEqual({
      imapHost: 'imap.example.com',
      smtpHost: 'smtp.example.com',
      smtpPort: '587',
      username: 'me',
      password: 'pw',
    });
    expect(out.imapPort).toBeUndefined();
  });
  it('sends a json account with source onedrive and drops a blank optional field', () => {
    const json = pimProviderById('json');
    const out = pimSubmitConfig(json, {
      ...pimInitialValues(json),
      oneDrivePath: ' /Docs/cal.json ',
      authAccountId: '',
    });
    expect(out).toEqual({ source: 'onedrive', oneDrivePath: '/Docs/cal.json' });
  });
});

// The sidecar enforces AccountValidation.SlugRegex `^[a-z0-9][a-z0-9\-_]*$` and answers 400 with
// the reason. Measured 2026-08-22: an operator typing a name or an email address gets that 400, and
// the cockpit showed only "HTTP 400" — so the rule has to hold here, before the request is sent.
describe('normalizePimAccountId', () => {
  it('lowercases what the operator types', () => {
    expect(normalizePimAccountId('Davide')).toBe('davide');
    expect(normalizePimAccountId('WORK-2')).toBe('work-2');
  });
  it('leaves an already-valid slug alone', () => {
    expect(normalizePimAccountId('work')).toBe('work');
  });
});

describe('pimAccountIdError', () => {
  it('accepts the slugs the sidecar accepts', () => {
    expect(pimAccountIdError('work')).toBeNull();
    expect(pimAccountIdError('personale-2')).toBeNull();
    expect(pimAccountIdError('a_b')).toBeNull();
    expect(pimAccountIdError('9lives')).toBeNull();
  });
  it('reports an empty id as required, not as a slug violation', () => {
    expect(pimAccountIdError('')).toBe('required');
    expect(pimAccountIdError('   ')).toBe('required');
  });
  it('rejects exactly what the sidecar rejects', () => {
    expect(pimAccountIdError('dvd@gmail.com')).toBe('slug');
    expect(pimAccountIdError('work id')).toBe('slug');
    expect(pimAccountIdError('Work')).toBe('slug');
    expect(pimAccountIdError('-work')).toBe('slug');
    expect(pimAccountIdError('_work')).toBe('slug');
  });
});

describe('pimMissingRequired', () => {
  it('reports empty visible required fields only', () => {
    const imap = pimProviderById('imap');
    const empty = {
      imapHost: '',
      imapPort: '',
      smtpHost: '',
      smtpPort: '',
      username: '',
      password: '',
    };
    expect(pimMissingRequired(imap, empty)).toEqual([
      'imapHost',
      'smtpHost',
      'username',
      'password',
    ]);
    expect(
      pimMissingRequired(imap, {
        ...empty,
        imapHost: 'h',
        smtpHost: 's',
        username: 'u',
        password: 'p',
      }),
    ).toEqual([]);
  });
  it('requires the OneDrive path of a json account', () => {
    const json = pimProviderById('json');
    expect(pimMissingRequired(json, pimInitialValues(json))).toEqual(['oneDrivePath']);
    expect(pimMissingRequired(json, { source: 'onedrive', oneDrivePath: '/p' })).toEqual([]);
  });
});

describe('managed providers', () => {
  it('moves the OAuth client to app fields for exactly the three OAuth providers', () => {
    expect(PIM_PROVIDERS.filter(pimIsManaged).map((p) => p.id)).toEqual([
      'google',
      'microsoft365',
      'outlook.com',
    ]);
  });
});
