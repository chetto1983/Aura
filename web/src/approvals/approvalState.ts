import type { Approval } from './useApprovals';

// approvalState holds the pure (non-component) helpers shared by the approval
// surfaces, kept out of the .tsx files so those export ONLY components
// (react-refresh/only-export-components, a blocking lint gate).

/**
 * True when a pending row is a D-06 terminal state — expired / auto-terminated.
 * The server marks it via the `terminal` flag; such a row is rendered with its
 * explicit terminal copy + warning tone and its verbs disabled, NEVER silently
 * dropped (APRV-03 / T-25-18).
 */
export function isTerminal(approval: Pick<Approval, 'terminal'>): boolean {
  return approval.terminal === true;
}

/**
 * Parse the raw JSON option set into a string[] the inline card can render as
 * buttons. The server projects `options` as raw JSON (string[] | null | object);
 * anything that is not a clean array of strings yields [] so the card falls back to
 * the free-text path (defensive — an unexpected shape never throws).
 */
export function parseOptions(options: unknown): string[] {
  if (!Array.isArray(options)) return [];
  return options.filter((o): o is string => typeof o === 'string');
}
