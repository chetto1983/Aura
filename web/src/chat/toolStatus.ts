// Tool-activity status derivation (D-02). Kept in its own module so
// ToolActivityCard.tsx only exports a component (react-refresh/only-export-components).

import type { DisplayPayload } from './displays/types';

export type ToolStatus = 'running' | 'done' | 'error' | 'canceled';

export interface ToolStatusInput {
  readonly result?: string | undefined;
  readonly isError?: boolean | undefined;
  readonly display?: DisplayPayload | undefined;
}

/** Derive the status purely from the presence/shape of the result. */
export function toolStatus(input: ToolStatusInput): ToolStatus {
  if (input.isError === true) return 'error';
  if (input.display?.type === 'code' && input.display.code?.cancelled === true) return 'canceled';
  return input.result === undefined ? 'running' : 'done';
}
