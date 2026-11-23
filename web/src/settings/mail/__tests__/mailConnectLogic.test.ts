import { describe, expect, it } from 'vitest';
import type { MailWatchResource } from '../../../governance/mailWatchApi';
import type { PimDiscovery } from '../../../governance/pimApi';
import { account } from '../../../test/mailWatchServer';
import {
  accountIdFromAddress,
  httpsUrl,
  isServerFailure,
  isValidAddress,
  mailProviderConfig,
  readStillFails,
  repairConfig,
  repairMessageKey,
  sameAddress,
  serversTyped,
  serverValuesFrom,
  smtpWarningKey,
  verifyMessageKey,
} from '../mailConnectLogic';

describe('accountIdFromAddress', () => {
  it('lowercases and turns every run of other characters into one dash', () => {
    expect(accountIdFromAddress('Mario.Rossi@Libero.it', 1)).toBe('mario-rossi-libero-it');
  });

  it('appends the attempt from the second on', () => {
    expect(accountIdFromAddress('Mario.Rossi@Libero.it', 3)).toBe('mario-rossi-libero-it-3');
    expect(accountIdFromAddress('a@b.c', 2)).toBe('a-b-c-2');
  });

  it('trims dashes at both ends', () => {
    expect(accountIdFromAddress('__a@b.c', 1)).toBe('a-b-c');
    expect(accountIdFromAddress('a@b.c__', 1)).toBe('a-b-c');
  });

  it('collapses a run of separators into one dash', () => {
    expect(accountIdFromAddress('a..b@c.d', 1)).toBe('a-b-c-d');
  });
});

describe('verifyMessageKey', () => {
  it('points auth_failed at the app password when the provider needs one', () => {
    expect(verifyMessageKey({ step: 'imap', reason: 'auth_failed' }, true)).toBe(
      'settings.mail.verify.appPassword',
    );
  });

  it('names the reason otherwise', () => {
    expect(verifyMessageKey({ step: 'imap', reason: 'auth_failed' }, false)).toBe(
      'settings.mail.verify.auth_failed',
    );
    expect(verifyMessageKey({ step: 'smtp', reason: 'tls_failed' }, true)).toBe(
      'settings.mail.verify.tls_failed',
    );
  });
});

describe('repairMessageKey', () => {
  it('says a refused password in repair may be wrong or need an app password', () => {
    expect(repairMessageKey({ step: 'imap', reason: 'auth_failed' })).toBe(
      'settings.mail.verify.authFailedRepair',
    );
  });

  it('names any other reason as the connect screen does', () => {
    expect(repairMessageKey({ step: 'imap', reason: 'mailbox_refused' })).toBe(
      'settings.mail.verify.mailbox_refused',
    );
  });
});

describe('smtpWarningKey', () => {
  it.each(['smtp_unreachable', 'tls_failed'])('has a sentence for %s', (warning) => {
    expect(smtpWarningKey(warning)).toBe(`settings.mail.verify.smtpWarning.${warning}`);
  });

  it.each([undefined, '', 'smtp_auth_failed'])('has none for %s', (warning) => {
    expect(smtpWarningKey(warning)).toBeNull();
  });
});

describe('repairConfig', () => {
  const blank = { imapHost: '', imapPort: '', smtpHost: '', smtpPort: '' };

  it('sends the password verbatim and nothing else when no server field is typed', () => {
    expect(repairConfig(blank, '  pass word ')).toEqual({ password: '  pass word ' });
  });

  it('adds only the server fields typed in, trimmed', () => {
    expect(
      repairConfig({ ...blank, imapHost: ' imap.new.example ', smtpPort: '465' }, 'pw'),
    ).toEqual({ imapHost: 'imap.new.example', smtpPort: '465', password: 'pw' });
  });

  it('sends nothing without a password', () => {
    expect(repairConfig(blank, '')).toBeNull();
    expect(repairConfig({ ...blank, imapHost: 'imap.new.example' }, '')).toBeNull();
  });
});

describe('serversTyped', () => {
  it('is false while every server field is blank', () => {
    expect(serversTyped({ imapHost: ' ', imapPort: '', smtpHost: '', smtpPort: '' })).toBe(false);
  });

  it.each(['imapHost', 'imapPort', 'smtpHost', 'smtpPort'] as const)('is true for %s', (key) => {
    const servers = { imapHost: '', imapPort: '', smtpHost: '', smtpPort: '', [key]: 'x' };
    expect(serversTyped(servers)).toBe(true);
  });
});

