// pimProviders.ts is the single source of truth for what the cockpit calendar/PIM connect wizard can
// configure, per provider. It mirrors the aura-pim-mcp sidecar's contract EXACTLY (operator directive
// 2026-06-27: "configure all variable on frontend not just google"):
//
//   - the provider set is AccountValidation.KnownProviders (google, microsoft365, outlook.com, imap,
//     ics, json);
//   - each field's `key` is the EXACT lowercase providerConfig key the provider service reads via
//     ProviderConfig.TryGetValue (GoogleProviderService → clientId/clientSecret; M365/OutlookCom →
//     tenantId/clientId; IcsProviderService → icsUrl; ImapProviderService → imapHost/imapPort/
//     smtpHost/smtpPort/username/password; JsonCalendarProviderService → source/filePath/oneDrivePath/
//     authAccountId). The sidecar validator is case-insensitive but the provider READS are
//     case-sensitive, so the casing here is load-bearing — a mismatch validates yet silently fails;
//   - `authFlow` routes the post-create connect step: 'google' = web-redirect (pimGoogleStart),
//     'device' = Microsoft/Outlook device-code (pimDeviceStart + poll), 'none' = credentials/URL ARE
//     the connection (imap/ics/json-local), nothing more to do.
//
// i18n label keys live under governance.mcp.calendar.{providers,fields,sources}.*. `outlook.com`
// carries a dot (the i18next key separator), so its provider label key is decoupled to `outlookCom`.

import type { PimProviderId } from './pimApi';

export type PimAuthFlow = 'google' | 'device' | 'none';

export interface PimFieldDef {
  /** The exact lowercase providerConfig key the sidecar reads. */
  readonly key: string;
  /** i18n key under governance.mcp.calendar.fields.* */
  readonly labelKey: string;
  readonly type: 'text' | 'password' | 'select';
  readonly required: boolean;
  /** Options for `select` fields ({value sent verbatim, i18n labelKey}). */
  readonly options?: readonly { readonly value: string; readonly labelKey: string }[];
  /** Native input placeholder (e.g. a default port) — literal, not translated. */
  readonly placeholder?: string;
  /** i18n key under governance.mcp.calendar.fields.* for an explanatory hint. */
  readonly hintKey?: string;
  /** Show this field only when another field's value equals `value` (json source branch). */
  readonly showIf?: { readonly key: string; readonly value: string };
}

export interface PimProviderDef {
  /** The provider id sent verbatim as the account `provider` field. */
  readonly id: PimProviderId;
  /** i18n key under governance.mcp.calendar.providers.* */
  readonly labelKey: string;
  readonly authFlow: PimAuthFlow;
  readonly fields: readonly PimFieldDef[];
}

const F = 'governance.mcp.calendar.fields';
const P = 'governance.mcp.calendar.providers';
const S = 'governance.mcp.calendar.sources';

export const PIM_PROVIDERS: readonly PimProviderDef[] = [
  {
    id: 'google',
    labelKey: `${P}.google`,
    authFlow: 'google',
    fields: [
      { key: 'clientId', labelKey: `${F}.clientId`, type: 'text', required: true },
      { key: 'clientSecret', labelKey: `${F}.clientSecret`, type: 'password', required: true },
    ],
  },
  {
    id: 'microsoft365',
    labelKey: `${P}.microsoft365`,
    authFlow: 'device',
    fields: [
      {
        key: 'tenantId',
        labelKey: `${F}.tenantId`,
        type: 'text',
        required: true,
        hintKey: `${F}.tenantIdHint`,
      },
      { key: 'clientId', labelKey: `${F}.clientId`, type: 'text', required: true },
    ],
  },
  {
    id: 'outlook.com',
    labelKey: `${P}.outlookCom`,
    authFlow: 'device',
    fields: [
      {
        key: 'tenantId',
        labelKey: `${F}.tenantId`,
        type: 'text',
        required: true,
        hintKey: `${F}.tenantIdHint`,
      },
      { key: 'clientId', labelKey: `${F}.clientId`, type: 'text', required: true },
    ],
  },
  {
    id: 'imap',
    labelKey: `${P}.imap`,
    authFlow: 'none',
    fields: [
      { key: 'imapHost', labelKey: `${F}.imapHost`, type: 'text', required: true },
      {
        key: 'imapPort',
        labelKey: `${F}.imapPort`,
        type: 'text',
        required: false,
        placeholder: '993',
      },
      { key: 'smtpHost', labelKey: `${F}.smtpHost`, type: 'text', required: true },
      {
        key: 'smtpPort',
        labelKey: `${F}.smtpPort`,
        type: 'text',
        required: false,
        placeholder: '587',
      },
      { key: 'username', labelKey: `${F}.username`, type: 'text', required: true },
      { key: 'password', labelKey: `${F}.password`, type: 'password', required: true },
    ],
  },
  {
    id: 'ics',
    labelKey: `${P}.ics`,
    authFlow: 'none',
    fields: [
      {
        key: 'icsUrl',
        labelKey: `${F}.icsUrl`,
        type: 'text',
        required: true,
        hintKey: `${F}.icsUrlHint`,
      },
    ],
  },
  {
    id: 'json',
    labelKey: `${P}.json`,
    authFlow: 'none',
    fields: [
      {
        key: 'source',
        labelKey: `${F}.source`,
        type: 'select',
        required: true,
        options: [
          { value: 'local', labelKey: `${S}.local` },
          { value: 'onedrive', labelKey: `${S}.onedrive` },
        ],
      },
      {
        key: 'filePath',
        labelKey: `${F}.filePath`,
        type: 'text',
        required: true,
        showIf: { key: 'source', value: 'local' },
      },
      {
        key: 'oneDrivePath',
        labelKey: `${F}.oneDrivePath`,
        type: 'text',
        required: true,
        showIf: { key: 'source', value: 'onedrive' },
      },
      {
        key: 'authAccountId',
        labelKey: `${F}.authAccountId`,
        type: 'text',
        required: false,
        hintKey: `${F}.authAccountIdHint`,
        showIf: { key: 'source', value: 'onedrive' },
      },
    ],
  },
];

