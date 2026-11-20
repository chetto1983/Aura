import { HttpError } from '../../api/json';
import {
  createPimAccount,
  listPimAccounts,
  PIM_PROVIDER_NOT_CONFIGURED,
  type PimAccount,
  type PimProviderId,
} from '../../governance/pimApi';
import { accountIdFromAddress, sameAddress } from './mailConnectLogic';

// mailAccountCreate is the create step of the mail screen: the account id comes from the address, a
// duplicate id under another address moves to the next suffix, and an account already holding this
// address is handed back for repair instead of being created twice.

// The relay gives a create that signs in 25 s (internal/agui/connect_pim_account_api.go
// pimVerifyClient); the browser waits a little longer so the relay's own answer arrives first.
const CREATE_TIMEOUT_MS = 30_000;
const LAST_ATTEMPT = 9;

const OAUTH_KINDS = ['google', 'microsoft365', 'outlook.com'] as const;

export type OAuthKind = (typeof OAUTH_KINDS)[number];

export function isOAuthKind(kind: string): kind is OAuthKind {
  return (OAUTH_KINDS as readonly string[]).includes(kind);
}

export type MailOutcome =
  | {
      readonly kind: 'created';
      readonly account: PimAccount;
      readonly smtpWarning: string | undefined;
    }
  | { readonly kind: 'existing'; readonly account: PimAccount };

export interface MailCreateRequest {
  readonly address: string;
  readonly provider: PimProviderId;
  readonly providerConfig: Record<string, string>;
  readonly recheck: boolean;
}

function isDuplicateId(error: unknown): boolean {
  return (
    error instanceof HttpError &&
    error.status === 409 &&
    error.reason !== PIM_PROVIDER_NOT_CONFIGURED
  );
}

async function accountWithAddress(address: string): Promise<PimAccount | undefined> {
  const { accounts } = await listPimAccounts();
  return accounts.find((account) => sameAddress(account.displayName, address));
}

/** Creates the account under the first free id, `-2` to `-9` after a duplicate. `recheck` reads the
 * list first: after a timeout the earlier create may have landed, and creating again would only
 * collide with it. A 409 that is not a duplicate id, and any other error, is thrown. */
export async function createMailAccount({
  address,
  provider,
  providerConfig,
  recheck,
}: MailCreateRequest): Promise<MailOutcome> {
  if (recheck) {
    const landed = await accountWithAddress(address);
    if (landed?.provider === provider) {
      return { kind: 'created', account: landed, smtpWarning: undefined };
    }
  }
  for (let attempt = 1; ; attempt++) {
    try {
      const { smtpWarning, ...account } = await createPimAccount(
        {
          id: accountIdFromAddress(address, attempt),
          displayName: address,
          provider,
          providerConfig,
        },
        AbortSignal.timeout(CREATE_TIMEOUT_MS),
      );
      return { kind: 'created', account, smtpWarning };
    } catch (error) {
      if (!isDuplicateId(error)) throw error;
      if (attempt === 1) {
        const existing = await accountWithAddress(address);
        if (existing !== undefined) return { kind: 'existing', account: existing };
      }
      if (attempt === LAST_ATTEMPT) throw error;
    }
  }
}
