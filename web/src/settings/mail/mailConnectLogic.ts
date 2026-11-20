// Pure rules of the mailbox connect flow: the account id an address maps to, which sentence a failed
// check gets and where it goes, whether an address is worth sending to the PIM, and the config an
// IMAP create sends.

import type { PimDiscovery, PimVerifyReason } from '../../governance/pimApi';

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

/** Whether a failed check is about the servers rather than the password: it then belongs under
 * «Impostazioni server», opened, not on the password field. */
export function isServerFailure(failure: {
  step: 'imap' | 'smtp';
  reason: PimVerifyReason;
}): boolean {
  return (
    failure.step === 'smtp' || failure.reason === 'unreachable' || failure.reason === 'tls_failed'
  );
}

/** Two spellings of one mailbox: the domain and, in practice, the local part ignore case. */
export function sameAddress(a: string, b: string): boolean {
  return a.trim().toLowerCase() === b.trim().toLowerCase();
}

export const SERVER_KEYS = ['imapHost', 'imapPort', 'smtpHost', 'smtpPort', 'username'] as const;

export type ServerValues = Record<(typeof SERVER_KEYS)[number], string>;

/** The server fields as discovery found them; the username falls back to the address. */
export function serverValuesFrom(discovery: PimDiscovery, address: string): ServerValues {
  return {
    imapHost: discovery.imapHost ?? '',
    imapPort: portText(discovery.imapPort),
    smtpHost: discovery.smtpHost ?? '',
    smtpPort: portText(discovery.smtpPort),
    username: discovery.username ?? address,
  };
}

/** The providerConfig an IMAP create sends. The password goes verbatim: a space can be part of it.
 * A discovered security key rides along only while its port is the discovered one; for a port the
 * person typed, the PIM picks the security from the port. */
export function mailProviderConfig(
  servers: ServerValues,
  password: string,
  discovery: PimDiscovery,
): Record<string, string> {
  const config: Record<string, string> = {};
  for (const key of SERVER_KEYS) {
    const value = servers[key].trim();
    if (value !== '') config[key] = value;
  }
  if (discovery.imapSecurity !== undefined && servers.imapPort === portText(discovery.imapPort)) {
    config.imapSecurity = discovery.imapSecurity;
  }
  if (discovery.smtpSecurity !== undefined && servers.smtpPort === portText(discovery.smtpPort)) {
    config.smtpSecurity = discovery.smtpSecurity;
  }
  config.password = password;
  return config;
}

function portText(port: number | undefined): string {
  return port === undefined ? '' : String(port);
}

/** The help link of a provider, kept only when it is https: ISPDB is a third party. */
export function httpsUrl(raw: string | undefined): string | null {
  if (raw === undefined) return null;
  try {
    return new URL(raw).protocol === 'https:' ? raw : null;
  } catch {
    return null;
  }
}
