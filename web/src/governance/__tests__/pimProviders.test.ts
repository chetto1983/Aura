import { describe, expect, it } from 'vitest';
import {
  PIM_PROVIDERS,
  normalizePimAccountId,
  pimAccountIdError,
  pimFieldVisible,
  pimInitialValues,
  pimMissingRequired,
  pimProviderById,
  pimSubmitConfig,
} from '../pimProviders';

// pimProviders unit tests — the provider/field schema is the contract the wizard submits to the
// aura-pim-mcp sidecar, so its visibility/validation/submit helpers get direct ground-truth coverage
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

describe('schema keys mirror the sidecar (lowercase, case-sensitive reads)', () => {
  it('uses the exact provider-config keys each provider service reads', () => {
    const keys = (id: string) => pimProviderById(id).fields.map((f) => f.key);
    expect(keys('google')).toEqual(['clientId', 'clientSecret']);
    expect(keys('microsoft365')).toEqual(['tenantId', 'clientId']);
    expect(keys('outlook.com')).toEqual(['tenantId', 'clientId']);
    expect(keys('imap')).toEqual([
      'imapHost',
      'imapPort',
      'smtpHost',
      'smtpPort',
      'username',
      'password',
    ]);
    expect(keys('ics')).toEqual(['icsUrl']);
    expect(keys('json')).toEqual(['source', 'filePath', 'oneDrivePath', 'authAccountId']);
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
    expect(values.source).toBe('local');
    expect(values.filePath).toBe('');
  });
});

describe('pimFieldVisible', () => {
  it('hides a showIf field until its gate matches', () => {
    const json = pimProviderById('json');
    const filePath = json.fields.find((f) => f.key === 'filePath');
    const oneDrivePath = json.fields.find((f) => f.key === 'oneDrivePath');
    if (filePath === undefined || oneDrivePath === undefined) throw new Error('missing field');

    expect(pimFieldVisible(filePath, { source: 'local' })).toBe(true);
    expect(pimFieldVisible(filePath, { source: 'onedrive' })).toBe(false);
    expect(pimFieldVisible(oneDrivePath, { source: 'onedrive' })).toBe(true);
  });
  it('always shows a field with no showIf', () => {
    const icsUrl = pimProviderById('ics').fields[0];
    if (icsUrl === undefined) throw new Error('missing ics field');
    expect(pimFieldVisible(icsUrl, {})).toBe(true);
  });
});

describe('pimSubmitConfig', () => {
  it('keeps only visible, non-empty, trimmed values', () => {
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
  it('drops hidden branch fields (json onedrive path omitted when source=local)', () => {
    const json = pimProviderById('json');
    const out = pimSubmitConfig(json, {
      source: 'local',
      filePath: '/tmp/cal.json',
      oneDrivePath: '/Docs/cal.json',
      authAccountId: 'ms',
    });
    expect(out).toEqual({ source: 'local', filePath: '/tmp/cal.json' });
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
    const google = pimProviderById('google');
    expect(pimMissingRequired(google, { clientId: '', clientSecret: '' })).toEqual([
      'clientId',
      'clientSecret',
    ]);
    expect(pimMissingRequired(google, { clientId: 'x', clientSecret: 'y' })).toEqual([]);
  });
  it('does not require a hidden branch field', () => {
    const json = pimProviderById('json');
    // source=local → oneDrivePath is hidden, so it is NOT required even though required:true.
    expect(pimMissingRequired(json, { source: 'local', filePath: '/p' })).toEqual([]);
  });
});
