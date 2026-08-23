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

/** One rendered choice: the label the operator reads, the value the server records. */
export interface ApprovalOption {
  readonly label: string;
  readonly value: string;
}

/**
 * Parse the raw JSON option set into the choices the inline card renders as buttons.
 *
 * The server persists `paused_states.options` from agent.PauseOption, which marshals as
 * `[{label, value}]` — NOT as a string array. This function used to accept only strings, so
 * every options pause silently fell through to the free-text box and no option button had
 * ever rendered. Both shapes are accepted now: a bare string is its own label and value (the
 * shape ask_user's schema also accepts), an object needs a non-empty label and falls back to
 * the label when the value is missing. Anything else is dropped rather than thrown, so an
 * unexpected payload still degrades to free text instead of blanking the card.
 */
export function parseOptions(options: unknown): ApprovalOption[] {
  if (!Array.isArray(options)) return [];
  return options.flatMap((o): ApprovalOption[] => {
    if (typeof o === 'string') return o === '' ? [] : [{ label: o, value: o }];
    if (o === null || typeof o !== 'object') return [];
    const { label, value } = o as { label?: unknown; value?: unknown };
    if (typeof label !== 'string' || label === '') return [];
    return [{ label, value: typeof value === 'string' && value !== '' ? value : label }];
  });
}