/** Look up a provider definition by id; defaults to Google (the first/most-common provider). */
export function pimProviderById(id: string): PimProviderDef {
  const found = PIM_PROVIDERS.find((p) => p.id === id);
  if (found !== undefined) return found;
  const [first] = PIM_PROVIDERS;
  if (first === undefined) throw new Error('PIM_PROVIDERS is empty');
  return first;
}

/** A field is visible when it has no showIf, or the gating field currently holds the showIf value. */
export function pimFieldVisible(field: PimFieldDef, values: Record<string, string>): boolean {
  if (field.showIf === undefined) return true;
  return values[field.showIf.key] === field.showIf.value;
}

/** The initial providerConfig draft for a provider: every select seeds to its first option so a
 * required select is never empty; text/password fields start blank. */
export function pimInitialValues(def: PimProviderDef): Record<string, string> {
  const values: Record<string, string> = {};
  for (const field of def.fields) {
    const firstOption = field.options?.[0];
    values[field.key] = field.type === 'select' && firstOption ? firstOption.value : '';
  }
  return values;
}

/** The providerConfig actually submitted: only VISIBLE fields with a non-empty trimmed value. (An
 * optional hidden/blank field is dropped so the sidecar sees just the keys it needs.) */
export function pimSubmitConfig(
  def: PimProviderDef,
  values: Record<string, string>,
): Record<string, string> {
  const out: Record<string, string> = {};
  for (const field of def.fields) {
    if (!pimFieldVisible(field, values)) continue;
    const value = (values[field.key] ?? '').trim();
    if (value !== '') out[field.key] = value;
  }
  return out;
}

/** The visible required fields that are still empty — drives inline validation before submit. */
// PIM_ACCOUNT_ID_RE mirrors the sidecar's AccountValidation.SlugRegex (`^[a-z0-9][a-z0-9\-_]*$`):
// a lowercase letter or digit, then lowercase letters, digits, hyphens and underscores. Anything
// else is a 400 from the sidecar, so the wizard has to refuse it before the request is sent.
const PIM_ACCOUNT_ID_RE = /^[a-z0-9][a-z0-9\-_]*$/;

/** normalizePimAccountId case-folds what the operator types. Case is the one violation with a
 * single obvious correction, so it is fixed silently; everything else is reported rather than
 * rewritten, because guessing at what a space or an @ was meant to be would change their input. */
export function normalizePimAccountId(raw: string): string {
  return raw.toLowerCase();
}

/** pimAccountIdError classifies the id the wizard would submit (trimmed, as the submit path trims):
 * null when the sidecar will accept it, 'required' when empty, 'slug' when it breaks the rule. */
export function pimAccountIdError(raw: string): 'required' | 'slug' | null {
  const id = raw.trim();
  if (id === '') return 'required';
  return PIM_ACCOUNT_ID_RE.test(id) ? null : 'slug';
}

export function pimMissingRequired(
  def: PimProviderDef,
  values: Record<string, string>,
): readonly string[] {
  return def.fields
    .filter((field) => field.required && pimFieldVisible(field, values))
    .filter((field) => (values[field.key] ?? '').trim() === '')
    .map((field) => field.key);
}
