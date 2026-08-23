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

/**
 * The gateway's approval SCOPES (PRD amendment #127), carried on an option's value as
 * `gateway_scope:<scope>:<subject>`. The value is a semantic code, never display text —
 * the same split ResolveOutcome uses, because the gateway is not locale-aware and each
 * surface renders its own copy.
 */
export type ApprovalScope = 'once' | 'session' | 'always';

export interface ScopeChoice {
  readonly scope: ApprovalScope;
  /** The tool (plus the verb of a multiplexed tool) the scope would cover. */
  readonly subject: string;
}

const SCOPE_PREFIX = 'gateway_scope:';
const SCOPES: readonly ApprovalScope[] = ['once', 'session', 'always'];

/**
 * Decode a gateway scope option value, or null when the option is an ordinary choice.
 * The subject is the whole remainder after the second colon: neither a tool name nor a
 * multiplexed action contains a colon, so it needs no unescaping.
 */
export function parseScopeChoice(value: string): ScopeChoice | null {
  if (!value.startsWith(SCOPE_PREFIX)) return null;
  const rest = value.slice(SCOPE_PREFIX.length);
  const separator = rest.indexOf(':');
  if (separator < 0) return null;
  const scope = rest.slice(0, separator);
  const subject = rest.slice(separator + 1);
  if (!SCOPES.includes(scope as ApprovalScope) || subject === '') return null;
  return { scope: scope as ApprovalScope, subject };
}
