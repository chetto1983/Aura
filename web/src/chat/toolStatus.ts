// Tool-activity status derivation (D-02). Kept in its own module so
// ToolActivityCard.tsx only exports a component (react-refresh/only-export-components).

export type ToolStatus = 'running' | 'done' | 'error';

export interface ToolStatusInput {
  readonly result?: string;
  readonly isError?: boolean;
}

/** Derive the status purely from the presence/shape of the result. */
export function toolStatus(input: ToolStatusInput): ToolStatus {
  if (input.isError === true) return 'error';
  return input.result === undefined ? 'running' : 'done';
}
