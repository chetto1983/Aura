import type { OpenRouterKeysResult } from '../settings/settingsApi';

/** What the route step tells the admin after a save: the masked labels of the keys the save
 * minted, and what OpenRouter refused. */
export interface OpenRouterKeysSummary {
  /** The caller's own key, when this save minted it; '' otherwise. */
  readonly ownLabel: string;
  /** The services key, when this save minted it; '' otherwise. Minting it is what restarts Aura. */
  readonly servicesLabel: string;
  /** The LAST run's errors: a later write in the same save runs the reconciler again, and what it
   * still reports is what is still wrong. */
  readonly errors: readonly string[];
}

export function summarizeOpenRouterKeys(
  runs: readonly OpenRouterKeysResult[],
  ownIdentityId: string,
): OpenRouterKeysSummary {
  let ownLabel = '';
  let servicesLabel = '';
  for (const run of runs) {
    ownLabel = run.minted_labels?.[ownIdentityId] ?? ownLabel;
    servicesLabel = run.services_label ?? servicesLabel;
  }
  return { ownLabel, servicesLabel, errors: runs.at(-1)?.errors ?? [] };
}
