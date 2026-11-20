// Pure rules of the mailbox connect flow: the account id an address maps to, which sentence a failed
// check gets, and whether an address is worth sending to the PIM.

import type { PimVerifyReason } from '../../governance/pimApi';

/** The id an address becomes: lowercase, every run outside [a-z0-9] one dash, no dash at the ends,
 * `-<attempt>` appended from the second attempt on (the retry after a 409). The caller checks the
 * result with pimAccountIdError. */
export function accountIdFromAddress(address: string, attempt: number): string {
  const base = address
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '');
  return attempt > 1 ? `${base}-${String(attempt)}` : base;
}

/** The i18n key of a failed check's sentence. A rejected password on a provider that wants an app
 * password says so, since the account password is the likeliest mistake there. */
export function verifyMessageKey(
  failure: { step: 'imap' | 'smtp'; reason: PimVerifyReason },
  appPasswordRequired: boolean,
): string {
  if (appPasswordRequired && failure.reason === 'auth_failed') {
    return 'settings.mail.verify.appPassword';
  }
  return `settings.mail.verify.${failure.reason}`;
}

const ADDRESS_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

/** Whether the address has the shape worth a discovery call; the PIM has the final word. */
export function isValidAddress(address: string): boolean {
  return ADDRESS_RE.test(address.trim());
}
