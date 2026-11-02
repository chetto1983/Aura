import { pimDeviceStart, pimGoogleStart, type PimDeviceStart, type PimGoogleStart } from './pimApi';
import type { PimAuthFlow } from './pimProviders';

// pimConnect.ts is the sign-in step of an account that already exists: right after the form creates
// it, or later from its row when the first attempt did not finish.

// PimConnectResult carries the connect outcome. `id` is a snapshot, so the poll target never tracks
// the live Account ID field, and `startFailed` keeps the (already-created) account's id + flow so the
// operator can retry sign-in WITHOUT re-creating the account (which would hit the sidecar's
// 409-duplicate).
export type PimConnectResult =
  | { readonly kind: 'google'; readonly id: string; readonly start: PimGoogleStart }
  | { readonly kind: 'device'; readonly id: string; readonly start: PimDeviceStart }
  | { readonly kind: 'none' }
  | { readonly kind: 'startFailed'; readonly id: string; readonly authFlow: PimAuthFlow };

// startConnect maps a failure to a `startFailed` result (NOT a thrown error) so a created account
// whose sign-in didn't start is reported as a recoverable state, not as a total create failure.
export async function startConnect(id: string, flow: PimAuthFlow): Promise<PimConnectResult> {
  if (flow === 'none') return { kind: 'none' };
  try {
    if (flow === 'google') return { kind: 'google', id, start: await pimGoogleStart(id) };
    return { kind: 'device', id, start: await pimDeviceStart(id) };
  } catch {
    return { kind: 'startFailed', id, authFlow: flow };
  }
}