describe('readStillFails', () => {
  // The next run the status showed right after the login: both sides of the comparison are the
  // server's clock.
  const baseline = '2026-11-23T12:00:00Z';
  const failing = {
    resource: 'mail',
    status: 'failing',
    last_error: 'provider_transient',
    failing_since: '2026-11-23T02:00:00Z',
    advanced_at: null,
  } as const;
  const watch = (next: string | null, resources: readonly MailWatchResource[]) =>
    account({ next_run_at: next, resources });

  it('is true once a run after the baseline still failed', () => {
    expect(readStillFails(watch('2026-11-23T12:05:00Z', [failing]), baseline)).toBe(true);
  });

  it('waits for a run later than the baseline', () => {
    expect(readStillFails(watch(baseline, [failing]), baseline)).toBe(false);
    expect(readStillFails(watch('2026-11-23T11:59:00Z', [failing]), baseline)).toBe(false);
    expect(readStillFails(watch(null, [failing]), baseline)).toBe(false);
  });

  it('does not judge a paused watch, whose next run a repair may never move', () => {
    const paused = { ...watch('2026-11-23T12:05:00Z', [failing]), paused: true };
    expect(readStillFails(paused, baseline)).toBe(false);
  });

  it('is false when reading works again', () => {
    const { last_error: _cleared, ...rest } = failing;
    const ok = { ...rest, status: 'ok' } as const;
    expect(readStillFails(watch('2026-11-23T12:05:00Z', [ok]), baseline)).toBe(false);
  });

  it('counts only a failing resource: a refused permission is not a reading that fails', () => {
    const refused = { ...failing, status: 'unsupported', last_error: 'permission_denied' } as const;
    expect(readStillFails(watch('2026-11-23T12:05:00Z', [refused]), baseline)).toBe(false);
  });

  it('does not judge without a baseline or a watch', () => {
    expect(readStillFails(watch('2026-11-23T12:05:00Z', [failing]), null)).toBe(false);
    expect(readStillFails(undefined, baseline)).toBe(false);
  });
});

describe('isValidAddress', () => {
  it.each(['a@example.com', ' a.b@example.org ', 'a+b@sub.example.com'])('accepts %s', (v) => {
    expect(isValidAddress(v)).toBe(true);
  });

  it.each(['', 'a', 'a@', '@example.com', 'a@example', 'a b@example.com', 'a@@example.com'])(
    'rejects %j',
    (v) => {
      expect(isValidAddress(v)).toBe(false);
    },
  );
});

describe('isServerFailure', () => {
  it('puts every SMTP failure and an unreachable or untrusted IMAP server on the servers', () => {
    expect(isServerFailure({ step: 'smtp', reason: 'smtp_auth_failed' })).toBe(true);
    expect(isServerFailure({ step: 'smtp', reason: 'smtp_unreachable' })).toBe(true);
    expect(isServerFailure({ step: 'imap', reason: 'unreachable' })).toBe(true);
    expect(isServerFailure({ step: 'imap', reason: 'tls_failed' })).toBe(true);
  });

  it('puts a refused IMAP login or inbox on the password', () => {
    expect(isServerFailure({ step: 'imap', reason: 'auth_failed' })).toBe(false);
    expect(isServerFailure({ step: 'imap', reason: 'mailbox_refused' })).toBe(false);
  });
});

describe('sameAddress', () => {
  it('ignores case and surrounding space', () => {
    expect(sameAddress(' Mario@Libero.it', 'mario@libero.it ')).toBe(true);
    expect(sameAddress('mario@libero.it', 'maria@libero.it')).toBe(false);
  });
});

const DISCOVERED: PimDiscovery = {
  kind: 'imap',
  imapHost: 'imap.example.com',
  imapPort: 993,
  imapSecurity: 'ssl',
  smtpHost: 'smtp.example.com',
  smtpPort: 587,
  smtpSecurity: 'starttls',
  username: 'me',
};

describe('serverValuesFrom', () => {
  it('copies what discovery found, ports as text', () => {
    expect(serverValuesFrom(DISCOVERED, 'me@example.com')).toEqual({
      imapHost: 'imap.example.com',
      imapPort: '993',
      smtpHost: 'smtp.example.com',
      smtpPort: '587',
      username: 'me',
    });
  });

  it('leaves an unknown domain blank and uses the address as the username', () => {
    expect(serverValuesFrom({ kind: 'unknown' }, 'me@example.com')).toEqual({
      imapHost: '',
      imapPort: '',
      smtpHost: '',
      smtpPort: '',
      username: 'me@example.com',
    });
  });
});

describe('mailProviderConfig', () => {
  const servers = serverValuesFrom(DISCOVERED, 'me@example.com');

  it('sends the servers, their discovered security and the password verbatim', () => {
    expect(
      mailProviderConfig({ ...servers, imapHost: ' imap.example.com ' }, ' pw ', DISCOVERED),
    ).toEqual({
      imapHost: 'imap.example.com',
      imapPort: '993',
      imapSecurity: 'ssl',
      smtpHost: 'smtp.example.com',
      smtpPort: '587',
      smtpSecurity: 'starttls',
      username: 'me',
      password: ' pw ',
    });
  });

  it('drops a blank field and the security of a port the person changed', () => {
    const config = mailProviderConfig(
      { ...servers, imapPort: '143', smtpPort: '' },
      'pw',
      DISCOVERED,
    );
    expect(config).not.toHaveProperty('imapSecurity');
    expect(config).not.toHaveProperty('smtpSecurity');
    expect(config).not.toHaveProperty('smtpPort');
    expect(config.imapPort).toBe('143');
  });

  it('sends no security when discovery found none', () => {
    const config = mailProviderConfig(
      serverValuesFrom({ kind: 'unknown' }, 'me@example.com'),
      'pw',
      { kind: 'unknown' },
    );
    expect(config).toEqual({ username: 'me@example.com', password: 'pw' });
    expect(config).not.toHaveProperty('imapSecurity');
    expect(config).not.toHaveProperty('smtpSecurity');
  });
});

describe('httpsUrl', () => {
  it('keeps an https link only', () => {
    expect(httpsUrl('https://help.example.com/x')).toBe('https://help.example.com/x');
    expect(httpsUrl('http://help.example.com/x')).toBeNull();
    expect(httpsUrl('javascript:alert(1)')).toBeNull();
    expect(httpsUrl('not a url')).toBeNull();
    expect(httpsUrl(undefined)).toBeNull();
  });
});
