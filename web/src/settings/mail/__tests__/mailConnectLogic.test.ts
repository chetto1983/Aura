import { describe, expect, it } from 'vitest';
import { accountIdFromAddress, isValidAddress, verifyMessageKey } from '../mailConnectLogic';

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
