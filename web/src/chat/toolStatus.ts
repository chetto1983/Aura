// Tool-activity status derivation (D-02). Kept in its own module so
// ToolActivityCard.tsx only exports a component (react-refresh/only-export-components).

import type { ToolCallMessagePartStatus } from '@assistant-ui/react';
import type { DisplayPayload } from './displays/types';

export type ToolStatus = 'running' | 'done' | 'error' | 'canceled' | 'interrupted';

export interface ToolStatusInput {
  readonly result?: string | undefined;
  readonly isError?: boolean | undefined;
  readonly display?: DisplayPayload | undefined;
  readonly partStatus?: ToolCallMessagePartStatus | undefined;
  /** The turn is paused on the person's answer to this call (snapshot `awaitingInput`). */
  readonly awaitingInput?: boolean | undefined;
}

/** A terminal native part without a result cannot still be executing, unless its turn
 *  is paused waiting on the person: that call is still open, not lost. */
export function toolStatus(input: ToolStatusInput): ToolStatus {
  if (input.awaitingInput === true && input.result === undefined) return 'running';
  if (input.isError === true) return 'error';
  if (input.display?.type === 'code' && input.display.code?.cancelled === true) return 'canceled';
  if (
    input.result === undefined &&
    (input.partStatus?.type === 'complete' || input.partStatus?.type === 'incomplete')
  )
    return 'interrupted';
  return input.result === undefined ? 'running' : 'done';
}
